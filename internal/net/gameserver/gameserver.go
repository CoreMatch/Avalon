package gameserver

import (
	"strings"
	"sync/atomic"

	"rwhps/internal/core/thread"
	"rwhps/internal/data/global"
	"rwhps/internal/game"
	"rwhps/internal/game/enums"
	"rwhps/internal/game/event"
	"rwhps/internal/game/player"
	"rwhps/internal/game/room"
	gameio "rwhps/internal/io"
	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
	"rwhps/internal/net/core"
	"rwhps/internal/net/core/server"
	netpacket "rwhps/internal/net/core/server/netpacket"
	"rwhps/internal/net/netconnectprotocol"
	"rwhps/internal/util/control"
	cmd "rwhps/internal/util/game/command"
	"rwhps/internal/util/log"
)

// chatVoter 是连接层对全局投票对象的最小依赖面.
//
// 上游 receiveChat 直接调 Data.vote!!.toVote(player, message); Go 版 Vote 位于
// 命令层 (internal/command/ex), net 层不能反向导入, 经 global.Vote 槽位 +
// 窄接口解耦 (与 room.cleanThread 的 voteStopper 同款手法).
type chatVoter interface {
	ToVote(votePlayer *player.Player, playerPick string)
}

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

// 编译期确认: 原版 GameVersionServer 实现 AbstractNetConnectServer 与 AbstractNetConnectData
var (
	_ server.AbstractNetConnectServer = (*GameVersionServer)(nil)
	_ server.AbstractNetConnectData   = (*GameVersionServer)(nil)
)

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
	room *room.ServerRoom

	// player 对应 player: PlayerHess, 玩家注册后绑定 (里程碑 2)
	player *player.Player

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
		room:               room.Default(),
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

// Room 对应 room (AbstractNetConnectServer 接缝, 返回状态壳)
func (g *GameVersionServer) Room() *game.GameSession {
	if g.room == nil {
		return nil
	}
	return g.room.GameSession
}

// ServerRoom 返回完整房间实体 (玩家/广播/命令链, 里程碑 2 起用)
func (g *GameVersionServer) ServerRoom() *room.ServerRoom { return g.room }

// Player 对应 player: PlayerHess (未注册时为 nil)
func (g *GameVersionServer) Player() *player.Player { return g.player }

// SetPlayer 绑定玩家实体 (玩家注册时由管理层调用)
func (g *GameVersionServer) SetPlayer(p *player.Player) { g.player = p }

// SendJumpToAnotherServer 实现 player.PlayerConnect: 发送跳转包
//
// 对应上游 playerJumpsToAnotherServer 里的发包动作; 包构造经 netconnectprotocol
// 桥接到 internal/relay.
func (g *GameVersionServer) SendJumpToAnotherServer(ipport string) {
	g.SendPacket(netconnectprotocol.FromRelayJumpsToAnotherServerPacket(ipport))
}

// PermissionStatus 对应 permissionStatus
func (g *GameVersionServer) PermissionStatus() core.ServerStatus { return g.permissionStatus }

// Name 实现 AbstractNetConnectData, 对应 name: player.name
//
// 上游声明为 lateinit —— 玩家未注册时访问会抛异常; Go 侧返回空串,
// 与 RegisterPlayerId 的"无即空串"约定一致.
func (g *GameVersionServer) Name() string {
	if g.player == nil {
		return ""
	}
	return g.player.Name()
}

// RegisterPlayerId 实现 AbstractNetConnectData, 对应 registerPlayerId: player.connectHexID
func (g *GameVersionServer) RegisterPlayerId() string {
	if g.player == nil {
		return ""
	}
	return g.player.ConnectHexID()
}

// BetaGameVersion 实现 AbstractNetConnectData, 对应 betaGameVersion = supportedversionBeta
func (g *GameVersionServer) BetaGameVersion() bool { return g.SupportedVersionBeta() }

