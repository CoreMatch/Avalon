package external

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"rwhps/internal/game"
)

// fakeProcess 是一个用 os.Pipe 搭起来的假 helper
//
// 用 os.Pipe 而不是 io.Pipe: io.Pipe 的 Write 会阻塞到有人读为止, 测试里
// "helper 脚本演完了就不再读"是常态, 会把 Backend 的 WriteLine 一起挂住.
// os.Pipe 有内核缓冲区, 行为更接近真实子进程.
//
// 方向约定 (与真实子进程一致):
//
//	Backend.WriteLine -> backendWrite -> helperRead   (测试侧读)
//	helperWrite       -> backendRead  -> Backend.ReadLine (测试侧写)
type fakeProcess struct {
	backendWrite *os.File
	backendRead  *os.File

	reader *bufio.Reader

	mu       sync.Mutex
	closed   bool
	killed   bool
	done     chan struct{}
	doneOnce sync.Once
	stderr   string
}

func newFakeProcess() (*fakeProcess, *os.File, *os.File) {
	helperRead, backendWrite, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	backendRead, helperWrite, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	fp := &fakeProcess{
		backendWrite: backendWrite,
		backendRead:  backendRead,
		reader:       bufio.NewReader(backendRead),
		done:         make(chan struct{}),
	}
	return fp, helperRead, helperWrite
}

func (f *fakeProcess) Start() error { return nil }

func (f *fakeProcess) ReadLine() (string, error) {
	// reader 必须是进程级的: 每次新建 bufio.Reader 会把上一次读多的数据丢掉
	line, err := f.reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) && line != "" {
			return strings.TrimRight(line, "\r\n"), nil
		}
		f.markDone()
		return "", ErrNotRunning
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (f *fakeProcess) WriteLine(line string) error {
	f.mu.Lock()
	closed := f.closed
	f.mu.Unlock()
	if closed {
		return ErrNotRunning
	}
	if _, err := io.WriteString(f.backendWrite, line+"\n"); err != nil {
		f.markDone()
		return err
	}
	return nil
}

func (f *fakeProcess) CloseStdin() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	return f.backendWrite.Close()
}

func (f *fakeProcess) Kill() {
	f.mu.Lock()
	if f.killed {
		f.mu.Unlock()
		return
	}
	f.killed = true
	f.mu.Unlock()

	// 关掉两端, 让阻塞中的读/写立刻返回
	_ = f.backendRead.Close()
	_ = f.backendWrite.Close()
	f.markDone()
}

func (f *fakeProcess) WaitFor(timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-f.done:
		return true
	case <-timer.C:
		return false
	}
}

func (f *fakeProcess) Done() <-chan struct{} { return f.done }

func (f *fakeProcess) StderrTail() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stderr
}

// markDone 关闭 done 通道
//
// 必须用 Once: 读 goroutine 看到 EOF 和测试主动 Kill 可能同时发生,
// 裸的 select-default 是 check-then-act, 两个 goroutine 都会走到 close.
func (f *fakeProcess) markDone() {
	f.doneOnce.Do(func() { close(f.done) })
}

// step 描述 helper 侧的一次应答
type step struct {
	// wantOp 期望收到的操作名, 空表示不校验
	wantOp string
	// reply 要写回的响应
	reply *Response
	// raw 直接写原始行 (用于构造非法响应)
	raw string
}

// serve 按脚本扮演 helper
//
// 应答的 id 一律按收到的请求对齐 —— 测试不该去手算第几个请求对应第几个 id,
// 那样一改步骤顺序就全错了. 要专门构造 id 不对齐的情况请用 step.raw.
func serve(t *testing.T, in *os.File, out *os.File, steps []step) {
	t.Helper()

	reader := bufio.NewReader(in)
	for _, s := range steps {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Logf("helper: 读请求失败 (可能已被关闭): %v", err)
			return
		}

		var req Request
		if err := jsonUnmarshalLine(line, &req); err != nil {
			t.Errorf("helper: 请求不是合法 JSON: %q", line)
			return
		}
		if s.wantOp != "" && req.Op != s.wantOp {
			t.Errorf("helper: 收到 op %q, 期望 %q", req.Op, s.wantOp)
			return
		}

		if s.raw != "" {
			if _, err := io.WriteString(out, s.raw+"\n"); err != nil {
				return
			}
			continue
		}
		if s.reply != nil {
			reply := *s.reply
			reply.ID = req.ID
			if _, err := io.WriteString(out, mustEncodeReply(&reply)+"\n"); err != nil {
				return
			}
		}
	}
}

// setup 装上假进程并返回 Backend
func setup(t *testing.T, steps ...step) (*Backend, *fakeProcess, *os.File, *os.File) {
	t.Helper()

	fp, helperRead, helperWrite := newFakeProcess()
	t.Cleanup(func() {
		_ = helperRead.Close()
		_ = helperWrite.Close()
	})

	old := newProcess
	newProcess = func(string, []string, string, int) process { return fp }
	t.Cleanup(func() { newProcess = old })

	b := newTestBackend(t, 2*time.Second)
	go serve(t, helperRead, helperWrite, steps)
	return b, fp, helperRead, helperWrite
}

