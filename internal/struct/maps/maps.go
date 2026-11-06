// Package maps 对应 net.rwhps.server.struct.map
//
// 目录名用 maps 而不是 map: Go 中被导入的包名声明在文件作用域, 会遮蔽宇宙作用域里的
// 内建标识符. 包若叫 map, 同一文件里的 `map[K]V` 类型声明就会失效.
//
// # 与原版的对应关系
//
//	BaseMap<K, V>       -> BaseMap[K, V]
//	IntMap<V>           -> IntMap[V]         (键为 int32, 对应 Kotlin 的 Int)
//	ObjectMap<K, V>     -> ObjectMap[K, V]
//	ObjectIntMap<K>     -> ObjectIntMap[K]   (值为 int32)
//	OrderedMap<K, V>    -> 已由 internal/struct/orderedmap 移植 (字符串键特化),
//	                       本包不重复, 以免出现两份有序表
//
// LongMap / LongIntMap 未移植: 前者全代码库只有 1 个使用点且不在 Relay 链路里,
// 后者零使用点. 需要时再补.
//
// # 线程安全语义
//
// 原版 threadSafety = true 时底层是 ConcurrentHashMap (本身并发安全), 另外对
// find / eachAll 这类遍历再套一层 synchronized(map). 但普通 put/remove 并不获取
// 那个监视器锁, 所以"遍历期间不被修改"这个保证在原版里其实不成立.
//
// Go 版用一把 RWMutex 统一管理, 读读并发、读写互斥 —— 比原版更强. 这是唯一一处
// 刻意强化的语义: 遍历期间并发写入在 Go 里会直接 panic (fatal error:
// concurrent map writes), 没有"将就着跑"这个选项.
package maps

import (
	"fmt"
	"sort"
	"sync"
)

// ControlFind 对应 Control.ControlFind
type ControlFind int

const (
	// ControlBreak 对应 BREAK: 中止遍历
	ControlBreak ControlFind = iota
	// ControlContinue 对应 CONTINUE: 继续遍历
	ControlContinue
)

// KeyValue 对应 KeyValue<K, V>
type KeyValue[K comparable, V any] struct {
	Key   K
	Value V
}

// BaseMap 对应 BaseMap<K, V>
//
// 原版继承 MutableMap<K, V>; Go 没有继承, 这里把公共方法直接实现一遍,
// 三个具体容器通过内嵌获得它们.
type BaseMap[K comparable, V any] struct {
	m            map[K]V
	threadSafety bool
	mu           sync.RWMutex
}

// NewBaseMap 对应 BaseMap(threadSafety) 的默认容量版
func NewBaseMap[K comparable, V any](threadSafety bool) *BaseMap[K, V] {
	return NewBaseMapCap[K, V](16, threadSafety)
}

// NewBaseMapCap 对应 BaseMap(capacity, threadSafety)
//
// capacity 只作为 make 的容量提示. 原版传给 fastutil 的初始容量与 Go map 的
// 扩容策略不同, 这里不追求逐字节一致 —— 它只影响性能, 不影响语义.
func NewBaseMapCap[K comparable, V any](capacity int, threadSafety bool) *BaseMap[K, V] {
	if capacity < 0 {
		capacity = 0
	}
	return &BaseMap[K, V]{
		m:            make(map[K]V, capacity),
		threadSafety: threadSafety,
	}
}

func (b *BaseMap[K, V]) readLock() {
	if b.threadSafety {
		b.mu.RLock()
	}
}

func (b *BaseMap[K, V]) readUnlock() {
	if b.threadSafety {
		b.mu.RUnlock()
	}
}

func (b *BaseMap[K, V]) writeLock() {
	if b.threadSafety {
		b.mu.Lock()
	}
}

func (b *BaseMap[K, V]) writeUnlock() {
	if b.threadSafety {
		b.mu.Unlock()
	}
}

// Size 对应 size
func (b *BaseMap[K, V]) Size() int {
	b.readLock()
	defer b.readUnlock()
	return len(b.m)
}

// IsEmpty 对应 isEmpty()
func (b *BaseMap[K, V]) IsEmpty() bool { return b.Size() == 0 }

// Get 对应 get(key): 不存在时返回零值与 false
func (b *BaseMap[K, V]) Get(key K) (V, bool) {
	b.readLock()
	defer b.readUnlock()
	v, ok := b.m[key]
	return v, ok
}

