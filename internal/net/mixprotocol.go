// Package net (Go-Server) 对应 net.rwhps.server.net.handler.tcp.StartMixProtocol
//
// 嗅探 TCP 首 8 字节, 将连接分派到 Game / RCON / HTTP 三个启动器之一.
// 与原 Kotlin 版一致: 首 8 字节大端 (Game) -> PREREGISTER_INFO_RECEIVE(160) 或 typeInt>1000
// 首 12 字节小端 (RCON) -> lengthLE 4..4096 && typeLE 0..3 且 !=1
// 余下视为 HTTP 请求
package net

import (
	"encoding/binary"
	"errors"
	"net"

	"rwhps/internal/data/global"
	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
	"rwhps/internal/net/core"
)

// MixKind 嗅探结果
type MixKind int

const (
	// MixUnknown 无法识别
	MixUnknown MixKind = iota
	// MixGame Game 协议
	MixGame
	// MixRcon RCON 协议
	MixRcon
	// MixHttp HTTP 协议
	MixHttp
)

// StartMixProtocol 对应 StartMixProtocol(map)
// Go 版: 不实际分派 pipeline, 只在首包解析前提供 8/12 字节嗅探
//   - 调用 Sniff(conn) 获得 MixKind 后, 取出对应 IRwHps, 创建 Channel + AbstractNet
//   - 真正的协议分发由调用方在首包到达时路由
type StartMixProtocol struct {
	game      *AbstractNet
	remote    *AbstractNet
	http      *AbstractNet
	httpProto core.IRwHps
}

// NewStartMixProtocol 对应 StartMixProtocol(map)
func NewStartMixProtocol(rwHps core.IRwHps) *StartMixProtocol {
	httpProto := GlobalCache.GetOrCreate("MixHttp", func() core.IRwHps {
		return core.NewNullRwHps(core.HttpProtocol)
	})
	remoteProto := GlobalCache.GetOrCreate("MixRemoteControl", func() core.IRwHps {
		return core.NewNullRwHps(core.RemoteControlProtocol)
	})

	return &StartMixProtocol{
		game:      NewAbstractNet(rwHps),
		remote:    NewAbstractNet(remoteProto),
		http:      NewAbstractNet(httpProto),
		httpProto: httpProto,
	}
}

// SetWebData 对应 StartHttp.setWebData
func (m *StartMixProtocol) SetWebData(_ any) { /* 占位, 由 HTTP 协议实现 */ }

// AbstractNet 对应 AbstractNet 启动器 (StartGameNetTcp/StartHttp/StartRemoteControl 通用父类)
//
// 每个协议类型持有一个 AbstractNet, 与原版一一对应:
//   StartGameNetTcp(rwHps)   -> 第一个 AbstractNet
//   StartRemoteControl(rwHps)-> 第二个
//   StartHttp(rwHps)         -> 第三个
//
// Go 版不需要 Netty pipeline, 直接复用单个 *AbstractNet 即可.
func (m *StartMixProtocol) pick(kind MixKind) *AbstractNet {
	switch kind {
	case MixGame:
		return m.game
	case MixRcon:
		return m.remote
	case MixHttp:
		return m.http
	default:
		return nil
	}
}

// HandleMixConn 对应 NetService 启动的 Accept 循环, 此处按嗅探结果分派
//
//   1. 读 12 字节 (前 4 字节足够 Game/RCON, 12 字节足够 RCON 的 3 个 int32LE)
//   2. 嗅探后创建 Channel 喂入对应 AbstractNet.HandleChannel
func (m *StartMixProtocol) HandleMixConn(conn net.Conn) error {
	peek := make([]byte, 12)
	n, err := conn.Read(peek)
	if err != nil {
		return err
	}
	peek = peek[:n]

	kind := Sniff(peek)
	target := m.pick(kind)
	if target == nil {
		// 失败: 当作 Game 处理, 原版注释 "写坨大的" 同思路
		target = m.game
		_, _ = conn.Write([]byte{})
		// 把 peek 数据写回 conn 是不可能 (net.Conn 没法 reset), 因此先把内容送入解码器
	}

	ch := core.NewChannel(conn)
	target.HandleChannelPeeked(ch, peek)
	return nil
}

// Sniff 嗅探首 8 字节, 决定是 Game / RCON / HTTP / Unknown
func Sniff(head []byte) MixKind {
	// 1. Game: [len BE][type BE] -> type > 1000 或 == PREREGISTER_INFO_RECEIVE(160)
	if len(head) >= 8 {
		length := int32(binary.BigEndian.Uint32(head[0:4]))
		typ := int32(binary.BigEndian.Uint32(head[4:8]))
		if length > 0 && typ > 0 {
			t := packettype.PacketType(typ)
			if int(t) > 1000 || t == packettype.PREREGISTER_INFO_RECEIVE {
				return MixGame
			}
		}
	}

	// 2. Rcon: [len LE][id LE][type LE] -> length 4..4096 && type 0..3 且 !=1
	if global.Config.RconMixEnable && len(head) >= 12 {
		length := int32(binary.LittleEndian.Uint32(head[0:4]))
		typ := int32(binary.LittleEndian.Uint32(head[8:12]))
		if length >= 4 && length <= 4096 && typ >= 0 && typ <= 3 && typ != 1 {
			return MixRcon
		}
	}

	// 3. HTTP: 首字节为字母, 含 "HTTP/" 或为 GET/POST/PUT/DELETE/HEAD
	if global.Config.WebMixEnable && len(head) >= 4 && IsHTTPReq(head) {
		return MixHttp
	}

	return MixUnknown
}

// IsHTTPReq 对应 StartMixProtocol.isHttpReq()
// 原 Kotlin 实现: msg.toString(UTF_8).let { isHttpReq(it) }
func IsHTTPReq(b []byte) bool {
	s := string(b)
	// 找首个空格或 '\r' '\n', 取 method 前缀
	for i, c := range s {
		if c == ' ' || c == '\r' || c == '\n' {
			method := s[:i]
			switch method {
			case "GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS", "PATCH", "TRACE", "CONNECT":
				return true
			}
			return false
		}
	}
	return false
}

// ErrProtocolNotMatch 协议嗅探失败
var ErrProtocolNotMatch = errors.New("mix protocol not match")

// CompileGuard 防止未使用导入
var _ = packet.NewEmptyPacket
