package packettype

import (
	"bytes"
	"testing"
)

// TestProtocolNumbers 协议号钉死: 数值与原版协议逐一对齐, 漂移即断线事故
func TestProtocolNumbers(t *testing.T) {
	cases := map[PacketType]int{
		PREREGISTER_INFO_RECEIVE: 160,
		PREREGISTER_INFO:         161,
		REGISTER_PLAYER:          110,
		PASSWD_ERROR:             113,
		SERVER_INFO:              106,
		TEAM_LIST:                115,
		HEART_BEAT:               108,
		HEART_BEAT_RESPONSE:      109,
		CHAT_RECEIVE:             140,
		CHAT:                     141,
		KICK:                     150,
		DISCONNECT:               111,
		START_GAME:               120,
		ACCEPT_START_GAME:        112,
		RETURN_TO_BATTLEROOM:     122,
		TICK:                     10,
		GAMECOMMAND_RECEIVE:      20,
		SYNC_CHECK:               30,
		SYNCCHECKSUM_STATUS:      31,
		SYNC:                     35,
		PACKET_DOWNLOAD_PENDING:  4,
		RELAY_117:                117,
		RELAY_118_117_RETURN:     118,
		RELAY_POW:                151,
		RELAY_POW_RECEIVE:        152,
		RELAY_VERSION_INFO:       163,
		RELAY_BECOME_SERVER:      170,
		FORWARD_CLIENT_ADD:       172,
		FORWARD_CLIENT_REMOVE:    173,
		PACKET_FORWARD_CLIENT_FROM:        174,
		PACKET_FORWARD_CLIENT_TO:          175,
		PACKET_FORWARD_CLIENT_TO_REPEATED: 176,
		PACKET_RECONNECT_TO:               178,
		GET_SERVER_INFO_RECEIVE:           3000,
		GET_SERVER_INFO:                   3001,
		UPDATA_CLASS_RECEIVE:              3010, // 上游拼写错误, 原样保留 (见 knowledge 0005)
		STATUS_RESULT:                     3999,
		SERVER_DEBUG_RECEIVE:              2000,
		SERVER_DEBUG:                      2001,
		EMPTYP_ACKAGE:                     0,
		NOT_RESOLVED:                      -1,
	}
	for pt, want := range cases {
		if pt.TypeInt() != want {
			t.Errorf("%s = %d, want %d", pt.NameEnum(), pt.TypeInt(), want)
		}
	}
}

// TestNameEnum 已知名与未知号
func TestNameEnum(t *testing.T) {
	if got := CHAT_RECEIVE.NameEnum(); got != "CHAT_RECEIVE" {
		t.Fatalf("NameEnum = %q", got)
	}
	if got := PacketType(9999).NameEnum(); got != "UNKNOWN" {
		t.Fatalf("未知号 NameEnum = %q, want UNKNOWN", got)
	}
}

// TestFrom 解析: 已知号原样返回, 未知号归一 NOT_RESOLVED
func TestFrom(t *testing.T) {
	if got := Resolver.From(140); got.TypeInt() != 140 {
		t.Fatalf("From(140) = %d", got.TypeInt())
	}
	if got := Resolver.From(9999); got.TypeInt() != int(NOT_RESOLVED) {
		t.Fatalf("From(9999) = %d, want NOT_RESOLVED", got.TypeInt())
	}
}

// TestTypeIntBytes 类型号大端 4 字节
func TestTypeIntBytes(t *testing.T) {
	if got := CHAT_RECEIVE.TypeIntBytes(); !bytes.Equal(got, []byte{0, 0, 0, 140}) {
		t.Fatalf("TypeIntBytes = % x", got)
	}
	if got := IntToBytes(0x01020304); !bytes.Equal(got, []byte{1, 2, 3, 4}) {
		t.Fatalf("IntToBytes = % x", got)
	}
}
