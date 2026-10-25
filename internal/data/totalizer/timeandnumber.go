// Package totalizer 对应 net.rwhps.server.data.totalizer
package totalizer

import (
	"sync"

	"rwhps/internal/util/timeutil"
)

// TimeAndNumber 对应 TimeAndNumber
// 指定时间 timeOut 内累加超过 countMax 即返回 false
type TimeAndNumber struct {
	timeOut  int
	countMax int

	mu        sync.Mutex
	startTime int
	count     int
}

// NewTimeAndNumber 对应 TimeAndNumber(timeOut, conutMax)
func NewTimeAndNumber(timeOut, countMax int) *TimeAndNumber {
	return &TimeAndNumber{timeOut: timeOut, countMax: countMax}
}

// Count 对应 count (get)
func (t *TimeAndNumber) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.count
}

// SetCount 对应 count (set)
func (t *TimeAndNumber) SetCount(v int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if v == 0 {
		t.startTime = timeutil.ConcurrentSecond()
	}
	t.count = v
}

// Add 对应 count++ 语义
func (t *TimeAndNumber) Add(delta int) {
	t.SetCount(t.Count() + delta)
}

// CheckStatus 对应 checkStatus
func (t *TimeAndNumber) CheckStatus() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.count < t.countMax {
		return true
	}
	if (timeutil.ConcurrentSecond() - t.startTime) > t.timeOut {
		// 原版这里调用 reset(), 而 reset 会把 startTime 打成当前秒 (见 Reset 的注释),
		// 不是置 0. 照抄成 startTime = 0 会让窗口判定永远失效.
		t.startTime = timeutil.ConcurrentSecond()
		t.count = 0
		return true
	}
	return false
}

// Reset 对应 reset
//
// 原版的赋值顺序是 `startTime = 0` 然后 `count = 0`, 而 count = 0 会经由属性委托的
// set 回调 (ReadWriteConcurrentSynchronize 的 set lambda) 把 startTime 重新打成
// 当前秒. 净效果是 startTime = now, 不是 0.
//
// 这里必须显式打戳: 直接写字段会让 startTime 停在 0 (Unix 纪元), 之后任何一次
// CheckStatus 都会认为"窗口早过期了"而走 reset 分支返回 true —— 限流器形同虚设.
func (t *TimeAndNumber) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.startTime = timeutil.ConcurrentSecond()
	t.count = 0
}
