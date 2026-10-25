package netconnectprotocol

import (
	"testing"

	gameio "rwhps/internal/io"
)

// buildUnitPacket 按原版读取顺序拼出字节:
//
//	String -> 8 字节 -> String -> 34 字节 -> Boolean
//	  └ true: String(内层流头) + 长度前缀的内层流
func buildUnitPacket(inner *gameio.GameOutputStream, present bool) []byte {
	o := gameio.NewGameOutputStream()
	o.WriteString("")
	o.WriteLong(0) // skip 8
	o.WriteString("hdr")
	o.WriteBytes(make([]byte, 34)) // skip 34
	o.WriteBoolean(present)
	if !present {
		return o.GetByteArray()
	}
	o.WriteString("c") // GetDecodeStream 读掉的内层流头
	o.WriteBytesAndLength(inner.GetByteArray())
	return o.GetByteArray()
}

// buildInner 按原版读取顺序拼出内层流:
//
//	skip 4 -> Int(单位数) -> [ String, Int, Boolean, IsString(名字), skip 16 ] * n
func buildInner(names []string) *gameio.GameOutputStream {
	inner := gameio.NewGameOutputStream()
	inner.WriteInt(0) // skip 4
	inner.WriteInt(int32(len(names)))
	for _, name := range names {
		inner.WriteString("unit-data")
		inner.WriteInt(1)
		inner.WriteBoolean(true)
		inner.WriteIsString(name)
		inner.WriteBytes(make([]byte, 16)) // skip 16
	}
	return inner
}

func TestGetPacketUnitName(t *testing.T) {
	names := []string{"tank", "tank", "aircraft", ""}
	got, err := GetPacketUnitName(buildUnitPacket(buildInner(names), true))
	if err != nil {
		t.Fatalf("GetPacketUnitName: %v", err)
	}

	want := []string{"tank", "aircraft", "Default"}
	if len(got) != len(want) {
		t.Fatalf("单位名 = %v, 期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("单位名[%d] = %q, 期望 %q (全部: %v)", i, got[i], want[i], got)
		}
	}
}

func TestGetPacketUnitNameNoUnits(t *testing.T) {
	// Boolean 为 false 时原版直接返回空 Seq
	got, err := GetPacketUnitName(buildUnitPacket(buildInner(nil), false))
	if err != nil {
		t.Fatalf("GetPacketUnitName: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("单位名 = %v, 期望空", got)
	}
}

func TestGetPacketUnitNameEmptyUnitList(t *testing.T) {
	// Boolean 为 true 但单位数为 0
	got, err := GetPacketUnitName(buildUnitPacket(buildInner(nil), true))
	if err != nil {
		t.Fatalf("GetPacketUnitName: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("单位名 = %v, 期望空", got)
	}
}
