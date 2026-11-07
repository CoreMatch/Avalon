package plugin

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/CoreMatch/Avalon/internal/game/event"
	"github.com/CoreMatch/Avalon/internal/util/file"
	"github.com/CoreMatch/Avalon/internal/util/jsonutil"
	"github.com/CoreMatch/Avalon/internal/util/log"
	"github.com/CoreMatch/Avalon/internal/util/timeutil"
)

// 退避参数 (ADR-0006 "Delivery guarantees" / "Version negotiation")
const (
	initialDeliverBackoff = 1 * time.Second
	maxDeliverBackoff     = 60 * time.Second

	initialManifestBackoff = 5 * time.Second
	maxManifestBackoff     = 5 * time.Minute
)

// Hub 持有全部已配置插件的运行时状态.
//
// 构造 (Load) 之后语义不可变: 插件列表固定, 配置改动需重启生效 (v1 不做
// 热加载, 见 backlog). Close 停止全部 worker 并等待退出.
type Hub struct {
	cfg     Config
	plugins []*Plugin
	client  *http.Client

	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// Plugin 是单个插件的运行时: 协商状态 + outbox + 投递 worker + 计数器
type Plugin struct {
	entry    Entry
	secrets  [][]byte
	disabled bool
	hub      *Hub

	mu         sync.Mutex
	ready      bool
	manifest   *Manifest
	negotiated map[string]int

	outbox *Outbox
	notify chan struct{}

	delivered atomic.Int64
	retries   atomic.Int64
	dropped   atomic.Int64
	denied    atomic.Int64
	failOpen  atomic.Int64
}

// Load 读取配置、校验、并行做首轮 manifest 协商, 并为每个启用的插件启动
// 唯一的投递 worker (单 worker = 该插件的投递严格按入队序).
//
// 配置缺失/非法返回错误使启动失败 —— 插件是显式配置, 不静默降级
// (与 Cascade 网关同一纪律). 插件不可达**不**失败: 标记 unready 后由
// worker 后台重试协商.
func Load() (*Hub, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}

	h := &Hub{
		cfg:    cfg,
		stopCh: make(chan struct{}),
		// 3xx 不跟随: 重定向后的请求不再携带原签名语义, 一律按失败处理
		client: &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}

	for _, e := range cfg.Entries {
		p := &Plugin{
			entry:    e,
			disabled: !e.enabled(),
			hub:      h,
			notify:   make(chan struct{}, 1),
		}
		for _, s := range e.Secrets {
			raw, _ := hex.DecodeString(s) // validate 已保证可解码
			p.secrets = append(p.secrets, raw)
		}
		p.outbox = openOutbox(outboxFile(e.Name), cfg.OutboxCapacity)
		h.plugins = append(h.plugins, p)
	}

	// 首轮协商: 并行, 每个插件独立超时 (上限 = admissionTimeoutMs).
	// 失败只记日志: 启动不得被不可达的插件拖死.
	var bootWG sync.WaitGroup
	for _, p := range h.plugins {
		if p.disabled {
			continue
		}
		bootWG.Add(1)
		go func(p *Plugin) {
			defer bootWG.Done()
			ctx, cancel := context.WithTimeout(context.Background(), h.admissionTimeout())
			defer cancel()
			negotiated, m, err := p.fetchManifest(ctx)
			if err != nil {
				log.Warnf("[Plugin]", "{0}: manifest negotiation failed at boot: {1} (retrying in background)", p.entry.Name, err)
				return
			}
			p.applyManifest(m, negotiated)
		}(p)
	}
	bootWG.Wait()

	for _, p := range h.plugins {
		if p.disabled {
			log.Clog("[Plugin] {0}: disabled by config", p.entry.Name)
			continue
		}
		h.wg.Add(1)
		go func(p *Plugin) {
			defer h.wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Errorf("[Plugin]", "{0} worker panic: {1}", p.entry.Name, r)
				}
			}()
			p.run()
		}(p)
	}
	h.loadSummary()
	return h, nil
}

// Close 停止全部 worker 并等待退出 (挂进 CloseServerHook 链)
func (h *Hub) Close() {
	h.stopOnce.Do(func() { close(h.stopCh) })
	h.wg.Wait()
}

func (h *Hub) stopped() bool {
	select {
	case <-h.stopCh:
		return true
	default:
		return false
	}
}

func (h *Hub) admissionTimeout() time.Duration {
	return time.Duration(h.cfg.AdmissionTimeoutMs) * time.Millisecond
}

func (h *Hub) deliveryTimeout() time.Duration {
	return time.Duration(h.cfg.DeliveryTimeoutMs) * time.Millisecond
}

// --- 插件运行时 ---

func (p *Plugin) isReady() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ready
}

func (p *Plugin) getManifest() *Manifest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.manifest
}

