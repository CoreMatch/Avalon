package server

import (
	"rwhps/internal/game"
	"rwhps/internal/net/core"
)

// AbstractNetConnectServer 对应 AbstractNetConnectServer
//
// 原注释: "Only provide interface but not implement. As the interface of game CoreNet,
// it provides various version support for GameServer"
//
// # 成员分组: 按"唯一实现者里到底有没有东西"划分
//
// 原版唯一的实现者 GameVersionServer 中, 25 个接口成员的实现密度差异极大.
// 移植前先按实现体分类, 是为了避免把空壳当成功能搬过来 —— 那会让接口看起来
// 比实际丰满, 是复刻里最隐蔽的一种造假.
//
//	有实体且纯协议 (3)   sendSystemMessage / sendChatMessage / sendKick
//	数据访问器     (6)   permissionStatus / supportedversionBeta /
//	                    supportedversionGame / supportedVersionInt / room / player
//	实现体为空     (9)   sendServerInfo / sendTeamData / sendPing / sendStartGame /
//	                    sendErrorPasswd / sendGameSave / receiveCheckPacket /
//	                    registerConnection / disconnect(非接口成员)
//	只 return 常量 (2)   getGameSaveData -> nullPacket.bytes / getPlayerInfo -> true
//	游戏语义       (4)   sendSurrender / receiveCommand / gameSummon / receiveChat
//	Relay flavour  (2)   sendRelayServerType / sendRelayServerTypeReply
//
// 本接口只声明前两组里"不依赖 PlayerHess"的部分. 其余显式延后, 理由逐条写在
// 文件末尾的延后清单里.
//
// 一处刻意的不一致: Kotlin 的 sendSystemMessage / sendChatMessage / sendKick
// 没有 @Throws, 内部 catch 掉 IOException 只打日志; Go 侧返回 error 让调用方决定.
// 静默丢包是原版的已知缺陷, 不复刻.
type AbstractNetConnectServer interface {
	// PermissionStatus 对应 permissionStatus: DataPermissionStatus.ServerStatus
	//
	// 原实现: `override var permissionStatus: ServerStatus = ServerStatus.InitialConnection`,
	// internal set —— 只有包内能改. Go 没有包内可见性, 实现方自行保证.
	PermissionStatus() core.ServerStatus

	// SupportedVersionBeta 对应 supportedversionBeta
	//
	// GameVersionServer 里恒为 false.
	SupportedVersionBeta() bool

	// SupportedVersionGame 对应 supportedversionGame
	//
	// GameVersionServer 里恒为 "1.15".
	SupportedVersionGame() string

	// SupportedVersionInt 对应 supportedVersionInt
	//
	// GameVersionServer 里恒为 176. 原版 Server-Core 内只有两处读到它:
	// Initialization.kt:299 (遥测 BaseDataSend, 已排除) 与 UpListMain.kt (插件).
	SupportedVersionInt() int

	// Room 对应 room: ServerRoom
	//
	// 这是整个接口里唯一一个"游戏侧类型已经移植好了"的成员: 原版的
	// game.room.ServerRoom 在 Go 侧对应 game.GameSession, 因此可以直接指过去.
	//
	// 依赖方向与 Kotlin 一致 (net.core.server -> game.room). 代价是 core/server
	// 现在依赖 game 包; 若将来 game 需要反向依赖本包就会成环, 届时要把 Room()
	// 拆成独立的 seam 接口.
	Room() *game.GameSession

	// SendSystemMessage 对应 sendSystemMessage(msg)
	//
	// 原实现: sendPacket(rwHps.abstractNetPacket.getSystemMessagePacket(msg))
	// 编码器一侧已移植为 netpacket.AbstractNetPacket.GetSystemMessagePacket.
	SendSystemMessage(msg string) error

	// SendChatMessage 对应 sendChatMessage(msg, sendBy, team)
	//
	// 原实现: sendPacket(rwHps.abstractNetPacket.getChatMessagePacket(msg, sendBy, team))
	SendChatMessage(msg, sendBy string, team int) error

	// SendKick 对应 sendKick(reason)
	//
	// 原实现连发两个包: PacketType.KICK (写入 reason) + PacketType.DISCONNECT
	// (写入固定字符串 "kicked"). 少发第二个包客户端不会走完断开流程.
	SendKick(reason string) error
}

// 以下成员在原版中存在, 本包刻意不声明. 逐条记录理由, 以免后来者以为漏搬.
//
// 游戏语义 (写的是 GameEngine.data.gameData.commandPacketList, 属游戏内核状态):
//
//	sendSurrender()          写入指令包列表, 且依赖 player.index
//	receiveCommand(packet)   解析 GameCommandOnePacket, 触发事件, 依赖 player / room
//	gameSummon(unit,x,y)     写入指令包列表, 依赖 player.index
//	receiveChat(packet)      聊天 + 命令分发; 其中 maxLen/muteAll/filterMessage 部分
//	                         看似可移植, 但同样依赖 player 与 Data.vote
//
// 实现体为空 (搬过来就是空方法, 零信息量):
//
//	sendServerInfo(utilData)  sendTeamData(gzip)       sendPing()
//	sendStartGame()           sendErrorPasswd()        sendGameSave(packet)
//	receiveCheckPacket(packet)                          registerConnection(packet)
//
// 只 return 常量 (同样零信息量):
//
//	getGameSaveData(packet)   return PacketType.nullPacket.bytes
//	getPlayerInfo(packet)     return true
//
// Relay flavour (属 Relay 批次):
//
//	sendRelayServerType(msg, run)      sendRelayServerTypeReply(packet)
//
// 依赖尚未移植的 PlayerHess:
//
//	player: PlayerHess
//
// 另外 AbstractNetConnectData 的 name / registerPlayerId 在 GameVersionServer 里
// 分别是 player.name 与 player.connectHexID, 同样要等 PlayerHess 落地才能有真实值.
