package netconnectprotocol

// TypeRelay 对应 net.rwhps.server.net.netconnectprotocol.TypeRelay:
// 解析 RelayProtocol 协议 (227 行的握手状态机 + 分发器).
//
// # 依赖方向
//
// 上游 TypeRelay 与 GameVersionRelay 同属 netconnectprotocol 包族; Go 侧
// GameVersionRelay 在 realize 子包. 本文件导入 realize 是合法方向 ——
// realize 不反向导入本包 (它对 UniversalAnalysisOfGamePackages 的依赖经
// realize.SetPacketTeamDataParser 注入, 见 realize 包注释), 因此无循环.

import (
	"strings"

	"rwhps/internal/data/global"
	gameio "rwhps/internal/io"
	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
	"rwhps/internal/net/core"
	"rwhps/internal/net/netconnectprotocol/realize"
	"rwhps/internal/util/javastr"
	"rwhps/internal/util/log"

	cmd "rwhps/internal/util/game/command"
)

// TypeRelay 对应 TypeRelay
//
// 与上游一致的两种形态:
//   - 工厂形态 (con == nil): 由 RelayRwHps 持有, GetTypeConnect 为每条连接实例化,
//     对应上游 constructor(con: Class<out GameVersionRelay>) 的"实例化模板"用途
//   - 连接形态 (con != nil): 绑定一个 GameVersionRelay, ProcessConnect 逐包分发
//
// 上游工厂形态经反射 (ReflectionUtils.accessibleConstructor) 实例化连接;
// Go 没有运行期反射实例化, 直接调用 realize.NewGameVersionRelay.
type TypeRelay struct {
	con *realize.GameVersionRelay
}

// 编译期确认: TypeRelay 实现 core.TypeConnect
var _ core.TypeConnect = (*TypeRelay)(nil)

// NewTypeRelay 对应 constructor(con: GameVersionRelay)
func NewTypeRelay(con *realize.GameVersionRelay) *TypeRelay { return &TypeRelay{con: con} }

// NewTypeRelayFactory 构造工厂形态, 对应上游的模板构造器
func NewTypeRelayFactory() *TypeRelay { return &TypeRelay{} }

// GetTypeConnect 对应 getTypeConnect: 为新连接创建处理器
func (t *TypeRelay) GetTypeConnect(ca *core.ConnectionAgreement) core.TypeConnect {
	return NewTypeRelay(realize.NewGameVersionRelay(ca))
}

// SetData 对应 setData: 非必要实现
func (t *TypeRelay) SetData(any) {}

// ProcessConnect 对应 processConnect
//
// 上游 @Throws(Exception) 由 netty 层兜底; Go 侧在入口 recover, 一条坏包
// 只记日志不带走连接 (与 gameserver.HeadlessTypeConnect 同款处理).
func (t *TypeRelay) ProcessConnect(p *packet.Packet) (err error) {
	con := t.con
	if con == nil || p == nil {
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			log.Errorf("[TypeRelay]", "processConnect panic: {0}", r)
			err = nil
		}
	}()

	if t.relayCheck(p) {
		return nil
	}

	return t.packetProcessing(p)
}

// hostProcessing 对应 protected hostProcessing: HOST (房主) 连接的包分发
func (t *TypeRelay) hostProcessing(p *packet.Packet) error {
	con := t.con

	switch packettype.PacketType(p.Type.TypeInt()) {
	case packettype.PACKET_FORWARD_CLIENT_TO:
		con.AddRelaySend(p)
		return nil

	case packettype.HEART_BEAT:
		con.PingData(p)

	case packettype.CHAT:
		inStream := gameio.NewGameInputStreamFromPacket(p, 0)
		message := inStream.ReadString()
		inStream.Skip(1)
		if inStream.ReadIsString() == con.Name() && strings.HasPrefix(message, ".") {
			response := global.RelayCommand.HandleMessageWith(message, con)
			if response == nil || response.Type == cmd.NoCommand {
				// Ignore
			} else if response.Type != cmd.Valid {
				var text string
				switch response.Type {
				case cmd.ManyArguments:
					text = "Too many arguments. Usage: " + response.Command.Text + " " + response.Command.ParamText
				case cmd.FewArguments:
					text = "Too few arguments. Usage: " + response.Command.Text + " " + response.Command.ParamText
				default:
					return nil
				}
				if np := t.netPacket(); np != nil {
					if pkt, err := np.GetSystemMessagePacket(text); err == nil {
						con.SendPacket(pkt)
					}
				}
			}
		}
		return nil

	case packettype.DISCONNECT:
		con.Disconnect()

	default:
		// Relay HOST should no longer send any more packets for the server to process
		// Ignore
	}
	return nil
}

// packetProcessing 对应 private packetProcessing: 按状态分流 HOST / 普通玩家
func (t *TypeRelay) packetProcessing(p *packet.Packet) error {
	if t.con.PermissionStatus() == core.RelayHostPermission {
		return t.hostProcessing(p)
	}
	return t.normalProcessing(p)
}