// GetOrPut 对应 operator get(key, defaultValue: V)
//
// 原版在键不存在时会把默认值 put 进去再返回, 有这个副作用.
func (b *BaseMap[K, V]) GetOrPut(key K, defaultValue V) V {
	b.writeLock()
	defer b.writeUnlock()
	if v, ok := b.m[key]; ok {
		return v
	}
	b.m[key] = defaultValue
	return defaultValue
}

// GetOrPutProv 对应 operator get(key, defaultValue: Prov<V>)
//
// 与 GetOrPut 的区别: 默认值是惰性求值的, 键已存在时不会调用 prov.
func (b *BaseMap[K, V]) GetOrPutProv(key K, prov func() V) V {
	b.writeLock()
	defer b.writeUnlock()
	if v, ok := b.m[key]; ok {
		return v
	}
	v := prov()
	b.m[key] = v
	return v
}

// GetOrDefault 对应 getOrDefault(key, defaultValue)
//
// 注意与 GetOrPut 的差别: 这个没有 put 副作用.
func (b *BaseMap[K, V]) GetOrDefault(key K, defaultValue V) V {
	b.readLock()
	defer b.readUnlock()
	if v, ok := b.m[key]; ok {
		return v
	}
	return defaultValue
}

// Put 对应 put(key, value), 返回被替换掉的旧值
func (b *BaseMap[K, V]) Put(key K, value V) (V, bool) {
	b.writeLock()
	defer b.writeUnlock()
	old, ok := b.m[key]
	b.m[key] = value
	return old, ok
}

// Set 是 Put 的语法糖, 对应 Kotlin 的 map[key] = value
func (b *BaseMap[K, V]) Set(key K, value V) { b.Put(key, value) }

// Remove 对应 remove(key), 返回被删掉的旧值
func (b *BaseMap[K, V]) Remove(key K) (V, bool) {
	b.writeLock()
	defer b.writeUnlock()
	old, ok := b.m[key]
	if ok {
		delete(b.m, key)
	}
	return old, ok
}

// ContainsKey 对应 containsKey(key)
func (b *BaseMap[K, V]) ContainsKey(key K) bool {
	b.readLock()
	defer b.readUnlock()
	_, ok := b.m[key]
	return ok
}

// ContainsValue 对应 containsValue(value)
//
// 需要 V 可比较; 不可比较的类型请用 EachAll 自行判断.
func (b *BaseMap[K, V]) ContainsValue(value V) bool {
	b.readLock()
	defer b.readUnlock()
	for _, v := range b.m {
		if any(v) == any(value) {
			return true
		}
	}
	return false
}

// Clear 对应 clear()
func (b *BaseMap[K, V]) Clear() {
	b.writeLock()
	defer b.writeUnlock()
	b.m = make(map[K]V)
}

// EachAll 对应 eachAll(block)
func (b *BaseMap[K, V]) EachAll(block func(k K, v V)) {
	b.readLock()
	defer b.readUnlock()
	for k, v := range b.m {
		block(k, v)
	}
}

// EachControl 对应 eachControl(findA)
func (b *BaseMap[K, V]) EachControl(findA func(k K, v V) ControlFind) {
	b.readLock()
	defer b.readUnlock()
	for k, v := range b.m {
		if findA(k, v) == ControlBreak {
			return
		}
	}
}

// Find 对应 find(findA)
//
// 原版遍历全表, 命中的话 result 被反复覆盖, 最终返回最后一个命中项. 照搬.
func (b *BaseMap[K, V]) Find(findA func(k K, v V) bool) (KeyValue[K, V], bool) {
	var result KeyValue[K, V]
	found := false
	b.EachAll(func(k K, v V) {
		if findA(k, v) {
			result = KeyValue[K, V]{Key: k, Value: v}
			found = true
		}
	})
	return result, found
}

// Find2 对应 find(findA, findB): 两个条件都满足
func (b *BaseMap[K, V]) Find2(findA, findB func(k K, v V) bool) (KeyValue[K, V], bool) {
	return b.Find(func(k K, v V) bool { return findA(k, v) && findB(k, v) })
}

// EachAllFind 对应 eachAllFind(find, block)
func (b *BaseMap[K, V]) EachAllFind(find func(k K, v V) bool, block func(k K, v V)) {
	b.EachAll(func(k K, v V) {
		if find(k, v) {
			block(k, v)
		}
	})
}

// EachFind 对应 eachFind(find, block): 只对最后一个命中项调用 block
func (b *BaseMap[K, V]) EachFind(find func(k K, v V) bool, block func(k K, v V)) {
	if kv, ok := b.Find(find); ok {
		block(kv.Key, kv.Value)
	}
}

