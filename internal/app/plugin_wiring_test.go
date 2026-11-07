package app

// 插件扩展点在启动链的接线测试 (ADR-0006).

import (
	"strings"
	"testing"

	"github.com/CoreMatch/Avalon/internal/data/global"
	"github.com/CoreMatch/Avalon/internal/plugin"
	"github.com/CoreMatch/Avalon/internal/plugin/plugintest"
)

// cleanupPluginHub 测试收尾: 关闭 Start 加载的 hub 并清空全局槽位
func cleanupPluginHub(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		if h := plugin.Default(); h != nil {
			h.Close()
			plugin.SetDefault(nil)
		}
	})
}

// TestStartVetoedByPluginAdmission 启动期单例房间被插件否决:
// 启动失败且错误携带插件的可读理由
func TestStartVetoedByPluginAdmission(t *testing.T) {
	setupBase(t)
	seedSettings(t)
	cleanupPluginHub(t)

	srv := plugintest.Start(t, true, "boot veto reason")
	plugintest.WriteConfig(t, srv.Entry("gate"))

	r := New(global.MainParameters{})
	err := r.Start()
	if err == nil {
		t.Fatal("否决建房时启动必须失败")
	}
	if !strings.Contains(err.Error(), "boot veto reason") {
		t.Fatalf("启动错误应携带插件理由: %v", err)
	}
	if r.Session != nil {
		t.Fatal("否决后不应创建房间")
	}
}

// TestStartPassesWithAllowingPlugin 插件放行: 启动链零回归
func TestStartPassesWithAllowingPlugin(t *testing.T) {
	setupBase(t)
	seedSettings(t)
	cleanupPluginHub(t)

	srv := plugintest.Start(t, false, "")
	plugintest.WriteConfig(t, srv.Entry("gate"))

	r := New(global.MainParameters{})
	if err := r.Start(); err != nil {
		t.Fatalf("放行时启动应成功: %v", err)
	}
	if r.Session == nil {
		t.Fatal("Session 未创建")
	}
	if srv.Requests() == 0 {
		t.Fatal("启动期准入调用应到达插件")
	}
}

// TestPluginConsoleCommandShowsStatus 上游 plugin 命令接缝接上进程外插件
// 状态输出 (只读)
func TestPluginConsoleCommandShowsStatus(t *testing.T) {
	setupBase(t)

	srv := plugintest.Start(t, false, "")
	plugintest.Install(t, srv.Entry("gate"))

	r := New(global.MainParameters{})
	var lines []string
	r.deps().PluginCommand("list", func(s string) { lines = append(lines, s) })

	if len(lines) != 1 {
		t.Fatalf("应输出 1 行状态, got %d: %v", len(lines), lines)
	}
	for _, want := range []string{"gate", "state=ready", "room.create", "game.over"} {
		if !strings.Contains(lines[0], want) {
			t.Fatalf("状态行缺 %q: %s", want, lines[0])
		}
	}
}
