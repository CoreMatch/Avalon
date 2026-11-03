package room

import (
	"errors"
	"testing"
	"time"

	"rwhps/internal/core/thread"
	"rwhps/internal/data/global"
	"rwhps/internal/game"
)

// fakeSim 记录调用并按需返回错误
type fakeSim struct {
	startErr error
	stopErr  error
	started  bool
	stopped  bool
	running  bool
}

func (f *fakeSim) Start(game.MatchConfig) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.started, f.running = true, true
	return nil
}

func (f *fakeSim) Stop() error {
	if f.stopErr != nil {
		return f.stopErr
	}
	f.stopped, f.running = true, false
	return nil
}

func (f *fakeSim) Running() bool    { return f.running }
func (f *fakeSim) PlayerCount() int { return 0 }

// newTestRoom 构造带假内核的房间, 并负责收尾定时器
func newTestRoom(t *testing.T, sim game.Simulation) *ServerRoom {
	t.Helper()
	r := NewServerRoom(game.NewGameSession("room-sm", sim))
	t.Cleanup(func() {
		thread.CloseTimeTask(thread.AutoCheckTask, nil)
		thread.CloseTimeTask(thread.GameOverTask, nil)
		thread.CloseTimeTask(thread.CallTeamTask, nil)
		// 事件默认监听会挂自动开局/AFK 倒计时 (上游同样常驻), 测试结束前收掉
		thread.CloseTimeTask(thread.AutoStartTask, nil)
		thread.CloseTimeTask(thread.PlayerAfkTask, nil)
		SetDefault(nil)
	})
	return r
}

// TestRoomAssembly 实体链装配: 上游 ServerRoom 的成员一个不少
func TestRoomAssembly(t *testing.T) {
	r := newTestRoom(t, nil)

	if r.PlayerManage == nil || r.Call == nil || r.TeamManage == nil {
		t.Fatal("PlayerManage / Call / TeamManage 缺失")
	}
	if r.ClientHandler == nil {
		t.Fatal("ClientHandler 缺失")
	}
	if r.BanUnit == nil || r.FlagData == nil {
		t.Fatal("BanUnit / FlagData 缺失")
	}
	// 上游默认值
	if !r.FlagData.SendGameStatusFlag() || !r.FlagData.ForcedCloseSendMsg() || !r.FlagData.OneSay() || !r.FlagData.AiWarn() {
		t.Fatal("FlagData 默认值应为 true/true/true/_/true")
	}
	if r.FlagData.Ai() {
		t.Fatal("FlagData.Ai 默认应为 false")
	}
	if r.IsMixPlayer {
		t.Fatal("IsMixPlayer 默认应为 false")
	}
}

// TestRoomStartGame 开局状态机: 内核成功后才写状态并挂检查线程
func TestRoomStartGame(t *testing.T) {
	old := global.ConfigServer.MaxGameIngTime
	global.ConfigServer.MaxGameIngTime = 3600
	t.Cleanup(func() { global.ConfigServer.MaxGameIngTime = old })

	fs := &fakeSim{}
	r := newTestRoom(t, fs)

	if err := r.StartGame(); err != nil {
		t.Fatalf("StartGame: %v", err)
	}
	if !fs.started || !r.IsStartGame {
		t.Fatal("开局后内核应启动且 IsStartGame 为 true")
	}
	if r.GetEndTime()-r.GetStartTime() != 3600 {
		t.Fatalf("计时 = %d, 期望 3600", r.GetEndTime()-r.GetStartTime())
	}
	// 上游 roomStartGame 挂上自动检查线程
	if !thread.ContainsTimeTask(thread.AutoCheckTask) {
		t.Fatal("开局后应有 AutoCheckTask")
	}
}

// TestRoomStartGameBackendError 内核失败时房间保持未开局
func TestRoomStartGameBackendError(t *testing.T) {
	boom := errors.New("boom")
	r := newTestRoom(t, &fakeSim{startErr: boom})

	if err := r.StartGame(); !errors.Is(err, boom) {
		t.Fatalf("StartGame err = %v, 期望 boom", err)
	}
	if r.IsStartGame {
		t.Fatal("内核失败时不应置开局状态")
	}
	if thread.ContainsTimeTask(thread.AutoCheckTask) {
		t.Fatal("内核失败时不应挂检查线程")
	}
}

