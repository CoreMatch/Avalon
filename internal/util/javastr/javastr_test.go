package javastr

import (
	"strings"
	"testing"
	"unicode/utf16"
)

func TestHashCodeKnownValues(t *testing.T) {
	// 这些值由独立脚本按 `h = 31*h + codeUnit` 逐字符算出, 其中
	// "hello" -> 99162322 是 Java 文档与社区广为引用的实测值, 用来交叉验证算法本身.
	cases := map[string]int32{
		"":                     0,
		"a":                    97,
		"ab":                   3105,
		"hello":                99162322,
		"RW-HPS":               -1865618237,
		"Crossing Large (10p)": -2079381603,
		"1234":                 1509442,
		"你好":                   652829,
	}
	for in, want := range cases {
		if got := HashCode(in); got != want {
			t.Errorf("HashCode(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestHashCodeWrapsAt32Bits(t *testing.T) {
	// Java 的 int 累加会静默回绕. 用 uint32 参考实现逐位对齐,
	// 确认 Go 的 int32 溢出行为与 32 位截断一致 (而不是无限精度).
	for _, n := range []int{1, 5, 40, 400, 4000} {
		s := strings.Repeat("abcdefghij", n)

		var ref uint32
		for i := 0; i < len(s); i++ {
			ref = ref*31 + uint32(s[i])
		}
		if got := HashCode(s); got != int32(ref) {
			t.Fatalf("HashCode(%d chars) = %d, want %d", len(s), got, int32(ref))
		}
	}
}

func TestHashCodeTruncatesRatherThanGrows(t *testing.T) {
	// 反证: 如果 Go 侧用了无限精度整数, 长短串的差值会持续增长;
	// 32 位截断下必然存在让结果落在负半区的输入.
	negative := 0
	for n := 1; n <= 200; n++ {
		if HashCode(strings.Repeat("x", n)) < 0 {
			negative++
		}
	}
	if negative == 0 {
		t.Fatal("no length produced a negative hash; 32-bit truncation is not happening")
	}
}

func TestHashCodeUsesUtf16CodeUnits(t *testing.T) {
	// 非 BMP 字符在 Java 里是两个 char (代理对), 在 Go 的 []rune 里是一个 rune.
	// 必须按 UTF-16 码元算才能与 Java 对齐.
	const emoji = "\U0001F600"

	units := utf16.Encode([]rune(emoji))
	if len(units) != 2 || units[0] != 0xD83D || units[1] != 0xDE00 {
		t.Fatalf("unexpected UTF-16 encoding: %#x", units)
	}

	var want int32
	for _, u := range units {
		want = 31*want + int32(u)
	}
	if got := HashCode(emoji); got != want {
		t.Fatalf("HashCode(%q) = %d, want %d (computed over UTF-16 code units)", emoji, got, want)
	}
	if want != 1772899 {
		t.Fatalf("hand check: HashCode(emoji) = %d, want 1772899", want)
	}
}

func TestHashCodeOfDigitsIsOrderSensitive(t *testing.T) {
	// coverRelayID 用 hashCode 把非数字房间 ID 压成 int, 顺序敏感是基本要求
	if HashCode("12") == HashCode("21") {
		t.Fatal(`HashCode("12") == HashCode("21")`)
	}
}
