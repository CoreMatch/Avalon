package gateway

import (
	"errors"
	"time"

	"rwhps/internal/data/global"
	"rwhps/internal/game"
	"rwhps/internal/game/event"
	"rwhps/internal/game/manage"
	"rwhps/internal/game/player"
	"rwhps/internal/game/room"
	"rwhps/internal/util/log"
	"rwhps/internal/util/timeutil"
)

// 契约 "Room snapshot" (Cascade-Backend-v1.md :103-135) 与 OpenAPI
// RoomSummary/RoomDetail 的 Go 镜像. 单房间后端 (方案 B, DECISIONS D-20):
// 快照恒返回唯一房间.

// 门面错误, 由控制面映射成契约错误码
var (
	// ErrRoomNotFound 房间码不是本后端的唯一房间 (ROOM_NOT_FOUND)
	ErrRoomNotFound = errors.New("gateway: room not found")
	// ErrRoomConflict 请求创建别的房间码 (ROOM_CONFLICT, 单房间后端)
	ErrRoomConflict = errors.New("gateway: room conflict")
	// ErrPlayerNotFound playerId 不在房间内
	ErrPlayerNotFound = errors.New("gateway: player not found")
)

// RoomSummary 对应契约 RoomSummary (openapi RoomSummary schema)
type RoomSummary struct {
	Code       string    `json:"code"`
	Title      string    `json:"title"`
	MapName    string    `json:"mapName"`
	MaxPlayers int       `json:"maxPlayers"`
	Players    int       `json:"players"`
	InGame     bool      `json:"inGame"`
	OwnerID    string    `json:"ownerId"`
	OwnerName  string    `json:"ownerName"`
	AdminCount int       `json:"adminCount"`
	CreatedAt  time.Time `json:"createdAt"`

	// Members 仅房间详情 (GET /v1/rooms/{code}) 携带.
	// 用指针区分"快照不提供"(nil, 省略) 与"详情里为空房间"(&[]{}，序列化为 []),
	// 契约 RoomDetail 要求 members 恒存在.
	Members *[]RoomMember `json:"members,omitempty"`
}

// RoomMember 对应 Cascade backend.RoomPlayer (internal/backend/client.go)
type RoomMember struct {
	PlayerID string `json:"playerId"`
	Name     string `json:"name"`
	Slot     int    `json:"slot"`
	IsOwner  bool   `json:"isOwner"`
	IsAdmin  bool   `json:"isAdmin"`
	IsAI     bool   `json:"isAi"`
}

// RoomSettings 对应契约房间设置更新体 (openapi updateRoomSettings,
// 字段与 Cascade api.roomSettingsRequest 一致).
//
// 有意修正（作者已批准，2026-11-04）: 引擎侧字段经 v2-ext room.settings
// 作用于 helper 内的 gameLinkServerData (DECISIONS D-20).
type RoomSettings struct {
	Title         *string  `json:"title"`
	MapName       *string  `json:"mapName"`
	Credits       *int32   `json:"credits"`
	FogOfWar      *int32   `json:"fogOfWar"`
	Income        *float32 `json:"income"`
	DisableNuke   *bool    `json:"disableNuke"`
	SharedControl *bool    `json:"sharedControl"`
	StartingUnits *int32   `json:"startingUnits"`
	MaxUnits      *int     `json:"maxUnits"`
}

// defaultRoom 取单例房间; 未装配时返回错误 (503/500 语义由调用方定)
func defaultRoom() (*room.ServerRoom, error) {
	r := room.Default()
	if r == nil {
		return nil, errors.New("gateway: default room not available")
	}
	return r, nil
}

// buildSummary 按契约字段从房间现状构造快照
//
// 字段取值 (单房间语义, DECISIONS D-20):
//   - code: 配置的房间码; title: 服务器名 (上游无房间标题, 以服务器名代之)
//   - mapName/players/inGame/maxPlayers: 房间状态壳与玩家列表
//   - owner: Data().Host 为真的玩家; 无房主时留空 (全 AI 局)
//   - createdAt: 房间装配时间 (ServerRoom.CreatedAt)
func buildSummary(r *room.ServerRoom, code string, withMembers bool) *RoomSummary {
	snap := r.Snapshot()
	group := r.PlayerManage.PlayerGroup

	s := &RoomSummary{
		Code:       code,
		Title:      global.Config.ServerName,
		MapName:    snap.MapName,
		MaxPlayers: snap.MaxPlayer,
		Players:    group.Size(),
		InGame:     snap.IsStartGame,
		CreatedAt:  r.CreatedAt,
	}

	var members []RoomMember
	group.EachAll(func(p *player.Player) {
		if p.IsAdmin {
			s.AdminCount++
		}
		if p.Data().Host && s.OwnerID == "" {
			s.OwnerID = p.ConnectHexID()
			s.OwnerName = p.Name()
		}
		if withMembers {
			members = append(members, RoomMember{
				PlayerID: p.ConnectHexID(),
				Name:     p.Name(),
				Slot:     p.Index(),
				IsOwner:  p.Data().Host,
				IsAdmin:  p.IsAdmin,
				IsAI:     p.Data().Bot,
			})
		}
	})
	if withMembers {
		if members == nil {
			members = []RoomMember{}
		}
		s.Members = &members
	}
	return s
}

