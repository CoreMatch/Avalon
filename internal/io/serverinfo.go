package gameio

import (
	"errors"

	"rwhps/internal/io/packet"
)

// ErrServerInfoVersion 对应 require(readByte >= 2)
var ErrServerInfoVersion = errors.New("server info version < 2")

// NewServerInfoPacket 对应 ServerInfoPacket(bytes)
func NewServerInfoPacket(bytes []byte) (*packet.ServerInfoPacket, error) {
	s := NewGameInputStreamFromBytes(bytes, 0)
	p := &packet.ServerInfoPacket{}
	s.ReadString()
	s.ReadInt()
	s.ReadInt()
	p.MapName = s.ReadString()
	p.Credits = int(s.ReadInt())
	p.Fog = int(s.ReadInt())
	p.RevealedMap = s.ReadBoolean()
	p.AIDifficuly = int(s.ReadInt())
	readByte := s.ReadByte()
	s.Skip(2) // Boolean*2
	if readByte >= 1 {
		s.Skip(8) // Int*2
	}
	if readByte < 2 {
		return nil, ErrServerInfoVersion
	}
	p.InitUnit = int(s.ReadInt())
	p.Income = s.ReadFloat()
	p.Nukes = s.ReadBoolean()
	return p, nil
}
