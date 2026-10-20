// Package gameio 对应 net.rwhps.server.io.GameInputStream / GameOutputStream
package gameio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"

	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
	"rwhps/internal/util/compression"
	"rwhps/internal/util/javautf"
)

// ErrEOF 对应 EOFException
var ErrEOF = errors.New("EOF")

// ---------------------------------------------------------------- InputStream

// GameInputStream 对应 GameInputStream
type GameInputStream struct {
	r            *bytes.Reader
	parseVersion int
}

// NewGameInputStreamFromBytes 对应 GameInputStream(bytes, parseVersion)
func NewGameInputStreamFromBytes(b []byte, parseVersion int) *GameInputStream {
	return &GameInputStream{r: bytes.NewReader(b), parseVersion: parseVersion}
}

// NewGameInputStreamFromPacket 对应 GameInputStream(packet, parseVersion)
func NewGameInputStreamFromPacket(p *packet.Packet, parseVersion int) *GameInputStream {
	return &GameInputStream{r: bytes.NewReader(p.Bytes), parseVersion: parseVersion}
}

// ParseVersion 返回解析版本号
func (s *GameInputStream) ParseVersion() int { return s.parseVersion }

// ReadByte 读取 1 字节 (有符号语义, 与 Java byte 一致)
func (s *GameInputStream) ReadByte() int {
	b, err := s.r.ReadByte()
	if err != nil {
		return 0
	}
	return int(int8(b))
}

// ReadUnsignedByte 读取无符号字节
func (s *GameInputStream) ReadUnsignedByte() int {
	b, err := s.r.ReadByte()
	if err != nil {
		return 0
	}
	return int(b)
}

// ReadBoolean 读取 1 字节布尔 (非 0 为 true)
func (s *GameInputStream) ReadBoolean() bool {
	b, err := s.r.ReadByte()
	if err != nil {
		return false
	}
	return b != 0
}

// ReadInt 读取 4 字节大端 int32
func (s *GameInputStream) ReadInt() int32 {
	var buf [4]byte
	if _, err := io.ReadFull(s.r, buf[:]); err != nil {
		return 0
	}
	return int32(binary.BigEndian.Uint32(buf[:]))
}

// ReadBackwardsInt 读取 4 字节小端 int32
func (s *GameInputStream) ReadBackwardsInt() int32 {
	var buf [4]byte
	if _, err := io.ReadFull(s.r, buf[:]); err != nil {
		return 0
	}
	return int32(binary.LittleEndian.Uint32(buf[:]))
}

// ReadShort 读取 2 字节大端 int16
func (s *GameInputStream) ReadShort() int16 {
	var buf [2]byte
	if _, err := io.ReadFull(s.r, buf[:]); err != nil {
		return 0
	}
	return int16(binary.BigEndian.Uint16(buf[:]))
}

// ReadBackwardsShort 读取 2 字节小端 int16
func (s *GameInputStream) ReadBackwardsShort() int16 {
	var buf [2]byte
	if _, err := io.ReadFull(s.r, buf[:]); err != nil {
		return 0
	}
	return int16(binary.LittleEndian.Uint16(buf[:]))
}

// ReadFloat 读取 4 字节 float32
func (s *GameInputStream) ReadFloat() float32 {
	return math.Float32frombits(uint32(s.ReadInt()))
}

// ReadDouble 读取 8 字节 float64
func (s *GameInputStream) ReadDouble() float64 {
	var buf [8]byte
	if _, err := io.ReadFull(s.r, buf[:]); err != nil {
		return 0
	}
	return math.Float64frombits(binary.BigEndian.Uint64(buf[:]))
}

// ReadLong 读取 8 字节 int64
func (s *GameInputStream) ReadLong() int64 {
	var buf [8]byte
	if _, err := io.ReadFull(s.r, buf[:]); err != nil {
		return 0
	}
	return int64(binary.BigEndian.Uint64(buf[:]))
}

