// Package messageformat 实现 Java MessageFormat 的占位符替换 (简化但引用感知)
package messageformat

import (
	"fmt"
	"strconv"
	"strings"
)

// Format 对应 java.text.MessageFormat.format
//
// 支持 {n} / {n,type} 占位符, 并遵循 Java 的单引号转义规则:
//   - '' 表示一个字面单引号
//   - 单个 ' 开始/结束一段引用, 引用内的字符 (含 {}) 均视为字面量
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
					b.WriteString(fmt.Sprint(args[id]))
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
