// Command plugin-events 逐行打印 hub 注册的事件全集 (契约事件名的代码真相).
//
// 消费方:
//   - HA-Contract scripts/check-avalon-plugin-event-drift.sh (三方一致性检查:
//     代码 / openapi / docs/plugins/EVENT-CATALOG.md 机器区块)
//   - 人工排查事件漂移
//
// 输出约定: 每行一个事件名, 无其他输出; 非零退出码表示取不到事件集.
package main

import (
	"fmt"
	"os"

	"github.com/CoreMatch/Avalon/internal/plugin"
)

func main() {
	names := plugin.EventNames()
	if len(names) == 0 {
		fmt.Fprintln(os.Stderr, "plugin-events: event set is empty")
		os.Exit(1)
	}
	for _, name := range names {
		fmt.Println(name)
	}
}
