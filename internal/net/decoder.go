package net

import (
	"encoding/binary"
	"errors"
	"io"
	"sync"

	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
	"rwhps/internal/net/core"
	"rwhps/internal/util/log"
)

// GamePacketDecoder 对应 GamePacketDecoder
//   1 2 3 4  5  6  7  8  ...
//  +-+-+-+-+-+-+-+-+---------------+
//  |0 |0 |0 |0 |0|0|0|0| Data|
//  +-+-+-+-+-+-+-+-+---------------+
//  |Data length|  Type | Data
//  |    Packet Head    | Data
//  +---------------+---------------+
const (
	HeaderSize    = 8
	MaxPacketSize = 50_000_000 // 50MB
)

// ErrPacketTooLarge 包长度超过 50MB
var ErrPacketTooLarge = errors.New("package size exceeds maximum")

// ErrUnknownProtocol 未知协议
var ErrUnknownProtocol = errors.New("unknown protocol")

// PacketStream 解码器, 复用 Netty ByteToMessageDecoder 的缓存思路:
// 每帧读取 [4 len][4 type][len bytes] 然后产出 Packet.
type PacketStream struct {
	mu sync.Mutex

	readPacketLength int
	readPacketType   int

	stopped bool
	buf     []byte
}

// NewPacketStream 构造解码器
func NewPacketStream() *PacketStream {
	return &PacketStream{
		readPacketLength: HeaderSize,
		readPacketType:   -1,
		buf:              make([]byte, 0, 64*1024),
	}
}

// Feed 喂入字节, 返回解析到的 Packet 与是否关闭.
// 一次 Feed 可能产生多个 Packet, 调用方循环调用.
func (d *PacketStream) Feed(data []byte) ([]*packet.Packet, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.stopped {
		return nil, true
	}
	d.buf = append(d.buf, data...)

	var out []*packet.Packet
	for {
		// 阶段 1 - 读取 [len][type] 头
		if d.readPacketType == -1 {
			if len(d.buf) < HeaderSize {
				return out, false
			}
			d.readPacketLength = int(int32(binary.BigEndian.Uint32(d.buf[0:4])))
			d.readPacketType = int(int32(binary.BigEndian.Uint32(d.buf[4:8])))

			if d.readPacketLength < 0 || d.readPacketType < 0 {
				log.Debug("Exception Packets, Close")
				d.stopped = true
				return nil, true
			}
			if d.readPacketLength > MaxPacketSize {
				log.Debug("Package size exceeds maximum")
				d.stopped = true
				return nil, true
			}
			d.buf = d.buf[HeaderSize:]
		}

		// 阶段 2 - 读取 [len] 字节数据
		if len(d.buf) < d.readPacketLength {
			return out, false
		}

		pt := packettype.Resolver.From(d.readPacketType)
		if pt.TypeInt() == int(packettype.NOT_RESOLVED) {
			log.Warnf("[GamePacketDecoder] Unknown Protocol Type: %d", d.readPacketType)
			d.stopped = true
			return nil, true
		}

		data := make([]byte, d.readPacketLength)
		copy(data, d.buf[:d.readPacketLength])
		d.buf = d.buf[d.readPacketLength:]
		out = append(out, packet.NewPacket(pt, data))

		d.readPacketLength = HeaderSize
		d.readPacketType = -1
	}
}

// ReadPacket 从 io.Reader 读取一个完整 Packet.
// 与 Feed 等价但直接驱动 Reader (用于 NetService 串行读循环).
func (d *PacketStream) ReadPacket(r io.Reader) (*packet.Packet, error) {
	var head [HeaderSize]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return nil, err
	}
	l := int(int32(binary.BigEndian.Uint32(head[0:4])))
	t := int(int32(binary.BigEndian.Uint32(head[4:8])))
	if l < 0 || t < 0 {
		log.Debug("Exception Packets, Close")
		return nil, ErrPacketTooLarge
	}
	if l > MaxPacketSize {
		log.Debug("Package size exceeds maximum")
		return nil, ErrPacketTooLarge
	}
	data := make([]byte, l)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	pt := packettype.Resolver.From(t)
	if pt.TypeInt() == int(packettype.NOT_RESOLVED) {
		log.Warnf("[GamePacketDecoder] Unknown Protocol Type: %d", t)
		return nil, ErrUnknownProtocol
	}
	return packet.NewPacket(pt, data), nil
}

// GamePacketEncoder 对应 GamePacketEncoder (封帧)
func GamePacketEncoder(p *packet.Packet) []byte {
	header := make([]byte, HeaderSize)
	binary.BigEndian.PutUint32(header[0:4], uint32(len(p.Bytes)))
	binary.BigEndian.PutUint32(header[4:8], uint32(p.Type.TypeInt()))
	out := make([]byte, 0, len(p.Bytes)+HeaderSize)
	out = append(out, header...)
	out = append(out, p.Bytes...)
	return out
}

// NewServerHandler 对应 NewServerHandler.channelRead0
// 首包时创建 TypeConnect + 触发 NetConnectNewEvent
func NewServerHandler(ch *core.Channel, abstractNet *AbstractNet) func(*packet.Packet) error {
	return func(msg *packet.Packet) error {
		if msg == nil {
			return nil
		}
		tc := ch.GetTypeConnect()
		if tc == nil {
			ca, created := newTypeConnect(ch, abstractNet)
			if created == nil {
				// 连接工厂链不完整, 无法处理任何协议包, 直接断开该连接
				log.Warn("[GamePacketDecoder] TypeConnect 初始化失败, 断开连接")
				_ = ch.Close()
				return nil
			}
			tc = created
			ch.SetTypeConnect(tc)

			if core.NetConnectNewEventHook != nil {
				if core.NetConnectNewEventHook(ca) {
					if anc := tc.AbstractNetConnect(); anc != nil {
						anc.Disconnect()
					}
					return nil
				}
			}
		}
		if anc := tc.AbstractNetConnect(); anc != nil {
			anc.RefreshLastReceivedTime()
		}
		if err := tc.ProcessConnect(msg); err != nil {
			log.Debugf("processConnect err: %v", err)
		}
		return nil
	}
}

// newTypeConnect 安全地构造连接处理器, 任一环节缺失时返回 nil 而非 panic
func newTypeConnect(ch *core.Channel, abstractNet *AbstractNet) (*core.ConnectionAgreement, core.TypeConnect) {
	if abstractNet == nil || abstractNet.rwHps == nil {
		return nil, nil
	}
	factory := abstractNet.rwHps.TypeConnect()
	if factory == nil {
		return nil, nil
	}
	ca := core.NewTCPConnectionAgreement(ch, abstractNet.rwHps)
	tc := factory.GetTypeConnect(ca)
	if tc == nil {
		return ca, nil
	}
	return ca, tc
}