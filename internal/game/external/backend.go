package external

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"rwhps/internal/game"
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
	// StopGrace Stop 之后等 helper 自行退出的时间, 默认 10s
	StopGrace time.Duration
}

// 默认值
const (
	DefaultStderrMax    = 8 * 1024
	DefaultHelloTimeout = 15 * time.Second
	DefaultOpTimeout    = 60 * time.Second
	DefaultStopGrace    = 10 * time.Second
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

// New 构造外部后端
//
// 只做参数校验, 不启动进程 —— 启动是惰性的 (第一次 Start 时才拉起 JVM),
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
	if cfg.StopGrace <= 0 {
		cfg.StopGrace = DefaultStopGrace
	}

	b := &Backend{cfg: cfg}
	if err := b.CheckAvailable(); err != nil {
		return nil, err
	}
	return b, nil
}

// CheckAvailable 检查 java 与 helper 是否就位
//
// 单独抽出来是因为启动链需要在不启动进程的前提下知道"外部依赖可不可用",
// 从而决定是把 Backend 装上还是继续用 Unimplemented 并给出明确提示.
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

// lineResult 是读 goroutine 交付的一条输出
type lineResult struct {
	line string
	err  error
}

// Backend 是 game.Simulation 的外部进程实现
type Backend struct {
	cfg Config

	mu     sync.Mutex
	proc   process
	lines  chan lineResult
	quit   chan struct{}
	nextID int64

	running bool
	players int
}

// 编译期确认 Backend 满足 game.Simulation
var _ game.Simulation = (*Backend)(nil)

// ensureProc 惰性启动 helper 并完成握手
//
// 进程意外退出后会被标记为 nil, 下次调用重新拉起 —— 外部依赖挂掉不应该
// 让服务器跟着挂掉, 但也不能假装它还在.
func (b *Backend) ensureProc() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.proc != nil {
		select {
		case <-b.proc.Done():
			b.dropProcLocked()
		default:
		}
	}
	if b.proc != nil {
		return nil
	}

	java := b.cfg.JavaPath
	if java == "" {
		java = "java"
	}
	proc := newProcess(java, b.javaArgs(), b.cfg.WorkDir, b.cfg.StderrMax)
	if err := proc.Start(); err != nil {
		return err
	}
	b.proc = proc
	b.lines = make(chan lineResult, 4)
	b.quit = make(chan struct{})
	go b.pump(proc, b.lines, b.quit)

	// 握手: 让"java 不存在 / jar 不存在 / 类路径错"在第一秒就暴露出来,
	// 而不是等到第一次开局才以一个奇怪的 EOF 出现.
	if err := b.helloLocked(); err != nil {
		b.terminateLocked()
		return err
	}
	return nil
}

// helloLocked 发送握手并校验版本 (调用方须持有 mu)
func (b *Backend) helloLocked() error {
	req := &Request{V: ProtocolVersion, ID: b.nextID, Op: OpHello}
	b.nextID++

	line, err := encodeRequest(req)
	if err != nil {
		return err
	}
	if err := b.proc.WriteLine(line); err != nil {
		return b.wrapProcError(err)
	}

	resp, err := b.readResponse(b.cfg.HelloTimeout)
	if err != nil {
		return b.wrapProcError(err)
	}
	if resp.ID != req.ID {
		return fmt.Errorf("%w: hello reply id %d, want %d", ErrProtocol, resp.ID, req.ID)
	}
	if !resp.OK {
		return b.wrapProcError(errorf(resp))
	}
	return nil
}

// pump 是进程存续期间唯一读 helper 输出的 goroutine
//
// 之所以要独占: 如果每次 readResponse 都自己起一个 goroutine 去 ReadLine,
// 那么一次超时就会留下一个没人管的读者, 它会抢走下一次操作的响应, 表现为
// "reply id 对不上"这种很难查的错. 由单一读者把行送进 channel, 超时只是
// 放弃等待, 响应不会丢, 下一个读者还能收到.
func (b *Backend) pump(proc process, lines chan lineResult, quit chan struct{}) {
	for {
		line, err := proc.ReadLine()
		res := lineResult{line: line, err: err}
		select {
		case lines <- res:
		case <-quit:
			return
		}
		if err != nil {
			return
		}
	}
}

// readResponse 读一条响应 (调用方须持有 mu)
func (b *Backend) readResponse(timeout time.Duration) (*Response, error) {
	// 先把已经到达的响应用掉, 免得和下面的 timer / Done 竞争
	select {
	case r := <-b.lines:
		return b.handleLine(r)
	default:
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case r := <-b.lines:
		return b.handleLine(r)
	case <-timer.C:
		// 读超时不关进程: 可能是 helper 卡住, 也可能是协议对不上.
		// 下一次操作会继续等, 由调用方决定是否终止.
		return nil, fmt.Errorf("external: helper did not reply within %s", timeout)
	case <-b.proc.Done():
		// 进程刚退, 但可能已经回了一条 —— 优先用掉它
		select {
		case r := <-b.lines:
			return b.handleLine(r)
		default:
			return nil, ErrNotRunning
		}
	}
}

// handleLine 把读 goroutine 交付的一行转成 Response
func (b *Backend) handleLine(r lineResult) (*Response, error) {
	if r.err != nil {
		return nil, r.err
	}
	return decodeResponse(r.line)
}

// wrapProcError 把裸错误包上一层 helper 的诊断信息
func (b *Backend) wrapProcError(err error) error {
	if err == nil {
		return nil
	}
	if b.proc == nil {
		return err
	}
	if tail := strings.TrimSpace(b.proc.StderrTail()); tail != "" {
		return fmt.Errorf("%w (helper stderr: %s)", err, tail)
	}
	return err
}

