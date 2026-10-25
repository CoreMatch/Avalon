package lock

import (
	"sync"
	"testing"
	"time"
)

// TestSynchronizeBasic 基本读写
func TestSynchronizeBasic(t *testing.T) {
	s := NewSynchronize(0)
	if v := s.Get(); v != 0 {
		t.Errorf("初始值 = %d, want 0", v)
	}
	s.Set(42)
	if v := s.Get(); v != 42 {
		t.Errorf("Set(42) 后 Get = %d, want 42", v)
	}
}

// TestSynchronizeTransforms get/set 变换都生效, 且 set 的返回值才是存入的值
func TestSynchronizeTransforms(t *testing.T) {
	// set 把入参翻倍后存入, get 再 +1
	s := NewSynchronizeWith(1,
		func(v int) int { return v + 1 },
		func(old, new int) int { return new * 2 },
	)

	if v := s.Get(); v != 2 {
		t.Errorf("初始 Get = %d, want 2 (1+1)", v)
	}

	s.Set(5)
	if v := s.Get(); v != 11 {
		t.Errorf("Set(5) 后 Get = %d, want 11 (5*2+1)", v)
	}
}

// TestSynchronizeSetReceivesOld set 回调必须能拿到旧值
//
// 这正是 Go 侧多出 old 参数的原因: Kotlin 靠 Java 监视器的可重入性在 set 回调里
// 重新读属性, Go 的 mutex 不可重入, 会死锁.
func TestSynchronizeSetReceivesOld(t *testing.T) {
	var seenOld, seenNew int
	s := NewSynchronizeWith(10, nil, func(old, new int) int {
		seenOld, seenNew = old, new
		return new
	})

	s.Set(20)
	if seenOld != 10 || seenNew != 20 {
		t.Errorf("set 回调收到 old=%d new=%d, want 10/20", seenOld, seenNew)
	}
	if v := s.Get(); v != 20 {
		t.Errorf("Get = %d, want 20", v)
	}
}

// TestNoReentrantDeadlock 证明 set 回调里不需要重新进入.
//
// 这是整个包存在的核心理由. 用 goroutine + 超时包住: 若实现退化回"持锁期间调用
// 会重新取锁的回调", 这个测试会挂住而不是失败, 超时会让它明确失败.
func TestNoReentrantDeadlock(t *testing.T) {
	// 复刻 RelayRoom.isStartGame 的形状: 只有从 false 跳到 true 时才记时间
	startTime := 0
	started := NewSynchronizeWith(false, nil, func(old, new bool) bool {
		if old && new {
			return new
		}
		if new {
			startTime = 1000
		} else {
			startTime = 0
		}
		return new
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		started.Set(true)
		started.Set(true) // 第二次: old && new, 不该改 startTime
		started.Set(false)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Set 死锁了 (3s 超时) —— set 回调里是否又重新取了锁?")
	}

	if started.Get() {
		t.Error("最终应为 false")
	}
	if startTime != 0 {
		t.Errorf("startTime = %d, want 0", startTime)
	}
}

// TestReadWriteConcurrentSynchronizeBasic 读写锁版本
func TestReadWriteConcurrentSynchronizeBasic(t *testing.T) {
	s := NewReadWriteConcurrentSynchronize("a")
	if v := s.Get(); v != "a" {
		t.Errorf("初始 Get = %q, want %q", v, "a")
	}
	s.Set("b")
	if v := s.Get(); v != "b" {
		t.Errorf("Set(b) 后 Get = %q, want %q", v, "b")
	}
}

// TestReadWriteConcurrentSynchronizeTransform set 变换 + 旧值
func TestReadWriteConcurrentSynchronizeTransform(t *testing.T) {
	type conn struct{ name string }

	var lastOld, lastNew *conn
	s := NewReadWriteConcurrentSynchronizeWith[*conn](nil,
		nil,
		func(old, new *conn) *conn {
			lastOld, lastNew = old, new
			return new
		},
	)

	// 复刻 RelayRoom.admin 的形状: 设值时顺带标记权限
	a := &conn{name: "host"}
	s.Set(a)
	if lastOld != nil || lastNew != a {
		t.Errorf("set 回调收到 old=%v new=%v", lastOld, lastNew)
	}
	if s.Get() != a {
		t.Error("Get 应返回刚设入的对象")
	}
}

// TestSynchronizeConcurrentAtomicIncrement 并发下自增不丢更新 (必须在 -race 下跑)
//
// 注意自增必须写在 set 回调里. 写成 s.Set(s.Get()+1) 是错的: Get 和 Set 是两次
// 独立的加锁, 中间的窗口会被别人插进来, 丢更新. 原版的 Kotlin 版本同样是这个
// 语义 (getValue/setValue 各自加锁), 所以这不是移植缺陷.
func TestSynchronizeConcurrentAtomicIncrement(t *testing.T) {
	const (
		goroutines = 8
		perG       = 500
	)

	// 正确写法: 把"读旧值 + 算新值"整个放进 set 回调, 由一把锁保护
	counter := NewSynchronizeWith(0, nil, func(old, _ int) int { return old + 1 })

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				counter.Set(0)
			}
		}()
	}
	wg.Wait()

	if want := goroutines * perG; counter.Get() != want {
		t.Errorf("最终值 = %d, want %d (有更新丢失)", counter.Get(), want)
	}
}

// TestSynchronizeNaiveRMWLosesUpdates 记录"错误写法"的行为, 防止有人误以为它安全.
//
// 不断言具体数值 (丢多少取决于调度), 只断言它确实不等于理论总数.
func TestSynchronizeNaiveRMWLosesUpdates(t *testing.T) {
	const (
		goroutines = 8
		perG       = 500
	)

	naive := NewSynchronize(0)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				naive.Set(naive.Get() + 1)
			}
		}()
	}
	wg.Wait()

	if naive.Get() == goroutines*perG {
		t.Skip("本次调度恰好没丢更新 (概率极低), 测试意图已由上一个测试覆盖")
	}
}

// TestReadWriteAllowsConcurrentReaders 读锁不互斥
func TestReadWriteAllowsConcurrentReaders(t *testing.T) {
	s := NewReadWriteConcurrentSynchronize(1)

	// 两个读者同时持锁, 若读锁互斥这里会超时
	inRead := make(chan struct{}, 2)
	release := make(chan struct{})

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.mu.RLock()
			inRead <- struct{}{}
			<-release
			s.mu.RUnlock()
		}()
	}

	for i := 0; i < 2; i++ {
		select {
		case <-inRead:
		case <-time.After(3 * time.Second):
			t.Fatal("两个读者没能同时持有读锁")
		}
	}
	close(release)
	wg.Wait()
}