// EachAllFinds 对应 eachAllFinds(findA, findB, block)
func (b *BaseMap[K, V]) EachAllFinds(findA, findB func(k K, v V) bool, block func(k K, v V)) {
	b.EachFind(func(k K, v V) bool { return findA(k, v) && findB(k, v) }, block)
}

// Keys 对应 toArrayKey(): 键的快照
//
// 原版返回 Seq<K>; Go 侧返回切片. 顺序不保证 (与原版一致, 都取决于底层实现).
func (b *BaseMap[K, V]) Keys() []K {
	b.readLock()
	defer b.readUnlock()
	out := make([]K, 0, len(b.m))
	for k := range b.m {
		out = append(out, k)
	}
	return out
}

// Values 对应 values / toArrayValues(): 值的快照
func (b *BaseMap[K, V]) Values() []V {
	b.readLock()
	defer b.readUnlock()
	out := make([]V, 0, len(b.m))
	for _, v := range b.m {
		out = append(out, v)
	}
	return out
}

// String 对应 toString()
//
// 一处刻意的差异: Go 的 fmt 打印 map 会按键排序输出 (自 1.12 起), 而 Java 的
// HashMap 顺序取决于哈希. 原版这里只是给日志看的, 不参与协议, 稳定输出反而更好.
func (b *BaseMap[K, V]) String() string {
	b.readLock()
	defer b.readUnlock()

	keys := make([]K, 0, len(b.m))
	for k := range b.m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return fmt.Sprint(keys[i]) < fmt.Sprint(keys[j])
	})

	var sb []byte
	sb = append(sb, '{')
	for i, k := range keys {
		if i > 0 {
			sb = append(sb, ',', ' ')
		}
		sb = append(sb, fmt.Sprintf("%v=%v", k, b.m[k])...)
	}
	sb = append(sb, '}')
	return string(sb)
}

// IntMap 对应 IntMap<V>
//
// 键是 int32, 对应 Kotlin 的 Int (Java int). RelayRoom.abstractNetConnectIntMap
// 与 serverRelayDataRoom 用的都是它.
type IntMap[V any] struct {
	BaseMap[int32, V]
}

// NewIntMap 对应 IntMap(threadSafety)
func NewIntMap[V any](threadSafety bool) *IntMap[V] {
	return &IntMap[V]{BaseMap: *NewBaseMapCap[int32, V](16, threadSafety)}
}

// NewIntMapCap 对应 IntMap(capacity, threadSafety)
func NewIntMapCap[V any](capacity int, threadSafety bool) *IntMap[V] {
	return &IntMap[V]{BaseMap: *NewBaseMapCap[int32, V](capacity, threadSafety)}
}

// ObjectMap 对应 ObjectMap<K, V>
type ObjectMap[K comparable, V any] struct {
	BaseMap[K, V]
}

// NewObjectMap 对应 ObjectMap(threadSafety)
func NewObjectMap[K comparable, V any](threadSafety bool) *ObjectMap[K, V] {
	return &ObjectMap[K, V]{BaseMap: *NewBaseMapCap[K, V](16, threadSafety)}
}

// NewObjectMapCap 对应 ObjectMap(capacity, threadSafety)
func NewObjectMapCap[K comparable, V any](capacity int, threadSafety bool) *ObjectMap[K, V] {
	return &ObjectMap[K, V]{BaseMap: *NewBaseMapCap[K, V](capacity, threadSafety)}
}

// ObjectIntMap 对应 ObjectIntMap<K>
//
// 值是 int32, 对应 Kotlin 的 Int. 注意 RelayRoom 里用的是 ObjectIntMap<Int>,
// 也就是键和值都是整数.
type ObjectIntMap[K comparable] struct {
	BaseMap[K, int32]
}

// NewObjectIntMap 对应 ObjectIntMap(threadSafety)
func NewObjectIntMap[K comparable](threadSafety bool) *ObjectIntMap[K] {
	return &ObjectIntMap[K]{BaseMap: *NewBaseMapCap[K, int32](16, threadSafety)}
}

// NewObjectIntMapCap 对应 ObjectIntMap(capacity, threadSafety)
func NewObjectIntMapCap[K comparable](capacity int, threadSafety bool) *ObjectIntMap[K] {
	return &ObjectIntMap[K]{BaseMap: *NewBaseMapCap[K, int32](capacity, threadSafety)}
}
