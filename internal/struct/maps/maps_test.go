package maps

import (
	"sync"
	"testing"
)

func TestBaseMapPutGetRemove(t *testing.T) {
	m := NewObjectMap[string, int](false)

	if !m.IsEmpty() {
		t.Error("新建后应为空")
	}

	if old, ok := m.Put("a", 1); ok || old != 0 {
		t.Errorf("首次 Put 应返回 (0, false), 得到 (%d, %v)", old, ok)
	}
	if old, ok := m.Put("a", 2); !ok || old != 1 {
		t.Errorf("覆盖 Put 应返回旧值 1, 得到 (%d, %v)", old, ok)
	}
	if m.Size() != 1 {
		t.Errorf("Size = %d, want 1", m.Size())
	}

	if v, ok := m.Get("a"); !ok || v != 2 {
		t.Errorf("Get(a) = (%d, %v), want (2, true)", v, ok)
	}
	if _, ok := m.Get("missing"); ok {
		t.Error("不存在的键应返回 false")
	}

	if old, ok := m.Remove("a"); !ok || old != 2 {
		t.Errorf("Remove 应返回旧值 2, 得到 (%d, %v)", old, ok)
	}
	if _, ok := m.Remove("a"); ok {
		t.Error("重复 Remove 应返回 false")
	}
	if !m.IsEmpty() {
		t.Error("删除后应为空")
	}
}

// TestGetOrPutHasSideEffect 对应 Kotlin `operator get(key, defaultValue)` 的 put 副作用
func TestGetOrPutHasSideEffect(t *testing.T) {
	m := NewObjectMap[string, int](false)

	if v := m.GetOrPut("a", 1); v != 1 {
		t.Errorf("GetOrPut = %d, want 1", v)
	}
	if m.Size() != 1 {
		t.Errorf("GetOrPut 应把默认值写进去, Size = %d, want 1", m.Size())
	}
	if v := m.GetOrPut("a", 99); v != 1 {
		t.Errorf("键已存在时应返回原值 1, 得到 %d", v)
	}

	// GetOrDefault 没有这个副作用
	if v := m.GetOrDefault("b", 5); v != 5 {
		t.Errorf("GetOrDefault = %d, want 5", v)
	}
	if m.ContainsKey("b") {
		t.Error("GetOrDefault 不应写入键")
	}
}

// TestGetOrPutProvIsLazy prov 版本在键已存在时不该被调用
func TestGetOrPutProvIsLazy(t *testing.T) {
	m := NewObjectMap[string, int](false)
	m.Set("a", 1)

	called := false
	v := m.GetOrPutProv("a", func() int {
		called = true
		return 99
	})
	if v != 1 {
		t.Errorf("GetOrPutProv = %d, want 1", v)
	}
	if called {
		t.Error("键已存在时 prov 不应被调用")
	}

	v = m.GetOrPutProv("b", func() int { return 7 })
	if v != 7 {
		t.Errorf("GetOrPutProv(b) = %d, want 7", v)
	}
	if !m.ContainsKey("b") {
		t.Error("键不存在时 prov 版本也应写入")
	}
}

// TestEachAllAndControl 遍历与中止
func TestEachAllAndControl(t *testing.T) {
	m := NewObjectMap[string, int](false)
	for _, k := range []string{"a", "b", "c"} {
		m.Set(k, len(k))
	}

	sum := 0
	m.EachAll(func(k string, v int) { sum += v })
	if sum != 3 {
		t.Errorf("EachAll 求和 = %d, want 3", sum)
	}

	// BREAK 必须真的停下
	seen := 0
	m.EachControl(func(k string, v int) ControlFind {
		seen++
		return ControlBreak
	})
	if seen != 1 {
		t.Errorf("BREAK 后仍遍历了 %d 项, want 1", seen)
	}

	// CONTINUE 走完全表
	seen = 0
	m.EachControl(func(k string, v int) ControlFind {
		seen++
		return ControlContinue
	})
	if seen != 3 {
		t.Errorf("CONTINUE 遍历了 %d 项, want 3", seen)
	}
}

// TestFindReturnsLastMatch 原版 Find 覆盖式赋值, 返回最后一个命中项
func TestFindReturnsLastMatch(t *testing.T) {
	m := NewObjectMap[string, int](false)
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)

	// 全都满足 -> 返回"最后一个", 而 map 遍历顺序不定, 所以只能断言命中且是三者之一
	kv, ok := m.Find(func(k string, v int) bool { return true })
	if !ok {
		t.Fatal("应能命中")
	}
	if kv.Value < 1 || kv.Value > 3 {
		t.Errorf("命中的值 = %d, 超出范围", kv.Value)
	}

	// 精确命中
	kv, ok = m.Find(func(k string, v int) bool { return k == "b" })
	if !ok || kv.Value != 2 {
		t.Errorf("Find(b) = (%v, %v), want (2, true)", kv.Value, ok)
	}

	// 不命中
	if _, ok := m.Find(func(k string, v int) bool { return false }); ok {
		t.Error("无命中时应返回 false")
	}

	// EachFind 只调一次 block
	calls := 0
	m.EachFind(func(k string, v int) bool { return v > 1 }, func(k string, v int) { calls++ })
	if calls != 1 {
		t.Errorf("EachFind 调用了 block %d 次, want 1", calls)
	}

	// Find2 需要两个条件同时满足
	if _, ok := m.Find2(
		func(k string, v int) bool { return v > 1 },
		func(k string, v int) bool { return k == "b" },
	); !ok {
		t.Error("Find2 双条件应命中 b")
	}
	if _, ok := m.Find2(
		func(k string, v int) bool { return v > 1 },
		func(k string, v int) bool { return k == "zzz" },
	); ok {
		t.Error("Find2 有一条不满足时不应命中")
	}
}

