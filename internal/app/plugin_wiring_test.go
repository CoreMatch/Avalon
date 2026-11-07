package app

// 插件扩展点在启动链的接线测试 (ADR-0006).

import (
	"strings"
	"testing"

	"github.com/CoreMatch/Avalon/internal/data/global"
	"github.com/CoreMatch/Avalon/internal/game/player"
	"github.com/CoreMatch/Avalon/internal/net/core"
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

// TestConnectionNewHookComposition 连接钩子组合 (契约 v1.2.0 connection.new):
// 内建 IP/24 ban 先行 (命中不咨询插件), 插件否决 = 拒绝连接, 放行 = 通过
func TestConnectionNewHookComposition(t *testing.T) {
	setupBase(t)
	cleanupPluginHub(t)

	srv := plugintest.Start(t, true, "blocked exit node")
	srv.SetVetoDeclare(true)
	plugintest.Install(t, srv.EntryWithVeto("gate", plugin.EventConnectionNew))

	r := New(global.MainParameters{})
	r.registerEventGlobal()

	// 插件否决 -> 拒绝连接 (理由只进日志, 钩子只表达拒绝)
	if !core.NetConnectNewEventHook(&core.ConnectionAgreement{
		IP: "203.0.113.7", IPLong24: "203.0.113", UseAgreementStr: "TCP-NIO",
	}) {
		t.Fatal("插件否决时应拒绝连接")
	}
	if n := srv.RequestsOf("/v1/events/connection.new"); n != 1 {
		t.Fatalf("准入咨询应恰好 1 次, got %d", n)
	}

	// IP/24 ban 命中 -> 直接拒绝, 不咨询插件
	global.Core.Admin.BanIP24("198.51.100")
	before := srv.RequestsOf("/v1/events/connection.new")
	if !core.NetConnectNewEventHook(&core.ConnectionAgreement{
		IP: "198.51.100.9", IPLong24: "198.51.100", UseAgreementStr: "TCP-NIO",
	}) {
		t.Fatal("banned /24 应拒绝连接")
	}
	if n := srv.RequestsOf("/v1/events/connection.new"); n != before {
		t.Fatalf("ban 命中不应咨询插件, 请求数 %d -> %d", before, n)
	}

	// 插件放行 -> 连接通过
	srv.SetDeny(false, "")
	if core.NetConnectNewEventHook(&core.ConnectionAgreement{
		IP: "192.0.2.5", IPLong24: "192.0.2", UseAgreementStr: "TCP-NIO",
	}) {
		t.Fatal("插件放行时应通过连接")
	}
}

// TestConnectionNewHookNoPluginZeroRegression 未配置插件: 钩子行为与
// 纯 IP/24 ban 完全一致 (零回归)
func TestConnectionNewHookNoPluginZeroRegression(t *testing.T) {
	setupBase(t)
	cleanupPluginHub(t)

	r := New(global.MainParameters{})
	r.registerEventGlobal()

	ca := &core.ConnectionAgreement{IP: "203.0.113.7", IPLong24: "203.0.113", UseAgreementStr: "TCP-NIO"}
	if core.NetConnectNewEventHook(ca) {
		t.Fatal("无插件时正常连接应通过")
	}
	global.Core.Admin.BanIP24("203.0.113")
	if !core.NetConnectNewEventHook(ca) {
		t.Fatal("无插件时 banned /24 仍应拒绝")
	}
}

// TestBootWiresKickAudit 启动链装上踢出审计钩子 (player.kicked 接线)
func TestBootWiresKickAudit(t *testing.T) {
	setupBase(t)
	seedSettings(t)
	cleanupPluginHub(t)
	oldKick := player.OnKicked
	t.Cleanup(func() { player.OnKicked = oldKick })

	srv := plugintest.Start(t, false, "")
	plugintest.WriteConfig(t, srv.Entry("audit"))

	r := New(global.MainParameters{})
	if err := r.Start(); err != nil {
		t.Fatalf("启动: %v", err)
	}
	if player.OnKicked == nil {
		t.Fatal("启动链应装上踢出审计钩子")
	}
}
