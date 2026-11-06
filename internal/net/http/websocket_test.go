package http

// WebSocket 升级握手与帧编解码单测 (DECISIONS D-14).
//
// 有意修正（作者已批准，2026-11-04）: 此前 "GET /WebSocket" 只回 404,
// 本组测试覆盖握手 / 文本帧往返 / 分片重组 / 控制帧 / 协议错误路径.

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- WS 测试客户端 ---

// wsHandshake 在 client 端发起升级握手, 返回响应状态行与 Sec-WebSocket-Accept
func wsHandshake(t *testing.T, client net.Conn, path, key string) string {
	t.Helper()
	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := client.Write([]byte(req)); err != nil {
		t.Fatalf("write handshake: %v", err)
	}
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	br := bufio.NewReader(client)
	statusLine, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	accept := ""
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read headers: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "sec-websocket-accept:") {
			accept = strings.TrimSpace(line[len("Sec-WebSocket-Accept:"):])
		}
	}
	t.Cleanup(func() { client.SetReadDeadline(time.Time{}) })
	_ = statusLine
	return statusLine + "|" + accept
}

// wsClientText 发送一条带掩码的客户端文本帧
func wsClientText(t *testing.T, client net.Conn, msg string) {
	t.Helper()
	wsClientFrame(t, client, opText, []byte(msg), true)
}

// wsClientFrame 发送一条带掩码的客户端帧
func wsClientFrame(t *testing.T, client net.Conn, opcode byte, payload []byte, fin bool) {
	t.Helper()
	client.SetWriteDeadline(time.Now().Add(2 * time.Second))
	defer client.SetWriteDeadline(time.Time{})

	b0 := opcode
	if fin {
		b0 |= 0x80
	}
	mask := [4]byte{0xA1, 0xB2, 0xC3, 0xD4}

	var frame []byte
	frame = append(frame, b0)
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
	masked := make([]byte, n)
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	frame = append(frame, masked...)

	if _, err := client.Write(frame); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

// wsReadServerFrame 读取一条服务端帧 (无掩码)
func wsReadServerFrame(t *testing.T, client net.Conn) (byte, string) {
	t.Helper()
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	defer client.SetReadDeadline(time.Time{})

	var hdr [2]byte
	if _, err := io.ReadFull(client, hdr[:]); err != nil {
		t.Fatalf("read frame header: %v", err)
	}
	opcode := hdr[0] & 0x0F
	if hdr[1]&0x80 != 0 {
		t.Fatal("服务端帧不应带掩码")
	}
	length := int64(hdr[1] & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(client, ext[:]); err != nil {
			t.Fatalf("read ext len: %v", err)
		}
		length = int64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(client, ext[:]); err != nil {
			t.Fatalf("read ext len: %v", err)
		}
		length = int64(binary.BigEndian.Uint64(ext[:]))
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(client, payload); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	return opcode, string(payload)
}

// recordWsHandler 记录收到的消息, 可配置回复
type recordWsHandler struct {
	mu       sync.Mutex
	messages []string
	reply    string
	closed   int
}

func (h *recordWsHandler) OnMessage(_ *StartWebSocket, conn Connection, msg string) {
	h.mu.Lock()
	h.messages = append(h.messages, msg)
	h.mu.Unlock()
	if h.reply != "" {
		_ = conn.WriteString(h.reply)
	}
}

func (h *recordWsHandler) OnClose(_ *StartWebSocket, _ Connection) {
	h.mu.Lock()
	h.closed++
	h.mu.Unlock()
}

func (h *recordWsHandler) snapshot() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.messages))
	copy(out, h.messages)
	return out
}

func (h *recordWsHandler) closeCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closed
}

// setupWs 构造带 WS 路由的 StartHttp 与管道连接
func setupWs(t *testing.T, path string, handler WebSocketHandler) (net.Conn, *recordWsHandler, chan struct{}) {
	t.Helper()
	wd := NewWebData()
	rec, _ := handler.(*recordWsHandler)
	if err := wd.AddWebSocketInstance(path, handler); err != nil {
		t.Fatal(err)
	}
	srv := NewStartHttp(nil)
	srv.SetWebData(wd)

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
	return client, rec, done
}

// expectedAccept 按 RFC 6455 计算期望的 Sec-WebSocket-Accept
func expectedAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// TestWsHandshakeAcceptKey 握手: 101 + 正确的 Sec-WebSocket-Accept
func TestWsHandshakeAcceptKey(t *testing.T) {
	client, _, _ := setupWs(t, "/WebSocket/echo", &recordWsHandler{})

	key := "dGhlIHNhbXBsZSBub25jZQ=="
	resp := wsHandshake(t, client, "/WebSocket/echo", key)

	if !strings.Contains(resp, "101") {
		t.Fatalf("应回 101: %q", resp)
	}
	if want := expectedAccept(key); !strings.Contains(resp, want) {
		t.Fatalf("Accept 键不符: %q, 期望含 %q", resp, want)
	}
}

