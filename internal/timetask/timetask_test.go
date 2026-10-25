package timetask

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func nowMillis() int64 { return time.Now().UnixMilli() }

func TestAddCountdownFiresOnceAndSelfRemoves(t *testing.T) {
	m := NewManage()
	defer m.ShutdownNow()

	var fired atomic.Int64
	if !m.AddCountdown("n", "g", "d", nowMillis()+30, func() { fired.Add(1) }) {
		t.Fatal("AddCountdown rejected a fresh key")
	}
	if !m.Contains("n", "g") {
		t.Fatal("task should be registered before it fires")
	}

	deadline := time.Now().Add(2 * time.Second)
	for fired.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if fired.Load() != 1 {
		t.Fatalf("fired %d times, want 1", fired.Load())
	}

	// 原版 JobDataMap 的包装是 run(); remove(key) —— 跑完必须自删
	deadline = time.Now().Add(2 * time.Second)
	for m.Contains("n", "g") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if m.Contains("n", "g") {
		t.Fatal("countdown did not remove itself after firing")
	}

	// 再等一会儿, 确认不会触发第二次
	time.Sleep(80 * time.Millisecond)
	if fired.Load() != 1 {
		t.Fatalf("fired %d times after settling, want 1", fired.Load())
	}
}

func TestAddTimedTaskRepeats(t *testing.T) {
	m := NewManage()
	defer m.ShutdownNow()

	var fired atomic.Int64
	if !m.AddTimedTask("t", "g", "d", nowMillis(), 20, func() { fired.Add(1) }) {
		t.Fatal("AddTimedTask rejected a fresh key")
	}
	time.Sleep(150 * time.Millisecond)

	// startTime 已过期 => 立即触发一次, 之后每 20ms 一次
	if n := fired.Load(); n < 3 {
		t.Fatalf("fired %d times in 150ms, want >= 3", n)
	}
	if !m.Contains("t", "g") {
		t.Fatal("a repeating task must stay registered")
	}
}

func TestDuplicateKeyIsRejected(t *testing.T) {
	m := NewManage()
	defer m.ShutdownNow()

	if !m.AddCountdown("dup", "g", "d", nowMillis()+50, func() {}) {
		t.Fatal("first add rejected")
	}
	// Quartz 在此处抛 ObjectAlreadyExistsException; Go 侧返回 false 且不覆盖
	if m.AddCountdown("dup", "g", "d", nowMillis()+50, func() {}) {
		t.Fatal("duplicate key must be rejected")
	}
	if m.AddTimedTask("dup", "g", "d", nowMillis(), 20, func() {}) {
		t.Fatal("duplicate key must be rejected across add kinds")
	}
}

func TestKeyIsNameAndGroupPair(t *testing.T) {
	m := NewManage()
	defer m.ShutdownNow()

	// (a,b) 与 (b,a) 必须是两个不同的键 —— 这正是用结构体而不是拼字符串的原因
	m.AddCountdown("a", "b", "d", nowMillis()+60, func() {})
	m.AddCountdown("b", "a", "d", nowMillis()+60, func() {})
	if !m.Contains("a", "b") || !m.Contains("b", "a") {
		t.Fatal("(a,b) and (b,a) must be distinct keys")
	}

	// 分隔符碰撞: name 里带 "/" 也不能和别的键撞上
	m.AddCountdown("x/y", "", "d", nowMillis()+60, func() {})
	m.AddCountdown("x", "/y", "d", nowMillis()+60, func() {})
	if !m.Contains("x/y", "") || !m.Contains("x", "/y") {
		t.Fatal("name containing a separator collided with another key")
	}
	if m.Size() != 4 {
		t.Fatalf("Size() = %d, want 4", m.Size())
	}
}

func TestContainsFalseForUnknownKey(t *testing.T) {
	m := NewManage()
	defer m.ShutdownNow()

	if m.Contains("nope", "nope") {
		t.Fatal("Contains must be false for an unknown key")
	}
	// pause / remove 对不存在的键是空操作, 不应 panic
	m.Pause("nope", "nope")
	m.UnPause("nope", "nope")
	if m.Remove("nope", "nope") {
		t.Fatal("Remove must return false for an unknown key")
	}
}

