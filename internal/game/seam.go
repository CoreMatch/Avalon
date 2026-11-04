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
	"rwhps/internal/game/event"
	"rwhps/internal/util/timeutil"
)

// ErrNotImplemented 表示该操作需要真实的游戏模拟内核, 而当前进程没有可用后端.
//
// 与 core.ErrNotImplemented (对应 ImplementedException) 区分: 后者是"这个方法永远不该被调用",
// 这里是"这个方法该被调用, 但 Go 版还没有实现它的后端".
var ErrNotImplemented = errors.New("game: no simulation backend available")

// ErrUnsupported 表示模拟后端不支持某个扩展操作.
//
// 有意修正（作者已批准，2026-11-04）: 对应 helper 协议 v2-ext 的向后兼容约定 ——
// 只实现了 v2 的 helper 对扩展操作回 "unsupported", Go 侧把它归一成本错误,
// 调用方降级为空操作并记日志, 不得 panic 或断开连接 (DECISIONS D-9).
var ErrUnsupported = errors.New("game: operation unsupported by simulation backend")

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

// RoomControl 是房间层对模拟内核的扩展操作接缝 (helper 协议 v2-ext).
//
// 有意修正（作者已批准，2026-11-04）: 上游这些操作直接调用进程内的引擎对象
// (gameScriptMultiPlayer.addAi / gameLinkFunction.allPlayerSync /
// gameHessData.getGameOverData 等); Go 版引擎在 helper 内, 经 v2-ext 扩展操作
// 完成 (DECISIONS D-9). 与 Simulation 分开声明:
//
//   - 不改动 v2 已有的四个操作, 既有实现与测试不受影响;
//   - 后端未实现本接口 (如 Unimplemented) 时, 调用方按类型断言失败降级 ——
//     与"无 helper"时的既有行为一致.
//
// 实现方返回 ErrUnsupported 表示 helper 只支持 v2; 调用方降级为空操作并记日志.
type RoomControl interface {
	// AddAI 对应 gameScriptMultiPlayer.addAi(): 让引擎生成一个 AI
	AddAI() error
	// SyncAllPlayer 对应 gameLinkFunction.allPlayerSync(): 引擎全量同步
	SyncAllPlayer() error
	// RemovePlayer 对应引擎侧玩家移除 (AiPlayer.kickPlayer 里的
	// playerPrivateData.removePlayer()): 按位置移除引擎内的玩家
	RemovePlayer(position int) error
	// GameResult 对应 gameHessData.getGameOverData(): 取结算数据.
	// ok=false 对应上游返回 null (尚未分出胜负); 非 nil 错误才是查询失败.
	GameResult() (data *event.GameOverData, ok bool, err error)
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

	// Maps 对应 room.maps: 当前房间使用的地图
	Maps *GameMaps
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
		Maps:       NewGameMaps(),
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
	maps := s.Maps
	cfg := MatchConfig{
		RoomID:    s.RoomID,
		MaxPlayer: s.MaxPlayer,
	}
	if maps != nil {
		cfg.MapName = maps.MapName
		cfg.MapPlayer = maps.MapPlayer
	}
	s.mu.RUnlock()

	// 原版 roomStartGame 之前会由地图管理器把地图落到磁盘; 这里先把地图准备好
	if maps != nil && maps.MapData != nil {
		if err := maps.MapData.ReadMap(); err != nil {
			return err
		}
	}

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
// 原版还会 fire(ServerGameOverEvent)、清理 banUnit、关闭计时任务 —— 这些属于事件层,
// 留到后续批次; 地图临时文件的清理在这里做, 对应 cleanData 末尾的 maps.mapData?.clean().
func (s *GameSession) GameOver() error {
	sim := s.simulation()
	if err := sim.Stop(); err != nil {
		return err
	}

	s.mu.Lock()
	s.IsStartGame = false
	s.StartTime = 0
	s.EndTime = 0
	maps := s.Maps
	s.mu.Unlock()

	if maps != nil && maps.MapData != nil {
		maps.MapData.Clean()
	}
	return nil
}

// PlayerCount 对应 room.playerManage.playerGroup.size
func (s *GameSession) PlayerCount() int { return s.simulation().PlayerCount() }

// Running 对应 room.isStartGame (以模拟内核的实际状态为准)
func (s *GameSession) Running() bool { return s.simulation().Running() }

// StopSimulation 只停止模拟内核, 不动房间状态
//
// 供 room.ServerRoom 组合使用: 上游 gr() 的清理与引擎停止是分开的两步,
// GameOver 把它们合在了壳里, 实体链落地后需要拆开.
func (s *GameSession) StopSimulation() error { return s.simulation().Stop() }

// ResetMatchState 清空开局状态位 (对应 cleanData 里的 isStartGame/startTime/endTime 部分)
func (s *GameSession) ResetMatchState() {
	s.mu.Lock()
	s.IsStartGame = false
	s.StartTime = 0
	s.EndTime = 0
	s.mu.Unlock()
}

// GetStartTime 对应 startTime 的线程安全读
func (s *GameSession) GetStartTime() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.StartTime
}

// GetEndTime 对应 endTime 的线程安全读
func (s *GameSession) GetEndTime() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.EndTime
}

// Snapshot 返回当前房间状态的可读副本, 供 version / hi 命令使用
func (s *GameSession) Snapshot() RoomState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	st := RoomState{
		RoomID:         s.RoomID,
		MaxPlayer:      s.MaxPlayer,
		IsStartGame:    s.IsStartGame,
		Sync:           s.Sync,
		MuteAll:        s.MuteAll,
		StartTime:      s.StartTime,
		EndTime:        s.EndTime,
		ReplayFileName: s.ReplayFileName,
	}
	if s.Maps != nil {
		st.MapName = s.Maps.MapName
		st.MapPlayer = s.Maps.MapPlayer
	}
	return st
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
