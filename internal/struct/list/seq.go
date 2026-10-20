// Package list 对应 net.rwhps.server.struct.list
package list

import (
	"fmt"
	"sync"
)

// Seq 对应 Seq<T>, 可调整大小且有序的对象数组.
// threadSafety 为 true 时所有操作加锁 (对应 ObjectLists.synchronize).
type Seq[T any] struct {
	mu      sync.Mutex
	list    []T
	locking bool
}

// NewSeq 对应 Seq(threadSafety = false) (默认容量 16)
func NewSeq[T any]() *Seq[T] {
	return NewSeqCap[T](16, false)
}

// NewSeqCap 对应 Seq(capacity, threadSafety)
func NewSeqCap[T any](capacity int, threadSafety bool) *Seq[T] {
	if capacity < 0 {
		capacity = 0
	}
	return &Seq[T]{list: make([]T, 0, capacity), locking: threadSafety}
}

// NewSeqOf 对应 Seq(array, threadSafety)
func NewSeqOf[T any](array []T, threadSafety bool) *Seq[T] {
	s := &Seq[T]{list: make([]T, len(array), len(array)), locking: threadSafety}
	copy(s.list, array)
	return s
}

func (s *Seq[T]) lock() {
	if s.locking {
		s.mu.Lock()
	}
}

func (s *Seq[T]) unlock() {
	if s.locking {
		s.mu.Unlock()
	}
}

// Size 对应 size
func (s *Seq[T]) Size() int {
	s.lock()
	defer s.unlock()
	return len(s.list)
}

// IsEmpty 对应 isEmpty
func (s *Seq[T]) IsEmpty() bool {
	s.lock()
	defer s.unlock()
	return len(s.list) == 0
}

// Any 对应 any() (名字保留, 语义为 isEmpty)
func (s *Seq[T]) Any() bool {
	return s.IsEmpty()
}

// Add 对应 add(element)
func (s *Seq[T]) Add(element T) {
	s.lock()
	defer s.unlock()
	s.list = append(s.list, element)
}

// AddAt 对应 add(index, element)
func (s *Seq[T]) AddAt(index int, element T) {
	s.lock()
	defer s.unlock()
	if index < 0 || index > len(s.list) {
		return
	}
	s.list = append(s.list, *new(T))
	copy(s.list[index+1:], s.list[index:])
	s.list[index] = element
}

// AddAll 对应 addAll(elements)
func (s *Seq[T]) AddAll(elements []T) {
	s.lock()
	defer s.unlock()
	s.list = append(s.list, elements...)
}

// Pop 对应 pop() 移除最后一个元素
func (s *Seq[T]) Pop() T {
	s.lock()
	defer s.unlock()
	var zero T
	if len(s.list) == 0 {
		return zero
	}
	v := s.list[len(s.list)-1]
	s.list = s.list[:len(s.list)-1]
	return v
}

// Peek 对应 peek() 获取最后一个元素
func (s *Seq[T]) Peek() T {
	s.lock()
	defer s.unlock()
	var zero T
	if len(s.list) == 0 {
		return zero
	}
	return s.list[len(s.list)-1]
}

// First 对应 first() 获取第一个元素
func (s *Seq[T]) First() T {
	s.lock()
	defer s.unlock()
	var zero T
	if len(s.list) == 0 {
		return zero
	}
	return s.list[0]
}

// RemoveFirst 对应 removeFirst()
func (s *Seq[T]) RemoveFirst() T {
	return s.RemoveAt(0)
}

// Get 对应 get(index)
func (s *Seq[T]) Get(index int) T {
	s.lock()
	defer s.unlock()
	var zero T
	if index < 0 || index >= len(s.list) {
		return zero
	}
	return s.list[index]
}

// Set 对应 set(index, element)
func (s *Seq[T]) Set(index int, element T) T {
	s.lock()
	defer s.unlock()
	var zero T
	if index < 0 || index >= len(s.list) {
		return zero
	}
	old := s.list[index]
	s.list[index] = element
	return old
}

// IndexOf 对应 indexOf(element)
func (s *Seq[T]) IndexOf(element T, equals func(a, b T) bool) int {
	s.lock()
	defer s.unlock()
	for i, v := range s.list {
		if equals(v, element) {
			return i
		}
	}
	return -1
}

// Remove 对应 remove(element)
func (s *Seq[T]) Remove(element T, equals func(a, b T) bool) bool {
	s.lock()
	defer s.unlock()
	for i, v := range s.list {
		if equals(v, element) {
			s.list = append(s.list[:i], s.list[i+1:]...)
			return true
		}
	}
	return false
}

// RemoveAt 对应 removeAt(index)
func (s *Seq[T]) RemoveAt(index int) T {
	s.lock()
	defer s.unlock()
	var zero T
	if index < 0 || index >= len(s.list) {
		return zero
	}
	v := s.list[index]
	s.list = append(s.list[:index], s.list[index+1:]...)
	return v
}

// Contains 对应 contains(element)
func (s *Seq[T]) Contains(element T, equals func(a, b T) bool) bool {
	return s.IndexOf(element, equals) >= 0
}

// Clear 对应 clear()
func (s *Seq[T]) Clear() {
	s.lock()
	defer s.unlock()
	s.list = s.list[:0]
}

// EachAll 对应 eachAll(block)
func (s *Seq[T]) EachAll(block func(T)) {
	s.lock()
	defer s.unlock()
	for _, v := range s.list {
		block(v)
	}
}

// Find 对应 find(findA)
func (s *Seq[T]) Find(findA func(T) bool) (T, bool) {
	s.lock()
	defer s.unlock()
	for _, v := range s.list {
		if findA(v) {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// Find2 对应 find(findA, findB)
func (s *Seq[T]) Find2(findA, findB func(T) bool) (T, bool) {
	s.lock()
	defer s.unlock()
	for _, v := range s.list {
		if findA(v) && findB(v) {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// Elements 对应 elements(), 返回底层切片的副本
func (s *Seq[T]) Elements() []T {
	s.lock()
	defer s.unlock()
	out := make([]T, len(s.list))
	copy(out, s.list)
	return out
}

// String 对应 toString()
func (s *Seq[T]) String() string {
	return fmt.Sprint(s.Elements())
}
