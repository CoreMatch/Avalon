// Package lock 对应 net.rwhps.server.util.concurrent.lock
//
// 原版是两个 Kotlin 属性委托 (ReadWriteProperty), 用 `by` 语法把 Get/Set 包上锁.
// Go 没有属性委托, 因此落成显式的 Get() / Set() 方法对.
//
// # 为什么 set 的回调签名多一个 old 参数
//
// 原版:
//
//	var isStartGame: Boolean by Synchronize(false, set = {
//	    if (isStartGame && it) { ... }     // <- 在 set 回调里又读了 isStartGame 自己
//	    ...
//	})
//
// 读 isStartGame 会走 getValue -> synchronized(this). Java 的监视器是**可重入**的,
// 同一个线程再次获取不会阻塞, 所以原版不会死锁.
//
// Go 的 sync.Mutex / sync.RWMutex **不可重入**: 在持有写锁的回调里再读一次属性,
// 直接死锁. 因此这里把旧值作为参数传进去:
//
//	set: func(old, new T) T
//
// 调用方无需重新进入即可拿到旧值. 这是语言约束导致的签名偏离, 不是随意改.
package lock

import "sync"

// Synchronize 对应 Synchronize<T>
//
// 原注释: "这个同步为重锁, 不适用高并发" —— 全字段一把互斥锁, 读写互相阻塞.
// 与高并发场景请用 ReadWriteConcurrentSynchronize.
type Synchronize[T any] struct {
	value T
	get   func(T) T
	set   func(old, new T) T
	mu    sync.Mutex
}

// NewSynchronize 对应 Synchronize(defaultValue): get/set 用恒等变换
func NewSynchronize[T any](defaultValue T) *Synchronize[T] {
	return NewSynchronizeWith(defaultValue, nil, nil)
}

// NewSynchronizeWith 对应 Synchronize(defaultValue, get, set)
//
// get 为 nil 时等价于恒等; set 为 nil 时直接存入新值.
// set 返回的值才是最终被存下来的那个 (与原版的 `return@Synchronize it` 一致).
func NewSynchronizeWith[T any](defaultValue T, get func(T) T, set func(old, new T) T) *Synchronize[T] {
	if get == nil {
		get = func(v T) T { return v }
	}
	return &Synchronize[T]{
		value: defaultValue,
		get:   get,
		set:   set,
	}
}

// Get 对应 getValue: 持锁读取并经过 get 变换
func (s *Synchronize[T]) Get() T {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(s.value)
}

// Set 对应 setValue: 持锁经过 set 变换后存入
func (s *Synchronize[T]) Set(value T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.set != nil {
		s.value = s.set(s.value, value)
		return
	}
	s.value = value
}

// ReadWriteConcurrentSynchronize 对应 ReadWriteConcurrentSynchronize<T>
//
// 原注释: "这个同步为轻锁" —— 读写锁, 读读并发、读写互斥.
//
// 一处没有对应物的差异: 原版用 ReentrantReadWriteLock(true), 即**公平**模式,
// 写者不会因持续到来的读者而饥饿. Go 的 sync.RWMutex 不保证公平 (但写者等待时会
// 阻止新读者进入, 实际饥饿风险很低).
type ReadWriteConcurrentSynchronize[T any] struct {
	value T
	get   func(T) T
	set   func(old, new T) T
	mu    sync.RWMutex
}

// NewReadWriteConcurrentSynchronize 对应 ReadWriteConcurrentSynchronize(defaultValue)
func NewReadWriteConcurrentSynchronize[T any](defaultValue T) *ReadWriteConcurrentSynchronize[T] {
	return NewReadWriteConcurrentSynchronizeWith(defaultValue, nil, nil)
}

// NewReadWriteConcurrentSynchronizeWith 对应 ReadWriteConcurrentSynchronize(defaultValue, get, set)
func NewReadWriteConcurrentSynchronizeWith[T any](
	defaultValue T, get func(T) T, set func(old, new T) T,
) *ReadWriteConcurrentSynchronize[T] {
	if get == nil {
		get = func(v T) T { return v }
	}
	return &ReadWriteConcurrentSynchronize[T]{
		value: defaultValue,
		get:   get,
		set:   set,
	}
}

// Get 对应 getValue
func (s *ReadWriteConcurrentSynchronize[T]) Get() T {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.get(s.value)
}

// Set 对应 setValue
func (s *ReadWriteConcurrentSynchronize[T]) Set(value T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.set != nil {
		s.value = s.set(s.value, value)
		return
	}
	s.value = value
}
