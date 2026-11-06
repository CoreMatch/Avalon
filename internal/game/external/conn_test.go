package external

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
)

// TestConnOpenCloseRoundTrip conn.open / conn.close 的请求-应答闭环
func TestConnOpenCloseRoundTrip(t *testing.T) {
	var openReq, closeReq Request

	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpConnOpen, before: func(req *Request, _ *os.File) {
			openReq = *req
		}, reply: ok()},
		step{wantOp: OpConnClose, before: func(req *Request, _ *os.File) {
			closeReq = *req
		}, reply: ok()},
	)

	if err := b.ConnOpen("r1", "c1"); err != nil {
		t.Fatalf("ConnOpen: %v", err)
	}
	if err := b.ConnClose("r1", "c1"); err != nil {
		t.Fatalf("ConnClose: %v", err)
	}

	if openReq.Room != "r1" || openReq.Conn != "c1" {
		t.Fatalf("conn.open = %+v", openReq)
	}
	if closeReq.Room != "r1" || closeReq.Conn != "c1" {
		t.Fatalf("conn.close = %+v", closeReq)
	}
}

// TestConnOpenFailurePropagates helper 拒绝登记时原因透传
func TestConnOpenFailurePropagates(t *testing.T) {
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpConnOpen, reply: &Response{V: ProtocolVersion, OK: false, Error: "room not found"}},
	)

	err := b.ConnOpen("nope", "c1")
	if err == nil || !strings.Contains(err.Error(), "room not found") {
		t.Fatalf("ConnOpen = %v, 期望 helper 的失败原因被透传", err)
	}
}

// TestConnPacketFireAndForget conn.packet 不等应答, 负载走 Base64
func TestConnPacketFireAndForget(t *testing.T) {
	var mu sync.Mutex
	var got Request

	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpConnPacket, before: func(req *Request, _ *os.File) {
			mu.Lock()
			got = *req
			mu.Unlock()
		}},
	)

	p := packet.NewPacket(packettype.PREREGISTER_INFO_RECEIVE, []byte{0x01, 0x02, 0x03})
	if err := b.ConnPacket("r1", "c1", p); err != nil {
		t.Fatalf("ConnPacket: %v", err)
	}

	// serve 收到请求即返回, 稍等让 before 记录完成
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		op := got.Op
		mu.Unlock()
		if op != "" || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if got.Room != "r1" || got.Conn != "c1" {
		t.Fatalf("conn.packet 路由 = %+v", got)
	}
	if got.PType != int(packettype.PREREGISTER_INFO_RECEIVE) {
		t.Fatalf("conn.packet 类型 = %d", got.PType)
	}
	data, err := decodeBase64(got.Data)
	if err != nil || len(data) != 3 || data[0] != 1 || data[2] != 3 {
		t.Fatalf("conn.packet 负载 = %v (%v)", data, err)
	}
}

// TestDecodePacketData 主动消息负载解码
func TestDecodePacketData(t *testing.T) {
	m := &PacketMessage{Data: encodeBase64([]byte{9, 8, 7})}
	data, err := DecodePacketData(m)
	if err != nil {
		t.Fatalf("DecodePacketData: %v", err)
	}
	if len(data) != 3 || data[0] != 9 || data[2] != 7 {
		t.Fatalf("解码结果 = %v", data)
	}

	// 空负载给空切片而不是 nil
	if data, err := DecodePacketData(&PacketMessage{}); err != nil || data == nil || len(data) != 0 {
		t.Fatalf("空负载 = %v (%v)", data, err)
	}
	if data, err := DecodePacketData(nil); err != nil || data == nil {
		t.Fatalf("nil 消息 = %v (%v)", data, err)
	}
}