func (p *Plugin) negotiatedFor(eventName string) (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.negotiated[eventName]
	return v, ok
}

func (p *Plugin) markUnready() {
	p.mu.Lock()
	p.ready = false
	p.mu.Unlock()
}

// applyManifest 协商成功: 置 ready, 丢弃版本不匹配的积压事件, 唤醒 worker
func (p *Plugin) applyManifest(m *Manifest, negotiated map[string]int) {
	p.mu.Lock()
	p.ready = true
	p.manifest = m
	p.negotiated = negotiated
	p.mu.Unlock()

	if n := p.outbox.filterDrop(func(r outboxRecord) bool {
		_, ok := negotiated[r.Event]
		return !ok
	}); n > 0 {
		p.dropped.Add(int64(n))
		log.Warnf("[Plugin]", "{0}: dropped {1} buffered event(s) not covered by the negotiated manifest", p.entry.Name, n)
	}
	if m.MaxConcurrency > 1 {
		log.Warnf("[Plugin]", "{0}: maxConcurrency={1} is reserved in v1, using 1 worker", p.entry.Name, m.MaxConcurrency)
	}

	names := make([]string, 0, len(negotiated))
	for _, name := range []string{EventRoomCreate, EventGameOver} {
		if _, ok := negotiated[name]; ok {
			names = append(names, name)
		}
	}
	log.Clog("[Plugin] {0} ready: id={1} version={2} events=[{3}]",
		p.entry.Name, m.ID, m.Version, strings.Join(names, ", "))
	p.wake()
}

// run 是单插件的投递主循环: 未就绪时做 manifest 重试, 就绪时按序投递
// (顺序与背压语义见 ADR-0006 "Ordering and backpressure").
func (p *Plugin) run() {
	manifestBackoff := initialManifestBackoff
	deliverBackoff := time.Duration(0)

	for {
		if p.hub.stopped() {
			return
		}

		if !p.isReady() {
			ctx, cancel := context.WithTimeout(context.Background(), p.hub.admissionTimeout())
			negotiated, m, err := p.fetchManifest(ctx)
			cancel()
			if err != nil {
				log.Warnf("[Plugin]", "{0}: manifest retry failed: {1}", p.entry.Name, err)
				if p.sleepOrStop(manifestBackoff) {
					return
				}
				manifestBackoff = minDuration(manifestBackoff*2, maxManifestBackoff)
				continue
			}
			manifestBackoff = initialManifestBackoff
			p.applyManifest(m, negotiated)
			continue
		}

		rec, ok := p.outbox.peek()
		if !ok {
			deliverBackoff = 0
			if p.waitNotify() {
				return
			}
			continue
		}

		if p.tooOld(rec) {
			p.outbox.pop()
			p.dropped.Add(1)
			log.Warnf("[Plugin]", "{0}: dropped event {1} (older than {2}h)", p.entry.Name, rec.EventID, p.hub.cfg.MaxEventAgeHours)
			continue
		}
		if _, ok := p.negotiatedFor(rec.Event); !ok {
			p.outbox.pop()
			p.dropped.Add(1)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), p.hub.deliveryTimeout())
		err := p.deliver(ctx, rec)
		cancel()
		if err == nil {
			p.outbox.pop()
			p.delivered.Add(1)
			deliverBackoff = 0
			continue
		}

		p.retries.Add(1)
		if isConnError(err) {
			// 连接级失败 = 进程可能已死/重启: 回到协商路径 (含其自身退避),
			// 重新协商也会刷新版本约定.
			log.Warnf("[Plugin]", "{0}: connection lost ({1}), renegotiating manifest", p.entry.Name, err)
			p.markUnready()
			continue
		}
		log.Warnf("[Plugin]", "{0}: delivery of {1} failed: {2} (retrying)", p.entry.Name, rec.EventID, err)
		if deliverBackoff == 0 {
			deliverBackoff = initialDeliverBackoff
		} else {
			deliverBackoff = minDuration(deliverBackoff*2, maxDeliverBackoff)
		}
		if p.sleepOrStop(deliverBackoff) {
			return
		}
	}
}

