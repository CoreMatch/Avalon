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
	// before 写回之前先执行的动作 (用于插入主动消息)
	before func(req *Request, out *os.File)
}

// serve 按脚本扮演 helper
//
// 应答的 id 与世代号一律按收到的请求对齐 —— 测试不该去手算第几个请求对应第几个 id,
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
		if req.ID <= 0 {
			t.Errorf("helper: 请求 id 非法: %d", req.ID)
			return
		}
		if req.Gen == 0 {
			t.Errorf("helper: 请求缺少世代号")
			return
		}

		if s.before != nil {
			s.before(&req, out)
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
			reply.Gen = req.Gen
			reply.Type = MsgResponse
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
		StopGrace:    50 * time.Millisecond,
		RestartDelay: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// 必须关掉: helper 退出后 Backend 会自动重启, 而 newProcess 是包级变量,
	// 泄漏到下一个测试就会把下一个测试的假进程抢走 (表现为"收到 op hello,
	// 期望 room.status"这类莫名其妙的错位).
	t.Cleanup(func() { _ = b.Close() })
	return b
}

// ok 构造一个 OK 响应, id 与世代号由 serve 按收到的请求对齐
func ok() *Response {
	return &Response{V: ProtocolVersion, Type: MsgResponse, Gen: 1, OK: true}
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

// TestHandshakeThenRoomCreate 握手通过后能建房间
func TestHandshakeThenRoomCreate(t *testing.T) {
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpRoomCreate, reply: ok()},
	)

	if err := b.EnsureRunning(); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	if b.Generation() != 1 {
		t.Fatalf("Generation = %d, 期望 1", b.Generation())
	}

	resp, err := b.Request(&Request{Op: OpRoomCreate, Room: "r1"})
	if err != nil {
		t.Fatalf("room.create: %v", err)
	}
	if !resp.OK {
		t.Fatalf("room.create 应答 = %+v, 期望 OK", resp)
	}
}

// TestHandshakeVersionMismatch helper 说的是别的协议版本
func TestHandshakeVersionMismatch(t *testing.T) {
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, raw: `{"v":99,"type":"response","id":1,"gen":1,"ok":true}`},
	)

	err := b.EnsureRunning()
	if err == nil || !strings.Contains(err.Error(), "protocol 99") {
		t.Fatalf("EnsureRunning = %v, 期望协议版本不匹配错误", err)
	}
}

// TestHandshakeFailure helper 明确拒绝握手
func TestHandshakeFailure(t *testing.T) {
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: &Response{V: ProtocolVersion, OK: false, Error: "class not found"}},
	)

	err := b.EnsureRunning()
	if err == nil || !strings.Contains(err.Error(), "class not found") {
		t.Fatalf("EnsureRunning = %v, 期望 helper 的失败原因被透传", err)
	}
}

// TestRoomStartHelperReportsFailure 开局失败时原因要透传
func TestRoomStartHelperReportsFailure(t *testing.T) {
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpRoomStart, reply: &Response{V: ProtocolVersion, OK: false, Error: "map file missing"}},
	)

	_, err := b.Request(&Request{Op: OpRoomStart, Room: "r1", Map: "nope"})
	if err == nil || !strings.Contains(err.Error(), "map file missing") {
		t.Fatalf("room.start = %v, 期望 helper 的失败原因被透传", err)
	}
}

// TestRoomStatusRoundTrip 状态查询的字段要能回到 Go 侧
func TestRoomStatusRoundTrip(t *testing.T) {
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpRoomStatus, reply: &Response{V: ProtocolVersion, OK: true, Running: true, Players: 4}},
	)

	resp, err := b.Request(&Request{Op: OpRoomStatus, Room: "r1"})
	if err != nil {
		t.Fatalf("room.status: %v", err)
	}
	if !resp.Running || resp.Players != 4 {
		t.Fatalf("room.status = %+v, 期望 running 且 players=4", resp)
	}
}

