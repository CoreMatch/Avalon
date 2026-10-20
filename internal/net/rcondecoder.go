package net

import (
	"encoding/binary"
	"errors"
	"io"

	gameio "rwhps/internal/io"
	"rwhps/internal/io/packet"
)

// RCON 线协议格式 (全小端 LE):
//   [4 length LE][4 id LE][4 type LE][body UTF-8][4 bytes 终止符 (writeIntLE(0))]
// length = id(4) + type(4) + body + terminator(4) = body.length + 12
//
// Kotlin Decoder 校验 length ∈ [4, 4096] (宽松区间, 最小 4), body 读取采用
// readBytes(readableBytes - 2) + readBytes(2) — 即把后 2 字节当终止符吃掉.
// 但实际编码写入 4 字节 0 作为终止符, 这里按 Kotlin 编码语义对齐:
//   读 length 后, 跟读 length 字节, 其中前 (length-12) 字节是 body, 后 4 字节是终止符.

// ErrRconLengthOutOfRange length 不在 [4, 4096]
var ErrRconLengthOutOfRange = errors.New("rcon: length out of range")

// RconReadPacket 从 io.Reader 读取并解析一个 RCON 包.
// 完整成功返回 *packet.RconPacket + nil; 不完整返回 nil, io.ErrUnexpectedEOF 或 io.EOF.
func RconReadPacket(r io.Reader) (*packet.RconPacket, error) {
	var head [8]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return nil, err
	}
	length := int32(binary.LittleEndian.Uint32(head[0:4]))
	if length < 4 || length > 4096 {
		return nil, ErrRconLengthOutOfRange
	}

	bodyAndTerm := int(length) - 8 // length = id(4) + type(4) + body + terminator(4)
	bodyLen := bodyAndTerm - 4
	if bodyLen < 0 {
		bodyLen = 0
	}

	idBuf := make([]byte, 4)
	typeBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, idBuf); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(r, typeBuf); err != nil {
		return nil, err
	}
	bodyBuf := make([]byte, bodyLen)
	if _, err := io.ReadFull(r, bodyBuf); err != nil {
		return nil, err
	}
	termBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, termBuf); err != nil {
		return nil, err
	}

	return &packet.RconPacket{
		ID:   int(int32(binary.LittleEndian.Uint32(idBuf))),
		Type: int(int32(binary.LittleEndian.Uint32(typeBuf))),
		Body: string(bodyBuf),
	}, nil
}

// RconWritePacket 编码一个 RCON 包为线格式 (全小端 LE).
// 对应 RemoteControlPacketEncoder.encode: length = body.length + 12.
func RconWritePacket(p *packet.RconPacket) []byte {
	body := []byte(p.Body)
	bodyLen := len(body)
	length := bodyLen + 12 // id(4) + type(4) + body + terminator(4? Kotlin writeIntLE(0)=4)
	// 修正: Kotlin 代码 out.writeIntLE(0) 写 4 字节终止, 故 length = body + 4 + 4 + 4 = body + 12
	out := make([]byte, 4+length)
	binary.LittleEndian.PutUint32(out[0:4], uint32(length))
	binary.LittleEndian.PutUint32(out[4:8], uint32(p.ID))
	binary.LittleEndian.PutUint32(out[8:12], uint32(p.Type))
	copy(out[12:12+bodyLen], body)
	// out[12+bodyLen : 16+bodyLen] 已是 0, 作为终止符
	return out
}

// 保留 gameio 引用避免 linter 警告未来扩展使用
var _ = gameio.ErrEOF
