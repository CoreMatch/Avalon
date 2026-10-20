package net

import (
	"bytes"
	"testing"

	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
)

// TestPacketStreamRoundTrip 验证 PacketStream 解码器与 GamePacketEncoder 配套工作
func TestPacketStreamRoundTrip(t *testing.T) {
	dec := NewPacketStream()

	// 构造若干个 Packet
	packets := []*packet.Packet{
		packet.NewPacket(packettype.PREREGISTER_INFO_RECEIVE, []byte{0x01, 0x02, 0x03}),
		packet.NewPacket(packettype.SERVER_INFO, []byte{0xAA, 0xBB, 0xCC, 0xDD}),
		packet.NewPacket(packettype.HEART_BEAT, []byte{0x00}),
	}

	// 编码并拼接
	var buf bytes.Buffer
	for _, p := range packets {
		buf.Write(GamePacketEncoder(p))
	}

	// 解码
	got, closed := dec.Feed(buf.Bytes())
	if closed {
		t.Fatal("decoder unexpectedly closed")
	}
	if len(got) != len(packets) {
		t.Fatalf("got %d packets, want %d", len(got), len(packets))
	}
	for i, p := range packets {
		if got[i].Type.TypeInt() != p.Type.TypeInt() {
			t.Errorf("packet[%d] type = %d, want %d", i, got[i].Type.TypeInt(), p.Type.TypeInt())
		}
		if !bytes.Equal(got[i].Bytes, p.Bytes) {
			t.Errorf("packet[%d] bytes = %x, want %x", i, got[i].Bytes, p.Bytes)
		}
	}
}

// TestPacketStreamFragmented 验证跨多个 Feed 调用的粘包/拆包
func TestPacketStreamFragmented(t *testing.T) {
	dec := NewPacketStream()
	frame := GamePacketEncoder(packet.NewPacket(packettype.HEART_BEAT, []byte{0xFF}))

	// 一字节一字节喂入
	var allPackets []*packet.Packet
	for _, b := range frame {
		pkts, closed := dec.Feed([]byte{b})
		if closed {
			t.Fatal("decoder unexpectedly closed")
		}
		allPackets = append(allPackets, pkts...)
	}
	if len(allPackets) != 1 {
		t.Fatalf("got %d packets, want 1", len(allPackets))
	}
	if allPackets[0].Type.TypeInt() != int(packettype.HEART_BEAT) {
		t.Errorf("type = %d, want %d", allPackets[0].Type.TypeInt(), packettype.HEART_BEAT)
	}
}

// TestReadPacket 验证 ReadPacket 一次性驱动 Reader
func TestReadPacket(t *testing.T) {
	frame := GamePacketEncoder(packet.NewPacket(packettype.SERVER_DEBUG, []byte("test")))
	dec := NewPacketStream()

	p, err := dec.ReadPacket(bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("ReadPacket err: %v", err)
	}
	if p.Type.TypeInt() != int(packettype.SERVER_DEBUG) {
		t.Errorf("type = %d, want %d", p.Type.TypeInt(), packettype.SERVER_DEBUG)
	}
	if string(p.Bytes) != "test" {
		t.Errorf("bytes = %q, want %q", p.Bytes, "test")
	}
}

// TestSniffGame 验证 Game 嗅探
func TestSniffGame(t *testing.T) {
	frame := GamePacketEncoder(packet.NewPacket(packettype.PREREGISTER_INFO_RECEIVE, []byte{0x01}))
	if kind := Sniff(frame); kind != MixGame {
		t.Errorf("Sniff = %v, want MixGame", kind)
	}
}

// TestSniffHTTP 验证 HTTP 嗅探
func TestSniffHTTP(t *testing.T) {
	head := []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")
	if kind := Sniff(head); kind != MixUnknown {
		// HTTP 默认未启用, 这里只确认不会 panic
		t.Logf("Sniff HTTP without enable: %v", kind)
	}
}