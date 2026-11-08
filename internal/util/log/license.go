// Package log 对应 上游 util.log.ex.PrintEx
package log

import (
	"bufio"
	"strings"
	"time"
)

// WaitLicense 对应 PrintEx.waitLicense
//
// 展示许可文本 (show), 然后循环读取 Yes/No:
//   - Yes -> 打印 Thanks ! 并执行 ok
//   - No  -> 打印 Thanks ! 并执行 no
//   - 其他 -> 打印 Re Enter 并重新提示
//
// 与原版的差异 (都是为了让 Go 版在无控制台时行为确定):
//  1. 原版 stdin 关闭时 nextLine() 抛 NoSuchElementException, 直接冒泡崩溃;
//     Go 版在读到 EOF 时打印一条提示并返回, 不执行 ok 也不执行 no ——
//     既不静默同意许可, 也不替用户退出进程.
//  2. 原版 "No" 分支执行 no() 后没有 return, 若 no() 不终止进程会静默空转;
//     Go 版在此 return.
//  3. 读取器是进程级共享的 (InputReader), 而不是每次新建一个 Scanner ——
//     原版那个 Scanner 会预读并吞掉管道输入里的后续行.
func WaitLicense(show, ok, no func()) {
	if show != nil {
		show()
	}

	Clog("Agree to enter : Yes , Otherwise please enter : No")
	WriteOutput("Please Enter (Yes/No) > ")

	reader := InputReader()
	for {
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			Clog("No console input, license not accepted")
			return
		}

		switch {
		case strings.EqualFold(strings.TrimRight(line, "\r\n"), "Yes"):
			Clog("Thanks !")
			if ok != nil {
				ok()
			}
			return
		case strings.EqualFold(strings.TrimRight(line, "\r\n"), "No"):
			Clog("Thanks !")
			if no != nil {
				no()
			}
			return
		default:
			Clog("Re Enter")
			WriteOutput("Please Enter (Yes/No) > ")
		}
	}
}

// WaitLicenseBounded 是 WaitLicense 的有界版本: timeout 内没有合法应答则
// 返回 false, 且不执行 ok/no —— 由调用方决定默认行为. 返回 true 表示收到
// 了合法应答 (Yes/No 已按原语义执行对应回调).
//
// 为什么需要它: 进程管理器 (如 Tinyvisor) 以 PTY 拉起进程时, stdin 是终端
// 却无人应答, WaitLicense 的无限阻塞读取会把整条启动链卡死 (2026-11-08
// 隔离实测). 无人值守场景必须有确定的时间上界.
//
// 读取经独立的 bufio (不共享控制台的 InputReader): 超时后遗留的读取协程
// 至多再吞掉一行后续输入, 不会与 InputMonitor 竞用同一个 bufio.Reader.
func WaitLicenseBounded(show, ok, no func(), timeout time.Duration) bool {
	if show != nil {
		show()
	}

	Clog("Agree to enter : Yes , Otherwise please enter : No")
	WriteOutput("Please Enter (Yes/No) > ")

	type readResult struct {
		line string
		err  error
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	reader := bufio.NewReader(Input())

	for {
		ch := make(chan readResult, 1)
		go func() {
			line, err := reader.ReadString('\n')
			ch <- readResult{line, err}
		}()

		select {
		case res := <-ch:
			if res.err != nil && res.line == "" {
				Clog("No console input, license not accepted")
				return false
			}
			answer := strings.TrimRight(res.line, "\r\n")
			switch {
			case strings.EqualFold(answer, "Yes"):
				Clog("Thanks !")
				if ok != nil {
					ok()
				}
				return true
			case strings.EqualFold(answer, "No"):
				Clog("Thanks !")
				if no != nil {
					no()
				}
				return true
			default:
				Clog("Re Enter")
				WriteOutput("Please Enter (Yes/No) > ")
			}
		case <-deadline.C:
			Clog("License prompt timed out without an answer; proceeding without acceptance")
			return false
		}
	}
}
