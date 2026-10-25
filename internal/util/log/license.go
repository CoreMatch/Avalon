// Package log 对应 net.rwhps.server.util.log.ex.PrintEx
package log

import (
	"bufio"
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
// 与原版的两处差异 (都是为了让 Go 版在无控制台时行为确定):
//  1. 原版 stdin 关闭时 nextLine() 抛 NoSuchElementException, 直接冒泡崩溃;
//     Go 版在 Scan 失败时打印一条提示并返回, 不执行 ok 也不执行 no ——
//     既不静默同意许可, 也不替用户退出进程.
//  2. 原版 "No" 分支执行 no() 后没有 return, 若 no() 不终止进程会静默空转;
//     Go 版在此 return.
func WaitLicense(show, ok, no func()) {
	if show != nil {
		show()
	}

	Clog("Agree to enter : Yes , Otherwise please enter : No")
	fmt.Fprint(Output(), "Please Enter (Yes/No) > ")

	sc := bufio.NewScanner(Input())
	for {
		if !sc.Scan() {
			Clog("No console input, license not accepted")
			return
		}

		switch {
		case strings.EqualFold(sc.Text(), "Yes"):
			Clog("Thanks !")
			if ok != nil {
				ok()
			}
			return
		case strings.EqualFold(sc.Text(), "No"):
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
