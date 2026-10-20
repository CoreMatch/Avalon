package packet

import (
	"encoding/hex"
	"fmt"
)

// GameCommandPacket 对应 GameCommandPacket
type GameCommandPacket struct {
	SendBy int
	Bytes  []byte
	Gzip   bool
}

// NewGameCommandPacket 对应 GameCommandPacket(sendBy, bytes, gzip)
func NewGameCommandPacket(sendBy int, bytes []byte, gzip bool) *GameCommandPacket {
	return &GameCommandPacket{SendBy: sendBy, Bytes: bytes, Gzip: gzip}
}

// String 对应 toString
func (g *GameCommandPacket) String() string {
	return fmt.Sprintf("GameCommandPacket {\n    Bytes=%v\n    BytesHex=%s\n    sendBy=%d\n}",
		g.Bytes, hex.EncodeToString(g.Bytes), g.SendBy)
}
