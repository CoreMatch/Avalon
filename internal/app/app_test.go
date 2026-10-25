package app

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"rwhps/internal/data/global"
	"rwhps/internal/data/totalizer"
	"rwhps/internal/game"
	"rwhps/internal/net/core"
	"rwhps/internal/util/file"
	"rwhps/internal/util/file/load"
	"rwhps/internal/util/file/plugindata"
	"rwhps/internal/util/log"
)

// setupBase 把数据根目录指到临时目录, 并保存/恢复测试会改动的全局状态
func setupBase(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	file.SetBasePath(dir)

	oldBundle := global.I18NBundle
	oldMap := global.I18NBundleMap
	oldCountry := global.ServerCountry
	oldCore := global.Core
	oldHook := global.CloseServerHook
	oldBackend := game.Backend
	oldOut := log.Output()
	oldIn := log.Input()
	oldTranslator := log.Bundle
	oldLevel := log.FromLevel("WARN")

	t.Cleanup(func() {
		global.I18NBundle = oldBundle
		global.I18NBundleMap = oldMap
		global.ServerCountry = oldCountry
		global.Core = oldCore
		global.CloseServerHook = oldHook
		game.Backend = oldBackend
		log.SetOutput(oldOut)
		log.SetInput(oldIn)
		log.Bundle = oldTranslator
		log.Set(oldLevel.Name)
	})

	global.I18NBundleMap = map[string]*load.I18NBundle{}
	log.SetOutput(io.Discard)
	return dir
}

// seedSettings 预置 Settings.bin, 让启动链不去探测国家、也不弹许可提示
func seedSettings(t *testing.T) {
	t.Helper()

	s, err := plugindata.Open(file.GetFolder(global.ServerDataPath).ToFile("Settings.bin"))
	if err != nil {
		t.Fatalf("open settings: %v", err)
	}
	s.SetString("serverCountry", "EN")
	s.SetString("eulaVersion", global.ServerEulaVersion)
	s.SetBool("initStart", false)
	if err := s.Save(); err != nil {
		t.Fatalf("save settings: %v", err)
	}
}

func TestLoadLang(t *testing.T) {
	setupBase(t)

	if err := loadLang(); err != nil {
		t.Fatalf("loadLang: %v", err)
	}

	if len(global.I18NBundleMap) != 4 {
		t.Fatalf("语言包数量 = %d, 期望 4", len(global.I18NBundleMap))
	}
	for _, lang := range []string{"CN", "HK", "RU", "EN"} {
		if global.I18NBundleMap[lang] == nil {
			t.Fatalf("缺少语言包 %s", lang)
		}
	}
	if global.I18NBundle != global.I18NBundleMap["EN"] {
		t.Fatal("默认语言应为 EN")
	}
	if global.URLData == nil {
		t.Fatal("URLData 未初始化")
	}
	if got := global.URLData.ReadString("Get.Api.ServerLanguage.Bak", ""); !strings.Contains(got, "getCountry") {
		t.Fatalf("URLData 内容异常: %q", got)
	}
	if log.Bundle == nil {
		t.Fatal("log.Bundle 未注入")
	}
	if got := log.Bundle("server.hi"); !strings.Contains(got, "RW-HPS") {
		t.Fatalf("翻译结果异常: %q", got)
	}
}

func TestInitServerLanguageExplicit(t *testing.T) {
	setupBase(t)
	if err := loadLang(); err != nil {
		t.Fatalf("loadLang: %v", err)
	}

	s, err := plugindata.Open(file.GetFolder(global.ServerDataPath).ToFile("Settings.bin"))
	if err != nil {
		t.Fatalf("open settings: %v", err)
	}

	initServerLanguage(s, "CN")
	if global.ServerCountry != "CN" {
		t.Fatalf("ServerCountry = %q, 期望 CN", global.ServerCountry)
	}
	if global.I18NBundle != global.I18NBundleMap["CN"] {
		t.Fatal("语言包未切换到 CN")
	}
	if got := readString(s, "serverCountry"); got != "CN" {
		t.Fatalf("持久化的 serverCountry = %q, 期望 CN", got)
	}

	// 非 HK/CN/RU 一律回落到 EN
	initServerLanguage(s, "JP")
	if global.ServerCountry != "EN" {
		t.Fatalf("ServerCountry = %q, 期望 EN", global.ServerCountry)
	}
}

