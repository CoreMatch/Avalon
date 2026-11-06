// Package enums 对应 net.rwhps.server.game.enums
package enums

// enumName 对应 Kotlin enum.name, 越界返回空串
func enumName(names []string, v int) string {
	if v < 0 || v >= len(names) {
		return ""
	}
	return names[v]
}

// lookupEnumIndex 对应 EnumUtils.from(entries, name, defaultValue), 大小写不敏感
func lookupEnumIndex(names []string, name string, def int) int {
	for i, n := range names {
		if asciiEqualFold(n, name) {
			return i
		}
	}
	return def
}

// lookupEnumIndexByIndex 对应 EnumUtils.from(entries, index, defaultValue).
// 原版先取 entries[index] 再判空, 越界会直接抛异常; 这里按文档返回默认值, 不照抄该缺陷.
func lookupEnumIndexByIndex(size, index, def int) int {
	if index < 0 || index >= size {
		return def
	}
	return index
}

// asciiEqualFold 对应 Kotlin String.equals(other, ignoreCase = true) 的 ASCII 子集
func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
