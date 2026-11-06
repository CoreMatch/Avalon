package file

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPathProcess 对应 FileUtils.pathProcess
func TestPathProcess(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/a/b", "/a/b/"},
		{"/a/b/", "/a/b/"},
		{"C:\\a\\b", "C:/a/b/"},
		{"/C:/a/b", "C:/a/b/"},
	}
	for _, c := range cases {
		if got := pathProcess(c.in); got != c.want {
			t.Errorf("pathProcess(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestCehckFolderPath 对应 FileUtils.cehckFolderPath
func TestCehckFolderPath(t *testing.T) {
	cases := []struct{ def, path, want string }{
		{"/base/", "", "/base/"},
		{"/base/", "/data", "/base/data"},
		{"/base/", "data", "/base/data"},
		{"/base/", "data/", "/base/data"},
		{"/base", "a/b", "/base/a/b"},
	}
	for _, c := range cases {
		if got := cehckFolderPath(c.def, c.path); got != c.want {
			t.Errorf("cehckFolderPath(%q,%q) = %q, want %q", c.def, c.path, got, c.want)
		}
	}
}

// TestBasePathAnchoring 验证绝对路径被锚定到 BasePath, 而不是文件系统根目录
func TestBasePathAnchoring(t *testing.T) {
	base := t.TempDir()
	SetBasePath(base)
	t.Cleanup(func() { SetBasePath(base) })

	want := filepath.ToSlash(base) + "/"
	if !strings.HasSuffix(BasePath(), "/") {
		t.Fatalf("BasePath() = %q, want trailing slash", BasePath())
	}
	if BasePath() != want {
		t.Fatalf("BasePath() = %q, want %q", BasePath(), want)
	}

	if got, want := GetFolder("/data").Path, filepath.ToSlash(base)+"/data"; got != want {
		t.Errorf("GetFolder(/data) = %q, want %q", got, want)
	}
	if got, want := GetFolder("data").Path, filepath.ToSlash(base)+"/data"; got != want {
		t.Errorf("GetFolder(data) = %q, want %q", got, want)
	}
	if got, want := GetFolder("").Path, filepath.ToSlash(base)+"/"; got != want {
		t.Errorf("GetFolder(\"\") = %q, want %q", got, want)
	}
	if got, want := GetFolder("a/b/").Path, filepath.ToSlash(base)+"/a/b"; got != want {
		t.Errorf("GetFolder(a/b/) = %q, want %q", got, want)
	}
	if got, want := GetFile("/data/Settings.bin").Path, filepath.ToSlash(base)+"/data/Settings.bin"; got != want {
		t.Errorf("GetFile = %q, want %q", got, want)
	}
	if got, want := GetPath("/maps"), filepath.ToSlash(base)+"/maps"; got != want {
		t.Errorf("GetPath = %q, want %q", got, want)
	}
}

// TestGetFolderCreates 对应 getFolder 的 mkdirs 行为
func TestGetFolderCreates(t *testing.T) {
	base := t.TempDir()
	SetBasePath(base)
	t.Cleanup(func() { SetBasePath(base) })

	f := GetFolder("/data/maps")
	if !f.IsFolder() {
		t.Fatalf("GetFolder should create directory, %q is not a folder", f.Path)
	}
}

// TestFileRoundTrip 验证读写
func TestFileRoundTrip(t *testing.T) {
	base := t.TempDir()
	SetBasePath(base)
	t.Cleanup(func() { SetBasePath(base) })

	f := GetFile("/data/test.txt")
	f.WriteFile("hello")
	if got := f.ReadFileStringData(); got != "hello" {
		t.Fatalf("ReadFileStringData = %q, want hello", got)
	}
	if b, err := f.ReadFileBytes(); err != nil || string(b) != "hello" {
		t.Fatalf("ReadFileBytes = %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(base, "data", "test.txt")); err != nil {
		t.Fatalf("file not anchored under base: %v", err)
	}
	f.Delete()
	if f.Exists() {
		t.Fatal("Delete did not remove file")
	}
}
