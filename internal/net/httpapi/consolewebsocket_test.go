package httpapi

// ConsoleWebSocket 集成测试 (DECISIONS D-14).
//
// 有意修正（作者已批准，2026-11-04）: 起真实 HTTP 服务 (in-memory 管道) ->
// WS 升级握手 -> 收一条控制台消息 -> 发一条命令, 全链路走通.

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"rwhps/internal/data/global"
	nethttp "rwhps/internal/net/http"
	"rwhps/internal/util/log"
	cmd "rwhps/internal/util/game/command"
)

// --- WS 客户端最小工具 (掩码帧, RFC 6455) ---

func wsWriteMasked(t *testing.T, conn net.Conn, opcode byte, payload []byte) {
	t.Helper()
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	defer conn.SetWriteDeadline(time.Time{})

	mask := [4]byte{0x11, 0x22, 0x33, 0x44}
	frame := []byte{0x80 | opcode}
	n := len(payload)
	switch {
	case n < 126:
		frame = append(frame, 0x80|byte(n))
	case n <= 0xFFFF:
		frame = append(frame, 0x80|126, byte(n>>8), byte(n))
	default:
		frame = append(frame, 0x80|127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		frame = append(frame, ext[:]...)
	}
	frame = append(frame, mask[:]...)
	for i := range payload {
		frame = append(frame, payload[i]^mask[i%4])
	}
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("write ws frame: %v", err)
	}
}

func wsReadText(t *testing.T, br *bufio.Reader) string {
	t.Helper()
	var hdr [2]byte
	if _, err := io.ReadFull(br, hdr[:]); err != nil {
		t.Fatalf("read ws header: %v", err)
	}
	opcode := hdr[0] & 0x0F
	if opcode != 0x1 {
		t.Fatalf("期望文本帧, got opcode %d", opcode)
	}
	length := int64(hdr[1] & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(br, ext[:]); err != nil {
			t.Fatalf("read ext: %v", err)
		}
		length = int64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(br, ext[:]); err != nil {
			t.Fatalf("read ext: %v", err)
		}
		length = int64(binary.BigEndian.Uint64(ext[:]))
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(br, payload); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	return string(payload)
}

// readHttpResponse 读一条 HTTP 响应, 返回头 (小写键) 与体
func readHttpResponse(t *testing.T, br *bufio.Reader) (map[string]string, string) {
	t.Helper()
	if _, err := br.ReadString('\n'); err != nil { // 状态行
		t.Fatalf("read status: %v", err)
	}
	headers := make(map[string]string)
	contentLength := 0
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read headers: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if idx := strings.IndexByte(line, ':'); idx > 0 {
			k := strings.ToLower(strings.TrimSpace(line[:idx]))
			v := strings.TrimSpace(line[idx+1:])
			headers[k] = v
			if k == "content-length" {
				for _, c := range v {
					contentLength = contentLength*10 + int(c-'0')
				}
			}
		}
	}
	body := make([]byte, contentLength)
	if contentLength > 0 {
		if _, err := io.ReadFull(br, body); err != nil {
			t.Fatalf("read body: %v", err)
		}
	}
	return headers, string(body)
}

