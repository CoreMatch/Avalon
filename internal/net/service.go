// Package net (Go-Server) 对应 net.rwhps.server.net.NetService
//
// NetService 是 RW-HPS 暴露给外部的唯一网络服务入口.
// 通过 OpenPort / OpenPortRUDP 在指定端口启动协议监听, 并在 Stop 时优雅关闭.
//
// 与原版差异:
//   - 不用 Netty ServerBootstrap, 直接用 net.Listen TCP/UDP 即可
//   - 不引入 Epoll/NIO 分支, Go runtime 自带 epoll (linux)
//   - 混合协议嗅探由 StartMixProtocol 提供, 通过 Sniff 决定走 Game/RCON/HTTP
package net

import (
	"errors"
	"net"
	"sync"

	"rwhps/internal/net/core"
	"rwhps/internal/util/log"
	"rwhps/internal/util/mathutil"
)

// NetService 对应 NetService
//   OpenPort: 启动 TCP Game 协议监听 (端口/端口段)
//   OpenPortRUDP: 启动 RUDP 协议监听 (Q3=A 精确复刻)
//   Stop: 关闭所有监听并清理
//   GetConnectSize: 当前连接数
type NetService struct {
	ID string

	start AbstractNetStarter

	mu          sync.Mutex
	closed      bool
	closeSeq    []func()
	errorIgnore bool
}

// AbstractNetStarter 对应 NetService.start 的抽象父类
// Go 版用 interface{} 解耦具体协议类型 (StartGameNetTcp / StartMixProtocol / StartHttp / StartRemoteControl)
type AbstractNetStarter interface {
	GetConnectSize() int
	Close()
}

// NetServiceKind 对应 NetTypeEnum
type NetServiceKind int

const (
	// NetServiceHeadless HeadlessNet
	NetServiceHeadless NetServiceKind = iota
	// NetServiceRemoteControl RemoteControl
	NetServiceRemoteControl
	// NetServiceHTTP HTTPNet
	NetServiceHTTP
	// NetServiceMix MixTlsAndGame
	NetServiceMix
	// NetServiceOther Other
	NetServiceOther
)

// 常量 (HeaderSize/MaxPacketSize 见 decoder.go)

// NewNetService 对应 NetService(id, rwHps)
func NewNetService(id string, rwHps core.IRwHps) *NetService {
	if rwHps == nil || rwHps.NetType() == core.NullProtocol {
		panic("No find Protocol")
	}

	ns := &NetService{ID: id}

	switch rwHps.NetType() {
	case core.HttpProtocol, core.RemoteControlProtocol, core.DedicatedToTheBackend:
		// 占位: HTTP / RCON / 专用后端实现见 StartHttp / StartRemoteControl
		ns.start = NewAbstractNet(rwHps)
	default:
		if CheckProtocolIsServer(rwHps.NetType()) {
			ns.start = NewAbstractNet(rwHps)
		} else {
			ns.start = NewAbstractNet(rwHps)
		}
	}

	return ns
}

// OpenPort 对应 openPort(port)
func (s *NetService) OpenPort(port int) {
	s.OpenPortRange(port, 1, 0)
}

// OpenPortRange 对应 openPort(port, startPort, endPort)
//
// TCP SO_BACKLOG=1024, TCP_NODELAY=true, SO_KEEPALIVE=true, 不引入 Epoll (Go runtime 自带 epoll).
// 主端口同步 Accept 阻塞, 端口段 (startPort..endPort) 启动额外监听.
func (s *NetService) OpenPortRange(port, startPort, endPort int) {
	log.Clog("Opening TCP port ...")

	listener, err := net.Listen("tcp", "0.0.0.0:"+itoa(port))
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			log.Errorf("[Port Bind Error] %v", err)
		} else {
			log.Errorf("[NET Error] %v", err)
		}
		return
	}

	s.mu.Lock()
	s.closeSeq = append(s.closeSeq, func() { _ = listener.Close() })
	s.mu.Unlock()

	// 端口段: 从 startPort 到 endPort 各开一个额外 listener
	for i := startPort; i <= endPort; i++ {
		l, e := net.Listen("tcp", "0.0.0.0:"+itoa(i))
		if e != nil {
			log.Warnf("[net] bind %d fail: %v", i, e)
			continue
		}
		s.mu.Lock()
		s.closeSeq = append(s.closeSeq, func() { _ = l.Close() })
		s.mu.Unlock()
		go s.acceptLoop(l)
	}

	log.Clog("Start listening ...")
	s.acceptLoop(listener)
}

// acceptLoop 接受新连接并交给 AbstractNet.HandleChannel
func (s *NetService) acceptLoop(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return
			}
			if !s.errorIgnore {
				log.Errorf("[NET Error] accept: %v", err)
			}
			return
		}

		ch := core.NewChannel(conn)
		if an, ok := s.start.(*AbstractNet); ok {
			an.HandleChannel(ch)
		}
	}
}

// OpenPortRUDP 对应 openPortRUDP(port)
//   RUDP 协议待 #7 实现, 此处先留接口.
func (s *NetService) OpenPortRUDP(port int) {
	log.Clog("Opening RUDP port ...")
	// TODO: 接入 ReliableServerSocket (Go 版待 #7 实现)
}

// GetConnectSize 对应 getConnectSize()
func (s *NetService) GetConnectSize() int {
	return s.start.GetConnectSize()
}

// Stop 对应 stop()
func (s *NetService) Stop() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.errorIgnore = true
	seq := s.closeSeq
	s.closed = true
	s.mu.Unlock()

	for _, fn := range seq {
		fn()
	}
	s.start.Close()
}

// SetWebData 对应 setWebData
func (s *NetService) SetWebData(_ any) *NetService {
	// 占位, 等待 AbstractNetWeb 实现
	return s
}

// Equals 对应 NetService.equals
func (s *NetService) Equals(other *NetService) bool {
	if other == nil {
		return false
	}
	return s.ID == other.ID
}

// HashCode 对应 NetService.hashCode
func (s *NetService) HashCode() int {
	h := 0
	for _, c := range s.ID {
		h = h*31 + int(c)
	}
	return h
}

// CoreID 对应 NetService.Companion.coreID()
func CoreID() string {
	return mathutil.GetRandomString(10)
}

// itoa 自带的 strconv.Itoa 引入过多, 用局部包装
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}