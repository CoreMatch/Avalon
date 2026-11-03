package gameserver

import (
	"encoding/base64"
	"sync"
	"testing"

	"rwhps/internal/data/bean"
	"rwhps/internal/data/global"
	"rwhps/internal/game"
	"rwhps/internal/game/external"
	gameio "rwhps/internal/io"
	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
	"rwhps/internal/net/core"
)

// --- 测试基建 ---

// mockHelper 记录 conn.* 调用的假内核
type mockHelper struct {
	mu      sync.Mutex
	opens   []string // "room/conn"
	closes  []string
	packets []fwdPacket
	openErr error
}

type fwdPacket struct {
	room, conn string
	ptype      int
	bytes      []byte
}

func (m *mockHelper) ConnOpen(room, conn string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.openErr != nil {
		return m.openErr
	}
	m.opens = append(m.opens, room+"/"+conn)
	return nil
}

func (m *mockHelper) ConnClose(room, conn string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closes = append(m.closes, room+"/"+conn)
	return nil
}

func (m *mockHelper) ConnPacket(room, conn string, p *packet.Packet) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.packets = append(m.packets, fwdPacket{
		room:  room,
		conn:  conn,
		ptype: p.Type.TypeInt(),
		bytes: append([]byte{}, p.Bytes...),
	})
	return nil
}

// captureWriter 捕获发包内容的假传输层 (走 UDP 接缝, 避免真实 TCP)
type captureWriter struct {
	mu      sync.Mutex
	packets []*packet.Packet
	closed  bool
}

func (c *captureWriter) WritePacket(p *packet.Packet) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.packets = append(c.packets, p.Clone())
	return nil
}

func (c *captureWriter) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return nil
}

func (c *captureWriter) IsClosedUDP() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *captureWriter) snapshot() []*packet.Packet {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*packet.Packet, len(c.packets))
	copy(out, c.packets)
	return out
}

// newTestServer 构造一个绑定假内核与假传输层的 GameVersionServer
//
// helper 传 nil 表示无内核场景.
func newTestServer(t *testing.T, h HelperBackend) (*GameVersionServer, *captureWriter) {
	t.Helper()

	// ConfigServer 在独立测试二进制里是零值 (MaxMessageLen=0 会让所有聊天走超长分支),
	// 装上游默认值并在结束时还原.
	oldCfg := global.ConfigServer
	global.ConfigServer = bean.NewServerConfig()
	t.Cleanup(func() { global.ConfigServer = oldCfg })

	SetHelper(h)
	t.Cleanup(func() { SetHelper(nil) })

	game.SetDefaultRoom(game.NewGameSession("room-test", nil))
	t.Cleanup(func() { game.SetDefaultRoom(nil) })

	w := &captureWriter{}
	ca := core.NewUDPConnectionAgreement(w, NewRwHps(core.ServerProtocol), "127.0.0.1:9999", 5123)
	g := NewGameVersionServer(ca)
	t.Cleanup(func() { connections.remove(g.connID) })
	return g, w
}

// --- 版本常量 (验收: 目标客户端 1.15 / 176) ---

// TestVersionConstants 对应 GameVersionServer.kt:72-90 的常量
func TestVersionConstants(t *testing.T) {
	g, _ := newTestServer(t, nil)

	if got := g.Version(); got != "1.15 RW-HPS" {
		t.Fatalf("Version = %q", got)
	}
	if got := g.SupportedVersionGame(); got != "1.15" {
		t.Fatalf("SupportedVersionGame = %q", got)
	}
	if got := g.SupportedVersionInt(); got != 176 {
		t.Fatalf("SupportedVersionInt = %d", got)
	}
	if g.SupportedVersionBeta() {
		t.Fatal("SupportedVersionBeta 应为 false")
	}
	if got := g.PermissionStatus(); got != core.ServerInitialConnection {
		t.Fatalf("PermissionStatus = %v", got)
	}

	// HeadlessTypeConnect.version = "${Data.SERVER_CORE_VERSION}: 1.0.0"
	if got := NewHeadlessTypeConnect().Version(); got != global.ServerCoreVersion+": 1.0.0" {
		t.Fatalf("TypeConnect Version = %q", got)
	}
}

// --- 连接建立 (验收: conn.open 时机与上游 netEngine.aM.add 一致) ---

