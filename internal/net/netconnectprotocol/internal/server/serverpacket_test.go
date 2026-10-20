package server

import (
	"testing"

	gameio "rwhps/internal/io"
	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
	"rwhps/internal/struct/list"
)

// TestGameTickPacketNoCmds 验证 gameTickPacketInternal 字节布局: tick + count=0 → TICK
func TestGameTickPacketNoCmds(t *testing.T) {
	p := GameTickPacket(123)
	if p.Type.TypeInt() != int(packettype.TICK) {
		t.Errorf("type 应为 TICK(%d), got %d", packettype.TICK, p.Type.TypeInt())
	}
	if len(p.Bytes) != 8 {
		t.Errorf("byte 长度应为 8, got %d", len(p.Bytes))
	}
	in := gameio.NewGameInputStreamFromPacket(p, 0)
	if got := in.ReadInt(); got != 123 {
		t.Errorf("tick = %d, want 123", got)
	}
	if got := in.ReadInt(); got != 0 {
		t.Errorf("count = %d, want 0", got)
	}
}

// TestGameTickCommandPacket 验证 gameTickCommandPacketInternal 字节布局
func TestGameTickCommandPacket(t *testing.T) {
	cmd := packet.NewGameCommandPacket(0, []byte{0xAA, 0xBB, 0xCC}, false)
	p := GameTickCommandPacket(7, cmd)
	if p.Type.TypeInt() != int(packettype.TICK) {
		t.Errorf("type 错误")
	}
	// tick(4) + count(4) + cmd(3) = 11
	if len(p.Bytes) != 11 {
		t.Errorf("byte 长度应为 11, got %d", len(p.Bytes))
	}
	in := gameio.NewGameInputStreamFromPacket(p, 0)
	if got := in.ReadInt(); got != 7 {
		t.Errorf("tick = %d", got)
	}
	if got := in.ReadInt(); got != 1 {
		t.Errorf("count = %d, want 1", got)
	}
	body := in.ReadNBytes(3)
	if string(body) != "\xaa\xbb\xcc" {
		t.Errorf("body mismatch: %v", body)
	}
}

// TestGameTickCommandsPacketSeq 验证多条时使用 Seq 容器
func TestGameTickCommandsPacketSeq(t *testing.T) {
	seq := list.NewSeq[*packet.GameCommandPacket]()
	seq.Add(packet.NewGameCommandPacket(0, []byte{0x01}, false))
	seq.Add(packet.NewGameCommandPacket(0, []byte{0x02}, false))
	p := GameTickCommandsPacket(9, seq)
	if p.Type.TypeInt() != int(packettype.TICK) {
		t.Errorf("type 错误")
	}
	in := gameio.NewGameInputStreamFromPacket(p, 0)
	if got := in.ReadInt(); got != 9 {
		t.Errorf("tick")
	}
	if got := in.ReadInt(); got != 2 {
		t.Errorf("count = %d, want 2", got)
	}
}

// TestChatMessagePacket 验证 sendBy 非空 + sendBy 为空 两个分支
func TestChatMessagePacket(t *testing.T) {
	// sendBy 非空
	p := ChatMessagePacket("hello", "alice", 7)
	if p.Type.TypeInt() != int(packettype.CHAT) {
		t.Errorf("type")
	}
	in := gameio.NewGameInputStreamFromPacket(p, 0)
	if in.ReadString() != "hello" {
		t.Errorf("msg")
	}
	if in.ReadByte() != 3 {
		t.Errorf("protocol version 应为 3")
	}
	if !in.ReadBoolean() {
		t.Errorf("sendBy 存在时 prefix 标志应为 true")
	}
	if in.ReadString() != "alice" {
		t.Errorf("sendBy")
	}
	if got := in.ReadInt(); got != 7 {
		t.Errorf("team1 = %d", got)
	}
	if got := in.ReadInt(); got != 7 {
		t.Errorf("team2 = %d", got)
	}

	// sendBy 为空: writeIsString -> writeBoolean(false)
	p2 := ChatMessagePacket("hello", "", 0)
	in2 := gameio.NewGameInputStreamFromPacket(p2, 0)
	in2.ReadString()
	in2.ReadByte()
	if in2.ReadBoolean() {
		t.Errorf("sendBy 为空时 prefix 标志应为 false")
	}
}

// TestChatUserMessagePacket
func TestChatUserMessagePacket(t *testing.T) {
	p := ChatUserMessagePacket("sim")
	if p.Type.TypeInt() != int(packettype.CHAT_RECEIVE) {
		t.Errorf("type 应为 CHAT_RECEIVE")
	}
	in := gameio.NewGameInputStreamFromPacket(p, 0)
	if in.ReadString() != "sim" {
		t.Errorf("msg")
	}
	if in.ReadByte() != 0 {
		t.Errorf("flag 应为 0")
	}
}

// TestPlayerExitPacket
func TestPlayerExitPacket(t *testing.T) {
	p := PlayerExitPacket("leave")
	if p.Type.TypeInt() != int(packettype.DISCONNECT) {
		t.Errorf("type 应为 DISCONNECT")
	}
	in := gameio.NewGameInputStreamFromPacket(p, 0)
	if in.ReadString() != "leave" {
		t.Errorf("cause 字段")
	}
}

// TestReceivingStatusPacket
func TestReceivingStatusPacket(t *testing.T) {
	p := ReceivingStatusPacket(100, 500)
	if p.Type.TypeInt() != int(packettype.PACKET_DOWNLOAD_PENDING) {
		t.Errorf("type")
	}
	in := gameio.NewGameInputStreamFromPacket(p, 0)
	if in.ReadByte() != 0 {
		t.Errorf("flag")
	}
	if in.ReadInt() != 100 {
		t.Errorf("receiveSize")
	}
	if in.ReadInt() != 500 {
		t.Errorf("allSize")
	}
}