package external

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"rwhps/internal/util/log"
)

// Config 外部 helper 的启动参数
//
// 所有字段都可以为空/零值, 此时用 Default* 常量兜底; 只有 HelperPath 是必填的.
type Config struct {
	// JavaPath java 可执行文件; 空表示按 PATH 里的 "java" 处理
	JavaPath string
	// HelperPath helper 的入口 (jar 路径或主类名), 必填
	HelperPath string
	// WorkDir 子进程工作目录; 空表示继承当前进程
	WorkDir string
	// ExtraArgs 追加给 java 的参数 (例如 -Xmx512m)
	ExtraArgs []string

	// StderrMax helper stderr 的保留上限, 默认 8KB
	StderrMax int
	// HelloTimeout 握手超时, 默认 15s (JVM 冷启动可能要几秒)
	HelloTimeout time.Duration
	// OpTimeout 单次操作超时, 默认 60s (开局要载入地图)
	OpTimeout time.Duration
	// StatusTimeout 状态查询 (room.status) 超时, 默认 5s
	//
	// 与 OpTimeout 分开: 状态查询挂在控制台命令这类交互路径上,
	// 不能跟着开局一起等 60s.
	StatusTimeout time.Duration
	// StopGrace Stop 之后等 helper 自行退出的时间, 默认 10s
	StopGrace time.Duration
	// RestartDelay helper 崩溃后重新拉起前的等待时间, 默认 2s
	RestartDelay time.Duration
}

// 默认值
const (
	DefaultStderrMax     = 8 * 1024
	DefaultHelloTimeout  = 15 * time.Second
	DefaultOpTimeout     = 60 * time.Second
	DefaultStatusTimeout = 5 * time.Second
	DefaultStopGrace     = 10 * time.Second
	DefaultRestartDelay  = 2 * time.Second
)

// 启动/配置层错误
var (
	// ErrHelperNotConfigured HelperPath 为空
	ErrHelperNotConfigured = errors.New("external: helper path is not configured")
	// ErrJavaNotFound java 可执行文件找不到
	ErrJavaNotFound = errors.New("external: java executable not found")
	// ErrHelperNotFound helper 入口文件不存在
	ErrHelperNotFound = errors.New("external: helper entry not found")
)

// Handlers 汇总 helper 主动消息与生命周期事件的处理入口
//
// 全部可选; 未设置的处理项会被安静地忽略 (只留一条日志).
//
// 约定: 这些回调在 Backend 的读 goroutine 里被调用, 因此实现方不能长时间阻塞,
// 更不能反过来调用 Backend 的同步请求 —— 那会与读 goroutine 抢同一个管道,
// 表现为"helper 不回话". 需要回话的场景请自己再开一个 goroutine.
type Handlers struct {
	// Packet 游戏包: helper 要把某个包发给某条客户端连接
	Packet func(*PacketMessage)
	// Event 玩家档案 / 聊天 / 命令 / 房间状态变化
	Event func(*EventMessage)
	// Fatal helper 自报的致命错误
	Fatal func(*FatalMessage)
	// HelperExit helper 进程退出; gen 是退出的那一代
	//
	// 这是"断开全部客户端"的触发点: 旧一代的 helper 承载着所有客户端连接,
	// 它一死, 这些连接全部作废, 必须由上层主动断开而不是留着假连接.
	HelperExit func(gen uint64)
	// HelperReady 新的 helper 已经握手完成; gen 是新的一代
	//
	// 上层在这里重建房间配置 (但不要自动开局).
	HelperReady func(gen uint64)
}

// session 是一代 helper 进程的运行时状态
type session struct {
	// proc 子进程
	proc process
	// gen 这一代的世代号
	gen uint64

	// protoErr 这一代遇到的第一条协议错误
	//
	// 协议错位 (版本不符 / 未知消息类型) 之后, 这一代 helper 吐出来的每一行
	// 都不能信了. 但正在等待应答的调用方需要知道"到底是哪错了", 而不是只看到
	// 一个含糊的"进程没了", 所以把第一条错误记在这里.
	protoErr error
}

