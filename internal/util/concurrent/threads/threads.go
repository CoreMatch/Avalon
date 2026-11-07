// Package threads 对应 上游 util.concurrent.threads
//
// 只移植了 GetNewThreadPool 里有调用方的两个工厂:
//
//	getNewFixedThreadPool(nThreads, name)  -> NewFixedThreadPool
//	nameThreadFactory(name)                -> NameThreadFactory
//
// 原版的另外三个工厂没有移植, 原因分别是:
//
//	getNewSingleThreadExecutor / getNewScheduledThreadPool
//	    全代码库零调用方. 不为假设的调用方写 API.
//	getEventLoopGroup
//	    返回 Netty 的 EventLoopGroup, Go 侧的网络层已经用自己的实现覆盖了这块,
//	    再包一层 Netty 形状的接口没有意义.
package threads

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CoreMatch/Avalon/internal/util/log"
)

// ThreadFactoryName 对应 ThreadFactoryName.nameThreadFactory(name)
//
// 原版给每条线程起名 `name + tag` (tag 从 1 开始自增), 并把
// Log.errorDispose 装成 uncaughtExceptionHandler.
//
// Go 的 goroutine 没有名字, 也没有"未捕获异常"这个概念, 所以这个工厂退化成
// 一个**只用于日志的序号生成器**: 每条 worker 启动时取一个序号, panic 时把它
// 拼进错误信息. 行为上等价于原版的"线程名 + 未捕获异常处理器", 只是名字不再
// 能被运行时读取.
type ThreadFactoryName struct {
	name string
	tag  atomic.Int64
}

// NameThreadFactory 对应 nameThreadFactory(name)
func NameThreadFactory(name string) *ThreadFactoryName {
	return &ThreadFactoryName{name: name}
}

// Next 对应 tag.getAndIncrement(): 返回下一条线程的名字
func (f *ThreadFactoryName) Next() string {
	return fmt.Sprintf("%s%d", f.name, f.tag.Add(1))
}

// Name 返回工厂的名字前缀
func (f *ThreadFactoryName) Name() string { return f.name }

// Pool 对应 getNewFixedThreadPool(nThreads, name) 返回的 ThreadPoolExecutor
//
// 原版参数: core = max = nThreads, keepAlive = 0ms, 无界 LinkedBlockingQueue,
// 自定义 ThreadFactory. 翻译成 Go 就是"n 条常驻 worker + 一条无界 FIFO 队列".
//
// # 与原版的差异
//
//   - shutdownNow() 在原版会 interrupt 正在执行的任务; Go 无法中断 goroutine,
//     因此这里只保证"不再取出新任务 + 丢弃队列里还没开始的任务", 正在跑的那一条
//     会跑完. 原版 interrupt 之后业务代码同样要靠 isInterrupted 主动退出, 实际
//     效果接近.
//   - 关闭后再提交任务, 原版抛 RejectedExecutionException (RuntimeException,
//     会沿着调用栈往上炸). Go 侧改为 Execute 返回 false, 不 panic —— 关机竞态
//     不该把整个进程带下去. 调用方可以按需忽略.
type Pool struct {
	factory *ThreadFactoryName
	size    int

	mu       sync.Mutex
	nonEmpty *sync.Cond
	queue    []func()
	stopped  bool

	// inFlight 是已取出尚未执行完的任务数, 与 queue 一起构成"排空"判据
	// (队列空只说明没有排队任务, 还可能有任务正在 worker 手里执行).
	inFlight int
	// idle 在池子排空 (queue 空且 inFlight==0) 或关闭时广播, 供 WaitIdle 等待
	idle *sync.Cond

	workers sync.WaitGroup
}

// NewFixedThreadPool 对应 getNewFixedThreadPool(nThreads, name)
//
// n <= 0 时按 1 处理: 原版 ThreadPoolExecutor 要求 corePoolSize > 0, 会直接抛
// IllegalArgumentException, 这里取更保守的下限.
func NewFixedThreadPool(n int, name string) *Pool {
	if n <= 0 {
		n = 1
	}
	p := &Pool{
		factory: NameThreadFactory(name),
		size:    n,
	}
	p.nonEmpty = sync.NewCond(&p.mu)
	p.idle = sync.NewCond(&p.mu)
	p.workers.Add(n)
	for i := 0; i < n; i++ {
		go p.worker()
	}
	return p
}

