// Package timetask 对应 net.rwhps.timetask.task.TimeTaskManage
//
// 原版基于 Quartz (org.quartz.Scheduler + StdSchedulerFactory), 这是一个第三方
// Java 库, 无法机械移植. 这里按它的**可观测语义**用 Go 重写:
//
//	以 (name, group) 为键的任务注册表
//	一次性倒计时        addCountdown
//	周期任务            addTimedTask
//	存在性查询          contains
//	暂停 / 恢复         pause / unPause
//	移除                remove
//
// # 与 Quartz 的有意偏差
//
//   - 没有 misfire 策略. Quartz 的 SimpleTrigger 默认
//     MISFIRE_INSTRUCTION_SMART_POLICY: 周期任务错过触发点时会立刻补一次然后
//     重新排程. Go 侧用 time.Timer, 错过就跳过, 不补. 对本项目的用法
//     (1 秒级的心跳任务) 影响是: 进程卡顿超过一个间隔时会少跑几次, 而不是补跑.
//   - 没有 cron. 原版 addTimedTask 有一个 cron 重载, 全代码库零调用方, 不移植.
//   - 重复添加同一个键, Quartz 抛 ObjectAlreadyExistsException. Go 侧
//     AddCountdown / AddTimedTask 返回 false, 不 panic.
//   - 无法 interrupt 正在执行的任务, 与 util/concurrent/threads 的限制相同.
//
// 其余语义 (键空间、绝对时间触发、倒计时跑完自删、暂停保留剩余时间) 与原版一致.
package timetask

import (
	"sync"
	"time"

	"rwhps/internal/util/concurrent/threads"
)

// poolSize 对应 Quartz 默认线程池的 threadCount = 10
//
// 原版 Threads 里那句 `// 10 Thread` 说的就是这个.
const poolSize = 10

// taskKey 对应 org.quartz.JobKey (name + group 唯一确定一个任务)
//
// 用结构体而不是拼字符串: 避免 name 里出现分隔符时发生键碰撞.
type taskKey struct {
	name  string
	group string
}

// Manage 对应 TimeTaskManage
type Manage struct {
	mu    sync.Mutex
	tasks map[taskKey]*task

	// pool 对应 Quartz 的 SimpleThreadPool: 任务在池里执行, 不占调度线程
	pool *threads.Pool
}

// NewManage 对应 TimeTaskManage()
//
// 原版 init 里会 scheduler.start(); 这里创建时即启动 worker 池.
func NewManage() *Manage {
	return &Manage{
		tasks: make(map[taskKey]*task),
		pool:  threads.NewFixedThreadPool(poolSize, "TimeTask-"),
	}
}

// AddCountdown 对应 addCountdown(name, group, description, startTime, runnable)
//
// startTime 是**绝对时间** (Unix 毫秒). 原版 Threads.newCountdown 会先把它算成
// Time.concurrentMillis() + 换算后的毫秒数 再传进来, 这里保持同样的约定.
//
// 跑完之后任务会把自己从注册表里移除 —— 对应原版 JobDataMap 里那个
// `runnable.run(); remove(key)` 包装, 不是额外加的优化.
//
// 返回 false 表示键已存在 (Quartz 在此处抛 ObjectAlreadyExistsException).
func (m *Manage) AddCountdown(name, group, description string, startTime int64, run func()) bool {
	return m.add(taskKey{name, group}, description, startTime, 0, run)
}

// AddTimedTask 对应 addTimedTask(name, group, description, startTime, intervalTime, runnable)
//
// startTime 是首次触发的绝对时间 (Unix 毫秒), intervalTime 是间隔毫秒数.
// 原版的 intervalTime 为 0 时 Quartz 会抛异常, 这里同样视为非法.
func (m *Manage) AddTimedTask(name, group, description string, startTime, intervalTime int64, run func()) bool {
	if intervalTime <= 0 {
		return false
	}
	return m.add(taskKey{name, group}, description, startTime, intervalTime, run)
}

func (m *Manage) add(k taskKey, description string, startTime, intervalTime int64, run func()) bool {
	if run == nil {
		return false
	}

	m.mu.Lock()
	if _, exists := m.tasks[k]; exists {
		m.mu.Unlock()
		return false
	}
	t := &task{
		key:         k,
		description: description,
		startTime:   startTime,
		interval:    intervalTime,
		run:         run,
		manager:     m,
		pauseCh:     make(chan struct{}, 1),
		resumeCh:    make(chan struct{}, 1),
		stopCh:      make(chan struct{}, 1),
	}
	m.tasks[k] = t
	m.mu.Unlock()

	go t.loop()
	return true
}

// Contains 对应 contains(name, group)
func (m *Manage) Contains(name, group string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.tasks[taskKey{name, group}]
	return ok
}

// Pause 对应 pause(name, group)
//
// 对不存在的键是空操作 (Quartz 的 pauseJob 同样如此).
func (m *Manage) Pause(name, group string) {
	if t := m.get(name, group); t != nil {
		t.pause()
	}
}

