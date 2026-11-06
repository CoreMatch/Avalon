package gateway

import (
	"crypto/tls"
	"io"
	"net"
	"testing"
	"time"

	"rwhps/internal/io/packettype"
	"rwhps/internal/net/core"
	"rwhps/internal/util/file/plugindata"
)

// startGameListener 起一个带网关的明文监听器, 服务端协程对每条连接跑
// AcceptGame (TLS+前导+会话), 返回监听地址与会话通道.
func startGameListener(t *testing.T, gw *Gateway) (addr string, accepted chan *core.Channel) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	accepted = make(chan *core.Channel, 4)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			ch := core.NewChannel(conn)
			if gw.AcceptGame(ch) {
				accepted <- ch
			} else {
				accepted <- nil
			}
		}
	}()
	return ln.Addr().String(), accepted
}

// mustJtiStore 在临时目录开一个 jti 账本
func mustJtiStore(t *testing.T) *plugindata.Settings {
	t.Helper()
	store, err := plugindata.Open(jtiFile(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// TestAcceptGameHandshakeAndGate 完整握手: TLS + CASC 前导 -> PENDING 会话,
// 门控只放行 Preregister; 绑定后放行全部.
func TestAcceptGameHandshakeAndGate(t *testing.T) {
	certPath, keyPath, _, clientCert, _ := tlsFixture(t)

	gw, err := New(Config{
		BackendID:   "backend-t",
		RoomCode:    "GW01",
		TLSCert:     certPath,
		TLSKey:      keyPath,
		ControlAddr: "127.0.0.1:0",
	}, NewJtiStore(mustJtiStore(t)))
	if err != nil {
		t.Fatalf("gateway.New: %v", err)
	}

	addr, accepted := startGameListener(t, gw)

	conn, err := tls.Dial("tcp", addr, &tls.Config{
		Certificates:     []tls.Certificate{clientCert},
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// 写前导 (会话 ID 为合法 UUIDv4)
	sessionID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	if err := writePreambleForTest(conn, sessionID); err != nil {
		t.Fatalf("write preamble: %v", err)
	}
	// 前导后跟一个 RW 帧 (模拟代理转发首包)
	if _, err := conn.Write([]byte{0, 0, 0, 1, 0, 0, 0, 160, 0x42}); err != nil {
		t.Fatal(err)
	}

	var ch *core.Channel
	select {
	case ch = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("AcceptGame 未在时限内完成")
	}
	if ch == nil {
		t.Fatal("AcceptGame 拒绝了合法握手")
	}
	if ch.Gate == nil {
		t.Fatal("握手后应挂上门控")
	}

	// 门控: PENDING 只放行 160
	if !ch.Gate.AllowPacket(int(packettype.PREREGISTER_INFO_RECEIVE)) {
		t.Fatal("PENDING 应放行 Preregister")
	}
	if ch.Gate.AllowPacket(int(packettype.REGISTER_PLAYER)) {
		t.Fatal("PENDING 不得放行 RegisterPlayer")
	}

	// 会话已登记
	sess := gw.Registry().Get(sessionID)
	if sess == nil || sess.State() != StatePending {
		t.Fatalf("会话应为 PENDING: %v", sess)
	}

	// 绑定后门控放行
	if err := gw.Registry().Bind(sessionID, "player-1", "jti-1"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if !ch.Gate.AllowPacket(int(packettype.REGISTER_PLAYER)) {
		t.Fatal("BOUND 应放行 RegisterPlayer")
	}

	// 服务端能从 TLS 连接读到客户端写的帧
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	head := make([]byte, 9)
	if _, err := io.ReadFull(ch.Conn, head); err != nil {
		t.Fatalf("读帧: %v", err)
	}
	if head[8] != 0x42 {
		t.Fatalf("帧负载 = %x", head[8])
	}
}

// TestAcceptGameRejectsBadPreamble 前导非法: 断开且不登记会话
func TestAcceptGameRejectsBadPreamble(t *testing.T) {
	certPath, keyPath, _, clientCert, _ := tlsFixture(t)

	gw, err := New(Config{
		BackendID:   "backend-t",
		RoomCode:    "GW01",
		TLSCert:     certPath,
		TLSKey:      keyPath,
		ControlAddr: "127.0.0.1:0",
	}, NewJtiStore(mustJtiStore(t)))
	if err != nil {
		t.Fatal(err)
	}
	addr, accepted := startGameListener(t, gw)

	conn, err := tls.Dial("tcp", addr, &tls.Config{
		Certificates:       []tls.Certificate{clientCert},
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// 错误 magic
	bad := make([]byte, 24)
	copy(bad, "XXXX")
	bad[4], bad[6] = 0, 24
	if _, err := conn.Write(bad); err != nil {
		t.Fatal(err)
	}

	select {
	case ch := <-accepted:
		if ch != nil {
			t.Fatal("非法前导应被拒绝")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AcceptGame 未在时限内返回")
	}
	if gw.Registry().Size() != 0 {
		t.Fatal("非法前导不应登记会话")
	}
}

// writePreambleForTest 按契约写 24 字节前导
func writePreambleForTest(w io.Writer, sessionID string) error {
	header := make([]byte, 0, 24)
	header = append(header, 'C', 'A', 'S', 'C')
	header = append(header, 0, 1) // version 1
	header = append(header, 0, 24)
	compact := ""
	for _, c := range sessionID {
		if c != '-' {
			compact += string(c)
		}
	}
	for i := 0; i < len(compact); i += 2 {
		var b byte
		for j := 0; j < 2; j++ {
			c := compact[i+j]
			var d byte
			switch {
			case c >= '0' && c <= '9':
				d = c - '0'
			case c >= 'a' && c <= 'f':
				d = c - 'a' + 10
			}
			b = b<<4 | d
		}
		header = append(header, b)
	}
	_, err := w.Write(header)
	return err
}
