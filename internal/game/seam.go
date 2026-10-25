// Package game (Go-Server) 对应 net.rwhps.server.game
//
// 这是整个复刻工程里唯一无法机械移植的地方, 原因在 Simulation 的注释里说明.
// 本文件只定义"接缝": 网络层与命令层通过这些接口与游戏模拟内核交互,
// 具体后端由外部提供; 没有后端时一切开局操作返回 ErrNotImplemented.
package game

import (
	"errors"
	"sync"

	"rwhps/internal/data/global"
	"rwhps/internal/util/timeutil"
)

// ErrNotImplemented 表示该操作需要真实的游戏模拟内核, 而当前进程没有可用后端.
//
// 与 core.ErrNotImplemented (对应 ImplementedException) 区分: 后者是"这个方法永远不该被调用",
// 这里是"这个方法该被调用, 但 Go 版还没有实现它的后端".
var ErrNotImplemented = errors.New("game: no simulation backend available")

// Simulation 对应 rusted-warfare.jar 里的游戏内核
//
// 原版并不是自己实现游戏, 而是在 JVM 内加载闭源的 rusted-warfare.jar,
// 再通过 ASM (dependent/redirections/**、HeadlessProxyClass) 改写字节码,
// 把游戏的服务端主循环接到自己的事件总线上.
//
// Go 进程无法执行 Java 字节码, 也没有等价的字节码改写手段, 因此模拟层不可能 1:1 移植.
// 这里只声明网络层与命令层真正调用到的四个操作, 不做任何假实现 ——
// 宁可让 start 明确失败, 也不要造一个"看起来在跑"的空房间.
//
// 可用的后端方向 (均未实现, 属于后续批次):
//   - 外部进程: 由 Java 版 RW-HPS 或原版游戏托管模拟, Go 侧只做协议网关
//   - 重写内核: 用 Go 重新实现 Rusted Warfare 的服务端模拟
type Simulation interface {
	// Start 对应 ServerRoom.roomStartGame: 载入地图并开始一局
	Start(cfg MatchConfig) error
	// Stop 对应 ServerRoom.gr: 结束当前局并回到大厅
	Stop() error
	// Running 对应 ServerRoom.isStartGame
	Running() bool
	// PlayerCount 对应 room.playerManage.playerGroup.size
	PlayerCount() int
}

// MatchConfig 一局游戏的可移植启动参数
// 只包含 GameSession 自己就能决定的字段, 不涉及游戏内核的配置项.
type MatchConfig struct {
	// RoomID 对应 ServerRoom.roomID
	RoomID string
	// MapName 对应 room.maps.mapName
	MapName string
	// MapPlayer 对应 room.maps.mapPlayer
	MapPlayer string
	// MaxPlayer 对应 Data.configServer.maxPlayer
	MaxPlayer int
}

// Unimplemented 是 Simulation 的占位实现: Start/Stop 返回 ErrNotImplemented.
//
// 它存在的意义是把"没有游戏内核"这件事显式化: 上层拿到的是可读的错误,
// 而不是 nil 指针 panic 或一个静默成功的空实现.
type Unimplemented struct{}

// Start 实现 Simulation
func (Unimplemented) Start(MatchConfig) error { return ErrNotImplemented }

// Stop 实现 Simulation
func (Unimplemented) Stop() error { return ErrNotImplemented }

// Running 实现 Simulation: 没有后端, 永远不在运行
func (Unimplemented) Running() bool { return false }

// PlayerCount 实现 Simulation: 没有后端, 永远 0 人
func (Unimplemented) PlayerCount() int { return 0 }

// Backend 是全局默认的模拟内核, 由具体实现方在启动时通过 SetBackend 注入.
// 默认 Unimplemented, 使 start 命令返回 ErrNotImplemented 而不是崩溃.
var Backend Simulation = Unimplemented{}

// SetBackend 注入模拟内核; 传 nil 恢复为 Unimplemented
func SetBackend(s Simulation) {
	if s == nil {
		Backend = Unimplemented{}
		return
	}
	Backend = s
}

