// Package javautf 实现 Java DataInputStream/DataOutputStream 的
// Modified UTF-8 编解码, 以及 Java 基本类型的无符号辅助方法.
//
// 与协议兼容性直接相关: 原版用 DataOutputStream.writeUTF / DataInputStream.readUTF
// 传输所有字符串, 该方法使用的是 "Modified UTF-8" (非标准 UTF-8):
//   - U+0000 被编码为两字节 0xC0 0x80
//   - 补充平面字符 (> U+FFFF) 编码为代理对 (CESU-8), 每个代理 3 字节
//   - 其余 BMP 字符与标准 UTF-8 一致
package javautf

import (
	"errors"
	"unicode/utf16"
)

// ErrMalformedInput 对应 Java 的 UTFDataFormatException
var ErrMalformedInput = errors.New("java modified utf-8: malformed input")

// Encode 将字符串编码为 Java Modified UTF-8 字节序列 (不含 2 字节长度前缀)
func Encode(s string) []byte {
	// 最坏情况: 每个字符 3 字节
	out := make([]byte, 0, len(s)*2)
	for _, r := range s {
		switch {
		case r == 0:
			out = append(out, 0xC0, 0x80)
		case r <= 0x7F:
			out = append(out, byte(r))
		case r <= 0x7FF:
			out = append(out, byte(0xC0|(r>>6)), byte(0x80|(r&0x3F)))
		case r <= 0xFFFF:
			out = append(out, byte(0xE0|(r>>12)), byte(0x80|((r>>6)&0x3F)), byte(0x80|(r&0x3F)))
		default:
			// 补充平面 -> 代理对, 每个代理单独 3 字节编码
			hi, lo := utf16.EncodeRune(r)
			out = appendSurrogate(out, hi)
			out = appendSurrogate(out, lo)
		}
	}
	return out
}

func appendSurrogate(out []byte, r rune) []byte {
	return append(out, byte(0xE0|(r>>12)), byte(0x80|((r>>6)&0x3F)), byte(0x80|(r&0x3F)))
}

// Decode 解码 Java Modified UTF-8 字节序列为字符串
func Decode(b []byte) (string, error) {
	// 估算容量
	runes := make([]rune, 0, len(b))
	i := 0
	n := len(b)
	for i < n {
		c := b[i]
		switch {
		case c < 0x80:
			runes = append(runes, rune(c))
			i++
		case c&0xE0 == 0xC0:
			if i+1 >= n {
				return "", ErrMalformedInput
			}
			c2 := b[i+1]
			if c2&0xC0 != 0x80 {
				return "", ErrMalformedInput
			}
			runes = append(runes, rune((rune(c&0x1F)<<6)|rune(c2&0x3F)))
			i += 2
		case c&0xF0 == 0xE0:
			if i+2 >= n {
				return "", ErrMalformedInput
			}
			c2 := b[i+1]
			c3 := b[i+2]
			if c2&0xC0 != 0x80 || c3&0xC0 != 0x80 {
				return "", ErrMalformedInput
			}
			r := (rune(c&0x0F) << 12) | (rune(c2&0x3F) << 6) | rune(c3&0x3F)
			runes = append(runes, r)
			i += 3
		default:
			return "", ErrMalformedInput
		}
	}
	// 处理代理对 (补充平面)
	return string(collapseSurrogates(runes)), nil
}

// collapseSurrogates 将连续的 UTF-16 代理对合并为单个补充平面码点
func collapseSurrogates(rs []rune) []rune {
	out := make([]rune, 0, len(rs))
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if r >= 0xD800 && r <= 0xDBFF && i+1 < len(rs) {
			lo := rs[i+1]
			if lo >= 0xDC00 && lo <= 0xDFFF {
				out = append(out, utf16.DecodeRune(r, lo))
				i++
				continue
			}
		}
		out = append(out, r)
	}
	return out
}
