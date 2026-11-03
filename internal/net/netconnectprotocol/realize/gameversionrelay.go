package realize

// GameVersionRelay 对应 net.rwhps.server.net.netconnectprotocol.realize.GameVersionRelay
// (1061 行的 Relay 协议实现体).
//
// 上游继承 AbstractNetConnect 并实现 AbstractNetConnectData / AbstractNetConnectRelay;
// Go 侧以嵌入 core.AbstractNetConnect + 方法集表达同一组合, 并编译期断言同时满足
// room.RelayConnect 接缝 (RelayRoom 对连接的最小依赖面).
//
// # 依赖方向 (与上游的一处结构性差异)
//
// 上游 GameVersionRelay 与 UniversalAnalysisOfGamePackages 同属 netconnectprotocol
// 包族, 直接互调. Go 侧 realize 是独立包, 若 realize 导入父包 netconnectprotocol,
// 而父包的 TypeRelay 又要导入 realize, 即构成循环. 按"消费方定义窄接口/注入"的
// 既有手法 (参照 room.RelayConnect 接缝): realize 不导入父包,
// UniversalAnalysisOfGamePackages.getPacketTeamData 经 packetTeamDataParser 注入
// (组合根 app 在 loadService 时接线). 其余行为逐行照搬.
//
// # Relay 风味不依赖游戏内核
//
// 与 Server 风味不同, 本实现体只做转发与房间管理, 不触碰游戏模拟内核,
// 因此上游函数体可完整照搬; 外置依赖只有两处, 均已有等价物:
//   - EmojiManager.containsEmoji -> internal/util/emoji.ContainsEmoji
//   - InetAddress.getAllByName   -> 上游 findMapURL 私有且零调用, 不移植 (见文末说明)

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"

	"rwhps/internal/data/global"
	"rwhps/internal/game"
	"rwhps/internal/game/player"
	"rwhps/internal/game/room"
	gameio "rwhps/internal/io"
	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
	"rwhps/internal/net/core"
	"rwhps/internal/net/core/server"
	relaypacket "rwhps/internal/net/netconnectprotocol/internal/relay"
	serverpacket "rwhps/internal/net/netconnectprotocol/internal/server"
	"rwhps/internal/util/algorithms"
	"rwhps/internal/util/compression"
	"rwhps/internal/util/emoji"
	cmd "rwhps/internal/util/game/command"
	gameutil "rwhps/internal/util/game"
	"rwhps/internal/util/isutils"
	"rwhps/internal/util/javastr"
	"rwhps/internal/util/log"
	"rwhps/internal/util/timeutil"
)

// 编译期确认: 原版 GameVersionRelay 同时实现两个接口并满足 RelayRoom 的接缝
var (
	_ server.AbstractNetConnectRelay = (*GameVersionRelay)(nil)
	_ server.AbstractNetConnectData  = (*GameVersionRelay)(nil)
	_ room.RelayConnect              = (*GameVersionRelay)(nil)
)

// newRelayProtocolVersion 对应 newRelayProtocolVersion = 172
const newRelayProtocolVersion = 172

// whitespacePattern 对应 idCustom 里的 "\\s".toRegex()
var whitespacePattern = regexp.MustCompile(`\s`)

// packetTeamDataParser 对应 UniversalAnalysisOfGamePackages.getPacketTeamData 的调用
//
// 由组合根注入的原因见文件头"依赖方向". 未注入时跳过队伍数据解析
// (生产路径必然在 loadService 接线).
var packetTeamDataParser func(parse *gameio.GameInputStream, pr *player.PlayerRelay) error

// SetPacketTeamDataParser 注入 getPacketTeamData 实现 (app.loadService 调用)
func SetPacketTeamDataParser(f func(parse *gameio.GameInputStream, pr *player.PlayerRelay) error) {
	packetTeamDataParser = f
}

// GameVersionRelay 对应 GameVersionRelay
//
// 原注释: "Relay protocol implementation. Direct forwarding consumes more bandwidth
// and the same effect as using VPN forwarding"
type GameVersionRelay struct {
	*core.AbstractNetConnect

	// netConnectAuthenticate 对应 netConnectAuthenticate: 客户端计算证明非 Bot (PoW)
	netConnectAuthenticate *algorithms.NetConnectProofOfWork
	// relaySelect 对应 relaySelect: 服务端类型询问的回调
	relaySelect func(string)

	// permissionStatus 对应 permissionStatus, 初始 InitialConnection
	permissionStatus core.RelayStatus
	// room 对应 room: Relay 实例 (房间)
	room *room.RelayRoom
	// site 对应 site: 连接在 RELAY 内的转发位置, 初始 -1
	site int32
	// cachePacket 对应 cachePacket: 缓存包 (relayDirectInspection 的输入)
	cachePacket *packet.Packet
	// name 对应 name, 初始 "NOT NAME"
	name string
	// playerRelay 对应 playerRelay: Relay-EX 玩家统计数据
	playerRelay *player.PlayerRelay
	// registerPlayerId 对应 registerPlayerId: 玩家注册后的 UUID-Hex
	registerPlayerId string
	// betaGameVersion 对应 betaGameVersion
	betaGameVersion bool
	// clientVersion 对应 clientVersion, 初始 151, relayDirectInspection 时被真实版本覆盖
	clientVersion int

	// replacePlayerHex 对应 replacePlayerHex
	replacePlayerHex string
	// startGameFlag 对应 startGameFlag (私有), 初始 true
	startGameFlag bool

	// 上游还声明了 joinTime = Time.concurrentSecond() 与 joinMessage = true 两个
	// 私有字段, 但全文件零读取 (遗留死字段), 不移植.
}

// NewGameVersionRelay 对应 GameVersionRelay(connectionAgreement)
func NewGameVersionRelay(ca *core.ConnectionAgreement) *GameVersionRelay {
	return &GameVersionRelay{
		AbstractNetConnect: core.NewAbstractNetConnect(ca),
		permissionStatus:   core.RelayInitialConnection,
		site:               -1,
		name:               "NOT NAME",
		clientVersion:      151,
		startGameFlag:      true,
	}
}

// ---------------------------------------------------------------------------
// 属性访问器 (满足 AbstractNetConnectRelay / AbstractNetConnectData / RelayConnect)
// ---------------------------------------------------------------------------

// PermissionStatus 对应 permissionStatus
func (g *GameVersionRelay) PermissionStatus() core.RelayStatus { return g.permissionStatus }

// SetPermissionStatus 对应 permissionStatus 的 internal set
//
// 上游只允许协议层 (TypeRelay) 推进状态; Go 没有包内可见性, 由调用约定保证.
// RelayRoom 的 admin setter 回调也经此方法把新房主打成 HostPermission.
func (g *GameVersionRelay) SetPermissionStatus(s core.RelayStatus) { g.permissionStatus = s }

// Room 对应 room
func (g *GameVersionRelay) Room() *room.RelayRoom { return g.room }

