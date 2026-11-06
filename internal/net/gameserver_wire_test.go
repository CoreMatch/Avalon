package net_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"rwhps/internal/game"
	"rwhps/internal/game/external"
	"rwhps/internal/game/room"
	gameio "rwhps/internal/io"
	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
	rwnet "rwhps/internal/net"
	"rwhps/internal/net/core"
	"rwhps/internal/net/gameserver"
)

// wireMockHelper 记录 conn.* 调用的假内核 (端到端测试用)
type wireMockHelper struct {
	mu      sync.Mutex
	opens   []string
	packets []wireFwd
}

type wireFwd struct {
	room, conn string
	ptype      int
	bytes      []byte
}

func (m *wireMockHelper) ConnOpen(room, conn string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.opens = append(m.opens, room+"/"+conn)
	return nil
}

func (m *wireMockHelper) ConnClose(room, conn string) error { return nil }

func (m *wireMockHelper) ConnPacket(room, conn string, p *packet.Packet) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.packets = append(m.packets, wireFwd{
		room: room, conn: conn, ptype: p.Type.TypeInt(), bytes: append([]byte{}, p.Bytes...),
	})
	return nil
}

func (m *wireMockHelper) firstConn() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.opens) == 0 {
		return ""
	}
	// opens 形如 "room/conn"
	s := m.opens[0]
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return s[i+1:]
		}
	}
	return ""
}

func (m *wireMockHelper) packetCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.packets)
}

// readFrame 从 TCP 连接读一个完整游戏帧 [4B长度][4B类型][负载]
func readFrame(t *testing.T, conn net.Conn) (int, []byte) {
	t.Helper()
	var head [8]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		t.Fatalf("读帧头: %v", err)
	}
	length := binary.BigEndian.Uint32(head[0:4])
	typeInt := binary.BigEndian.Uint32(head[4:8])
	body := make([]byte, length)
	if _, err := io.ReadFull(conn, body); err != nil {
		t.Fatalf("读帧体: %v", err)
	}
	return int(typeInt), body
}

// TestWireHandshakeRoundTrip 端到端握手: 真实 TCP 帧进 -> 转发内核 -> 内核应答路由回客户端
//
// 验收"握手响应": 客户端首包 PREREGISTER_INFO_RECEIVE 经 Go 网关进引擎,
// 引擎的 PREREGISTER_INFO 应答经同一条连接回到客户端, 帧格式与包号不变.
func TestWireHandshakeRoundTrip(t *testing.T) {
	h := &wireMockHelper{}
	gameserver.SetHelper(h)
	t.Cleanup(func() { gameserver.SetHelper(nil) })

	room.SetDefault(room.NewServerRoom(game.NewGameSession("room-wire", nil)))
	t.Cleanup(func() { room.SetDefault(nil) })

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	an := rwnet.NewAbstractNet(gameserver.NewRwHps(core.ServerProtocol))
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		serverConn, err := listener.Accept()
		if err != nil {
			return
		}
		an.HandleChannel(core.NewChannel(serverConn))
	}()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// 1. 客户端发送握手首包
	payload := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	frame := rwnet.GamePacketEncoder(packet.NewPacket(packettype.PREREGISTER_INFO_RECEIVE, payload))
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("write: %v", err)
	}

	// 2. 等内核收到 conn.open + conn.packet
	deadline := time.Now().Add(3 * time.Second)
	for (h.firstConn() == "" || h.packetCount() == 0) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	connID := h.firstConn()
	if connID == "" {
		t.Fatal("内核没有收到 conn.open")
	}
	if h.packetCount() == 0 {
		t.Fatal("内核没有收到转发的握手包")
	}

	// 3. 引擎应答 PREREGISTER_INFO, 经 Go 路由回客户端
	respBody := []byte("preregister-response")
	gameserver.DeliverPacket(&external.PacketMessage{
		V:     external.ProtocolVersion,
		Type:  external.MsgPacket,
		Gen:   1,
		Room:  "room-wire",
		Conn:  connID,
		PType: int(packettype.PREREGISTER_INFO),
		Data:  base64.StdEncoding.EncodeToString(respBody),
	})

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	typeInt, body := readFrame(t, conn)
	if typeInt != int(packettype.PREREGISTER_INFO) {
		t.Fatalf("客户端收到的包类型 = %d, 期望 PREREGISTER_INFO", typeInt)
	}
	if !bytes.Equal(body, respBody) {
		t.Fatalf("客户端收到的负载 = %x, 期望 %x", body, respBody)
	}

	_ = conn.Close()
	<-serverDone
}

// TestWireVersionRejection 端到端版本拒绝: 引擎拒绝错误版本客户端的 KICK 原样到达
//
// 版本判定在引擎内完成 (上游同样如此, 见 DECISIONS D-4); Go 网关的职责是
// 把拒绝包完整、不变形地送达客户端.
func TestWireVersionRejection(t *testing.T) {
	h := &wireMockHelper{}
	gameserver.SetHelper(h)
	t.Cleanup(func() { gameserver.SetHelper(nil) })

	room.SetDefault(room.NewServerRoom(game.NewGameSession("room-reject", nil)))
	t.Cleanup(func() { room.SetDefault(nil) })

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	an := rwnet.NewAbstractNet(gameserver.NewRwHps(core.ServerProtocol))
	go func() {
		serverConn, err := listener.Accept()
		if err != nil {
			return
		}
		an.HandleChannel(core.NewChannel(serverConn))
	}()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// 客户端随便发一个包触发连接建立
	if _, err := conn.Write(rwnet.GamePacketEncoder(packet.NewPacket(packettype.PREREGISTER_INFO_RECEIVE, []byte{0x01}))); err != nil {
		t.Fatalf("write: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for h.firstConn() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	connID := h.firstConn()
	if connID == "" {
		t.Fatal("内核没有收到 conn.open")
	}

	// 引擎判定版本不符, 发 KICK
	kick := gameio.NewGameOutputStream()
	kick.WriteString("unsupported client version")
	gameserver.DeliverPacket(&external.PacketMessage{
		V:     external.ProtocolVersion,
		Type:  external.MsgPacket,
		Gen:   1,
		Room:  "room-reject",
		Conn:  connID,
		PType: int(packettype.KICK),
		Data:  base64.StdEncoding.EncodeToString(kick.GetByteArray()),
	})

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	typeInt, body := readFrame(t, conn)
	if typeInt != int(packettype.KICK) {
		t.Fatalf("客户端收到的包类型 = %d, 期望 KICK", typeInt)
	}
	in := gameio.NewGameInputStreamFromBytes(body, 0)
	if got := in.ReadString(); got != "unsupported client version" {
		t.Fatalf("拒绝原因 = %q", got)
	}
}