// ReadChar 读取 2 字节 UTF-16 字符
func (s *GameInputStream) ReadChar() rune {
	var buf [2]byte
	if _, err := io.ReadFull(s.r, buf[:]); err != nil {
		return 0
	}
	return rune(binary.BigEndian.Uint16(buf[:]))
}

// ReadString 对应 DataInputStream.readUTF (2 字节长度 + Modified UTF-8)
func (s *GameInputStream) ReadString() string {
	var lenBuf [2]byte
	if _, err := io.ReadFull(s.r, lenBuf[:]); err != nil {
		return ""
	}
	n := int(binary.BigEndian.Uint16(lenBuf[:]))
	if n == 0 {
		return ""
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(s.r, buf); err != nil {
		return ""
	}
	str, err := javautf.Decode(buf)
	if err != nil {
		return ""
	}
	return str
}

// ReadIsString 对应 readIsString
func (s *GameInputStream) ReadIsString() string {
	if s.ReadBoolean() {
		return s.ReadString()
	}
	return ""
}

// ReadIsInt 对应 readIsInt
func (s *GameInputStream) ReadIsInt() int32 {
	if s.ReadBoolean() {
		return s.ReadInt()
	}
	return 0
}

// Skip 跳过指定长度
func (s *GameInputStream) Skip(n int64) {
	if n <= 0 {
		return
	}
	_, _ = s.r.Seek(n, io.SeekCurrent)
}

// ReadNBytes 读取指定长度字节 (不足则返回剩余)
func (s *GameInputStream) ReadNBytes(n int) []byte {
	if n <= 0 {
		return []byte{}
	}
	buf := make([]byte, n)
	read, _ := io.ReadFull(s.r, buf)
	return buf[:read]
}

// ReadAllBytes 读取剩余全部字节
func (s *GameInputStream) ReadAllBytes() []byte {
	b, _ := io.ReadAll(s.r)
	return b
}

// ReadStreamBytes 读取 4 字节长度前缀的字节块
func (s *GameInputStream) ReadStreamBytes() []byte {
	return s.ReadNBytes(int(s.ReadInt()))
}

// ReadStreamBytesNew 读取 (跳过 relay type) + 长度前缀字节块
func (s *GameInputStream) ReadStreamBytesNew() []byte {
	s.ReadInt()
	return s.ReadStreamBytes()
}

// TransferTo 将剩余字节写入 out
func (s *GameInputStream) TransferTo(out io.Writer) {
	_, _ = io.Copy(out, s.r)
}

// TransferToFixedLength 将指定长度字节写入 out
func (s *GameInputStream) TransferToFixedLength(out io.Writer, length int) {
	_, _ = io.CopyN(out, s.r, int64(length))
}

// GetDecodeStream 对应 getDecodeStream(bl)
func (s *GameInputStream) GetDecodeStream(bl bool) *GameInputStream {
	s.ReadString()
	raw := s.ReadStreamBytes()
	if bl {
		if dec, err := compression.GzipDecode(raw); err == nil {
			return NewGameInputStreamFromBytes(dec, 0)
		}
	}
	return NewGameInputStreamFromBytes(raw, 0)
}

// GetStream 对应 getStream
func (s *GameInputStream) GetStream() *GameInputStream {
	raw := s.ReadStreamBytesNew()
	return NewGameInputStreamFromBytes(raw, 0)
}

// GetDecodeBytes 对应 getDecodeBytes
func (s *GameInputStream) GetDecodeBytes() []byte {
	s.ReadString()
	return s.ReadStreamBytes()
}

// Size 返回剩余可读字节数 (对应 available)
func (s *GameInputStream) Size() int64 {
	return int64(s.r.Len())
}

// ReadHeadIntBytes 对应 GameInputStream.readHeadInt(bytes)
func ReadHeadIntBytes(b []byte) (int32, error) {
	if len(b) < 4 {
		return 0, ErrEOF
	}
	return int32(binary.BigEndian.Uint32(b[:4])), nil
}

// ReadHeadIntPacket 对应 GameInputStream.readHeadInt(packet)
func ReadHeadIntPacket(p *packet.Packet) (int32, error) {
	return ReadHeadIntBytes(p.Bytes)
}

// ---------------------------------------------------------------- OutputStream

// GameOutputStream 对应 GameOutputStream
type GameOutputStream struct {
	buf *bytes.Buffer
}

// NewGameOutputStream 创建空输出流
func NewGameOutputStream() *GameOutputStream {
	return &GameOutputStream{buf: &bytes.Buffer{}}
}

// NewGameOutputStreamFrom 已包含初始数据 (对应 GameOutputStream(buffer))
func NewGameOutputStreamFrom(b []byte) *GameOutputStream {
	return &GameOutputStream{buf: bytes.NewBuffer(b)}
}

// CreatePacket 构建 Packet
func (o *GameOutputStream) CreatePacket(t packettype.PacketType) *packet.Packet {
	return packet.NewPacket(t, o.GetByteArray())
}

// GetByteArray 返回当前缓冲区内容 (不复制)
func (o *GameOutputStream) GetByteArray() []byte {
	return o.buf.Bytes()
}

// GetFramedBytes 输出网络帧 [4字节长度][4字节类型][数据]
func (o *GameOutputStream) GetFramedBytes(typeInt int) []byte {
	data := o.GetByteArray()
	out := make([]byte, 0, len(data)+8)
	var head [8]byte
	binary.BigEndian.PutUint32(head[0:4], uint32(len(data)))
	binary.BigEndian.PutUint32(head[4:8], uint32(typeInt))
	out = append(out, head[:]...)
	out = append(out, data...)
	return out
}

// Size 已写入字节数
func (o *GameOutputStream) Size() int { return o.buf.Len() }

// WriteByte 写入 1 字节
func (o *GameOutputStream) WriteByte(v byte) *GameOutputStream {
	_ = o.buf.WriteByte(v)
	return o
}

// WriteBytes 写入原始字节
func (o *GameOutputStream) WriteBytes(v []byte) *GameOutputStream {
	_, _ = o.buf.Write(v)
	return o
}

// WriteBytesAndLength 写入 4 字节长度 + 字节
func (o *GameOutputStream) WriteBytesAndLength(v []byte) *GameOutputStream {
	o.WriteInt(int32(len(v)))
	_, _ = o.buf.Write(v)
	return o
}

// WriteBoolean 写入布尔
func (o *GameOutputStream) WriteBoolean(v bool) *GameOutputStream {
	if v {
		_ = o.buf.WriteByte(1)
	} else {
		_ = o.buf.WriteByte(0)
	}
	return o
}

// WriteInt 写入 4 字节大端 int32
func (o *GameOutputStream) WriteInt(v int32) *GameOutputStream {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(v))
	_, _ = o.buf.Write(b[:])
	return o
}