// Site 对应 site
func (g *GameVersionRelay) Site() int32 { return g.site }

// Name 对应 name
func (g *GameVersionRelay) Name() string { return g.name }

// PlayerRelay 对应 playerRelay
func (g *GameVersionRelay) PlayerRelay() *player.PlayerRelay { return g.playerRelay }

// StartGameFlag 对应 startGameFlag
func (g *GameVersionRelay) StartGameFlag() bool { return g.startGameFlag }

// SetStartGameFlag 对应 startGameFlag 的 setter
func (g *GameVersionRelay) SetStartGameFlag(v bool) { g.startGameFlag = v }

// RegisterPlayerId 对应 registerPlayerId
func (g *GameVersionRelay) RegisterPlayerId() string { return g.registerPlayerId }

// BetaGameVersion 对应 betaGameVersion
func (g *GameVersionRelay) BetaGameVersion() bool { return g.betaGameVersion }

// ClientVersion 对应 clientVersion
func (g *GameVersionRelay) ClientVersion() int { return g.clientVersion }

// Version 对应 version: "1.15 RELAY"
func (g *GameVersionRelay) Version() string { return "1.15 RELAY" }

// netPacket 取包构造器, 对应 rwHps.abstractNetPacket
func (g *GameVersionRelay) netPacket() netpacketLike {
	rw := g.RwHps()
	if rw == nil {
		return nil
	}
	return rw.AbstractNetPacket()
}

// netpacketLike 是 netpacket.AbstractNetPacket 的子集, 只保留本实现体用到的成员
type netpacketLike interface {
	GetSystemMessagePacket(msg string) (*packet.Packet, error)
	GetChatMessagePacket(msg string, sendBy string, team int) (*packet.Packet, error)
	GetExitPacket() (*packet.Packet, error)
}

