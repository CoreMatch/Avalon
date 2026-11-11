package bean

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/CoreMatch/Avalon/internal/util/file"
)

// TestNewCoreConfigUplistEnableDefault 自报开关默认保持自报
// (有意修正, 作者已批准, 2026-11-04, DECISIONS D-22)
func TestNewCoreConfigUplistEnableDefault(t *testing.T) {
	if !NewCoreConfig().UplistEnable {
		t.Fatal("uplistEnable 默认应为 true (保持自报)")
	}
}

// TestClientStartAllowed clientStart 三态语义 (解耦, DECISIONS D-26):
// "" 跟随 CascadeLobby (遗留), "allow"/"deny" 显式覆盖
func TestClientStartAllowed(t *testing.T) {
	cases := []struct {
		lobby bool
		mode  string
		want  bool
	}{
		{lobby: false, mode: "", want: true},      // 遗留非大厅: 允许
		{lobby: true, mode: "", want: false},      // 遗留大厅: 拒绝
		{lobby: true, mode: "allow", want: true},  // 大厅子服形态: 显式放行
		{lobby: false, mode: "allow", want: true}, // 显式放行与非大厅一致
		{lobby: true, mode: "deny", want: false},  // 显式拒绝与大厅一致
		{lobby: false, mode: "deny", want: false}, // 非大厅显式拒绝
	}
	for _, tc := range cases {
		c := CoreConfig{CascadeLobby: tc.lobby, ClientStart: tc.mode}
		if got := c.ClientStartAllowed(); got != tc.want {
			t.Errorf("CascadeLobby=%v ClientStart=%q: got %v, want %v",
				tc.lobby, tc.mode, got, tc.want)
		}
	}
}

// TestValidateCoreConfig clientStart 合法值通过, 非法值显式失败 (不静默纠正)
func TestValidateCoreConfig(t *testing.T) {
	for _, mode := range []string{"", "allow", "deny"} {
		if err := ValidateCoreConfig(CoreConfig{ClientStart: mode}); err != nil {
			t.Errorf("合法值 %q 应通过: %v", mode, err)
		}
	}
	for _, mode := range []string{"true", "false", "Allow", "DENY", "yes", "on"} {
		if err := ValidateCoreConfig(CoreConfig{ClientStart: mode}); err == nil {
			t.Errorf("非法值 %q 应使启动失败", mode)
		}
	}
}

// TestCoreConfigLoadClientStart 既有配置文件缺少 clientStart 键时保持默认 ""
// (遗留语义: 行为完全由 CascadeLobby 决定, 升级不产生无声变化)
func TestCoreConfigLoadClientStart(t *testing.T) {
	f := &file.FileUtils{Path: filepath.Join(t.TempDir(), "Config.json")}

	if err := os.WriteFile(f.Path, []byte(`{"cascadeLobby": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := LoadOrInit(f, NewCoreConfig())
	if c.ClientStart != "" {
		t.Fatalf("缺少 clientStart 的旧配置应保持默认 \"\", got %q", c.ClientStart)
	}
	if c.ClientStartAllowed() {
		t.Fatal("旧大厅配置升级后客户端开局应仍被拒绝 (遗留语义)")
	}

	if err := os.WriteFile(f.Path, []byte(`{"cascadeLobby": true, "clientStart": "allow"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c = LoadOrInit(f, NewCoreConfig())
	if !c.ClientStartAllowed() {
		t.Fatal("显式 clientStart=allow 应放行客户端开局")
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
