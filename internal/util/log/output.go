// Package log 对应 net.rwhps.server.util.log 的输出重定向部分.
//
// 原版把 System.out 换成了自己的实现, 交互式输出另走 Data.privateOut / JLine terminal.
// Go 版不需要替换 os.Stdout, 而是把"当前输出目标"收敛成一个可替换的 io.Writer,
// 这样控制台、加载进度、许可提示与日志本身共享同一个出口.
package log

import (
	"bufio"
	"io"
	"os"
	"sync"
)

var (
	outMu sync.RWMutex
	outW  io.Writer = os.Stdout

	inMu     sync.RWMutex
	inR      io.Reader = os.Stdin
	inReader *bufio.Reader
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
	inReader = nil
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

// InputReader 返回与当前输入绑定的缓冲读取器 (进程内单例)
//
// 原版每次 waitLicense 都新建一个 Scanner, 而 Scanner 会预读一整块输入:
// 交互式终端下看不出来 (用户会再敲一行), 但管道输入时 "Yes\nNo\n" 的第二行
// 会被第一个 Scanner 吃掉, 第二个提示只能读到 EOF.
// Go 版让所有读输入的地方共用同一个 bufio.Reader, 管道输入的行为与人工输入一致.
func InputReader() *bufio.Reader {
	inMu.Lock()
	defer inMu.Unlock()
	if inReader == nil {
		inReader = bufio.NewReader(inR)
	}
	return inReader
}
