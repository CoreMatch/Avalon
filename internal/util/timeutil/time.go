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

// NanoTime 对应 Time.nanoTime
func NanoTime() int64 { return time.Since(startTime).Nanoseconds() }
