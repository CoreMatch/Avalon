package plugin

// 测试基建: 按契约实现插件侧的假 HTTP 服务 (fakePlugin), 以及把
// /data 隔离到临时目录的辅助函数.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CoreMatch/Avalon/internal/util/file"
)

// testSecret 生成 32 字节随机密钥 (hex 形式给配置, 原始字节留测试用)
func testSecret(t *testing.T) (hexStr string, raw []byte) {
	t.Helper()
	raw = make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(raw), raw
}

// fakeRequest 记录一次到达假插件的请求
type fakeRequest struct {
	Path string
	Body []byte
}

// eventHandler 返回 (状态码, 应答正文); 正文为 nil 时用默认应答
type eventHandler func(body []byte) (int, []byte)

// fakePlugin 是按 avalon-plugin-v1 契约实现的插件侧测试替身
type fakePlugin struct {
	t       *testing.T
	secret  []byte
	baseURL string

	mu          sync.Mutex
	manifest    *Manifest // nil -> manifest 端点回 500
	manifestRaw []byte    // 非 nil 时原样提供 (测畸形 manifest)
	handlers    map[string]eventHandler
	requests    []fakeRequest
	signResp    bool   // 是否给应答签名 (默认 true)
	verifyReq   bool   // 是否校验请求签名 (默认 true)
	respSecret  []byte // 应答签名密钥; 为空时用 secret (测轮换重叠)
	badReqSig   int    // 请求签名校验失败计数
}

// newFakePlugin 起一个默认可用的假插件: 支持两个事件, room.create 放行
func newFakePlugin(t *testing.T) *fakePlugin {
	t.Helper()
	_, raw := testSecret(t)
	f := &fakePlugin{
		t:         t,
		secret:    raw,
		manifest:  defaultTestManifest(),
		handlers:  map[string]eventHandler{},
		signResp:  true,
		verifyReq: true,
	}
	f.handlers[EventRoomCreate] = func(_ []byte) (int, []byte) {
		return http.StatusOK, []byte(`{"allowed":true}`)
	}
	f.handlers[EventGameOver] = func(_ []byte) (int, []byte) {
		return http.StatusOK, []byte(`{}`)
	}
	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)
	f.baseURL = srv.URL
	return f
}

// defaultTestManifest 两个事件都支持 v1 的标准 manifest
func defaultTestManifest() *Manifest {
	return &Manifest{
		Schema:   ManifestSchema,
		Protocol: ProtocolV1,
		ID:       "fake-plugin",
		Version:  "1.0.0-test",
		Events: map[string]EventCapability{
			EventRoomCreate: {Versions: []int{1}},
			EventGameOver:   {Versions: []int{1}},
		},
		TimeoutMs:      2000,
		MaxConcurrency: 1,
	}
}

// v2TestManifest 全事件目录的 manifest; vetoEvents 里的事件声明 veto 能力
func v2TestManifest(vetoEvents ...string) *Manifest {
	m := &Manifest{
		Schema:   ManifestSchema,
		Protocol: ProtocolV1,
		ID:       "fake-plugin-v2",
		Version:  "2.0.0-test",
		Events:   map[string]EventCapability{},
	}
	vetoSet := map[string]bool{}
	for _, name := range vetoEvents {
		vetoSet[name] = true
	}
	for _, name := range allEventNames {
		m.Events[name] = EventCapability{Versions: []int{1}, Veto: vetoSet[name]}
	}
	return m
}

// observeAckHandlers 给全部观察档事件装默认 200 应答 (room.create 放行)
func observeAckHandlers(f *fakePlugin) {
	f.handlers[EventRoomCreate] = func(_ []byte) (int, []byte) {
		return http.StatusOK, []byte(`{"allowed":true}`)
	}
	f.handlers[EventGameOver] = func(_ []byte) (int, []byte) {
		return http.StatusOK, []byte(`{}`)
	}
	for _, name := range []string{
		EventPlayerJoin, EventPlayerLeave, EventPlayerBanned, EventPlayerUnbanned,
		EventPlayerIpBanned, EventGameStart, EventPlayerChat,
		EventUnitOperation, EventFactoryBuild, EventPlayerKicked,
	} {
		f.handlers[name] = func(_ []byte) (int, []byte) {
			return http.StatusOK, []byte(`{}`)
		}
	}
}

