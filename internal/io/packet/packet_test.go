package packet

import (
	"strings"
	"testing"

	"github.com/CoreMatch/Avalon/internal/io/packettype"
	"github.com/CoreMatch/Avalon/internal/util/control"
)

// TestNewPacket 构造默认状态为 CONTINUE
func TestNewPacket(t *testing.T) {
	p := NewPacket(packettype.CHAT_RECEIVE, []byte{1, 2})
	if p.Status != control.CONTINUE {
		t.Fatal("新包默认应 CONTINUE")
	}
	if !p.EqualsType(packettype.CHAT_RECEIVE) || p.EqualsType(packettype.CHAT) {
		t.Fatal("EqualsType 应按类型号比较")
	}
}

// TestNewPacketFromInt 已知号解析; 未知号归一 NOT_RESOLVED (并记 Fatal 日志, 不退出)
func TestNewPacketFromInt(t *testing.T) {
	p := NewPacketFromInt(140, []byte{})
	if p.Type.TypeInt() != 140 {
		t.Fatalf("已知号应原样解析, got %d", p.Type.TypeInt())
	}
	u := NewPacketFromInt(9999, []byte{})
	if u.Type.TypeInt() != int(packettype.NOT_RESOLVED) {
		t.Fatalf("未知号应归一 NOT_RESOLVED, got %d", u.Type.TypeInt())
	}
}

// TestNewEmptyPacket 空包对应上游 nullPacket
func TestNewEmptyPacket(t *testing.T) {
	p := NewEmptyPacket()
	if p.Type.TypeInt() != int(packettype.EMPTYP_ACKAGE) || len(p.Bytes) != 0 {
		t.Fatalf("空包 = %v", p)
	}
}

// TestClone 深拷贝: 改克隆字节不影响原包
func TestClone(t *testing.T) {
	p := NewPacket(packettype.CHAT, []byte{1, 2, 3})
	p.Status = control.STOPPED
	c := p.Clone()
	c.Bytes[0] = 99
	if p.Bytes[0] != 1 {
		t.Fatal("Clone 应深拷贝字节")
	}
	if c.Status != control.STOPPED || c.Type.TypeInt() != int(packettype.CHAT) {
		t.Fatal("Clone 应保留状态与类型")
	}
}

// TestString 输出含十六进制与类型号 (排障格式钉死)
func TestString(t *testing.T) {
	s := NewPacket(packettype.CHAT, []byte{0xAB}).String()
	if !strings.Contains(s, "abcd") && !strings.Contains(s, "ab") {
		t.Fatalf("String 应含十六进制: %s", s)
	}
	if !strings.Contains(s, "type=141") {
		t.Fatalf("String 应含类型号: %s", s)
	}
}

// TestGameCommandPacket 字段与字符串
func TestGameCommandPacket(t *testing.T) {
	g := NewGameCommandPacket(3, []byte{0x01}, true)
	if g.SendBy != 3 || !g.Gzip || len(g.Bytes) != 1 {
		t.Fatalf("字段 = %+v", g)
	}
	if !strings.Contains(g.String(), "sendBy=3") {
		t.Fatalf("String = %s", g.String())
	}
}

// TestRconConstant Source RCON 协议常量钉死
func TestRconConstant(t *testing.T) {
	if RconServerdataResponseValue != 0 || RconServerdataAuthResponse != 2 ||
		RconServerdataExecCommand != 2 || RconServerdataAuth != 3 {
		t.Fatal("RCON 常量值与 Source 协议不一致")
	}
	r := NewRconPacket(7, int(RconServerdataAuth), "passwd")
	if r.ID != 7 || r.Type != 3 || r.Body != "passwd" {
		t.Fatalf("RconPacket = %+v", r)
	}
}
