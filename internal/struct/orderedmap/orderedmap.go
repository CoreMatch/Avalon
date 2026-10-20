// Package orderedmap 对应 net.rwhps.server.struct.map.OrderedMap
package orderedmap

// Map 保持插入顺序的字符串映射
type Map[V any] struct {
	keys   []string
	values map[string]V
}

// New 创建 OrderedMap
func New[V any]() *Map[V] {
	return &Map[V]{values: map[string]V{}}
}

// Put 插入/更新
func (m *Map[V]) Put(k string, v V) {
	if _, ok := m.values[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.values[k] = v
}

// Get 获取
func (m *Map[V]) Get(k string) (V, bool) {
	v, ok := m.values[k]
	return v, ok
}

// ContainsKey 是否包含
func (m *Map[V]) ContainsKey(k string) bool {
	_, ok := m.values[k]
	return ok
}

// Remove 删除
func (m *Map[V]) Remove(k string) {
	if _, ok := m.values[k]; !ok {
		return
	}
	delete(m.values, k)
	for i, key := range m.keys {
		if key == k {
			m.keys = append(m.keys[:i], m.keys[i+1:]...)
			break
		}
	}
}

// Size 大小
func (m *Map[V]) Size() int { return len(m.keys) }

// Keys 键的顺序副本
func (m *Map[V]) Keys() []string {
	out := make([]string, len(m.keys))
	copy(out, m.keys)
	return out
}

// EachAll 按插入顺序遍历
func (m *Map[V]) EachAll(f func(k string, v V)) {
	for _, k := range m.keys {
		f(k, m.values[k])
	}
}