// newTestBackend 构造一个指向假 helper 文件的 Backend
func newTestBackend(t *testing.T, opTimeout time.Duration) *Backend {
	t.Helper()

	// HelperPath 必须指向一个真实存在的文件, 否则 New 的可用性检查会拒绝
	dir := t.TempDir()
	helper := filepath.Join(dir, "helper.jar")
	if err := os.WriteFile(helper, []byte("fake"), 0o644); err != nil {
		t.Fatalf("写假 helper: %v", err)
	}

	b, err := New(Config{
		HelperPath:   helper,
		HelloTimeout: 2 * time.Second,
		OpTimeout:    opTimeout,
		StopGrace:    500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return b
}

// ok 构造一个 OK 响应, id 由 serve 按收到的请求对齐
func ok() *Response {
	return &Response{V: ProtocolVersion, OK: true}
}

func TestNewRequiresHelperPath(t *testing.T) {
	if _, err := New(Config{}); err != ErrHelperNotConfigured {
		t.Fatalf("New(空配置) = %v, 期望 ErrHelperNotConfigured", err)
	}
}

func TestNewRejectsMissingHelper(t *testing.T) {
	_, err := New(Config{HelperPath: filepath.Join(t.TempDir(), "nope.jar")})
	if err == nil {
		t.Fatal("New(不存在的 helper) 应报错")
	}
	if !strings.Contains(err.Error(), "helper entry not found") {
		t.Fatalf("错误 = %v, 期望 ErrHelperNotFound", err)
	}
}

func TestNewRejectsMissingJava(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "helper.jar")
	if err := os.WriteFile(helper, []byte("fake"), 0o644); err != nil {
		t.Fatalf("写假 helper: %v", err)
	}

	_, err := New(Config{
		JavaPath:   filepath.Join(dir, "no-such-java"),
		HelperPath: helper,
	})
	if err == nil {
		t.Fatal("New(java 不存在) 应报错")
	}
	if !strings.Contains(err.Error(), "java executable not found") {
		t.Fatalf("错误 = %v, 期望 ErrJavaNotFound", err)
	}
}

func TestHandshakeThenStart(t *testing.T) {
	// 握手 + start, id 从 0 开始递增
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpStart, reply: ok()},
	)

	if err := b.Start(game.MatchConfig{RoomID: "r1", MapName: "m", MaxPlayer: 10}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !b.Running() {
		t.Fatal("Start 成功后 Running 应为 true")
	}
	if b.PlayerCount() != 0 {
		t.Fatalf("PlayerCount = %d, 期望 0 (helper 未报告人数)", b.PlayerCount())
	}
}

func TestHandshakeVersionMismatch(t *testing.T) {
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, raw: `{"v":99,"id":0,"ok":true}`},
	)

	err := b.Start(game.MatchConfig{})
	if err == nil || !strings.Contains(err.Error(), "protocol 99") {
		t.Fatalf("Start = %v, 期望协议版本不匹配错误", err)
	}
}

func TestHandshakeFailure(t *testing.T) {
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: &Response{V: ProtocolVersion, ID: 0, OK: false, Error: "class not found"}},
	)

	err := b.Start(game.MatchConfig{})
	if err == nil || !strings.Contains(err.Error(), "class not found") {
		t.Fatalf("Start = %v, 期望 helper 的失败原因被透传", err)
	}
}

func TestStartHelperReportsFailure(t *testing.T) {
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpStart, reply: &Response{V: ProtocolVersion, ID: 1, OK: false, Error: "map file missing"}},
	)

	err := b.Start(game.MatchConfig{MapName: "nope"})
	if err == nil || !strings.Contains(err.Error(), "map file missing") {
		t.Fatalf("Start = %v, 期望 helper 的失败原因被透传", err)
	}
	if b.Running() {
		t.Fatal("开局失败后 Running 应保持 false")
	}
}

func TestStopKeepsProcessAlive(t *testing.T) {
	b, fp, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpStart, reply: ok()},
		step{wantOp: OpStop, reply: ok()},
	)

	if err := b.Start(game.MatchConfig{}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := b.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if b.Running() {
		t.Fatal("Stop 后 Running 应为 false")
	}
	// Stop 只发操作, 不杀进程 (反复起 JVM 的代价太高)
	select {
	case <-fp.Done():
		t.Fatal("Stop 不应该终止 helper 进程")
	default:
	}
}

func TestCloseTerminatesProcess(t *testing.T) {
	b, fp, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpStart, reply: ok()},
	)

	if err := b.Start(game.MatchConfig{}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !fp.WaitFor(time.Second) {
		t.Fatal("Close 之后 helper 应当退出")
	}
}

func TestStatusRoundTrip(t *testing.T) {
	// Status 自己会完成握手, 不需要先开局
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpStatus, reply: &Response{V: ProtocolVersion, OK: true, Running: true, Players: 4}},
	)

	running, players, err := b.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !running || players != 4 {
		t.Fatalf("Status = (%v, %d), 期望 (true, 4)", running, players)
	}
	// 查询过之后 PlayerCount 应该跟着刷新
	if b.PlayerCount() != 4 {
		t.Fatalf("PlayerCount = %d, 期望 4", b.PlayerCount())
	}
}

