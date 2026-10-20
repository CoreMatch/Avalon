package core

import (
	"encoding/binary"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"rwhps/internal/io/packet"
)

// Channel 对应 Netty 的 Channel.
// 由于 Go 使用 net.Conn 而非 Netty Pipeline, 这里承载 Channel 的
// 属性存储 (TypeConnect) 与串行化写出职责.
type Channel struct {
	Conn net.Conn

	writeMu sync.Mutex

	tcMu        sync.Mutex
	typeConnect TypeConnect

	LocalPort int

	closed   atomic.Bool
	lastRecv atomic.Int64
}

// NewChannel 由 net.Conn 创建 Channel
func NewChannel(conn net.Conn) *Channel {
	c := &Channel{Conn: conn}
	if addr, ok := conn.LocalAddr().(*net.TCPAddr); ok {
		c.LocalPort = addr.Port
	}
	return c
}

// GetTypeConnect 对应 channel.attr(nettyChannelData).get()
func (c *Channel) GetTypeConnect() TypeConnect {
	c.tcMu.Lock()
	defer c.tcMu.Unlock()
	return c.typeConnect
}

// SetTypeConnect 对应 channel.attr(nettyChannelData).set(type)
func (c *Channel) SetTypeConnect(tc TypeConnect) {
	c.tcMu.Lock()
	defer c.tcMu.Unlock()
	c.typeConnect = tc
}

// WriteRaw 串行化写出原始字节 (保持包顺序)
func (c *Channel) WriteRaw(b []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.Conn.Write(b)
	return err
}

// WritePacket 对应 GamePacketEncoder: [4 字节长度][4 字节类型][数据]
func (c *Channel) WritePacket(p *packet.Packet) error {
	frame := make([]byte, 0, len(p.Bytes)+8)
	var head [8]byte
	binary.BigEndian.PutUint32(head[0:4], uint32(len(p.Bytes)))
	binary.BigEndian.PutUint32(head[4:8], uint32(p.Type.TypeInt()))
	frame = append(frame, head[:]...)
	frame = append(frame, p.Bytes...)
	return c.WriteRaw(frame)
}

// Close 关闭底层连接
func (c *Channel) Close() error {
	if c.closed.Swap(true) {
		return nil
	}
	return c.Conn.Close()
}

// IsClosedChannel 报告底层连接是否已关闭
func (c *Channel) IsClosedChannel() bool { return c.closed.Load() }

// LastReceiveMs 返回最近一次收到数据时的时间戳 (ms).
// 对应 AbstractNetConnect.lastReceivedTime, 但放在 Channel 层以便无连接时也保持时间戳.
func (c *Channel) LastReceiveMs() int64 { return c.lastRecv.Load() }

// RefreshLastAck 更新最近一次收到数据的时间戳.
func (c *Channel) RefreshLastAck() { c.lastRecv.Store(time.Now().UnixMilli()) }
