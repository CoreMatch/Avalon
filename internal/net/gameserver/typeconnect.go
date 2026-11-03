package gameserver

import (
	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
	"rwhps/internal/net/core"
	"rwhps/internal/util/control"
	"rwhps/internal/util/log"
)

// HeadlessTypeConnect 对应 HeadlessTypeConnect: 解析 ServerProtocol 协议
//
// 与上游一致的两种形态:
//   - 工厂形态 (con == nil): 由 RwHps 持有, GetTypeConnect 为每条连接实例化处理器,
//     对应上游 constructor(con: Class<out GameVersionServer>) 的"实例化模板"用途
//   - 连接形态 (con != nil): 绑定一个 GameVersionServer, ProcessConnect 逐包分发
type HeadlessTypeConnect struct {
	con *GameVersionServer
}

// NewHeadlessTypeConnect 构造工厂形态, 对应上游的模板构造器
func NewHeadlessTypeConnect() *HeadlessTypeConnect { return &HeadlessTypeConnect{} }

// GetTypeConnect 对应 getTypeConnect: 为新连接创建处理器
//
// 上游在这里创建 PlayerConnectX 并 netEngine.aM.add(playerConnect) (引擎登记连接);
// Go 版把"引擎登记"放到 GameVersionServer 构造里经 conn.open 完成, 时机不变.
//
// 上游随后把 playerConnect 加进 netEngine.aM; Go 侧对应物是 helper 的 conn.open,
// 已在 NewGameVersionServer 内完成. UDP 标志位 (h/i = false) 属引擎内部状态,
// 外置后由 helper 自行管理.
func (t *HeadlessTypeConnect) GetTypeConnect(ca *core.ConnectionAgreement) core.TypeConnect {
	return &HeadlessTypeConnect{con: NewGameVersionServer(ca)}
}

// SetData 对应 setData: 非必要实现
func (t *HeadlessTypeConnect) SetData(any) {}

// ProcessConnect 对应 processConnect: 按包类型分发
//
// 分支与上游逐条一致 (含 STOPPED 标记的位置):
//
//	CHAT_RECEIVE            -> receiveChat (Go 侧拦截)
//	GAMECOMMAND_RECEIVE     -> receiveCommand (事件可拦截, 默认放行给引擎)
//	RELAY_118_117_RETURN    -> 先置 STOPPED 再 sendRelayServerTypeReply
//	DISCONNECT              -> disconnect 并置 STOPPED
//	其余                    -> 状态仍为 CONTINUE 时 receivePacket 转给引擎
//
// 上游用 try/catch 包住全部分发 (Log.error 后连接继续存活); Go 侧同样吞掉
// 单包错误只记日志, 不让一条坏包带走整条连接.
func (t *HeadlessTypeConnect) ProcessConnect(p *packet.Packet) error {
	con := t.con
	if con == nil || p == nil {
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			log.Errorf("[GameServer]", "processConnect panic: {0}", r)
		}
	}()

	switch packettype.PacketType(p.Type.TypeInt()) {
	case packettype.CHAT_RECEIVE:
		con.ReceiveChat(p)
	case packettype.GAMECOMMAND_RECEIVE:
		con.ReceiveCommand(p)
	case packettype.RELAY_118_117_RETURN:
		p.Status = control.STOPPED
		con.SendRelayServerTypeReply(p)
	case packettype.DISCONNECT:
		con.Disconnect()
		p.Status = control.STOPPED
	default:
	}

	if p.Status == control.CONTINUE {
		con.ReceivePacket(p)
	}
	return nil
}

// AbstractNetConnect 实现 core.TypeConnect
func (t *HeadlessTypeConnect) AbstractNetConnect() *core.AbstractNetConnect {
	if t.con == nil {
		return nil
	}
	return t.con.AbstractNetConnect
}

// Version 对应 version: "${Data.SERVER_CORE_VERSION}: 1.0.0"
func (t *HeadlessTypeConnect) Version() string { return Version() }
