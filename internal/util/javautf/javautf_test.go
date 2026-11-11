package javautf

import (
	"bytes"
	"errors"
	"testing"
)

// TestEncodeKnownVectors Modified UTF-8 的协议关键位形 (与 Java writeUTF 逐字节对齐)
func TestEncodeKnownVectors(t *testing.T) {
	cases := []struct {
		in   string
		want []byte
	}{
		{"A", []byte{0x41}},                                        // ASCII
		{"\x00", []byte{0xC0, 0x80}},                               // NUL 双字节 (非标准 UTF-8)
		{"\u00e9", []byte{0xC3, 0xA9}},                             // 2 字节 BMP (é)
		{"\u4e2d", []byte{0xE4, 0xB8, 0xAD}},                       // 3 字节 BMP (中)
		{"\U0001F600", []byte{0xED, 0xA0, 0xBD, 0xED, 0xB8, 0x80}}, // 补充平面 -> 代理对各 3 字节
	}
	for _, tc := range cases {
		if got := Encode(tc.in); !bytes.Equal(got, tc.want) {
			t.Errorf("Encode(%q) = % x, want % x", tc.in, got, tc.want)
		}
	}
}

// TestRoundtrip 编解码往返
func TestRoundtrip(t *testing.T) {
	for _, s := range []string{
		"",
		"hello",
		"a\x00b",          // 内嵌 NUL
		"中文 mixed \u00e9", // BMP 混合
		"emoji \U0001F600 end",
	} {
		out, err := Decode(Encode(s))
		if err != nil {
			t.Fatalf("Decode(%q): %v", s, err)
		}
		if out != s {
			t.Fatalf("roundtrip %q -> %q", s, out)
		}
	}
}

// TestDecodeMalformed 非法输入归一为 ErrMalformedInput (对应 UTFDataFormatException)
func TestDecodeMalformed(t *testing.T) {
	bad := [][]byte{
		{0xC3},             // 2 字节序列被截断
		{0xC3, 0x41},       // 续字节高位错
		{0xE4, 0xB8},       // 3 字节序列被截断
		{0xE4, 0x41, 0xAD}, // 续字节高位错
		{0x80},             // 孤立续字节
		{0xFF},             // 非法首字节
	}
	for _, b := range bad {
		if _, err := Decode(b); !errors.Is(err, ErrMalformedInput) {
			t.Errorf("Decode(% x) err = %v, want ErrMalformedInput", b, err)
		}
	}
}

// TestDecodeLoneSurrogate 孤立代理不合并, 原样保留 (不与后续普通字符合并)
func TestDecodeLoneSurrogate(t *testing.T) {
	// 手工构造: 仅高代理 D800 的 3 字节编码, 后跟 'A'
	b := []byte{0xED, 0xA0, 0x80, 0x41}
	out, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(out)) != 2 {
		t.Fatalf("孤立代理应保留为 1 个 rune + 'A', got %q", out)
	}
}