// deliver 投递一条事件并校验应答签名; 2xx+验签通过才算成功
func (p *Plugin) deliver(ctx context.Context, rec outboxRecord) error {
	body := []byte(rec.Body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.eventURL(rec.Event), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	signRequest(req, p.secrets[0], body)

	resp, err := p.hub.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(respBody) > maxResponseBytes {
		return fmt.Errorf("response exceeds %d bytes", maxResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	if err := verifyResponse(p.secrets, resp, respBody); err != nil {
		return fmt.Errorf("ack signature: %w", err)
	}
	return nil
}

func (p *Plugin) eventURL(eventName string) string {
	return strings.TrimRight(p.entry.BaseURL, "/") + "/v1/events/" + eventName
}

func (p *Plugin) tooOld(rec outboxRecord) bool {
	maxAge := time.Duration(p.hub.cfg.MaxEventAgeHours) * time.Hour
	return timeutil.ConcurrentMillis()-rec.EnqueuedAtMs > maxAge.Milliseconds()
}

// sleepOrStop 睡 d 时长; 期间收到停止信号返回 true
func (p *Plugin) sleepOrStop(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-p.hub.stopCh:
		return true
	case <-t.C:
		return false
	}
}

// waitNotify 空队列等待: 被新事件唤醒返回 false, 收到停止信号返回 true
func (p *Plugin) waitNotify() bool {
	select {
	case <-p.hub.stopCh:
		return true
	case <-p.notify:
		return false
	}
}

// wake 非阻塞唤醒 worker (缓冲 1, 多次唤醒合并成一次)
func (p *Plugin) wake() {
	select {
	case p.notify <- struct{}{}:
	default:
	}
}

// admissionTimeout 单插件准入调用上限 = min(manifest 自报, Avalon 上限)
func (p *Plugin) admissionTimeout() time.Duration {
	cap := p.hub.admissionTimeout()
	if m := p.getManifest(); m != nil && m.TimeoutMs > 0 {
		if self := time.Duration(m.TimeoutMs) * time.Millisecond; self < cap {
			return self
		}
	}
	return cap
}

// --- 两个扩展点的对外语义 ---

// Decision 是建房准入的结论
type Decision struct {
	// Allowed 是否放行
	Allowed bool
	// Reason 否决理由 (已净化: 去控制符、截断 200 rune); 放行时为空
	Reason string
	// Plugin 首个否决的插件名 (放行时为空)
	Plugin string
}

// admissionOutcome 单插件准入调用结果 (零值 = 放行贡献)
type admissionOutcome struct {
	denied     bool
	denyReason string
}

// AdmitRoomCreate 并行咨询全部就绪且订阅 room.create 的插件.
//
// 语义 (契约 "room.create"): 任一有效否决即否决 (理由串联); 超时/不可达/
// 畸形应答/无理由拒绝一律 fail-open 放行并计数告警; 未就绪插件跳过并计
// fail-open (它本应参与准入). 至多一次, 不重试.
func (h *Hub) AdmitRoomCreate(req RoomCreateRequest) Decision {
	candidates := make([]*Plugin, 0, len(h.plugins))
	for _, p := range h.plugins {
		if p.disabled {
			continue
		}
		if !p.isReady() {
			p.failOpen.Add(1)
			log.Warnf("[Plugin]", "{0}: not ready, room.create admission skipped (fail-open)", p.entry.Name)
			continue
		}
		if _, ok := p.negotiatedFor(EventRoomCreate); !ok {
			continue // 未订阅: 不参与准入, 也不算失败
		}
		candidates = append(candidates, p)
	}
	if len(candidates) == 0 {
		return Decision{Allowed: true}
	}

	body := []byte(jsonutil.ToJSON(buildRoomCreateEvent(req, time.Now())))

	results := make([]admissionOutcome, len(candidates))
	var wg sync.WaitGroup
	for i, p := range candidates {
		wg.Add(1)
		go func(i int, p *Plugin) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Errorf("[Plugin]", "{0} admission panic: {1}", p.entry.Name, r)
				}
			}()
			results[i] = p.callAdmission(body)
		}(i, p)
	}
	wg.Wait()

	var reasons []string
	var firstPlugin string
	for i, r := range results {
		if r.denied {
			candidates[i].denied.Add(1)
			reasons = append(reasons, candidates[i].entry.Name+": "+r.denyReason)
			if firstPlugin == "" {
				firstPlugin = candidates[i].entry.Name
			}
		}
	}
	if len(reasons) > 0 {
		return Decision{Allowed: false, Reason: strings.Join(reasons, "; "), Plugin: firstPlugin}
	}
	return Decision{Allowed: true}
}

// callAdmission 对单个插件执行一次准入调用; 任何异常路径都 fail-open
func (p *Plugin) callAdmission(body []byte) admissionOutcome {
	fail := func(err error) admissionOutcome {
		p.failOpen.Add(1)
		log.Warnf("[Plugin]", "{0}: room.create admission failed open: {1}", p.entry.Name, err)
		return admissionOutcome{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), p.admissionTimeout())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.eventURL(EventRoomCreate), bytes.NewReader(body))
	if err != nil {
		return fail(err)
	}
	req.Header.Set("Content-Type", "application/json")
	signRequest(req, p.secrets[0], body)

	resp, err := p.hub.client.Do(req)
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fail(err)
	}
	if len(respBody) > maxResponseBytes {
		return fail(fmt.Errorf("response exceeds %d bytes", maxResponseBytes))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fail(fmt.Errorf("status %d", resp.StatusCode))
	}
	if err := verifyResponse(p.secrets, resp, respBody); err != nil {
		return fail(fmt.Errorf("verdict signature: %w", err))
	}

	verdict := struct {
		Allowed *bool  `json:"allowed"`
		Reason  string `json:"reason"`
	}{}
	if err := jsonUnmarshal(respBody, &verdict); err != nil {
		return fail(fmt.Errorf("verdict parse: %w", err))
	}
	if verdict.Allowed == nil {
		return fail(errors.New("verdict missing 'allowed'"))
	}
	if *verdict.Allowed {
		return admissionOutcome{}
	}
	reason := sanitizeReason(verdict.Reason)
	if reason == "" {
		return fail(errors.New("deny without readable reason"))
	}
	return admissionOutcome{denied: true, denyReason: reason}
}

