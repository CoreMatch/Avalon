package compression

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestGzipRoundtrip 压缩-解压往返 (含空数据)
func TestGzipRoundtrip(t *testing.T) {
	for _, data := range [][]byte{
		[]byte("hello avalon"),
		make([]byte, 0),
		make([]byte, 1<<16), // 全零大块, 压缩率敏感
	} {
		out, err := GzipDecode(GzipEncode(data))
		if err != nil {
			t.Fatalf("roundtrip err: %v", err)
		}
		if len(out) != len(data) {
			t.Fatalf("roundtrip 长度 = %d, want %d", len(out), len(data))
		}
		for i := range data {
			if out[i] != data[i] {
				t.Fatal("roundtrip 内容不一致")
			}
		}
	}
}

// TestGzipDecodeInvalid 非 gzip 输入明确报错
func TestGzipDecodeInvalid(t *testing.T) {
	if _, err := GzipDecode([]byte("not gzip at all")); err == nil {
		t.Fatal("非法输入应报错")
	}
}

// makeZip 造一个含指定条目的临时 zip, 返回路径
func makeZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pack.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestZipFileNames 按裸后缀过滤并去后缀 (上游语义: endWith 不带点)
func TestZipFileNames(t *testing.T) {
	p := makeZip(t, map[string]string{
		"maps/inner.tmx": "<map/>",
		"other.tmx":      "<map/>",
		"readme.txt":     "hi",
	})

	names, err := ZipFileNames(p, "tmx")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Fatalf("应命中 2 个 tmx 条目, got %v", names)
	}
	seen := map[string]bool{}
	for _, n := range names {
		seen[n] = true
	}
	if !seen["maps/inner"] || !seen["other"] {
		t.Fatalf("条目名应去掉后缀: %v", names)
	}

	// 文件不存在
	if _, err := ZipFileNames(filepath.Join(t.TempDir(), "nope.zip"), "tmx"); err == nil {
		t.Fatal("zip 缺失应报错")
	}
}

// TestZipEntryBytes 第一个"后缀命中且名字包含"的条目内容
func TestZipEntryBytes(t *testing.T) {
	p := makeZip(t, map[string]string{
		"a.txt":   "wrong",
		"inner.tmx": "<map-inner/>",
	})

	// suffix 带点 (与 MapType.fileType 一致)
	b, err := ZipEntryBytes(p, ".tmx", "inner")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "<map-inner/>" {
		t.Fatalf("条目内容 = %q", b)
	}

	// 名字不包含 -> 未命中
	if _, err := ZipEntryBytes(p, ".tmx", "ghost"); !errors.Is(err, ErrZipEntryNotFound) {
		t.Fatalf("未命中应为 ErrZipEntryNotFound, got %v", err)
	}
	// 后缀不匹配 -> 未命中
	if _, err := ZipEntryBytes(p, ".zip", "inner"); !errors.Is(err, ErrZipEntryNotFound) {
		t.Fatalf("后缀不匹配应为 ErrZipEntryNotFound, got %v", err)
	}
	// zip 缺失
	if _, err := ZipEntryBytes(filepath.Join(t.TempDir(), "nope.zip"), ".tmx", "x"); err == nil {
		t.Fatal("zip 缺失应报错")
	}
}
