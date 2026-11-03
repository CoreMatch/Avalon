package gameserver

import (
	"sync/atomic"

	"rwhps/internal/data/global"
	"rwhps/internal/game"
	gameio "rwhps/internal/io"
	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
	"rwhps/internal/net/core"
	"rwhps/internal/net/core/server"
	netpacket "rwhps/internal/net/core/server/netpacket"
	"rwhps/internal/net/netconnectprotocol"
	"rwhps/internal/util/control"
	"rwhps/internal/util/log"
)

// 上游常量, 来源 GameVersionServer.kt:72-90:
//
//	supportedversionBeta = false
//	supportedversionGame = "1.15"
//	supportedVersionInt  = 176
//	version              = "1.15 RW-HPS"
const (
	supportedVersionBeta = false
	supportedVersionGame = "1.15"
	supportedVersionInt  = 176
	serverVersionString  = "1.15 RW-HPS"
)

// connSeq 连接 ID 序号: helper 协议里 conn 只需房间内唯一, 用单调序号即可
var connSeq atomic.Int64

// 编译期确认: 原版 GameVersionServer 实现 AbstractNetConnectServer
// (AbstractNetConnectData 的 name / registerPlayerId 依赖 PlayerHess, 里程碑 2 补齐)
var _ server.AbstractNetConnectServer = (*GameVersionServer)(nil)

// GameVersionServer 对应 GameVersionServer: 通用服务端协议实现体
//
// 上游继承 AbstractNetConnect 并实现 AbstractNetConnectServer / AbstractNetConnectData;
// Go 侧以嵌入 core.AbstractNetConnect + 方法集表达同一组合.
//
// 与上游的根本差异只在"游戏内核在哪": 上游的 playerConnectX 把包送进同 JVM 的
// 引擎, Go 版经 HelperBackend 送进外部进程. 协议行为 (分发顺序 / 包布局 /
// 状态迁移) 一律照搬.
type GameVersionServer struct {
	*core.AbstractNetConnect

	// room 对应 room: ServerRoom, 构造时取单例房间
	// (上游: playerConnectX.room = HeadlessModuleManage.hessLoaderMap[...].room)
	room *game.GameSession

	// connID helper 协议里的连接 ID, 构造时分配
	connID string

	// permissionStatus 对应 permissionStatus, 初始 InitialConnection
	permissionStatus core.ServerStatus

	// relaySelect 对应 relaySelect: Relay 密码询问的回调 (Relay flavour)
	relaySelect func(string)
}

// NewGameVersionServer 对应 GameVersionServer(playerConnectX) + getTypeConnect 里的登记动作
//
// 上游在 getTypeConnect 里 netEngine.aM.add(playerConnect): 连接一建立引擎就知道它.
// Go 版等价动作是向 helper 发 conn.open; 失败只记日志不断连接 —— 没有引擎时
// 握手自然不会有回应, 超时检测 (180s) 会收走这条连接, 与上游"引擎未就绪"的
// 表现一致.
func NewGameVersionServer(ca *core.ConnectionAgreement) *GameVersionServer {
	g := &GameVersionServer{
		AbstractNetConnect: core.NewAbstractNetConnect(ca),
		room:               game.DefaultRoom(),
		connID:             nextConnID(),
		permissionStatus:   core.ServerInitialConnection,
	}
	connections.add(g)

	if h := currentHelper(); h != nil && g.room != nil {
		if err := h.ConnOpen(g.room.RoomID, g.connID); err != nil {
			log.Errorf("[GameServer]", "conn.open failed: {0}", err)
		}
	}
	return g
}

func nextConnID() string {
	n := connSeq.Add(1)
	// 与上游 connectHexID 的用途相同: 只是一个不重复的连接标识
	const digits = "0123456789abcdef"
	buf := make([]byte, 0, 16)
	for v := n; v > 0; v >>= 4 {
		buf = append([]byte{digits[v&0xF]}, buf...)
	}
	if len(buf) == 0 {
		buf = []byte{'0'}
	}
	return string(buf)
}

