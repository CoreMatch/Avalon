package algorithms

import (
	"encoding/hex"
	"testing"
)

// TestBigIntegerHex 对齐 BigInteger(1, bytes).toString(16)
func TestBigIntegerHex(t *testing.T) {
	cases := []struct {
		in   string // hex 输入
		want string
	}{
		{"", "0"},
		{"00", "0"},
		{"0000", "0"},
		{"01", "1"},
		{"0a", "a"},
		{"00ff", "ff"},
		{"0f0f", "f0f"},
		{"deadbeef", "deadbeef"},
	}
	for _, c := range cases {
		in, err := hex.DecodeString(c.in)
		if err != nil {
			t.Fatalf("bad fixture %q: %v", c.in, err)
		}
		if got := BigIntegerHex(in); got != c.want {
			t.Errorf("BigIntegerHex(%s) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestSHA256 长度与已知向量
func TestSHA256(t *testing.T) {
	sum := SHA256([]byte("abc"))
	if len(sum) != 32 {
		t.Fatalf("len = %d", len(sum))
	}
	if got := hex.EncodeToString(sum); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Errorf("sha256(abc) = %s", got)
	}
}
