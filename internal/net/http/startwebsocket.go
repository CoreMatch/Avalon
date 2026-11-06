// Package http 对应 net.rwhps.server.net.http
package http

import "sync"

// StartWebSocket 对应 net.rwhps.server.net.handler.tcp.StartWebSocket
// 由于 Go 不使用 Netty, 这里是抽象出 WebSocket 帧接入 (wsConn) 与广播能力的轻量包装.
type StartWebSocket struct {
	handler   WebSocketHandler
	mu        sync.Mutex
	connected map[string]Connection // RemoteAddr -> Connection
}

// NewStartWebSocket 对应 StartWebSocket(webSocket)
func NewStartWebSocket(handler WebSocketHandler) *StartWebSocket {
	return &StartWebSocket{
		handler:   handler,
		connected: make(map[string]Connection),
	}
}

// AddConnection 由 StartHttp 在 WebSocket 握手成功时调用
func (s *StartWebSocket) AddConnection(addr string, conn Connection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected[addr] = conn
}

// OnMessage 由 StartHttp 在收到 WebSocket 文本帧时调用
func (s *StartWebSocket) OnMessage(conn Connection, msg string) {
	s.handler.OnMessage(s, conn, msg)
}

// OnClose 由 StartHttp 在连接关闭时调用
func (s *StartWebSocket) OnClose(conn Connection) {
	addr := connKey(conn)
	s.mu.Lock()
	delete(s.connected, addr)
	s.mu.Unlock()
	s.handler.OnClose(s, conn)
}

// BroadCast 对应 broadCast, 给所有已连接客户端群发消息
func (s *StartWebSocket) BroadCast(msg string) {
	s.mu.Lock()
	conns := make([]Connection, 0, len(s.connected))
	for _, c := range s.connected {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	for _, c := range conns {
		_ = c.WriteString(msg)
	}
}

// HasConnection 对应 hasChannel
func (s *StartWebSocket) HasConnection(conn Connection) bool {
	addr := connKey(conn)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.connected[addr]
	return ok
}

func connKey(c Connection) string {
	type addrIface interface {
		RemoteAddr() string
	}
	if a, ok := c.(addrIface); ok {
		return a.RemoteAddr()
	}
	return ""
}