package messageformat

import "testing"

// TestCutting 对应 StringFilteringUtil.cutting 的行为
func TestCutting(t *testing.T) {
	cases := []struct {
		in     string
		length int
		want   string
	}{
		{"abc", 5, "abc"},   // 长度不足, 原样返回
		{"abc", 3, "abc"},   // 等长: 上游走 substring(0,3), 结果相同
		{"abcdef", 3, "abc"}, // 超长截断
		{"", 3, ""},
		{"一二三四五六七八九十一二三四五六七", 15, "一二三四五六七八九十一二三四五"}, // 中文按字符数
	}
	for _, c := range cases {
		if got := Cutting(c.in, c.length); got != c.want {
			t.Errorf("Cutting(%q, %d) = %q, 期望 %q", c.in, c.length, got, c.want)
		}
	}
}
