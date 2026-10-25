// Package log 对应 net.rwhps.server.util.log.LoadLogUtils
package log

import (
	"fmt"
)

// Translator 对应 Data.i18NBundle.getinput
//
// 用函数注入而不是直接 import global / load: global 依赖本包,
// log -> global 会形成循环依赖.
type Translator func(key string, params ...any) string

// Bundle 由启动链注入的国际化取词函数
//
// 未注入时 getInput 退回 key 本身 (原版此处会 NPE), 便于单测与早期启动阶段使用.
var Bundle Translator

// getInput 对应 Data.i18NBundle.getinput(input, ...)
func getInput(key string, params ...any) string {
	if Bundle == nil {
		return key
	}
	return Bundle(key, params...)
}

// LoadStatusLog 对应 LoadLogUtils.loadStatusLog
//
// 输出形如:
//
//	[2024-01-01 00:00:00] <key>Start    <- 不换行, 等待 run 完成
//	[2024-01-01 00:00:00] <key>OK       <- 成功, 另起一行
//	[2024-01-01 00:00:00] <key>ERROR    <- 失败, 另起一行, 并记录一次 track
//
// 原版用 try/catch(Exception) 捕获失败; Go 版用 recover 捕获 panic,
// 因为这里唯一会中断加载流程的就是 panic.
func LoadStatusLog(input string, run func()) {
	printOverride(getInput(input, "Start"), false)

	failed := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				failed = true
				printOverride(getInput(input, "ERROR"), true)
				logs(LevelTrack, input, fmt.Errorf("%v", r))
			}
		}()
		run()
	}()
	if failed {
		return
	}

	printOverride(getInput(input, "OK"), true)
}

// printOverride 对应 LoadLogUtils.printOverride
//
// 与原版一致: 直接写终端, 不经过 logs(), 因此没有级别前缀也没有 [INFO] 标记;
// nextLine 为 false 时用 \r + 行尾空格覆盖上一行, 让"正在加载"与"OK"落在同一行.
func printOverride(text string, nextLine bool) {
	line := "[" + timestamp() + "] " + FormatColors(text+"&fr", false)
	if nextLine {
		fmt.Fprint(Output(), "\r"+line+"   \n")
		return
	}
	fmt.Fprint(Output(), line)
}
