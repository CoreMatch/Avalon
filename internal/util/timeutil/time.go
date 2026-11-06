// Package timeutil 对应 net.rwhps.server.util.Time
package timeutil

import "time"

var startTime = time.Now()

// Millis 对应 Time.millis
func Millis() int64 { return time.Now().UnixMilli() }

// Second 对应 Time.second
func Second() int64 { return time.Now().UnixMilli() / 1000 }

// ConcurrentMillis 对应 Time.concurrentMillis
func ConcurrentMillis() int64 { return time.Now().UnixMilli() }

// ConcurrentSecond 对应 Time.concurrentSecond
func ConcurrentSecond() int { return int(time.Now().UnixMilli() / 1000) }

// GetTimeFutureMillis 对应 Time.getTimeFutureMillis: 当前毫秒 + 偏移
func GetTimeFutureMillis(addTime int64) int64 { return Millis() + addTime }

// GetTimeSinceMillis 对应 Time.getTimeSinceMillis: 当前毫秒 - 之前时刻
func GetTimeSinceMillis(prevTime int64) int64 { return Millis() - prevTime }

// GetTimeSinceSecond 对应 Time.getTimeSinceSecond: 当前秒 - 之前时刻
func GetTimeSinceSecond(prevTime int) int { return ConcurrentSecond() - prevTime }

// formatPatterns 对应 Time.format 的格式表, 下标即 fot 参数
var formatPatterns = [...]string{
	"2006-01-02",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05Z",
	"02-01-2006 15:04:05",
	"01-02-2006 15:04:05",
	"2006-01-02_15-04-05",
	"15:04:05",
}

// Format 对应 Time.format(gmt, fot): 按格式表把毫秒时间戳格式化为本地时间字符串
//
// 上游用 SimpleDateFormat(默认时区) 格式化; Go 版同样走本地时区 ——
// gametime 命令用它格式化"已进行时长"时, 显示值会带时区偏移, 与上游一致.
func Format(gmt int64, fot int) string {
	if fot < 0 || fot >= len(formatPatterns) {
		return ""
	}
	return time.UnixMilli(gmt).Format(formatPatterns[fot])
}

// NanoTime 对应 Time.nanoTime
func NanoTime() int64 { return time.Since(startTime).Nanoseconds() }
