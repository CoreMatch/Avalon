// Package net (Go-Server) 对应 StartMixProtocol.read 的 readerIndex 保存/恢复
package net

import "net"

// peekedConn 把已从底层连接读出的头部字节重新放回读取流最前面.
//
// 对应 Kotlin 里 StartMixProtocol.read 的做法:
//
//	val readerIndex = firstData.readerIndex()
//	try { run(firstData) } finally { firstData.readerIndex(readerIndex) }
//
// 嗅探时为了看清首包协议, 必须把字节读出来; 而嗅探完成后选中的处理器
// (Game 解码器 / RCON 帧读取 / HTTP 请求行解析) 又要从头开始读同一批字节.
// Go 的 net.Conn 没有 readerIndex, 因此用一个只读一次的前缀缓冲把字节"塞回去".
type peekedConn struct {
	net.Conn

	head []byte
}

// newPeekedConn 包装 conn, 使其后续 Read 先吐出 head
// head 为空时直接返回原 conn, 不额外分配
func newPeekedConn(conn net.Conn, head []byte) net.Conn {
	if len(head) == 0 {
		return conn
	}
	return &peekedConn{Conn: conn, head: head}
}

// Read 先读完 head, 之后透传到底层连接
func (p *peekedConn) Read(b []byte) (int, error) {
	if len(p.head) > 0 {
		n := copy(b, p.head)
		p.head = p.head[n:]
		return n, nil
	}
	return p.Conn.Read(b)
}

// Pending 返回尚未回放的字节数 (测试与诊断用)
func (p *peekedConn) Pending() int { return len(p.head) }
