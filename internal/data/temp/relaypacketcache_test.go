package temp

import (
	"testing"

	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
)

func TestRelayPacketCacheStartsEmpty(t *testing.T) {
	c := NewRelayPacketCache()
	if c.StartGamePacket != nil {
		t.Fatal("a fresh cache must hold a nil packet (Kotlin: Packet? = null)")
	}
}

func TestRelayPacketCacheRoundTrip(t *testing.T) {
	c := NewRelayPacketCache()

	// 对应 GameVersionRelay 里的写入/读取/清空三段用法
	p := packet.NewPacket(packettype.RELAY_VERSION_INFO, []byte{1, 2, 3})
	c.StartGamePacket = p
	if c.StartGamePacket != p {
		t.Fatal("StartGamePacket did not hold the assigned packet")
	}

	c.StartGamePacket = nil
	if c.StartGamePacket != nil {
		t.Fatal("StartGamePacket was not cleared")
	}
}
