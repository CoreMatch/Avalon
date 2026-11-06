package algorithms

import (
	"math/big"
	"strings"
	"testing"
)

func TestEncodeHex(t *testing.T) {
	data := []byte{0x00, 0x0f, 0xa5, 0xff}

	if got := EncodeHex(data, true); got != "000fa5ff" {
		t.Errorf("小写 = %q", got)
	}
	if got := EncodeHex(data, false); got != "000FA5FF" {
		t.Errorf("大写 = %q", got)
	}
	if got := EncodeHex(nil, true); got != "" {
		t.Errorf("空输入 = %q", got)
	}
}

func TestEncodeHexStrString(t *testing.T) {
	// "你" 的 UTF-8 是 e4 bd a0
	if got := EncodeHexStrString("你"); got != "e4bda0" {
		t.Errorf("EncodeHexStrString = %q", got)
	}
}

func TestDecodeHex(t *testing.T) {
	got, err := DecodeHex("000FA5FF")
	if err != nil {
		t.Fatalf("DecodeHex: %v", err)
	}
	want := []byte{0x00, 0x0f, 0xa5, 0xff}
	if len(got) != len(want) {
		t.Fatalf("长度 = %d, 期望 %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("[%d] = %#x, 期望 %#x", i, got[i], want[i])
		}
	}
}

func TestDecodeHexStripsWhitespace(t *testing.T) {
	// 原版 replace("\\s".toRegex(), "")
	got, err := DecodeHex("00 0f\ta5\nff")
	if err != nil {
		t.Fatalf("DecodeHex: %v", err)
	}
	if len(got) != 4 || got[3] != 0xff {
		t.Fatalf("结果 = % x", got)
	}
}

func TestDecodeHexOddLengthPadsZero(t *testing.T) {
	// 奇数长度时原版在前面补 0
	got, err := DecodeHex("f")
	if err != nil {
		t.Fatalf("DecodeHex: %v", err)
	}
	if len(got) != 1 || got[0] != 0x0f {
		t.Fatalf("结果 = % x, 期望 0f", got)
	}
}

func TestDecodeHexIllegal(t *testing.T) {
	if _, err := DecodeHex("zz"); err == nil {
		t.Fatal("非法十六进制字符应返回错误")
	}
}

func TestDecodeHexStrBlank(t *testing.T) {
	// 原版 isBlank 时原样返回
	if got := DecodeHexStr("   "); got != "   " {
		t.Errorf("DecodeHexStr(空白) = %q", got)
	}
	if got := DecodeHexStr(""); got != "" {
		t.Errorf("DecodeHexStr(空) = %q", got)
	}
}

func TestDecodeHexStrRoundTrip(t *testing.T) {
	if got := DecodeHexStr("e4bda0"); got != "你" {
		t.Errorf("DecodeHexStr = %q", got)
	}
}

func TestAppendHex(t *testing.T) {
	var b strings.Builder
	AppendHex(&b, 0x0f, true)
	AppendHex(&b, 0xa5, false)
	if b.String() != "0fA5" {
		t.Errorf("AppendHex = %q", b.String())
	}
}

func TestIsHexNumber(t *testing.T) {
	cases := map[string]bool{
		"0x10":               true,
		"0X1F":               true,
		"#ff":                true,
		"-0x10":              true,
		"0x":                 false,
		"0xzz":               false,
		"10":                 false,
		"0b101":              false,
		"":                   false,
		"-0x":                false,
		"0x7fffffffffffffff": true,
		"0x8000000000000000": false, // 超出 long, Long.decode 会抛
	}
	for value, want := range cases {
		if got := IsHexNumber(value); got != want {
			t.Errorf("IsHexNumber(%q) = %v, 期望 %v", value, got, want)
		}
	}
}

func TestToHexInt(t *testing.T) {
	cases := map[int]string{
		0:           "0",
		255:         "ff",
		-1:          "ffffffff", // Integer.toHexString 按无符号解释
		-2147483648: "80000000",
	}
	for value, want := range cases {
		if got := ToHexInt(value); got != want {
			t.Errorf("ToHexInt(%d) = %q, 期望 %q", value, got, want)
		}
	}
}

func TestToHexLong(t *testing.T) {
	if got := ToHexLong(255); got != "ff" {
		t.Errorf("ToHexLong(255) = %q", got)
	}
	if got := ToHexLong(-1); got != "ffffffffffffffff" {
		t.Errorf("ToHexLong(-1) = %q", got)
	}
}

func TestHexToIntLong(t *testing.T) {
	if v, err := HexToInt("ff"); err != nil || v != 255 {
		t.Errorf("HexToInt(ff) = %d, %v", v, err)
	}
	// Integer.parseInt 按补码回绕, 与 Java 一致
	if v, err := HexToInt("ffffffff"); err != nil || v != -1 {
		t.Errorf("HexToInt(ffffffff) = %d, %v", v, err)
	}
	if _, err := HexToInt("zz"); err == nil {
		t.Error("HexToInt(zz) 应报错")
	}
	if v, err := HexToLong("ff"); err != nil || v != 255 {
		t.Errorf("HexToLong(ff) = %d, %v", v, err)
	}
	// Long.parseLong 不接受超过 Long.MAX_VALUE 的值, 原版同样抛 NumberFormatException
	if _, err := HexToLong("ffffffffffffffff"); err == nil {
		t.Error("HexToLong(ffffffffffffffff) 应报错")
	}
}

func TestToUnicodeHex(t *testing.T) {
	if got := ToUnicodeHex('你'); got != `\u4f60` {
		t.Errorf("ToUnicodeHex('你') = %q, 期望 \\u4f60", got)
	}
	if got := ToUnicodeHex('A'); got != `\u0041` {
		t.Errorf("ToUnicodeHex('A') = %q", got)
	}
}

func TestToBigInteger(t *testing.T) {
	got, err := ToBigInteger("ff")
	if err != nil {
		t.Fatalf("ToBigInteger: %v", err)
	}
	if got == nil || got.Cmp(big.NewInt(255)) != 0 {
		t.Fatalf("ToBigInteger(ff) = %v", got)
	}

	// 原版的 null 分支
	if got, err := ToBigInteger(""); err != nil || got != nil {
		t.Errorf("ToBigInteger(空) = %v, %v", got, err)
	}

	if _, err := ToBigInteger("zz"); err == nil {
		t.Error("ToBigInteger(zz) 应报错")
	}
}

func TestFormatHex(t *testing.T) {
	if got := FormatHex("e88c670380cb220095268f"); got != "e8 8c 67 03 80 cb 22 00 95 26 8f" {
		t.Errorf("FormatHex = %q", got)
	}
	// 奇数长度: 原版循环条件是 i < length-1, 最后一个字符被丢弃
	if got := FormatHex("e88c6"); got != "e8 8c" {
		t.Errorf("FormatHex(奇数) = %q", got)
	}
	// 长度不足 2: 原版越界抛异常, Go 返回空串
	if got := FormatHex("a"); got != "" {
		t.Errorf("FormatHex(单位长度) = %q", got)
	}
}