// ConnID 返回 helper 协议里的连接 ID (测试与路由用)
func (g *GameVersionServer) ConnID() string { return g.connID }

// Room 对应 room
func (g *GameVersionServer) Room() *game.GameSession { return g.room }

// PermissionStatus 对应 permissionStatus
func (g *GameVersionServer) PermissionStatus() core.ServerStatus { return g.permissionStatus }

// SupportedVersionBeta 对应 supportedversionBeta
func (g *GameVersionServer) SupportedVersionBeta() bool { return supportedVersionBeta }

// SupportedVersionGame 对应 supportedversionGame
func (g *GameVersionServer) SupportedVersionGame() string { return supportedVersionGame }

// SupportedVersionInt 对应 supportedVersionInt
func (g *GameVersionServer) SupportedVersionInt() int { return supportedVersionInt }

// Version 对应 version: "1.15 RW-HPS"
func (g *GameVersionServer) Version() string { return serverVersionString }

// netPacket 取包构造器, 对应 rwHps.abstractNetPacket
func (g *GameVersionServer) netPacket() netpacket.AbstractNetPacket {
	rw := g.RwHps()
	if rw == nil {
		return nil
	}
	return rw.AbstractNetPacket()
}

// SendSystemMessage 对应 sendSystemMessage(msg)
//
// 上游 catch IOException 只打日志; Go 侧按接缝约定把错误交给调用方
// (见 AbstractNetConnectServer 注释).
func (g *GameVersionServer) SendSystemMessage(msg string) error {
	np := g.netPacket()
	if np == nil {
		return core.ErrNotImplemented
	}
	p, err := np.GetSystemMessagePacket(msg)
	if err != nil {
		return err
	}
	g.SendPacket(p)
	return nil
}

// SendChatMessage 对应 sendChatMessage(msg, sendBy, team)
func (g *GameVersionServer) SendChatMessage(msg, sendBy string, team int) error {
	np := g.netPacket()
	if np == nil {
		return core.ErrNotImplemented
	}
	p, err := np.GetChatMessagePacket(msg, sendBy, team)
	if err != nil {
		return err
	}
	g.SendPacket(p)
	return nil
}

// SendKick 对应 sendKick(reason): KICK(reason) + DISCONNECT("kicked") 两连发
//
// 少发第二个包客户端不会走完断开流程, 因此两包都发; 与上游字节布局一致.
func (g *GameVersionServer) SendKick(reason string) error {
	o := gameio.NewGameOutputStream()
	o.WriteString(reason)
	g.SendPacket(o.CreatePacket(packettype.KICK))

	dis := gameio.NewGameOutputStream()
	dis.WriteString("kicked")
	g.SendPacket(dis.CreatePacket(packettype.DISCONNECT))
	return nil
}

// Disconnect 对应 disconnect()
//
// 上游顺序: isDis 守卫 -> 从房间移除玩家 + PlayerLeaveEvent + playerExitInternalPacket
// + player.clear() -> super.close(null). 玩家相关步骤依赖 PlayerHess (里程碑 2),
// 此处先完成连接层自身闭环: 注销路由 + 通知 helper 关连接 + 关闭传输层.
func (g *GameVersionServer) Disconnect() {
	if g.IsDis {
		return
	}
	g.IsDis = true

	connections.remove(g.connID)

	if h := currentHelper(); h != nil && g.room != nil {
		if err := h.ConnClose(g.room.RoomID, g.connID); err != nil {
			log.Debugf("[GameServer]", "conn.close: {0}", err)
		}
	}

	g.Close(nil)
}