// normalProcessing 对应 private normalProcessing: 非 HOST 连接的包分发
//
// 上游把大量包类型列入"忽略"名单 (防止普通玩家伪造服务端包), 照搬.
func (t *TypeRelay) normalProcessing(p *packet.Packet) error {
	con := t.con

	switch packettype.PacketType(p.Type.TypeInt()) {
	// USERS (NON-HOST) SHOULD NOT SEND RELAY PACKETS
	case packettype.RELAY_117, packettype.RELAY_118_117_RETURN, packettype.RELAY_POW,
		packettype.RELAY_POW_RECEIVE, packettype.RELAY_VERSION_INFO, packettype.RELAY_BECOME_SERVER,
		packettype.FORWARD_CLIENT_ADD, packettype.FORWARD_CLIENT_REMOVE,
		packettype.PACKET_FORWARD_CLIENT_FROM, packettype.PACKET_FORWARD_CLIENT_TO,
		packettype.PACKET_FORWARD_CLIENT_TO_REPEATED,
		// 防止假冒
		packettype.TICK, packettype.SYNC, packettype.SERVER_INFO, packettype.HEART_BEAT,
		packettype.START_GAME, packettype.RETURN_TO_BATTLEROOM, packettype.CHAT,
		packettype.KICK, packettype.PACKET_RECONNECT_TO,
		// 每个玩家不一样, 不需要处理/缓存
		packettype.PASSWD_ERROR, packettype.TEAM_LIST, packettype.PREREGISTER_INFO,
		// Refusal to Process
		packettype.EMPTYP_ACKAGE, packettype.NOT_RESOLVED:
		// Ignore

	case packettype.GAMECOMMAND_RECEIVE:
		return con.ReceiveCommand(p)

	case packettype.REGISTER_PLAYER:
		con.RelayRegisterConnection(p)

	case packettype.CHAT_RECEIVE:
		return con.ReceiveChat(p)

	case packettype.ACCEPT_START_GAME:
		con.Room().SetIsStartGame(true) // 上游 room!!, 房间为 nil 时 panic 由入口 recover 接住
		con.SendPackageToHOST(p)

	case packettype.DISCONNECT:
		con.SendPackageToHOST(p)
		con.Disconnect()

	default:
		con.SendPackageToHOST(p)
	}
	return nil
}

// relayCheck 对应 private relayCheck: 握手状态机
//
// 返回 true 表示包已被握手逻辑消费, 不再进入分发. 状态迁移:
//
//	InitialConnection --PREREGISTER_INFO_RECEIVE--> GetPlayerInfo
//	GetPlayerInfo     --REGISTER_PLAYER-----------> WaitCertified
//	WaitCertified     --RELAY_POW_RECEIVE (通过)--> CertifiedEnd
//	CertifiedEnd      --RELAY_118_117_RETURN------> (服务端类型回复, 状态不变)
//
// 其余状态一律断开 (你肯定没验证).
func (t *TypeRelay) relayCheck(p *packet.Packet) bool {
	con := t.con
	con.RefreshLastReceivedTime()

	permissionStatus := con.PermissionStatus()

	if permissionStatus >= core.RelayPlayerPermission {
		return false
	}

	switch permissionStatus {
	// Initial Connection
	case core.RelayInitialConnection:
		if p.EqualsType(packettype.PREREGISTER_INFO_RECEIVE) {
			con.SetPermissionStatus(core.RelayGetPlayerInfo)
			con.SetCachePacket(p)
			registerServer := gameio.NewGameOutputStream()
			registerServer.WriteString(global.ServerIDRelayGet)
			registerServer.WriteInt(1)
			registerServer.WriteInt(0)
			registerServer.WriteInt(0)
			registerServer.WriteString("com.corrodinggames.rts.server")
			registerServer.WriteString(global.ServerRelayUUID)
			registerServer.WriteInt(javastr.HashCode("Dr @ 2022"))
			con.SendPacket(registerServer.CreatePacket(packettype.PREREGISTER_INFO))
		} else {
			con.ExCommand(p)
		}
		return true

	case core.RelayGetPlayerInfo:
		if p.EqualsType(packettype.REGISTER_PLAYER) {
			con.RelayRegisterConnection(p)
			// Wait Certified
			con.SetPermissionStatus(core.RelayWaitCertified)
			con.SendRelayServerInfo()
			con.SendVerifyClientValidity()
		}
		return true

	case core.RelayWaitCertified:
		if p.EqualsType(packettype.RELAY_POW_RECEIVE) {
			if con.ReceiveVerifyClientValidity(p) {
				// Certified End
				con.SetPermissionStatus(core.RelayCertifiedEnd)
				_ = con.RelayDirectInspection(nil)
			} else {
				con.SendVerifyClientValidity()
			}
		} else {
			con.Disconnect()
		}
		return true

	case core.RelayCertifiedEnd:
		if p.EqualsType(packettype.RELAY_118_117_RETURN) {
			con.SendRelayServerTypeReply(p)
		}
		return true

	// 你肯定没验证
	default:
		con.Disconnect()
		return true
	}
}

// netPacket 取包构造器, 对应 con.rwHps.abstractNetPacket
func (t *TypeRelay) netPacket() netpacketLike {
	rw := t.con.RwHps()
	if rw == nil {
		return nil
	}
	return rw.AbstractNetPacket()
}

// netpacketLike 是 netpacket.AbstractNetPacket 的子集, 只保留本文件用到的成员
type netpacketLike interface {
	GetSystemMessagePacket(msg string) (*packet.Packet, error)
}

// AbstractNetConnect 实现 core.TypeConnect
func (t *TypeRelay) AbstractNetConnect() *core.AbstractNetConnect {
	if t.con == nil {
		return nil
	}
	return t.con.AbstractNetConnect
}

// Version 对应 version: "RELAY 1.1.0"
func (t *TypeRelay) Version() string { return "RELAY 1.1.0" }
