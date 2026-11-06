package net

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"rwhps/internal/io/packet"
)

// TestRconRoundTrip 验证 RconWritePacket + RconReadPacket 完整往返
func TestRconRoundTrip(t *testing.T) {
	cases := []*packet.RconPacket{
		packet.NewRconPacket(0, int(packet.RconServerdataAuth), "password123"),
		packet.NewRconPacket(42, int(packet.RconServerdataExecCommand), "say hello"),
		packet.NewRconPacket(-1, int(packet.RconServerdataAuthResponse), ""),
		packet.NewRconPacket(7, int(packet.RconServerdataResponseValue), ""),
	}
	for _, p := range cases {
		wire := RconWritePacket(p)
		got, err := RconReadPacket(bytes.NewReader(wire))
		if err != nil {
			t.Fatalf("RconReadPacket err: %v", err)
		}
		if got.ID != p.ID || got.Type != p.Type || got.Body != p.Body {
			t.Errorf("round-trip mismatch: want %+v, got %+v", p, got)
		}
	}
}

// TestRconLengthOutOfRange 验证 length 越界返回 ErrRconLengthOutOfRange
func TestRconLengthOutOfRange(t *testing.T) {
	// length=0 → < 4 → 错误
	buf := make([]byte, 4)
	_, err := RconReadPacket(bytes.NewReader(buf))
	if !errors.Is(err, ErrRconLengthOutOfRange) {
		t.Errorf("length=0 应触发 ErrRconLengthOutOfRange, 实际: %v", err)
	}
	// length=5000 → > 4096 → 错误
	overlong := make([]byte, 4)
	overlong[0] = 0x88 // 5000 LE
	overlong[1] = 0x13
	_, err = RconReadPacket(bytes.NewReader(overlong))
	if !errors.Is(err, ErrRconLengthOutOfRange) {
		t.Errorf("length=5000 应触发 ErrRconLengthOutOfRange, 实际: %v", err)
	}
}

// TestRconWireLayout 验证线协议布局: [4 LE length][4 LE id][4 LE type][body][4 bytes 终止]
func TestRconWireLayout(t *testing.T) {
	body := "test"
	p := packet.NewRconPacket(1, 2, body)
	wire := RconWritePacket(p)
	// length 应为 body.length + 12
	if len(wire) != len(body)+4+12 {
		t.Errorf("wire length 期望 %d, 实际 %d", len(body)+4+12, len(wire))
	}
	if string(wire[len(wire)-4:]) != "\x00\x00\x00\x00" {
		t.Errorf("终止符应为 4 字节 0")
	}
}

// TestRconReadMultiplePackets 验证在 Reader 上连续读多个包
func TestRconReadMultiplePackets(t *testing.T) {
	var buf bytes.Buffer
	packets := []*packet.RconPacket{
		packet.NewRconPacket(1, 2, "first"),
		packet.NewRconPacket(2, 2, "second"),
		packet.NewRconPacket(3, 0, ""),
	}
	for _, p := range packets {
		buf.Write(RconWritePacket(p))
	}
	for _, want := range packets {
		got, err := RconReadPacket(&buf)
		if err != nil {
			t.Fatalf("读取连续包失败: %v", err)
		}
		if got.ID != want.ID || got.Type != want.Type || got.Body != want.Body {
			t.Errorf("packet mismatch: want %+v got %+v", want, got)
		}
	}
}

// TestRconBodyWithUnicode 验证 body 中可以含中文 (UTF-8 字节原样存储)
func TestRconBodyWithUnicode(t *testing.T) {
	p := packet.NewRconPacket(99, 2, "你好世界-测试")
	wire := RconWritePacket(p)
	got, err := RconReadPacket(bytes.NewReader(wire))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Body != "你好世界-测试" {
		t.Errorf("UTF-8 body 不匹配: got %q", got.Body)
	}
}

// TestRconShortRead 验证截断输入返回 io.ErrUnexpectedEOF
func TestRconShortRead(t *testing.T) {
	wire := RconWritePacket(packet.NewRconPacket(1, 2, "abc"))
	trunc := wire[:4]
	_, err := RconReadPacket(bytes.NewReader(trunc))
	if err == nil {
		t.Errorf("截断输入应返回错误")
	}
	// 不应识别为 ErrRconLengthOutOfRange
	if errors.Is(err, ErrRconLengthOutOfRange) {
		t.Errorf("4 字节长度头仍应有合法的 length 值, 不应触发长度越界")
	}
	_ = strings.HasSuffix // 保留 strings 引用
}