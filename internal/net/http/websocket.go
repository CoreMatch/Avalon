package http

// WebSocket 升级握手与帧编解码 (RFC 6455 的最小子集).
//
// 有意修正（作者已批准，2026-11-04）: 上游经 Netty 的 WebSocket 管线提供
// "GET /WebSocket" 升级; Go 版此前只有 404 占位 (DECISIONS D-14), 本文件把
// 升级握手与文本帧收发补齐, 让 WebData 里注册的 WebSocketHandler 可达.
//
// 范围取舍: 只实现控制台场景用到的部分 ——
//   - 握手: Sec-WebSocket-Key -> Sec-WebSocket-Accept (SHA1 + 魔法 GUID)
//   - 数据帧: text/binary 与分片重组 (客户端帧必须带掩码, RFC 6455 §5.3)
//   - 控制帧: ping -> 回 pong, close -> 回 close 并结束会话, pong 忽略
//   - 不做扩展协商 (compression 等), 上游控制台同样不用

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"sync"
)

// wsGUID 是 RFC 6455 §4.2.2 规定的握手魔法常量
const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// WebSocket 操作码
const (
	opContinuation byte = 0x0
	opText         byte = 0x1
	opBinary       byte = 0x2
	opClose        byte = 0x8
	opPing         byte = 0x9
	opPong         byte = 0xA
)

// errWsProtocol 客户端帧违反 RFC (未掩码 / 分片错位等)
var errWsProtocol = errors.New("http: websocket protocol error")

// wsAcceptKey 对应 Sec-WebSocket-Accept 的计算 (RFC 6455 §4.2.2 第 4 步)
func wsAcceptKey(key string) string {
	h := sha1.New()
	h.Write([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// handleWebSocket 完成升级握手并接管连接, 直到会话结束 (close 帧 / 读错误).
//
// 返回后连接即作废: 调用方 (HandleConn) 应直接结束请求循环.
func (s *StartHttp) handleWebSocket(conn ConnIO, reader *bufio.Reader, req *HttpRequestLite) {
	path, _ := splitURL(req.URL)
	ws := s.webData.RunWebSocketInstance(path)
	if ws == nil {
		writeNotFound(conn, req, false)
		return
	}

	// 握手校验 (RFC 6455 §4.2.1): Upgrade 头 + Sec-WebSocket-Key 缺一不可
	if !strings.Contains(strings.ToLower(req.GetHeader("Upgrade")), "websocket") {
		sendBadRequest(conn, req, false)
		return
	}
	key := req.GetHeader("Sec-WebSocket-Key")
	if key == "" {
		sendBadRequest(conn, req, false)
		return
	}

	// 101 Switching Protocols
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + wsAcceptKey(key) + "\r\n\r\n"
	if _, err := conn.Write([]byte(resp)); err != nil {
		return
	}

	wc := newWsConn(conn)
	ws.AddConnection(wc.RemoteAddr(), wc)
	defer ws.OnClose(wc)

	// 帧循环: 数据帧重组分片后交给 handler, 控制帧就地处理
	var msgOpcode byte
	var msgBuf []byte
	for {
		fin, op, data, err := readWsFrame(reader)
		if err != nil {
			// EOF / 协议错误 / 读失败都结束会话; 连接由收尾路径关闭
			return
		}

		switch op {
		case opText, opBinary:
			if msgOpcode != 0 {
				return // 上一条消息未结束就来新消息, 违反 RFC
			}
			msgOpcode = op
			msgBuf = append(msgBuf[:0], data...)
		case opContinuation:
			if msgOpcode == 0 {
				return // 没有起始帧的分片, 违反 RFC
			}
			msgBuf = append(msgBuf, data...)
		case opPing:
			if err := wc.writeFrame(opPong, data); err != nil {
				return
			}
			continue
		case opPong:
			continue
		case opClose:
			_ = wc.writeFrame(opClose, nil)
			return
		default:
			return // 未知操作码
		}

		if fin && msgOpcode != 0 {
			ws.OnMessage(wc, string(msgBuf))
			msgOpcode = 0
			msgBuf = msgBuf[:0]
		}
	}
}

// readWsFrame 读取一条客户端帧并解除掩码
//
// 客户端到服务端的帧按 RFC 必须带掩码; 未带掩码按协议错误处理.
func readWsFrame(r *bufio.Reader) (fin bool, opcode byte, payload []byte, err error) {
	var hdr [2]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return
	}
	fin = hdr[0]&0x80 != 0
	opcode = hdr[0] & 0x0F
	masked := hdr[1]&0x80 != 0
	length := int64(hdr[1] & 0x7F)

	switch length {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return
		}
		length = int64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return
		}
		if ext[0]&0x80 != 0 {
			err = errWsProtocol
			return
		}
		length = int64(binary.BigEndian.Uint64(ext[:]))
	}

	if !masked {
		err = errWsProtocol
		return
	}
	var maskKey [4]byte
	if _, err = io.ReadFull(r, maskKey[:]); err != nil {
		return
	}

	payload = make([]byte, length)
	if _, err = io.ReadFull(r, payload); err != nil {
		return
	}
	for i := range payload {
		payload[i] ^= maskKey[i%4]
	}
	return
}

// wsConn 是升级成功后的连接包装, 实现 Connection (WriteString/Close).
//
// 写路径会被多个 goroutine 共用 (帧循环的应答 + 控制台转发的推送),
// 用 writeMu 串行化, 保证一帧一帧完整写出.
type wsConn struct {
	conn    ConnIO
	writeMu sync.Mutex
	closed  bool
}

func newWsConn(conn ConnIO) *wsConn { return &wsConn{conn: conn} }

// WriteString 实现 Connection: 以文本帧发出一条消息
func (c *wsConn) WriteString(msg string) error {
	return c.writeFrame(opText, []byte(msg))
}

// Close 实现 Connection
func (c *wsConn) Close() error {
	c.writeMu.Lock()
	c.closed = true
	c.writeMu.Unlock()
	return c.conn.Close()
}

// RemoteAddr 供 StartWebSocket 作连接键 (尽力取对端地址)
func (c *wsConn) RemoteAddr() string {
	if a, ok := c.conn.(addrConn); ok {
		if addr := a.RemoteAddr(); addr != nil {
			return addr.String()
		}
	}
	return ""
}

// writeFrame 写出一条服务端帧 (不带掩码, RFC 6455 §5.1)
func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed {
		return errWsClosed
	}

	frame := make([]byte, 0, len(payload)+10)
	frame = append(frame, 0x80|opcode) // FIN = 1
	n := len(payload)
	switch {
	case n < 126:
		frame = append(frame, byte(n))
	case n <= 0xFFFF:
		frame = append(frame, 126, byte(n>>8), byte(n))
	default:
		frame = append(frame, 127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		frame = append(frame, ext[:]...)
	}
	frame = append(frame, payload...)

	_, err := c.conn.Write(frame)
	return err
}

var errWsClosed = errors.New("http: websocket connection closed")