// Start 实现 game.Simulation
func (b *Backend) Start(cfg game.MatchConfig) error {
	if err := b.ensureProc(); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	req := &Request{
		V:         ProtocolVersion,
		ID:        b.nextID,
		Op:        OpStart,
		Room:      cfg.RoomID,
		Map:       cfg.MapName,
		MapPlayer: cfg.MapPlayer,
		MaxPlayer: cfg.MaxPlayer,
	}
	b.nextID++

	line, err := encodeRequest(req)
	if err != nil {
		return err
	}
	if err := b.proc.WriteLine(line); err != nil {
		return b.wrapProcError(err)
	}

	resp, err := b.readResponse(b.cfg.OpTimeout)
	if err != nil {
		return b.wrapProcError(err)
	}
	if resp.ID != req.ID {
		return fmt.Errorf("%w: start reply id %d, want %d", ErrProtocol, resp.ID, req.ID)
	}
	if !resp.OK {
		return b.wrapProcError(errorf(resp))
	}

	b.running = true
	b.players = resp.Players
	return nil
}

// Stop 实现 game.Simulation
//
// 只发 stop 操作, 不杀进程 —— 进程要活到服务器关闭, 反复起 JVM 的代价太高.
func (b *Backend) Stop() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.proc == nil {
		return nil
	}
	select {
	case <-b.proc.Done():
		b.dropProcLocked()
		b.running = false
		return nil
	default:
	}

	req := &Request{V: ProtocolVersion, ID: b.nextID, Op: OpStop}
	b.nextID++

	line, err := encodeRequest(req)
	if err != nil {
		return err
	}
	if err := b.proc.WriteLine(line); err != nil {
		b.terminateLocked()
		return b.wrapProcError(err)
	}

	resp, err := b.readResponse(b.cfg.OpTimeout)
	if err != nil {
		b.terminateLocked()
		return b.wrapProcError(err)
	}
	if resp.ID != req.ID {
		b.terminateLocked()
		return fmt.Errorf("%w: stop reply id %d, want %d", ErrProtocol, resp.ID, req.ID)
	}
	if !resp.OK {
		// 局可能已经结束了, 但协议上这是一次失败; 不终止进程, 让调用方决定
		return b.wrapProcError(errorf(resp))
	}

	b.running = false
	b.players = 0
	return nil
}

// Status 向 helper 查询一次当前状态 (对应 OpStatus)
//
// PlayerCount 返回的是缓存值, 因为这个方法有一次网络往返, 不适合放在
// game.Simulation 的 getter 里被命令层随手调用; 需要精确人数时由调用方
// (启动链的定时任务、status 命令) 显式调它.
func (b *Backend) Status() (running bool, players int, err error) {
	if err := b.ensureProc(); err != nil {
		return false, 0, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	req := &Request{V: ProtocolVersion, ID: b.nextID, Op: OpStatus}
	b.nextID++

	line, err := encodeRequest(req)
	if err != nil {
		return false, 0, err
	}
	if err := b.proc.WriteLine(line); err != nil {
		return false, 0, b.wrapProcError(err)
	}

	resp, err := b.readResponse(b.cfg.OpTimeout)
	if err != nil {
		return false, 0, b.wrapProcError(err)
	}
	if resp.ID != req.ID {
		return false, 0, fmt.Errorf("%w: status reply id %d, want %d", ErrProtocol, resp.ID, req.ID)
	}
	if !resp.OK {
		return false, 0, b.wrapProcError(errorf(resp))
	}

	b.running = resp.Running
	b.players = resp.Players
	return resp.Running, resp.Players, nil
}

// Running 实现 game.Simulation
func (b *Backend) Running() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.proc == nil {
		return false
	}
	select {
	case <-b.proc.Done():
		b.dropProcLocked()
		b.running = false
		return false
	default:
	}
	return b.running
}

// PlayerCount 实现 game.Simulation
//
// 原版读的是 room.playerManage.playerGroup.size, 即"游戏内玩家数";
// Go 侧没有玩家管理 (属于未移植的 game 层), 因此这个值只能由 helper 通过
// status 操作给出. 为避免每次查询都起一次往返, 只在 Start/Stop/Status 时同步,
// 中间用缓存值 —— 需要精确值时调用 Status.
func (b *Backend) PlayerCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.players
}

// Close 终止 helper 进程 (对应服务器退出时关闭外部依赖)
func (b *Backend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.proc == nil {
		return nil
	}
	b.terminateLocked()
	return nil
}

// dropProcLocked 丢掉当前进程的引用并叫停读 goroutine (调用方须持有 mu)
//
// 不终止进程 —— 调用方要么已经知道进程死了, 要么正要自己收尾.
func (b *Backend) dropProcLocked() {
	if b.quit != nil {
		close(b.quit)
		b.quit = nil
	}
	b.proc = nil
	b.lines = nil
}

// terminateLocked 先礼后兵: 关 stdin 给 helper 自己退出的机会, 超时再 Kill
func (b *Backend) terminateLocked() {
	if b.proc == nil {
		return
	}
	proc := b.proc
	b.dropProcLocked()
	b.running = false
	b.players = 0

	// 先叫停读 goroutine: 它可能正阻塞在 ReadLine 上, 关掉 quit 之后
	// 下一次投递就会直接返回, 不会变成没人收的僵尸 goroutine.
	_ = proc.CloseStdin()

	if !proc.WaitFor(b.cfg.StopGrace) {
		proc.Kill()
		// 再等一小会儿, 确保底层 Wait 已返回, 避免僵尸进程
		proc.WaitFor(2 * time.Second)
	}
}
