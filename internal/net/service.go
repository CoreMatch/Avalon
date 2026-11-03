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
	"strconv"
	"sync"

	"rwhps/internal/data/global"
	"rwhps/internal/net/core"
	nethttp "rwhps/internal/net/http"
	"rwhps/internal/util/log"
	"rwhps/internal/util/mathutil"
)

// ChannelStarter 对应 NetService.start 的静态类型 AbstractNet
//
// 四个实现与原版一一对应:
//
//	*AbstractNet        -> StartGameNetTcp
//	*StartRemoteControl -> StartRemoteControl
//	*nethttp.StartHttp  -> StartHttp
//	*StartMixProtocol   -> StartMixProtocol (继承 StartHttp)
type ChannelStarter interface {
	// HandleChannel 接管一条新连接 (可能阻塞, 调用方应在独立 goroutine 中调用)
	HandleChannel(ch *core.Channel)
	// GetConnectSize 当前活跃连接数
	GetConnectSize() int
	// Close 断开该启动器上的全部连接
	Close()
}

// webDataSetter 对应 Kotlin 的 `if (start is AbstractNetWeb) start.setWebData(data)`
type webDataSetter interface {
	SetWebData(d *nethttp.WebData)
}

// NetService 对应 NetService
//
//	OpenPort:     启动 TCP 协议监听 (端口/端口段)
//	OpenPortRUDP: 启动 RUDP 协议监听 (待 Relay 批次)
//	Stop:         关闭所有监听并清理
//	GetConnectSize: 当前连接数
type NetService struct {
	// ID 对应 NetService.id
	ID string

	// NetType 对应 NetService.netType (NetTypeEnum)
	NetType NetServiceKind

	start ChannelStarter

	mu          sync.Mutex
	closed      bool
	closeSeq    []func()
	errorIgnore bool
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

// String 对应 NetTypeEnum.name
func (k NetServiceKind) String() string {
	switch k {
	case NetServiceHeadless:
		return "HeadlessNet"
	case NetServiceRemoteControl:
		return "RemoteControl"
	case NetServiceHTTP:
		return "HTTPNet"
	case NetServiceMix:
		return "MixTlsAndGame"
	default:
		return "Other"
	}
}

// NewNetService 对应 NetService(id, rwHps)
//
// 启动器选择顺序完全照搬原版构造函数:
//
//	mixProtocolEnable            -> StartMixProtocol
//	checkProtocolIsServer        -> StartGameNetTcp
//	DedicatedToTheBackend        -> StartGameNetTcp
//	HttpProtocol                 -> StartHttp
//	RemoteControlProtocol        -> StartRemoteControl
//	其余 (含 GlobalProtocol)      -> 原版 TODO(), 这里 panic 以尽早暴露配置错误
func NewNetService(id string, rwHps core.IRwHps) *NetService {
	if rwHps == nil || rwHps.NetType() == core.NullProtocol {
		panic("No find Protocol")
	}

	ns := &NetService{ID: id}

	switch {
	case global.Config.MixProtocolEnable:
		ns.start = NewStartMixProtocol(rwHps)
	case core.CheckProtocolIsServer(rwHps.NetType()):
		ns.start = NewAbstractNet(rwHps)
	default:
		switch rwHps.NetType() {
		case core.DedicatedToTheBackend:
			ns.start = NewAbstractNet(rwHps)
		case core.HttpProtocol:
			ns.start = nethttp.NewStartHttp(rwHps)
		case core.RemoteControlProtocol:
			ns.start = NewStartRemoteControl(rwHps)
		default:
			panic("NetService: unsupported protocol " + rwHps.NetType().String())
		}
	}

	ns.NetType = kindOf(ns.start)

	// 对应上游 NetService 构造函数末尾:
	// `if (abstractNet is AbstractNetWeb) setWebData()` (默认注入 Data.webData)
	if _, ok := ns.start.(webDataSetter); ok {
		ns.SetWebData(nethttp.DefaultWebData)
	}

	// 对应 Kotlin init { NetStaticData.netService.add(this) }
	NetStaticData.AddService(ns)

	return ns
}

// kindOf 对应原版构造函数末尾的 when (abstractNet) { is ... -> NetTypeEnum... }
func kindOf(s ChannelStarter) NetServiceKind {
	switch s.(type) {
	case *StartMixProtocol:
		return NetServiceMix
	case *StartRemoteControl:
		return NetServiceRemoteControl
	case *nethttp.StartHttp:
		return NetServiceHTTP
	case *AbstractNet:
		return NetServiceHeadless
	default:
		return NetServiceOther
	}
}

// SetWebData 对应 setWebData(data = Data.webData)
func (s *NetService) SetWebData(d *nethttp.WebData) *NetService {
	if w, ok := s.start.(webDataSetter); ok {
		w.SetWebData(d)
	}
	return s
}

// OpenPort 对应 openPort(port)
func (s *NetService) OpenPort(port int) {
	s.OpenPortRange(port, 1, 0)
}

// OpenPortRange 对应 openPort(port, startPort, endPort)
//
// 原版在此处先 Data.config.save(); Go 版配置落盘由启动链 (app 包) 负责, 这里不再重复.
// TCP SO_BACKLOG=1024, TCP_NODELAY=true, SO_KEEPALIVE=true 由 net.Listen 默认值覆盖,
// 不引入 Epoll (Go runtime 自带 epoll).
func (s *NetService) OpenPortRange(port, startPort, endPort int) {
	log.Clog("Opening TCP port ...")

	listener, err := net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(port))
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			log.Errorf("[Port Bind Error]", err)
		} else {
			log.Errorf("[NET Error]", err)
		}
		return
	}

	s.mu.Lock()
	if s.closed {
		// Stop 抢在监听器登记之前发生: 自己关掉, 否则会留下一个
		// 永远没人关的 listener (Stop 的 closeSeq 快照里没有它).
		s.mu.Unlock()
		_ = listener.Close()
		return
	}
	s.closeSeq = append(s.closeSeq, func() { _ = listener.Close() })
	s.mu.Unlock()

	// 端口段: 从 startPort 到 endPort 各开一个额外 listener
	for i := startPort; i <= endPort; i++ {
		l, e := net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(i))
		if e != nil {
			log.Warnf("[net]", "bind {0} fail: {1}", i, e)
			continue
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = l.Close()
			return
		}
		s.closeSeq = append(s.closeSeq, func() { _ = l.Close() })
		s.mu.Unlock()
		go s.acceptLoop(l)
	}

	log.Clog("Start listening ...")
	s.acceptLoop(listener)
}

