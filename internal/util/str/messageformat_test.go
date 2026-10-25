package messageformat

import "testing"

// TestFormatNumbers 对齐 Java NumberFormat 的默认输出
func TestFormatNumbers(t *testing.T) {
	cases := []struct {
		pattern string
		args    []any
		want    string
	}{
		{"({0} ms)", []any{1234}, "(1,234 ms)"},
		{"({0} ms)", []any{999}, "(999 ms)"},
		{"{0}", []any{int64(1234567)}, "1,234,567"},
		{"{0}", []any{-1234}, "-1,234"},
		{"{0}", []any{uint16(1234)}, "1,234"},
		{"{0}", []any{1234.5678}, "1,234.568"},
		{"{0}", []any{1.5}, "1.5"},
		{"{0}", []any{0.0}, "0"},
		{"{0}/{1}", []any{1000, 2000}, "1,000/2,000"},
	}
	for _, c := range cases {
		if got := Format(c.pattern, c.args); got != c.want {
			t.Errorf("Format(%q, %v) = %q, want %q", c.pattern, c.args, got, c.want)
		}
	}
}

// TestFormatNonNumbers 字符串/布尔/null 按 Java toString 输出
func TestFormatNonNumbers(t *testing.T) {
	if got := Format("{0}", []any{"abc"}); got != "abc" {
		t.Errorf("string arg = %q", got)
	}
	if got := Format("{0}", []any{true}); got != "true" {
		t.Errorf("bool arg = %q", got)
	}
	if got := Format("{0}", []any{nil}); got != "null" {
		t.Errorf("nil arg = %q", got)
	}
}

// TestFormatApostrophe 对齐 Java 单引号引用规则
func TestFormatApostrophe(t *testing.T) {
	// 未闭合的单引号会吞掉后续占位符 (Java 的已知行为)
	if got := Format("It's {0}", []any{"x"}); got != "Its {0}" {
		t.Errorf("unterminated quote = %q, want %q", got, "Its {0}")
	}
	// '' 表示一个字面单引号
	if got := Format("a '' b", nil); got != "a ' b" {
		t.Errorf("escaped quote = %q", got)
	}
	// 成对引号内的内容不替换
	if got := Format("'{0}'", []any{"x"}); got != "{0}" {
		t.Errorf("quoted placeholder = %q, want %q", got, "{0}")
	}
}

// TestFormatOutOfRange 越界占位符原样保留 (Java 同行为)
func TestFormatOutOfRange(t *testing.T) {
	if got := Format("{1}", []any{"x"}); got != "{1}" {
		t.Errorf("out of range = %q, want %q", got, "{1}")
	}
	if got := Format("{0} {1}", []any{"x"}); got != "x {1}" {
		t.Errorf("partial = %q", got)
	}
}
