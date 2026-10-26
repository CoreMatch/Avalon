package server

import (
	"rwhps/internal/game/room"
	"rwhps/internal/io/packet"
	"rwhps/internal/net/core"
)

// AbstractNetConnectRelay 对应 AbstractNetConnectRelay
//
// 原注释: "Only provide interface but not implement. As the interface of game CoreNet,
// it provides various version support for GameRelay"
//
// # 与 AbstractNetConnectServer 的取舍不同: 这里 24 个成员全部声明
//
// 同包的 Server 风味接口只声明了 9 个成员, 因为它的唯一实现者 GameVersionServer
// 里 25 个成员的实现密度差异极大 —— 9 个是空壳, 2 个只 return 常量, 4 个写游戏内核
// 状态. Relay 风味相反: 唯一实现者 GameVersionRelay (1061 行) 对 24 个接口成员
// **全部有实体实现**, 没有空壳也没有常量返回, 因此不做裁剪.
//
// 裁剪的依据始终是实现体的函数体, 不是接口签名 —— 这一点在 Server 风味上已经验证过:
// 照签名搬运会把 12 个"看起来是协议管线"的成员搬成 9 个空方法.
//
// # 命名
//
// 沿用本包已确立的约定: 去掉 get 前缀 (原版 getPingData / getRelayT4 在这里是
// **发送动作**而非访问器, 但同样去掉). permissionStatus / room 这两个 Kotlin 属性
// 落成访问器方法 PermissionStatus() / Room(), 因为 Go 的接口不能有字段.
//
// # 与原版的三处系统性差异
//
//   - 受检异常: 原版 relayDirectInspection / receiveChat / receiveCommand 三个成员
//     带 @Throws(IOException::class), 其余成员在函数体内 catch 掉只打日志. Go 侧
//     给这三个加 error 返回值, 与 AbstractNetConnectServer 的 SendXxx 一致 ——
//     让调用方决定, 而不是静默丢包.
//   - 默认参数: Kotlin 的 relayDirectInspection(relayRoom = null) /
//     sendRelayServerType(run = null) / sendRelayServerId(multicast = false) 在 Go 里
//     没有对应物, 统一改成显式参数, 调用方传 nil / false. 没有拆成一对重载方法,
//     是因为那会强制实现方写两个几乎相同的函数体.
//   - AbstractNetConnectData 的成员 (name / registerPlayerId / betaGameVersion /
//     clientVersion) 不在本接口重复声明. 原版 GameVersionRelay 同时实现两个接口,
//     Go 侧同样由实现方同时满足 AbstractNetConnectData 与本接口.
//
// # 不在本接口内的成员
//
// disconnect() 不属于本接口 —— 它在原版属于父类 AbstractNetConnect, 由
// AbstractNetConnectData 那一侧的继承链提供. site / cachePacket / playerRelay 等
// 是 GameVersionRelay 自己的属性, 不是接口成员.
type AbstractNetConnectRelay interface {
	// PermissionStatus 对应 permissionStatus: DataPermissionStatus.RelayStatus
	//
	// 原实现: `override var permissionStatus: RelayStatus = RelayStatus.InitialConnection`,
	// internal set. Go 没有包内可见性, 由实现方自行保证只有协议层能推进状态.
	PermissionStatus() core.RelayStatus

	// Room 对应 room: RelayRoom?
	//
	// 原实现 protected set, 只有 relayDirectInspection 会写入 (拿到 relayRoom 参数时).
	// 返回 nil 对应 Kotlin 的 null —— 连接尚未加入任何房间.
	Room() *room.RelayRoom

	// SetCachePacket 对应 setCachePacket(packet)
	//
	// 原实现就是一句 `cachePacket = packet`. 这个缓存是 relayDirectInspection 的输入:
	// 后者第一行就是 `GameInputStream(cachePacket!!)`, 所以 SetCachePacket 必须
	// 在 RelayDirectInspection 之前被调用, 否则原版直接 NPE.
	SetCachePacket(p *packet.Packet)

	// SetLastSentPacket 对应 setlastSentPacket(packet) (原版方法名是小写的 l)
	//
	// 原实现是空方法, 注释写明"此协议下不被使用". 保留声明是因为接口要求它 ——
	// Server 风味的 GameVersionServer 用同一个方法做重连时的包缓存, 两种风味
	// 共用一套 ConnectionAgreement 之上的接口形状.
	SetLastSentPacket(p *packet.Packet)

	// SendRelayServerInfo 对应 sendRelayServerInfo()
	//
	// 原实现从 Cache.packetCache 取 (没有则构造) relayServerInitInfoInternalPacket
	// 再发出去. 对应协议里的服务端自我介绍.
	SendRelayServerInfo()

	// RelayDirectInspection 对应 relayDirectInspection(relayRoom = null)
	//
	// "Inspection ID Direct join Server". 从 cachePacket 里读出协议版本 / 客户端版本 /
	// 查询串 / 玩家名, 然后按 relayRoom 是否为 nil 分两条路:
	//   - nil:    空查询串或 "RELAYCN" 时回送服务端类型列表 (sendRelayServerType),
	//	            否则按自定义 ID 走 idCustom
	//   - 非 nil: 记录房间并 addRelayConnect
	//
	// 带 error: 原版 @Throws(IOException::class).
	RelayDirectInspection(relayRoom *room.RelayRoom) error

	// RelayRegisterConnection 对应 relayRegisterConnection(packet)
	RelayRegisterConnection(p *packet.Packet)

	// SendVerifyClientValidity 对应 sendVerifyClientValidity()
	//
	// "Check if Relay's checksum is correct - send". 取一个新的 PoW 挑战
	// (RelayRoom.randPow) 并按 authenticateType 分支出不同字段写进
	// PacketType.RELAY_POW 包. authenticateType 是 Kotlin Byte, 2..4 / 5..6
	// 的区间判断依赖它的**有符号**值.
	SendVerifyClientValidity()

	// ReceiveVerifyClientValidity 对应 receiveVerifyClientValidity(packet)
	//
	// "Check whether the check of Relay is correct - check". 校验失败返回 false,
	// 且**不**区分"没有挑战"与"挑战不匹配"—— 原版两种情况都走同一个 false,
	// 由上层决定是否断开.
	ReceiveVerifyClientValidity(p *packet.Packet) bool

	// SendRelayServerType 对应 sendRelayServerType(msg, run = null)
	//
	// "Server type". 发送服务端类型列表, 并把 run 存起来等客户端的回复
	// (sendRelayServerTypeReply 时回调). run 为 nil 表示不需要回调.
	SendRelayServerType(msg string, run func(string))

	// SendRelayServerTypeReply 对应 sendRelayServerTypeReply(packet)
	//
	// "Type Reply". 解析客户端选中的服务端 ID: 有回调就走回调, 没有则 idCustom.
	SendRelayServerTypeReply(p *packet.Packet)

	// SendRelayServerId 对应 sendRelayServerId(multicast = false)
	//
	// "Set up RELAY HOST".
	SendRelayServerId(multicast bool)

	// ReceiveChat 对应 receiveChat(packet)
	//
	// "Accept language pack". 未完成注册的连接直接忽略; 房间全员禁言或本连接被禁言
	// 时同样忽略. 以 "." / "-" 开头的消息走 RELAY_COMMAND 命令处理器, 其余转给 HOST.
	//
	// 带 error: 原版 @Throws(IOException::class).
	ReceiveChat(p *packet.Packet) error

	// ReceiveCommand 对应 receiveCommand(packet)
	//
	// 原版函数体只有一句 sendPackageToHOST(packet), 但带 @Throws —— 因为它转发的是
	// 可能抛 IOException 的调用. Go 侧同样返回 error.
	ReceiveCommand(p *packet.Packet) error

	// RelayT4 对应 getRelayT4(msg)
	//
	// "Send a message". 原实现 sendPacket(chatUserMessagePacketInternal(msg)),
	// 即转发一条用户聊天消息. 名字里的 T4 是原版的遗留命名, 无实际含义.
	RelayT4(msg string)

	// PingData 对应 getPingData(packet)
	//
	// "Return Ping Packet". 原实现把来包的前 8 字节原样搬回, 再写一个 0 字节
	// (注释: 将协议版本降低到 0, 来减少发送一个字节), 以 HEART_BEAT_RESPONSE 发出.
	PingData(p *packet.Packet)

	// AddGroup 对应 addGroup(packet)
	//
	// "Group Package".
	AddGroup(p *packet.Packet)

	// AddGroupPing 对应 addGroupPing(packet)
	//
	// "Group Ping Package".
	AddGroupPing(p *packet.Packet)

	// AddRelayConnect 对应 addRelayConnect()
	//
	// "Connect to Relay server".
	AddRelayConnect()

	// AddReRelayConnect 对应 addReRelayConnect()
	//
	// "Reconnect to Relay server".
	AddReRelayConnect()

	// AddRelaySend 对应 addRelaySend(packet)
	//
	// "Parse the package".
	AddRelaySend(p *packet.Packet)

	// SendPackageToHOST 对应 sendPackageToHOST(packet)
	//
	// 把包转发给房主. 这是 Relay 转发的核心路径, receiveChat / receiveCommand /
	// multicastAnalysis 最终都落到这里.
	SendPackageToHOST(p *packet.Packet)

	// RelayPlayerDisconnect 对应 relayPlayerDisconnect()
	//
	// "Send RELAY to HOST Player disconnected".
	RelayPlayerDisconnect()

	// MulticastAnalysis 对应 multicastAnalysis(packet)
	//
	// "Multicast Multiplexing".
	MulticastAnalysis(p *packet.Packet)
}
