package relay

import (
	"strings"
	"testing"

	gameio "rwhps/internal/io"
	"rwhps/internal/io/packettype"
)

// TestRelayServerInitInfo 验证 RelayServerInitInfoPacket 字节布局
func TestRelayServerInitInfo(t *testing.T) {
	p := RelayServerInitInfoPacket()
	if p.Type.TypeInt() != int(packettype.RELAY_VERSION_INFO) {
		t.Errorf("type 应为 RELAY_VERSION_INFO")
	}
	in := gameio.NewGameInputStreamFromPacket(p, 0)
	if in.ReadByte() != 0 {
		t.Errorf("byte0")
	}
	if in.ReadInt() != 151 {
		t.Errorf("version = 151")
	}
	if in.ReadInt() != 1 {
		t.Errorf("count")
	}
	if in.ReadBoolean() {
		t.Errorf("false flag")
	}
}

// TestRelayServerType 验证 117 包与 118 包的字节布局 + trim
func TestRelayServerType(t *testing.T) {
	p := RelayServerTypePacket("请输入密码")
	if p.Type.TypeInt() != int(packettype.RELAY_117) {
		t.Errorf("type 应为 RELAY_117")
	}
	in := gameio.NewGameInputStreamFromPacket(p, 0)
	if in.ReadByte() != 1 {
		t.Errorf("byte0 != 1")
	}
	if in.ReadInt() != 5 {
		t.Errorf("magic != 5")
	}
	if in.ReadString() != "请输入密码" {
		t.Errorf("msg")
	}
}

// TestRelayServerTypeReplyDecode 验证 118 包 (跳过 5 字节 → UTF-8 → trim)
func TestRelayServerTypeReplyDecode(t *testing.T) {
	// 模拟服务端回应: 5 字节 (1 byte + int) + " hello \n"
	o := gameio.NewGameOutputStream()
	o.WriteByte(0)
	o.WriteInt(0)
	o.WriteString("  hello  ")
	reply := o.CreatePacket(packettype.RELAY_118_117_RETURN)

	got := RelayServerTypeReply(reply)
	if got != "hello" {
		t.Errorf("trim 结果 = %q, want %q", got, "hello")
	}
}

// TestFromRelayJumpsToAnotherServer 验证从 RELAY 跳转包布局
func TestFromRelayJumpsToAnotherServer(t *testing.T) {
	p := FromRelayJumpsToAnotherServerPacket("127.0.0.1:1234")
	if p.Type.TypeInt() != int(packettype.PACKET_RECONNECT_TO) {
		t.Errorf("type 应为 PACKET_RECONNECT_TO")
	}
	in := gameio.NewGameInputStreamFromPacket(p, 0)
	if in.ReadByte() != 0 {
		t.Errorf("byte0")
	}
	if in.ReadInt() != 3 {
		t.Errorf("proto = 3")
	}
	if in.ReadBoolean() {
		t.Errorf("debug flag")
	}
	if in.ReadInt() != 1 {
		t.Errorf("For")
	}
	if in.ReadString() != "127.0.0.1:1234" {
		t.Errorf("ip")
	}
}

// TestRelayServerTypeReplyTrimWithTabs 验证 trim 也裁剪 tab/换行
func TestRelayServerTypeReplyTrimWithTabs(t *testing.T) {
	o := gameio.NewGameOutputStream()
	o.WriteByte(0)
	o.WriteInt(0)
	o.WriteString("\t\nabc\t\r")
	p := o.CreatePacket(packettype.RELAY_118_117_RETURN)

	got := RelayServerTypeReply(p)
	if got != "abc" {
		t.Errorf("trim 多种控制字符 = %q, want %q", got, "abc")
	}
	// 同时验证 helper 函数
	if trimmed := trimControl("   xyz   "); trimmed != "xyz" {
		t.Errorf("trimControl 直调 = %q", trimmed)
	}
	if !strings.ContainsRune("hello", 'h') {
		t.Fail()
	}
	_ = strings.ContainsRune // 保留引用
}