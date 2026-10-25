package thread

import (
	"time"

	"rwhps/internal/struct/list"
	"rwhps/internal/timetask"
	"rwhps/internal/util/concurrent/threads"
	"rwhps/internal/util/timeutil"
)

// Threads 对应 object Threads
//
// 原版是个 Kotlin object (全局单例), Go 侧落成包级变量 + 函数, 语义等价.
// 所有池子在包初始化时创建, 对应原版对象首次被访问时初始化.
//
// # 池子大小
//
//	coreThread     3 条  "Core-"
//	coreNetThread  4 条  "Core-Net-"   (closeNet 后会换成 3 条, 照搬原版)
//	otherThread    10 条 "Other-"
//	service        10 条 来自 timetask (Quartz SimpleThreadPool 默认值)
var (
	coreThread    *threads.Pool
	coreNetThread *threads.Pool
	otherThread   *threads.Pool
	service       *timetask.Manage

	// savePool 对应 SAVE_POOL: Seq<Runnable>(), 非线程安全.
	// 原版的添加点都在启动期 (Administration / LibraryManager), 运行期在退出时
	// 一次性跑完, 因此不加锁是与原版一致的选择.
	savePool *list.Seq[func()]

	// dataAutoSave 对应 dataAutoSave: Seq<Runnable>(8)
	dataAutoSave *list.Seq[func()]
)

func init() {
	coreThread = threads.NewFixedThreadPool(3, "Core-")
	coreNetThread = threads.NewFixedThreadPool(4, "Core-Net-")
	service = timetask.NewManage()
	otherThread = threads.NewFixedThreadPool(10, "Other-")

	savePool = list.NewSeq[func()]()
	dataAutoSave = list.NewSeqCap[func()](8, false)
}

// Close 对应 close(): 关闭全部线程池
func Close() {
	coreThread.ShutdownNow()
	coreNetThread.ShutdownNow()
	service.ShutdownNow()
	otherThread.ShutdownNow()
}

// CloseNet 对应 closeNet(): 关闭网络线程池并重建
//
// 注意原版重建时用的是 3 而不是初始的 4 —— 这是个看起来像笔误的行为, 照搬.
func CloseNet() {
	coreNetThread.ShutdownNow()
	coreNetThread = threads.NewFixedThreadPool(3, "Core-Net-")
}

// NewThreadOther 对应 newThreadOther(run): 杂物线程
func NewThreadOther(run func()) { otherThread.Execute(run) }

// NewThreadCore 对应 newThreadCore(run): 核心线程池
func NewThreadCore(run func()) { coreThread.Execute(run) }

// NewThreadCoreNet 对应 newThreadCoreNet(run): 网络线程池
func NewThreadCoreNet(run func()) { coreNetThread.Execute(run) }

// AddSavePool 对应 addSavePool(run): 注册一个退出时执行的任务
func AddSavePool(run func()) { savePool.Add(run) }

// RunSavePool 对应 runSavePool()
func RunSavePool() { savePool.EachAll(func(run func()) { run() }) }

// AddDataAutoSave 对应 dataAutoSave.add(run)
func AddDataAutoSave(run func()) { dataAutoSave.Add(run) }

// RunDataAutoSave 对应 dataAutoSave.eachAll(Runnable::run)
func RunDataAutoSave() { dataAutoSave.EachAll(func(run func()) { run() }) }

// NewCountdown 对应 newCountdown(taskFlag, endTime, timeUnit, run)
//
// endTime 是时长 (原版是 Int + TimeUnit 两个参数, Go 侧合成一个 time.Duration).
// 内部换算成绝对毫秒时间戳, 与原版
// `Time.concurrentMillis() + TimeUnit.MILLISECONDS.convert(endTime, timeUnit)` 一致.
//
// 返回值: false 表示键已存在, 任务没有排上. 原版此处返回 Unit, 但 Quartz 在重复键上
// 会抛 ObjectAlreadyExistsException (未受检异常), 直接沿调用栈往上炸. Go 没有异常,
// 静默丢弃会让"本该排上的任务没排上"变得无法察觉, 因此选择返回 bool 让调用方能够检测.
func NewCountdown(flag CallTimeTask, endTime time.Duration, run func()) bool {
	return NewCountdownNamed(flag.String(), flag.Group(), flag.Description(), endTime, run)
}

// NewCountdownNamed 对应 newCountdown(name, group, description, endTime, timeUnit, run)
func NewCountdownNamed(name, group, description string, endTime time.Duration, run func()) bool {
	return service.AddCountdown(name, group, description, timeutil.ConcurrentMillis()+endTime.Milliseconds(), run)
}

// NewTimedTask 对应 newTimedTask(taskFlag, startTime, intervalTime, timeUnit, run)
//
// startTime 是"多久之后开始", intervalTime 是"执行间隔", 都是时长.
// 原版内部同样换算成绝对毫秒时间戳. 返回值语义同 NewCountdown.
func NewTimedTask(flag CallTimeTask, startTime, intervalTime time.Duration, run func()) bool {
	return NewTimedTaskNamed(flag.String(), flag.Group(), flag.Description(), startTime, intervalTime, run)
}

// NewTimedTaskNamed 对应 newTimedTask(name, group, description, startTime, intervalTime, timeUnit, run)
func NewTimedTaskNamed(name, group, description string, startTime, intervalTime time.Duration, run func()) bool {
	now := timeutil.ConcurrentMillis()
	return service.AddTimedTask(name, group, description, now+startTime.Milliseconds(), intervalTime.Milliseconds(), run)
}

// ContainsTimeTask 对应 containsTimeTask(taskFlag)
func ContainsTimeTask(flag CallTimeTask) bool {
	return service.Contains(flag.String(), flag.Group())
}

// ContainsTimeTaskNamed 对应 containsTimeTask(name, group)
func ContainsTimeTaskNamed(name, group string) bool { return service.Contains(name, group) }

// Pause 对应 pause(taskFlag)
func Pause(flag CallTimeTask) { service.Pause(flag.String(), flag.Group()) }

// PauseNamed 对应 pause(name, group)
func PauseNamed(name, group string) { service.Pause(name, group) }

// UnPause 对应 unPause(taskFlag)
func UnPause(flag CallTimeTask) { service.UnPause(flag.String(), flag.Group()) }

// UnPauseNamed 对应 unPause(name, group)
func UnPauseNamed(name, group string) { service.UnPause(name, group) }

// CloseTimeTask 对应 closeTimeTask(taskFlag, run)
func CloseTimeTask(flag CallTimeTask, run func()) bool {
	return CloseTimeTaskNamed(flag.String(), flag.Group(), run)
}

// CloseTimeTaskNamed 对应 closeTimeTask(name, group, run)
//
// 原版语义: 移除成功才跑 run, 并返回移除结果.
func CloseTimeTaskNamed(name, group string, run func()) bool {
	if service.Remove(name, group) {
		if run != nil {
			run()
		}
		return true
	}
	return false
}

// coreNet 仅供包内测试使用, 用于验证 closeNet 换掉了池子
func coreNet() *threads.Pool { return coreNetThread }
