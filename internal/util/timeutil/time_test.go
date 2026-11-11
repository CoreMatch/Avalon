package timeutil

import (
	"testing"
	"time"
)

// TestClockSanity 时钟读数与标准库同刻一致 (毫秒级容差)
func TestClockSanity(t *testing.T) {
	now := time.Now().UnixMilli()
	if d := Millis() - now; d < -1000 || d > 1000 {
		t.Fatalf("Millis 偏差 %d ms", d)
	}
	if d := ConcurrentMillis() - now; d < -1000 || d > 1000 {
		t.Fatalf("ConcurrentMillis 偏差 %d ms", d)
	}
	if d := Second()*1000 - now; d < -2000 || d > 2000 {
		t.Fatalf("Second 偏差 %d ms", d)
	}
	if d := int64(ConcurrentSecond())*1000 - now; d < -2000 || d > 2000 {
		t.Fatalf("ConcurrentSecond 偏差 %d ms", d)
	}
}

// TestFutureSince 偏移运算方向正确
func TestFutureSince(t *testing.T) {
	base := Millis()
	if got := GetTimeFutureMillis(500); got < base+500 || got > base+1500 {
		t.Fatalf("GetTimeFutureMillis = %d (base %d)", got, base)
	}
	prev := Millis() - 100
	if got := GetTimeSinceMillis(prev); got < 100 || got > 1100 {
		t.Fatalf("GetTimeSinceMillis = %d", got)
	}
	prevSec := ConcurrentSecond() - 1
	if got := GetTimeSinceSecond(prevSec); got < 1 || got > 2 {
		t.Fatalf("GetTimeSinceSecond = %d", got)
	}
}

// TestFormatPatternTable 格式表按索引钉死 (防表序漂移);
// 期望值用标准库同一模式独立算出, 本地时区两侧一致
func TestFormatPatternTable(t *testing.T) {
	ts := int64(1730000000000) // 2024-10-27 固定时刻
	patterns := []string{
		"2006-01-02",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		"02-01-2006 15:04:05",
		"01-02-2006 15:04:05",
		"2006-01-02_15-04-05",
		"15:04:05",
	}
	for i, p := range patterns {
		want := time.UnixMilli(ts).Format(p)
		if got := Format(ts, i); got != want {
			t.Errorf("Format(ts, %d) = %q, want %q (模式 %q)", i, got, want, p)
		}
	}
	// 越界索引返回空串
	if got := Format(ts, -1); got != "" {
		t.Fatalf("Format 负索引应为空串, got %q", got)
	}
	if got := Format(ts, len(patterns)); got != "" {
		t.Fatalf("Format 越界索引应为空串, got %q", got)
	}
}

// TestNanoTime 单调递增且非负
func TestNanoTime(t *testing.T) {
	a := NanoTime()
	if a < 0 {
		t.Fatal("NanoTime 应非负")
	}
	b := NanoTime()
	if b < a {
		t.Fatal("NanoTime 应单调不减")
	}
}
