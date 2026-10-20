package net

import (
	"sync"

	gameio "rwhps/internal/io"
	"rwhps/internal/io/packet"
	"rwhps/internal/net/core"
)

// GroupNet 对应 GroupNet
// 支持 TCP Channel 群组 + UDP ConnectionAgreement 群组的广播
type GroupNet struct {
	mu          sync.RWMutex
	channels    map[*core.Channel]struct{}
	agreements  map[*core.ConnectionAgreement]struct{}

	singleUserRelay bool
}

// NewGroupNet 对应 GroupNet()
func NewGroupNet() *GroupNet {
	return &GroupNet{
		channels:   make(map[*core.Channel]struct{}),
		agreements: make(map[*core.ConnectionAgreement]struct{}),
	}
}

// AddChannel 对应 add(channel)
func (g *GroupNet) AddChannel(ch *core.Channel) {
	g.mu.Lock()
	g.channels[ch] = struct{}{}
	g.mu.Unlock()
}

// AddAgreement 对应 add(connectionAgreement)
func (g *GroupNet) AddAgreement(a *core.ConnectionAgreement) {
	g.mu.Lock()
	g.agreements[a] = struct{}{}
	g.mu.Unlock()
}

// SetSingleUserRelay 对应 Data.config.singleUserRelay
func (g *GroupNet) SetSingleUserRelay(v bool) { g.singleUserRelay = v }

// Broadcast 对应 broadcast(msg)
func (g *GroupNet) Broadcast(msg any) {
	if g.singleUserRelay {
		return
	}
	g.BroadcastAndUDP(msg)
}

// BroadcastAndUDP 对应 broadcastAndUDP(msg)
func (g *GroupNet) BroadcastAndUDP(msg any) {
	p, ok := msg.(*packet.Packet)
	if !ok {
		return
	}

	g.mu.RLock()
	chans := make([]*core.Channel, 0, len(g.channels))
	for ch := range g.channels {
		chans = append(chans, ch)
	}
	ags := make([]*core.ConnectionAgreement, 0, len(g.agreements))
	for a := range g.agreements {
		ags = append(ags, a)
	}
	g.mu.RUnlock()

	for _, ch := range chans {
		_ = ch.WritePacket(p)
	}
	for _, a := range ags {
		_ = a.Send(p)
	}
}

// RemoveChannel 对应 remove(channel)
func (g *GroupNet) RemoveChannel(ch *core.Channel) {
	g.mu.Lock()
	delete(g.channels, ch)
	g.mu.Unlock()
}

// RemoveAgreement 对应 remove(connectionAgreement)
func (g *GroupNet) RemoveAgreement(a *core.ConnectionAgreement) {
	g.mu.Lock()
	delete(g.agreements, a)
	g.mu.Unlock()
}

// Flush 对应 flush()
func (g *GroupNet) Flush() {}

// Disconnect 对应 disconnect()
func (g *GroupNet) Disconnect() {
	g.mu.Lock()
	for ch := range g.channels {
		_ = ch.Close()
	}
	for a := range g.agreements {
		_ = a.Close(g)
	}
	g.mu.Unlock()
}

// ChannelSize 返回 TCP 连接数
func (g *GroupNet) ChannelSize() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.channels)
}

// AgreementSize 返回 UDP 协议数
func (g *GroupNet) AgreementSize() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.agreements)
}

// NetStaticData 对应 NetStaticData
type NetStaticData struct {
	GroupNet    *GroupNet
	NetServices []*netServiceEntry
	BlackList   *BlackList
}

type netServiceEntry struct {
	ID string
}

// BlackList 对应 BlackList (暂留占位)
type BlackList struct {
	mu    sync.RWMutex
	items map[string]struct{}
}

// NewBlackList 对应 BlackList()
func NewBlackList() *BlackList {
	return &BlackList{items: make(map[string]struct{})}
}

// Add 添加 IP 到黑名单
func (b *BlackList) Add(s string) {
	b.mu.Lock()
	b.items[s] = struct{}{}
	b.mu.Unlock()
}

// Remove 删除 IP
func (b *BlackList) Remove(s string) {
	b.mu.Lock()
	delete(b.items, s)
	b.mu.Unlock()
}

// Contains 检查 IP 是否在黑名单
func (b *BlackList) Contains(s string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.items[s]
	return ok
}

// CheckProtocolIsServer 对应 NetStaticData.checkProtocolIsServer
func CheckProtocolIsServer(t core.NetType) bool {
	switch t {
	case core.ServerProtocol, core.ServerProtocolOld, core.ServerTestProtocol:
		return true
	case core.RelayProtocol, core.RelayMulticastProtocol:
		return true
	default:
		return false
	}
}

// _ 占位避免未使用导入
var _ = gameio.ErrEOF