// Package log 对应 net.rwhps.server.util.log.ex.PrintEx
package log

import (
	"fmt"
	"strings"
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
	fmt.Fprint(Output(), "Please Enter (Yes/No) > ")

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
			fmt.Fprint(Output(), "Please Enter (Yes/No) > ")
		}
	}
}
