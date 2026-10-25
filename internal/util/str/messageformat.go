// Package messageformat 实现 Java MessageFormat 的占位符替换.
//
// 只覆盖原版用到的子集: {n} 与 {n,type}. 与 Java 对齐的关键行为:
//   - 单引号是引用符: '' 表示一个字面单引号, 单引号内的 {} 不替换
//   - 数字参数按 NumberFormat 默认格式输出 (千位分隔 + 最多 3 位小数)
//   - 越界的占位符原样保留
package messageformat

import (
	"fmt"
	"strconv"
	"strings"
)

// Format 对应 java.text.MessageFormat.format
func Format(pattern string, args []any) string {
	var b strings.Builder
	i, n := 0, len(pattern)
	for i < n {
		c := pattern[i]
		if c == '\'' {
			if i+1 < n && pattern[i+1] == '\'' {
				b.WriteByte('\'')
				i += 2
				continue
			}
			i++
			for i < n {
				if pattern[i] == '\'' {
					if i+1 < n && pattern[i+1] == '\'' {
						b.WriteByte('\'')
						i += 2
						continue
					}
					i++
					break
				}
				b.WriteByte(pattern[i])
				i++
			}
			continue
		}
		if c == '{' {
			j := i + 1
			for j < n && pattern[j] != '}' {
				j++
			}
			if j < n {
				content := pattern[i+1 : j]
				idxStr := content
				if k := strings.Index(content, ","); k >= 0 {
					idxStr = content[:k]
				}
				idxStr = strings.TrimSpace(idxStr)
				if id, err := strconv.Atoi(idxStr); err == nil && id >= 0 && id < len(args) {
					b.WriteString(formatArg(args[id]))
				} else {
					b.WriteString(pattern[i : j+1])
				}
				i = j + 1
				continue
			}
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// formatArg 对应 MessageFormat 对单个参数的格式化 (数字走 NumberFormat)
func formatArg(a any) string {
	switch v := a.(type) {
	case nil:
		return "null"
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case int:
		return groupInt(int64(v))
	case int8:
		return groupInt(int64(v))
	case int16:
		return groupInt(int64(v))
	case int32:
		return groupInt(int64(v))
	case int64:
		return groupInt(v)
	case uint:
		return groupUint(uint64(v))
	case uint8:
		return groupUint(uint64(v))
	case uint16:
		return groupUint(uint64(v))
	case uint32:
		return groupUint(uint64(v))
	case uint64:
		return groupUint(v)
	case float32:
		return groupFloat(float64(v))
	case float64:
		return groupFloat(v)
	default:
		return fmt.Sprint(a)
	}
}

// groupInt 按 NumberFormat 的默认分组输出整数 (1234 -> "1,234")
func groupInt(v int64) string {
	if v < 0 {
		return "-" + groupDigits(strconv.FormatUint(uint64(-v), 10))
	}
	return groupDigits(strconv.FormatUint(uint64(v), 10))
}

func groupUint(v uint64) string {
	return groupDigits(strconv.FormatUint(v, 10))
}

// groupFloat 对应 NumberFormat 默认格式: 最多 3 位小数, 去掉末尾 0, 整数部分分组
func groupFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', 3, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	intPart, fracPart, hasFrac := strings.Cut(s, ".")
	out := groupDigits(intPart)
	if hasFrac && fracPart != "" {
		out += "." + fracPart
	}
	if neg {
		out = "-" + out
	}
	return out
}

// groupDigits 每三位插入分隔符
func groupDigits(s string) string {
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
		if len(s) > pre {
			b.WriteByte(',')
		}
	}
	for i := pre; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	return b.String()
}