// ReceivePacket 对应 receivePacket: 把包交给游戏引擎
//
// 上游: playerConnectX.netEnginePackaging.addPacket(packet); Go 版经 conn.packet
// 转发给外部内核. 无内核时静默丢弃 —— 该路径只在端口已开放时可达, 而端口
// 只有在 start 成功后才开放, 正常情况下内核必然存在.
func (g *GameVersionServer) ReceivePacket(p *packet.Packet) {
	h := currentHelper()
	if h == nil || g.room == nil || p == nil {
		return
	}
	if err := h.ConnPacket(g.room.RoomID, g.connID, p); err != nil {
		log.Errorf("[GameServer]", "forward packet {0} to helper: {1}", p.Type.TypeInt(), err)
	}
}

// ReceiveChat 对应 receiveChat
//
// 上游完整流程: 读消息 -> AFK 任务清理 -> 投票 -> 命令分发 (. - _ 前缀) ->
// 非命令时做 maxLen / muteAll / filterMessage 检查 -> PlayerChatEvent -> 控制台回显.
//
// 里程碑 1 只落地不依赖玩家的两道检查 (maxLen / muteAll); 命令分发 / 过滤 /
// 事件 / 回显依赖 PlayerHess 与客户端命令体系 (里程碑 2/3), 落地前聊天不向下
// 转发 —— 上游的引擎本来就收不到聊天包, 拦住才是等价行为.
func (g *GameVersionServer) ReceiveChat(p *packet.Packet) {
	stream := gameio.NewGameInputStreamFromPacket(p, 0)
	message := stream.ReadString()

	// TODO(里程碑3): AFK Stop / Data.vote / 命令分发 (依赖 player 与 clientHandler)

	if len(message) > global.ConfigServer.MaxMessageLen {
		_ = g.SendSystemMessage(i18n("message.maxLen"))
		p.Status = control.STOPPED
		return
	}
	if g.room != nil && g.room.Snapshot().MuteAll {
		p.Status = control.STOPPED
		return
	}

	// TODO(里程碑2/3): filterMessage + PlayerChatEvent + Log.clog 回显 (依赖 player)
	p.Status = control.STOPPED
}

// ReceiveCommand 对应 receiveCommand
//
// 上游: 解析 GameCommandOnePacket, 按配置/事件决定是否拦截, 其余放行给引擎.
// 事件体系落地前没有拦截依据, 包保持 CONTINUE 由 ReceivePacket 转给引擎 ——
// 与上游"事件全部放行"时的行为一致.
func (g *GameVersionServer) ReceiveCommand(*packet.Packet) {
	// TODO(里程碑3): GameCommandOnePacket 解析 + PlayerOperationUnitEvent /
	// PlayerOperationFactoryBuildUnitEvent 拦截 + turnStoneIntoGold / Ping 处理
}

// SendRelayServerType 对应 sendRelayServerType(msg, run) (Relay flavour)
//
// 上游: 发送 117 包 -> 记录回调 -> inputPassword = true (超时检测放宽到 300s).
func (g *GameVersionServer) SendRelayServerType(msg string, run func(string)) {
	g.SendPacket(netconnectprotocol.RelayServerTypePacket(msg))
	g.relaySelect = run
	g.ConnectReceiveData.InputPassword = true
}

// SendRelayServerTypeReply 对应 sendRelayServerTypeReply(packet) (Relay flavour)
//
// 上游: inputPassword = false -> 解析 118 包里的 ID -> 调用并清空回调.
func (g *GameVersionServer) SendRelayServerTypeReply(p *packet.Packet) {
	g.ConnectReceiveData.InputPassword = false

	id := netconnectprotocol.RelayServerTypeReply(p)
	if g.relaySelect != nil {
		g.relaySelect(id)
		g.relaySelect = nil
	}
}

// i18n 对应 Data.i18NBundle.getinput; 语言包缺失时原样返回 key
func i18n(key string, params ...any) string {
	if global.I18NBundle == nil {
		return key
	}
	return global.I18NBundle.GetInput(key, params...)
}