func TestInitServerLanguageUsesStoredValue(t *testing.T) {
	setupBase(t)
	if err := loadLang(); err != nil {
		t.Fatalf("loadLang: %v", err)
	}

	s, err := plugindata.Open(file.GetFolder(global.ServerDataPath).ToFile("Settings.bin"))
	if err != nil {
		t.Fatalf("open settings: %v", err)
	}
	// 已存过国家时不再发请求探测 (测试环境没有网络, 也不会卡在超时上)
	s.SetString("serverCountry", "RU")

	initServerLanguage(s, "")
	if global.ServerCountry != "RU" {
		t.Fatalf("ServerCountry = %q, 期望 RU", global.ServerCountry)
	}
}

func TestEULA(t *testing.T) {
	setupBase(t)
	if err := loadLang(); err != nil {
		t.Fatalf("loadLang: %v", err)
	}
	global.ServerCountry = "EN"

	s, err := plugindata.Open(file.GetFolder(global.ServerDataPath).ToFile("Settings.bin"))
	if err != nil {
		t.Fatalf("open settings: %v", err)
	}

	// 第一个提示同意许可, 第二个提示需要 WEB 面板初始化 (Yes 才会把 initStart 置 false)
	log.SetInput(strings.NewReader("Yes\nYes\n"))
	eula(s)

	if got := readString(s, "eulaVersion"); got != global.ServerEulaVersion {
		t.Fatalf("eulaVersion = %q, 期望 %q", got, global.ServerEulaVersion)
	}
	if s.GetBool("initStart", true) {
		t.Fatal("initStart 应被置为 false")
	}
	if !file.GetFolder(global.ServerDataPath).ToFile("Settings.bin").Exists() {
		t.Fatal("eula 结束后应把 Settings.bin 落盘")
	}
}

func TestEULASkipsWhenVersionMatches(t *testing.T) {
	setupBase(t)
	if err := loadLang(); err != nil {
		t.Fatalf("loadLang: %v", err)
	}

	s, err := plugindata.Open(file.GetFolder(global.ServerDataPath).ToFile("Settings.bin"))
	if err != nil {
		t.Fatalf("open settings: %v", err)
	}
	s.SetString("eulaVersion", global.ServerEulaVersion)
	s.SetBool("initStart", false)

	// 输入为空: 若仍尝试读许可, WaitLicense 会打印 "No console input" 并返回,
	// 但只要走了提示分支, 输出里就会出现 Yes/No 字样. 这里断言完全没有提示.
	var out bytes.Buffer
	log.SetOutput(&out)
	log.SetInput(strings.NewReader(""))
	eula(s)

	if strings.Contains(out.String(), "Yes/No") {
		t.Fatalf("不应再次询问许可, 实际输出: %q", out.String())
	}
}

func TestEventsFire(t *testing.T) {
	setupBase(t)
	log.SetOutput(io.Discard) // 处理器 panic 会被记录下来, 别污染测试输出

	bus := NewEvents()

	var got []string
	bus.Register("ServerLoadEvent", func(ev GlobalEvent) {
		got = append(got, ev.EventName())
	})
	// 单个处理器 panic 不应影响其它处理器
	bus.Register("ServerLoadEvent", func(GlobalEvent) { panic("boom") })

	bus.ServerLoad()

	if len(got) != 1 || got[0] != "ServerLoadEvent" {
		t.Fatalf("分发结果 = %v", got)
	}
	if n := bus.ListenerCount("ServerLoadEvent"); n != 2 {
		t.Fatalf("ListenerCount = %d, 期望 2", n)
	}

	// 没有监听者的事件不应 panic
	bus.ServerStartType(core.ServerProtocol)
	if n := bus.ListenerCount("ServerStartTypeEvent"); n != 0 {
		t.Fatalf("ListenerCount = %d, 期望 0", n)
	}
}

func TestExitGuard(t *testing.T) {
	g := &exitGuard{}

	if g.second(1) {
		t.Fatal("第一次 Ctrl-C 不应退出")
	}
	if !g.second(1) {
		t.Fatal("连续第二次 Ctrl-C 应退出")
	}

	g.reset()
	if g.second(2) {
		t.Fatal("换一种中断类型不应算作第二次")
	}
	if !g.second(2) {
		t.Fatal("连续第二次 EOF 应退出")
	}
}