// GameSession 对应 ServerRoom: 一局游戏对外暴露的可移植状态.
//
// 与原版的差别: PlayerManage / CallManage / EventManage 都依赖游戏内核, 由 Simulation 承担;
// 这里只保留命令层 (version / hi / stop) 真正读取的字段, 因此可以脱离游戏内核存在.
type GameSession struct {
	// RoomID 对应 ServerRoom.roomID
	RoomID string

	// Simulation 驱动本局的模拟内核, 在构造时注入
	// nil 等同于没有后端, 开局操作返回 ErrNotImplemented
	Simulation Simulation

	mu sync.RWMutex

	// MapName 对应 room.maps.mapName
	MapName string
	// MapPlayer 对应 room.maps.mapPlayer
	MapPlayer string
	// MaxPlayer 对应 Data.configServer.maxPlayer
	MaxPlayer int

	// IsStartGame 对应 room.isStartGame
	IsStartGame bool
	// Sync 对应 room.sync
	Sync bool
	// MuteAll 对应 room.muteAll
	MuteAll bool

	// StartTime / EndTime 对应 room.startTime / endTime, Unix 秒
	// EndTime = StartTime + configServer.maxGameIngTime, 与原版一致;
	// maxGameIngTime 为 -1 (禁用) 时 EndTime 会小于 StartTime, 原版同样如此.
	StartTime int64
	EndTime   int64

	// ReplayFileName 对应 room.replayFileName
	ReplayFileName string
}

// DefaultMapName 对应 GameMaps.mapName 的默认值
const DefaultMapName = "Crossing Large (10p)"

// DefaultMapPlayer 对应 GameMaps.mapPlayer 的默认值
const DefaultMapPlayer = "[z;p10]"

// NewGameSession 对应 ServerRoom(gameModule) 的字段初始化
//
// sim 传 nil 表示没有模拟内核, 等价于注入 Unimplemented.
func NewGameSession(roomID string, sim Simulation) *GameSession {
	if sim == nil {
		sim = Unimplemented{}
	}
	return &GameSession{
		RoomID:     roomID,
		Simulation: sim,
		MapName:    DefaultMapName,
		MapPlayer:  DefaultMapPlayer,
		MaxPlayer:  global.ConfigServer.MaxPlayer,
		Sync:       true,
		MuteAll:    global.ConfigServer.MuteAll,
	}
}

// simulation 返回本局的模拟内核, 未注入时给出 Unimplemented
func (s *GameSession) simulation() Simulation {
	if s.Simulation == nil {
		return Unimplemented{}
	}
	return s.Simulation
}

// StartGame 对应 ServerRoom.roomStartGame
//
// 与原版一致: 只有模拟内核真正启动成功后才会写入 startTime/endTime 并置 isStartGame,
// 失败时房间保持未开局状态.
func (s *GameSession) StartGame() error {
	sim := s.simulation()

	s.mu.RLock()
	cfg := MatchConfig{
		RoomID:    s.RoomID,
		MapName:   s.MapName,
		MapPlayer: s.MapPlayer,
		MaxPlayer: s.MaxPlayer,
	}
	s.mu.RUnlock()

	if err := sim.Start(cfg); err != nil {
		return err
	}

	now := int64(timeutil.ConcurrentSecond())
	s.mu.Lock()
	s.IsStartGame = true
	s.StartTime = now
	s.EndTime = now + int64(global.ConfigServer.MaxGameIngTime)
	s.mu.Unlock()
	return nil
}

// GameOver 对应 ServerRoom.gr 里的 cleanData 部分
//
// 原版还会 fire(ServerGameOverEvent)、清理 banUnit、关闭计时任务、
// 清理地图临时文件 —— 这些都属于事件层与地图管理, 留到后续批次.
func (s *GameSession) GameOver() error {
	sim := s.simulation()
	if err := sim.Stop(); err != nil {
		return err
	}

	s.mu.Lock()
	s.IsStartGame = false
	s.StartTime = 0
	s.EndTime = 0
	s.mu.Unlock()
	return nil
}

// PlayerCount 对应 room.playerManage.playerGroup.size
func (s *GameSession) PlayerCount() int { return s.simulation().PlayerCount() }

// Running 对应 room.isStartGame (以模拟内核的实际状态为准)
func (s *GameSession) Running() bool { return s.simulation().Running() }

// Snapshot 返回当前房间状态的可读副本, 供 version / hi 命令使用
func (s *GameSession) Snapshot() RoomState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return RoomState{
		RoomID:         s.RoomID,
		MapName:        s.MapName,
		MapPlayer:      s.MapPlayer,
		MaxPlayer:      s.MaxPlayer,
		IsStartGame:    s.IsStartGame,
		Sync:           s.Sync,
		MuteAll:        s.MuteAll,
		StartTime:      s.StartTime,
		EndTime:        s.EndTime,
		ReplayFileName: s.ReplayFileName,
	}
}

// RoomState 是 GameSession 的无锁快照
type RoomState struct {
	RoomID         string
	MapName        string
	MapPlayer      string
	MaxPlayer      int
	IsStartGame    bool
	Sync           bool
	MuteAll        bool
	StartTime      int64
	EndTime        int64
	ReplayFileName string
}

// 编译期确认
var _ Simulation = Unimplemented{}
