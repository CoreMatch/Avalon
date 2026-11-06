package messageformat

import (
	"fmt"
	"strings"
)

// ParseLog 对应 net.rwhps.server.util.str.Parser.parseLog.
//
// 把 text 里的 {} 依次替换成 args; 支持 \{ \} 转义; 参数用尽时保留花括号内的原文.
// 与 Kotlin 实现逐分支对应, 包括 "找不到闭合括号就把剩余内容原样输出" 的行为.
func ParseLog(text string, args ...any) string {
	if len(args) == 0 || text == "" {
		return text
	}

	var b strings.Builder
	offset := 0
	argsIndex := 0

	start := strings.Index(text, "{")
	for start > -1 {
		if start > 0 && text[start-1] == '\\' {
			// 转义的开括号: 去掉反斜杠, 输出字面量 {
			b.WriteString(text[offset : start-1])
			b.WriteString("{")
			offset = start + 1
		} else {
			b.WriteString(text[offset:start])
			offset = start + 1

			end := indexFrom(text, "}", offset)
			var expr strings.Builder
			for end > -1 {
				if end > offset && text[end-1] == '\\' {
					// 转义的闭括号: 去掉反斜杠, 继续找下一个
					expr.WriteString(text[offset : end-1])
					expr.WriteString("}")
					offset = end + 1
					end = indexFrom(text, "}", offset)
					continue
				}
				expr.WriteString(text[offset:end])
				break
			}

			if end == -1 {
				// 没有闭合括号: 从 { 开始原样输出
				b.WriteString(text[start:])
				offset = len(text)
			} else {
				if argsIndex <= len(args)-1 {
					b.WriteString(fmt.Sprint(args[argsIndex]))
				} else {
					b.WriteString(expr.String())
				}
				offset = end + 1
				argsIndex++
			}
		}

		next := indexFrom(text, "{", offset)
		if next < 0 {
			break
		}
		start = next
	}

	if offset < len(text) {
		b.WriteString(text[offset:])
	}
	return b.String()
}

// indexFrom 返回 s 从 from 开始第一次出现 token 的下标, 未找到返回 -1
func indexFrom(s, token string, from int) int {
	if from > len(s) {
		return -1
	}
	idx := strings.Index(s[from:], token)
	if idx < 0 {
		return -1
	}
	return from + idx
}
