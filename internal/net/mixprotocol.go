// Package net (Go-Server) 对应 net.rwhps.server.net.handler.tcp.StartMixProtocol
//
// 嗅探 TCP 首包, 将连接分派到 Game / RCON / HTTP 三个启动器之一.
//
// 判定顺序与原版一致 (StartMixProtocol.initChannel):
//  1. Game  首 8 字节大端: [length][type], length>0 && type>0 且 type>1000 或 == PREREGISTER_INFO_RECEIVE(160)
//  2. RCON  首 12 字节小端: [length][id][type], length 4..4096 && type 0..3 且 !=1 (需 rconMixEnable)
//  3. HTTP  webMixEnable 打开时无条件命中 (原版此处直接 initChannel 后 return true)
//  4. 其余   原版移除嗅探器后 super.channelRead, 即落到父类 StartHttp, 同样走 HTTP
//
// 因此 Game/RCON 都未命中时, 结果一定是 HTTP —— 这与原版"我该往哪去呢"的兜底一致.
package net

import (
	"encoding/binary"

	"rwhps/internal/data/global"
	"rwhps/internal/game/manage"
	"rwhps/internal/io/packettype"
	"rwhps/internal/net/core"
	nethttp "rwhps/internal/net/http"
)

// MixKind 嗅探结果
type MixKind int

const (
	// MixUnknown 无法识别 (最终仍按 HTTP 处理, 见包注释第 4 条)
	MixUnknown MixKind = iota
	// MixGame Game 协议
	MixGame
	// MixRcon RCON 协议
	MixRcon
	// MixHttp HTTP 协议
	MixHttp
)

// String 返回可读名称
func (k MixKind) String() string {
	switch k {
	case MixGame:
		return "Game"
	case MixRcon:
		return "RCON"
	case MixHttp:
		return "HTTP"
	default:
		return "Unknown"
	}
}

// mixHeadLen 一次嗅探最多需要的字节数
// Game 要 8 字节, RCON 要 12 字节 (length+id+type 三个 int32LE)
const mixHeadLen = 12

// StartMixProtocol 对应 StartMixProtocol(mixData)
//
// 原版继承 StartHttp 并额外持有 StartGameNetTcp / StartRemoteControl 两个子启动器;
// Go 版没有继承, 用组合持有三个子启动器, 由 HandleChannel 按嗅探结果转发.
type StartMixProtocol struct {
	game   *AbstractNet
	remote *StartRemoteControl
	http   *nethttp.StartHttp
}

// NewStartMixProtocol 对应 StartMixProtocol(mixData)
//
// mixData 在 Go 版由 GlobalCache + manage.AddIRwHps 现场构造:
//
//	GameProtocol          -> 调用方传入的 rwHps
//	RemoteControlProtocol -> Cache["MixRemoteControl"] -> addIRwHps(..., "MixRemoteControlDef")
//	HttpProtocol          -> Cache["MixHttp"]          -> addIRwHps(..., "MixHttpDef")
func NewStartMixProtocol(rwHps core.IRwHps) *StartMixProtocol {
	remoteProto := GlobalCache.GetOrCreate("MixRemoteControl", func() core.IRwHps {
		return manage.AddIRwHps(core.RemoteControlProtocol, "MixRemoteControlDef")
	})
	httpProto := GlobalCache.GetOrCreate("MixHttp", func() core.IRwHps {
		return manage.AddIRwHps(core.HttpProtocol, "MixHttpDef")
	})

	return &StartMixProtocol{
		game:   NewAbstractNet(rwHps),
		remote: NewStartRemoteControl(remoteProto),
		http:   nethttp.NewStartHttp(httpProto),
	}
}

// SetWebData 对应 StartHttp.setWebData (StartMixProtocol 继承自 StartHttp)
func (m *StartMixProtocol) SetWebData(d *nethttp.WebData) { m.http.SetWebData(d) }

// pick 按嗅探结果选出目标启动器
// MixUnknown 返回 http: 原版三个分支都不命中时会移除嗅探器并 super.channelRead,
// 而父类正是 StartHttp.
func (m *StartMixProtocol) pick(kind MixKind) ChannelStarter {
	switch kind {
	case MixGame:
		return m.game
	case MixRcon:
		return m.remote
	default:
		return m.http
	}
}

