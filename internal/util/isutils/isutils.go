// Package isutils 对应 net.rwhps.server.util.IsUtils
//
// 原版是个大 object, 混装了空判断 / 数字判断 / IPv4 判断 / 数学判断等一堆互不相关的
// 静态方法. 这里遵循本项目"只移植有调用方的成员"的约定, 先把有调用方的三个正则
// 判断落下来; 其余成员等出现消费者时再按需追加, 不预写 API.
package isutils

import (
	"net"
	"regexp"
)

var (
	// patternNumeric 对应 IsUtils.PATTERN
	//
	// ^...$ 锚点是必须的: Java 的 Pattern.matches() 要求全串匹配,
	// 而 Go 的 MatchString 只要求串内存在匹配.
	patternNumeric = regexp.MustCompile(`^[-+]?\d+(?:\.\d+)?$`)

	// patternNegativeNumber 对应 IsUtils.PATTERNNegativeNumber
	//
	// 注意原版这个正则写的是 `[-+]?[0-9]*` —— 星号在字符组外面, 匹配**空串**,
	// 所以 isNumericNegative("") 为 true. 这是个明显的笔误 (大概想写 \d+),
	// 但调用方 ServerCommands 依赖的是它的实际行为, 因此照搬.
	patternNegativeNumber = regexp.MustCompile(`^[-+]?[0-9]*$`)
)

// IsBlank 对应 isBlank(string?)
//
// 原版: str.isNullOrBlank() —— null 或全空白为真. Go 的 string 没有 null,
// 空串与全空白同样返回 true. 消费者是 GameVersionRelay (relayDirectInspection
// 的查询串判断与 newRelayId 的自定义 ID 判断).
func IsBlank(s string) bool {
	for _, r := range s {
		if !isWhitespace(r) {
			return false
		}
	}
	return true
}

// isWhitespace 对应 Kotlin Char.isWhitespace 的常用子集: 空格与制表/换行类控制字符
func isWhitespace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return r == 0x85 || r == 0xA0
}

// IsNumeric 对应 isNumeric(string)
//
// 原版用的是 Pattern.matches() —— **全串匹配**, 不是 find(). Go 的
// MatchString 是"串内是否存在匹配", 语义不同, 因此这里必须显式加 ^...$ 锚点
// (未开 (?m), 所以 ^/$ 是文本首尾而非行首行尾), 否则 "a1b" 会被误判为数字.
//
// 全串必须是: 可选符号 + 至少一位数字 + 可选小数部分.
func IsNumeric(s string) bool { return patternNumeric.MatchString(s) }

// IsNumericNegative 对应 isNumericNegative(string)
func IsNumericNegative(s string) bool { return patternNegativeNumber.MatchString(s) }

// NotIsNumeric 对应 notIsNumeric(string)
func NotIsNumeric(s string) bool { return !IsNumeric(s) }

// IsDomainName 对应 isDomainName(domain)
//
// 原版: InetAddress.getByName(domain) 解析成功且结果是 IPv4 才为 true.
// Go 用 net.ResolveIPAddr("ip4", ...) 对齐: 解析出 IPv4 地址即通过.
// 与原版一致, 这里会真实触发一次 DNS 解析.
func IsDomainName(domain string) bool {
	if domain == "" {
		return false
	}
	addr, err := net.ResolveIPAddr("ip4", domain)
	return err == nil && addr != nil && addr.IP.To4() != nil
}