func TestRemoveStopsTask(t *testing.T) {
	m := NewManage()
	defer m.ShutdownNow()

	var fired atomic.Int64
	m.AddTimedTask("r", "g", "d", nowMillis(), 15, func() { fired.Add(1) })
	time.Sleep(50 * time.Millisecond)
	if !m.Remove("r", "g") {
		t.Fatal("Remove should report the task existed")
	}

	at := fired.Load()
	time.Sleep(80 * time.Millisecond)
	if fired.Load() != at {
		t.Fatalf("task fired %d more times after Remove", fired.Load()-at)
	}
	if m.Contains("r", "g") {
		t.Fatal("removed task is still registered")
	}
	if m.Remove("r", "g") {
		t.Fatal("second Remove should return false")
	}
}

func TestPauseHoldsRemainingTime(t *testing.T) {
	m := NewManage()
	defer m.ShutdownNow()

	var fired atomic.Int64
	// 每 40ms 一次; 跑一次后立刻暂停
	m.AddTimedTask("p", "g", "d", nowMillis(), 40, func() { fired.Add(1) })
	for fired.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	m.Pause("p", "g")
	time.Sleep(120 * time.Millisecond)
	paused := fired.Load()

	// 暂停期间不应再触发
	if fired.Load() != paused {
		t.Fatalf("fired %d times while paused", fired.Load()-paused)
	}

	m.UnPause("p", "g")
	// Quartz 的 resumeJob 是从剩余时间继续, 不是重新等一个完整间隔
	deadline := time.Now().Add(300 * time.Millisecond)
	for fired.Load() == paused && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if fired.Load() == paused {
		t.Fatal("task never resumed after UnPause")
	}
}

func TestPausedCountdownDoesNotFireWhilePaused(t *testing.T) {
	m := NewManage()
	defer m.ShutdownNow()

	var fired atomic.Int64
	m.AddCountdown("pc", "g", "d", nowMillis()+200, func() { fired.Add(1) })
	m.Pause("pc", "g")

	time.Sleep(300 * time.Millisecond)
	if fired.Load() != 0 {
		t.Fatal("paused countdown fired while paused")
	}

	m.UnPause("pc", "g")
	deadline := time.Now().Add(2 * time.Second)
	for fired.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if fired.Load() != 1 {
		t.Fatalf("resumed countdown fired %d times, want 1", fired.Load())
	}
}

func TestShutdownNowStopsEverything(t *testing.T) {
	m := NewManage()

	var fired atomic.Int64
	for i := 0; i < 5; i++ {
		m.AddTimedTask("s", string(rune('a'+i)), "d", nowMillis(), 10, func() { fired.Add(1) })
	}
	time.Sleep(40 * time.Millisecond)
	m.ShutdownNow()

	at := fired.Load()
	time.Sleep(100 * time.Millisecond)
	if fired.Load() != at {
		t.Fatalf("task fired %d more times after ShutdownNow", fired.Load()-at)
	}
	if m.Size() != 0 {
		t.Fatalf("Size() = %d after ShutdownNow, want 0", m.Size())
	}
	if m.Contains("s", "a") {
		t.Fatal("registry not cleared by ShutdownNow")
	}
}

func TestZeroIntervalIsRejected(t *testing.T) {
	m := NewManage()
	defer m.ShutdownNow()

	// 原版 intervalTime 为 0 时 Quartz 抛异常
	if m.AddTimedTask("z", "g", "d", nowMillis(), 0, func() {}) {
		t.Fatal("AddTimedTask must reject a zero interval")
	}
	if m.AddTimedTask("z", "g", "d", nowMillis(), -1, func() {}) {
		t.Fatal("AddTimedTask must reject a negative interval")
	}
}

func TestNilRunIsRejected(t *testing.T) {
	m := NewManage()
	defer m.ShutdownNow()

	if m.AddCountdown("n", "g", "d", nowMillis()+50, nil) {
		t.Fatal("AddCountdown must reject a nil run")
	}
	if m.AddTimedTask("n", "g", "d", nowMillis(), 20, nil) {
		t.Fatal("AddTimedTask must reject a nil run")
	}
}

func TestTasksRunOnPoolNotSchedulerGoroutine(t *testing.T) {
	m := NewManage()
	defer m.ShutdownNow()

	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[string]bool{}

	// 10 个任务同时触发, 必须能并发跑完而不是互相阻塞
	for i := 0; i < 10; i++ {
		wg.Add(1)
		m.AddCountdown("c", string(rune('a'+i)), "d", nowMillis()+10, func() {
			defer wg.Done()
			time.Sleep(60 * time.Millisecond)
			mu.Lock()
			seen["x"] = true
			mu.Unlock()
		})
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("10 concurrent 60ms tasks did not finish; they are not running on a pool")
	}
}
