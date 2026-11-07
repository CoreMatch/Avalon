package plugin

import (
	"crypto/rand"
	"time"

	"github.com/CoreMatch/Avalon/internal/data/global"
	"github.com/CoreMatch/Avalon/internal/game/event"
)

// 契约事件名与版本 (avalon-plugin-v1.yaml)
const (
	EventRoomCreate = "room.create"
	EventGameOver   = "game.over"

	// emitVersion v1 中 Avalon 对两个事件都只发版本 1
	emitVersion = 1

	// ProtocolV1 协议标识, manifest 必须精确匹配
	ProtocolV1 = "avalon-plugin/v1"
	// ManifestSchema manifest 的 schema 字段值
	ManifestSchema = "avalon-plugin/manifest"
)

// 建房种类 (room.create 的 room.kind)
const (
	KindServer  = "server"  // 启动期单例房间
	KindCascade = "cascade" // Cascade 控制面 POST /v1/rooms/create
	KindRelay   = "relay"   // relay 协议建房
)

// ulidEncoding Crockford base32 (不含 I L O U)
const ulidEncoding = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// newEventID 生成 "evt_" + 26 字符 ULID (48bit 毫秒时间 + 80bit 随机).
//
// 时间前缀保证字典序 ≈ 时间序; 随机部分来自 crypto/rand. 生成一次即固定,
// 重试与重放复用同一 ID (插件据此去重).
func newEventID(now time.Time) string {
	var b [26]byte

	ms := now.UnixMilli()
	for i := 9; i >= 0; i-- {
		b[i] = ulidEncoding[ms&31]
		ms >>= 5
	}

	var rnd [10]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		// crypto/rand 失败只在系统熵池异常时发生; 用时间戳填充保持可用
		// (唯一性由时间前缀与低碰撞概率兜底, 事件 ID 不是安全边界).
		for i := range rnd {
			rnd[i] = byte(now.UnixNano() >> (i * 3))
		}
	}
	// 10 字节 = 80bit = 16 个 5bit 组, 与 ULID 规范的随机段编码一致
	b[10] = ulidEncoding[(rnd[0]&248)>>3]
	b[11] = ulidEncoding[((rnd[0]&7)<<2)|((rnd[1]&192)>>6)]
	b[12] = ulidEncoding[(rnd[1]&62)>>1]
	b[13] = ulidEncoding[((rnd[1]&1)<<4)|((rnd[2]&240)>>4)]
	b[14] = ulidEncoding[((rnd[2]&15)<<1)|((rnd[3]&128)>>7)]
	b[15] = ulidEncoding[(rnd[3]&124)>>2]
	b[16] = ulidEncoding[((rnd[3]&3)<<3)|((rnd[4]&224)>>5)]
	b[17] = ulidEncoding[rnd[4]&31]
	b[18] = ulidEncoding[(rnd[5]&248)>>3]
	b[19] = ulidEncoding[((rnd[5]&7)<<2)|((rnd[6]&192)>>6)]
	b[20] = ulidEncoding[(rnd[6]&62)>>1]
	b[21] = ulidEncoding[((rnd[6]&1)<<4)|((rnd[7]&240)>>4)]
	b[22] = ulidEncoding[((rnd[7]&15)<<1)|((rnd[8]&128)>>7)]
	b[23] = ulidEncoding[(rnd[8]&124)>>2]
	b[24] = ulidEncoding[((rnd[8]&3)<<3)|((rnd[9]&224)>>5)]
	b[25] = ulidEncoding[rnd[9]&31]

	return "evt_" + string(b[:])
}

// eventServer 信封里的服务器段
type eventServer struct {
	ServerID    string `json:"serverId"`
	ServerName  string `json:"serverName"`
	CoreVersion string `json:"coreVersion"`
}

// currentServer 在发出时刻读取服务器身份字段
func currentServer() eventServer {
	return eventServer{
		ServerID:    global.ConfigServer.ServerID,
		ServerName:  global.Config.ServerName,
		CoreVersion: global.ServerCoreVersion,
	}
}