// TestUnmatchedResponseIDIsIgnored 未匹配的应答 id 只记日志, 不是致命错误
func TestUnmatchedResponseIDIsIgnored(t *testing.T) {
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		// 先吐一个对不上的 id, 再吐真正的应答
		step{wantOp: OpRoomStatus, before: func(req *Request, out *os.File) {
			writeRaw(t, out, `{"v":2,"type":"response","id":9999,"gen":1,"ok":true}`)
		}, reply: &Response{V: ProtocolVersion, OK: true, Players: 2}},
	)

	if err := b.EnsureRunning(); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}

	resp, err := b.Request(&Request{Op: OpRoomStatus, Room: "r1"})
	if err != nil {
		t.Fatalf("room.status: %v", err)
	}
	if resp.Players != 2 {
		t.Fatalf("players = %d, 期望 2", resp.Players)
	}
}

// TestUnknownEventIsIgnored 未知事件名只记日志
func TestUnknownEventIsIgnored(t *testing.T) {
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpRoomStatus, before: func(req *Request, out *os.File) {
			writeRaw(t, out, `{"v":2,"type":"event","gen":1,"event":"what.is.this","room":"r1"}`)
		}, reply: &Response{V: ProtocolVersion, OK: true, Players: 1}},
	)

	var mu sync.Mutex
	var seen []string
	b.SetHandlers(Handlers{
		Event: func(ev *EventMessage) {
			mu.Lock()
			seen = append(seen, ev.Event)
			mu.Unlock()
		},
	})

	if err := b.EnsureRunning(); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	if _, err := b.Request(&Request{Op: OpRoomStatus, Room: "r1"}); err != nil {
		t.Fatalf("room.status: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 0 {
		t.Fatalf("未知事件不应被分发, 收到 %v", seen)
	}
}

// TestPacketDeliveredToHandler helper 主动吐的游戏包要能到 Go 侧
func TestPacketDeliveredToHandler(t *testing.T) {
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpRoomStatus, before: func(req *Request, out *os.File) {
			writeRaw(t, out, `{"v":2,"type":"packet","gen":1,"room":"r1","conn":"c1","ptype":115,"data":"AQID"}`)
		}, reply: &Response{V: ProtocolVersion, OK: true}},
	)

	packets := make(chan *PacketMessage, 4)
	b.SetHandlers(Handlers{
		Packet: func(p *PacketMessage) {
			packets <- p
		},
	})

	if err := b.EnsureRunning(); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	if _, err := b.Request(&Request{Op: OpRoomStatus, Room: "r1"}); err != nil {
		t.Fatalf("room.status: %v", err)
	}

	select {
	case p := <-packets:
		if p.Room != "r1" || p.Conn != "c1" || p.PType != 115 {
			t.Fatalf("包 = %+v, 期望 room=r1 conn=c1 ptype=115", p)
		}
		data, err := decodeBase64(p.Data)
		if err != nil {
			t.Fatalf("base64 解码: %v", err)
		}
		if len(data) != 3 || data[0] != 1 || data[2] != 3 {
			t.Fatalf("负载 = %v, 期望 [1 2 3]", data)
		}
	case <-time.After(time.Second):
		t.Fatal("没有收到游戏包")
	}
}

// TestConnPacketIsFireAndForget conn.packet 不等应答, 但写失败要报出来
func TestConnPacketIsFireAndForget(t *testing.T) {
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		// conn.packet 之后 helper 不该再回任何东西
	)

	if err := b.EnsureRunning(); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	if err := b.Send(&Request{Op: OpConnPacket, Room: "r1", Conn: "c1", PType: 115, Data: "AQID"}); err != nil {
		t.Fatalf("conn.packet: %v", err)
	}

	// 写失败必须立刻报错
	b.Close()
	if err := b.Send(&Request{Op: OpConnPacket, Room: "r1", Conn: "c1", PType: 115}); err == nil {
		t.Fatal("backend 已关闭时 conn.packet 应报错")
	}
}

// TestCloseTerminatesProcess 关闭 backend 要把 helper 进程带走
func TestCloseTerminatesProcess(t *testing.T) {
	b, fp, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
	)

	if err := b.EnsureRunning(); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !fp.WaitFor(time.Second) {
		t.Fatal("Close 之后 helper 应当退出")
	}
}

// TestHelperDiesBeforeHandshake helper 什么都不回就直接消失
func TestHelperDiesBeforeHandshake(t *testing.T) {
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

	if err := b.EnsureRunning(); err == nil {
		t.Fatal("helper 立即退出时 EnsureRunning 应报错")
	}
}

