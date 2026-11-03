package event

import (
	"rwhps/internal/game/enums"
	"rwhps/internal/game/player"
)

// 本文件对应 net.rwhps.server.game.event.game 下的全部事件类.
//
// 上游每个事件都携带 gameModule (内核模块), 监听器经它取房间/引擎;
// Go 版监听器是闭包, 房间在注册时捕获, 事件只保留最小负载 (见包注释).
//
// @EventOnlyRead 注解 (上游标记"只读事件") 不改变运行行为, 只在注释里保留出处.

// PlayerJoinEvent 对应 PlayerJoinEvent: 玩家加入服务器事件 (@EventOnlyRead)
//
// 触发点: 上游 PlayerConnectX.a (玩家注册时); Go 版在连接层收到引擎的
// player.join 事件并完成 AddAbstractPlayer 之后.
type PlayerJoinEvent struct {
	Player *player.Player
}

// PlayerLeaveEvent 对应 PlayerLeaveEvent: 玩家离开服务器事件
//
// 触发点: 上游 GameVersionServer.disconnect; Go 版在 Disconnect 里.
type PlayerLeaveEvent struct {
	Player *player.Player
}

// PlayerChatEvent 对应 PlayerChatEvent: 玩家发送聊天消息事件 (@EventOnlyRead)
//
// 触发点: 上游 receiveChat 通过全部检查后; Message 是 filterMessage 之后的文本.
type PlayerChatEvent struct {
	Player  *player.Player
	Message string
}

// PlayerBanEvent 对应 PlayerBanEvent: 玩家被服务区封禁事件 (@EventOnlyRead)
type PlayerBanEvent struct {
	Player *player.Player
}

// PlayerUnBanEvent 对应 PlayerUnBanEvent: 玩家被服务区解禁事件
//
// 上游核心代码没有触发点 (unban 命令误发 PlayerBanEvent, 见命令移植处注释),
// 事件类本身保留, 供插件与后续修正使用.
type PlayerUnBanEvent struct {
	Player *player.Player
}

// PlayerIpBanEvent 对应 PlayerIpBanEvent: 玩家被 Ban IP 时事件
type PlayerIpBanEvent struct {
	Player *player.Player
}

// PlayerIpUnBanEvent 对应 PlayerIpUnBanEvent: 玩家被解 Ban IP 时事件
type PlayerIpUnBanEvent struct {
	Player *player.Player
}

// PlayerOperationUnitEvent 对应 PlayerOperationUnitEvent: 玩家操作单位事件
//
// 触发点: 上游 receiveCommand 解析 GameCommandOnePacket 的 operationUnit 块.
// ResultStatus 对应 @JvmField resultStatus: 监听器置 false 可拦截该操作
// (上游把包标记 STOPPED, 不再转给引擎).
type PlayerOperationUnitEvent struct {
	Player   *player.Player
	Actions  enums.GameCommandActions
	UnitName string
	X        float32
	Y        float32

	// ResultStatus 操作是否有效, 上游初值 true
	ResultStatus bool
}

// PlayerOperationFactoryBuildUnitEvent 对应 PlayerOperationFactoryBuildUnitEvent:
// 玩家工厂建造单位事件
//
// 触发点: 上游 receiveCommand 里 actionId 不以 "_" 开头时.
// 上游 ActionIdData 是按 actionId 驻留的包装类, Go 版直接用 string 表达.
type PlayerOperationFactoryBuildUnitEvent struct {
	Player   *player.Player
	ActionID string

	// ResultStatus 操作是否有效, 上游初值 true
	ResultStatus bool
}

// ServerGameStartEvent 对应 ServerGameStartEvent: 服务器开始游戏事件 (@EventOnlyRead)
//
// 触发点: 上游客户端命令 /start 的末尾 (引擎 multiplayerStart 之后).
type ServerGameStartEvent struct{}

// ServerGameOverEvent 对应 ServerGameOverEvent: 服务器结束游戏事件 (@EventOnlyRead)
//
// 触发点: 上游 ServerRoom.cleanData. GameOverData 来自引擎结算数据,
// Go 版引擎在 helper 内, 该数据尚无协议通道 (DECISIONS D-9), 允许为 nil ——
// 与上游 `gameOverData` 可为 null 的声明一致.
type ServerGameOverEvent struct {
	GameOverData *GameOverData
}

// GameOverData 对应 ServerGameOverEvent.GameOverData
type GameOverData struct {
	// GameTime 对应 gameTime
	GameTime int
	// AllPlayerList 对应 allPlayerList
	AllPlayerList []string
	// WinPlayerList 对应 winPlayerList
	WinPlayerList []string
	// MapName 对应 mapName
	MapName string
	// PlayerData 对应 playerData: 玩家名 -> 战绩键值表
	PlayerData map[string]map[string]int
	// ReplayName 对应 replayName
	ReplayName string
}

// ServerHessStartPort 对应 ServerHessStartPort: 无头端开启端口监听事件
//
// 触发点: 上游 LinkGameNet (引擎网络层开端口); Go 版在 start 命令成功、
// 游戏端口开放之后.
type ServerHessStartPort struct{}
