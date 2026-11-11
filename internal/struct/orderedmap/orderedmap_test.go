package orderedmap

import (
	"reflect"
	"testing"
)

// TestPutGetContains 基本读写
func TestPutGetContains(t *testing.T) {
	m := New[int]()
	if m.Size() != 0 {
		t.Fatal("新 Map 应为空")
	}
	m.Put("a", 1)
	if v, ok := m.Get("a"); !ok || v != 1 {
		t.Fatalf("Get(a) = %d/%v", v, ok)
	}
	if _, ok := m.Get("nope"); ok {
		t.Fatal("缺失键应 ok=false")
	}
	if !m.ContainsKey("a") || m.ContainsKey("nope") {
		t.Fatal("ContainsKey 判定错误")
	}
}

// TestInsertionOrder 插入顺序保持 (对应上游 OrderedMap 的核心语义)
func TestInsertionOrder(t *testing.T) {
	m := New[string]()
	for _, k := range []string{"delta", "alpha", "charlie", "bravo"} {
		m.Put(k, k+"-v")
	}
	want := []string{"delta", "alpha", "charlie", "bravo"}
	if !reflect.DeepEqual(m.Keys(), want) {
		t.Fatalf("Keys = %v, want %v", m.Keys(), want)
	}

	var order []string
	m.EachAll(func(k string, v string) {
		order = append(order, k)
		if v != k+"-v" {
			t.Fatalf("EachAll 值错位: %s -> %s", k, v)
		}
	})
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("EachAll 顺序 = %v, want %v", order, want)
	}
}

// TestPutUpdateKeepsOrder 更新已有键不改变插入顺序
func TestPutUpdateKeepsOrder(t *testing.T) {
	m := New[int]()
	m.Put("a", 1)
	m.Put("b", 2)
	m.Put("a", 10) // 更新, 不是新插入
	if m.Size() != 2 {
		t.Fatalf("更新不应增大 Size, got %d", m.Size())
	}
	if !reflect.DeepEqual(m.Keys(), []string{"a", "b"}) {
		t.Fatalf("更新不应改变顺序: %v", m.Keys())
	}
	if v, _ := m.Get("a"); v != 10 {
		t.Fatalf("更新后 Get(a) = %d", v)
	}
}

// TestRemove 删除维护顺序; 删不存在的键是空操作
func TestRemove(t *testing.T) {
	m := New[int]()
	m.Put("a", 1)
	m.Put("b", 2)
	m.Put("c", 3)

	m.Remove("b") // 中间
	if !reflect.DeepEqual(m.Keys(), []string{"a", "c"}) {
		t.Fatalf("删中间键后 = %v", m.Keys())
	}
	m.Remove("a") // 头
	m.Remove("zz") // 不存在
	if m.Size() != 1 || m.Keys()[0] != "c" {
		t.Fatalf("删头键后 = %v", m.Keys())
	}
	if m.ContainsKey("a") || m.ContainsKey("b") {
		t.Fatal("已删键不应再命中")
	}
}

// TestKeysReturnsCopy Keys 是副本, 外部修改不影响 Map
func TestKeysReturnsCopy(t *testing.T) {
	m := New[int]()
	m.Put("a", 1)
	m.Put("b", 2)
	keys := m.Keys()
	keys[0] = "hacked"
	if m.Keys()[0] != "a" {
		t.Fatal("Keys 应返回副本")
	}
}