func TestStatusHelperReportsFailure(t *testing.T) {
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpStart, reply: ok()},
		step{wantOp: OpStatus, reply: &Response{V: ProtocolVersion, OK: false, Error: "room is gone"}},
	)

	if err := b.Start(game.MatchConfig{}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, _, err := b.Status(); err == nil || !strings.Contains(err.Error(), "room is gone") {
		t.Fatalf("Status = %v, 期望失败原因被透传", err)
	}
}

func TestReplyIDMismatchIsRejected(t *testing.T) {
	// helper 回了一个对不上的 id (协议错位时最常见的症状)
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, raw: `{"v":1,"id":77,"ok":true}`},
	)

	err := b.Start(game.MatchConfig{})
	if err == nil || !strings.Contains(err.Error(), "hello reply id 77") {
		t.Fatalf("Start = %v, 期望 id 不对齐被拒绝", err)
	}
}

func TestHelperDiesBeforeStart(t *testing.T) {
	// helper 什么都不回就直接消失
	fp, helperRead, helperWrite := newFakeProcess()
	t.Cleanup(func() {
		_ = helperRead.Close()
		_ = helperWrite.Close()
	})

	old := newProcess
	newProcess = func(string, []string, string, int) process { return fp }
	t.Cleanup(func() { newProcess = old })

	b := newTestBackend(t, 2*time.Second)

	// 立刻关掉管道的两端, 模拟"java 找不到类直接退出"
	_ = helperRead.Close()
	_ = helperWrite.Close()

	if err := b.Start(game.MatchConfig{}); err == nil {
		t.Fatal("helper 立即退出时 Start 应报错")
	}
}

func TestHelperRespawnsAfterDeath(t *testing.T) {
	// 第一任 helper 握手后死掉, 第二任接手: 外部依赖挂掉不该让服务器跟着挂掉
	first, read1, write1 := newFakeProcess()
	second, read2, write2 := newFakeProcess()
	t.Cleanup(func() {
		_ = read1.Close()
		_ = write1.Close()
		_ = read2.Close()
		_ = write2.Close()
	})

	old := newProcess
	calls := 0
	newProcess = func(string, []string, string, int) process {
		calls++
		if calls == 1 {
			return first
		}
		return second
	}
	t.Cleanup(func() { newProcess = old })

	b := newTestBackend(t, 2*time.Second)

	go serve(t, read1, write1, []step{
		{wantOp: OpHello, reply: ok()},
		{wantOp: OpStart, reply: ok()},
	})
	if err := b.Start(game.MatchConfig{}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// 第一任消失
	first.Kill()
	deadline := time.Now().Add(2 * time.Second)
	for b.Running() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if b.Running() {
		t.Fatal("helper 退出后 Running 应为 false")
	}

	go serve(t, read2, write2, []step{
		{wantOp: OpHello, reply: ok()},
		{wantOp: OpStart, reply: ok()},
	})
	if err := b.Start(game.MatchConfig{}); err != nil {
		t.Fatalf("重新拉起后 Start: %v", err)
	}
	if calls != 2 {
		t.Fatalf("newProcess 被调用 %d 次, 期望 2 (应当重新拉起)", calls)
	}
}

func TestReadTimeoutDoesNotStealNextReply(t *testing.T) {
	// helper 故意不回答第一次 start, 但必须把第二次的应答留给第二次
	//
	// 这正是"每次 readResponse 起一个 goroutine"会出错的地方: 超时后那个
	// goroutine 还堵在 ReadLine 上, 会把第二次的响应抢走.
	fp, helperRead, helperWrite := newFakeProcess()
	t.Cleanup(func() {
		_ = helperRead.Close()
		_ = helperWrite.Close()
	})

	old := newProcess
	newProcess = func(string, []string, string, int) process { return fp }
	t.Cleanup(func() { newProcess = old })

	b := newTestBackend(t, 300*time.Millisecond)

	go serve(t, helperRead, helperWrite, []step{
		{wantOp: OpHello, reply: ok()},
		// 第一次 start: 只读请求, 不回复 -> 触发读超时
		step{wantOp: OpStart},
		// 超时之后重试, 这一次要能拿到应答
		step{wantOp: OpStart, reply: ok()},
	})

	if err := b.Start(game.MatchConfig{}); err == nil {
		t.Fatal("helper 不应答时 Start 应报超时")
	}

	if err := b.Start(game.MatchConfig{}); err != nil {
		t.Fatalf("第二次 Start: %v", err)
	}
	if !b.Running() {
		t.Fatal("第二次 Start 成功后 Running 应为 true")
	}
}

// --- 测试用的小工具 ---

func jsonUnmarshalLine(line string, v any) error {
	return json.Unmarshal([]byte(strings.TrimRight(line, "\r\n")), v)
}

func mustEncodeReply(resp *Response) string {
	b, err := json.Marshal(resp)
	if err != nil {
		panic(err)
	}
	return string(b)
}
