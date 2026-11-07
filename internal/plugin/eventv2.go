package plugin

import (
	"time"
)

// v2 事件目录 (契约 avalon-plugin-v1 v1.2.0, 三档: observe / veto / deliver).
//
// 档位语义:
//   - deliver: WAL 持久 + 重试 + 至少一次 (仅 game.over, v1 既有)
//   - observe: 尽力投递 —— 内存队列、满则丢最旧、失败不重试、重启不重放
//     (观察类事件过期即无价值, 不做持久化)
//   - veto:    同步准入, 有界超时, 超时/错误 fail-open (room.create 为 v1 既有;
//     v2 新增 player.join / unit.operation / factory.build / connection.new /
//     game.start, 后五者须运营授权)
const (
	EventPlayerJoin     = "player.join"
	EventPlayerLeave    = "player.leave"
	EventPlayerBanned   = "player.banned"
	EventPlayerUnbanned = "player.unbanned"
	EventPlayerIpBanned = "player.ipbanned"
	EventGameStart      = "game.start"
	EventPlayerChat     = "player.chat"
	EventUnitOperation  = "unit.operation"
	EventFactoryBuild   = "factory.build"

	// EventConnectionNew 连接级准入 (v1.2.0): 首包时刻、协议状态之前咨询.
	// 仅 veto 档 (无观察模式): 载荷只有连接层信息, 否决 = 裸断连,
	// 理由只进服务端日志 (对端尚未进入协议, 无呈现通道).
	EventConnectionNew = "connection.new"
	// EventPlayerKicked 踢出审计 (v1.2.0): observe 档, 单一踢出咽喉点
	// (player.KickPlayer) 发射, 携带理由与重进锁定时长.
	EventPlayerKicked = "player.kicked"
)

// allEventNames 是协商与投递共用的事件全集 (版本协商逐个事件取交集)
var allEventNames = []string{
	EventRoomCreate,
	EventGameOver,
	EventPlayerJoin,
	EventPlayerLeave,
	EventPlayerBanned,
	EventPlayerUnbanned,
	EventPlayerIpBanned,
	EventGameStart,
	EventPlayerChat,
	EventUnitOperation,
	EventFactoryBuild,
	EventConnectionNew,
	EventPlayerKicked,
}

// vetoGrantEvents 是需要运营显式授权才可进入否决模式的事件.
// room.create 不在其中: 它沿用 v1 语义 (订阅即参与准入), 保持零回归.
var vetoGrantEvents = map[string]bool{
	EventPlayerJoin:    true,
	EventUnitOperation: true,
	EventFactoryBuild:  true,
	EventConnectionNew: true,
	EventGameStart:     true,
}

// Tier 是事件投递档位
type Tier int

const (
	// TierDeliver WAL 持久 + 重试 + 至少一次
	TierDeliver Tier = iota
	// TierObserve 尽力投递, 失败即弃
	TierObserve
)

// tierFor 返回事件的投递档位; 否决调用不入队, 不在此列
func tierFor(eventName string) Tier {
	if eventName == EventGameOver {
		return TierDeliver
	}
	return TierObserve
}

// PlayerInfo 是契约 PlayerInfo 段: Avalon 视角的玩家身份.
//
// 对局内数据 (击杀/经济/位置/存活) 属引擎内部, 不在契约内 (见
// knowledge/0007 的 X 类判断), 这里不设占位字段.
type PlayerInfo struct {
	Name      string `json:"name"`
	UUID      string `json:"uuid"`
	Team      int    `json:"team"`
	Slot      int    `json:"slot"`
	Host      bool   `json:"host"`
	Bot       bool   `json:"bot"`
	Spectator bool   `json:"spectator"`
	Admin     bool   `json:"admin"`
	Address   string `json:"address"`
}

// roomRef 契约 RoomRef 段
type roomRef struct {
	Kind string `json:"kind"`
	Code string `json:"code"`
}

func serverRoomRef(code string) roomRef {
	return roomRef{Kind: KindServer, Code: code}
}

// playerJoinEvent 契约 PlayerJoinEvent (观察与否决共用同一载荷形状)
type playerJoinEvent struct {
	EventID      string      `json:"eventId"`
	Event        string      `json:"event"`
	EventVersion int         `json:"eventVersion"`
	OccurredAt   string      `json:"occurredAt"`
	Server       eventServer `json:"server"`
	Room         roomRef     `json:"room"`
	Player       PlayerInfo  `json:"player"`
}