// Backend 持有 helper 子进程, 并把线协议封装成"请求-应答"与"主动消息"两类接口.
//
// 它同时实现 game.Simulation (见 simulation.go): 以"一个 Backend 对应一个房间"
// 的方式驱动 room.* 操作. 将来若需要一 helper 多房间的路由接口, 会在那一层
// 另行定义, 不影响 Simulation 这条单房间通路.
//
// 并发模型:
//
//	读:  只有 pump 一个 goroutine 读 helper 的 stdout, 它把每一行解码后分发
//	写:  所有写操作经 writeMu 串行化, 保证一行一条消息
//	状态: mu 保护 session / 世代号 / 待答表 / 回调 / 房间状态
//
// 请求与应答按 ID 配对, 不依赖"下一条就是上一个请求的应答"——
// helper 在两次请求之间完全可能先吐一个游戏包或一条事件.
type Backend struct {
	cfg Config

	// mu 保护 session / gen / nextID / pending / handlers / room / running
	mu sync.Mutex
	// session 当前这一代 helper; 没有 helper 时为 nil
	session *session
	// gen 世代号, 从 1 开始; 0 表示尚未建立过 helper
	gen uint64
	// nextID 请求序号, 从 1 开始
	nextID int64
	// pending 等待应答的请求, 按请求 ID 索引
	pending map[int64]chan *Response
	// handlers 主动消息与生命周期回调
	handlers Handlers
	// room 当前这一代 helper 上已创建的房间 ID; 空表示尚未创建
	//
	// helper 进程一死就作废 (见 handleExit), 新一代上必须重新 room.create.
	room string
	// running room 是否处于对局中 (最近一次 room.start / room.stop 的本地记录),
	// 仅在无法向存活的 helper 求证时作为回落
	running bool

	// writeMu 串行化对 helper 的写入
	writeMu sync.Mutex
	// startMu 串行化"拉起 helper"这件事 (惰性启动与崩溃重启会并发)
	startMu sync.Mutex

	// done 在 Close 时关闭, 用于终止重启循环
	done     chan struct{}
	doneOnce sync.Once

	// closed 标记 Backend 已经关闭
	closed bool
}

// New 构造外部后端
//
// 只做参数校验, 不启动进程 —— 启动是惰性的 (第一次真正用到 helper 时才拉起 JVM),
// 这样"配置了 helper 但 helper 不在"不会影响服务器启动.
func New(cfg Config) (*Backend, error) {
	if strings.TrimSpace(cfg.HelperPath) == "" {
		return nil, ErrHelperNotConfigured
	}
	if cfg.StderrMax <= 0 {
		cfg.StderrMax = DefaultStderrMax
	}
	if cfg.HelloTimeout <= 0 {
		cfg.HelloTimeout = DefaultHelloTimeout
	}
	if cfg.OpTimeout <= 0 {
		cfg.OpTimeout = DefaultOpTimeout
	}
	if cfg.StatusTimeout <= 0 {
		cfg.StatusTimeout = DefaultStatusTimeout
	}
	if cfg.StopGrace <= 0 {
		cfg.StopGrace = DefaultStopGrace
	}
	if cfg.RestartDelay <= 0 {
		cfg.RestartDelay = DefaultRestartDelay
	}

	b := &Backend{
		cfg:     cfg,
		pending: make(map[int64]chan *Response),
		done:    make(chan struct{}),
	}
	if err := b.CheckAvailable(); err != nil {
		return nil, err
	}
	return b, nil
}

// SetHandlers 设置主动消息与生命周期回调
func (b *Backend) SetHandlers(h Handlers) {
	b.mu.Lock()
	b.handlers = h
	b.mu.Unlock()
}

