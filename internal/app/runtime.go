// Package app 对应 net.rwhps.server.Main 以及它所编排的 core.Application / core.Initialization.
//
// 原版这三部分分别是一个 object (Main)、一个 class (Application, 实例挂在 Data.core)
// 和一个 class (Initialization, 构造时做静态初始化、伴生对象做启动期初始化)。
// Go 版把它们收敛成一个 Runtime: 启动链需要一个地方持有 core / console / events 三件状态,
// 而 Go 没有 object 与伴生对象, 分散成三个包反而要多绕一圈全局变量。
//
// 文件名对应关系:
//
//	runtime.go        Application.kt + Main.inputMonitorInit
//	initialization.go Initialization.kt
//	bootstrap.go      Main.main
//	console.go        Main.inputMonitor
//	events.go         EventGlobal.kt + PluginManage 的全局事件总线
package app

import (
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"rwhps/internal/command"
	"rwhps/internal/data/global"
	"rwhps/internal/game"
	"rwhps/internal/net"
	"rwhps/internal/util/console"
	"rwhps/internal/util/log"
)

// Runtime 对应 Main 对象 + Data.core + Data.privateReader 的组合体
type Runtime struct {
	// Core 对应 Data.core (core.Application 的实例)
	Core *global.Application

	// Console 对应 Data.privateReader (JLine 的 LineReader)
	Console *console.Console

	// Events 对应 EventGlobal + PluginManage 的全局事件总线
	Events *Events

	// Session 对应 HeadlessModuleManage.hps.room, 即单房间模式下的那一间
	//
	// 原版由游戏模块 (闭源 rusted-warfare.jar) 加载时创建; Go 版没有游戏模块,
	// 这里创建一个绑定 game.Backend 的空房间, 让 start / hi / version 有确定的落脚点.
	Session *game.GameSession

	// Params 对应 Data.mainParameters
	Params global.MainParameters

	// ExitProcess 结束进程, 默认 os.Exit
	//
	// 单独抽出来是为了让控制台的"按两次退出"逻辑可以被测试观察, 而不是直接杀测试进程.
	ExitProcess func(code int)

	mu       sync.Mutex
	started  bool
	shutdown bool
}

// New 对应 Main.main 开头创建 Data.core 的那一段
//
// 它不启动任何服务, 只把全局单例与进程级钩子接好:
//   - global.Core        = 新的 Application (data/Settings.bin)
//   - global.CloseServerHook = NetServer.closeServer (停止全部 NetService)
func New(params global.MainParameters) *Runtime {
	global.MainParametersValue = params

	r := &Runtime{
		Core:        global.NewApplication(),
		Events:      NewEvents(),
		Params:      params,
		ExitProcess: os.Exit,
	}

	global.Core = r.Core
	global.CloseServerHook = func() {
		for _, s := range net.NetStaticData.Services() {
			s.Stop()
		}
	}
	return r
}

// Run 对应 Main.main: 完整启动后进入控制台循环
//
// 原版 main 的最后一个动作是 newThreadCore(this::inputMonitor), main 随即返回,
// 进程靠 Netty 的非守护线程继续存活. Go 版没有这种"隐式存活", 因此 Run 会阻塞:
// 控制台循环返回 (仅 -noPrint 模式) 后再等一个退出信号.
func Run(args []string) error {
	r := New(global.CreateMainParameters(args))
	if err := r.Start(); err != nil {
		return err
	}
	r.InputMonitor()
	r.Wait()
	return nil
}

// Wait 阻塞到进程收到退出信号
//
// 原版 Ctrl-C 由 JLine 接管 (终端 raw mode), 不走进程信号; Go 版没有 raw mode,
// 因此 Ctrl-C 只会作为 SIGINT 到达, 由 console.go 的 watchInterrupt 处理.
// 这里只在控制台循环已经退出 (-noPrint) 时才等待信号, 两者不会争抢同一个 SIGINT.
func (r *Runtime) Wait() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sig)

	<-sig
	r.Core.Exit(nil)
}

// Save 对应 Application.save()
//
// 原版 save 里还有 runSavePool / dataAutoSave / runOnDisable 三份保存工作,
// 分别属于线程池、定时任务与插件系统, 都还没有移植; 这里保留 Settings 与日志刷盘.
func (r *Runtime) Save() {
	r.Core.Save()
	log.SaveLog()
}

// initConsole 对应 Main.inputMonitorInit
//
// 原版在这里做三件事: 换掉 System.in、把 System.out/err 接到 JLine 的 printAbove、
// 建带 Tab 补全的 LineReader. Go 版没有行编辑器:
//   - 提示符与日志共用同一个 writer, 不做 printAbove 重绘 (见 util/console 的说明)
//   - -noPrint 时按原版把输入输出全部丢弃, 并让 ReadLine 立刻返回 ErrClosed
func (r *Runtime) initConsole() {
	if r.Params.NoPrint {
		log.SetOutput(io.Discard)
		r.Console = console.New(nil, io.Discard)
		return
	}
	r.Console = console.New(log.InputReader(), log.Output())
}

// deps 组装命令层需要的外部能力
func (r *Runtime) deps() *command.Deps {
	d := command.NewDeps()
	d.Print = func(text string) { log.Clog(text) }
	d.Exit = func(run func()) { r.Core.Exit(run) }
	d.SetLanguage = func(lang string) { initServerLanguage(r.Core.Settings, lang) }
	d.ServerStartType = r.Events.ServerStartType
	d.Session = r.Session
	return d
}
