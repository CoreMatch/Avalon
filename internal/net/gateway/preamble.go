// Package gateway 实现 Cascade 边缘代理的后端契约 (Cascade-Backend-v1).
//
// 有意修正（作者已批准，2026-11-04）: Go Server 作为 Cascade 的后端
// (方案 B 单房间合规体, DECISIONS D-17~D-21). 契约权威文本在 Cascade 仓库
// HA-Contract/Cascade-Backend-v1.md; 本包实现其中的后端侧:
//
//   - 游戏端口 mTLS + 24 字节 CASC 前导 (preamble.go)
//   - 会话状态机 PENDING/BOUND/CLOSED 与绑定前门控 (session.go)
//   - Ed25519 JWT 身份校验, jti 一次性消费且持久化 (jwt.go / jtistore.go)
//   - /v1 控制面: health / rooms / sessions identity / 房间门面 (control.go)
//
// 关闭 (global.Config.CascadeEnable=false) 时本包不参与任何链路,
// 明文 TCP 直连行为与既有完全一致.
package gateway

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// 契约 "Game TCP preamble" (Cascade-Backend-v1.md :23-38):
// TLS 握手后, 代理在任何 RW 字节之前写入恰好 24 字节:
//
//	偏移 0  4B  ASCII magic "CASC"
//	偏移 4  2B  协议版本 = 1 (大端)
//	偏移 6  2B  头长 = 24 (大端)
//	偏移 8  16B UUIDv4 会话 ID
//
// 后端对未知 magic/版本/头长一律拒绝并断开.
const (
	// PreambleSize 前导固定长度
	PreambleSize = 24
	// PreambleVersion 当前唯一版本
	PreambleVersion = 1
)

// preambleMagic 契约固定 "CASC" (改名前旧版为 "MCAG", 见 Cascade 升级说明)
var preambleMagic = [4]byte{'C', 'A', 'S', 'C'}

// 前导错误: 契约要求"拒绝并关闭连接", 调用方记录后断开即可
var (
	// ErrPreambleMagic magic 不是 CASC
	ErrPreambleMagic = errors.New("gateway: invalid preamble magic")
	// ErrPreambleVersion 版本不受支持
	ErrPreambleVersion = errors.New("gateway: unsupported preamble version")
	// ErrPreambleSize 头长字段不是 24
	ErrPreambleSize = errors.New("gateway: invalid preamble size")
	// ErrSessionIDNotV4 会话 ID 不是 UUIDv4
	ErrSessionIDNotV4 = errors.New("gateway: session ID is not UUIDv4")
)

// ReadPreamble 从连接读取并校验 24 字节前导, 返回会话 ID 的标准
// 36 字符 UUID 文本形式 (8-4-4-4-12 小写十六进制).
//
// 与 Cascade internal/backend/transport.go 的 EncodePreamble/ReadPreamble
// 逐字段对齐 (同一契约的两端实现).
func ReadPreamble(r io.Reader) (string, error) {
	header := make([]byte, PreambleSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return "", err
	}
	return ParsePreamble(header)
}

// ParsePreamble 校验一段恰好 24 字节的前导
func ParsePreamble(header []byte) (string, error) {
	if len(header) != PreambleSize {
		return "", fmt.Errorf("%w: got %d bytes", ErrPreambleSize, len(header))
	}
	if header[0] != preambleMagic[0] || header[1] != preambleMagic[1] ||
		header[2] != preambleMagic[2] || header[3] != preambleMagic[3] {
		return "", ErrPreambleMagic
	}
	if v := binary.BigEndian.Uint16(header[4:6]); v != PreambleVersion {
		return "", fmt.Errorf("%w: %d", ErrPreambleVersion, v)
	}
	if s := binary.BigEndian.Uint16(header[6:8]); s != PreambleSize {
		return "", fmt.Errorf("%w: %d", ErrPreambleSize, s)
	}
	b := header[8:]
	// UUIDv4: 版本半字节 = 4, 变体高位 = 10
	if b[6]&0xf0 != 0x40 || b[8]&0xc0 != 0x80 {
		return "", ErrSessionIDNotV4
	}
	return formatUUID(b), nil
}

// formatUUID 16 字节 -> 8-4-4-4-12 文本
func formatUUID(b []byte) string {
	const hexDigits = "0123456789abcdef"
	out := make([]byte, 36)
	j := 0
	for i := 0; i < 16; i++ {
		switch i {
		case 4, 6, 8, 10:
			out[j] = '-'
			j++
		}
		out[j] = hexDigits[b[i]>>4]
		out[j+1] = hexDigits[b[i]&0x0f]
		j += 2
	}
	return string(out)
}
