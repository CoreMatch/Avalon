package messageformat

import "testing"

// TestParseLog 对齐 Parser.parseLog 的逐分支行为
func TestParseLog(t *testing.T) {
	cases := []struct {
		text string
		args []any
		want string
	}{
		{"a {} b", []any{"x"}, "a x b"},
		{"{} {}", []any{1, 2}, "1 2"},
		// 参数用尽时写入的是花括号内的原文, 空占位符即空串 (原版同行为)
		{"{} {}", []any{1}, "1 "},
		{"no placeholder", []any{"x"}, "no placeholder"},
		{"", []any{"x"}, ""},
		{"{}", nil, "{}"},
		{"{0} and {}", []any{"a", "b"}, "a and b"},
	}
	for _, c := range cases {
		if got := ParseLog(c.text, c.args...); got != c.want {
			t.Errorf("ParseLog(%q, %v) = %q, want %q", c.text, c.args, got, c.want)
		}
	}
}

// TestParseLogEscape 转义的开括号输出字面量
func TestParseLogEscape(t *testing.T) {
	if got := ParseLog(`a \{b`, "x"); got != "a {b" {
		t.Errorf("escaped open brace = %q, want %q", got, "a {b")
	}
}

// TestParseLogUnclosed 没有闭合括号时原样输出剩余内容
func TestParseLogUnclosed(t *testing.T) {
	if got := ParseLog("a {b", "x"); got != "a {b" {
		t.Errorf("unclosed = %q, want %q", got, "a {b")
	}
}
