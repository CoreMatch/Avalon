package list

import (
	"sync"
	"testing"
)

func intEq(a, b int) bool { return a == b }

// TestNewSeqVariants 三种构造与入参防御
func TestNewSeqVariants(t *testing.T) {
	if s := NewSeq[int](); s.Size() != 0 || !s.IsEmpty() {
		t.Fatal("NewSeq 应为空")
	}
	if s := NewSeqCap[int](-5, false); s.Size() != 0 {
		t.Fatal("负容量应归零")
	}

	src := []int{1, 2, 3}
	s := NewSeqOf(src, false)
	src[0] = 99 // 构造应拷贝, 不与入参共享底层
	if s.Get(0) != 1 {
		t.Fatal("NewSeqOf 应拷贝入参切片")
	}
	if s.Size() != 3 {
		t.Fatalf("Size = %d, want 3", s.Size())
	}
}

// TestAddSizeEmpty 增删与计数
func TestAddSizeEmpty(t *testing.T) {
	s := NewSeq[string]()
	if !s.Any() {
		t.Fatal("Any 语义同 isEmpty, 空时应为 true")
	}
	s.Add("a")
	s.Add("b")
	if s.Size() != 2 || s.IsEmpty() || s.Any() {
		t.Fatal("Add 后计数/空判错误")
	}
	s.AddAll([]string{"c", "d"})
	if s.Size() != 4 {
		t.Fatalf("AddAll 后 Size = %d, want 4", s.Size())
	}
}

// TestAddAt 插入位置与越界防御
func TestAddAt(t *testing.T) {
	s := NewSeqOf([]int{1, 3}, false)
	s.AddAt(1, 2) // 中间
	s.AddAt(0, 0) // 头
	s.AddAt(4, 4) // 尾 (index == len 合法)
	if s.String() != "[0 1 2 3 4]" {
		t.Fatalf("AddAt 序列 = %s", s.String())
	}
	s.AddAt(-1, 99)
	s.AddAt(99, 99) // 越界静默忽略 (上游语义)
	if s.Size() != 5 {
		t.Fatal("越界 AddAt 应为空操作")
	}
}

// TestPopPeekFirst 尾/首元素读写与空栈防御
func TestPopPeekFirst(t *testing.T) {
	s := NewSeq[int]()
	if v := s.Pop(); v != 0 {
		t.Fatal("空栈 Pop 应返回零值")
	}
	if v := s.Peek(); v != 0 {
		t.Fatal("空栈 Peek 应返回零值")
	}
	if v := s.First(); v != 0 {
		t.Fatal("空栈 First 应返回零值")
	}

	s.AddAll([]int{1, 2, 3})
	if v := s.Peek(); v != 3 {
		t.Fatalf("Peek = %d, want 3", v)
	}
	if v := s.First(); v != 1 {
		t.Fatalf("First = %d, want 1", v)
	}
	if v := s.Pop(); v != 3 || s.Size() != 2 {
		t.Fatalf("Pop = %d, Size = %d", v, s.Size())
	}
}

// TestGetSet 索引读写与越界防御
func TestGetSet(t *testing.T) {
	s := NewSeqOf([]int{10, 20, 30}, false)
	if v := s.Get(1); v != 20 {
		t.Fatalf("Get(1) = %d", v)
	}
	if v := s.Get(-1); v != 0 {
		t.Fatal("Get 负索引应返回零值")
	}
	if v := s.Get(3); v != 0 {
		t.Fatal("Get 越界应返回零值")
	}

	if old := s.Set(1, 25); old != 20 {
		t.Fatalf("Set 应返回旧值, got %d", old)
	}
	if v := s.Get(1); v != 25 {
		t.Fatalf("Set 后 Get = %d", v)
	}
	if old := s.Set(9, 1); old != 0 || s.Size() != 3 {
		t.Fatal("Set 越界应为空操作")
	}
}

