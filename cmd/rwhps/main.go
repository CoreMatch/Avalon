// rwhps 主程序入口 (对应 net.rwhps.server.Main)
//
// 整个启动流程在 internal/app 里 (runtime / initialization / bootstrap / console / events),
// 这里只做两件事: 把命令行参数交给 app.Run, 以及在启动失败时落一条日志.
package main

import (
	"fmt"
	"os"

	"rwhps/internal/app"
	"rwhps/internal/util/log"
)

func main() {
	if err := app.Run(os.Args[1:]); err != nil {
		// 注意: log.Error 不是 printf 风格 (对应 Kotlin 的 vararg 拼接), 先自己格式化
		log.Error(fmt.Sprintf("start failed: %v", err))
		os.Exit(1)
	}
}