// ClientVersion 实现 AbstractNetConnectData, 对应 clientVersion = supportedVersionInt
func (g *GameVersionServer) ClientVersion() int { return g.SupportedVersionInt() }

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
// 上游顺序: isDis 守卫 -> 玩家清理 (移出房间列表 / PlayerLeaveEvent /
// playerExitInternalPacket 进引擎 / player.clear()) -> super.close(null).
//
// Go 侧差异只有一处: playerExitInternalPacket 上游进引擎包队列, Go 版经
// conn.packet 发给 helper; 连接本身也要 conn.close 显式注销 (引擎在外部进程,
// 不会自己感知到).
func (g *GameVersionServer) Disconnect() {
	if g.IsDis {
		return
	}
	g.IsDis = true

	connections.remove(g.connID)

	if g.player != nil && g.room != nil {
		g.room.PlayerManage.RemoveFromGroup(g.player)
		if !g.room.IsStartGame {
			g.room.PlayerManage.RemoveFromAll(g.player)
		}
		// 上游在移出列表后触发 PlayerLeaveEvent (oneAdmin 移交等默认监听依赖该时序)
		g.room.EventManage.Fire(&event.PlayerLeaveEvent{Player: g.player})

		if h := currentHelper(); h != nil {
			pkt := netconnectprotocol.PlayerExitPacket("exited")
			if err := h.ConnPacket(g.room.RoomID, g.connID, pkt); err != nil {
				log.Debugf("[GameServer]", "player exit packet: {0}", err)
			}
		}
		g.player.Clear()
	}

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
// 与上游一致的包状态语义: 只有"被拦截"的路径置 STOPPED; 普通聊天通过全部检查
// 后保持 CONTINUE, 由 ProcessConnect 继续转给引擎 (上游同样如此).
func (g *GameVersionServer) ReceiveChat(p *packet.Packet) {
	stream := gameio.NewGameInputStreamFromPacket(p, 0)
	message := stream.ReadString()

	pl := g.player
	if pl == nil || g.room == nil {
		// 上游 player 是 lateinit, 未注册即抛异常; Go 侧无玩家时无法走聊天链,
		// 直接拦下 (等价于异常被 processConnect 的 recover 吞掉后连接继续).
		p.Status = control.STOPPED
		return
	}

	// Afk Stop: 管理员发言即取消 AFK 权力移交倒计时
	if pl.IsAdmin && thread.ContainsTimeTask(thread.PlayerAfkTask) {
		thread.CloseTimeTask(thread.PlayerAfkTask, nil)
	}

	if v, ok := global.Vote.(chatVoter); ok && v != nil {
		v.ToVote(pl, message)
	}

	// Msg Command: 以 . - _ 开头的消息转给客户端命令系统
	var response *cmd.CommandResponse
	if strings.HasPrefix(message, ".") || strings.HasPrefix(message, "-") || strings.HasPrefix(message, "_") {
		strEnd := len(message)
		if strEnd > 3 {
			strEnd = 3
		}
		if message[1:strEnd] == "qc" {
			// 上游 message.substring(5): 跳过 qc 前缀区
			response = g.room.ClientHandler.HandleMessageWith("/"+safeSubstr(message, 5), pl)
		} else {
			response = g.room.ClientHandler.HandleMessageWith("/"+message[1:], pl)
		}
	}

	if response == nil || response.Type == cmd.NoCommand {
		if len(message) > global.ConfigServer.MaxMessageLen {
			_ = g.SendSystemMessage(i18n("message.maxLen"))
			p.Status = control.STOPPED
			return
		}
		if g.room.Snapshot().MuteAll {
			p.Status = control.STOPPED
			return
		}
		messageOut, ok := filterChatMessage(pl, message)
		if !ok {
			p.Status = control.STOPPED
			return
		}
		g.room.EventManage.Fire(&event.PlayerChatEvent{Player: pl, Message: messageOut})
	} else if response.Type != cmd.Valid {
		switch response.Type {
		case cmd.ManyArguments:
			_ = pl.SendSystemMessage("Too many arguments. Usage: " + response.Command.Text + " " + response.Command.ParamText)
			p.Status = control.STOPPED
		case cmd.FewArguments:
			_ = pl.SendSystemMessage("Too few arguments. Usage: " + response.Command.Text + " " + response.Command.ParamText)
			p.Status = control.STOPPED
		default:
			// Ignore (上游对 unknownCommand 等不做处理)
		}
	} else {
		p.Status = control.STOPPED
	}

	log.Clog("[&by {0} &fr]: &y{1}", pl.Name(), log.FormatColors(message, true))
}

// filterChatMessage 对应 Data.core.admin.filterMessage; 无 Administration 时原样放行
func filterChatMessage(pl *player.Player, message string) (string, bool) {
	if global.Core == nil || global.Core.Admin == nil {
		return message, true
	}
	return global.Core.Admin.FilterMessage(pl, message)
}

// safeSubstr 对应 Kotlin substring(fromIndex): 越界时返回空串而不 panic
func safeSubstr(s string, from int) string {
	if from >= len(s) {
		return ""
	}
	return s[from:]
}

// ReceiveCommand 对应 receiveCommand
//
// 上游: 解析 GameCommandOnePacket, 按配置/事件决定是否拦截, 其余放行给引擎.
// 与上游一致: 解析失败只记日志 (try/catch), 包保持 CONTINUE 转给引擎.
func (g *GameVersionServer) ReceiveCommand(p *packet.Packet) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("[GameServer]", "receiveCommand panic: {0}", r)
		}
	}()

	if g.player == nil || g.room == nil {
		return
	}

	command := gameio.NewGameCommandOnePacket(gameio.NewGameInputStreamFromPacket(p, 0).GetDecodeBytes())

	if ou := command.OperationUnit; ou != nil {
		if global.ConfigServer.TurnStoneIntoGold && ou.GameCommandActions == enums.ActionBuild {
			// 上游: gameSummon(unitName, x, y) 把建造指令塞进引擎队列.
			// Go 版引擎在 helper 内, 召唤指令无协议通道 (DECISIONS D-9), 不拦截.
		} else {
			ev := &event.PlayerOperationUnitEvent{
				Player:       g.player,
				Actions:      ou.GameCommandActions,
				UnitName:     ou.UnitName,
				X:            ou.X,
				Y:            ou.Y,
				ResultStatus: true,
			}
			g.room.EventManage.Fire(ev)
			if !ev.ResultStatus {
				p.Status = control.STOPPED
				return
			}
		}
	}

	// 地图 Ping (c_6_ 前缀): 触发玩家注册的 Ping 回调 (如 summon/nuke/clone)
	if command.MapPoint != nil && strings.HasPrefix(command.ActionID, "c_6_") {
		action := enums.PingFromName(strings.TrimPrefix(command.ActionID, "c_6_"))
		if lambda, ok := g.player.GetData("Ping"); ok {
			if fn, ok := lambda.(func(*GameVersionServer, enums.GamePingActions, float32, float32)); ok {
				fn(g, action, command.MapPoint[0], command.MapPoint[1])
			}
			g.player.RemoveData("Ping")
		}
	}

	// 工厂建造单位事件: actionId 不以 "_" 开头时触发
	if !strings.HasPrefix(command.ActionID, "_") {
		ev := &event.PlayerOperationFactoryBuildUnitEvent{
			Player:       g.player,
			ActionID:     command.ActionID,
			ResultStatus: true,
		}
		g.room.EventManage.Fire(ev)
		if !ev.ResultStatus {
			p.Status = control.STOPPED
			return
		}
	}
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
