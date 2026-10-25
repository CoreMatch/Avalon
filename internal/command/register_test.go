package command

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"rwhps/internal/data/global"
	"rwhps/internal/game"
	"rwhps/internal/game/manage"
	"rwhps/internal/net"
	"rwhps/internal/net/core"
	cmd "rwhps/internal/util/game/command"
	"rwhps/internal/util/log"
)

// recorder 收集命令输出
type recorder struct{ lines []string }

func (r *recorder) print(s string) { r.lines = append(r.lines, s) }
func (r *recorder) text() string   { return strings.Join(r.lines, "\n") }

// setup 构造一个干净的命令环境
func setup(t *testing.T) (*cmd.Handler, *cmd.Handler, *Deps, *recorder) {
	t.Helper()

	server := cmd.NewHandler("")
	ping := cmd.NewHandler("")
	rec := &recorder{}
	d := NewDeps()
	d.Print = rec.print

	Register(server, ping, d)
	return server, ping, d, rec
}

// resetGlobals 复位被命令改动的全局状态
func resetGlobals(t *testing.T) {
	t.Helper()
	oldStart := global.StartServer
	oldBundle := log.Bundle
	t.Cleanup(func() {
		global.StartServer = oldStart
		log.Bundle = oldBundle
		for _, s := range net.NetStaticData.Services() {
			s.Stop()
		}
		net.NetStaticData.ResetRwHps()
	})
}

// withBundle 注入取词函数, 让输出里能看到 key 与实参
func withBundle(t *testing.T) {
	t.Helper()
	log.Bundle = func(key string, params ...any) string {
		if len(params) == 0 {
			return key
		}
		parts := make([]string, len(params))
		for i, p := range params {
			parts[i] = fmt.Sprint(p)
		}
		return key + "(" + strings.Join(parts, " ") + ")"
	}
	t.Cleanup(func() { log.Bundle = nil })
}

// TestRegisterCommandSet 确认命令集与原版一致, 且没有把 log 命令带过来
func TestRegisterCommandSet(t *testing.T) {
	server, ping, _, _ := setup(t)

	want := []string{"help", "stop", "version", "setlanguage", "exit", "plugins", "plugin", "tryupdate",
		"startrelay", "startrelaytest", "startnetservice", "start"}
	for _, name := range want {
		resp := server.HandleMessage(name + " ")
		if resp.Type == cmd.UnknownCommand {
			t.Fatalf("缺少命令 %q", name)
		}
	}

	if resp := server.HandleMessage("log "); resp.Type != cmd.UnknownCommand {
		t.Fatal("log 命令 (HotLoadClass 孵化器) 不应被移植")
	}

	// hi 注册在 PING_COMMAND 上
	if resp := ping.HandleMessage("hi"); resp.Type != cmd.Valid {
		t.Fatalf("ping 命令 hi 未注册: %v", resp.Type)
	}
}

// TestHelp 对应 help 命令的输出格式
func TestHelp(t *testing.T) {
	server, _, _, rec := setup(t)
	withBundle(t)

	server.HandleMessage("help")

	out := rec.text()
	if !strings.Contains(out, "Commands:") {
		t.Fatalf("缺少 Commands: 头: %q", out)
	}
	if !strings.Contains(out, "   version - serverCommands.version") {
		t.Fatalf("缺少 version 条目: %q", out)
	}
	// HIDE 的命令不出现
	if strings.Contains(out, "stop -") {
		t.Fatalf("HIDE 命令不应出现在 help 中: %q", out)
	}
	// 带参数的命令要拼出 <param>
	if !strings.Contains(out, "setlanguage <Language>") {
		t.Fatalf("缺少 setlanguage 参数提示: %q", out)
	}
}

// TestHelpHashDescription 以 # 开头的描述直接输出原文
func TestHelpHashDescription(t *testing.T) {
	h := cmd.NewHandler("")
	rec := &recorder{}
	d := NewDeps()
	d.Print = rec.print
	h.Register("x", "#raw text", func([]string, any) {})

	registerCore(h, d)
	h.HandleMessage("help")

	if !strings.Contains(rec.text(), "   x - raw text") {
		t.Fatalf("输出 = %q", rec.text())
	}
}

// TestStopWithoutServer 没有服务时提示 Server does not start
func TestStopWithoutServer(t *testing.T) {
	resetGlobals(t)
	server, _, _, rec := setup(t)

	server.HandleMessage("stop")

	if !strings.Contains(rec.text(), "Server does not start") {
		t.Fatalf("输出 = %q", rec.text())
	}
}

// TestStopStopsAllServices 有服务时逐个关闭
func TestStopStopsAllServices(t *testing.T) {
	resetGlobals(t)
	server, _, _, rec := setup(t)

	ns := net.NewNetService("t", core.NewNullRwHps(core.ServerProtocol))
	server.HandleMessage("stop")

	if strings.Contains(rec.text(), "Server does not start") {
		t.Fatalf("不应提示未启动: %q", rec.text())
	}
	found := false
	for _, s := range net.NetStaticData.Services() {
		if s == ns {
			found = true
		}
	}
	if found {
		t.Fatal("stop 后服务仍在列表中")
	}
}