// TestEachAllFind 对应 eachAllFind: 所有命中项都过一遍 block
func TestEachAllFind(t *testing.T) {
	m := NewObjectMap[string, int](false)
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)

	got := map[string]int{}
	m.EachAllFind(func(k string, v int) bool { return v >= 2 }, func(k string, v int) {
		got[k] = v
	})
	if len(got) != 2 || got["b"] != 2 || got["c"] != 3 {
		t.Errorf("EachAllFind 结果 = %v", got)
	}
}

// TestKeysAndValues 快照语义
func TestKeysAndValues(t *testing.T) {
	m := NewObjectMap[string, int](false)
	m.Set("a", 1)
	m.Set("b", 2)

	keys := m.Keys()
	if len(keys) != 2 {
		t.Fatalf("Keys 长度 = %d, want 2", len(keys))
	}
	// 改快照不能影响原表
	keys[0] = "zzz"
	if _, ok := m.Get("zzz"); ok {
		t.Error("Keys 返回的应是快照")
	}

	vals := m.Values()
	if len(vals) != 2 {
		t.Errorf("Values 长度 = %d, want 2", len(vals))
	}
}

// TestContainsValue 需要 V 可比较
func TestContainsValue(t *testing.T) {
	m := NewObjectMap[string, int](false)
	m.Set("a", 1)

	if !m.ContainsValue(1) {
		t.Error("应能找到值 1")
	}
	if m.ContainsValue(99) {
		t.Error("不应找到不存在的值")
	}
}

// TestIntMapKeysAreInt32 键类型必须是 int32, 对应 Kotlin 的 Int.
//
// 这条看起来多余, 但很关键: 若误用 Go 的 int (64 位), 在 64 位平台上
// Kotlin 侧回绕得到的负 ID 会变成完全不同的正数, 房间查找全部落空.
func TestIntMapKeysAreInt32(t *testing.T) {
	var m *IntMap[string] = NewIntMap[string](false)
	m.Set(int32(-5), "neg")
	if v, ok := m.Get(int32(-5)); !ok || v != "neg" {
		t.Errorf("负 int32 键应能取到, 得到 (%q, %v)", v, ok)
	}
	// int32(-1) 与 int(-1) 在 64 位平台下是不同的 map 键
	if _, ok := m.Get(int32(-1)); ok {
		t.Error("不应误命中")
	}
}

// TestObjectIntMap 值是 int32
func TestObjectIntMap(t *testing.T) {
	m := NewObjectIntMap[string](false)
	m.Set("a", 7)
	if v, ok := m.Get("a"); !ok || v != 7 {
		t.Errorf("Get(a) = (%d, %v), want (7, true)", v, ok)
	}
}

// TestClear 对应 clear()
func TestClear(t *testing.T) {
	m := NewObjectMap[string, int](false)
	m.Set("a", 1)
	m.Clear()
	if m.Size() != 0 {
		t.Errorf("Clear 后 Size = %d, want 0", m.Size())
	}
	if _, ok := m.Get("a"); ok {
		t.Error("Clear 后不应再取到值")
	}
}

// TestStringIsDeterministic String 输出稳定 (Go 侧按键排序)
func TestStringIsDeterministic(t *testing.T) {
	m := NewObjectMap[string, int](false)
	m.Set("b", 2)
	m.Set("a", 1)

	want := "{a=1, b=2}"
	for i := 0; i < 20; i++ {
		if got := m.String(); got != want {
			t.Fatalf("String() = %q, want %q", got, want)
		}
	}
}

// TestThreadSafetyConcurrent threadSafety = true 时并发读写不应触发
// "fatal error: concurrent map writes".
//
// 这个测试必须在 -race 下跑才有意义.
func TestThreadSafetyConcurrent(t *testing.T) {
	const (
		goroutines = 8
		perG       = 200
	)

	m := NewIntMapCap[int](64, true)

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				key := int32(g*perG + i)
				m.Set(key, i)
				m.Get(key)
				m.ContainsKey(key)
				m.Size()
				m.EachAll(func(k int32, v int) {})
			}
		}(g)
	}
	wg.Wait()

	if m.Size() != goroutines*perG {
		t.Errorf("Size = %d, want %d", m.Size(), goroutines*perG)
	}
}
