// Package console 对应 net.rwhps.server.util.console (JLine 的最小替代)
//
// 原版用 JLine3 的 LineReader: 带 Tab 补全、行内编辑, 并且能 printAbove 把日志
// 打印到当前提示符上方而不打断输入. Go 版只保留交互所需的最小能力:
// 带提示符读一行, 以及把文本写到输出.
//
// 明确不做的事:
//   - Tab 补全 (TabCompleter.kt): 需要终端 raw mode 与行编辑器, 标准库没有
//   - printAbove: 没有行编辑器就没有"提示符上方"的概念, 直接换行输出
package console

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sync"
)

// ErrClosed 输入已结束 (对应 EndOfFileException)
var ErrClosed = errors.New("console: input closed")

// Console 对应 JLine 的 LineReader
type Console struct {
	mu   sync.Mutex
	in   *bufio.Reader
	out  io.Writer
	dead bool
}

// New 创建控制台; in 为 nil 时 ReadLine 立即返回 ErrClosed
//
// 传入 *bufio.Reader 时直接复用, 不再套一层缓冲 ——
// 否则会与其它读同一输入源的地方 (如 log.WaitLicense) 各缓冲一块, 互相吞掉数据.
func New(in io.Reader, out io.Writer) *Console {
	c := &Console{out: out}
	switch r := in.(type) {
	case nil:
		c.dead = true
	case *bufio.Reader:
		c.in = r
	default:
		c.in = bufio.NewReader(r)
	}
	return c
}

// ReadLine 打印提示符并读取一行 (不含行尾换行)
//
// 输入结束返回 ErrClosed, 对应原版的 EndOfFileException.
func (c *Console) ReadLine(prompt string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.dead || c.in == nil {
		return "", ErrClosed
	}

	if prompt != "" && c.out != nil {
		fmt.Fprint(c.out, prompt)
	}

	line, err := c.in.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			if line == "" {
				return "", ErrClosed
			}
			// 最后一行没有换行符, 仍然算一条有效输入
			return trimEOL(line), nil
		}
		return "", err
	}
	return trimEOL(line), nil
}

// PrintAbove 对应 LineReader.printAbove: 输出一行
func (c *Console) PrintAbove(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.out != nil {
		fmt.Fprintln(c.out, text)
	}
}

// Print 对应 LineReader.print: 输出但不换行
func (c *Console) Print(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.out != nil {
		fmt.Fprint(c.out, text)
	}
}

// Close 标记输入结束, 之后的 ReadLine 一律返回 ErrClosed
func (c *Console) Close() {
	c.mu.Lock()
	c.dead = true
	c.mu.Unlock()
}

func trimEOL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
