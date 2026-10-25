package plugindata

import (
	"bytes"
	"encoding/base64"
	"testing"

	"rwhps/internal/util/file"
)

// fixture 由独立实现 (python gzip + struct) 生成, 用来钉死 Bin 布局:
// gzip( int32 2, "a"=bool true, "b"="hi" )
const fixture = "H4sIAAAAAAAC/2NgYGBiYExkYGRgTGJhYMrIBADcywC5EQAAAA=="

// TestDecodeFixture 校验与 Java 侧一致的字节布局
func TestDecodeFixture(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString(fixture)
	if err != nil {
		t.Fatal(err)
	}
	data, err := decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if data.Size() != 2 {
		t.Fatalf("size = %d, want 2", data.Size())
	}
	a, ok := data.Get("a")
	if !ok || a.Kind != KindBool || !a.Bool {
		t.Errorf("a = %+v", a)
	}
	b, ok := data.Get("b")
	if !ok || b.Kind != KindString || b.Str != "hi" {
		t.Errorf("b = %+v", b)
	}
}

// TestRoundTrip 所有类型写入后重新打开应完全一致, 包括 tag 5 的原样字节
func TestRoundTrip(t *testing.T) {
	base := t.TempDir()
	file.SetBasePath(base)
	t.Cleanup(func() { file.SetBasePath(base) })

	f := file.GetFile("/data/Settings.bin")
	s, err := Open(f)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.SetString("serverConnectUuid", "abc-123")
	s.SetBool("initStart", true)
	s.SetInt("eulaVersion", 7)
	s.SetLong("runPid", 1<<40)
	s.SetFloat("ratio", 1.5)
	rawBytes := []byte{0xAC, 0xED, 0x00, 0x01}
	s.SetRaw("admin", "net.rwhps.server.net.Administration", rawBytes)
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	re, err := Open(f)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if re.Size() != 6 {
		t.Fatalf("size = %d, want 6", re.Size())
	}
	if got := re.GetString("serverConnectUuid", func() string { return "fallback" }); got != "abc-123" {
		t.Errorf("string = %q", got)
	}
	if !re.GetBool("initStart", false) {
		t.Error("bool lost")
	}
	if got := re.GetInt("eulaVersion", 0); got != 7 {
		t.Errorf("int = %d", got)
	}
	if got := re.GetLong("runPid", 0); got != 1<<40 {
		t.Errorf("long = %d", got)
	}
	if got := re.GetFloat("ratio", 0); got != 1.5 {
		t.Errorf("float = %v", got)
	}
	v, ok := re.Get("admin")
	if !ok || v.Kind != KindRaw || v.Raw.Class != "net.rwhps.server.net.Administration" || !bytes.Equal(v.Raw.Bytes, rawBytes) {
		t.Errorf("raw = %+v", v)
	}
}

// TestDefaultValueWritesBack GetString 缺省值应写入存储
func TestDefaultValueWritesBack(t *testing.T) {
	base := t.TempDir()
	file.SetBasePath(base)
	t.Cleanup(func() { file.SetBasePath(base) })

	s, err := Open(file.GetFile("/data/Settings.bin"))
	if err != nil {
		t.Fatal(err)
	}
	called := 0
	got := s.GetString("serverCountry", func() string { called++; return "EN" })
	if got != "EN" || called != 1 {
		t.Fatalf("got %q, called %d", got, called)
	}
	if again := s.GetString("serverCountry", func() string { called++; return "CN" }); again != "EN" || called != 1 {
		t.Errorf("default should not be recomputed: %q, called %d", again, called)
	}
}

// TestCorruptFileIsReadOnly 解码失败时拒绝覆盖原文件
func TestCorruptFileIsReadOnly(t *testing.T) {
	base := t.TempDir()
	file.SetBasePath(base)
	t.Cleanup(func() { file.SetBasePath(base) })

	f := file.GetFile("/data/Settings.bin")
	f.WriteFileBytes([]byte("not gzip at all"))
	before, _ := f.ReadFileBytes()

	s, err := Open(f)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !s.ReadOnly() {
		t.Error("store should be read-only")
	}
	if err := s.Save(); err != ErrReadOnly {
		t.Errorf("Save = %v, want ErrReadOnly", err)
	}
	after, _ := f.ReadFileBytes()
	if !bytes.Equal(before, after) {
		t.Error("corrupt file must not be overwritten")
	}
}

// TestMissingFile 不存在的文件视为空存储
func TestMissingFile(t *testing.T) {
	base := t.TempDir()
	file.SetBasePath(base)
	t.Cleanup(func() { file.SetBasePath(base) })

	s, err := Open(file.GetFile("/data/Settings.bin"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if s.Size() != 0 {
		t.Errorf("size = %d, want 0", s.Size())
	}
	if s.ReadOnly() {
		t.Error("missing file should not be read-only")
	}
}