// TestWsHandshakeRFCExample 用 RFC 6455 §1.3 的样例向量验证 Accept 计算
func TestWsHandshakeRFCExample(t *testing.T) {
	if got := wsAcceptKey("dGhlIHNhbXBsZSBub25jZQ=="); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("RFC 样例向量不符: %q", got)
	}
}

// TestWsHandshakeUnknownRoute 未注册的 WS 路径回 404
func TestWsHandshakeUnknownRoute(t *testing.T) {
	client, _, _ := setupWs(t, "/WebSocket/echo", &recordWsHandler{})

	req := "GET /WebSocket/nope HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\n" +
		"Sec-WebSocket-Key: abc\r\n\r\n"
	client.Write([]byte(req))
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 512)
	n, _ := client.Read(buf)
	if !strings.Contains(string(buf[:n]), "404") {
		t.Fatalf("未注册路由应回 404: %q", string(buf[:n]))
	}
}

// TestWsHandshakeMissingKey 缺 Sec-WebSocket-Key 回 400
func TestWsHandshakeMissingKey(t *testing.T) {
	client, _, _ := setupWs(t, "/WebSocket/echo", &recordWsHandler{})

	req := "GET /WebSocket/echo HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\n\r\n"
	client.Write([]byte(req))
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 512)
	n, _ := client.Read(buf)
	if !strings.Contains(string(buf[:n]), "400") {
		t.Fatalf("缺 Key 应回 400: %q", string(buf[:n]))
	}
}

// TestWsTextRoundTrip 文本帧往返: 客户端发 -> handler 收 -> 回复帧
func TestWsTextRoundTrip(t *testing.T) {
	client, rec, _ := setupWs(t, "/WebSocket/echo", &recordWsHandler{reply: "pong-msg"})

	wsHandshake(t, client, "/WebSocket/echo", "a2V5")
	wsClientText(t, client, "hello-ws")

	op, msg := wsReadServerFrame(t, client)
	if op != opText || msg != "pong-msg" {
		t.Fatalf("回复 = %d/%q", op, msg)
	}
	if got := rec.snapshot(); len(got) != 1 || got[0] != "hello-ws" {
		t.Fatalf("handler 收到的消息 = %v", got)
	}
}

// TestWsFragmentedMessage 分片帧重组
func TestWsFragmentedMessage(t *testing.T) {
	client, rec, _ := setupWs(t, "/WebSocket/echo", &recordWsHandler{})

	wsHandshake(t, client, "/WebSocket/echo", "a2V5")
	wsClientFrame(t, client, opText, []byte("frag-"), false)
	wsClientFrame(t, client, opContinuation, []byte("part"), true)

	deadline := time.Now().Add(2 * time.Second)
	for len(rec.snapshot()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := rec.snapshot(); len(got) != 1 || got[0] != "frag-part" {
		t.Fatalf("分片应重组为一条消息: %v", got)
	}
}

// TestWsPingPong ping 帧换 pong 帧
func TestWsPingPong(t *testing.T) {
	client, _, _ := setupWs(t, "/WebSocket/echo", &recordWsHandler{})

	wsHandshake(t, client, "/WebSocket/echo", "a2V5")
	wsClientFrame(t, client, opPing, []byte("hb"), true)

	op, msg := wsReadServerFrame(t, client)
	if op != opPong || msg != "hb" {
		t.Fatalf("ping 应换回同负载 pong: %d/%q", op, msg)
	}
}

// TestWsCloseEndsSession close 帧结束会话并回调 OnClose
func TestWsCloseEndsSession(t *testing.T) {
	client, rec, done := setupWs(t, "/WebSocket/echo", &recordWsHandler{})

	wsHandshake(t, client, "/WebSocket/echo", "a2V5")
	wsClientFrame(t, client, opClose, nil, true)

	// 服务端会回 close 应答帧; net.Pipe 无缓冲, 不读走它会阻塞服务端收尾
	if op, _ := wsReadServerFrame(t, client); op != opClose {
		t.Fatalf("close 应收到 close 应答, got %d", op)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("close 帧后服务端应结束会话")
	}
	if rec.closeCount() != 1 {
		t.Fatalf("OnClose 应回调一次, got %d", rec.closeCount())
	}
}

// TestWsUnmaskedFrameRejected 未掩码的客户端帧违反 RFC, 会话结束
func TestWsUnmaskedFrameRejected(t *testing.T) {
	client, rec, done := setupWs(t, "/WebSocket/echo", &recordWsHandler{})

	wsHandshake(t, client, "/WebSocket/echo", "a2V5")

	// 手工写一条不带掩码的帧
	frame := []byte{0x80 | opText, 0x02, 'h', 'i'}
	client.Write(frame)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("未掩码帧应终止会话")
	}
	if got := rec.snapshot(); len(got) != 0 {
		t.Fatalf("未掩码帧不应被接受: %v", got)
	}
}