// roomSummary 契约 GET /v1/rooms 与 /v1/rooms/{code} 的数据源
func (g *Gateway) roomSummary(withMembers bool) (*RoomSummary, error) {
	r, err := defaultRoom()
	if err != nil {
		return nil, err
	}
	return buildSummary(r, g.cfg.RoomCode, withMembers), nil
}

// findPlayer 按 playerId (connectHexID, 即 JWT sub 的游戏 UUID) 查玩家
func findPlayer(r *room.ServerRoom, playerID string) *player.Player {
	found, ok := r.PlayerManage.PlayerAll.Find(func(p *player.Player) bool {
		return p.ConnectHexID() == playerID
	})
	if !ok {
		return nil
	}
	return found
}

// kickPlayer 对应门面 POST /v1/rooms/{code}/kick: 复用控制台 kick 语义
// (KickTime 记录 + KickPlayer 发包; servercommands.go 的 kick 同款)
func (g *Gateway) kickPlayer(code, playerID string) error {
	if code != g.cfg.RoomCode {
		return ErrRoomNotFound
	}
	r, err := defaultRoom()
	if err != nil {
		return err
	}
	p := findPlayer(r, playerID)
	if p == nil {
		return ErrPlayerNotFound
	}
	// 与控制台 kick 默认时长一致 (60 秒禁止重进)
	p.KickTime = timeutil.GetTimeFutureMillis(60 * 1000)
	p.KickPlayer("Cascade kick", 0)
	return nil
}

// banPlayer 对应门面 POST /v1/rooms/{code}/ban: 复用 D-10 修正后的
// 封禁语义 —— 对目标触发 PlayerBanEvent, 默认监听完成封禁记录与踢出
// (servercommands.go 的 ban 同款).
func (g *Gateway) banPlayer(code, playerID string) error {
	if code != g.cfg.RoomCode {
		return ErrRoomNotFound
	}
	r, err := defaultRoom()
	if err != nil {
		return err
	}
	p := findPlayer(r, playerID)
	if p == nil {
		return ErrPlayerNotFound
	}
	r.EventManage.Fire(&event.PlayerBanEvent{Player: p})
	return nil
}

// listPlayers 对应门面 GET /v1/rooms/{code}/players
func (g *Gateway) listPlayers(code string) ([]RoomMember, error) {
	if code != g.cfg.RoomCode {
		return nil, ErrRoomNotFound
	}
	s, err := g.roomSummary(true)
	if err != nil {
		return nil, err
	}
	if s.Members == nil {
		return []RoomMember{}, nil
	}
	return *s.Members, nil
}

// applySettings 对应门面 PUT /v1/rooms/{code}/settings (D-20).
//
// mapName 直接作用于 Go 房间 (下次开局生效); 引擎侧字段经 v2-ext
// room.settings 作用于 helper 内的 gameLinkServerData.
func (g *Gateway) applySettings(s *RoomSettings) (*RoomSummary, error) {
	r, err := defaultRoom()
	if err != nil {
		return nil, err
	}
	if s.MapName != nil && *s.MapName != "" {
		r.Maps.MapName = *s.MapName
	}
	if err := applyEngineSettings(r, s); err != nil {
		return nil, err
	}
	return buildSummary(r, g.cfg.RoomCode, false), nil
}

// applyEngineSettings 把引擎侧字段经 v2-ext room.settings 写入 helper 内的
// gameLinkServerData (D-20). 无后端 / helper 不支持时降级为仅记日志,
// 与其余 v2-ext 扩展的降级行为一致.
func applyEngineSettings(r *room.ServerRoom, s *RoomSettings) error {
	err := r.ApplySettings(game.RoomSettings{
		Credits:       s.Credits,
		Fog:           s.FogOfWar,
		Income:        s.Income,
		DisableNuke:   s.DisableNuke,
		SharedControl: s.SharedControl,
		StartingUnits: s.StartingUnits,
		MaxUnits:      s.MaxUnits,
	})
	if errors.Is(err, manage.ErrNotPortable) {
		log.Debugf("[Cascade]", "engine settings degraded (no helper support): %+v", s)
		return nil
	}
	return err
}

// createRoom 对应门面 POST /v1/rooms/create (单房间幂等语义, D-20):
// 与本服房间码一致即接受 (可顺带调整 maxPlayers), 其它码一律冲突.
func (g *Gateway) createRoom(code, title string, maxPlayers int) (*RoomSummary, error) {
	if code != g.cfg.RoomCode {
		return nil, ErrRoomConflict
	}
	r, err := defaultRoom()
	if err != nil {
		return nil, err
	}
	if maxPlayers > 0 {
		r.MaxPlayer = maxPlayers
	}
	_ = title // 单房间后端无独立房间标题, 以服务器名呈现 (D-20)
	return buildSummary(r, g.cfg.RoomCode, false), nil
}