func buildPlayerJoinEvent(roomCode string, info PlayerInfo, now time.Time) *playerJoinEvent {
	return &playerJoinEvent{
		EventID:      newEventID(now),
		Event:        EventPlayerJoin,
		EventVersion: emitVersion,
		OccurredAt:   now.UTC().Format(time.RFC3339Nano),
		Server:       currentServer(),
		Room:         serverRoomRef(roomCode),
		Player:       info,
	}
}

// playerLeaveEvent 契约 PlayerLeaveEvent
type playerLeaveEvent struct {
	EventID      string      `json:"eventId"`
	Event        string      `json:"event"`
	EventVersion int         `json:"eventVersion"`
	OccurredAt   string      `json:"occurredAt"`
	Server       eventServer `json:"server"`
	Room         roomRef     `json:"room"`
	Player       PlayerInfo  `json:"player"`
	// BanRemainingMs 重进锁定剩余毫秒; >0 表示带着踢/封窗口离开.
	// 断开原因 (自愿/被踢/网络断开) 取不到, 契约不携带.
	BanRemainingMs int64 `json:"banRemainingMs"`
	// InGame 离开时房间是否在对局中
	InGame bool `json:"inGame"`
}

func buildPlayerLeaveEvent(roomCode string, info PlayerInfo, banRemainingMs int64, inGame bool, now time.Time) *playerLeaveEvent {
	return &playerLeaveEvent{
		EventID:        newEventID(now),
		Event:          EventPlayerLeave,
		EventVersion:   emitVersion,
		OccurredAt:     now.UTC().Format(time.RFC3339Nano),
		Server:         currentServer(),
		Room:           serverRoomRef(roomCode),
		Player:         info,
		BanRemainingMs: banRemainingMs,
		InGame:         inGame,
	}
}

// playerActionEvent 契约 PlayerActionEvent (banned/unbanned/ipbanned 同形)
type playerActionEvent struct {
	EventID      string      `json:"eventId"`
	Event        string      `json:"event"`
	EventVersion int         `json:"eventVersion"`
	OccurredAt   string      `json:"occurredAt"`
	Server       eventServer `json:"server"`
	Room         roomRef     `json:"room"`
	Player       PlayerInfo  `json:"player"`
}

func buildPlayerActionEvent(eventName, roomCode string, info PlayerInfo, now time.Time) *playerActionEvent {
	return &playerActionEvent{
		EventID:      newEventID(now),
		Event:        eventName,
		EventVersion: emitVersion,
		OccurredAt:   now.UTC().Format(time.RFC3339Nano),
		Server:       currentServer(),
		Room:         serverRoomRef(roomCode),
		Player:       info,
	}
}

// MatchInfo 契约 game.start 的 match 段: 开局时刻的房间快照
type MatchInfo struct {
	MapName    string       `json:"mapName"`
	MapPlayer  string       `json:"mapPlayer"`
	MaxPlayers int          `json:"maxPlayers"`
	StartTime  int64        `json:"startTime"`
	Players    []PlayerInfo `json:"players"`
}

// gameStartEvent 契约 GameStartEvent
type gameStartEvent struct {
	EventID      string      `json:"eventId"`
	Event        string      `json:"event"`
	EventVersion int         `json:"eventVersion"`
	OccurredAt   string      `json:"occurredAt"`
	Server       eventServer `json:"server"`
	Room         roomRef     `json:"room"`
	Match        MatchInfo   `json:"match"`
}

func buildGameStartEvent(roomCode string, match MatchInfo, now time.Time) *gameStartEvent {
	if match.Players == nil {
		match.Players = []PlayerInfo{}
	}
	return &gameStartEvent{
		EventID:      newEventID(now),
		Event:        EventGameStart,
		EventVersion: emitVersion,
		OccurredAt:   now.UTC().Format(time.RFC3339Nano),
		Server:       currentServer(),
		Room:         serverRoomRef(roomCode),
		Match:        match,
	}
}

// playerChatEvent 契约 PlayerChatEvent (消息为内置过滤后的文本)
type playerChatEvent struct {
	EventID      string      `json:"eventId"`
	Event        string      `json:"event"`
	EventVersion int         `json:"eventVersion"`
	OccurredAt   string      `json:"occurredAt"`
	Server       eventServer `json:"server"`
	Room         roomRef     `json:"room"`
	Player       PlayerInfo  `json:"player"`
	Message      string      `json:"message"`
}

func buildPlayerChatEvent(roomCode string, info PlayerInfo, message string, now time.Time) *playerChatEvent {
	return &playerChatEvent{
		EventID:      newEventID(now),
		Event:        EventPlayerChat,
		EventVersion: emitVersion,
		OccurredAt:   now.UTC().Format(time.RFC3339Nano),
		Server:       currentServer(),
		Room:         serverRoomRef(roomCode),
		Player:       info,
		Message:      message,
	}
}

