package game

import (
	"errors"
	"testing"

	"rwhps/internal/data/global"
)

// fakeSim 记录调用并按需返回错误
type fakeSim struct {
	startErr error
	stopErr  error

	started bool
	stopped bool
	cfg     MatchConfig
	players int
	running bool
}

func (f *fakeSim) Start(cfg MatchConfig) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.started = true
	f.cfg = cfg
	f.running = true
	return nil
}

func (f *fakeSim) Stop() error {
	if f.stopErr != nil {
		return f.stopErr
	}
	f.stopped = true
	f.running = false
	return nil
}

func (f *fakeSim) Running() bool    { return f.running }
func (f *fakeSim) PlayerCount() int { return f.players }

// TestUnimplementedReturnsErr 占位后端必须明确失败, 不能静默成功
func TestUnimplementedReturnsErr(t *testing.T) {
	var sim Simulation = Unimplemented{}

	if err := sim.Start(MatchConfig{}); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Start err = %v, want ErrNotImplemented", err)
	}
	if err := sim.Stop(); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Stop err = %v, want ErrNotImplemented", err)
	}
	if sim.Running() {
		t.Fatal("Unimplemented.Running() 应为 false")
	}
	if got := sim.PlayerCount(); got != 0 {
		t.Fatalf("Unimplemented.PlayerCount() = %d, want 0", got)
	}
}

// TestSetBackend 注入/复位全局后端
func TestSetBackend(t *testing.T) {
	old := Backend
	t.Cleanup(func() { Backend = old })

	fs := &fakeSim{}
	SetBackend(fs)
	if Backend != Simulation(fs) {
		t.Fatal("SetBackend 未生效")
	}

	SetBackend(nil)
	if _, ok := Backend.(Unimplemented); !ok {
		t.Fatalf("SetBackend(nil) 应恢复 Unimplemented, got %T", Backend)
	}
}

// TestNewGameSessionDefaults 对应 ServerRoom 的字段初值
func TestNewGameSessionDefaults(t *testing.T) {
	oldMax, oldMute := global.ConfigServer.MaxPlayer, global.ConfigServer.MuteAll
	global.ConfigServer.MaxPlayer = 12
	global.ConfigServer.MuteAll = true
	t.Cleanup(func() {
		global.ConfigServer.MaxPlayer = oldMax
		global.ConfigServer.MuteAll = oldMute
	})

	s := NewGameSession("room-1", nil)
	if s.RoomID != "room-1" {
		t.Fatalf("RoomID = %q", s.RoomID)
	}
	if s.Maps == nil {
		t.Fatal("Maps 不应为 nil")
	}
	if s.Maps.MapName != DefaultMapName || s.Maps.MapPlayer != DefaultMapPlayer {
		t.Fatalf("地图默认值 = %q / %q", s.Maps.MapName, s.Maps.MapPlayer)
	}
	if s.MaxPlayer != 12 {
		t.Fatalf("MaxPlayer = %d, want 12", s.MaxPlayer)
	}
	if !s.Sync {
		t.Fatal("sync 默认应为 true")
	}
	if !s.MuteAll {
		t.Fatal("MuteAll 应取 configServer.muteAll")
	}
	if s.IsStartGame || s.StartTime != 0 || s.EndTime != 0 {
		t.Fatal("新房间不应处于开局状态")
	}
	// 未注入后端时归一化为 Unimplemented, 而不是留 nil
	if _, ok := s.Simulation.(Unimplemented); !ok {
		t.Fatalf("nil Simulation 应归一化为 Unimplemented, got %T", s.Simulation)
	}
}

// TestStartGameWithoutBackend 没有后端时开局必须失败, 且不改房间状态
func TestStartGameWithoutBackend(t *testing.T) {
	s := NewGameSession("room-1", nil)

	if err := s.StartGame(); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("StartGame err = %v, want ErrNotImplemented", err)
	}
	if s.IsStartGame || s.StartTime != 0 || s.EndTime != 0 {
		t.Fatal("开局失败后不应写入开局状态")
	}
	if s.Running() {
		t.Fatal("没有后端时 Running() 应为 false")
	}
	if got := s.PlayerCount(); got != 0 {
		t.Fatalf("PlayerCount() = %d, want 0", got)
	}
}

