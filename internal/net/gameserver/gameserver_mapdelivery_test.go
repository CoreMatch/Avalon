package gameserver

// 协议 v4 地图分发的连接层验收:
//
//   - START_GAME(120) 只读解析: 自定义地图分支 ([byte 0][int32 枚举=1]
//     [int32 大小][地图字节]...) 解析出地图流大小, 其余形态一律 -1;
//   - 送达成功记入连接状态 (供"客户端拒收"分类);
//   - 发送失败归类为"传输中断"并断开;
//   - 地图流已送达但客户端无任何接受/对局包即断开 -> "客户端拒收或加载失败";
//     已接受 (ACCEPT_START_GAME/TICK/GAMECOMMAND) 或默认地图开局不触发.

import (
	"encoding/base64"
	"errors"
	"sync"
	"testing"

	"github.com/CoreMatch/Avalon/internal/game/external"
	"github.com/CoreMatch/Avalon/internal/io/packet"
	"github.com/CoreMatch/Avalon/internal/io/packettype"
	"github.com/CoreMatch/Avalon/internal/net/core"
)

// startGamePayload 按引擎官方格式构造 START_GAME 负载
func startGamePayload(ordinal int32, mapStream []byte) []byte {
	out := []byte{0}
	out = append(out, byte(ordinal>>24), byte(ordinal>>16), byte(ordinal>>8), byte(ordinal))
	if ordinal == 1 {
		n := int32(len(mapStream))
		out = append(out, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
		out = append(out, mapStream...)
	}
	return out
}

func TestParseStartGameMapStream(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want int64
	}{
		{"自定义地图带流", startGamePayload(1, make([]byte, 14642)), 14642},
		{"默认地图无流", startGamePayload(0, nil), -1},
		{"存档分支无流", startGamePayload(2, nil), -1},
		{"负载过短", []byte{0, 0, 0, 1}, -1},
		{"首字节异常", append([]byte{9}, startGamePayload(1, make([]byte, 4))[1:]...), -1},
	}
	for _, c := range cases {
		if got := parseStartGameMapStream(c.data); got != c.want {
			t.Errorf("%s: parseStartGameMapStream = %d, 期望 %d", c.name, got, c.want)
		}
	}
}

// deliverStartGame 经协议路由投递一条引擎产出的 START_GAME
func deliverStartGame(t *testing.T, connID string, payload []byte) {
	t.Helper()
	DeliverPacket(&external.PacketMessage{
		V: external.ProtocolVersion, Type: external.MsgPacket, Gen: 1,
		Room: "room-test", Conn: connID,
		PType: int(packettype.START_GAME),
		Data:  base64.StdEncoding.EncodeToString(payload),
	})
}

func TestRouteStartGameDeliversMapStream(t *testing.T) {
	h := &mockHelper{}
	g, w := newTestServer(t, h)

	mapBytes := []byte("<?xml version=\"1.0\"?><map/>")
	deliverStartGame(t, g.ConnID(), startGamePayload(1, mapBytes))

	sent := w.snapshot()
	if len(sent) != 1 {
		t.Fatalf("应发出 1 个包, 实际 %d", len(sent))
	}
	if sent[0].Type.TypeInt() != int(packettype.START_GAME) {
		t.Fatalf("发出包类型 = %d, 期望 120", sent[0].Type.TypeInt())
	}
	// 送达状态已记录 (大小 = 负载声明的地图流大小)
	size, _ := g.mapDeliveryOutcome()
	if size != int64(len(mapBytes)) {
		t.Fatalf("记录的地图流大小 = %d, 期望 %d", size, len(mapBytes))
	}
	// 客户端随后接受开局 -> 不再归类拒收
	g.ReceivePacket(packet.NewPacketFromInt(int(packettype.ACCEPT_START_GAME), []byte{}))
	if _, outcome := g.mapDeliveryOutcome(); outcome != "" {
		t.Fatalf("已接受开局后不应再归类拒收: %q", outcome)
	}
}

func TestRouteStartGameDefaultMapNoClassification(t *testing.T) {
	h := &mockHelper{}
	g, _ := newTestServer(t, h)

	deliverStartGame(t, g.ConnID(), startGamePayload(0, nil))

	size, outcome := g.mapDeliveryOutcome()
	if outcome != "" || size != -1 {
		t.Fatalf("默认地图开局不应进入地图分发分类: size=%d outcome=%q", size, outcome)
	}
}

// failingWriter 写失败的传输层: 复现"传输中断"
type failingWriter struct {
	mu     sync.Mutex
	closed bool
}

func (f *failingWriter) WritePacket(*packet.Packet) error { return errors.New("connection reset") }
func (f *failingWriter) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}
func (f *failingWriter) IsClosedUDP() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func TestRouteStartGameTransferInterrupted(t *testing.T) {
	newTestServer(t, &mockHelper{}) // 占用默认路由表项; 另建一个写失败的连接

	w := &failingWriter{}
	ca := core.NewUDPConnectionAgreement(w, NewProtocolProvider(core.ServerProtocol), "127.0.0.1:9998", 5123)
	g := NewGameVersionServer(ca)
	t.Cleanup(func() { connections.remove(g.ConnID()) })

	deliverStartGame(t, g.ConnID(), startGamePayload(1, []byte("<map/>")))

	if !g.IsDis {
		t.Fatal("START_GAME 写失败应断开连接 (传输中断)")
	}
}

func TestClientRefusedMapStreamClassification(t *testing.T) {
	h := &mockHelper{}
	g, _ := newTestServer(t, h)

	deliverStartGame(t, g.ConnID(), startGamePayload(1, []byte("<map/>")))
	// 客户端未发任何接受包即断开
	g.Disconnect()

	size, outcome := g.mapDeliveryOutcome()
	if outcome == "" {
		t.Fatal("地图流已送达且无接受包的断连应归类为客户端拒收/加载失败")
	}
	if size != int64(len("<map/>")) {
		t.Fatalf("分类携带的地图流大小 = %d", size)
	}
}

func TestAcceptedStartClearsRefusalClassification(t *testing.T) {
	h := &mockHelper{}
	g, _ := newTestServer(t, h)

	deliverStartGame(t, g.ConnID(), startGamePayload(1, []byte("<map/>")))

	// 客户端回 ACCEPT_START_GAME -> 已接受开局
	accept := packet.NewPacketFromInt(int(packettype.ACCEPT_START_GAME), []byte{})
	g.ReceivePacket(accept)

	g.Disconnect()
	if _, outcome := g.mapDeliveryOutcome(); outcome != "" {
		t.Fatalf("已接受开局的断连不应归类为拒收: %q", outcome)
	}
	h.mu.Lock()
	forwarded := len(h.packets)
	h.mu.Unlock()
	if forwarded == 0 {
		t.Fatal("ACCEPT_START_GAME 应转发给引擎")
	}
}