// WriteBackwardsInt 写入 4 字节小端 int32
func (o *GameOutputStream) WriteBackwardsInt(v int32) *GameOutputStream {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(v))
	_, _ = o.buf.Write(b[:])
	return o
}

// WriteIsInt 对应 writeIsInt(value: Int?)
// 注意: 原版 isBlank 对非 null 的 Int 恒为 false, 故这里始终写 true + 值
func (o *GameOutputStream) WriteIsInt(v int32) *GameOutputStream {
	o.WriteBoolean(true)
	o.WriteInt(v)
	return o
}

// WriteIsIntFrom 对应 writeIsInt(gameInputStream)
func (o *GameOutputStream) WriteIsIntFrom(in *GameInputStream) *GameOutputStream {
	if in.ReadBoolean() {
		o.WriteBoolean(true)
		o.WriteInt(in.ReadInt())
	} else {
		o.WriteBoolean(false)
	}
	return o
}

// WriteShort 写入 2 字节大端 int16
func (o *GameOutputStream) WriteShort(v int16) *GameOutputStream {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], uint16(v))
	_, _ = o.buf.Write(b[:])
	return o
}

// WriteBackwardsShort 写入 2 字节小端 int16
func (o *GameOutputStream) WriteBackwardsShort(v int16) *GameOutputStream {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], uint16(v))
	_, _ = o.buf.Write(b[:])
	return o
}