// TestHelperRespawnsAfterDeath helper 崩溃后要自动补一个, 并且世代号递增
func TestHelperRespawnsAfterDeath(t *testing.T) {
	first, read1, write1 := newFakeProcess()
	second, read2, write2 := newFakeProcess()
	t.Cleanup(func() {
		_ = read1.Close()
		_ = write1.Close()
		_ = read2.Close()
		_ = write2.Close()
	})

	old := newProcess
	var calls int
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
	})
	if err := b.EnsureRunning(); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	if b.Generation() != 1 {
		t.Fatalf("第一代 Generation = %d, 期望 1", b.Generation())
	}

	// 第一任消失
	first.Kill()

	// 等到第二代拉起来
	deadline := time.Now().Add(3 * time.Second)
	for b.Generation() == 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if b.Generation() != 2 {
		t.Fatalf("重启后 Generation = %d, 期望 2", b.Generation())
	}
	if calls != 2 {
		t.Fatalf("newProcess 被调用 %d 次, 期望 2 (应当重新拉起)", calls)
	}
}

// TestHelperExitCallback 上一代 helper 退出要通知上层
func TestHelperExitCallback(t *testing.T) {
	fp, helperRead, helperWrite := newFakeProcess()
	t.Cleanup(func() {
		_ = helperRead.Close()
		_ = helperWrite.Close()
	})

	old := newProcess
	newProcess = func(string, []string, string, int) process { return fp }
	t.Cleanup(func() { newProcess = old })

	b := newTestBackend(t, 2*time.Second)

	exits := make(chan uint64, 4)
	b.SetHandlers(Handlers{
		HelperExit: func(gen uint64) {
			exits <- gen
		},
	})

	go serve(t, helperRead, helperWrite, []step{
		{wantOp: OpHello, reply: ok()},
	})
	if err := b.EnsureRunning(); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}

	fp.Kill()
	select {
	case gen := <-exits:
		if gen != 1 {
			t.Fatalf("退出的世代号 = %d, 期望 1", gen)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("helper 退出没有通知上层")
	}
}

// TestReadTimeoutDoesNotStealNextReply helper 不回答第一次请求时,
// 超时之后重试要能拿到第二次的应答 —— 单读者模型的价值就在这里
func TestReadTimeoutDoesNotStealNextReply(t *testing.T) {
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
		// 第一次 room.status: 只读请求, 不回复 -> 触发读超时
		step{wantOp: OpRoomStatus},
		// 超时之后重试, 这一次要能拿到应答
		step{wantOp: OpRoomStatus, reply: ok()},
	})

	if err := b.EnsureRunning(); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	if _, err := b.Request(&Request{Op: OpRoomStatus, Room: "r1"}); err == nil {
		t.Fatal("helper 不应答时 room.status 应报超时")
	}
	if _, err := b.Request(&Request{Op: OpRoomStatus, Room: "r1"}); err != nil {
		t.Fatalf("第二次 room.status: %v", err)
	}
}

// TestConcurrentRequestsKeepTheirOwnReply 并发请求必须各回各的
func TestConcurrentRequestsKeepTheirOwnReply(t *testing.T) {
	fp, helperRead, helperWrite := newFakeProcess()
	t.Cleanup(func() {
		_ = helperRead.Close()
		_ = helperWrite.Close()
	})

	old := newProcess
	newProcess = func(string, []string, string, int) process { return fp }
	t.Cleanup(func() { newProcess = old })

	b := newTestBackend(t, 3*time.Second)

	go serve(t, helperRead, helperWrite, []step{
		{wantOp: OpHello, reply: ok()},
		step{wantOp: OpRoomStatus, reply: &Response{V: ProtocolVersion, OK: true, Players: 1}},
		step{wantOp: OpRoomStatus, reply: &Response{V: ProtocolVersion, OK: true, Players: 2}},
		step{wantOp: OpRoomStatus, reply: &Response{V: ProtocolVersion, OK: true, Players: 3}},
	})

	if err := b.EnsureRunning(); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := b.Request(&Request{Op: OpRoomStatus, Room: "r1"})
			if err != nil {
				errs <- err
				return
			}
			if resp.Players < 1 || resp.Players > 3 {
				errs <- errors.New("拿到的 players 不在期望范围")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("并发请求失败: %v", err)
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

func writeRaw(t *testing.T, out *os.File, line string) {
	t.Helper()
	if _, err := io.WriteString(out, line+"\n"); err != nil {
		t.Logf("写原始行失败: %v", err)
	}
}
