package isutils

import "testing"

func TestIsNumeric(t *testing.T) {
	// 原版是 Pattern.matches(), 全串匹配
	yes := []string{"0", "1", "1234", "-1", "+1", "1.5", "-1.5", "+0.0", "007"}
	for _, s := range yes {
		if !IsNumeric(s) {
			t.Errorf("IsNumeric(%q) = false, want true", s)
		}
	}

	no := []string{
		"",             // 空串: \d+ 要求至少一位
		" ",            // 空白
		"a",            // 字母
		"a1b",          // 关键: 全串匹配下必须为 false (Go 的 MatchString 会误判为 true)
		"1a",           // 尾部杂质
		"a1",           // 头部杂质
		"1.5.5",        // 两个小数点
		"1.",           // 小数点后无数字
		".5",           // 小数点前无数字
		"1e5",          // 科学计数法不在原版正则里
		"1 2",          // 中间空格
		"--1",          // 双符号
		"+-1",          // 双符号
		"1-",           // 尾部符号
		"0x10",         // 十六进制
		"1_000",        // 下划线分隔
		"\u0661\u0662", // 阿拉伯-印度数字: Java 的 \d 默认不含 Unicode 数字
	}
	for _, s := range no {
		if IsNumeric(s) {
			t.Errorf("IsNumeric(%q) = true, want false", s)
		}
	}
}

func TestNotIsNumeric(t *testing.T) {
	if NotIsNumeric("42") {
		t.Error("NotIsNumeric(\"42\") = true, want false")
	}
	if !NotIsNumeric("abc") {
		t.Error("NotIsNumeric(\"abc\") = false, want true")
	}
}

func TestIsNumericNegative(t *testing.T) {
	yes := []string{"", "0", "1", "-1", "+1", "007", "1234567890"}
	for _, s := range yes {
		if !IsNumericNegative(s) {
			t.Errorf("IsNumericNegative(%q) = false, want true", s)
		}
	}

	// 原版正则是 [-+]?[0-9]*, 所以"只有符号、没有数字"和空串一样能匹配
	no := []string{"a", "1a", "a1", "1.5", "1 2"}
	for _, s := range no {
		if IsNumericNegative(s) {
			t.Errorf("IsNumericNegative(%q) = true, want false", s)
		}
	}
}