// TestVersionWithoutProtocol NullProtocol 时只输出版本信息
func TestVersionWithoutProtocol(t *testing.T) {
	resetGlobals(t)
	server, _, _, rec := setup(t)
	withBundle(t)

	server.HandleMessage("version")

	out := rec.text()
	if !strings.Contains(out, "status.versionS") {
		t.Fatalf("缺少 versionS 输出: %q", out)
	}
	if !strings.Contains(out, global.ServerCoreVersion) {
		t.Fatalf("缺少版本号: %q", out)
	}
	if !strings.Contains(out, "NullProtocol") {
		t.Fatalf("缺少协议名: %q", out)
	}
	// NullProtocol 既不是服务端协议也不是 relay, 不应输出后续行
	if strings.Contains(out, "status.versionS.server") || strings.Contains(out, "status.versionS.relay") {
		t.Fatalf("NullProtocol 不应输出 server/relay 行: %q", out)
	}
}

// TestVersionServerProtocol 服务端协议输出房间行
func TestVersionServerProtocol(t *testing.T) {
	resetGlobals(t)
	server, _, d, rec := setup(t)
	withBundle(t)

	// 注册一个服务端协议并让 RwHps 指向它
	manageAddServer(t)
	d.Session = game.NewGameSession("room-x", nil)

	server.HandleMessage("version")

	out := rec.text()
	if !strings.Contains(out, "status.versionS.server") {
		t.Fatalf("缺少 server 行: %q", out)
	}
	if !strings.Contains(out, "room-x") {
		t.Fatalf("缺少 roomID: %q", out)
	}
	if !strings.Contains(out, "Crossing Large (10p)") {
		t.Fatalf("缺少地图名: %q", out)
	}
}

// TestSetLanguage 透传参数
func TestSetLanguage(t *testing.T) {
	server, _, d, _ := setup(t)

	var got string
	d.SetLanguage = func(lang string) { got = lang }

	server.HandleMessage("setlanguage CN")

	if got != "CN" {
		t.Fatalf("SetLanguage 收到 %q", got)
	}
}

// TestExit 触发退出回调
func TestExit(t *testing.T) {
	server, _, d, _ := setup(t)

	called := false
	d.Exit = func(run func()) {
		called = true
		if run != nil {
			run()
		}
	}

	server.HandleMessage("exit")

	if !called {
		t.Fatal("Exit 未被调用")
	}
}

// TestPlugins 插件列表为空时无输出, 有插件时按 plugin.info 格式化
func TestPlugins(t *testing.T) {
	server, _, d, rec := setup(t)
	withBundle(t)

	d.Plugins = func(each func(name, description, author, version string)) {
		each("core", "desc", "dr", "1.0")
	}

	server.HandleMessage("plugins")

	if !strings.Contains(rec.text(), "plugin.info(core desc dr 1.0)") {
		t.Fatalf("输出 = %q", rec.text())
	}
}

// TestPlugin 透传参数给插件管理器
func TestPlugin(t *testing.T) {
	server, _, d, _ := setup(t)

	var got []string
	d.PluginCommand = func(text string, out func(string)) {
		got = append(got, text)
	}

	server.HandleMessage("plugin list all")

	if len(got) != 1 || got[0] != "list all" {
		t.Fatalf("PluginCommand 收到 %q", got)
	}
}

// TestTryUpdate 如实报告不支持自更新
func TestTryUpdate(t *testing.T) {
	server, _, _, rec := setup(t)

	server.HandleMessage("tryupdate")

	if !strings.Contains(rec.text(), "not supported") {
		t.Fatalf("输出 = %q", rec.text())
	}
}

// TestStartRelayNotImplemented Relay 未移植时明确报告
func TestStartRelayNotImplemented(t *testing.T) {
	resetGlobals(t)
	server, _, _, rec := setup(t)

	server.HandleMessage("startrelay")
	server.HandleMessage("startrelaytest")

	if got := strings.Count(rec.text(), "not implemented"); got != 2 {
		t.Fatalf("输出 = %q", rec.text())
	}
}

// TestStartRelayRejectsWhenRunning 已有服务时拒绝
func TestStartRelayRejectsWhenRunning(t *testing.T) {
	resetGlobals(t)
	server, _, _, rec := setup(t)

	net.NewNetService("t", core.NewNullRwHps(core.ServerProtocol))
	server.HandleMessage("startrelay")

	if !strings.Contains(rec.text(), "The server is not closed") {
		t.Fatalf("输出 = %q", rec.text())
	}
}

