package thread

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestCallTimeTaskMetadataMatchesOriginal(t *testing.T) {
	// 名称 / 分组 / 描述必须与原版逐一对应, 否则已排程任务的 Quartz JobKey 会变
	want := map[CallTimeTask][3]string{
		CallTeamTask:                  {"CallTeamTask", "[Core]Server", "Update player team list"},
		PlayerAfkTask:                 {"PlayerAfkTask", "[Core]Server", "Transfer of authority"},
		GameOverTask:                  {"GameOverTask", "[Core]Server", "Check gameover"},
		VoteTask:                      {"VoteTask", "[Core]Server", "Vote"},
		AutoStartTask:                 {"AutoStartTask", "[CoreX]Server", "Start automatically"},
		AutoUpdateMapsTask:            {"AutoUpdateMapsTask", "[CoreX]Server", "Auto update maps"},
		AutoCheckTask:                 {"AutoCheckTask", "[CoreX]Server", "Auto Check player survives"},
		CustomUpServerListTask:        {"CustomUpServerListTask", "UpList", "[Plugin UpList] Update Data"},
		UpServerListTask:              {"UpServerListTask", "UpList", "Core"},
		UpServerListNewTask:           {"UpServerListNewTask", "UpList", "Core"},
		BlackListCheckTask:            {"BlackListCheckTask", "[BlackList]", "超时去除"},
		ServerUploadDataTask:          {"ServerUploadDataTask", "[RCN]", "列表更新"},
		ServerHostKeepAlive:           {"ServerHostKeepAlive", "[RCN]", "保活"},
		ServerUploadDataCheckTimeTask: {"ServerUploadData_CheckTimeTask", "[RCN]", "超时"},
		RelayRoomCheckTimeTask:        {"RelayRoom_CheckTimeTask", "[RCN]", "房间超时机制"},
		ServerStatusUpdate:            {"ServerStatusUpdate", "[RCN]", "状态更新"},
		ServerUpStatistics:            {"ServerUpStatistics", "[RW-HPS]", "统计数据更新"},
	}

	if len(want) != len(callTimeTaskNames) {
		t.Fatalf("enum has %d values, table has %d", len(callTimeTaskNames), len(want))
	}
	for task, w := range want {
		if got := task.String(); got != w[0] {
			t.Errorf("%d.String() = %q, want %q", task, got, w[0])
		}
		if got := task.Group(); got != w[1] {
			t.Errorf("%s.Group() = %q, want %q", w[0], got, w[1])
		}
		if got := task.Description(); got != w[2] {
			t.Errorf("%s.Description() = %q, want %q", w[0], got, w[2])
		}
	}
}

func TestCallTimeTaskKeysAreUnique(t *testing.T) {
	// 两个不同任务不能落在同一个 (name, group) 上, 否则会互相顶掉
	seen := map[[2]string]string{}
	for i := 0; i < len(callTimeTaskNames); i++ {
		task := CallTimeTask(i)
		key := [2]string{task.String(), task.Group()}
		if prev, ok := seen[key]; ok {
			t.Fatalf("%s and %s share the key %v", prev, task.String(), key)
		}
		seen[key] = task.String()
	}
}

func TestNewThreadOtherRunsTask(t *testing.T) {
	done := make(chan struct{})
	NewThreadOther(func() { close(done) })
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("NewThreadOther never ran the task")
	}
}

func TestNewThreadCoreRunsTask(t *testing.T) {
	done := make(chan struct{})
	NewThreadCore(func() { close(done) })
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("NewThreadCore never ran the task")
	}
}

func TestNewThreadCoreNetRunsTask(t *testing.T) {
	done := make(chan struct{})
	NewThreadCoreNet(func() { close(done) })
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("NewThreadCoreNet never ran the task")
	}
}

func TestCloseNetReplacesPool(t *testing.T) {
	before := coreNet()

	done := make(chan struct{})
	NewThreadCoreNet(func() { close(done) })
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("NewThreadCoreNet never ran the task before CloseNet")
	}

	CloseNet()
	if coreNet() == before {
		t.Fatal("CloseNet did not replace the pool")
	}
	// 原版重建时用 3 而不是 4, 照搬
	if got := coreNet().Size(); got != 3 {
		t.Fatalf("rebuilt pool size = %d, want 3", got)
	}

	done2 := make(chan struct{})
	NewThreadCoreNet(func() { close(done2) })
	select {
	case <-done2:
	case <-time.After(2 * time.Second):
		t.Fatal("NewThreadCoreNet never ran the task after CloseNet")
	}
}

func TestSavePoolRunsInOrder(t *testing.T) {
	var order []int
	AddSavePool(func() { order = append(order, 1) })
	AddSavePool(func() { order = append(order, 2) })
	AddSavePool(func() { order = append(order, 3) })

	RunSavePool()

	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Fatalf("save pool ran in order %v, want [1 2 3]", order)
	}
}