func TestNewWiresGlobals(t *testing.T) {
	setupBase(t)

	r := New(global.MainParameters{})
	if global.Core != r.Core {
		t.Fatal("global.Core 未指向 Runtime.Core")
	}
	if global.CloseServerHook == nil {
		t.Fatal("CloseServerHook 未接上")
	}
	if r.Events == nil {
		t.Fatal("Events 未初始化")
	}
	if r.ExitProcess == nil {
		t.Fatal("ExitProcess 未设置")
	}
	// 没有任何 NetService 时关服不应 panic
	global.CloseServerHook()
}

func TestInputMonitorNoPrint(t *testing.T) {
	setupBase(t)

	r := New(global.MainParameters{NoPrint: true})

	done := make(chan struct{})
	go func() {
		r.InputMonitor()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("-noPrint 模式下 InputMonitor 应立即返回")
	}
}

func TestInputMonitorExitOnDoubleEOF(t *testing.T) {
	setupBase(t)

	r := New(global.MainParameters{})
	var code int
	r.ExitProcess = func(c int) { code = c }

	// 空输入: ReadLine 立刻返回 ErrClosed
	log.SetInput(strings.NewReader(""))

	var out bytes.Buffer
	log.SetOutput(&out)

	r.InputMonitor()

	if code != 1 {
		t.Fatalf("退出码 = %d, 期望 1", code)
	}
	if !strings.Contains(out.String(), "Catch EndOfFile") {
		t.Fatalf("缺少第一次 EOF 的提示: %q", out.String())
	}
	if !strings.Contains(out.String(), "exit") {
		t.Fatalf("缺少退出提示: %q", out.String())
	}
}

func TestHandleConsoleLineUnknownCommand(t *testing.T) {
	setupBase(t)

	r := New(global.MainParameters{})
	var out bytes.Buffer
	log.SetOutput(&out)

	idling := totalizer.NewTimeAndNumber(5, 10)
	if stop := r.handleConsoleLine("nosuchcommand", idling); stop {
		t.Fatal("未知命令不应停止监控")
	}
	if !strings.Contains(out.String(), "Unknown command. Check help") {
		t.Fatalf("输出 = %q", out.String())
	}
}

func TestHandleConsoleLineTooManyArguments(t *testing.T) {
	setupBase(t)

	r := New(global.MainParameters{})
	var out bytes.Buffer
	log.SetOutput(&out)

	global.ServerCommand.Register("say", "test", func([]string, any) {})
	idling := totalizer.NewTimeAndNumber(5, 10)
	if stop := r.handleConsoleLine("say a b", idling); stop {
		t.Fatal("参数过多不应停止监控")
	}
	if !strings.Contains(out.String(), "Too many arguments. Usage: say") {
		t.Fatalf("输出 = %q", out.String())
	}
}

func TestStart(t *testing.T) {
	setupBase(t)
	seedSettings(t)

	var out bytes.Buffer
	log.SetOutput(&out)

	r := New(global.MainParameters{})
	if err := r.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if r.Session == nil {
		t.Fatal("Session 未创建")
	}
	if r.Session.RoomID != global.ConfigServer.ServerID {
		t.Fatalf("RoomID = %q, 期望 %q", r.Session.RoomID, global.ConfigServer.ServerID)
	}
	if global.Config.Port == 0 {
		t.Fatal("Config 未加载")
	}
	if global.ConfigServer.MaxPlayer == 0 {
		t.Fatal("ConfigServer 未加载")
	}

	text := out.String()
	for _, want := range []string{"Welcome to RW-HPS", "github.com/deng-rui/RW-HPS", "Server loaded"} {
		if !strings.Contains(text, want) {
			t.Fatalf("启动输出缺少 %q\n实际输出:\n%s", want, text)
		}
	}

	// 默认命令是 start, 没有模拟后端时应当明确失败, 而不是假装已启动
	if global.StartServer {
		t.Fatal("没有模拟后端时 StartServer 应保持 false")
	}

	// 再次 Start 应当幂等
	if err := r.Start(); err != nil {
		t.Fatalf("重复 Start: %v", err)
	}
}
