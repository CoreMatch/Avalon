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
		t.startTime = 0
		t.count = 0
		return true
	}
	return false
}

// Reset 对应 reset
func (t *TimeAndNumber) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.startTime = 0
	t.count = 0
}