// TestGr 强制结束: 状态清零, 检查线程收掉
func TestGr(t *testing.T) {
	fs := &fakeSim{}
	r := newTestRoom(t, fs)
	if err := r.StartGame(); err != nil {
		t.Fatalf("StartGame: %v", err)
	}
	r.BanUnit.Add("tank")

	r.Gr()

	if r.IsStartGame || r.GetStartTime() != 0 || r.GetEndTime() != 0 {
		t.Fatal("Gr 后开局状态应清零")
	}
	if r.BanUnit.Size() != 0 {
		t.Fatal("Gr 后 banUnit 应清空")
	}
	if !r.FlagData.OneSay() || !r.FlagData.SendGameStatusFlag() {
		t.Fatal("Gr 后 FlagData 应重置")
	}
	if thread.ContainsTimeTask(thread.AutoCheckTask) {
		t.Fatal("Gr 后 AutoCheckTask 应收掉")
	}
	// gr 不停内核 (上游由引擎自行回到大厅)
	if fs.stopped {
		t.Fatal("Gr 不应调用 Simulation.Stop")
	}
}

// TestGameOverCommand gameover 入口: 先停内核再走 gr 清理
func TestGameOverCommand(t *testing.T) {
	fs := &fakeSim{}
	r := newTestRoom(t, fs)
	if err := r.StartGame(); err != nil {
		t.Fatalf("StartGame: %v", err)
	}

	if err := r.GameOver(); err != nil {
		t.Fatalf("GameOver: %v", err)
	}
	if !fs.stopped {
		t.Fatal("GameOver 应停内核")
	}
	if r.IsStartGame {
		t.Fatal("GameOver 后应清零开局状态")
	}
}

// TestGameOverBackendError 内核拒绝停止时不清空状态
func TestGameOverBackendError(t *testing.T) {
	boom := errors.New("boom")
	fs := &fakeSim{stopErr: boom}
	r := newTestRoom(t, fs)
	if err := r.StartGame(); err != nil {
		t.Fatalf("StartGame: %v", err)
	}

	if err := r.GameOver(); !errors.Is(err, boom) {
		t.Fatalf("GameOver err = %v, 期望 boom", err)
	}
	if !r.IsStartGame {
		t.Fatal("内核失败时不应清空开局状态")
	}
}

// TestBattleRoom 对应 battleRoom: 恢复 CallTeamTask + cleanData
func TestBattleRoom(t *testing.T) {
	r := newTestRoom(t, &fakeSim{})
	if err := r.StartGame(); err != nil {
		t.Fatalf("StartGame: %v", err)
	}
	r.BanUnit.Add("tank")

	r.BattleRoom()

	if r.IsStartGame {
		t.Fatal("BattleRoom 后应清零开局状态")
	}
	if r.BanUnit.Size() != 0 {
		t.Fatal("BattleRoom 后 banUnit 应清空")
	}
}

// TestAutoCheckEndsEmptyRoom 自动检查: 无 AI 且 0 在线玩家时收掉房间
//
// 上游每秒检查一次; 这里等两个周期确保触发. 这是"关键迁移"之一:
// 开局 -> 玩家走光 -> gr() 自动发生.
func TestAutoCheckEndsEmptyRoom(t *testing.T) {
	r := newTestRoom(t, &fakeSim{})
	if err := r.StartGame(); err != nil {
		t.Fatalf("StartGame: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	// 经 Snapshot 读 (带锁): 检查线程会在另一 goroutine 里写 IsStartGame
	for r.GameSession.Snapshot().IsStartGame && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if r.GameSession.Snapshot().IsStartGame {
		t.Fatal("0 玩家时自动检查应触发 gr() 结束对局")
	}
}

// TestDefaultSlot 单例槽位
func TestDefaultSlot(t *testing.T) {
	r := newTestRoom(t, nil)
	SetDefault(r)
	if Default() != r {
		t.Fatal("Default 应返回已设置的房间")
	}
}