// UnPause 对应 unPause(name, group)
func (m *Manage) UnPause(name, group string) {
	if t := m.get(name, group); t != nil {
		t.unPause()
	}
}

// Remove 对应 remove(name, group)
//
// 返回该键此前是否存在. 原版在 remove 里会先 pause 再 unschedule 再 deleteJob,
// 这里合成一步: 停掉调度协程并从注册表删除.
func (m *Manage) Remove(name, group string) bool {
	k := taskKey{name, group}

	m.mu.Lock()
	t, ok := m.tasks[k]
	if ok {
		delete(m.tasks, k)
	}
	m.mu.Unlock()

	if ok {
		t.stop()
	}
	return ok
}

// ShutdownNow 对应 shutdownNow()
func (m *Manage) ShutdownNow() {
	m.mu.Lock()
	tasks := make([]*task, 0, len(m.tasks))
	for k, t := range m.tasks {
		tasks = append(tasks, t)
		delete(m.tasks, k)
	}
	m.mu.Unlock()

	for _, t := range tasks {
		t.stop()
	}
	m.pool.ShutdownNow()
}

func (m *Manage) get(name, group string) *task {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tasks[taskKey{name, group}]
}

// Size 对应 serverRelayDataRoom.size 那样的统计用途, 返回当前任务数
func (m *Manage) Size() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tasks)
}

// task 是一个已排程的任务, 由独占的调度协程驱动.
type task struct {
	key         taskKey
	description string

	// startTime: 首次触发的绝对时间 (Unix 毫秒)
	// interval: 0 表示一次性倒计时, > 0 表示周期任务
	startTime int64
	interval  int64

	run     func()
	manager *Manage

	mu     sync.Mutex
	timer  *time.Timer
	fireAt time.Time

	// pausedRemaining 是 doPause 记下的剩余时间.
	// doPause / doResume 在同一把锁内成对出现, 因此不需要额外的同步.
	pausedRemaining time.Duration

	pauseCh  chan struct{}
	resumeCh chan struct{}
	stopCh   chan struct{}
}

// loop 是任务的调度协程: 自己持有一个 time.Timer, 不占用 worker 池.
func (t *task) loop() {
	t.mu.Lock()
	t.fireAt = time.UnixMilli(t.startTime)
	t.timer = time.NewTimer(time.Until(t.fireAt))
	t.mu.Unlock()

	for {
		select {
		case <-t.timer.C:
			if !t.onFire() {
				return
			}
		case <-t.pauseCh:
			t.doPause()
			// 暂停期间只等恢复或停止
			select {
			case <-t.resumeCh:
				t.doResume()
			case <-t.stopCh:
				return
			}
		case <-t.stopCh:
			return
		}
	}
}

// onFire 触发一次任务; 返回 false 表示这条协程可以结束了 (倒计时跑完).
func (t *task) onFire() bool {
	t.mu.Lock()
	interval := t.interval
	t.mu.Unlock()

	if interval == 0 {
		// 倒计时: 跑完自删, 与原版 JobDataMap 里的包装一致 ——
		// 先 run 再 remove, 所以回调执行期间 contains 仍然为 true.
		run := t.run
		m := t.manager
		k := t.key
		m.pool.Execute(func() {
			run()
			m.Remove(k.name, k.group)
		})
		return false
	}

	t.mu.Lock()
	t.fireAt = time.Now().Add(time.Duration(interval) * time.Millisecond)
	t.timer.Reset(time.Duration(interval) * time.Millisecond)
	t.mu.Unlock()

	// 周期任务直接提交, 不等它跑完再排下一次
	t.manager.pool.Execute(t.run)
	return true
}

// doPause 对应 Quartz pauseJob: 停表并记下剩余时间.
func (t *task) doPause() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.timer.Stop() {
		// 已经触发过但还没被 select 取走, 把通道排空, 否则恢复后会立刻误触发
		select {
		case <-t.timer.C:
		default:
		}
	}
	remaining := time.Until(t.fireAt)
	if remaining < 0 {
		remaining = 0
	}
	t.pausedRemaining = remaining
	t.fireAt = time.Time{} // 清零表示"已暂停, 无计划触发时间"
}

// doResume 对应 Quartz resumeJob: 从剩余时间继续, 不是从完整间隔重新开始.
func (t *task) doResume() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.fireAt = time.Now().Add(t.pausedRemaining)
	t.timer.Reset(t.pausedRemaining)
}

// pausedRemaining 是 doPause 存下来的剩余时间, 单独一个字段避免和 fireAt 抢锁.
// 由于 doPause / doResume 都在同一把锁内成对出现, 读它不需要额外同步.

func (t *task) pause() {
	select {
	case t.pauseCh <- struct{}{}:
	default:
	}
}

func (t *task) unPause() {
	select {
	case t.resumeCh <- struct{}{}:
	default:
	}
}

func (t *task) stop() {
	select {
	case t.stopCh <- struct{}{}:
	default:
	}
}
