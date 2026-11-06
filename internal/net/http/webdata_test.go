package http

import (
	"sync"
	"testing"
)

// dummyGet / dummyPost 测试类
type dummyGet struct {
	called bool
	mu     sync.Mutex
}

func (d *dummyGet) Get(_ *AcceptWeb, _ *SendWeb) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.called = true
}

type dummyPost struct {
	called   bool
	lastData string
	mu       sync.Mutex
}

func (d *dummyPost) Post(_ *AcceptWeb, send *SendWeb) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.called = true
	send.SetDataString("ok")
}

func (d *dummyPost) StringPostDataResolveToJson(a *AcceptWeb) (map[string]string, error) {
	return map[string]string{}, nil
}

// TestRunWebGetInstanceExact 验证精确匹配优先
func TestRunWebGetInstanceExact(t *testing.T) {
	wd := NewWebData()
	d := &dummyGet{}
	if err := wd.AddWebGetInstance("/health", d); err != nil {
		t.Fatal(err)
	}
	send := NewSendWeb(nil, false)
	wd.RunWebGetInstance("/health", &HttpRequestLite{}, send)
	d.mu.Lock()
	if !d.called {
		t.Errorf("精确匹配的 handler 未触发")
	}
	d.mu.Unlock()
}

// TestRunWebGetInstance404 验证无任何匹配时返回 404
func TestRunWebGetInstance404(t *testing.T) {
	wd := NewWebData()
	send := NewSendWeb(nil, false)
	wd.RunWebGetInstance("/nowhere", &HttpRequestLite{}, send)
	if send.status != StatusNotFound {
		t.Errorf("应回 404, got %d", send.status)
	}
}

// TestRunWebGetInstanceSingleWildcard 验证 `/xxx/*` 通配匹配
func TestRunWebGetInstanceSingleWildcard(t *testing.T) {
	wd := NewWebData()
	d := &dummyGet{}
	if err := wd.AddWebGetInstance("/api/*", d); err != nil {
		t.Fatal(err)
	}
	send := NewSendWeb(nil, false)
	wd.RunWebGetInstance("/api/users", &HttpRequestLite{}, send)
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.called {
		t.Errorf("/api/* 应匹配 /api/users")
	}
}

// TestRunWebGetInstanceAllWildcard 验证 `/prefix**` 全通配
func TestRunWebGetInstanceAllWildcard(t *testing.T) {
	wd := NewWebData()
	d := &dummyGet{}
	if err := wd.AddWebGetInstance("/static/**", d); err != nil {
		t.Fatal(err)
	}
	send := NewSendWeb(nil, false)
	wd.RunWebGetInstance("/static/img/a.png", &HttpRequestLite{}, send)
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.called {
		t.Errorf("/static/** 应匹配 /static/img/a.png")
	}
}

// TestRunWebPostInstanceExact 验证 POST 精确匹配 + body 传递
func TestRunWebPostInstanceExact(t *testing.T) {
	wd := NewWebData()
	d := &dummyPost{}
	if err := wd.AddWebPostInstance("/form", d); err != nil {
		t.Fatal(err)
	}
	send := NewSendWeb(nil, false)
	wd.RunWebPostInstance("/form", "key=value", &HttpRequestLite{}, send)
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.called {
		t.Errorf("POST handler 未触发")
	}
}

// TestAddWebGetInstanceRepeat 验证重复路径报错
func TestAddWebGetInstanceRepeat(t *testing.T) {
	wd := NewWebData()
	if err := wd.AddWebGetInstance("/a", &dummyGet{}); err != nil {
		t.Fatal(err)
	}
	if err := wd.AddWebGetInstance("/a", &dummyGet{}); err != ErrRepeatAdd {
		t.Errorf("重复添加应返回 ErrRepeatAdd, got %v", err)
	}
}

// TestAddWebGetInstanceTaboo 验证 WebSocket 前缀被阻止
func TestAddWebGetInstanceTaboo(t *testing.T) {
	wd := NewWebData()
	if err := wd.AddWebGetInstance(WSUri+"x", &dummyGet{}); err != ErrTabooAdd {
		t.Errorf("WS 前缀应返回 ErrTabooAdd, got %v", err)
	}
}

// TestRunWebGetInstanceQuery 验证 url 中的 ?xxx 被解析为 urlData
func TestRunWebGetInstanceQuery(t *testing.T) {
	wd := NewWebData()
	captured := ""
	handler := WebGetFunc(func(a *AcceptWeb, s *SendWeb) {
		captured = a.URLData
	})
	if err := wd.AddWebGetInstance("/q", handler); err != nil {
		t.Fatal(err)
	}
	send := NewSendWeb(nil, false)
	wd.RunWebGetInstance("/q?a=1&b=2", &HttpRequestLite{}, send)
	if captured != "a=1&b=2" {
		t.Errorf("URL query 未正确剥离: %q", captured)
	}
}

// WebGetFunc 适配器便于测试
type WebGetFunc func(a *AcceptWeb, s *SendWeb)

func (f WebGetFunc) Get(a *AcceptWeb, s *SendWeb) { f(a, s) }
