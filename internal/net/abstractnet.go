package net

import (
	"sync"

	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
	"rwhps/internal/net/core"
	"rwhps/internal/util/log"
)

// AcceptorIdleStateTrigger 对应 AcceptorIdleStateTrigger
// 检测 idle 超过阈值的连接并清理
type AcceptorIdleStateTrigger struct {
	AbstractNet *AbstractNet

	connectNum int

	mu sync.Mutex
}

// ChannelActive 对应 channelActive
func (t *AcceptorIdleStateTrigger) ChannelActive() {}

// ChannelUnregistered 对应 channelRegistered -> connectNum.IncrementAndGet
func (t *AcceptorIdleStateTrigger) ChannelUnregistered() {
	t.mu.Lock()
	t.connectNum++
	t.mu.Unlock()
}

// ChannelDisconnected 对应 channelUnregistered -> connectNum.DecrementAndGet
func (t *AcceptorIdleStateTrigger) ChannelDisconnected() {
	t.mu.Lock()
	t.connectNum--
	t.mu.Unlock()
}

// GetConnectSize 对应 connectNum.get
func (t *AcceptorIdleStateTrigger) GetConnectSize() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.connectNum
}

// Clear 对应 clear(ctx) 清理连接
func (t *AcceptorIdleStateTrigger) Clear(ch *core.Channel) {
	tc := ch.GetTypeConnect()
	if tc != nil {
		if anc := tc.AbstractNetConnect(); anc != nil {
			anc.Disconnect()
			return
		}
	}
	_ = ch.Close()
}

// TimeoutDetection 对应 TimeoutDetection.checkTimeoutDetection
// 5min 未响应 (inputPassword) 或 3min 未响应 (normal) 即视为超时
func TimeoutDetection(ch *core.Channel) bool {
	tc := ch.GetTypeConnect()
	if tc == nil {
		return true
	}
	net := tc.AbstractNetConnect()
	if net.ConnectReceiveData.ReceiveBigPacket {
		return false
	}
	now := ch.LastReceiveMs()
	if net.ConnectReceiveData.InputPassword {
		return now > net.LastReceivedTime()+300*1000
	}
	return now > net.LastReceivedTime()+180*1000
}

// AbstractNet 对应 AbstractNet (Go 版: 抽象启动期行为 + 解码器)
// 与 Netty 版差异: 不用 pipeline, 直接驱动 read 协程 + PacketStream.
type AbstractNet struct {
	rwHps core.IRwHps

	IdleTrigger *AcceptorIdleStateTrigger

	// channels 记录本监听器上的存活连接, 供 Close 时统一断开
	channelsMu sync.Mutex
	channels   map[*core.Channel]struct{}
}

// NewAbstractNet 对应 AbstractNet(rwHps).init()
func NewAbstractNet(rwHps core.IRwHps) *AbstractNet {
	// 设置全局 Log/Ping 命令钩子, 供 AbstractNetConnect 使用
	core.LogCommand = nil
	core.PingCommand = nil
	return &AbstractNet{
		rwHps:       rwHps,
		IdleTrigger: &AcceptorIdleStateTrigger{},
		channels:    make(map[*core.Channel]struct{}),
	}
}

// trackChannel 登记存活连接
func (n *AbstractNet) trackChannel(ch *core.Channel) {
	n.channelsMu.Lock()
	n.channels[ch] = struct{}{}
	n.channelsMu.Unlock()
}

// untrackChannel 注销连接
func (n *AbstractNet) untrackChannel(ch *core.Channel) {
	n.channelsMu.Lock()
	delete(n.channels, ch)
	n.channelsMu.Unlock()
}

// RwHps 对应 abstractNet.rwHps
func (n *AbstractNet) RwHps() core.IRwHps { return n.rwHps }

// GetConnectSize 对应 abstractNet.getConnectSize
func (n *AbstractNet) GetConnectSize() int { return n.IdleTrigger.GetConnectSize() }

// HandleChannel 接受一个新连接, 注册并启动 read 协程
func (n *AbstractNet) HandleChannel(ch *core.Channel) {
	n.serveChannel(ch, nil)
}

// HandleChannelPeeked 与 HandleChannel 等价, 但先 feed 已 peek 的字节
// 用于 StartMixProtocol 在嗅探后直接送入解码器
func (n *AbstractNet) HandleChannelPeeked(ch *core.Channel, peeked []byte) {
	n.serveChannel(ch, peeked)
}

// serveChannel 读取循环: 解码 -> 分发 -> 退出时清理连接
func (n *AbstractNet) serveChannel(ch *core.Channel, peeked []byte) {
	n.IdleTrigger.ChannelUnregistered()
	n.trackChannel(ch)
	handler := NewServerHandler(ch, n)
	dec := NewPacketStream()
	go func() {
		defer func() {
			n.untrackChannel(ch)
			n.IdleTrigger.Clear(ch)
			n.IdleTrigger.ChannelDisconnected()
		}()
		if len(peeked) > 0 {
			ch.RefreshLastAck()
			pkts, closed := dec.Feed(peeked)
			if closed {
				return
			}
			n.dispatchAll(handler, pkts)
		}
		buf := make([]byte, 64*1024)
		for {
			ch.RefreshLastAck()
			readNum, err := ch.Conn.Read(buf)
			if readNum > 0 {
				ch.RefreshLastAck()
				pkts, closed := dec.Feed(buf[:readNum])
				if closed {
					return
				}
				n.dispatchAll(handler, pkts)
			}
			if err != nil {
				return
			}
		}
	}()
}

// dispatchAll 逐包分发
func (n *AbstractNet) dispatchAll(handler func(*packet.Packet) error, pkts []*packet.Packet) {
	for _, p := range pkts {
		if !validPacket(p) {
			continue
		}
		if err := handler(p); err != nil {
			log.Errorf("handle packet err: %v", err)
		}
	}
}

// Close 对应 abstractNet.close(): 断开本监听器上的全部存活连接
func (n *AbstractNet) Close() {
	n.channelsMu.Lock()
	chans := make([]*core.Channel, 0, len(n.channels))
	for ch := range n.channels {
		chans = append(chans, ch)
	}
	n.channels = make(map[*core.Channel]struct{})
	n.channelsMu.Unlock()

	for _, ch := range chans {
		if tc := ch.GetTypeConnect(); tc != nil {
			if anc := tc.AbstractNetConnect(); anc != nil {
				anc.Disconnect()
				continue
			}
		}
		_ = ch.Close()
	}
}

// validPacket 检查包是否可处理
func validPacket(p *packet.Packet) bool {
	if p == nil {
		return false
	}
	if p.Type == nil {
		return false
	}
	if p.Type.TypeInt() == int(packettype.NOT_RESOLVED) {
		log.Warn("[GamePacketDecoder] Unknown Protocol")
		return false
	}
	return true
}