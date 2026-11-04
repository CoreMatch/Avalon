package bean

import (
	"os"
	"path/filepath"
	"testing"

	"rwhps/internal/util/file"
)

// TestNewCoreConfigUplistEnableDefault 自报开关默认保持自报
// (有意修正, 作者已批准, 2026-11-04, DECISIONS D-22)
func TestNewCoreConfigUplistEnableDefault(t *testing.T) {
	if !NewCoreConfig().UplistEnable {
		t.Fatal("uplistEnable 默认应为 true (保持自报)")
	}
}

// TestCoreConfigLoadUplistEnable 既有配置文件缺少 uplistEnable 键时保持默认 true;
// 显式 false 时按配置生效 (LoadOrInit 的 defaults-merge 语义)
func TestCoreConfigLoadUplistEnable(t *testing.T) {
	f := &file.FileUtils{Path: filepath.Join(t.TempDir(), "Config.json")}

	// 旧版配置 (无 uplistEnable 键): 保持默认自报, 其余字段照常加载
	if err := os.WriteFile(f.Path, []byte(`{"port": 5123, "serverName": "old"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := LoadOrInit(f, NewCoreConfig())
	if !c.UplistEnable {
		t.Fatal("缺少 uplistEnable 的旧配置应保持默认 true")
	}
	if c.Port != 5123 || c.ServerName != "old" {
		t.Fatalf("旧配置其余字段应照常加载: %+v", c)
	}

	// 显式关闭
	if err := os.WriteFile(f.Path, []byte(`{"uplistEnable": false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c = LoadOrInit(f, NewCoreConfig())
	if c.UplistEnable {
		t.Fatal("显式 uplistEnable=false 应生效")
	}
}
