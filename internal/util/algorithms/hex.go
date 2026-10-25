// Package algorithms 对应 net.rwhps.server.util.algorithms.HexUtils (含其依赖的 Base16Codec)
package algorithms

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// ErrIllegalHex 对应 VariableException.ArrayRuntimeException("Illegal hexadecimal character ...")
var ErrIllegalHex = errors.New("algorithms: illegal hexadecimal character")

// lowerAlphabets / upperAlphabets 对应 Base16Codec 里的 alphabets
const (
	lowerAlphabets = "0123456789abcdef"
	upperAlphabets = "0123456789ABCDEF"
)

// IsHexNumber 对应 HexUtils.isHexNumber
//
// 原版先要求带 0x / 0X / # 前缀, 再用 Long.decode 试解析;
// Long.decode 支持负号, 因此 "-0x10" 也算合法.
func IsHexNumber(value string) bool {
	index := 0
	if strings.HasPrefix(value, "-") {
		index = 1
	}
	body := value[index:]
	if !strings.HasPrefix(body, "0x") && !strings.HasPrefix(body, "0X") && !strings.HasPrefix(body, "#") {
		return false
	}

	body = strings.TrimPrefix(body, "#")
	body = strings.TrimPrefix(body, "0x")
	body = strings.TrimPrefix(body, "0X")
	if index == 1 {
		body = "-" + body
	}
	_, err := strconv.ParseInt(body, 16, 64)
	return err == nil
}

// EncodeHex 对应 Base16Codec.encode: 字节数组 -> 十六进制
//
// 原版返回 CharArray, Go 直接返回 string (Go 里没有 char[] 与 string 的区别).
func EncodeHex(data []byte, toLowerCase bool) string {
	alphabets := upperAlphabets
	if toLowerCase {
		alphabets = lowerAlphabets
	}

	out := make([]byte, len(data)*2)
	for i, b := range data {
		out[i*2] = alphabets[(b&0xF0)>>4]
		out[i*2+1] = alphabets[b&0x0F]
	}
	return string(out)
}

// EncodeHexStr 对应 encodeHexStr(data: ByteArray, toLowerCase)
func EncodeHexStr(data []byte, toLowerCase bool) string {
	return EncodeHex(data, toLowerCase)
}

// EncodeHexStrString 对应 encodeHexStr(data: String, charset)
//
// 原版默认 UTF-8; Go 的 string 本身就是 UTF-8 字节序列, 无需显式转换.
func EncodeHexStrString(s string) string {
	return EncodeHex([]byte(s), true)
}

// DecodeHex 对应 Base16Codec.decode: 十六进制 -> 字节数组
//
// 两处刻意保留的原版行为:
//  1. 先去掉全部空白字符 (原版 replace("\\s".toRegex(), ""))
//  2. 奇数长度时在前面补一个 "0" 凑成偶数
//
// 与原版的差异: 原版遇到非法十六进制字符抛 ArrayRuntimeException;
// Go 版返回 ErrIllegalHex (附位置信息), 不 panic.
func DecodeHex(hexStr string) ([]byte, error) {
	encoded := strings.Join(strings.Fields(hexStr), "")
	if len(encoded)%2 != 0 {
		encoded = "0" + encoded
	}

	out := make([]byte, len(encoded)/2)
	for i := 0; i < len(out); i++ {
		hi, err := hexDigit(encoded[i*2], i*2)
		if err != nil {
			return nil, err
		}
		lo, err := hexDigit(encoded[i*2+1], i*2+1)
		if err != nil {
			return nil, err
		}
		out[i] = byte(hi<<4 | lo)
	}
	return out, nil
}

// hexDigit 对应 Base16Codec.toDigit
func hexDigit(ch byte, index int) (int, error) {
	switch {
	case ch >= '0' && ch <= '9':
		return int(ch - '0'), nil
	case ch >= 'a' && ch <= 'f':
		return int(ch-'a') + 10, nil
	case ch >= 'A' && ch <= 'F':
		return int(ch-'A') + 10, nil
	default:
		return 0, fmt.Errorf("%w %q at index %d", ErrIllegalHex, string(ch), index)
	}
}