func (f *fakePlugin) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))

	f.mu.Lock()
	f.requests = append(f.requests, fakeRequest{Path: r.URL.Path, Body: body})
	f.mu.Unlock()

	if f.verifyReq {
		tsRaw := r.Header.Get(headerTimestamp)
		sig := r.Header.Get(headerSignature)
		ts, err := strconv.ParseInt(tsRaw, 10, 64)
		ok := err == nil && sig == signBody(f.secret, ts, body)
		if !ok {
			f.mu.Lock()
			f.badReqSig++
			f.mu.Unlock()
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/manifest":
		f.mu.Lock()
		raw := f.manifestRaw
		m := f.manifest
		f.mu.Unlock()
		var out []byte
		if raw != nil {
			out = raw
		} else if m != nil {
			out, _ = json.Marshal(m)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		f.writeSigned(w, http.StatusOK, out)

	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/events/"):
		name := strings.TrimPrefix(r.URL.Path, "/v1/events/")
		f.mu.Lock()
		h := f.handlers[name]
		f.mu.Unlock()
		if h == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		status, resp := h(body)
		if resp == nil {
			resp = []byte(`{}`)
		}
		f.writeSigned(w, status, resp)

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// writeSigned 写应答 (按 signResp 决定是否签名)
func (f *fakePlugin) writeSigned(w http.ResponseWriter, status int, body []byte) {
	if f.signResp {
		secret := f.secret
		if f.respSecret != nil {
			secret = f.respSecret
		}
		ts := time.Now().Unix()
		w.Header().Set(headerTimestamp, strconv.FormatInt(ts, 10))
		w.Header().Set(headerSignature, signBody(secret, ts, body))
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// setManifest 热换 manifest (测协商切换)
func (f *fakePlugin) setManifest(m *Manifest, raw []byte) {
	f.mu.Lock()
	f.manifest = m
	f.manifestRaw = raw
	f.mu.Unlock()
}

// setHandler 热换某事件的处理
func (f *fakePlugin) setHandler(event string, h eventHandler) {
	f.mu.Lock()
	f.handlers[event] = h
	f.mu.Unlock()
}

// requestsOf 返回指定路径的请求副本
func (f *fakePlugin) requestsOf(path string) []fakeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeRequest
	for _, r := range f.requests {
		if r.Path == path {
			out = append(out, r)
		}
	}
	return out
}

// requestCount 返回总请求数
func (f *fakePlugin) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// --- 环境隔离 ---

// isolateBase 把 /data 根目录指到临时目录; 调用本函数的测试不得并行
// (SetBasePath 是进程级的)
func isolateBase(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	file.SetBasePath(dir)
	return dir
}

// writePluginsJSON 在隔离目录里落一份 Plugins.json
func writePluginsJSON(t *testing.T, content string) {
	t.Helper()
	f := file.GetFolder("/data").ToFile("Plugins.json")
	f.WriteFile(content)
}

// entryFor 为假插件生成一条配置条目
func entryFor(name string, f *fakePlugin, hexSecret string) Entry {
	return Entry{
		Name:    name,
		BaseURL: f.baseURL,
		Secrets: []string{hexSecret},
	}
}

// configWith 生成带条目的合法配置 JSON
func configWith(entries ...Entry) string {
	cfg := NewConfig()
	cfg.Entries = entries
	b, _ := json.Marshal(cfg)
	return string(b)
}

// waitFor 在超时内轮询条件; 失败报测试错误
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", what)
}