func TestDataAutoSave(t *testing.T) {
	var ran atomic.Int64
	AddDataAutoSave(func() { ran.Add(1) })
	AddDataAutoSave(func() { ran.Add(1) })

	RunDataAutoSave()

	if ran.Load() != 2 {
		t.Fatalf("data auto save ran %d tasks, want 2", ran.Load())
	}
}

func TestNewCountdownFiresOnce(t *testing.T) {
	var fired atomic.Int64
	NewCountdown(PlayerAfkTask, 30*time.Millisecond, func() { fired.Add(1) })

	if !ContainsTimeTask(PlayerAfkTask) {
		t.Fatal("countdown not registered")
	}

	deadline := time.Now().Add(2 * time.Second)
	for fired.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if fired.Load() != 1 {
		t.Fatalf("fired %d times, want 1", fired.Load())
	}
	// 原版 ClientCommands 的用法: 30 秒后跑一次然后自删
	deadline = time.Now().Add(2 * time.Second)
	for ContainsTimeTask(PlayerAfkTask) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if ContainsTimeTask(PlayerAfkTask) {
		t.Fatal("countdown did not remove itself")
	}
}

func TestNewTimedTaskRepeatsAndCloseTimeTask(t *testing.T) {
	var fired atomic.Int64
	// 原版 CallManage 的用法: 0 秒后开始, 每 1 秒一次
	NewTimedTask(AutoCheckTask, 0, 20*time.Millisecond, func() { fired.Add(1) })

	if !ContainsTimeTask(AutoCheckTask) {
		t.Fatal("timed task not registered")
	}
	time.Sleep(120 * time.Millisecond)
	if fired.Load() < 3 {
		t.Fatalf("fired %d times, want >= 3", fired.Load())
	}

	var closed atomic.Int64
	if !CloseTimeTask(AutoCheckTask, func() { closed.Add(1) }) {
		t.Fatal("CloseTimeTask reported the task did not exist")
	}
	if closed.Load() != 1 {
		t.Fatal("CloseTimeTask did not run the callback")
	}
	if ContainsTimeTask(AutoCheckTask) {
		t.Fatal("task still registered after CloseTimeTask")
	}

	at := fired.Load()
	time.Sleep(80 * time.Millisecond)
	if fired.Load() != at {
		t.Fatalf("task fired %d more times after CloseTimeTask", fired.Load()-at)
	}
	// 第二次关闭应返回 false 且不跑回调
	closed.Store(0)
	if CloseTimeTask(AutoCheckTask, func() { closed.Add(1) }) {
		t.Fatal("second CloseTimeTask should return false")
	}
	if closed.Load() != 0 {
		t.Fatal("second CloseTimeTask ran the callback")
	}
}

func TestPauseUnPauseRoundTrip(t *testing.T) {
	var fired atomic.Int64
	NewTimedTask(CallTeamTask, 0, 30*time.Millisecond, func() { fired.Add(1) })
	for fired.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	Pause(CallTeamTask)
	time.Sleep(120 * time.Millisecond)
	paused := fired.Load()
	if fired.Load() != paused {
		t.Fatal("task fired while paused")
	}

	UnPause(CallTeamTask)
	deadline := time.Now().Add(2 * time.Second)
	for fired.Load() == paused && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if fired.Load() == paused {
		t.Fatal("task did not resume after UnPause")
	}
	CloseTimeTask(CallTeamTask, nil)
}

func TestNamedTaskAPI(t *testing.T) {
	var fired atomic.Int64
	if !NewTimedTaskNamed("n", "g", "d", 0, 20*time.Millisecond, func() { fired.Add(1) }) {
		t.Fatal("NewTimedTaskNamed rejected a fresh key")
	}
	if !ContainsTimeTaskNamed("n", "g") {
		t.Fatal("named task not registered")
	}
	time.Sleep(80 * time.Millisecond)
	if fired.Load() < 2 {
		t.Fatalf("fired %d times, want >= 2", fired.Load())
	}

	PauseNamed("n", "g")
	UnPauseNamed("n", "g")
	if !CloseTimeTaskNamed("n", "g", nil) {
		t.Fatal("CloseTimeTaskNamed reported the task did not exist")
	}
}

func TestDuplicateCountdownIsRejected(t *testing.T) {
	// 原版 Quartz 在重复键上抛 ObjectAlreadyExistsException; Go 侧返回 false
	NewCountdown(GameOverTask, 50*time.Millisecond, func() {})
	defer CloseTimeTask(GameOverTask, nil)

	if NewCountdown(GameOverTask, 50*time.Millisecond, func() {}) {
		t.Fatal("duplicate countdown must be rejected")
	}
	if NewTimedTask(GameOverTask, 0, 20*time.Millisecond, func() {}) {
		t.Fatal("duplicate key must be rejected across task kinds")
	}
}