// DecodeHexStr 对应 decodeHexStr(hexStr, charset)
//
// 原版对空串 (isBlank) 直接返回原值, 不做解码.
func DecodeHexStr(hexStr string) string {
	if strings.TrimSpace(hexStr) == "" {
		return hexStr
	}
	b, err := DecodeHex(hexStr)
	if err != nil {
		// 原版此处抛异常; Go 版退回空串, 由调用方决定如何处理
		return ""
	}
	return string(b)
}

// AppendHex 对应 Base16Codec.appendHex
func AppendHex(builder *strings.Builder, b byte, toLowerCase bool) {
	alphabets := upperAlphabets
	if toLowerCase {
		alphabets = lowerAlphabets
	}
	builder.WriteByte(alphabets[(b&0xF0)>>4])
	builder.WriteByte(alphabets[b&0x0F])
}

// ToUnicodeHex 对应 toUnicodeHex(ch: Char)
//
// 原版 char 是 UTF-16 码元, 每次取 `code shr N and 15` 这样一个 4 位分组;
// Go 的 rune 是码点, 因此这里先按原版语义截到低 16 位再取分组.
// 代价: 增补平面字符 (如 emoji, U+1F600) 会被截断, 与原版一致 ——
// 原版的 char 类型本身就表示不了这些字符.
func ToUnicodeHex(ch rune) string {
	code := ch & 0xFFFF
	nib := func(v rune) byte { return lowerAlphabets[v&0x0F] }
	return `\u` + string([]byte{
		nib(code >> 12), nib(code >> 8), nib(code >> 4), nib(code),
	})
}

// ToHexInt 对应 toHex(value: Int)
//
// 原版是 Integer.toHexString, 按无符号 32 位解释 (负数会得到 8 位十六进制).
func ToHexInt(value int) string {
	return strconv.FormatInt(int64(uint32(value)), 16)
}

// ToHexLong 对应 toHex(value: Long): 按无符号 64 位解释
func ToHexLong(value int64) string {
	return strconv.FormatUint(uint64(value), 16)
}

// HexToInt 对应 hexToInt(value): value.toInt(16)
//
// 与原版的差异: 原版非法输入抛 NumberFormatException, Go 返回 error.
// Integer.parseInt 接受整个无符号 32 位范围并按补码回绕 (0xffffffff -> -1),
// 因此这里先按 64 位解析再截断到 int32, 与 Java 的回绕行为一致.
func HexToInt(value string) (int, error) {
	n, err := strconv.ParseInt(value, 16, 64)
	if err != nil {
		return 0, err
	}
	return int(int32(n)), nil
}

// HexToLong 对应 hexToLong(value): value.toLong(16)
//
// Long.parseLong 不接受超过 Long.MAX_VALUE 的值, 因此 "ffffffffffffffff" 会报错 ——
// 与原版一致.
func HexToLong(value string) (int64, error) {
	return strconv.ParseInt(value, 16, 64)
}

// ToBigInteger 对应 toBigInteger(hexStr)
//
// 原版: 入参为 null 返回 null, 否则 BigInteger(hexStr, 16) (非法字符抛 NumberFormatException).
// Go 版用 (nil, nil) 表示原版的 null 分支.
func ToBigInteger(hexStr string) (*big.Int, error) {
	if hexStr == "" {
		return nil, nil
	}
	i, ok := new(big.Int).SetString(hexStr, 16)
	if !ok {
		return nil, fmt.Errorf("algorithms: cannot parse %q as hex BigInteger", hexStr)
	}
	return i, nil
}

// FormatHex 对应 format(hexStr): 每两位加一个空格
//
// 保留原版的两个细节: 奇数长度时最后一个字符被丢弃 (循环条件是 i < length-1),
// 长度不足 2 时原版会越界抛异常, Go 版返回空串.
func FormatHex(hexStr string) string {
	length := len(hexStr)
	if length < 2 {
		return ""
	}

	var b strings.Builder
	b.Grow(length + length/2)
	b.WriteByte(hexStr[0])
	b.WriteByte(hexStr[1])
	for i := 2; i < length-1; i += 2 {
		b.WriteByte(' ')
		b.WriteByte(hexStr[i])
		b.WriteByte(hexStr[i+1])
	}
	return b.String()
}
