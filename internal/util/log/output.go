// Package log 对应 net.rwhps.server.util.log 的输出重定向部分.
//
// 原版把 System.out 换成了自己的实现, 交互式输出另走 Data.privateOut / JLine terminal.
// Go 版不需要替换 os.Stdout, 而是把"当前输出目标"收敛成一个可替换的 io.Writer,
// 这样控制台、加载进度、许可提示与日志本身共享同一个出口.
package log

import (
	"io"
	"os"
	"sync"
)

var (
	outMu sync.RWMutex
	outW  io.Writer = os.Stdout

	inMu sync.RWMutex
	inR  io.Reader = os.Stdin
)

// SetOutput 重定向全部输出 (对应原版替换 System.out 与 Data.privateOut 的作用)
//
// 传 nil 恢复 os.Stdout. 该函数应在启动链早期调用, 之后所有日志/进度/提示都会写到新目标.
func SetOutput(w io.Writer) {
	outMu.Lock()
	defer outMu.Unlock()
	if w == nil {
		outW = os.Stdout
		return
	}
	outW = w
}

// Output 返回当前输出目标
func Output() io.Writer {
	outMu.RLock()
	defer outMu.RUnlock()
	return outW
}

// SetInput 重定向交互输入 (对应 System.in), 传 nil 恢复 os.Stdin
func SetInput(r io.Reader) {
	inMu.Lock()
	defer inMu.Unlock()
	if r == nil {
		inR = os.Stdin
		return
	}
	inR = r
}

// Input 返回当前交互输入
func Input() io.Reader {
	inMu.RLock()
	defer inMu.RUnlock()
	return inR
}