// TestConnOpenOnConstruction 连接处理器构造时即向内核登记
func TestConnOpenOnConstruction(t *testing.T) {
	h := &mockHelper{}
	g, _ := newTestServer(t, h)

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.opens) != 1 {
		t.Fatalf("conn.open 次数 = %d, 期望 1", len(h.opens))
	}
	if h.opens[0] != "room-test/"+g.ConnID() {
		t.Fatalf("conn.open = %q, 期望 room-test/<connID>", h.opens[0])
	}
}

// TestConnOpenFailureDoesNotDropConnection 内核登记失败只记日志, 不断连接
//
// 与上游"引擎未就绪"的表现一致: 握手不会有回应, 超时检测收走连接.
func TestConnOpenFailureDoesNotDropConnection(t *testing.T) {
	h := &mockHelper{openErr: core.ErrNotImplemented}
	g, w := newTestServer(t, h)

	if g.IsDis {
		t.Fatal("conn.open 失败不应断开连接")
	}
	if w.IsClosedUDP() {
		t.Fatal("传输层不应被关闭")
	}
	if connections.get(g.ConnID()) != g {
		t.Fatal("连接仍应在路由表中")
	}
}

// --- 分发 (验收: 帧解析后的包按上游分支处理) ---

// TestProcessConnectForwardsGamePackets 非拦截包经 conn.packet 转给内核
//
// 以握手首包 PREREGISTER_INFO_RECEIVE 为例: 上游同样直接进引擎.
func TestProcessConnectForwardsGamePackets(t *testing.T) {
	h := &mockHelper{}
	g, _ := newTestServer(t, h)
	tc := &HeadlessTypeConnect{con: g}

	payload := []byte{0xCA, 0xFE, 0xBA, 0xBE}
	if err := tc.ProcessConnect(packet.NewPacket(packettype.PREREGISTER_INFO_RECEIVE, payload)); err != nil {
		t.Fatalf("ProcessConnect: %v", err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.packets) != 1 {
		t.Fatalf("转发包数 = %d, 期望 1", len(h.packets))
	}
	fp := h.packets[0]
	if fp.room != "room-test" || fp.conn != g.ConnID() {
		t.Fatalf("转发目标 = %s/%s", fp.room, fp.conn)
	}
	if fp.ptype != int(packettype.PREREGISTER_INFO_RECEIVE) {
		t.Fatalf("转发包类型 = %d", fp.ptype)
	}
	if string(fp.bytes) != string(payload) {
		t.Fatalf("转发包负载 = %x", fp.bytes)
	}
}

// TestChatIsIntercepted 聊天包不转发给引擎 (上游由 RW-HPS 自行处理)
func TestChatIsIntercepted(t *testing.T) {
	h := &mockHelper{}
	g, w := newTestServer(t, h)
	tc := &HeadlessTypeConnect{con: g}

	o := gameio.NewGameOutputStream()
	o.WriteString("hello everyone")
	if err := tc.ProcessConnect(o.CreatePacket(packettype.CHAT_RECEIVE)); err != nil {
		t.Fatalf("ProcessConnect: %v", err)
	}

	h.mu.Lock()
	n := len(h.packets)
	h.mu.Unlock()
	if n != 0 {
		t.Fatalf("聊天包不应转发给引擎, 转发了 %d 个包", n)
	}
	// 玩家实体落地前聊天被拦在 Go 侧, 客户端也不会收到回包
	if pkts := w.snapshot(); len(pkts) != 0 {
		t.Fatalf("不应有回包, 收到 %d 个", len(pkts))
	}
}

// TestChatMaxLenReplies 超长消息回 message.maxLen 系统消息
//
// 对应上游 receiveChat 的 maxMessageLen 分支.
func TestChatMaxLenReplies(t *testing.T) {
	g, w := newTestServer(t, &mockHelper{})

	old := global.ConfigServer.MaxMessageLen
	global.ConfigServer.MaxMessageLen = 5
	t.Cleanup(func() { global.ConfigServer.MaxMessageLen = old })

	o := gameio.NewGameOutputStream()
	o.WriteString("this message is far too long")
	p := o.CreatePacket(packettype.CHAT_RECEIVE)
	g.ReceiveChat(p)

	pkts := w.snapshot()
	if len(pkts) != 1 {
		t.Fatalf("应回一条系统消息, 收到 %d 个包", len(pkts))
	}
	if pkts[0].Type.TypeInt() != int(packettype.CHAT) {
		t.Fatalf("回包类型 = %d, 期望 CHAT", pkts[0].Type.TypeInt())
	}
}

// TestChatMuteAllSilenced 全员禁言时聊天被静默拦下
func TestChatMuteAllSilenced(t *testing.T) {
	g, w := newTestServer(t, &mockHelper{})
	g.room.MuteAll = true

	o := gameio.NewGameOutputStream()
	o.WriteString("hi")
	p := o.CreatePacket(packettype.CHAT_RECEIVE)
	g.ReceiveChat(p)

	if pkts := w.snapshot(); len(pkts) != 0 {
		t.Fatalf("禁言时不应有任何回包, 收到 %d 个", len(pkts))
	}
}

// TestDisconnectPacket 客户端 DISCONNECT: 关连接 + 通知内核 + 清路由
func TestDisconnectPacket(t *testing.T) {
	h := &mockHelper{}
	g, w := newTestServer(t, h)
	tc := &HeadlessTypeConnect{con: g}

	if err := tc.ProcessConnect(packet.NewPacket(packettype.DISCONNECT, nil)); err != nil {
		t.Fatalf("ProcessConnect: %v", err)
	}

	if !g.IsDis {
		t.Fatal("IsDis 应置位")
	}
	h.mu.Lock()
	closes := len(h.closes)
	h.mu.Unlock()
	if closes != 1 {
		t.Fatalf("conn.close 次数 = %d, 期望 1", closes)
	}
	if !w.IsClosedUDP() {
		t.Fatal("传输层应被关闭")
	}
	if connections.get(g.ConnID()) != nil {
		t.Fatal("连接应从路由表移除")
	}

	// 上游 isDis 守卫: 重复断开不再动作
	if err := tc.ProcessConnect(packet.NewPacket(packettype.DISCONNECT, nil)); err != nil {
		t.Fatalf("重复 ProcessConnect: %v", err)
	}
	h.mu.Lock()
	closes = len(h.closes)
	h.mu.Unlock()
	if closes != 1 {
		t.Fatalf("重复断开不应再次 conn.close, 次数 = %d", closes)
	}
}

// --- 发包布局 (验收: 与上游字节布局一致) ---

// TestSendKickLayout 对应 sendKick: KICK(reason) + DISCONNECT("kicked")
func TestSendKickLayout(t *testing.T) {
	g, w := newTestServer(t, nil)

	if err := g.SendKick("version mismatch"); err != nil {
		t.Fatalf("SendKick: %v", err)
	}

	pkts := w.snapshot()
	if len(pkts) != 2 {
		t.Fatalf("应发两个包, 实发 %d", len(pkts))
	}

	if pkts[0].Type.TypeInt() != int(packettype.KICK) {
		t.Fatalf("首包类型 = %d, 期望 KICK", pkts[0].Type.TypeInt())
	}
	in := gameio.NewGameInputStreamFromPacket(pkts[0], 0)
	if got := in.ReadString(); got != "version mismatch" {
		t.Fatalf("KICK 原因 = %q", got)
	}

	if pkts[1].Type.TypeInt() != int(packettype.DISCONNECT) {
		t.Fatalf("次包类型 = %d, 期望 DISCONNECT", pkts[1].Type.TypeInt())
	}
	in2 := gameio.NewGameInputStreamFromPacket(pkts[1], 0)
	if got := in2.ReadString(); got != "kicked" {
		t.Fatalf("DISCONNECT 负载 = %q, 期望 kicked", got)
	}
}

// TestSendSystemAndChatMessage 系统消息 / 玩家聊天走包构造器
func TestSendSystemAndChatMessage(t *testing.T) {
	g, w := newTestServer(t, nil)

	if err := g.SendSystemMessage("welcome"); err != nil {
		t.Fatalf("SendSystemMessage: %v", err)
	}
	if err := g.SendChatMessage("hi", "alice", 3); err != nil {
		t.Fatalf("SendChatMessage: %v", err)
	}

	pkts := w.snapshot()
	if len(pkts) != 2 {
		t.Fatalf("应发两个包, 实发 %d", len(pkts))
	}
	for _, p := range pkts {
		if p.Type.TypeInt() != int(packettype.CHAT) {
			t.Fatalf("包类型 = %d, 期望 CHAT", p.Type.TypeInt())
		}
	}
}

// --- 内核 -> 客户端 路由 (验收: 握手响应 / 版本拒绝路径) ---

// TestHandshakeResponseRouted 引擎的握手响应经 Go 路由回客户端
//
// 完整链路: 客户端首包 -> conn.packet 进引擎 -> 引擎回 PREREGISTER_INFO ->
// Go 按 connID 找到连接并发出. 引擎侧的握手逻辑在 helper 内 (上游在 JVM 内),
// 这里模拟引擎的应答包.
func TestHandshakeResponseRouted(t *testing.T) {
	h := &mockHelper{}
	g, w := newTestServer(t, h)
	tc := &HeadlessTypeConnect{con: g}

	// 1. 客户端首包进引擎
	if err := tc.ProcessConnect(packet.NewPacket(packettype.PREREGISTER_INFO_RECEIVE, []byte{0x01})); err != nil {
		t.Fatalf("ProcessConnect: %v", err)
	}

	// 2. 引擎应答 PREREGISTER_INFO
	respBody := []byte("server-info-payload")
	routePacket(externalPacketMessageForTest(g.ConnID(), int(packettype.PREREGISTER_INFO), respBody))

	pkts := w.snapshot()
	if len(pkts) != 1 {
		t.Fatalf("客户端应收到握手响应, 实收 %d 个包", len(pkts))
	}
	if pkts[0].Type.TypeInt() != int(packettype.PREREGISTER_INFO) {
		t.Fatalf("响应类型 = %d, 期望 PREREGISTER_INFO", pkts[0].Type.TypeInt())
	}
	if string(pkts[0].Bytes) != string(respBody) {
		t.Fatalf("响应负载 = %x", pkts[0].Bytes)
	}
}

// TestVersionRejectionForwarded 版本拒绝路径: 引擎拒绝错误版本客户端
//
// 上游由引擎在 PREREGISTER 握手内拒绝 (Go 侧不做版本判定, 见 DECISIONS D-4);
// 拒绝表现为引擎发来 KICK + DISCONNECT, Go 必须原样路由给客户端.
func TestVersionRejectionForwarded(t *testing.T) {
	g, w := newTestServer(t, nil)

	kick := gameio.NewGameOutputStream()
	kick.WriteString("unsupported client version")
	routePacket(externalPacketMessageForTest(g.ConnID(), int(packettype.KICK), kick.GetByteArray()))

	dis := gameio.NewGameOutputStream()
	dis.WriteString("kicked")
	routePacket(externalPacketMessageForTest(g.ConnID(), int(packettype.DISCONNECT), dis.GetByteArray()))

	pkts := w.snapshot()
	if len(pkts) != 2 {
		t.Fatalf("客户端应收到拒绝两连包, 实收 %d", len(pkts))
	}
	if pkts[0].Type.TypeInt() != int(packettype.KICK) || pkts[1].Type.TypeInt() != int(packettype.DISCONNECT) {
		t.Fatalf("拒绝包序列 = %d,%d", pkts[0].Type.TypeInt(), pkts[1].Type.TypeInt())
	}
	in := gameio.NewGameInputStreamFromPacket(pkts[0], 0)
	if got := in.ReadString(); got != "unsupported client version" {
		t.Fatalf("拒绝原因 = %q", got)
	}
}

// TestRoutePacketUnknownConn 未知连接的包只丢弃, 不 panic
func TestRoutePacketUnknownConn(t *testing.T) {
	newTestServer(t, nil) // 路由表里有一个合法连接
	routePacket(externalPacketMessageForTest("no-such-conn", int(packettype.PREREGISTER_INFO), []byte{1}))
	// 到达这里即通过: 无 panic, 无可观察副作用
}

// TestDisconnectAll helper 进程退出时断开全部连接
func TestDisconnectAll(t *testing.T) {
	g1, w1 := newTestServer(t, nil)
	g2, w2 := newTestServer(t, nil)

	disconnectAll()

	if !g1.IsDis || !g2.IsDis {
		t.Fatal("全部连接都应断开")
	}
	if !w1.IsClosedUDP() || !w2.IsClosedUDP() {
		t.Fatal("传输层都应关闭")
	}
	if connections.get(g1.ConnID()) != nil || connections.get(g2.ConnID()) != nil {
		t.Fatal("路由表应清空")
	}
}

// --- 注册 (验收: NetStaticData.RwHps() 不再落到 NullRwHps) ---
// 注册相关测试见 rwhps_test.go.

// externalPacketMessageForTest 构造一条引擎 -> Go 的包消息
func externalPacketMessageForTest(conn string, ptype int, data []byte) *external.PacketMessage {
	return &external.PacketMessage{
		V:     external.ProtocolVersion,
		Type:  external.MsgPacket,
		Gen:   1,
		Room:  "room-test",
		Conn:  conn,
		PType: ptype,
		Data:  base64.StdEncoding.EncodeToString(data),
	}
}
