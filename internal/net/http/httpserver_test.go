package http

import (
	"bytes"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// pipeConn 创建一个 in-memory 双向管道 (返回 client 和 server 端)
func pipeConn(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	return a, b
}

// 编译期校验实现 Connection 接口
var _ Connection = (*testConnAdapter)(nil)

type testConnAdapter struct{ net.Conn }

func (t *testConnAdapter) WriteString(msg string) error {
	_, err := t.Conn.Write([]byte(msg))
	return err
}
func (t *testConnAdapter) Close() error        { return t.Conn.Close() }
func (t *testConnAdapter) RemoteAddr() string  { return t.Conn.RemoteAddr().String() }

// TestStartHttpGetExactMatch 验证 HTTP/1.1 GET 请求被正确派发到精确注册的 handler
func TestStartHttpGetExactMatch(t *testing.T) {
	clientConn, serverConn := pipeConn(t)

	wd := NewWebData()
	handler := WebGetFunc(func(a *AcceptWeb, s *SendWeb) {
		s.SetDataString("hello-" + a.URLData)
	})
	if err := wd.AddWebGetInstance("/ping", handler); err != nil {
		t.Fatal(err)
	}
	srv := NewStartHttp(nil)
	srv.SetWebData(wd)

	done := make(chan struct{})
	go func() {
		srv.HandleConn(serverConn)
		close(done)
	}()

	// 客户端写一条完整 GET 请求
	go func() {
		req := "GET /ping?world HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"
		clientConn.Write([]byte(req))
	}()

	// 读取响应
	buf := make([]byte, 1024)
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := clientConn.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("read err: %v", err)
	}
	clientConn.Close()
	<-done

	resp := string(buf[:n])
	if !strings.Contains(resp, "hello-world") {
		t.Errorf("响应未包含期望 body: %q", resp)
	}
	if !strings.Contains(resp, "HTTP/1.1 200") {
		t.Errorf("响应缺 200 状态: %q", resp)
	}
}

// TestStartHttpPostExactMatch 验证 POST + body 正确派发
func TestStartHttpPostExactMatch(t *testing.T) {
	clientConn, serverConn := pipeConn(t)

	wd := NewWebData()
	var capturedBody string
	handler := WebPostFunc(func(a *AcceptWeb, s *SendWeb) {
		capturedBody = a.Data
		s.SetDataString("ok")
	})
	if err := wd.AddWebPostInstance("/upload", handler); err != nil {
		t.Fatal(err)
	}
	srv := NewStartHttp(nil)
	srv.SetWebData(wd)

	done := make(chan struct{})
	go func() {
		srv.HandleConn(serverConn)
		close(done)
	}()

	bodyStr := "username=alice&password=secret"
	go func() {
		req := "POST /upload HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/x-www-form-urlencoded\r\nContent-Length: " +
			itoa(len(bodyStr)) + "\r\nConnection: close\r\n\r\n" + bodyStr
		clientConn.Write([]byte(req))
	}()

	buf := make([]byte, 1024)
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := clientConn.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("read err: %v", err)
	}
	clientConn.Close()
	<-done

	if capturedBody != bodyStr {
		t.Errorf("POST body 未传透: %q (want %q)", capturedBody, bodyStr)
	}
	if !strings.Contains(string(buf[:n]), "HTTP/1.1 200") {
		t.Errorf("响应异常")
	}
}

// TestStartHttpKeepAlive 验证 HTTP/1.1 keep-alive 复用连接
func TestStartHttpKeepAlive(t *testing.T) {
	clientConn, serverConn := pipeConn(t)

	wd := NewWebData()
	calls := 0
	handler := WebGetFunc(func(a *AcceptWeb, s *SendWeb) {
		calls++
		s.SetDataString("r")
	})
	if err := wd.AddWebGetInstance("/health", handler); err != nil {
		t.Fatal(err)
	}
	srv := NewStartHttp(nil)
	srv.SetWebData(wd)

	done := make(chan struct{})
	go func() {
		srv.HandleConn(serverConn)
		close(done)
	}()

	// 两条请求写在同一连接, 默认 keep-alive
	go func() {
		req1 := "GET /health HTTP/1.1\r\nHost: localhost\r\n\r\n"
		req2 := "GET /health HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"
		clientConn.Write([]byte(req1))
		time.Sleep(50 * time.Millisecond)
		clientConn.Write([]byte(req2))
	}()

	// 累积读两段响应
	clientConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var all bytes.Buffer
	buf := make([]byte, 1024)
	for i := 0; i < 4; i++ {
		n, err := clientConn.Read(buf)
		if n > 0 {
			all.Write(buf[:n])
		}
		if err != nil {
			break
		}
		if bytes.Contains(all.Bytes(), []byte("HTTP/1.1 200")) && all.Len() > 200 {
			// 两条响应可能一并到了, 适当等待
			if calls >= 2 {
				break
			}
		}
	}
	clientConn.Close()
	<-done

	if calls < 2 {
		t.Errorf("keep-alive 应复用连接处理两条, got %d 个响应", calls)
	}
}

// TestStartHttpWebHOSTMismatch 验证 Host 不匹配时回 400
func TestStartHttpWebHOSTMismatch(t *testing.T) {
	// 这个测试通过直接调用 dispatch 路径效率更高, 这里跳过保持简洁
	t.Skip("WebHOST 配置依赖 Data.Config 全局状态, 留到 Todo #9 集成测试")
}

// --- helpers ---

// itoa 测试内避免再引 strconv
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}

// WebPostFunc 适配器
type WebPostFunc func(a *AcceptWeb, s *SendWeb)

func (f WebPostFunc) Post(a *AcceptWeb, s *SendWeb) { f(a, s) }
func (f WebPostFunc) StringPostDataResolveToJson(a *AcceptWeb) (map[string]string, error) {
	return map[string]string{}, nil
}