// TestConsoleWebSocketIntegration 全链路: 起 HTTP 服务 -> WS 握手 ->
// 收一条控制台消息 -> 发一条命令.
func TestConsoleWebSocketIntegration(t *testing.T) {
	// --- 全局状态装配与收尾 ---
	oldToken, oldCmd, oldHook := global.Config.WebToken, global.ServerCommand, log.ConsoleHook
	global.Config.WebToken = "ws-test-token"

	handler := cmd.NewHandler("")
	handler.Register("wstest", "#ws integration test", func(_ []string, param any) {
		if print, ok := param.(func(string)); ok {
			print("ws-cmd-ok")
		}
	})
	global.ServerCommand = handler

	log.SetOutput(io.Discard) // 控制台噪声不进测试输出
	t.Cleanup(func() {
		global.Config.WebToken = oldToken
		global.ServerCommand = oldCmd
		log.ConsoleHook = oldHook
		log.SetOutput(nil)
	})

	// --- 起 HTTP 服务 (in-memory 管道) ---
	webData := nethttp.NewWebData()
	Register(webData)

	srv := nethttp.NewStartHttp(nil)
	srv.SetWebData(webData)

	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		srv.HandleConn(server)
		close(done)
	}()
	t.Cleanup(func() {
		client.Close()
		<-done
	})
	br := bufio.NewReader(client)
	client.SetReadDeadline(time.Now().Add(3 * time.Second))
	defer client.SetReadDeadline(time.Time{})

	// --- 1. AuthCookie 换取 cookie ---
	if _, err := client.Write([]byte(
		"GET /HttpApi/api/AuthCookie?passwd=ws-test-token HTTP/1.1\r\n" +
			"Host: localhost\r\n\r\n")); err != nil {
		t.Fatalf("write auth req: %v", err)
	}
	headers, _ := readHttpResponse(t, br)
	setCookie := headers["set-cookie"]
	if setCookie == "" {
		t.Fatal("AuthCookie 应下发 Set-Cookie")
	}
	// "HttpApi-Authentication=<value>; Max-Age=..."
	cookieVal := ""
	if i := strings.IndexByte(setCookie, '='); i >= 0 {
		rest := setCookie[i+1:]
		if j := strings.IndexByte(rest, ';'); j >= 0 {
			cookieVal = rest[:j]
		}
	}
	if cookieVal == "" {
		t.Fatalf("无法解析 cookie: %q", setCookie)
	}

	// --- 2. WS 升级握手 ---
	if _, err := client.Write([]byte(
		"GET /WebSocket/HttpApi/api/Console HTTP/1.1\r\n" +
			"Host: localhost\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Key: a2V5\r\n" +
			"Sec-WebSocket-Version: 13\r\n\r\n")); err != nil {
		t.Fatalf("write upgrade: %v", err)
	}
	statusLine, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read upgrade status: %v", err)
	}
	if !strings.Contains(statusLine, "101") {
		t.Fatalf("升级应回 101: %q", statusLine)
	}
	for { // 读完升级头
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read upgrade headers: %v", err)
		}
		if strings.TrimRight(line, "\r\n") == "" {
			break
		}
	}

	// --- 3. 鉴权 + getConsole: 收到缓存 (控制台消息链路的入口) ---
	wsWriteMasked(t, client, 0x1, []byte(`{"cookie":"`+cookieVal+`","type":"getConsole"}`))
	first := wsReadText(t, br)
	if !strings.Contains(first, `"code":200`) {
		t.Fatalf("getConsole 应回 code 200: %q", first)
	}

	// --- 4. 收一条控制台消息: 日志输出经 ConsoleHook 实时推送 ---
	// 生产里控制台输出与 WS 客户端读取不在同一线程; 测试里同样拆开,
	// 否则 net.Pipe 的同步写会与读取互相等待.
	go log.Clog("ws-console-line")
	pushed := wsReadText(t, br)
	if !strings.Contains(pushed, `"code":1000`) || !strings.Contains(pushed, "ws-console-line") {
		t.Fatalf("控制台输出应经 WS 推送: %q", pushed)
	}

	// --- 5. 发一条命令: runCommand 执行服务端命令, 输出回流到控制台代理 ---
	wsWriteMasked(t, client, 0x1, []byte(`{"type":"runCommand","runCommand":"wstest"}`))
	cmdOut := wsReadText(t, br)
	if !strings.Contains(cmdOut, "ws-cmd-ok") {
		t.Fatalf("命令输出应回流到控制台代理: %q", cmdOut)
	}
}

// TestConsoleWebSocketRejectsBadCookie 过期/缺失 cookie 回 401
func TestConsoleWebSocketRejectsBadCookie(t *testing.T) {
	oldToken, oldHook := global.Config.WebToken, log.ConsoleHook
	global.Config.WebToken = "ws-test-token"
	log.SetOutput(io.Discard)
	t.Cleanup(func() {
		global.Config.WebToken = oldToken
		log.ConsoleHook = oldHook
		log.SetOutput(nil)
	})

	webData := nethttp.NewWebData()
	Register(webData)
	srv := nethttp.NewStartHttp(nil)
	srv.SetWebData(webData)

	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		srv.HandleConn(server)
		close(done)
	}()
	t.Cleanup(func() {
		client.Close()
		<-done
	})
	br := bufio.NewReader(client)
	client.SetReadDeadline(time.Now().Add(3 * time.Second))
	defer client.SetReadDeadline(time.Time{})

	if _, err := client.Write([]byte(
		"GET /WebSocket/HttpApi/api/Console HTTP/1.1\r\n" +
			"Host: localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
			"Sec-WebSocket-Key: a2V5\r\n\r\n")); err != nil {
		t.Fatalf("write upgrade: %v", err)
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read headers: %v", err)
		}
		if strings.TrimRight(line, "\r\n") == "" {
			break
		}
	}

	wsWriteMasked(t, client, 0x1, []byte(`{"cookie":"not-exists","type":"register"}`))
	resp := wsReadText(t, br)
	if !strings.Contains(resp, `"code":401`) {
		t.Fatalf("非法 cookie 应回 401: %q", resp)
	}
}