// acceptLoop 接受新连接并交给启动器
//
// HandleChannel 在 StartHttp / StartRemoteControl 里是阻塞的 (占满一个连接的处理循环),
// 因此必须在独立 goroutine 中调用; AbstractNet 内部还会再起协程, 多一层不影响语义.
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
				log.Errorf("[NET Error]", "accept: {0}", err)
			}
			return
		}

		ch := core.NewChannel(conn)
		go s.start.HandleChannel(ch)
	}
}

// OpenPortRUDP 对应 openPortRUDP(port)
//
// RUDP (ReliableServerSocket) 属于 Relay 批次, 此处先留接口.
func (s *NetService) OpenPortRUDP(port int) {
	log.Clog("Opening RUDP port ...")
	// TODO: 接入 ReliableServerSocket (Relay 批次)
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
	s.closeSeq = nil
	s.closed = true
	s.mu.Unlock()

	for _, fn := range seq {
		fn()
	}
	s.start.Close()

	// 对应 NetStaticData.netService.remove(this)
	NetStaticData.RemoveService(s)
}

// Equals 对应 NetService.equals
func (s *NetService) Equals(other *NetService) bool {
	if other == nil {
		return false
	}
	return s.ID == other.ID
}

// HashCode 对应 NetService.hashCode (String.hashCode 的 31 进制算法)
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
