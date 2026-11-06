package external

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// process 是 helper 子进程的抽象
//
// 抽出来是为了让 Backend 可以在测试里用管道假进程驱动, 不需要真的起一个 JVM.
type process interface {
	// Start 启动进程
	Start() error
	// ReadLine 读一行 (不含换行符); 进程已退出时返回 ErrNotRunning
	ReadLine() (string, error)
	// WriteLine 写一行 (自动补换行符并 flush)
	WriteLine(line string) error
	// CloseStdin 关闭 stdin, 相当于给 helper 发 EOF
	CloseStdin() error
	// Kill 立即终止进程
	Kill()
	// WaitFor 等待进程退出, 超时返回 false
	WaitFor(timeout time.Duration) bool
	// Done 进程退出时关闭
	Done() <-chan struct{}
	// StderrTail 返回已捕获的 stderr 末尾, 用于失败时诊断
	StderrTail() string
}

// newProcess 是进程构造的注入点
//
// 默认用 os/exec 起真实子进程; 测试替换成管道假进程, 这样不需要真的装个 JVM
// 也能把 Backend 的协议交互完整跑一遍.
var newProcess = func(name string, args []string, workDir string, stderrMax int) process {
	return newOSProcess(name, args, workDir, stderrMax)
}

// limitedBuffer 是一个有上限的环形缓冲, 用来接 helper 的 stderr
//
// 不限量的话, 一个刷错误的 helper 能把 Go 进程的内存吃光 —— 外部进程的崩溃
// 不应该变成宿主进程的崩溃.
type limitedBuffer struct {
	mu   sync.Mutex
	buf  []byte
	max  int
	head int // 环形缓冲的写入位置
	full bool
}

func newLimitedBuffer(max int) *limitedBuffer {
	return &limitedBuffer{buf: make([]byte, max), max: max}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, c := range p {
		b.buf[b.head] = c
		b.head = (b.head + 1) % b.max
		if b.head == 0 {
			b.full = true
		}
	}
	return len(p), nil
}

// tail 按时间顺序返回最后 max 字节
func (b *limitedBuffer) tail() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.full {
		return string(b.buf[:b.head])
	}
	out := make([]byte, 0, b.max)
	out = append(out, b.buf[b.head:]...)
	out = append(out, b.buf[:b.head]...)
	return string(out)
}

// osProcess 用 os/exec 驱动一个真实子进程
type osProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *limitedBuffer

	done      chan struct{}
	closeOnce sync.Once
	waitErr   error
}

// newOSProcess 对应"把 java -jar helper.jar 拉起来"这件事
//
// name 与 args 由调用方给全 (默认已解析好的 java 路径与 helper 路径),
// 这里不做任何 PATH 猜测之外的魔法.
func newOSProcess(name string, args []string, workDir string, stderrMax int) *osProcess {
	p := &osProcess{
		cmd:    exec.Command(name, args...),
		stderr: newLimitedBuffer(stderrMax),
		done:   make(chan struct{}),
	}
	if workDir != "" {
		p.cmd.Dir = workDir
	}
	p.cmd.Stderr = p.stderr
	return p
}

// Start 实现 process
func (p *osProcess) Start() error {
	stdin, err := p.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("external: stdin pipe: %w", err)
	}
	stdout, err := p.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("external: stdout pipe: %w", err)
	}

	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("external: start helper: %w", err)
	}

	p.stdin = stdin
	p.stdout = bufio.NewReader(stdout)

	// Wait 必须在一个独立 goroutine 里做, 否则 CloseStdin/Kill 之后没人收尸
	go func() {
		p.waitErr = p.cmd.Wait()
		p.closeOnce.Do(func() { close(p.done) })
	}()

	return nil
}

// ReadLine 实现 process
func (p *osProcess) ReadLine() (string, error) {
	select {
	case <-p.done:
		return "", ErrNotRunning
	default:
	}

	line, err := p.stdout.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) && line != "" {
			// 最后一行没有换行符, 仍然按一条消息处理
			return strings.TrimRight(line, "\r\n"), nil
		}
		// 管道断开基本意味着进程没了
		select {
		case <-p.done:
			return "", ErrNotRunning
		default:
		}
		if errors.Is(err, io.EOF) {
			return "", ErrNotRunning
		}
		return "", fmt.Errorf("external: read from helper: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// WriteLine 实现 process
func (p *osProcess) WriteLine(line string) error {
	select {
	case <-p.done:
		return ErrNotRunning
	default:
	}

	if p.stdin == nil {
		return ErrNotRunning
	}
	if _, err := io.WriteString(p.stdin, line+"\n"); err != nil {
		return fmt.Errorf("external: write to helper: %w", err)
	}
	return nil
}

// CloseStdin 实现 process
func (p *osProcess) CloseStdin() error {
	if p.stdin == nil {
		return nil
	}
	if err := p.stdin.Close(); err != nil {
		return fmt.Errorf("external: close helper stdin: %w", err)
	}
	return nil
}

// Kill 实现 process
func (p *osProcess) Kill() {
	if p.cmd.Process == nil {
		return
	}
	_ = p.cmd.Process.Kill()
}

// Done 实现 process
func (p *osProcess) Done() <-chan struct{} { return p.done }

// StderrTail 实现 process
func (p *osProcess) StderrTail() string { return p.stderr.tail() }

// WaitFor 实现 process: 等待进程退出, 超时返回 false
func (p *osProcess) WaitFor(timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-p.done:
		return true
	case <-timer.C:
		return false
	}
}

// exitError 返回进程的非零退出原因, 便于上层判断"是 helper 自己挂了还是被我们杀的"
func (p *osProcess) exitError() error {
	select {
	case <-p.done:
	default:
		return nil
	}
	if p.waitErr == nil {
		return nil
	}
	var ee *exec.ExitError
	if errors.As(p.waitErr, &ee) {
		return fmt.Errorf("external: helper exited with %s", ee.ProcessState.String())
	}
	return fmt.Errorf("external: helper wait: %w", p.waitErr)
}
