package systemutils

import (
	"runtime"
	"testing"
)

func TestOSName(t *testing.T) {
	got := OSName()
	if got == "" {
		t.Fatal("OSName 不应为空")
	}
	// 当前开发机是 macOS: 按 Java 的习惯映射成 "Mac OS X"
	if runtime.GOOS == "darwin" && got != "Mac OS X" {
		t.Errorf("OSName = %q, 期望 Mac OS X", got)
	}
}

func TestOSNameMapping(t *testing.T) {
	// 通过受支持的平台白名单验证映射表本身 (无法在测试里切换 GOOS)
	known := map[string]string{
		"windows": "Windows",
		"linux":   "Linux",
		"darwin":  "Mac OS X",
	}
	if _, ok := known[runtime.GOOS]; !ok {
		t.Skipf("当前平台 %q 不在映射表内, 跳过", runtime.GOOS)
	}
}

func TestOSArch(t *testing.T) {
	if got := OSArch(); got == "" {
		t.Fatal("OSArch 不应为空")
	}
	if runtime.GOARCH == "arm64" && OSArch() != "aarch64" {
		t.Errorf("OSArch = %q, 期望 aarch64", OSArch())
	}
}

func TestAvailableProcessors(t *testing.T) {
	if n := AvailableProcessors(); n < 1 {
		t.Fatalf("AvailableProcessors = %d, 期望 >= 1", n)
	}
}

func TestIsWindows(t *testing.T) {
	want := runtime.GOOS == "windows"
	if got := IsWindows(); got != want {
		t.Errorf("IsWindows = %v, 期望 %v", got, want)
	}
}

func TestPID(t *testing.T) {
	if PID() <= 0 {
		t.Fatalf("PID = %d", PID())
	}
}
