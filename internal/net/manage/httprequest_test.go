package manage

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestDoGet 200 返回响应体, 并带 User-Agent
func TestDoGet(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("get-ok"))
	}))
	defer srv.Close()

	h := NewHttpRequestManage("avalon-test/1.0")
	if got := h.DoGet(srv.URL); got != "get-ok" {
		t.Fatalf("DoGet = %q", got)
	}
	if gotUA != "avalon-test/1.0" {
		t.Fatalf("User-Agent = %q", gotUA)
	}
}

// TestDoPost 表单编码: 参数按 A=B&C=D 解析
func TestDoPost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", ct)
		}
		_ = r.ParseForm()
		_, _ = w.Write([]byte(r.PostForm.Get("a") + "+" + r.PostForm.Get("b")))
	}))
	defer srv.Close()

	h := NewHttpRequestManage("ua")
	if got := h.DoPost(srv.URL, "a=1&b=2&=skip&malformed"); got != "1+2" {
		t.Fatalf("DoPost = %q", got)
	}
}

// TestDoPostJson JSON 体与自定义头 (服务端回显收到的头, 客户端断言)
func TestDoPostJson(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
			t.Errorf("Content-Type = %q", ct)
		}
		_, _ = w.Write([]byte(r.Header.Get("X-Extra")))
	}))
	defer srv.Close()

	h := NewHttpRequestManage("ua")
	body := `{"k":"v"}`
	if got := h.DoPostJson(srv.URL, body); got != "" {
		t.Fatalf("DoPostJson 无自定义头, 回显应为空, got %q", got)
	}
	if got := h.DoPostJsonWithHead(srv.URL, map[string]string{"X-Extra": "yes"}, body); got != "yes" {
		t.Fatalf("DoPostJsonWithHead 自定义头回显 = %q, want yes", got)
	}
}

// TestFailurePaths 失败一律空串 (对应原版 catch 后 return "")
func TestFailurePaths(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	h := NewHttpRequestManage("ua")
	if got := h.DoGet(srv.URL); got != "" {
		t.Fatalf("500 应返回空串, got %q", got)
	}
	if got := h.DoGet("://bad url"); got != "" {
		t.Fatalf("非法 URL 应返回空串, got %q", got)
	}
	// 连接拒绝 (端口 1 无人监听)
	if got := h.DoGet("http://127.0.0.1:1"); got != "" {
		t.Fatalf("连接失败应返回空串, got %q", got)
	}
}
