// Package load 对应 net.rwhps.server.util.file.load.LoadIni / I18NBundle
package load

import (
	"strconv"
	"strings"

	"rwhps/internal/struct/orderedmap"
)

// Ini 对应 LoadIni (基于 Java Properties 解析)
type Ini struct {
	data *orderedmap.Map[string]
}

// NewIniFromBytes 从字节构建 (按 UTF-8 解码)
func NewIniFromBytes(b []byte) *Ini {
	ini := &Ini{data: orderedmap.New[string]()}
	p := parseProperties(string(b))
	p.EachAll(func(k, v string) { ini.data.Put(k, v) })
	return ini
}

// Data 返回底层数据
func (i *Ini) Data() *orderedmap.Map[string] { return i.data }

// ReadString 读取字符串
func (i *Ini) ReadString(input, def string) string {
	if v, ok := i.data.Get(input); ok {
		return v
	}
	return def
}

// ReadInt 读取整数
func (i *Ini) ReadInt(input string, def int) int {
	if v, ok := i.data.Get(input); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

// ReadBoolean 读取布尔 (Kotlin toBoolean 语义)
func (i *Ini) ReadBoolean(input string, def bool) bool {
	if v, ok := i.data.Get(input); ok {
		return strings.EqualFold(v, "true")
	}
	return def
}

// ReadFloat 读取浮点
func (i *Ini) ReadFloat(input string, def float32) float32 {
	if v, ok := i.data.Get(input); ok {
		if n, err := strconv.ParseFloat(strings.TrimSpace(v), 32); err == nil {
			return float32(n)
		}
	}
	return def
}

// ReadLong 读取长整型
func (i *Ini) ReadLong(input string, def int64) int64 {
	if v, ok := i.data.Get(input); ok {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return n
		}
	}
	return def
}

// SetObject 设置
func (i *Ini) SetObject(input string, key string) { i.data.Put(input, key) }

// parseProperties 解析 Java .properties 文本 (支持续行与转义)
func parseProperties(text string) *orderedmap.Map[string] {
	out := orderedmap.New[string]()
	lines := splitLines(text)
	var logical strings.Builder
	continuing := false

	flush := func(s string) {
		if s == "" {
			return
		}
		trimmed := strings.TrimLeft(s, " \t\f")
		if !continuing && (strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "!")) {
			return
		}
		k, v := splitKeyValue(s)
		out.Put(unescape(k), unescape(v))
	}

	for _, raw := range lines {
		line := raw
		if continuing {
			line = strings.TrimLeft(line, " \t\f")
		}
		// 判断是否续行
		trailing := countTrailingBackslashes(line)
		if trailing%2 == 1 {
			logical.WriteString(line[:len(line)-1])
			continuing = true
			continue
		}
		logical.WriteString(line)
		flush(logical.String())
		logical.Reset()
		continuing = false
	}
	if logical.Len() > 0 {
		flush(logical.String())
	}
	return out
}

func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.Split(text, "\n")
}

func countTrailingBackslashes(s string) int {
	n := 0
	for i := len(s) - 1; i >= 0 && s[i] == '\\'; i-- {
		n++
	}
	return n
}

func splitKeyValue(s string) (string, string) {
	i := 0
	n := len(s)
	// 跳过前导空白
	for i < n && isPropSpace(s[i]) {
		i++
	}
	keyStart := i
	for i < n {
		c := s[i]
		if c == '\\' {
			i += 2
			continue
		}
		if c == '=' || c == ':' || isPropSpace(c) {
			break
		}
		i++
	}
	key := s[keyStart:i]
	// 跳过分隔空白
	for i < n && isPropSpace(s[i]) {
		i++
	}
	if i < n && (s[i] == '=' || s[i] == ':') {
		i++
	}
	for i < n && isPropSpace(s[i]) {
		i++
	}
	return key, s[i:]
}

func isPropSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\f'
}

func unescape(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(s) {
			b.WriteByte('\\')
			break
		}
		switch s[i] {
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 'f':
			b.WriteByte('\f')
		case 'u':
			if i+4 < len(s) {
				if v, err := strconv.ParseUint(s[i+1:i+5], 16, 32); err == nil {
					b.WriteRune(rune(v))
					i += 4
					continue
				}
			}
			b.WriteByte('u')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
