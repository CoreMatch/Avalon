// Package plugintest 为接线测试 (gateway / realize / app) 提供按
// avalon-plugin-v1 契约实现的假插件服务与 hub 装配, 避免各包重复实现
// 签名与协商逻辑.
package plugintest

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/CoreMatch/Avalon/internal/plugin"
	"github.com/CoreMatch/Avalon/internal/util/file"
)

const (
	headerTimestamp = "X-Avalon-Timestamp"
	headerSignature = "X-Avalon-Signature"
)

// Server 是一个可控行为的假插件 (HTTP 服务)
type Server struct {
	URL       string
	SecretHex string
	secret    []byte

	mu       sync.Mutex
	deny     bool
	reason   string
	requests int
}

// Start 启动一个假插件: deny 决定 room.create 放行与否决
func Start(t *testing.T, deny bool, reason string) *Server {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		SecretHex: hex.EncodeToString(raw),
		secret:    raw,
		deny:      deny,
		reason:    reason,
	}
	srv := httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(srv.Close)
	s.URL = srv.URL
	return s
}

// SetDeny 热切换准入行为
func (s *Server) SetDeny(deny bool, reason string) {
	s.mu.Lock()
	s.deny = deny
	s.reason = reason
	s.mu.Unlock()
}

// Requests 返回收到的请求总数
func (s *Server) Requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests++
	deny := s.deny
	reason := s.reason
	s.mu.Unlock()

	switch r.URL.Path {
	case "/v1/manifest":
		out, _ := json.Marshal(map[string]any{
			"schema":   "avalon-plugin/manifest",
			"protocol": "avalon-plugin/v1",
			"id":       "plugintest",
			"version":  "0.0.0-test",
			"events": map[string]any{
				"room.create": map[string]any{"versions": []int{1}},
				"game.over":   map[string]any{"versions": []int{1}},
			},
			"timeoutMs":      2000,
			"maxConcurrency": 1,
		})
		s.writeSigned(w, http.StatusOK, out)

	case "/v1/events/room.create":
		resp := `{"allowed":true}`
		if deny {
			resp = fmt.Sprintf(`{"allowed":false,"reason":%q}`, reason)
		}
		s.writeSigned(w, http.StatusOK, []byte(resp))

	case "/v1/events/game.over":
		s.writeSigned(w, http.StatusOK, []byte(`{}`))

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *Server) writeSigned(w http.ResponseWriter, status int, body []byte) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	w.Header().Set(headerTimestamp, ts)
	w.Header().Set(headerSignature, sign(s.secret, ts, body))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func sign(secret []byte, ts string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	fmt.Fprintf(mac, "%s.", ts)
	mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

// Entry 为假插件生成一条配置条目
func (s *Server) Entry(name string) plugin.Entry {
	return plugin.Entry{Name: name, BaseURL: s.URL, Secrets: []string{s.SecretHex}}
}

// WriteConfig 在当前 file.BasePath 下写 Plugins.json
func WriteConfig(t *testing.T, entries ...plugin.Entry) {
	t.Helper()
	cfg := map[string]any{
		"version":            1,
		"admissionTimeoutMs": 2500,
		"deliveryTimeoutMs":  10000,
		"outboxCapacity":     128,
		"maxEventAgeHours":   24,
		"entries":            entries,
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	file.GetFolder("/data").ToFile("Plugins.json").WriteFile(string(b))
}

// Install 写配置并加载 hub, 注册进全局槽位, 测试结束自动清理.
// 调用方必须先隔离 file.BasePath.
func Install(t *testing.T, entries ...plugin.Entry) *plugin.Hub {
	t.Helper()
	WriteConfig(t, entries...)
	hub, err := plugin.Load()
	if err != nil {
		t.Fatalf("plugin.Load: %v", err)
	}
	old := plugin.Default()
	plugin.SetDefault(hub)
	t.Cleanup(func() {
		plugin.SetDefault(old)
		hub.Close()
	})
	return hub
}
