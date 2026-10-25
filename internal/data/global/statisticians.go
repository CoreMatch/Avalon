// Package global 对应 net.rwhps.server.data.global
package global

import (
	"sync"

	"rwhps/internal/util/log"
	"rwhps/internal/util/timeutil"
)

// 统计计时器, 对应 Statisticians
var (
	statMu    sync.Mutex
	statTimes = map[string]int64{}
)

// AddTime 对应 Statisticians.addTime: 绑定一个起始时间戳 (毫秒).
// 原版重复添加会抛 RepeatAddException; Go 侧记录错误并保留首次时间, 避免因计时器
// 重复注册直接终止服务.
func AddTime(name string) {
	statMu.Lock()
	defer statMu.Unlock()
	if _, ok := statTimes[name]; ok {
		log.Errorf("[Statisticians.Time]", "Add {0}", name)
		return
	}
	statTimes[name] = timeutil.Millis()
}

// ComputeTime 对应 Statisticians.computeTime: 返回与起始时间的差值 (毫秒) 并移除计时器
func ComputeTime(name string) int64 {
	statMu.Lock()
	defer statMu.Unlock()
	start, ok := statTimes[name]
	if !ok {
		return 0
	}
	delete(statTimes, name)
	return timeutil.Millis() - start
}