// WriteFloat 写入 float32
func (o *GameOutputStream) WriteFloat(v float32) *GameOutputStream {
	return o.WriteInt(int32(math.Float32bits(v)))
}

// WriteFloatInt 对应 writeFloat(value: Int)
func (o *GameOutputStream) WriteFloatInt(v int) *GameOutputStream {
	return o.WriteFloat(float32(v))
}

// WriteDouble 写入 float64
func (o *GameOutputStream) WriteDouble(v float64) *GameOutputStream {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], math.Float64bits(v))
	_, _ = o.buf.Write(b[:])
	return o
}

// WriteLong 写入 int64
func (o *GameOutputStream) WriteLong(v int64) *GameOutputStream {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(v))
	_, _ = o.buf.Write(b[:])
	return o
}

// WriteChar 写入 2 字节字符
func (o *GameOutputStream) WriteChar(v rune) *GameOutputStream {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], uint16(v))
	_, _ = o.buf.Write(b[:])
	return o
}

// WriteString 对应 DataOutputStream.writeUTF
func (o *GameOutputStream) WriteString(v string) *GameOutputStream {
	b := javautf.Encode(v)
	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(b)))
	_, _ = o.buf.Write(lenBuf[:])
	_, _ = o.buf.Write(b)
	return o
}

// WriteIsString 对应 writeIsString(value: String?)
func (o *GameOutputStream) WriteIsString(v string) *GameOutputStream {
	if v == "" {
		o.WriteBoolean(false)
	} else {
		o.WriteBoolean(true)
		o.WriteString(v)
	}
	return o
}

// WriteIsStringFrom 对应 writeIsString(gameInputStream)
func (o *GameOutputStream) WriteIsStringFrom(in *GameInputStream) *GameOutputStream {
	if in.ReadBoolean() {
		o.WriteBoolean(true)
		o.WriteString(in.ReadString())
	} else {
		o.WriteBoolean(false)
	}
	return o
}

// WriteEnum 写入枚举序号, nil 写 -1
func (o *GameOutputStream) WriteEnum(ordinal *int32) *GameOutputStream {
	if ordinal == nil {
		return o.WriteInt(-1)
	}
	return o.WriteInt(*ordinal)
}

// TransferTo 从输入流读取剩余内容写入本流
func (o *GameOutputStream) TransferTo(in *GameInputStream) *GameOutputStream {
	in.TransferTo(o.buf)
	return o
}

// TransferToFixedLength 从输入流读取固定长度写入本流
func (o *GameOutputStream) TransferToFixedLength(in *GameInputStream, length int) *GameOutputStream {
	in.TransferToFixedLength(o.buf, length)
	return o
}

// FlushEncodeDataFromCompress 对应 flushEncodeData(CompressOutputStream)
func (o *GameOutputStream) FlushEncodeDataFromCompress(head string, data []byte) *GameOutputStream {
	o.WriteString(head)
	o.WriteInt(int32(len(data)))
	_, _ = o.buf.Write(data)
	return o
}

// FlushEncodeDataFromInput 对应 flushEncodeData(GameInputStream)
func (o *GameOutputStream) FlushEncodeDataFromInput(in *GameInputStream) *GameOutputStream {
	o.WriteString(in.ReadString())
	length := in.ReadInt()
	o.WriteInt(length)
	in.TransferToFixedLength(o.buf, int(length))
	return o
}

// FlushMapData 对应 flushMapData
func (o *GameOutputStream) FlushMapData(mapSize int32, data []byte) *GameOutputStream {
	o.WriteInt(mapSize)
	_, _ = o.buf.Write(data)
	return o
}

// Reset 清空缓冲区
func (o *GameOutputStream) Reset() {
	o.buf.Reset()
}
