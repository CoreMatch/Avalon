package core

import (
	"net"
	"strings"
	"sync"

	"rwhps/internal/io/packet"
	"rwhps/internal/util/ip"
	"rwhps/internal/util/log"
)

// ChannelKind 标识 ConnectionAgreement 使用的传输类型
type ChannelKind int

const (
	KindTCP ChannelKind = iota
	KindUDP
	KindHeadless
	KindTest
)

// ConnectionAgreement 对应 ConnectionAgreement
// 统一封装 TCP/UDP/Headless/Test 四种连接协议的发送与关闭
// 注: 为遵循 Go 习惯, 把原 Kotlin 的 `val` 映射为方法 getter.
type ConnectionAgreement struct {
	Kind         ChannelKind
	Channel      *Channel
	UDPOUT       UDPWriter
	RwHps        IRwHps
	UseAgreementStr string
	isClosed     func() bool
	closeClean   func()
	ID           string

	IP            string
	IPLong24      string
	IPCountry     string
	IPCountryAll  string
	LocalPort     int

	writeMu sync.Mutex
}

// UDPWriter 对应 RUDP/UDP 写出接口 (DataOutputStream + socket)
type UDPWriter interface {
	WritePacket(*packet.Packet) error
	Close() error
	IsClosedUDP() bool
}

// NewTCPConnectionAgreement 对应 TCP-NIO 构造
func NewTCPConnectionAgreement(ch *Channel, rwHps IRwHps) *ConnectionAgreement {
	remote := ch.Conn.RemoteAddr().String()
	host := convertIp(remote)
	c := &ConnectionAgreement{
		Kind:            KindTCP,
		Channel:         ch,
		RwHps:           rwHps,
		UseAgreementStr: "TCP-NIO",
		isClosed:        ch.IsClosedChannel,
		IP:              host,
		IPLong24:        ip.IpToLong24NoError(host),
		IPCountry:       ip.GetIpCountry(host),
		IPCountryAll:    ip.GetIpCountryAll(host),
		LocalPort:       ch.LocalPort,
	}
	c.closeClean = func() { ch.SetTypeConnect(nil) }
	return c
}

// NewUDPConnectionAgreement 对应 UDP-BIO 构造
func NewUDPConnectionAgreement(out UDPWriter, rwHps IRwHps, remote string, localPort int) *ConnectionAgreement {
	host := convertIp(remote)
	c := &ConnectionAgreement{
		Kind:            KindUDP,
		UDPOUT:          out,
		RwHps:           rwHps,
		UseAgreementStr: "UDP-BIO",
		isClosed:        out.IsClosedUDP,
		IP:              host,
		IPLong24:        ip.IpToLong24NoError(host),
		IPCountry:       ip.GetIpCountry(host),
		IPCountryAll:    ip.GetIpCountryAll(host),
		LocalPort:       localPort,
	}
	c.closeClean = func() {}
	return c
}

// NewHeadlessConnectionAgreement 对应 Headless-AI 构造
func NewHeadlessConnectionAgreement(rwHps IRwHps) *ConnectionAgreement {
	host := "127.0.0.1"
	return &ConnectionAgreement{
		Kind:            KindHeadless,
		RwHps:           rwHps,
		UseAgreementStr: "Headless-AI",
		isClosed:        func() bool { return false },
		IP:              host,
		IPLong24:        ip.IpToLong24NoError(host),
		IPCountry:       ip.GetIpCountry(host),
		IPCountryAll:    ip.GetIpCountryAll(host),
	}
}

// NewTestConnectionAgreement 对应 Test 构造
func NewTestConnectionAgreement() *ConnectionAgreement {
	return &ConnectionAgreement{
		Kind:            KindTest,
		RwHps:           NewNullRwHps(NullProtocol),
		UseAgreementStr: "Test",
		isClosed:        func() bool { return false },
		closeClean:      func() {},
	}
}

// IsClosed 对应 isClosed
func (c *ConnectionAgreement) IsClosed() bool { return c.isClosed() }

// Add 对应 add(groupNet)
func (c *ConnectionAgreement) Add(groupNet IGroupNet) {
	switch c.Kind {
	case KindTCP:
		if c.Channel != nil {
			groupNet.AddChannel(c.Channel)
		}
	case KindUDP:
		groupNet.AddAgreement(c)
	}
}

// Remove 对应 remove(groupNet)
func (c *ConnectionAgreement) Remove(groupNet IGroupNet) {
	if groupNet == nil {
		return
	}
	switch c.Kind {
	case KindTCP:
		if c.Channel != nil {
			groupNet.RemoveChannel(c.Channel)
		}
	case KindUDP:
		groupNet.RemoveAgreement(c)
	}
}

// Send 对应 send(packet) 接管 Send 逻辑, 整合不同协议的发送逻辑
func (c *ConnectionAgreement) Send(p *packet.Packet) error {
	switch c.Kind {
	case KindTCP:
		c.writeMu.Lock()
		defer c.writeMu.Unlock()
		return c.Channel.WritePacket(p)
	case KindUDP:
		if c.UDPOUT == nil {
			return nil
		}
		return c.UDPOUT.WritePacket(p)
	default:
		return nil
	}
}

// Close 对应 close(groupNet) 关闭连接
func (c *ConnectionAgreement) Close(groupNet IGroupNet) error {
	if NetConnectCloseEventHook != nil {
		NetConnectCloseEventHook(c)
	}

	c.Remove(groupNet)

	switch c.Kind {
	case KindTCP:
		if c.Channel != nil {
			_ = c.Channel.Close()
		}
	case KindUDP:
		if c.UDPOUT != nil {
			if err := c.UDPOUT.Close(); err != nil {
				log.Errorf("[Reliable UDP Close] Passive: %v", err)
			}
		}
	}

	if c.closeClean != nil {
		c.closeClean()
	}
	return nil
}

// convertIp 对应 convertIp(ipString)
// 原版 java: ipString.substring(1, ipString.indexOf(':'))
// Go 的 RemoteAddr.String() 已无前缀 "/", 直接按 ":" 截断即可
func convertIp(ipString string) string {
	if idx := strings.Index(ipString, ":"); idx >= 0 {
		return ipString[:idx]
	}
	return ipString
}

// UDPOutputStream 默认实现: 直接写入 net.Conn (用于 RUDP 透传)
type UDPOutputStream struct {
	mu   sync.Mutex
	conn net.Conn
}

// NewUDPOutputStream 创建 UDP 写出器
func NewUDPOutputStream(conn net.Conn) *UDPOutputStream {
	return &UDPOutputStream{conn: conn}
}

// WritePacket 实现 UDPWriter
func (u *UDPOutputStream) WritePacket(p *packet.Packet) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	data := p.Bytes
	header := make([]byte, 8)
	header[0] = byte(len(data) >> 24)
	header[1] = byte(len(data) >> 16)
	header[2] = byte(len(data) >> 8)
	header[3] = byte(len(data))
	typeBytes := p.Type.TypeIntBytes()
	copy(header[4:], typeBytes)
	if _, err := u.conn.Write(header); err != nil {
		return err
	}
	_, err := u.conn.Write(data)
	return err
}

// Close 实现 UDPWriter
func (u *UDPOutputStream) Close() error { return u.conn.Close() }

// IsClosedUDP 实现 UDPWriter
func (u *UDPOutputStream) IsClosedUDP() bool {
	if u.conn == nil {
		return true
	}
	return false
}