package net

import (
	"net"
	"testing"
)

// TestPeekedConnReplaysHead 验证嗅探读出的字节会被回放给后续处理器
func TestPeekedConnReplaysHead(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		_, _ = server.Write([]byte("BODY"))
	}()

	conn := newPeekedConn(client, []byte("HEAD"))
	pc, ok := conn.(*peekedConn)
	if !ok {
		t.Fatalf("newPeekedConn 未返回 *peekedConn: %T", conn)
	}
	if got := pc.Pending(); got != 4 {
		t.Fatalf("Pending() = %d, want 4", got)
	}

	buf := make([]byte, 16)
	n, err := pc.Read(buf)
	if err != nil {
		t.Fatalf("第一次 Read 失败: %v", err)
	}
	if string(buf[:n]) != "HEAD" {
		t.Fatalf("回放内容 = %q, want %q", buf[:n], "HEAD")
	}

	// head 耗尽后应透传到底层连接
	n, err = pc.Read(buf)
	if err != nil {
		t.Fatalf("第二次 Read 失败: %v", err)
	}
	if string(buf[:n]) != "BODY" {
		t.Fatalf("透传内容 = %q, want %q", buf[:n], "BODY")
	}
	if got := pc.Pending(); got != 0 {
		t.Fatalf("Pending() = %d, want 0", got)
	}
}

// TestPeekedConnPartialRead 验证 head 大于读缓冲时分多次吐出, 不丢字节
func TestPeekedConnPartialRead(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	pc := newPeekedConn(client, []byte("0123456789")).(*peekedConn)

	var got []byte
	buf := make([]byte, 3)
	for len(got) < 10 {
		n, err := pc.Read(buf)
		if err != nil {
			t.Fatalf("Read 失败: %v", err)
		}
		got = append(got, buf[:n]...)
	}
	if string(got) != "0123456789" {
		t.Fatalf("got %q", got)
	}
}

// TestNewPeekedConnEmptyHead 空 head 不应包装, 直接返回原连接
func TestNewPeekedConnEmptyHead(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	if got := newPeekedConn(client, nil); got != net.Conn(client) {
		t.Fatalf("空 head 应返回原连接, got %T", got)
	}
}

// TestPeekedConnIsNetConn 确保包装后仍满足 net.Conn (LocalAddr/Close 等透传)
func TestPeekedConnIsNetConn(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()

	var conn net.Conn = newPeekedConn(client, []byte("x"))
	if conn.LocalAddr() == nil {
		t.Fatal("LocalAddr 不应为 nil")
	}

	// 先吐掉 head, 之后才真正碰到底层连接
	if n, err := conn.Read(make([]byte, 1)); n != 1 || err != nil {
		t.Fatalf("回放 head 失败: n=%d err=%v", n, err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("关闭后 Read 应返回错误")
	}
}