// TestIndexOfContainsRemove 基于 equals 的查找与移除
func TestIndexOfContainsRemove(t *testing.T) {
	s := NewSeqOf([]int{1, 2, 3, 2}, false)
	if i := s.IndexOf(2, intEq); i != 1 {
		t.Fatalf("IndexOf 应返回首个命中, got %d", i)
	}
	if i := s.IndexOf(9, intEq); i != -1 {
		t.Fatal("未命中应返回 -1")
	}
	if !s.Contains(3, intEq) || s.Contains(9, intEq) {
		t.Fatal("Contains 判定错误")
	}

	if !s.Remove(2, intEq) {
		t.Fatal("Remove 命中应返回 true")
	}
	if s.String() != "[1 3 2]" {
		t.Fatalf("Remove 应只移除首个命中: %s", s.String())
	}
	if s.Remove(9, intEq) {
		t.Fatal("Remove 未命中应返回 false")
	}
}

// TestRemoveAtFirst 按索引移除
func TestRemoveAtFirst(t *testing.T) {
	s := NewSeqOf([]int{1, 2, 3}, false)
	if v := s.RemoveAt(1); v != 2 {
		t.Fatalf("RemoveAt(1) = %d", v)
	}
	if v := s.RemoveAt(9); v != 0 {
		t.Fatal("RemoveAt 越界应返回零值")
	}
	if v := s.RemoveFirst(); v != 1 || s.Size() != 1 {
		t.Fatalf("RemoveFirst = %d, Size = %d", v, s.Size())
	}
}

// TestClearElementsString 清空与副本语义
func TestClearElementsString(t *testing.T) {
	s := NewSeqOf([]int{1, 2}, false)
	s.Clear()
	if !s.IsEmpty() {
		t.Fatal("Clear 后应为空")
	}

	s.AddAll([]int{5, 6})
	e := s.Elements()
	e[0] = 99 // Elements 是副本, 改它不影响 Seq
	if s.Get(0) != 5 {
		t.Fatal("Elements 应返回副本")
	}
	if s.String() != "[5 6]" {
		t.Fatalf("String = %s", s.String())
	}
}

// TestFind 单/双谓词查找
func TestFind(t *testing.T) {
	s := NewSeqOf([]int{1, 2, 3, 4}, false)
	if v, ok := s.Find(func(v int) bool { return v%2 == 0 }); !ok || v != 2 {
		t.Fatalf("Find = %d/%v", v, ok)
	}
	if _, ok := s.Find(func(v int) bool { return v > 9 }); ok {
		t.Fatal("Find 未命中应 ok=false")
	}
	if v, ok := s.Find2(
		func(v int) bool { return v > 2 },
		func(v int) bool { return v%2 == 0 },
	); !ok || v != 4 {
		t.Fatalf("Find2 = %d/%v", v, ok)
	}
}

// TestEachAllSnapshot 快照遍历: 回调里再入遍历不死锁 (上游可重入锁的 Go 等价),
// 且回调期间的增删不影响本轮遍历
func TestEachAllSnapshot(t *testing.T) {
	s := NewSeqCap[int](0, true)
	s.AddAll([]int{1, 2, 3})

	seen := []int{}
	s.EachAll(func(v int) {
		seen = append(seen, v)
		if v == 1 {
			s.EachAll(func(int) {}) // 嵌套遍历: 持锁回调会自锁死
			s.Add(4)                // 遍历中增删不影响本轮快照
		}
	})
	if len(seen) != 3 || seen[0] != 1 || seen[1] != 2 || seen[2] != 3 {
		t.Fatalf("快照遍历应见且仅见原有 3 个元素: %v", seen)
	}
	if s.Size() != 4 {
		t.Fatal("遍历中的 Add 应生效于下一轮")
	}
}

// TestThreadSafety 加锁变体并发读写不竞态 (-race 下运行)
func TestThreadSafety(t *testing.T) {
	s := NewSeqCap[int](0, true)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s.Add(n)
				_ = s.Size()
				s.EachAll(func(int) {})
			}
		}(i)
	}
	wg.Wait()
	if s.Size() != 800 {
		t.Fatalf("并发 Add 后 Size = %d, want 800", s.Size())
	}
}