// Size 对应 getPoolSize()
func (p *Pool) Size() int { return p.size }

// Execute 对应 execute(runnable)
//
// 返回 false 表示线程池已经关闭, 任务被丢弃 (原版在此处抛
// RejectedExecutionException).
func (p *Pool) Execute(task func()) bool {
	if task == nil {
		// 原版 execute(null) 抛 NullPointerException
		return false
	}
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return false
	}
	p.queue = append(p.queue, task)
	p.nonEmpty.Signal()
	p.mu.Unlock()
	return true
}

// ShutdownNow 对应 shutdownNow()
//
// 返回**还没有开始执行**就被丢弃的任务, 与原版的返回值语义一致
// (List<Runnable> tasks that never commenced). 正在执行的任务不返回, 也杀不掉.
//
// 注意原版的 shutdownNow() 同样是**立即返回**的: 它只发起停机, 不等任务跑完
// (要等得另外调 awaitTermination). 所以这里也不阻塞 —— 正在执行的那条任务会
// 自己跑完之后让 worker 退出.
func (p *Pool) ShutdownNow() []func() {
	p.mu.Lock()
	p.stopped = true
	dropped := p.queue
	p.queue = nil
	p.nonEmpty.Broadcast()
	p.idle.Broadcast()
	p.mu.Unlock()

	return dropped
}

// WaitIdle 阻塞到队列清空且没有任务正在执行, 返回 true; 超时仍未排空返回 false.
// timeout <= 0 表示不设上限.
//
// 对应原版 ExecutorService.awaitTermination 的"等待在途任务"语义, 但不要求先
// shutdown —— 本池 worker 常驻, 测试用它等待任务排空后再恢复全局状态.
// ShutdownNow 之后排队任务已被丢弃, 此处只等在执行的任务收尾.
func (p *Pool) WaitIdle(timeout time.Duration) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	var deadline time.Time
	hasDeadline := timeout > 0
	if hasDeadline {
		deadline = time.Now().Add(timeout)
		// sync.Cond 没有带超时的等待, 到点广播一次让自己醒来复查
		timer := time.AfterFunc(timeout, func() {
			p.mu.Lock()
			p.idle.Broadcast()
			p.mu.Unlock()
		})
		defer timer.Stop()
	}

	for len(p.queue) > 0 || p.inFlight > 0 {
		p.idle.Wait()
		if hasDeadline && !time.Now().Before(deadline) {
			break
		}
	}
	return len(p.queue) == 0 && p.inFlight == 0
}

// worker 是一条常驻 worker: 从队列头部取任务执行, 队列空且未关闭时等待.
func (p *Pool) worker() {
	defer p.workers.Done()

	// 对应 ThreadFactoryName 里装的 Log.errorDispose:
	// 原版是线程的 uncaughtExceptionHandler, Go 侧是 goroutine 顶层的 recover.
	name := p.factory.Next()
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("[Error] %s", name, fmt.Errorf("%v", r))
		}
	}()

	for {
		p.mu.Lock()
		for len(p.queue) == 0 && !p.stopped {
			p.nonEmpty.Wait()
		}
		if len(p.queue) == 0 {
			// 已关闭且队列已排空
			p.mu.Unlock()
			return
		}
		task := p.queue[0]
		p.queue[0] = nil // 及时释放闭包引用
		p.queue = p.queue[1:]
		p.inFlight++
		p.mu.Unlock()

		p.runOne(task)
	}
}

// runOne 执行单条任务并核销在途计数.
// 计数核销放在 defer 里: 任务 panic 时 (顶层 recover 接住前) 也必须核销,
// 否则 WaitIdle 永远等不到排空.
func (p *Pool) runOne(task func()) {
	defer func() {
		p.mu.Lock()
		p.inFlight--
		if p.inFlight == 0 && len(p.queue) == 0 {
			p.idle.Broadcast()
		}
		p.mu.Unlock()
	}()
	task()
}