// RoomCreateRequest 是治理面对建房准入的输入 (各建房路径就地构造)
type RoomCreateRequest struct {
	// Kind 建房种类: KindServer / KindCascade / KindRelay
	Kind string
	// Code 房间码: server=ServerID; cascade=请求的码; relay=自定义码,
	// 自动生成时为空串
	Code string
	// MaxPlayers 请求的最大人数
	MaxPlayers int
	// IsMod relay mod 房标志 (非 relay 恒 false)
	IsMod bool
	// CreatorName server="console"; cascade="cascade"; relay=建房玩家名
	CreatorName string
	// CreatorAddress 来源 IP (未知为空)
	CreatorAddress string
}

// roomCreateEvent 契约 RoomCreateEvent 载荷
type roomCreateEvent struct {
	EventID      string      `json:"eventId"`
	Event        string      `json:"event"`
	EventVersion int         `json:"eventVersion"`
	OccurredAt   string      `json:"occurredAt"`
	Server       eventServer `json:"server"`
	Room         struct {
		Kind       string `json:"kind"`
		Code       string `json:"code"`
		MaxPlayers int    `json:"maxPlayers"`
		IsMod      bool   `json:"isMod"`
	} `json:"room"`
	Creator struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	} `json:"creator"`
}

// buildRoomCreateEvent 构造建房准入载荷 (eventId 在调用时生成一次,
// 并行发给各插件的是同一份正文)
func buildRoomCreateEvent(req RoomCreateRequest, now time.Time) *roomCreateEvent {
	e := &roomCreateEvent{
		EventID:      newEventID(now),
		Event:        EventRoomCreate,
		EventVersion: emitVersion,
		OccurredAt:   now.UTC().Format(time.RFC3339Nano),
		Server:       currentServer(),
	}
	e.Room.Kind = req.Kind
	e.Room.Code = req.Code
	e.Room.MaxPlayers = req.MaxPlayers
	e.Room.IsMod = req.IsMod
	e.Creator.Name = req.CreatorName
	e.Creator.Address = req.CreatorAddress
	return e
}

// gameOverEvent 契约 GameOverEvent 载荷
type gameOverEvent struct {
	EventID      string      `json:"eventId"`
	Event        string      `json:"event"`
	EventVersion int         `json:"eventVersion"`
	OccurredAt   string      `json:"occurredAt"`
	Server       eventServer `json:"server"`
	Room         struct {
		Kind string `json:"kind"`
		Code string `json:"code"`
	} `json:"room"`
	// Result 引擎未产出结算数据时为 null (对局结束这一事实仍然投递)
	Result *gameOverResult `json:"result"`
}

// gameOverResult 契约 result 段, 字段与 event.GameOverData 一一对应
type gameOverResult struct {
	MapName       string                    `json:"mapName"`
	GameTime      int                       `json:"gameTime"`
	AllPlayerList []string                  `json:"allPlayerList"`
	WinPlayerList []string                  `json:"winPlayerList"`
	PlayerData    map[string]map[string]int `json:"playerData"`
	ReplayName    string                    `json:"replayName"`
}

// buildGameOverEvent 构造对局结束载荷; data 为 nil 时 result=null
func buildGameOverEvent(roomCode string, data *event.GameOverData, now time.Time) *gameOverEvent {
	e := &gameOverEvent{
		EventID:      newEventID(now),
		Event:        EventGameOver,
		EventVersion: emitVersion,
		OccurredAt:   now.UTC().Format(time.RFC3339Nano),
		Server:       currentServer(),
	}
	e.Room.Kind = KindServer
	e.Room.Code = roomCode
	if data != nil {
		e.Result = &gameOverResult{
			MapName:       data.MapName,
			GameTime:      data.GameTime,
			AllPlayerList: data.AllPlayerList,
			WinPlayerList: data.WinPlayerList,
			PlayerData:    data.PlayerData,
			ReplayName:    data.ReplayName,
		}
	}
	return e
}