// TestStartGameWithBackend 对应 ServerRoom.roomStartGame 的计时语义
func TestStartGameWithBackend(t *testing.T) {
	oldTime := global.ConfigServer.MaxGameIngTime
	global.ConfigServer.MaxGameIngTime = 3600
	t.Cleanup(func() { global.ConfigServer.MaxGameIngTime = oldTime })

	fs := &fakeSim{}
	s := NewGameSession("room-2", fs)
	s.Maps.MapName = "Dust"
	s.Maps.MapPlayer = "[p4]"
	s.MaxPlayer = 4

	if err := s.StartGame(); err != nil {
		t.Fatalf("StartGame err = %v", err)
	}
	if !fs.started {
		t.Fatal("Simulation.Start 未被调用")
	}
	if fs.cfg.RoomID != "room-2" || fs.cfg.MapName != "Dust" || fs.cfg.MapPlayer != "[p4]" || fs.cfg.MaxPlayer != 4 {
		t.Fatalf("MatchConfig = %+v", fs.cfg)
	}
	if !s.IsStartGame {
		t.Fatal("开局后 IsStartGame 应为 true")
	}
	if s.StartTime == 0 {
		t.Fatal("StartTime 未写入")
	}
	// endTime = startTime + maxGameIngTime
	if got := s.EndTime - s.StartTime; got != 3600 {
		t.Fatalf("EndTime-StartTime = %d, want 3600", got)
	}
	if !s.Running() {
		t.Fatal("开局后 Running() 应为 true")
	}
}

// TestStartGameBackendError 后端失败时房间保持未开局
func TestStartGameBackendError(t *testing.T) {
	boom := errors.New("boom")
	s := NewGameSession("room-3", &fakeSim{startErr: boom})

	if err := s.StartGame(); !errors.Is(err, boom) {
		t.Fatalf("StartGame err = %v, want boom", err)
	}
	if s.IsStartGame || s.StartTime != 0 {
		t.Fatal("后端失败时不应写入开局状态")
	}
}

// TestGameOver 对应 roomStartGame 的逆操作
func TestGameOver(t *testing.T) {
	fs := &fakeSim{}
	s := NewGameSession("room-4", fs)
	if err := s.StartGame(); err != nil {
		t.Fatalf("StartGame err = %v", err)
	}

	if err := s.GameOver(); err != nil {
		t.Fatalf("GameOver err = %v", err)
	}
	if !fs.stopped {
		t.Fatal("Simulation.Stop 未被调用")
	}
	if s.IsStartGame || s.StartTime != 0 || s.EndTime != 0 {
		t.Fatal("GameOver 后房间状态应清零")
	}
}

// TestGameOverWithoutBackend 没有后端时结束局也要明确失败
func TestGameOverWithoutBackend(t *testing.T) {
	s := NewGameSession("room-5", nil)
	if err := s.GameOver(); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("GameOver err = %v, want ErrNotImplemented", err)
	}
}

// TestGameOverBackendError 后端失败时不清空房间状态
func TestGameOverBackendError(t *testing.T) {
	boom := errors.New("boom")
	fs := &fakeSim{stopErr: boom}
	s := NewGameSession("room-6", fs)
	if err := s.StartGame(); err != nil {
		t.Fatalf("StartGame err = %v", err)
	}

	if err := s.GameOver(); !errors.Is(err, boom) {
		t.Fatalf("GameOver err = %v, want boom", err)
	}
	if !s.IsStartGame {
		t.Fatal("后端失败时不应清空开局状态")
	}
}

// TestSnapshot 快照应与内部字段一致
func TestSnapshot(t *testing.T) {
	s := NewGameSession("room-7", &fakeSim{players: 3})
	s.ReplayFileName = "replay.dat"
	if err := s.StartGame(); err != nil {
		t.Fatalf("StartGame err = %v", err)
	}

	st := s.Snapshot()
	if st.RoomID != "room-7" || st.MapName != DefaultMapName || st.ReplayFileName != "replay.dat" {
		t.Fatalf("Snapshot = %+v", st)
	}
	if !st.IsStartGame || st.StartTime == 0 {
		t.Fatalf("Snapshot 开局状态 = %+v", st)
	}
	if got := s.PlayerCount(); got != 3 {
		t.Fatalf("PlayerCount() = %d, want 3", got)
	}
}
