// Package plugindata 对应 net.rwhps.server.util.file.plugin.PluginData / AbstractSerializableData.
//
// 原版用它持久化 data/Settings.bin (服务端身份、EULA 版本、管理员数据等).
// 这里只实现 Bin 格式 (gzip + key/tag/value), 因为它是唯一被 Settings.bin 使用的格式;
// tag 5 (JVM 序列化对象) 的字节原样保留, 保证 Go 与原版可以交替读写同一个文件.
package plugindata

import (
	"errors"
	"sync"

	"rwhps/internal/struct/orderedmap"
	"rwhps/internal/util/file"
)

// Kind 对应 Bin 格式里的类型 tag
type Kind byte

const (
	// KindBool tag 0
	KindBool Kind = 0
	// KindInt tag 1
	KindInt Kind = 1
	// KindLong tag 2
	KindLong Kind = 2
	// KindFloat tag 3
	KindFloat Kind = 3
	// KindString tag 4
	KindString Kind = 4
	// KindRaw tag 5: 原版用 JVM 序列化器写入的对象, 本包不解码, 只透传
	KindRaw Kind = 5
)

// Raw 对应 tag 5 的内容 (类名 + 序列化字节)
type Raw struct {
	Class string
	Bytes []byte
}

// Value 对应 SerializableValue
type Value struct {
	Kind  Kind
	Bool  bool
	Int   int32
	Long  int64
	Float float32
	Str   string
	Raw   Raw
}

// Settings 对应 PluginData, 一个 key/value 有序存储
type Settings struct {
	mu       sync.Mutex
	data     *orderedmap.Map[*Value]
	file     *file.FileUtils
	readOnly bool
}

// ErrReadOnly 解码失败后的保护性错误: 拒绝覆盖原文件
var ErrReadOnly = errors.New("plugindata: store is read-only after a decode failure")

// Open 读取 f 指向的 Bin 文件; 文件不存在时返回空存储.
// 解码失败时返回只读存储与错误, 调用方可以继续启动, 但 Save 不会覆盖原文件.
func Open(f *file.FileUtils) (*Settings, error) {
	s := &Settings{data: orderedmap.New[*Value](), file: f}
	if !f.Exists() {
		return s, nil
	}
	bytes, err := f.ReadFileBytes()
	if err != nil {
		s.readOnly = true
		return s, err
	}
	if len(bytes) == 0 {
		return s, nil
	}
	data, err := decode(bytes)
	if err != nil {
		s.readOnly = true
		return s, err
	}
	s.data = data
	return s, nil
}

// File 返回底层文件句柄
func (s *Settings) File() *file.FileUtils { return s.file }

// ReadOnly 解码失败后为 true
func (s *Settings) ReadOnly() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readOnly
}

// Size 条目数量
func (s *Settings) Size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Size()
}

// Keys 按插入顺序返回 key
func (s *Settings) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Keys()
}

// Has 是否存在 key
func (s *Settings) Has(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.ContainsKey(key)
}

// Get 取原始 Value
func (s *Settings) Get(key string) (*Value, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Get(key)
}

// GetString 对应 pluginData[key, { default }]: 不存在时写入默认值并返回
func (s *Settings) GetString(key string, def func() string) string {
	s.mu.Lock()
	if v, ok := s.data.Get(key); ok && v != nil && v.Kind == KindString {
		s.mu.Unlock()
		return v.Str
	}
	s.mu.Unlock()
	value := def()
	s.SetString(key, value)
	return value
}

// GetBool 对应 pluginData[key, default]
func (s *Settings) GetBool(key string, def bool) bool {
	if v, ok := s.Get(key); ok && v != nil && v.Kind == KindBool {
		return v.Bool
	}
	return def
}

// GetInt 对应 pluginData[key, default]
func (s *Settings) GetInt(key string, def int32) int32 {
	if v, ok := s.Get(key); ok && v != nil && v.Kind == KindInt {
		return v.Int
	}
	return def
}

// GetLong 对应 pluginData[key, default]
func (s *Settings) GetLong(key string, def int64) int64 {
	if v, ok := s.Get(key); ok && v != nil && v.Kind == KindLong {
		return v.Long
	}
	return def
}

// GetFloat 对应 pluginData[key, default]
func (s *Settings) GetFloat(key string, def float32) float32 {
	if v, ok := s.Get(key); ok && v != nil && v.Kind == KindFloat {
		return v.Float
	}
	return def
}

// Set 写入任意 Value
func (s *Settings) Set(key string, v *Value) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Put(key, v)
}

// SetString 写入字符串
func (s *Settings) SetString(key, v string) { s.Set(key, &Value{Kind: KindString, Str: v}) }

// SetBool 写入布尔
func (s *Settings) SetBool(key string, v bool) { s.Set(key, &Value{Kind: KindBool, Bool: v}) }

// SetInt 写入 int
func (s *Settings) SetInt(key string, v int32) { s.Set(key, &Value{Kind: KindInt, Int: v}) }

// SetLong 写入 long
func (s *Settings) SetLong(key string, v int64) { s.Set(key, &Value{Kind: KindLong, Long: v}) }

// SetFloat 写入 float
func (s *Settings) SetFloat(key string, v float32) { s.Set(key, &Value{Kind: KindFloat, Float: v}) }

// SetRaw 写入 tag 5 原样字节
func (s *Settings) SetRaw(key, class string, bytes []byte) {
	s.Set(key, &Value{Kind: KindRaw, Raw: Raw{Class: class, Bytes: bytes}})
}

// Save 写回文件, 对应 pluginData.save()
func (s *Settings) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readOnly {
		return ErrReadOnly
	}
	if s.file == nil {
		return nil
	}
	s.file.WriteFileBytes(encode(s.data))
	return nil
}
