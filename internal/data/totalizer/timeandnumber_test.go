package totalizer

import (
	"testing"

	"rwhps/internal/util/timeutil"
)

// TestResetStampsStartTime 锁住 Reset 的打戳行为.
//
// 原版 reset() 是 `startTime = 0; count = 0`, 而 count = 0 会经由属性委托的 set
// 回调把 startTime 打成当前秒. 净效果 startTime = now.
//
// Go 版曾经直接写字段 (startTime = 0; count = 0), 跳过了这个副作用, 导致 startTime
// 永远停在 Unix 纪元 —— 之后任何一次 CheckStatus 都认为窗口早过期, 限流器失效.
// 这个测试就是为了防止再退回去.
func TestResetStampsStartTime(t *testing.T) {
	c := NewTimeAndNumber(10, 3)

	// 构造后 startTime 是 0, 此时冲到上限会走"窗口过期"分支返回 true
	if c.startTime != 0 {
		t.Fatalf("构造后 startTime = %d, 期望 0", c.startTime)
	}
	c.SetCount(3)
	if !c.CheckStatus() {
		t.Error("未打戳过的计数器冲到上限应返回 true")
	}

	// Reset 之后 startTime 必须是当前秒, 窗口内才拦得住
	c.Reset()
	if c.startTime == 0 {
		t.Fatal("Reset 后 startTime 应被打成当前秒, 实际仍是 0")
	}
	if c.Count() != 0 {
		t.Errorf("Reset 后 Count = %d, 期望 0", c.Count())
	}
	c.SetCount(3)
	if c.CheckStatus() {
		t.Error("Reset 后窗口内冲到上限, CheckStatus 应为 false")
	}
}

// TestSetCountZeroStampsStartTime count 被设为 0 时同样打戳 (对应 Kotlin set lambda)
func TestSetCountZeroStampsStartTime(t *testing.T) {
	c := NewTimeAndNumber(10, 3)

	c.SetCount(0)
	if c.startTime == 0 {
		t.Fatal("SetCount(0) 后 startTime 应被打成当前秒")
	}

	c.SetCount(3)
	if c.CheckStatus() {
		t.Error("SetCount(0) 打戳后窗口内应能拦住")
	}
}

// TestCheckStatusBelowMax 计数未到上限时永远放行, 与 startTime 无关
func TestCheckStatusBelowMax(t *testing.T) {
	c := NewTimeAndNumber(10, 3)

	for _, n := range []int{0, 1, 2} {
		c.SetCount(n)
		if !c.CheckStatus() {
			t.Errorf("count = %d 小于上限, CheckStatus 应为 true", n)
		}
	}
}

// TestCheckStatusExpiredWindowResets 窗口过期后自动重置并放行
func TestCheckStatusExpiredWindowResets(t *testing.T) {
	c := NewTimeAndNumber(10, 1)

	// 直接把 startTime 推到很久以前, 避免依赖真实时钟推进
	c.startTime = timeutil.ConcurrentSecond() - 100
	c.SetCount(1)

	if !c.CheckStatus() {
		t.Error("窗口已过期时应返回 true")
	}
	// 过期分支走完会重置 (并把 startTime 重新打成当前秒), 计数归零
	if c.Count() != 0 {
		t.Errorf("过期分支执行后 Count = %d, 期望 0", c.Count())
	}
	if c.startTime == 0 {
		t.Error("过期分支应把 startTime 打成当前秒")
	}
}

// TestAddNeverStampsStartTime 记录一个原版的隐蔽行为.
//
// count 只在被设为 0 时打戳 startTime, 而 Add/累加永远不会把 count 设成 0.
// 因此纯累加路径上 startTime 一直是 0 (Unix 纪元), 计数器冲到上限时
// CheckStatus 会认为"窗口早过期了"而返回 true —— 限不住.
//
// 原版如此 (GameVersionRelay 里的 playerSyncCount.count++ 正是这条路), 照搬.
// 写下这个测试是为了让行为可见, 而不是暗示它是对的.
func TestAddNeverStampsStartTime(t *testing.T) {
	c := NewTimeAndNumber(10, 2)

	c.Add(1)
	c.Add(1)
	if c.Count() != 2 {
		t.Errorf("Count = %d, 期望 2", c.Count())
	}
	if c.startTime != 0 {
		t.Errorf("纯累加路径 startTime 应保持 0, 实际 %d", c.startTime)
	}
	if !c.CheckStatus() {
		t.Error("未打戳过的计数器冲到上限应返回 true (原版行为, 见测试注释)")
	}
}