// HandleChannel 读一次首包, 嗅探协议后把连接连同已读字节转交给目标启动器
//
// 只 Read 一次而非 io.ReadFull: 原版是在第一次 channelRead 回调里就做判定,
// 拿到多少字节就判多少, 不会等满 12 字节. 慢速客户端因此可能被判为 HTTP,
// 这是原版既有行为, 保持一致.
func (m *StartMixProtocol) HandleChannel(ch *core.Channel) {
	buf := make([]byte, mixHeadLen)

	// Read 返回 0 且无错误属于"虚假唤醒", 重试即可; 真出错就回收连接
	var n int
	for n == 0 {
		var err error
		n, err = ch.Conn.Read(buf)
		if err != nil {
			_ = ch.Close()
			return
		}
	}

	head := buf[:n]
	kind := Sniff(head)

	// 把已读字节回放到连接头部, 让目标处理器从头解析
	ch.Conn = newPeekedConn(ch.Conn, head)
	m.pick(kind).HandleChannel(ch)
}

// GetConnectSize 返回三个子启动器的活跃连接总数
//
// 与原版差异: 原版 StartMixProtocol 继承自 StartHttp, 而 StartHttp.initChannel 不挂
// IdleStateHandler, 所以 getConnectSize() 恒为 0 (AcceptorIdleStateTrigger 从未被加入
// pipeline). Go 版三个子启动器各自登记连接, 这里求和, 给 stop/version 提供真实数字.
func (m *StartMixProtocol) GetConnectSize() int {
	return m.game.GetConnectSize() + m.remote.GetConnectSize() + m.http.GetConnectSize()
}

// Close 断开三个子启动器上的全部连接
func (m *StartMixProtocol) Close() {
	m.game.Close()
	m.remote.Close()
	m.http.Close()
}

// Sniff 嗅探首包, 决定是 Game / RCON / HTTP / Unknown
func Sniff(head []byte) MixKind {
	// 1. Game: [length BE][type BE]
	//
	// 关键: 原版是 IRwHps.packetType.from(type).typeInt > 1000, 而不是裸的 type > 1000.
	// from() 对未知协议号返回 NOT_RESOLVED(-1), 所以只有"已知且 >1000"的协议号才算 Game.
	// 若用裸值比较, RCON 包 (length LE=10 -> BE 0x0A000000, id LE=1 -> BE 0x01000000)
	// 会因 id 被当作 16777216 而误判成 Game, RCON 分支永远走不到.
	if len(head) >= 8 {
		length := int32(binary.BigEndian.Uint32(head[0:4]))
		typ := int32(binary.BigEndian.Uint32(head[4:8]))
		if length > 0 && typ > 0 {
			resolved := packettype.Resolver.From(int(typ))
			if resolved.TypeInt() > 1000 || resolved.TypeInt() == int(packettype.PREREGISTER_INFO_RECEIVE) {
				return MixGame
			}
		}
	}

	// 2. RCON: [length LE][id LE][type LE] -> length 4..4096 && type 0..3 且 !=1
	if global.Config.RconMixEnable && len(head) >= 12 {
		length := int32(binary.LittleEndian.Uint32(head[0:4]))
		typ := int32(binary.LittleEndian.Uint32(head[8:12]))
		if length >= 4 && length <= 4096 && typ >= 0 && typ <= 3 && typ != 1 {
			return MixRcon
		}
	}

	// 3. HTTP: 原版在 webMixEnable 打开时对该分支无条件 return true,
	//    不做请求行校验 (isHttpReq 只用来决定 ssl 参数), 这里保持一致.
	if global.Config.WebMixEnable {
		return MixHttp
	}

	return MixUnknown
}

// 编译期确认三个启动器都满足 ChannelStarter
var (
	_ ChannelStarter = (*StartMixProtocol)(nil)
	_ ChannelStarter = (*AbstractNet)(nil)
	_ ChannelStarter = (*StartRemoteControl)(nil)
	_ ChannelStarter = (*nethttp.StartHttp)(nil)
)