// CheckAvailable 检查 java 与 helper 是否就位
//
// 单独抽出来是因为启动链需要在不启动进程的前提下知道"外部依赖可不可用",
// 从而决定是把 Backend 装上还是继续用无后端状态并给出明确提示.
func (b *Backend) CheckAvailable() error {
	java := b.cfg.JavaPath
	if java == "" {
		java = "java"
	}
	// 带路径分隔符时按文件检查; 否则认为是 PATH 查找, 交给 exec 自己解析
	if strings.ContainsAny(java, `/\`) {
		if !fileExists(java) {
			return fmt.Errorf("%w: %s", ErrJavaNotFound, java)
		}
	}

	helper := b.cfg.HelperPath
	// 形如 com.example.Helper 的类名不检查文件存在性
	if strings.ContainsAny(helper, `/\`) && !fileExists(helper) {
		return fmt.Errorf("%w: %s", ErrHelperNotFound, helper)
	}
	return nil
}

// javaArgs 拼出 java 命令行
//
// 只有这一处知道"helper 是怎么被拉起来的", 换启动方式 (例如换成 native-image
// 可执行文件或另一个语言的 helper) 只需要改这里.
func (b *Backend) javaArgs() []string {
	args := make([]string, 0, len(b.cfg.ExtraArgs)+4)
	args = append(args, b.cfg.ExtraArgs...)
	if isJar(b.cfg.HelperPath) {
		args = append(args, "-jar", b.cfg.HelperPath)
	} else {
		// 当成主类名
		args = append(args, "-cp", filepath.Dir(b.cfg.HelperPath), filepath.Base(b.cfg.HelperPath))
	}
	return args
}

func isJar(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".jar")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// Generation 返回当前 helper 的世代号
//
// 0 表示还没有建立过 helper. 世代号每换一个 helper 进程就加一,
// 上层可以据此判断"我手上这份房间状态还作不作数".
func (b *Backend) Generation() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.gen
}

// Ready 报告是否有一个活着的 helper 进程 (非阻塞, 不拉起进程).
//
// 供 /v1/health 的 ready 判据使用 (DECISIONS D-21 修订): 配置了 helper
// 时, 就绪必须等 helper 活着. 健康检查不得触发 JVM 启动, 所以这里
// 只看现状, 不 EnsureRunning.
func (b *Backend) Ready() bool {
	b.mu.Lock()
	s := b.session
	closed := b.closed
	b.mu.Unlock()
	if closed || s == nil {
		return false
	}
	select {
	case <-s.proc.Done():
		return false
	default:
		return true
	}
}

// EnsureRunning 保证有一个活着的 helper, 没有就拉起
//
// 惰性: 第一次真正用到 helper 时才启动 JVM. helper 崩溃后也会自动补一个,
// 补上来的那一代会拿到新的世代号.
func (b *Backend) EnsureRunning() error {
	b.startMu.Lock()
	ready, err := b.ensureRunningLocked()
	b.startMu.Unlock()
	if err != nil {
		return err
	}
	// HelperReady 在 startMu 之外调用: 上层要在回调里重建房间配置, 而重建
	// 本身可能又要发请求 (room.create). 拿着 startMu 回调就是自己等自己.
	if ready > 0 {
		b.fireReady(ready)
	}
	return nil
}

// fireReady 通知上层"新的一代 helper 已经就位"
func (b *Backend) fireReady(gen uint64) {
	if h := b.handlerSnapshot(); h.HelperReady != nil {
		h.HelperReady(gen)
	}
}

// ensureRunningLocked 是 EnsureRunning 的内部版本 (调用方须持有 startMu)
//
// 返回值是新拉起的那一代世代号; 0 表示"本来就有一个活着的, 什么都没做".
func (b *Backend) ensureRunningLocked() (uint64, error) {
	b.mu.Lock()
	s := b.session
	closed := b.closed
	b.mu.Unlock()

	if closed {
		return 0, ErrNotRunning
	}
	if s != nil {
		select {
		case <-s.proc.Done():
			// 进程已经死了; pump 稍后会收拾残局, 这里直接当作"没有"
		default:
			return 0, nil
		}
	}
	return b.startLocked()
}

// startLocked 拉起一个新的 helper 进程并完成握手 (调用方须持有 startMu)
//
// 返回值是这一代的世代号.
func (b *Backend) startLocked() (uint64, error) {
	java := b.cfg.JavaPath
	if java == "" {
		java = "java"
	}
	proc := newProcess(java, b.javaArgs(), b.cfg.WorkDir, b.cfg.StderrMax)
	if err := proc.Start(); err != nil {
		return 0, err
	}

	b.mu.Lock()
	b.gen++
	s := &session{proc: proc, gen: b.gen}
	b.session = s
	b.mu.Unlock()

	go b.pump(s)

	// 握手: 让"java 不存在 / jar 不存在 / 类路径错"在第一秒就暴露出来,
	// 而不是等到第一次开局才以一个奇怪的 EOF 出现.
	resp, err := b.doRequest(s, &Request{Op: OpHello}, b.cfg.HelloTimeout)
	if err != nil {
		b.terminate(s)
		return 0, err
	}
	if !resp.OK {
		b.terminate(s)
		return 0, errorf(resp)
	}

	return s.gen, nil
}

// Request 发起一次"请求-应答"操作
//
// req 里的 V / Gen / ID 由本方法填写, 调用方只需要给 Op 和业务字段.
// 返回的 Response 里 OK 为 false 时 error 非 nil.
func (b *Backend) Request(req *Request) (*Response, error) {
	if err := b.EnsureRunning(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	s := b.session
	b.mu.Unlock()
	if s == nil {
		return nil, ErrNotRunning
	}
	return b.doRequest(s, req, b.cfg.OpTimeout)
}

// Send 发起一次"只写不等"的请求
//
// 用于 conn.packet 这类 fire-and-forget 的转发: helper 不需要回答,
// 写失败要立刻报告给调用方 (否则客户端的包会静默丢失).
func (b *Backend) Send(req *Request) error {
	if err := b.EnsureRunning(); err != nil {
		return err
	}
	b.mu.Lock()
	s := b.session
	if s == nil {
		b.mu.Unlock()
		return ErrNotRunning
	}
	b.nextID++
	req.V = ProtocolVersion
	req.Gen = s.gen
	req.ID = b.nextID
	b.mu.Unlock()

	line, err := encodeRequest(req)
	if err != nil {
		return err
	}
	if err := b.writeLine(s, line); err != nil {
		return b.wrapProcError(s, err)
	}
	return nil
}

// doRequest 发出请求并等待对应的应答
//
// 应答按请求 ID 配对, 不假设"下一条消息就是这个请求的应答":
// helper 完全可能在两次请求之间先吐一个游戏包或一条事件.
func (b *Backend) doRequest(s *session, req *Request, timeout time.Duration) (*Response, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, ErrNotRunning
	}
	req.V = ProtocolVersion
	req.Gen = s.gen
	b.nextID++
	req.ID = b.nextID
	ch := make(chan *Response, 1)
	b.pending[req.ID] = ch
	b.mu.Unlock()

	defer b.removePending(req.ID)

	line, err := encodeRequest(req)
	if err != nil {
		return nil, err
	}
	if err := b.writeLine(s, line); err != nil {
		return nil, b.wrapProcError(s, err)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case resp, ok := <-ch:
		if !ok || resp == nil {
			// 通道关闭 = helper 走了. 如果它是因为协议错位被杀的, 把真正的原因带出来.
			if pe := b.protoError(s); pe != nil {
				return nil, pe
			}
			return nil, ErrNotRunning
		}
		if !resp.OK {
			return resp, errorf(resp)
		}
		return resp, nil
	case <-timer.C:
		// 超时不是协议错误: 可能只是 helper 慢 (开局要载入地图).
		// 应答如果随后才到, 会因为找不到待答表而被丢弃并记一条日志.
		return nil, fmt.Errorf("external: helper did not reply within %s", timeout)
	case <-s.proc.Done():
		if pe := b.protoError(s); pe != nil {
			return nil, pe
		}
		return nil, ErrNotRunning
	}
}

// writeLine 串行化地写一行给 helper
func (b *Backend) writeLine(s *session, line string) error {
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	return s.proc.WriteLine(line)
}

// removePending 丢掉一个待答项
func (b *Backend) removePending(id int64) {
	b.mu.Lock()
	delete(b.pending, id)
	b.mu.Unlock()
}

// pump 是进程存续期间唯一读 helper 输出的 goroutine
//
// 之所以要独占: 如果每个请求都自己起一个 goroutine 去 ReadLine,
// 那么一次超时会留下一个没人管的读者, 它会抢走下一次请求的应答,
// 表现为"reply id 对不上"这种很难查的错. 由单一读者把行解码后分发,
// 超时只是放弃等待, 应答不会丢.
func (b *Backend) pump(s *session) {
	for {
		line, err := s.proc.ReadLine()
		if err != nil {
			b.handleExit(s, err)
			return
		}
		b.dispatch(s, line)
	}
}

// dispatch 解码并分发一行 helper 输出
func (b *Backend) dispatch(s *session, line string) {
	msg, err := decodeMessage(line)
	if err != nil {
		// 协议层错误 (含"未知消息类型")属于版本错位, 只能当致命处理:
		// 继续读下去只会把错位扩散到每一行.
		log.Errorf("[GameHelper]", "protocol error from helper: {0}", err)
		b.noteProtoErr(s, err)
		if h := b.handlerSnapshot(); h.Fatal != nil {
			h.Fatal(&FatalMessage{V: ProtocolVersion, Gen: s.gen, Error: err.Error()})
		}
		// 这一代 helper 已经不可信, 杀掉它让重启流程换一个上来.
		// 正在等应答的调用方会从 pending 通道关闭里拿到 protoErr, 而不是干等超时.
		//
		// 这里直接 Kill 而不是走 terminate: terminate 要先等 StopGrace 让 helper
		// 自己退出, 而协议错位的 helper 不会自己退出, 白等一轮.
		s.proc.Kill()
		return
	}

	switch m := msg.(type) {
	case *Response:
		b.deliverResponse(s, m)
	case *PacketMessage:
		b.deliverPacket(s, m)
	case *EventMessage:
		b.deliverEvent(s, m)
	case *FatalMessage:
		b.deliverFatal(s, m)
	}
}

// noteProtoErr 记录这一代遇到的第一条协议错误
func (b *Backend) noteProtoErr(s *session, err error) {
	b.mu.Lock()
	if s.protoErr == nil {
		s.protoErr = err
	}
	b.mu.Unlock()
}

// protoError 取这一代记录的第一条协议错误
func (b *Backend) protoError(s *session) error {
	if s == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return s.protoErr
}

// deliverResponse 把应答交给等它的请求
func (b *Backend) deliverResponse(s *session, resp *Response) {
	if resp.Gen != s.gen {
		// 上一代 helper 迟到的应答, 丢弃
		log.Warnf("[GameHelper]", "drop response of stale generation {0} (current {1})", resp.Gen, s.gen)
		return
	}

	b.mu.Lock()
	ch, ok := b.pending[resp.ID]
	if ok {
		delete(b.pending, resp.ID)
	}
	b.mu.Unlock()

	if !ok {
		// 未匹配的应答: 要么是上一次超时后迟到的, 要么 helper 发错了 id.
		// 都不是致命协议错误, 记一条日志继续.
		log.Warnf("[GameHelper]", "drop unmatched response id {0}", resp.ID)
		return
	}
	ch <- resp
}

// deliverPacket 分发一个游戏包
func (b *Backend) deliverPacket(s *session, pkt *PacketMessage) {
	if pkt.Gen != s.gen {
		log.Warnf("[GameHelper]", "drop packet of stale generation {0} (current {1})", pkt.Gen, s.gen)
		return
	}
	if h := b.handlerSnapshot(); h.Packet != nil {
		h.Packet(pkt)
	}
}

// deliverEvent 分发一个事件
func (b *Backend) deliverEvent(s *session, ev *EventMessage) {
	if ev.Gen != s.gen {
		log.Warnf("[GameHelper]", "drop event of stale generation {0} (current {1})", ev.Gen, s.gen)
		return
	}
	if !knownEvent(ev.Event) {
		// 未知事件名只记日志: 两侧版本不一致时, 新事件不该让老 helper 直接瘫掉
		log.Warnf("[GameHelper]", "ignore unknown event {0}", ev.Event)
		return
	}
	if h := b.handlerSnapshot(); h.Event != nil {
		h.Event(ev)
	}
}

// deliverFatal 分发 helper 自报的致命错误
func (b *Backend) deliverFatal(s *session, fatal *FatalMessage) {
	if fatal.Gen != s.gen {
		// 上一代 helper 迟到的致命消息: 那一代的房间早就作废了,
		// 报出来只会让上层以为当前这一代也出事了.
		log.Warnf("[GameHelper]", "drop fatal of stale generation {0} (current {1})", fatal.Gen, s.gen)
		return
	}
	log.Errorf("[GameHelper]", "helper reported fatal error: {0}", fatal.Error)
	if h := b.handlerSnapshot(); h.Fatal != nil {
		h.Fatal(fatal)
	}
}

// knownEvent 判断事件名是否是协议里定义过的
func knownEvent(name string) bool {
	switch name {
	case EventPlayerJoin, EventPlayerLeave, EventPlayerUpdate,
		EventChat, EventCommand, EventRoomStarted, EventRoomStopped:
		return true
	}
	return false
}

// handleExit 处理 helper 进程退出
//
// 顺序很重要:
//  1. 先让所有等待中的请求失败 —— 否则它们会一直等到超时.
//  2. 再通知上层 (上层要在这里断开全部客户端).
//  3. 最后安排重启.
func (b *Backend) handleExit(s *session, cause error) {
	b.mu.Lock()
	if b.session != s {
		// 已经被新的一代替代了, 这一代退出不用管
		b.mu.Unlock()
		return
	}
	b.session = nil
	pending := b.pending
	b.pending = make(map[int64]chan *Response)
	// 这一代承载的房间随进程一起作废: 新一代 helper 上没有这个房间,
	// 留着旧房间号只会让 Stop/Status 去查一个不存在的对象.
	b.room = ""
	b.running = false
	b.mu.Unlock()

	for _, ch := range pending {
		close(ch)
	}

	log.Errorf("[GameHelper]", "helper generation {0} exited: {1}", s.gen, cause)

	if h := b.handlerSnapshot(); h.HelperExit != nil {
		h.HelperExit(s.gen)
	}

	b.scheduleRestart()
}

// scheduleRestart 安排一次重启
//
// 重启不是同步的: 崩溃往往发生在有人正等着一个应答的时候,
// 同步重启会把调用方一起拖住.
func (b *Backend) scheduleRestart() {
	delay := b.cfg.RestartDelay
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-b.done:
			return
		case <-timer.C:
		}
		if err := b.EnsureRunning(); err != nil {
			log.Errorf("[GameHelper]", "restart helper failed: {0}", err)
			// 还没起来就再试一次: helper 起不来的原因多半是临时的
			// (资源没释放、端口还占着), 但也可能是致命的, 所以只重试不放弃.
			b.scheduleRestart()
			return
		}
	}()
}

// handlerSnapshot 取一份回调快照, 避免调用回调时持锁
func (b *Backend) handlerSnapshot() Handlers {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.handlers
}

// wrapProcError 把裸错误包上一层 helper 的诊断信息
func (b *Backend) wrapProcError(s *session, err error) error {
	if err == nil {
		return nil
	}
	if s == nil {
		return err
	}
	if tail := strings.TrimSpace(s.proc.StderrTail()); tail != "" {
		return fmt.Errorf("%w (helper stderr: %s)", err, tail)
	}
	return err
}

// terminate 先礼后兵: 关 stdin 给 helper 自己退出的机会, 超时再 Kill
func (b *Backend) terminate(s *session) {
	if s == nil {
		return
	}
	_ = s.proc.CloseStdin()
	if !s.proc.WaitFor(b.cfg.StopGrace) {
		s.proc.Kill()
		// 再等一小会儿, 确保底层 Wait 已返回, 避免僵尸进程
		s.proc.WaitFor(2 * time.Second)
	}
}

// Close 终止 helper 进程 (对应服务器退出时关闭外部依赖)
func (b *Backend) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	s := b.session
	b.session = nil
	pending := b.pending
	b.pending = make(map[int64]chan *Response)
	b.mu.Unlock()

	b.doneOnce.Do(func() { close(b.done) })

	for _, ch := range pending {
		close(ch)
	}

	b.terminate(s)
	return nil
}
