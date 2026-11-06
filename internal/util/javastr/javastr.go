// Package javastr 对应 Java 字符串的兼容行为
//
// 这里放"Java/Kotlin 的 String 有、而 Go 的 string 语义不同"的那几个操作.
// 目前只有一个 hashCode, 但全代码库有 14 处 .hashCode() 调用, 值得独立成包.
package javastr

import "unicode/utf16"

// HashCode 对应 java.lang.String.hashCode()
//
//	s[0]*31^(n-1) + s[1]*31^(n-2) + ... + s[n-1]
//
// 用 int32 运算: Java 的 int 是 32 位且静默回绕, Go 的 int32 溢出同样是二进制
// 回绕, 因此逐字符乘 31 累加即可, 不需要显式取模.
//
// # 为什么必须走 UTF-16
//
// Java 的 char 是 UTF-16 码元, 而 Go 的 string 是 UTF-8 字节序列. 直接用
// []byte(s) 或 range s 都会在非 ASCII 输入上算出不同的值. 先 utf16.Encode
// 成码元序列, 才能与 Java 逐位对齐. 空串返回 0, 与 Java 一致.
func HashCode(s string) int32 {
	units := utf16.Encode([]rune(s))
	var h int32
	for _, u := range units {
		h = 31*h + int32(u)
	}
	return h
}