// sendSystemMessage 对应 sendPacket(rwHps.abstractNetPacket.getSystemMessagePacket(msg))
func (g *GameVersionRelay) sendSystemMessage(msg string) error {
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

// sendChatMessage 对应 sendPacket(rwHps.abstractNetPacket.getChatMessagePacket(...))
func (g *GameVersionRelay) sendChatMessage(msg, sendBy string, team int) error {
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

// i18n 对应 Data.i18NBundle.getinput; 语言包缺失时原样返回 key
func i18n(key string, params ...any) string {
	if global.I18NBundle == nil {
		return key
	}
	return global.I18NBundle.GetInput(key, params...)
}

// ---------------------------------------------------------------------------
// AbstractNetConnectRelay 成员
// ---------------------------------------------------------------------------

// SetCachePacket 对应 setCachePacket(packet)
func (g *GameVersionRelay) SetCachePacket(p *packet.Packet) { g.cachePacket = p }

// SetLastSentPacket 对应 setlastSentPacket(packet): 此协议下不被使用 (上游空实现)
func (g *GameVersionRelay) SetLastSentPacket(*packet.Packet) {}

// SendRelayServerInfo 对应 sendRelayServerInfo
//
// 从 Cache.packetCache 取 (没有则构造) relayServerInitInfoInternalPacket 再发出.
func (g *GameVersionRelay) SendRelayServerInfo() {
	cPacket := global.PacketCacheInstance.GetOrCreate("sendRelayServerInfo", func() *packet.Packet {
		return relaypacket.RelayServerInitInfoPacket()
	})
	g.SendPacket(cPacket)
}

// RelayDirectInspection 对应 relayDirectInspection(relayRoom = null)
//
// 从 cachePacket 读出协议版本 / 客户端版本 / 查询串 / 玩家名, 然后按 relayRoom
// 是否为 nil 分两条路 (见接口注释). 上游在 cachePacket 为 null 时经 `!!` 抛 NPE,
// Go 侧返回 error.
func (g *GameVersionRelay) RelayDirectInspection(relayRoom *room.RelayRoom) error {
	if g.cachePacket == nil {
		return errors.New("relay: cachePacket is null (Kotlin: cachePacket!!)")
	}

	inStream := gameio.NewGameInputStreamFromPacket(g.cachePacket, 0)
	inStream.ReadString()
	packetVersion := inStream.ReadInt()
	g.clientVersion = int(inStream.ReadInt())
	g.betaGameVersion = gameutil.GetBetaVersion(g.clientVersion)

	if packetVersion >= 1 {
		inStream.Skip(4)
	}
	queryString := ""
	if packetVersion >= 2 {
		queryString = inStream.ReadIsString()
	}
	if packetVersion >= 3 {
		// Player Name
		inStream.ReadString()
	}

	if relayRoom == nil {
		if isutils.IsBlank(queryString) || strings.EqualFold(queryString, "RELAYCN") {
			g.SendRelayServerType(i18n("relay.hi", global.ServerCoreVersion), nil)
		} else {
			g.idCustom(queryString)
		}
	} else {
		g.room = relayRoom
		g.AddRelayConnect()
	}
	return nil
}

// SendVerifyClientValidity 对应 sendVerifyClientValidity
//
// 取一个新的 PoW 挑战 (RelayRoom.randPow) 并按 authenticateType 分支写 RELAY_POW 包.
func (g *GameVersionRelay) SendVerifyClientValidity() {
	g.netConnectAuthenticate = room.RandPow()
	pow := g.netConnectAuthenticate
	authenticateType := pow.AuthenticateType()

	defer func() {
		if e := recover(); e != nil {
			log.Error(e)
		}
	}()

	o := gameio.NewGameOutputStream()
	// 返回相同
	o.WriteInt(pow.ResultInt())
	o.WriteInt(int32(authenticateType))
	if authenticateType == 0 || (authenticateType >= 2 && authenticateType <= 4) || authenticateType == 6 {
		o.WriteBoolean(true)
		o.WriteInt(pow.InitInt1())
	} else {
		o.WriteBoolean(false)
	}
	if authenticateType == 1 || (authenticateType >= 2 && authenticateType <= 4) {
		o.WriteBoolean(true)
		o.WriteInt(pow.InitInt2())
	} else {
		o.WriteBoolean(false)
	}
	if authenticateType >= 5 && authenticateType <= 6 {
		o.WriteString(pow.Outcome())
		o.WriteString(pow.FixedInitial())
		o.WriteInt(pow.MaximumNumberOfCalculations())
	}

	o.WriteBoolean(false)

	g.SendPacket(o.CreatePacket(packettype.RELAY_POW))
}

// ReceiveVerifyClientValidity 对应 receiveVerifyClientValidity(packet)
//
// 校验失败 (或没有挑战 / 包解析异常) 一律返回 false, 与上游一致.
func (g *GameVersionRelay) ReceiveVerifyClientValidity(p *packet.Packet) (ok bool) {
	// 上游 try/catch(Exception) 吞掉一切错误并返回 false; Go 用 recover 等效.
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()

	inStream := gameio.NewGameInputStreamFromPacket(p, 0)
	if g.netConnectAuthenticate != nil {
		if g.netConnectAuthenticate.VerifyPOWResult(inStream.ReadInt(), int(inStream.ReadInt()), inStream.ReadString()) {
			g.netConnectAuthenticate = nil
			return true
		}
	}
	// Ignore, 正常情况下不会到这里, 处理器会处理
	return false
}

// SendRelayServerType 对应 sendRelayServerType(msg, run = null)
func (g *GameVersionRelay) SendRelayServerType(msg string, run func(string)) {
	g.SendPacket(relaypacket.RelayServerTypePacket(msg))
	g.relaySelect = run
	g.ConnectReceiveData.InputPassword = true
}

// SendRelayServerTypeReply 对应 sendRelayServerTypeReply(packet)
//
// 注意: 上游在回调后**不**清空 relaySelect, 照搬.
func (g *GameVersionRelay) SendRelayServerTypeReply(p *packet.Packet) {
	defer func() {
		if e := recover(); e != nil {
			log.Error(e)
		}
	}()

	g.ConnectReceiveData.InputPassword = false

	id := relaypacket.RelayServerTypeReply(p)
	if g.relaySelect == nil {
		g.idCustom(id)
	} else {
		g.relaySelect(id)
	}
}

// ReceiveChat 对应 receiveChat(packet)
func (g *GameVersionRelay) ReceiveChat(p *packet.Packet) error {
	inStream := gameio.NewGameInputStreamFromPacket(p, 0)

	if g.permissionStatus < core.RelayPlayerJoinPermission {
		return nil
	}

	message := inStream.ReadString()

	// 上游 room!!.allmute || playerRelay!!.mute: 两者在此状态下均已就绪
	if g.room.AllMute || g.playerRelay.Mute > timeutil.ConcurrentSecond() {
		return nil
	}

	g.playerRelay.SetLastSentMessage(message)

	if strings.HasPrefix(message, ".") || strings.HasPrefix(message, "-") {
		response := global.RelayCommand.HandleMessageWith(message, g)
		// 上游 `?: return@command`: Go 的 HandleMessageWith 恒返回非 nil,
		// NoCommand 分支与上游 null 分支的可观察行为一致 (忽略).
		if response != nil {
			switch response.Type {
			case cmd.NoCommand, cmd.Valid:
				// Ignore
			case cmd.ManyArguments:
				msg := "Too many arguments. Usage: " + response.Command.Text + " " + response.Command.ParamText
				if err := g.sendSystemMessage(msg); err != nil {
					return err
				}
			case cmd.FewArguments:
				msg := "Too few arguments. Usage: " + response.Command.Text + " " + response.Command.ParamText
				if err := g.sendSystemMessage(msg); err != nil {
					return err
				}
			default:
				g.SendPackageToHOST(p)
			}
		}
	}

	g.SendPackageToHOST(p)
	return nil
}

// ReceiveCommand 对应 receiveCommand(packet): 函数体只有转发给房主
func (g *GameVersionRelay) ReceiveCommand(p *packet.Packet) error {
	g.SendPackageToHOST(p)
	return nil
}

// SendRelayServerId 对应 sendRelayServerId(multicast = false): 建立 RELAY HOST
func (g *GameVersionRelay) SendRelayServerId(multicast bool) {
	defer func() {
		if e := recover(); e != nil {
			log.Error(e)
		}
	}()

	g.ConnectReceiveData.InputPassword = false
	if g.room == nil {
		log.Clog("sendRelayServerId -> relay : null")
		g.room = room.SingleRoom()
	}

	if g.site != -1 {
		g.room.RemoveAbstractNetConnect(g.site)
		// 这个代表是第二任 (后妈)
		g.site = -2
	}

	g.room.SetAdmin(g)

	const public = false
	visibility := "隐藏"
	if public {
		visibility = "开放"
	}

	o := gameio.NewGameOutputStream()
	if g.clientVersion >= newRelayProtocolVersion {
		_ = o.WriteByte(2)
		o.WriteBoolean(true)
		o.WriteBoolean(true)
		o.WriteBoolean(true)
		o.WriteString(g.room.ServerUUID)
		o.WriteBoolean(g.room.IsMod) // MOD
		o.WriteBoolean(public)
		o.WriteBoolean(true)
		o.WriteString("{{RELAY-CN}} Room ID : " + global.ConfigRelay.MainID + g.room.ID + "\n" +
			"你的房间是 <" + visibility + "> 在列表\n" +
			"This Server Use RW-HPS Project (Test)")
		o.WriteBoolean(multicast)
		o.WriteIsString(g.registerPlayerId)
	} else {
		// packetVersion
		_ = o.WriteByte(1)
		// allowThisConnectionForwarding
		o.WriteBoolean(true)
		// removeThisConnection
		o.WriteBoolean(true)
		// useServerId
		o.WriteIsString(g.room.ServerUUID)
		// useMods
		o.WriteBoolean(g.room.IsMod) // MOD
		// showPublicly
		o.WriteBoolean(public)
		// relayMessageOnServer
		o.WriteString("{{RELAY-CN}} Room ID : " + global.ConfigRelay.MainID + g.room.ID + "\n" +
			"你的房间是 <" + visibility + "> 在列表\n" +
			"This Server Use RW-HPS Project")
		// useMulticast
		o.WriteBoolean(multicast)
	}

	g.SendPacket(o.CreatePacket(packettype.RELAY_BECOME_SERVER))
	_ = g.sendChatMessage(
		i18n("relay.server.admin.connect",
			global.ConfigRelay.MainID+g.room.ID,
			global.ConfigRelay.MainID+strconv.FormatInt(int64(g.room.InternalID), 10)),
		"RELAY_CN-ADMIN", 5)
	_ = g.sendChatMessage(i18n("relay", global.ConfigRelay.MainID+g.room.ID), "RELAY_CN-ADMIN", 5)

	// 人即像树，树枝越向往光明的天空，树根越伸向阴暗的地底
	// 禁止玩家使用 Server/Relay 做玩家名
	if strings.EqualFold(g.name, "SERVER") || strings.EqualFold(g.name, "RELAY") {
		g.room.Re() // Close Room
	}
}

// RelayT4 对应 getRelayT4(msg): 转发一条用户聊天消息
func (g *GameVersionRelay) RelayT4(msg string) {
	defer func() {
		if e := recover(); e != nil {
			log.Error(e)
		}
	}()
	g.SendPacket(serverpacket.ChatUserMessagePacket(msg))
}

// PingData 对应 getPingData(packet): 前 8 字节原样搬回 + 一个 0 字节
func (g *GameVersionRelay) PingData(p *packet.Packet) {
	inStream := gameio.NewGameInputStreamFromPacket(p, 0)
	out := gameio.NewGameOutputStream()
	out.TransferToFixedLength(inStream, 8)
	// 将协议版本降低 (0), 来减少发送一个字节
	_ = out.WriteByte(0)
	g.SendPacket(out.CreatePacket(packettype.HEART_BEAT_RESPONSE))
}

// AddGroup 对应 addGroup(packet)
func (g *GameVersionRelay) AddGroup(p *packet.Packet) {
	g.room.GroupNet.BroadcastAndUDP(p)
}

// AddGroupPing 对应 addGroupPing(packet)
func (g *GameVersionRelay) AddGroupPing(p *packet.Packet) {
	g.room.GroupNet.BroadcastAndUDP(p)
}

// AddRelayConnect 对应 addRelayConnect(): 以玩家身份接入房间
//
// 上游用一个大 try/catch 包住全流程: 任何异常都把状态退回 CertifiedEnd、
// 从群发池移除并重新走 relayDirectInspection. Go 用 defer/recover 等效.
func (g *GameVersionRelay) AddRelayConnect() {
	defer func() {
		if e := recover(); e != nil {
			g.permissionStatus = core.RelayCertifiedEnd

			g.ConnectionAgreement.Remove(g.room.GroupNet)

			log.Errorf("[Relay] addRelayConnect", "{0}", e)

			_ = g.RelayDirectInspection(nil)
		}
		// 上游 finally 只有被注释掉的 cachePacket = null, 照搬为空
	}()

	g.permissionStatus = core.RelayPlayerPermission

	g.ConnectReceiveData.InputPassword = false
	if g.room == nil {
		log.Clog("?????")
		g.room = room.SingleRoom()
	}

	g.site = g.room.SetAddPosition()
	g.room.SetAbstractNetConnect(g)

	o := gameio.NewGameOutputStream()
	if g.clientVersion >= newRelayProtocolVersion {
		_ = o.WriteByte(1)
		o.WriteInt(g.site)
		// ?
		if g.registerPlayerId == "" {
			// 上游 registerPlayerId!! 在此抛 NPE 进入 catch 恢复路径
			panic("relay: registerPlayerId is null (Kotlin: registerPlayerId!!)")
		}
		o.WriteString(g.registerPlayerId)
		//o.writeBoolean(false)
		// User UUID
		o.WriteIsString("") // 上游 writeIsString(null)
		o.WriteIsString(g.IP())
		g.room.Admin().SendPacket(o.CreatePacket(packettype.FORWARD_CLIENT_ADD)) // 上游 admin!!
	} else {
		_ = o.WriteByte(0)
		o.WriteInt(g.site)
		if g.registerPlayerId == "" {
			panic("relay: registerPlayerId is null (Kotlin: registerPlayerId!!)")
		}
		o.WriteString(g.registerPlayerId)
		o.WriteIsString("") // 上游 writeIsString(null)
		g.room.Admin().SendPacket(o.CreatePacket(packettype.FORWARD_CLIENT_ADD))
	}

	if g.cachePacket == nil {
		// 上游 cachePacket!! 在此抛 NPE 进入 catch 恢复路径
		panic("relay: cachePacket is null (Kotlin: cachePacket!!)")
	}
	g.SendPackageToHOST(g.cachePacket)
	g.ConnectionAgreement.Add(g.room.GroupNet)
	_ = g.sendChatMessage(i18n("relay", global.ConfigRelay.MainID+g.room.ID), "RELAY_CN-ADMIN", 5)
	g.room.SetAddSize()
}

// RelayRegisterConnection 对应 relayRegisterConnection(packet)
//
// 首次调用 (registerPlayerId 为空) 只从 REGISTER_PLAYER 包里解析名字与 UUID-Hex;
// 之后的调用 (状态 >= PlayerPermission) 做 UUID 重复检测 / Relay-EX 数据绑定 /
// 踢出与封禁检查 / 同步次数检查 / 取代玩家, 最后转给房主.
func (g *GameVersionRelay) RelayRegisterConnection(p *packet.Packet) {
	sendPacketData := p

	if g.registerPlayerId == "" {
		func() {
			defer func() {
				if e := recover(); e != nil {
					log.Errorf("[No UUID-Hex]", "{0}", e)
				}
			}()
			stream := gameio.NewGameInputStreamFromPacket(p, 0)
			stream.ReadString()
			stream.Skip(12)
			g.name = stream.ReadString()
			stream.ReadIsString()
			stream.ReadString()
			g.registerPlayerId = stream.ReadString()
		}()
	} else if g.permissionStatus >= core.RelayPlayerPermission {
		if g.permissionStatus == core.RelayPlayerPermission {
			if !g.room.IsStartGame() {
				// 通过检测房间已有的 UUID-Hex, 来激进的解决一些问题
				//
				// Luke 对此的回答 :
				// True might make more sense, but a connection might be stale for a bit need to be careful with that.
				// example just after a crash
				//
				// 上游在 find 回调内直接 kick (ConcurrentHashMap 允许遍历中修改);
				// Go 的线程安全 map 持锁回调, kick->disconnect 会反向改本表导致死锁,
				// 因此先查后踢, 可观察行为一致.
				_, duplicate := g.room.AbstractNetConnectIntMap.Find(func(_ int32, other room.RelayConnect) bool {
					return other.PermissionStatus() == core.RelayPlayerJoinPermission && other.RegisterPlayerId() == g.registerPlayerId
				})
				if duplicate {
					g.Kick("[UUID Check] HEX 重复, 换个房间试试")
					return
				}
			}

			// Relay-EX
			if g.playerRelay == nil {
				if pr, ok := g.room.RelayPlayersData.Get(g.registerPlayerId); ok {
					g.playerRelay = pr
				} else {
					g.playerRelay = player.NewPlayerRelay(g, g.registerPlayerId, g.name)
					g.room.RelayPlayersData.Put(g.registerPlayerId, g.playerRelay)
				}
				g.playerRelay.NowName = g.name
				g.playerRelay.SetDisconnect(false)
				g.playerRelay.Con = g
			}

			if g.room.RelayKickData.ContainsKey("BAN" + g.IP()) {
				g.Kick("[BAN] 您被这个房间BAN了 请换一个房间")
				return
			}

			// 上游:
			//   relayKickData["KICK$registerPlayerId"]
			//       .ifNullResult(relayKickData["KICK$ipLong24"]) { null }
			// ifNullResult(result, blockNotNull) 的语义是: 接收者为 null 时返回
			// result, 否则返回 blockNotNull(接收者) —— 而这里的 blockNotNull 恒返回
			// null. 即 KICK$uuid 存在时 time 反而为 null (上游如此, 照搬).
			var kickTime int
			hasKickTime := false
			if _, uuidKicked := g.room.RelayKickData.Get("KICK" + g.registerPlayerId); !uuidKicked {
				if v, ipKicked := g.room.RelayKickData.Get("KICK" + g.IPLong24()); ipKicked {
					kickTime, hasKickTime = v, true
				}
			}

			if hasKickTime {
				if kickTime > timeutil.ConcurrentSecond() {
					g.Kick("[踢出等待] 您被这个房间踢出了 请稍等一段时间 或者换一个房间")
					return
				}
				g.room.RelayKickData.Remove("KICK" + g.registerPlayerId)
				g.room.RelayKickData.Remove("KICK" + g.IPLong24())
			}

			if g.room.IsStartGame() {
				if !g.room.SyncFlag {
					g.Kick("[Sync Lock] 这个房间拒绝重连")
					return
				}

				if g.playerRelay.PlayerSyncCount.CheckStatus() {
					g.playerRelay.PlayerSyncCount.Add(1)
				} else {
					// 上游写入常量 300 (不是当前秒+300), 照搬
					g.room.RelayKickData.Put("KICK"+g.registerPlayerId, 300)
					g.Kick("[同步检测] 您同步次数太多 请稍等一段时间 或者换一个房间")
					return
				}
			}
		}

		if g.room.IsStartGame() {
			if g.room.ReplacePlayerHex != "" {
				g.replacePlayerHex = g.room.ReplacePlayerHex
				g.room.ReplacePlayerHex = ""
				g.room.SendMsg("玩家 " + g.name + ", 取代了旧玩家")
			}
			if g.replacePlayerHex != "" {
				out := gameio.NewGameOutputStream()
				stream := gameio.NewGameInputStreamFromPacket(p, 0)
				out.WriteString(stream.ReadString())
				out.TransferToFixedLength(stream, 12)
				out.WriteString(stream.ReadString())
				out.WriteIsStringFrom(stream)
				out.WriteString(stream.ReadString())
				out.WriteString(g.replacePlayerHex)
				stream.ReadString()
				out.TransferTo(stream)
				sendPacketData = out.CreatePacket(packettype.REGISTER_PLAYER)
			}
		}

		g.permissionStatus = core.RelayPlayerJoinPermission
		g.SendPackageToHOST(sendPacketData)
	}
}

// AddReRelayConnect 对应 addReRelayConnect(): 房主迁移后所有连接重新向新房主注册
func (g *GameVersionRelay) AddReRelayConnect() {
	defer func() {
		if e := recover(); e != nil {
			// 上游 catch (e: IOException) e.printStackTrace()
			log.Errorf("[Relay] addReRelayConnect", "{0}", e)
		}
		// 上游 finally 只有被注释掉的 cachePacket = null, 照搬为空
	}()

	admin := g.room.Admin() // 上游 admin!!

	o := gameio.NewGameOutputStream()
	if g.clientVersion >= newRelayProtocolVersion {
		_ = o.WriteByte(1)
		o.WriteInt(g.site)
		// ?
		o.WriteString(g.registerPlayerId) // 上游 registerPlayerId!!
		// User UUID
		o.WriteIsString(g.registerPlayerId)
		o.WriteIsString(g.IP())
		admin.SendPacket(o.CreatePacket(packettype.FORWARD_CLIENT_ADD))
	} else {
		_ = o.WriteByte(0)
		o.WriteInt(g.site)
		o.WriteString(g.registerPlayerId)
		o.WriteIsString(g.registerPlayerId)
		admin.SendPacket(o.CreatePacket(packettype.FORWARD_CLIENT_ADD))
	}
	// 上游在这里把同一个 FORWARD_CLIENT_ADD 又发了一次, 照搬
	admin.SendPacket(o.CreatePacket(packettype.FORWARD_CLIENT_ADD))

	if g.cachePacket == nil {
		// 上游 cachePacket!! 抛 NPE; 不在本方法的 catch(IOException) 内, 向上传播
		panic("relay: cachePacket is null (Kotlin: cachePacket!!)")
	}
	o1 := gameio.NewGameOutputStream()
	o1.WriteInt(g.site)
	o1.WriteInt(int32(len(g.cachePacket.Bytes) + 8))
	o1.WriteInt(int32(len(g.cachePacket.Bytes)))
	o1.WriteInt(160) // 上游硬编码 160 (PREREGISTER_INFO_RECEIVE 的协议号)
	o1.WriteBytes(g.cachePacket.Bytes)
	admin.SendPacket(o1.CreatePacket(packettype.PACKET_FORWARD_CLIENT_FROM))
}

// AddRelaySend 对应 addRelaySend(packet): 解析转发包并投递给目标连接
func (g *GameVersionRelay) AddRelaySend(p *packet.Packet) {
	defer func() {
		if e := recover(); e != nil {
			log.Errorf("[RELAY NormalForwarding Error]", "{0}", e)
		}
	}()

	inStream := gameio.NewGameInputStreamFromPacket(p, 0)
	target := inStream.ReadInt()

	typeInt := inStream.ReadInt()
	bytes := inStream.ReadStreamBytes()

	abstractNetConnect, _ := g.room.GetAbstractNetConnect(target)

	sendPacketData := packet.NewPacketFromInt(int(typeInt), bytes)
	if abstractNetConnect != nil {
		abstractNetConnect.SendPacket(sendPacketData)
	}
	// 上游把可能为 null 的目标连接也传给 sendPacketExtractInformation
	var targetRelay *GameVersionRelay
	if abstractNetConnect != nil {
		targetRelay, _ = abstractNetConnect.(*GameVersionRelay)
	}
	g.SendPacketExtractInformation(sendPacketData, targetRelay)
}

// SendPackageToHOST 对应 sendPackageToHOST(packet): 把包转发给房主
//
// 这是 Relay 转发的核心路径. 上游 room!! 在房间为 null 时抛 NPE, 且**不**在
// 本方法的 catch(IOException) 范围内 —— Go 用 panic 保持同样的传播语义
// (调用方如 AddRelayConnect 的 recover 会接住).
func (g *GameVersionRelay) SendPackageToHOST(p *packet.Packet) {
	if g.room == nil {
		panic("relay: room is null (Kotlin: room!!)")
	}

	o := gameio.NewGameOutputStream()
	o.WriteInt(g.site)
	o.WriteInt(int32(len(p.Bytes) + 8))
	o.WriteInt(int32(len(p.Bytes)))
	o.WriteBytes(p.Type.TypeIntBytes())
	o.WriteBytes(p.Bytes)
	// 上游 admin?.sendPacket: admin 可空
	if admin := g.room.Admin(); admin != nil {
		admin.SendPacket(o.CreatePacket(packettype.PACKET_FORWARD_CLIENT_FROM))
	}
}

// RelayPlayerDisconnect 对应 relayPlayerDisconnect(): 通知 HOST 本连接断开
func (g *GameVersionRelay) RelayPlayerDisconnect() {
	out := gameio.NewGameOutputStream()
	_ = out.WriteByte(0)
	out.WriteInt(g.site)
	g.SendPackageToHOST(out.CreatePacket(packettype.FORWARD_CLIENT_REMOVE))
}

// MulticastAnalysis 对应 multicastAnalysis(packet)
func (g *GameVersionRelay) MulticastAnalysis(*packet.Packet) {
	// Protocol not supported
}

// ---------------------------------------------------------------------------
// 连接生命周期
// ---------------------------------------------------------------------------

// Kick 对应 internal kick(msg): 发 KICK 包并断开
//
// 上游是 internal 可见性 (模块内), 消费者是 RelayClientCommands; Go 没有等价
// 可见性, 导出并在注释标明.
func (g *GameVersionRelay) Kick(msg string) {
	o := gameio.NewGameOutputStream()
	o.WriteString(msg)
	g.SendPacket(o.CreatePacket(packettype.KICK))
	g.Disconnect()
}

// Disconnect 对应 disconnect()
//
// 上游顺序: isDis 守卫 -> 状态 >= PlayerPermission 时做房间清理
// (synchronized(room) 内: 房主迁移或关房 / 普通玩家移除) -> close(groupNet).
func (g *GameVersionRelay) Disconnect() {
	if g.IsDis {
		return
	}
	g.IsDis = true

	g.ConnectReceiveData.ReceiveBigPacket = false

	if g.permissionStatus >= core.RelayPlayerPermission {
		if g.room == nil {
			// 上游 room!! 抛 NPE (状态 >= PlayerPermission 时房间必然已设置)
			panic("relay: room is null (Kotlin: room!!)")
		}
		g.room.SetRemoveSize()

		// 避免多个玩家断开导致 NPE (上游 synchronized(room!!))
		g.room.WithDisconnectLock(func() {
			func() {
				defer func() {
					if e := recover(); e != nil {
						if !g.room.CloseRoom() {
							log.Debugf("[Relay Close Error]", "{0}", e)
						}
					}
				}()

				if g.permissionStatus == core.RelayHostPermission {
					room.ServerRelayIpData.Remove(g.IP(), func(a, b string) bool { return a == b })
					if g.room.GetSize() > 0 {
						// Move Room Admin
						g.adminMoveNew()
					} else {
						// Close Room
						g.room.Re()
					}
				} else {
					g.room.RemoveAbstractNetConnect(g.site)

					// 解决一些问题, 让 HOST 删掉无用转发ID
					g.RelayPlayerDisconnect()

					if g.permissionStatus >= core.RelayPlayerJoinPermission {
						if !g.room.IsStartGame() {
							g.room.RelayPlayersData.Remove(g.registerPlayerId)
						}
					}
				}
			}()

			if g.room.GetSize() <= 0 && !g.room.CloseRoom() {
				log.Debug("[Relay] Game over")
				g.room.Re()
			}
			g.Close(g.room.GroupNet)
		})
	} else {
		g.Close(nil)
	}
}

// SendPacketExtractInformation 对应 open sendPacketExtractInformation(packet, abstractNetConnect)
//
// 在转发路径上顺带提取信息: TEAM_LIST 回填队伍数据 / 返回战场重置开局标志 /
// START_GAME 缓存开局包.
func (g *GameVersionRelay) SendPacketExtractInformation(p *packet.Packet, abstractNetConnect *GameVersionRelay) {
	switch packettype.PacketType(p.Type.TypeInt()) {
	case packettype.TEAM_LIST:
		if !g.room.IsStartGame() {
			if abstractNetConnect != nil {
				if abstractNetConnect.playerRelay == nil {
					// 上游 playerRelay!! 抛 NPE, 被 addRelaySend 的 catch 记日志后中止
					log.Errorf("[RELAY NormalForwarding Error]", "{0}",
						errors.New("playerRelay is null (Kotlin: playerRelay!!)"))
					return
				}
				if packetTeamDataParser != nil {
					parse := gameio.NewGameInputStreamFromBytes(p.Bytes, abstractNetConnect.clientVersion)
					if err := packetTeamDataParser(parse, abstractNetConnect.playerRelay); err != nil {
						// 上游 getPacketTeamData 抛异常时同样落到 addRelaySend 的 catch
						log.Errorf("[RELAY NormalForwarding Error]", "{0}", err)
						return
					}
				}
			}
		}
		if abstractNetConnect != nil && abstractNetConnect.startGameFlag && g.site == -2 && g.room.GamePacket.StartGamePacket != nil {
			abstractNetConnect.SendPacket(g.room.GamePacket.StartGamePacket)
			abstractNetConnect.startGameFlag = false
		}
	case packettype.RETURN_TO_BATTLEROOM:
		if g.room.IsStartGame() {
			g.room.SetIsStartGame(false)
			g.room.GamePacket.StartGamePacket = nil
		}
	case packettype.START_GAME:
		g.room.SetIsStartGame(true)
		if g.site == -1 && g.room.GamePacket.StartGamePacket == nil {
			in := gameio.NewGameInputStreamFromPacket(p, 0)
			in.Skip(1)
			if in.ReadInt() != 0 {
				g.room.GamePacket.StartGamePacket = p
			}
		}
	default:
		// 没其他必要
	}
}

// ---------------------------------------------------------------------------
// 房间 ID 解析与建房 (上游 private 成员)
// ---------------------------------------------------------------------------

// idCustom 对应 private idCustom(inId)
//
// 解析玩家输入的房间 ID: old / R 前缀 / C 自定义 / new(s) mod(s) 前缀 /
// P 人数 I 收入自定义参数, 最终建房 (newRelayId) 或加入已有房间 (getRelay).
func (g *GameVersionRelay) idCustom(inId string) {
	// 过滤 制表符 空格 换行符
	id := whitespacePattern.ReplaceAllString(inId, "")
	if strings.EqualFold(id, "old") {
		id = room.ServerRelayOld.GetOrDefault(g.registerPlayerId, "")
	} else {
		room.ServerRelayOld.Put(g.registerPlayerId, id)
	}

	if id == "" {
		g.SendRelayServerType(i18n("relay.server.error.id", "什么都没输入就点击确认"), nil)
		return
	}
	if emoji.ContainsEmoji(id) {
		g.SendRelayServerType(i18n("relay.server.error.id", "不能使用Emoji"), nil)
		return
	}

	if g.idDistribute(id) {
		if global.ConfigRelay.MainServer {
			g.SendPacket(relaypacket.FromRelayJumpsToAnotherServerPacket(firstRuneString(id, 1)+".relay.rwhps.net/"+id))
			return
		}
		id = dropRunes(id, 2)
	} else if strings.EqualFold(firstRuneString(id, 0), "R") {
		id = dropRunes(id, 1)
	}

	if id == "" {
		g.SendRelayServerType(i18n("relay.server.no", "R"), nil)
		return
	}

	// 上游此处还声明了 var uplist = false, 全函数零使用 (遗留死变量), 不移植
	mods := strings.Contains(strings.ToLower(id), "mod")
	customId := ""
	newRoom := true

	if strings.EqualFold(firstRuneString(id, 0), "C") {
		id = dropRunes(id, 1)
		if id == "" {
			g.SendRelayServerType(i18n("relay.id.re"), nil)
			return
		}

		ary := strings.Split(id, "@")
		if len(ary) > 1 {
			id = ary[1]
		} else {
			id = ""
		}
		customId = ary[0]

		// 上游 customId[0] 在空串上抛 IndexOutOfBounds (被外层 catch 或向上传播),
		// Go 用 rune 切片越界 panic 保持同样的崩溃语义 (由上层 recover 接住)
		customRunes := []rune(customId)
		if strings.EqualFold(string(customRunes[0]), "M") {
			customId = string(customRunes[1:])
			if !g.checkLength(customId) {
				return
			}
			taken, _ := room.GetCheckRelay(customId)
			if taken || g.idDistribute("R"+customId) {
				g.SendRelayServerType(i18n("relay.id.re"), nil)
				return
			}
			mods = true
		} else {
			if !g.checkLength(customId) {
				return
			}
			taken, _ := room.GetCheckRelay(customId)
			if taken || g.idDistribute("R"+customId) {
				g.SendRelayServerType(i18n("relay.id.re"), nil)
				return
			}
		}
	} else {
		lower := strings.ToLower(id)
		switch {
		case strings.HasPrefix(lower, "news") || strings.HasPrefix(lower, "mods"):
			id = dropRunes(id, 4)
		case strings.HasPrefix(lower, "new") || strings.HasPrefix(lower, "mod"):
			id = dropRunes(id, 3)
		default:
			newRoom = false
		}
	}

	if newRoom {
		custom := newCustomRelayData()

		// 上游 try/catch(NumberFormatException): 解析失败时回 relay.server.error
		if err := g.parseCustomParams(&id, &custom); err != nil {
			if !errors.Is(err, errCustomParamRejected) {
				g.SendRelayServerType(i18n("relay.server.error", err.Error()), nil)
			}
			return
		}

		g.newRelayId(customId, mods, custom)
		if custom.maxPlayerSize != -1 || custom.maxUnitSizt != 200 {
			_ = g.sendChatMessage(
				"自定义人数: "+strconv.Itoa(custom.maxPlayerSize)+" 自定义单位: "+strconv.Itoa(custom.maxUnitSizt),
				"RELAY_CN-Custom", 5)
		}
	} else {
		if strings.Contains(id, ".") {
			g.SendRelayServerType(i18n("relay.server.error", "不能包含 [ . ]"), nil)
			return
		}
		r, err := room.GetRelay(id)
		if err != nil {
			// 上游 catch (e: Exception) { debug(e); sendRelayServerType(relay.server.error, e.message) }
			log.Debug(err)
			g.SendRelayServerType(i18n("relay.server.error", err.Error()), nil)
			return
		}
		g.room = r
		if g.room != nil {
			g.AddRelayConnect()
		} else {
			g.SendRelayServerType(i18n("relay.server.no", id), nil)
		}
	}
}

// errCustomParamRejected 是 parseCustomParams 的哨兵错误:
// 已经向客户端发过拒绝原因 (relay.id.maxPlayer.re 等), 不需要再发 relay.server.error
var errCustomParamRejected = errors.New("relay: custom param rejected")

// parseCustomParams 对应 idCustom 里 newRoom 分支的 try 块 (P 人数 / I 收入解析)
//
// 上游数字解析失败抛 NumberFormatException; Go 的 strconv 错误经返回值传回.
// 上游 toFloat 对超大值返回 Infinity 而不抛异常, Go 的 ParseFloat 在 ErrRange
// 时同样给出 ±Inf, 按同一范围检查拒绝.
func (g *GameVersionRelay) parseCustomParams(id *string, custom *customRelayData) error {
	if strings.HasPrefix(strings.ToLower(*id), "p") {
		*id = dropRunes(*id, 1)
		arry := splitFullWidthComma(*id)

		v, err := strconv.Atoi(arry[0])
		if err != nil {
			return err
		}
		custom.maxPlayerSize = v
		if custom.maxPlayerSize < 0 || custom.maxPlayerSize > 100 {
			g.SendRelayServerType(i18n("relay.id.maxPlayer.re"), nil)
			return errCustomParamRejected
		}
		if len(arry) > 1 {
			if strings.Contains(strings.ToUpper(arry[1]), "I") {
				ay := splitFold(arry[1], "I")
				if len(ay) > 1 {
					mu, err := strconv.Atoi(ay[0])
					if err != nil {
						return err
					}
					custom.maxUnitSizt = mu
					income, err := parseJavaFloat(ay[1])
					if err != nil {
						return err
					}
					custom.income = income
				} else {
					income, err := parseJavaFloat(ay[0])
					if err != nil {
						return err
					}
					custom.income = income
				}
			} else {
				mu, err := strconv.Atoi(arry[1])
				if err != nil {
					return err
				}
				custom.maxUnitSizt = mu
			}
			// 上游 !in 0..Int.MAX_VALUE: int32 解析结果只会小于 0 时越界
			if custom.maxUnitSizt < 0 || custom.maxUnitSizt > math.MaxInt32 {
				g.SendRelayServerType(i18n("relay.id.maxUnit.re"), nil)
				return errCustomParamRejected
			}
		}
	}
	if strings.HasPrefix(strings.ToLower(*id), "i") {
		*id = dropRunes(*id, 1)
		arry := splitFullWidthComma(*id)

		income, err := parseJavaFloat(arry[0])
		if err != nil {
			return err
		}
		custom.income = income
		// 上游 !in 0F..Float.MAX_VALUE (NaN 同样不在范围内)
		if !(custom.income >= 0 && custom.income <= math.MaxFloat32) {
			g.SendRelayServerType(i18n("relay.id.income.re"), nil)
			return errCustomParamRejected
		}
	}
	return nil
}

// idDistribute 对应 private idDistribute(id): 上游恒返回 false
func (g *GameVersionRelay) idDistribute(string) bool {
	return false
}

// checkLength 对应 private checkLength(str)
func (g *GameVersionRelay) checkLength(str string) bool {
	if len([]rune(str)) > 7 || len([]rune(str)) < 3 {
		g.SendRelayServerType(i18n("relay.id.re"), nil)
		return false
	}
	if strings.Contains(str, ".") {
		g.SendRelayServerType(i18n("relay.server.error", "不能包含 [ . ]"), nil)
		return false
	}
	return true
}

// adminMoveNew 对应 private adminMoveNew(): 房主迁移
func (g *GameVersionRelay) adminMoveNew() {
	// 更新最小玩家
	g.room.UpdateMinSize()
	if next, ok := g.room.GetAbstractNetConnect(g.room.MinSize()); ok {
		next.SendRelayServerId(false)
		// 上游遍历 abstractNetConnectIntMap.values; Go 侧同样取快照遍历,
		// 避免持锁回调期间转发链反向修改房间表
		for _, obj := range g.room.AbstractNetConnectIntMap.Values() {
			obj.AddReRelayConnect()
		}
		g.AddReRelayConnect()

		np := g.netPacket()
		if np == nil {
			panic(core.ErrNotImplemented)
		}
		exitPacket, err := np.GetExitPacket()
		if err != nil {
			// 上游 getExitPacket 抛异常时传播到 disconnect 的 catch, Go 用 panic 等效
			panic(err)
		}
		g.SendPackageToHOST(exitPacket)
	}
}

// newRelayId 对应 private newRelayId(id = null, mod, customRelayData = CustomRelayData())
func (g *GameVersionRelay) newRelayId(id string, mod bool, customRelayData customRelayData) {
	maxPlayer := 10
	if customRelayData.maxPlayerSize != -1 {
		maxPlayer = customRelayData.maxPlayerSize
	}

	// 上游两个分支分别是 RelayRoom.getRelay(playerName=..., id 默认 "") 与
	// RelayRoom.getRelay(id, ...); Go 的 GetRelayNew 以空 id 表达"自动生成".
	if isutils.IsBlank(id) {
		g.room = room.GetRelayNew("", g.name, mod, g.betaGameVersion, g.clientVersion, maxPlayer)
	} else {
		g.room = room.GetRelayNew(id, g.name, mod, g.betaGameVersion, g.clientVersion, maxPlayer)
	}

	g.room.IsMod = mod

	if customRelayData.maxPlayerSize != -1 || customRelayData.income != 1 {
		g.customModePlayerSize(customRelayData)
	}

	g.SendRelayServerId(false)
	g.room.SetAddSize()
}

// customModePlayerSize 对应 private customModePlayerSize(customRelayData)
//
// 给自定义房间伪造一套服务端握手包 (PREREGISTER_INFO / SERVER_INFO / TEAM_LIST),
// 让客户端接受自定义的人数 / 单位数 / 收入.
func (g *GameVersionRelay) customModePlayerSize(customRelayData customRelayData) {
	registerServer := gameio.NewGameOutputStream()
	registerServer.WriteString(global.ServerID)
	registerServer.WriteInt(1)
	registerServer.WriteInt(int32(g.clientVersion))
	registerServer.WriteInt(int32(g.clientVersion))
	registerServer.WriteString("com.corrodinggames.rts.server")
	registerServer.WriteString(g.room.ServerUUID)
	registerServer.WriteInt(javastr.HashCode("Dr @ 2022"))
	g.SendPacket(registerServer.CreatePacket(packettype.PREREGISTER_INFO))

	o2 := gameio.NewGameOutputStream()
	o2.WriteString(global.ServerIDRelay)
	o2.WriteInt(int32(g.clientVersion))
	o2.WriteInt(int32(game.CustomMap))
	o2.WriteString("RW-HPS RELAY Custom Mode")
	// credits
	o2.WriteInt(0)
	// mist
	o2.WriteInt(2)
	o2.WriteBoolean(true)
	o2.WriteInt(1)
	_ = o2.WriteByte(0)
	o2.WriteBoolean(false)
	o2.WriteBoolean(false)
	g.SendPacket(o2.CreatePacket(packettype.SERVER_INFO))

	o := gameio.NewGameOutputStream()
	o.WriteInt(0)
	/* RELAY Custom MaxPlayer */
	o.WriteBoolean(false)
	o.WriteInt(int32(customRelayData.maxPlayerSize))
	// 上游 flushEncodeData(CompressOutputStream.getGzipOutputStream("teams", true)
	//     .also { for (i in 0 until maxPlayerSize) it.writeBoolean(false) })
	// writeBoolean(false) 即 0 字节; `0 until 负数` 为空区间
	teamsLen := customRelayData.maxPlayerSize
	if teamsLen < 0 {
		teamsLen = 0
	}
	o.FlushEncodeDataFromCompress("teams", compression.GzipEncode(make([]byte, teamsLen)))
	// mist
	o.WriteInt(2)
	// credits
	o.WriteInt(0)
	o.WriteBoolean(true)
	o.WriteInt(1)
	_ = o.WriteByte(5)
	// RELAY Custom MaxUnit
	o.WriteInt(int32(customRelayData.maxUnitSizt))
	o.WriteInt(int32(customRelayData.maxUnitSizt))
	// initUnit
	o.WriteInt(1)
	// RELAY Custom income
	o.WriteFloat(customRelayData.income)
	// Ban nuke
	o.WriteBoolean(true)
	o.WriteBoolean(false)
	o.WriteBoolean(false)
	// sharedControl
	o.WriteBoolean(false)
	// gamePaused
	o.WriteBoolean(false)
	g.SendPacket(o.CreatePacket(packettype.TEAM_LIST))
}

// customRelayData 对应 private data class CustomRelayData
type customRelayData struct {
	maxPlayerSize int
	maxUnitSizt   int
	income        float32
}

// newCustomRelayData 对应 CustomRelayData() 的默认值
func newCustomRelayData() customRelayData {
	return customRelayData{maxPlayerSize: -1, maxUnitSizt: 200, income: 1}
}

// ---------------------------------------------------------------------------
// 字符串工具 (对应上游 Kotlin 标准库调用)
// ---------------------------------------------------------------------------

// firstRuneString 对应 Kotlin 的 id[index].toString() (取第 index 个 UTF-16 字符)
//
// 上游对空串/越界抛 IndexOutOfBounds; 本实现的所有调用点在上游都有前置空判断,
// 因此越界时返回空串不影响可观察行为.
func firstRuneString(s string, index int) string {
	i := 0
	for _, r := range s {
		if i == index {
			return string(r)
		}
		i++
	}
	return ""
}

// dropRunes 对应 Kotlin 的 id.substring(n) (按字符数)
func dropRunes(s string, n int) string {
	if n <= 0 {
		return s
	}
	runes := []rune(s)
	if n >= len(runes) {
		return ""
	}
	return string(runes[n:])
}

// splitFullWidthComma 对应 `if (id.contains("，")) id.split("，") else id.split(",")`
func splitFullWidthComma(s string) []string {
	if strings.Contains(s, "，") {
		return strings.Split(s, "，")
	}
	return strings.Split(s, ",")
}

// splitFold 对应 Kotlin split(sep, ignoreCase = true)
func splitFold(s, sep string) []string {
	upper := strings.ToUpper(s)
	sepUpper := strings.ToUpper(sep)
	var out []string
	rest, restUpper := s, upper
	for {
		idx := strings.Index(restUpper, sepUpper)
		if idx < 0 {
			out = append(out, rest)
			return out
		}
		out = append(out, rest[:idx])
		rest = rest[idx+len(sep):]
		restUpper = restUpper[idx+len(sep):]
	}
}

// parseJavaFloat 对应 Kotlin String.toFloat()
//
// 与 strconv.ParseFloat 的差异: Kotlin 对越界值返回 ±Infinity 而不是错误,
// 因此 ErrRange 时保留 ±Inf 结果, 其余错误原样返回.
func parseJavaFloat(s string) (float32, error) {
	v, err := strconv.ParseFloat(s, 32)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, err
	}
	return float32(v), nil
}

// 上游 companion object 还有 urlMath 正则与 findMapURL(mapName): 用 DNS 解析判断
// 地图名里是否含 URL. 它在原版是 private 且**零调用** (死代码), 不移植.
// 若将来需要, Go 等价实现是 net.LookupHost (对应 InetAddress.getAllByName).
