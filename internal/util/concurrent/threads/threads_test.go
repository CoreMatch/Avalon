package threads

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewFixedThreadPoolRunsTasks(t *testing.T) {
	p := NewFixedThreadPool(4, "Test-")
	defer p.ShutdownNow()

	var mu sync.Mutex
	var got []int
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		if !p.Execute(func() {
			defer wg.Done()
			mu.Lock()
			got = append(got, 1)
			mu.Unlock()
		}) {
			t.Fatal("Execute rejected a task on a live pool")
		}
	}
	wg.Wait()

	mu.Lock()
	n := len(got)
	mu.Unlock()
	if n != 200 {
		t.Fatalf("ran %d tasks, want 200", n)
	}
}

func TestExecuteAfterShutdownIsRejected(t *testing.T) {
	p := NewFixedThreadPool(2, "Test-")
	dropped := p.ShutdownNow()
	if len(dropped) != 0 {
		t.Fatalf("ShutdownNow dropped %d tasks, want 0", len(dropped))
	}
	if p.Execute(func() {}) {
		t.Fatal("Execute must return false after ShutdownNow")
	}
	// 原版在此处抛 RejectedExecutionException; Go 侧只拒绝, 不 panic.
}

func TestShutdownNowDropsQueuedTasks(t *testing.T) {
	// 1 条 worker + 一个先堵住 worker 的任务, 让后续任务全部堆在队列里.
	p := NewFixedThreadPool(1, "Test-")
	release := make(chan struct{})
	started := make(chan struct{})

	if !p.Execute(func() {
		close(started)
		<-release
	}) {
		t.Fatal("Execute rejected the blocking task")
	}
	<-started

	queued := 50
	for i := 0; i < queued; i++ {
		if !p.Execute(func() {}) {
			t.Fatalf("Execute rejected queued task %d", i)
		}
	}

	dropped := p.ShutdownNow()
	close(release)

	if len(dropped) != queued {
		t.Fatalf("ShutdownNow dropped %d tasks, want %d", len(dropped), queued)
	}
}

func TestPoolPanicDoesNotKillWorker(t *testing.T) {
	// 对应 ThreadFactoryName 的 uncaughtExceptionHandler: 任务 panic 只杀当前任务.
	p := NewFixedThreadPool(1, "Test-")
	defer p.ShutdownNow()

	done := make(chan struct{})
	if !p.Execute(func() {
		defer func() {
			if r := recover(); r != nil {
				close(done)
			}
		}()
		panic("boom")
	}) {
		t.Fatal("Execute rejected the panicking task")
	}
	<-done

	// worker 必须还活着
	next := make(chan struct{})
	if !p.Execute(func() { close(next) }) {
		t.Fatal("worker died after a task panicked")
	}
	select {
	case <-next:
	case <-time.After(2 * time.Second):
		t.Fatal("worker never picked up the next task")
	}
}

func TestFactoryNamesAreSequential(t *testing.T) {
	f := NameThreadFactory("Core-")
	want := []string{"Core-1", "Core-2", "Core-3"}
	for _, w := range want {
		if got := f.Next(); got != w {
			t.Fatalf("Next() = %q, want %q", got, w)
		}
	}
}

func TestPoolSize(t *testing.T) {
	if got := NewFixedThreadPool(3, "Test-").Size(); got != 3 {
		t.Fatalf("Size() = %d, want 3", got)
	}
	// n <= 0 时原版抛 IllegalArgumentException, 这里退化为 1
	if got := NewFixedThreadPool(0, "Test-").Size(); got != 1 {
		t.Fatalf("Size() = %d, want 1", got)
	}
}

func TestConcurrentSubmitAndShutdown(t *testing.T) {
	p := NewFixedThreadPool(8, "Test-")
	var executed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				if p.Execute(func() { executed.Add(1) }) {
					continue
				}
				return
			}
		}()
	}
	time.Sleep(5 * time.Millisecond)
	p.ShutdownNow()
	wg.Wait()

	if executed.Load() == 0 {
		t.Fatal("no task ran at all")
	}
}