// EmitGameOver 把对局结束事件入队到全部启用的插件 (非阻塞).
//
// 就绪且已声明不订阅 game.over 的插件直接跳过; 未就绪插件先入队, 协商
// 成功后补投 (版本不匹配时在 applyManifest 里丢弃并计数).
// 同一事件的多个插件副本共享同一 eventId 与正文.
func (h *Hub) EmitGameOver(roomCode string, data *event.GameOverData) {
	if len(h.plugins) == 0 {
		return
	}
	ev := buildGameOverEvent(roomCode, data, time.Now())
	body := jsonutil.ToJSON(ev)
	rec := outboxRecord{
		EventID:      ev.EventID,
		Event:        EventGameOver,
		EnqueuedAtMs: timeutil.ConcurrentMillis(),
		Body:         body,
	}
	for _, p := range h.plugins {
		if p.disabled {
			continue
		}
		if p.isReady() {
			if _, ok := p.negotiatedFor(EventGameOver); !ok {
				continue
			}
		}
		if dropped := p.outbox.append(rec); dropped > 0 {
			p.dropped.Add(int64(dropped))
			log.Warnf("[Plugin]", "{0}: outbox full, dropped {1} oldest event(s)", p.entry.Name, dropped)
		}
		p.wake()
	}
}

// --- 运维可见性 ---

// PluginStatus 是单个插件的只读状态快照 (控制台 plugin 命令用)
type PluginStatus struct {
	Name            string
	BaseURL         string
	Enabled         bool
	Ready           bool
	ManifestID      string
	ManifestVersion string
	Events          []string
	Queued          int
	Delivered       int64
	Retries         int64
	Dropped         int64
	Denied          int64
	FailOpen        int64
}

// Status 返回全部插件 (含停用) 的状态快照, 按配置顺序
func (h *Hub) Status() []PluginStatus {
	out := make([]PluginStatus, 0, len(h.plugins))
	for _, p := range h.plugins {
		st := PluginStatus{
			Name:      p.entry.Name,
			BaseURL:   p.entry.BaseURL,
			Enabled:   !p.disabled,
			Ready:     p.isReady(),
			Queued:    p.outbox.size(),
			Delivered: p.delivered.Load(),
			Retries:   p.retries.Load(),
			Dropped:   p.dropped.Load(),
			Denied:    p.denied.Load(),
			FailOpen:  p.failOpen.Load(),
		}
		if m := p.getManifest(); m != nil {
			st.ManifestID = m.ID
			st.ManifestVersion = m.Version
		}
		for _, name := range []string{EventRoomCreate, EventGameOver} {
			if _, ok := p.negotiatedFor(name); ok {
				st.Events = append(st.Events, name)
			}
		}
		out = append(out, st)
	}
	return out
}

// --- 工具 ---

// maxReasonRunes 否决理由呈现给玩家前的长度上限
const maxReasonRunes = 200

// sanitizeReason 去控制符、去首尾空白、按 rune 截断
func sanitizeReason(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if runes := []rune(s); len(runes) > maxReasonRunes {
		s = string(runes[:maxReasonRunes])
	}
	return s
}

// isConnError 判断是否连接级失败 (进程死/拒连): 超时不算, 走投递退避
func isConnError(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return false
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	return false
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func jsonUnmarshal(data []byte, v any) error {
	return json.NewDecoder(bytes.NewReader(data)).Decode(v)
}

func outboxFile(name string) *file.FileUtils {
	return file.GetFolder("/data/plugins/outbox").ToFile(outboxFileName(name) + ".jsonl")
}

// loadSummary 启动日志: 无插件时保持安静 (默认状态不打扰)
func (h *Hub) loadSummary() {
	if len(h.plugins) == 0 {
		return
	}
	for _, p := range h.plugins {
		switch {
		case p.disabled:
			continue // Load 里已打印
		case p.isReady():
			// applyManifest 已打印 ready 行
		default:
			log.Clog("[Plugin] {0}: not ready at boot, will retry in background", p.entry.Name)
		}
	}
}