// TestStartNetserviceNoParam 缺参数时打印提示且不 panic
//
// 原版这里走的是 Log.clog (而不是 StrCons), 所以断言要抓 log 的输出.
func TestStartNetserviceNoParam(t *testing.T) {
	resetGlobals(t)
	server, _, _, _ := setup(t)

	buf := &bytes.Buffer{}
	log.SetOutput(buf)
	log.SetColorEnabled(false)
	t.Cleanup(func() {
		log.SetOutput(nil)
		log.SetColorEnabled(true)
	})

	server.HandleMessage("startnetservice abc true")

	if !strings.Contains(buf.String(), "[Start Service] No parameter") {
		t.Fatalf("输出 = %q", buf.String())
	}
}

// TestStartNetserviceNoBind isPort=false 时只触发启动事件, 不绑定端口
func TestStartNetserviceNoBind(t *testing.T) {
	resetGlobals(t)
	server, _, d, _ := setup(t)

	var got core.NetType = core.NullProtocol
	d.ServerStartType = func(tp core.NetType) { got = tp }

	server.HandleMessageWith("startnetservice abc false", core.NewNullRwHps(core.HttpProtocol))

	if got != core.HttpProtocol {
		t.Fatalf("ServerStartType 收到 %v", got)
	}
	if len(net.NetStaticData.Services()) != 0 {
		t.Fatal("isPort=false 不应创建 NetService")
	}
}

// TestStartWithoutBackend 没有模拟内核时 start 失败并把标志复位
func TestStartWithoutBackend(t *testing.T) {
	resetGlobals(t)
	server, _, d, rec := setup(t)

	global.StartServer = false
	d.Session = game.NewGameSession("room-y", nil)

	server.HandleMessage("start")

	if global.StartServer {
		t.Fatal("开局失败后 StartServer 应复位")
	}
	if strings.Contains(rec.text(), "server.load.headless") {
		t.Fatalf("开局失败不应打印加载完成: %q", rec.text())
	}
}

// TestStartWhenAlreadyRunning 已启动时拒绝重复启动
func TestStartWhenAlreadyRunning(t *testing.T) {
	resetGlobals(t)
	server, _, d, rec := setup(t)

	global.StartServer = true
	d.Session = game.NewGameSession("room-z", nil)

	server.HandleMessage("start")

	if !strings.Contains(rec.text(), "The server is not closed") {
		t.Fatalf("输出 = %q", rec.text())
	}
}

// TestStartWithoutSession 没有房间时提示
func TestStartWithoutSession(t *testing.T) {
	resetGlobals(t)
	server, _, d, rec := setup(t)

	global.StartServer = false
	d.Session = nil

	server.HandleMessage("start")

	if !strings.Contains(rec.text(), "No room available") {
		t.Fatalf("输出 = %q", rec.text())
	}
}

// TestStartWithBackend 有后端时正常开局
func TestStartWithBackend(t *testing.T) {
	resetGlobals(t)
	server, _, d, _ := setup(t)
	withBundle(t)

	global.StartServer = false
	d.Session = game.NewGameSession("room-ok", stubSim{})

	server.HandleMessage("start")

	if !global.StartServer {
		t.Fatal("开局成功后 StartServer 应为 true")
	}
	if !d.Session.IsStartGame {
		t.Fatal("房间应处于开局状态")
	}
}

// TestHiWithoutConnection 没有连接对象时静默返回
func TestHiWithoutConnection(t *testing.T) {
	resetGlobals(t)
	_, ping, _, _ := setup(t)

	resp := ping.HandleMessageWith("hi", nil)
	if resp.Type != cmd.Valid {
		t.Fatalf("hi 应被视为合法命令: %v", resp.Type)
	}
}

// TestAtoiOr 端口解析失败回退默认值
func TestAtoiOr(t *testing.T) {
	cases := map[string]int{"": 0, "5201": 5201, "x": 0, "1x": 0, "-1": 0}
	for in, want := range cases {
		if got := atoiOr(in, 0); got != want {
			t.Fatalf("atoiOr(%q) = %d, want %d", in, got, want)
		}
	}
	if got := atoiOr("bad", 7); got != 7 {
		t.Fatalf("atoiOr(bad,7) = %d", got)
	}
}

// stubSim 一个永远成功的模拟后端
type stubSim struct{}

func (stubSim) Start(game.MatchConfig) error { return nil }
func (stubSim) Stop() error                  { return nil }
func (stubSim) Running() bool                { return true }
func (stubSim) PlayerCount() int             { return 1 }

// manageAddServer 注册一个 ServerProtocol 实现, 使 NetStaticData.RwHps() 指向它
func manageAddServer(t *testing.T) {
	t.Helper()
	manage.AddIRwHps(core.ServerProtocol, "cmdtest")
	net.NetStaticData.ResetRwHps()
	t.Cleanup(func() {
		manage.Clear()
		net.NetStaticData.ResetRwHps()
	})
}
