// Package packet 对应 net.rwhps.server.io.packet.Packet
package packet

import (
	"encoding/hex"
	"fmt"

	"rwhps/internal/io/packettype"
	"rwhps/internal/util/control"
	"rwhps/internal/util/log"
)

// Packet 网络传输包的具象化
type Packet struct {
	// Type 包的类型
	Type packettype.AbstractPacketType
	// Bytes 这个包包含的字节
	Bytes []byte
	// Status 决定这个包是否向下继续传递
	Status control.EventNext
}

// NewPacketFromInt 对应 Packet(type0: Int, bytes: ByteArray)
func NewPacketFromInt(type0 int, bytes []byte) *Packet {
	t := packettype.Resolver.From(type0)
	p := &Packet{Type: t, Bytes: bytes, Status: control.CONTINUE}
	if t.TypeInt() == int(packettype.NOT_RESOLVED) {
		log.Fatal(fmt.Sprintf("ERROR , %d", type0))
	}
	return p
}

// NewPacket 对应 Packet(type0: AbstractPacketType, bytes: ByteArray)
func NewPacket(t packettype.AbstractPacketType, bytes []byte) *Packet {
	return &Packet{Type: t, Bytes: bytes, Status: control.CONTINUE}
}

// NewEmptyPacket 对应 PacketType.nullPacket
func NewEmptyPacket() *Packet {
	return NewPacket(packettype.EMPTYP_ACKAGE, []byte{})
}

// EqualsType 对应 equals, 比较类型号
func (p *Packet) EqualsType(other packettype.AbstractPacketType) bool {
	return other.TypeInt() == p.Type.TypeInt()
}

// String 对应 toString
func (p *Packet) String() string {
	return fmt.Sprintf("Packet{\n    Bytes=%v\n    BytesHex=%s\n    type=%d\n}",
		p.Bytes, hex.EncodeToString(p.Bytes), p.Type.TypeInt())
}

// Clone 复制包
func (p *Packet) Clone() *Packet {
	b := make([]byte, len(p.Bytes))
	copy(b, p.Bytes)
	return &Packet{Type: p.Type, Bytes: b, Status: p.Status}
}
