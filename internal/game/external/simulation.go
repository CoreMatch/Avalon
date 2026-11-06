package external

import (
	"errors"

	"rwhps/internal/game"
)

// ErrNoRoom 表示 Backend 上还没有创建过房间 (Start 从未成功),
// 此时 Stop 没有可操作的对象.
var ErrNoRoom = errors.New("external: no active room")

// 编译期确认: 启动链 (app.loadSimulation) 与测试都依赖这条赋值成立.
var _ game.Simulation = (*Backend)(nil)

// Start 实现 game.Simulation, 对应 ServerRoom.roomStartGame
//
// 拆成两步:
//
//  1. room.create —— 房间在 helper 侧是独立游戏引擎实例, 必须先存在才能开局.
//     同一房间只创建一次 (按世代跟踪); 协议同时要求 room.create 幂等,
//     重复创建已存在的房间返回 OK, 见 protocol.go 的 OpRoomCreate 注释.
//  2. room.start —— 携带地图参数真正开局.
//
// 任一步失败都原样返回 helper 的失败原因; 房间创建成功而开局失败时,
// 房间保留 (下次 Start 不再重复创建), 与原版"房间先于对局存在"的语义一致.
func (b *Backend) Start(cfg game.MatchConfig) error {
	if err := b.EnsureRunning(); err != nil {
		return err
	}

	b.mu.Lock()
	room := b.room
	b.mu.Unlock()

	if room != cfg.RoomID {
		if _, err := b.Request(&Request{
			Op:        OpRoomCreate,
			Room:      cfg.RoomID,
			MaxPlayer: cfg.MaxPlayer,
		}); err != nil {
			return err
		}
		b.mu.Lock()
		b.room = cfg.RoomID
		b.mu.Unlock()
	}

	if _, err := b.Request(&Request{
		Op:        OpRoomStart,
		Room:      cfg.RoomID,
		Map:       cfg.MapName,
		MapPlayer: cfg.MapPlayer,
	}); err != nil {
		return err
	}

	b.mu.Lock()
	b.running = true
	b.mu.Unlock()
	return nil
}

// Stop 实现 game.Simulation, 对应 ServerRoom.gr (结束当前局)
//
// 只结束对局, 不销毁房间: 原版在对局结束后房间继续存在, 可以再次开局.
// 尚未创建过房间时返回 ErrNoRoom.
func (b *Backend) Stop() error {
	b.mu.Lock()
	room := b.room
	b.mu.Unlock()
	if room == "" {
		return ErrNoRoom
	}

	if _, err := b.Request(&Request{Op: OpRoomStop, Room: room}); err != nil {
		return err
	}

	b.mu.Lock()
	b.running = false
	b.mu.Unlock()
	return nil
}

// Running 实现 game.Simulation, 对应 ServerRoom.isStartGame
//
// 不启动进程: helper 未存活时直接 false (惰性启动是 Backend 的核心约定,
// 状态查询不该把 JVM 拉起来).
//
// helper 存活时以 room.status 的应答为准 —— 游戏内核的真实状态在 helper 侧;
// 查询失败 (超时/进程刚好退出) 才回落到本地记录.
func (b *Backend) Running() bool {
	s := b.liveSession()
	if s == nil {
		return false
	}
	room := b.currentRoom()
	if room == "" {
		return false
	}
	resp, err := b.status(s, room)
	if err != nil {
		return b.localRunning()
	}
	return resp.Running
}

// PlayerCount 实现 game.Simulation, 对应 room.playerManage.playerGroup.size
//
// 人数以 helper 侧为准 (玩家档案当前由游戏内核持有), 经 room.status 查询;
// 无 helper / 无房间 / 查询失败时为 0.
func (b *Backend) PlayerCount() int {
	s := b.liveSession()
	if s == nil {
		return 0
	}
	room := b.currentRoom()
	if room == "" {
		return 0
	}
	resp, err := b.status(s, room)
	if err != nil {
		return 0
	}
	return resp.Players
}

// status 对指定房间发一次 room.status, 用独立的 StatusTimeout
//
// 不复用 Request: Request 会在进程不在时拉起新进程, 而状态查询必须是无副作用的.
func (b *Backend) status(s *session, room string) (*Response, error) {
	return b.doRequest(s, &Request{Op: OpRoomStatus, Room: room}, b.cfg.StatusTimeout)
}

// liveSession 返回当前存活的 helper 会话; 无会话或进程已退出时为 nil
//
// 与 ensureRunningLocked 里的判活逻辑一致, 但不触发任何启动.
func (b *Backend) liveSession() *session {
	b.mu.Lock()
	s := b.session
	closed := b.closed
	b.mu.Unlock()
	if closed || s == nil {
		return nil
	}
	select {
	case <-s.proc.Done():
		return nil
	default:
		return s
	}
}

// currentRoom 取当前房间 ID (空表示尚未创建)
func (b *Backend) currentRoom() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.room
}

// localRunning 取本地记录的对局状态 (回落用)
func (b *Backend) localRunning() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running
}
