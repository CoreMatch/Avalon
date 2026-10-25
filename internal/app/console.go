// Package app 对应 net.rwhps.server.Main.inputMonitor.
package app

import (
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"rwhps/internal/data/global"
	"rwhps/internal/data/totalizer"
	"rwhps/internal/util/console"
	cmd "rwhps/internal/util/game/command"
	"rwhps/internal/util/log"
)

// exitGuard 对应 inputMonitor 里的 last 变量
//
// 原版用一个 Int 区分"上次是 Ctrl-C 还是 EOF", 第二次同类中断才真正退出.
// 因为 Ctrl-C 在 Go 里来自另一个 goroutine, 这里加锁共享.
type exitGuard struct {
	mu   sync.Mutex
	last int
}

// reset 对应读到一行后 last = 0
func (g *exitGuard) reset() {
	g.mu.Lock()
	g.last = 0
	g.mu.Unlock()
}

// second 标记当前中断类型, 返回这是否是连续第二次同类中断
func (g *exitGuard) second(mark int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.last == mark {
		return true
	}
	g.last = mark
	return false
}

// InputMonitor 对应 Main.inputMonitor: 控制台主循环
//
// 原版的三种中断来源在这里的对应关系:
//   - UserInterruptException (Ctrl-C) -> SIGINT, 由 watchInterrupt 处理
//   - EndOfFileException             -> console.ErrClosed
//   - InterruptedIOException         -> 其它读错误, 直接继续读
//
// -noPrint 模式下原版立刻 return, 这里保持一致.
func (r *Runtime) InputMonitor() {
	if r.Params.NoPrint {
		return
	}
	if r.Console == nil {
		r.initConsole()
	}

	guard := &exitGuard{}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT)
	defer signal.Stop(sig)

	done := make(chan struct{})
	defer close(done)

	// 原版 Ctrl-C 由 JLine 在终端 raw mode 下捕获并抛异常交给本循环;
	// Go 版没有 raw mode, Ctrl-C 只会作为 SIGINT 到达, 因此单独一个 goroutine 接收,
	// 与读循环共用 guard, 保证"两次"的计数是同一份.
	go func() {
		for {
			select {
			case <-done:
				return
			case <-sig:
				if guard.second(1) {
					r.Console.PrintAbove("force exit")
					r.ExitProcess(255)
					return
				}
				r.Console.PrintAbove(tr("server.exit.ctrl"))
			}
		}
	}()

	// 对应 TimeAndNumber(5, 10): 5 秒内连续 10 次异常就停止监控
	idling := totalizer.NewTimeAndNumber(5, 10)

	for {
		line, err := r.Console.ReadLine("> ")
		switch {
		case err == nil:
			guard.reset()
		case errors.Is(err, console.ErrClosed):
			if !guard.second(2) {
				r.Console.PrintAbove("Catch EndOfFile, again to exit application")
				continue
			}
			r.Console.PrintAbove("exit")
			r.ExitProcess(1)
			return
		default:
			// 对应 InterruptedIOException: 读被打断不算错误, 继续读
			continue
		}

		if line == "" {
			continue
		}

		if r.handleConsoleLine(line, idling) {
			return
		}
	}
}

// handleConsoleLine 执行一行命令并打印参数错误
//
// 返回 true 表示控制台循环应当结束 (对应原版 catch 分支里的 return).
func (r *Runtime) handleConsoleLine(line string, idling *totalizer.TimeAndNumber) (stop bool) {
	defer func() {
		if rec := recover(); rec != nil {
			// 原版把整段包在 try/catch 里; 命令实现 panic 不该终止控制台
			stop = r.reportIdling(idling, rec)
		}
	}()

	resp := global.ServerCommand.HandleMessageWith(line, global.DefPrint)
	if resp == nil || resp.Type == cmd.NoCommand || resp.Type == cmd.Valid {
		return false
	}

	switch resp.Type {
	case cmd.ManyArguments:
		log.Clog("Too many arguments. Usage: " + resp.Command.Text + " " + resp.Command.ParamText)
	case cmd.FewArguments:
		log.Clog("Too few arguments. Usage: " + resp.Command.Text + " " + resp.Command.ParamText)
	default:
		log.Clog("Unknown command. Check help")
	}
	return false
}

// reportIdling 对应 catch 里那段"错误太多就停掉监控"的逻辑, 返回是否停止监控
//
// 原版: Log.error(e) —— 直接打印异常. Go 的 log.Error 把参数当作 tag,
// 传 error 会打出空正文, 因此改用 Errorf.
func (r *Runtime) reportIdling(idling *totalizer.TimeAndNumber, rec any) bool {
	if idling.CheckStatus() {
		idling.Add(1)
		log.Clog("InputMonitor Error")
		log.Errorf("InputMonitor", "{0}", rec)
		return false
	}
	log.Clog("InputMonitor Idling")
	return true
}
