package manage

// CallManage 单测: v2-ext 接线 (DECISIONS D-9).
//
// 有意修正（作者已批准，2026-11-04）: 上游引擎级能力 (全量同步 / 结算播报)
// 经 RoomHooks 转发到 helper 协议扩展操作; 这里用假房间验证转发与降级.

import (
	"testing"
	"time"

	"rwhps/internal/core/thread"
	"rwhps/internal/data/temp"
	"rwhps/internal/game/event"
)

// fakeRoomHooks 是 RoomHooks 的记录型假实现
type fakeRoomHooks struct {
	flag *temp.ServerCacheFlag

	addAIErr   error
	addAICalls int
	syncErr    error
	syncCalls  int

	result    *event.GameOverData
	resultOK  bool
	resultErr error

	gameOverData *event.GameOverData
	grCalls      int
}

func newFakeRoomHooks() *fakeRoomHooks {
	return &fakeRoomHooks{flag: temp.NewServerCacheFlag()}
}

func (f *fakeRoomHooks) GetRoomID() string                  { return "fake-room" }
func (f *fakeRoomHooks) GetEndTime() int64                  { return time.Now().Unix() + 3600 }
func (f *fakeRoomHooks) GetFlagData() *temp.ServerCacheFlag { return f.flag }
func (f *fakeRoomHooks) Gr()                                { f.grCalls++ }

func (f *fakeRoomHooks) AddAI() error {
	f.addAICalls++
	return f.addAIErr
}

func (f *fakeRoomHooks) SyncAllPlayer() error {
	f.syncCalls++
	return f.syncErr
}

func (f *fakeRoomHooks) RemovePlayer(int) error { return nil }

func (f *fakeRoomHooks) GameResult() (*event.GameOverData, bool, error) {
	return f.result, f.resultOK, f.resultErr
}

func (f *fakeRoomHooks) SetGameOverData(d *event.GameOverData) { f.gameOverData = d }

// TestSyncAllPlayerDelegates syncAllPlayer 经房间转发 (v2-ext)
func TestSyncAllPlayerDelegates(t *testing.T) {
	hooks := newFakeRoomHooks()
	c := NewCallManage(NewPlayerManage(), hooks)

	c.SyncAllPlayer()
	if hooks.syncCalls != 1 {
		t.Fatalf("SyncAllPlayer 应经房间转发, calls = %d", hooks.syncCalls)
	}

	// 房间报错 (含降级后的错误) 只记日志, 不向上传播
	hooks.syncErr = ErrNotPortable
	c.SyncAllPlayer()
	if hooks.syncCalls != 2 {
		t.Fatalf("出错也应完成转发, calls = %d", hooks.syncCalls)
	}
}

// TestStartCheckBroadcastGameResult 检查线程的结算播报分支:
// 经 game.result 取数 -> 暂存房间 -> 清标志位 -> 播报.
//
// 有意修正（作者已批准，2026-11-04）: 上游数据来自进程内引擎, Go 版经
// helper 协议 v2-ext 的 game.result 操作获取 (DECISIONS D-9).
func TestStartCheckBroadcastGameResult(t *testing.T) {
	hooks := newFakeRoomHooks()
	hooks.result = &event.GameOverData{
		GameTime:      100,
		AllPlayerList: []string{"alice", "bob"},
		WinPlayerList: []string{"alice"},
		MapName:       "m1",
	}
	hooks.resultOK = true

	pm := NewPlayerManage()
	// 两个玩家: 避开 0/1 人分支的 gr() 干扰
	pm.AddAbstractPlayer(&pmMockConnect{}, profileData("h1", "alice", 0, 1), nil)
	pm.AddAbstractPlayer(&pmMockConnect{}, profileData("h2", "bob", 1, 2), nil)

	c := NewCallManage(pm, hooks)
	c.StartCheckThread()
	t.Cleanup(func() { thread.CloseTimeTask(thread.AutoCheckTask, nil) })

	// SendGameStatusFlag 默认 true; 等检查线程 (1s 周期) 完成播报
	deadline := time.Now().Add(3 * time.Second)
	for hooks.flag.SendGameStatusFlag() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	if hooks.flag.SendGameStatusFlag() {
		t.Fatal("播报后 sendGameStatusFlag 应置 false")
	}
	if hooks.gameOverData != hooks.result {
		t.Fatal("结算数据应暂存到房间")
	}
}

// TestStartCheckGameResultNotOver 未分胜负 (无数据): 等价上游拿到 null,
// 不播报也不清标志位.
func TestStartCheckGameResultNotOver(t *testing.T) {
	hooks := newFakeRoomHooks()
	hooks.resultOK = false

	pm := NewPlayerManage()
	pm.AddAbstractPlayer(&pmMockConnect{}, profileData("h1", "alice", 0, 1), nil)
	pm.AddAbstractPlayer(&pmMockConnect{}, profileData("h2", "bob", 1, 2), nil)

	c := NewCallManage(pm, hooks)
	c.StartCheckThread()
	t.Cleanup(func() { thread.CloseTimeTask(thread.AutoCheckTask, nil) })

	// 等两个周期, 确认不会误播报
	time.Sleep(2200 * time.Millisecond)
	if !hooks.flag.SendGameStatusFlag() {
		t.Fatal("未分胜负时不应清 sendGameStatusFlag")
	}
	if hooks.gameOverData != nil {
		t.Fatal("未分胜负时不应暂存结算数据")
	}
}