// unitOperationEvent 契约 UnitOperationEvent
type unitOperationEvent struct {
	EventID      string      `json:"eventId"`
	Event        string      `json:"event"`
	EventVersion int         `json:"eventVersion"`
	OccurredAt   string      `json:"occurredAt"`
	Server       eventServer `json:"server"`
	Room         roomRef     `json:"room"`
	Player       PlayerInfo  `json:"player"`
	Action       string      `json:"action"`
	UnitName     string      `json:"unitName"`
	X            float32     `json:"x"`
	Y            float32     `json:"y"`
}

func buildUnitOperationEvent(roomCode string, info PlayerInfo, action, unitName string, x, y float32, now time.Time) *unitOperationEvent {
	return &unitOperationEvent{
		EventID:      newEventID(now),
		Event:        EventUnitOperation,
		EventVersion: emitVersion,
		OccurredAt:   now.UTC().Format(time.RFC3339Nano),
		Server:       currentServer(),
		Room:         serverRoomRef(roomCode),
		Player:       info,
		Action:       action,
		UnitName:     unitName,
		X:            x,
		Y:            y,
	}
}

// factoryBuildEvent 契约 FactoryBuildEvent
type factoryBuildEvent struct {
	EventID      string      `json:"eventId"`
	Event        string      `json:"event"`
	EventVersion int         `json:"eventVersion"`
	OccurredAt   string      `json:"occurredAt"`
	Server       eventServer `json:"server"`
	Room         roomRef     `json:"room"`
	Player       PlayerInfo  `json:"player"`
	ActionID     string      `json:"actionId"`
}

func buildFactoryBuildEvent(roomCode string, info PlayerInfo, actionID string, now time.Time) *factoryBuildEvent {
	return &factoryBuildEvent{
		EventID:      newEventID(now),
		Event:        EventFactoryBuild,
		EventVersion: emitVersion,
		OccurredAt:   now.UTC().Format(time.RFC3339Nano),
		Server:       currentServer(),
		Room:         serverRoomRef(roomCode),
		Player:       info,
		ActionID:     actionID,
	}
}

// connectionNewEvent 契约 ConnectionNewEvent (v1.2.0): 仅连接层信息.
// 首包时刻尚无协议状态与玩家身份, 载荷不设占位字段; 也不带 room 段 ——
// 连接此时不归属任何房间.
type connectionNewEvent struct {
	EventID      string      `json:"eventId"`
	Event        string      `json:"event"`
	EventVersion int         `json:"eventVersion"`
	OccurredAt   string      `json:"occurredAt"`
	Server       eventServer `json:"server"`
	Connection   struct {
		Address   string `json:"address"`
		IpLong24  string `json:"ipLong24"`
		Transport string `json:"transport"`
	} `json:"connection"`
}

func buildConnectionNewEvent(address, ipLong24, transport string, now time.Time) *connectionNewEvent {
	e := &connectionNewEvent{
		EventID:      newEventID(now),
		Event:        EventConnectionNew,
		EventVersion: emitVersion,
		OccurredAt:   now.UTC().Format(time.RFC3339Nano),
		Server:       currentServer(),
	}
	e.Connection.Address = address
	e.Connection.IpLong24 = ipLong24
	e.Connection.Transport = transport
	return e
}

// playerKickedEvent 契约 PlayerKickedEvent (v1.2.0, observe 审计档)
type playerKickedEvent struct {
	EventID      string      `json:"eventId"`
	Event        string      `json:"event"`
	EventVersion int         `json:"eventVersion"`
	OccurredAt   string      `json:"occurredAt"`
	Server       eventServer `json:"server"`
	Room         roomRef     `json:"room"`
	Player       PlayerInfo  `json:"player"`
	// Reason 踢出文案 (发射前经 sanitizeReason 净化)
	Reason string `json:"reason"`
	// DurationSec 重进锁定秒数; 0 = 只踢不锁
	DurationSec int `json:"durationSec"`
}

func buildPlayerKickedEvent(roomCode string, info PlayerInfo, reason string, durationSec int, now time.Time) *playerKickedEvent {
	return &playerKickedEvent{
		EventID:      newEventID(now),
		Event:        EventPlayerKicked,
		EventVersion: emitVersion,
		OccurredAt:   now.UTC().Format(time.RFC3339Nano),
		Server:       currentServer(),
		Room:         serverRoomRef(roomCode),
		Player:       info,
		Reason:       sanitizeReason(reason),
		DurationSec:  durationSec,
	}
}
