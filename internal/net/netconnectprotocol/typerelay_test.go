package netconnectprotocol

// TypeRelay 单测: 握手状态机 (InitialConnection→GetPlayerInfo→WaitCertified→
// CertifiedEnd) 与各分支, 以及 HOST/普通玩家分发.
//
// 手法参照 gameserver_test.go: captureWriter 假传输层走 UDP 接缝, 不起真实 TCP.
// 房间在测试结束前 Re() 掉, 避免污染 room 包的全局房间表.

import (
	"strconv"
	"strings"
	"sync"
	"testing"

	"rwhps/internal/data/global"
	gameio "rwhps/internal/io"
	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
	"rwhps/internal/net/core"
	"rwhps/internal/net/netconnectprotocol/realize"
	"rwhps/internal/util/algorithms"
	cmd "rwhps/internal/util/game/command"
)

// --- 测试基建 ---

// captureWriter 捕获发包内容的假传输层
type relayCaptureWriter struct {
	mu      sync.Mutex
	packets []*packet.Packet
	closed  bool
}

func (c *relayCaptureWriter) WritePacket(p *packet.Packet) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.packets = append(c.packets, p.Clone())
	return nil
}

func (c *relayCaptureWriter) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return nil
}

func (c *relayCaptureWriter) IsClosedUDP() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *relayCaptureWriter) snapshot() []*packet.Packet {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*packet.Packet, len(c.packets))
	copy(out, c.packets)
	return out
}

func (c *relayCaptureWriter) countType(t packettype.PacketType) int {
	n := 0
	for _, p := range c.snapshot() {
		if packettype.PacketType(p.Type.TypeInt()) == t {
			n++
		}
	}
	return n
}

func (c *relayCaptureWriter) lastOfType(t packettype.PacketType) *packet.Packet {
	var last *packet.Packet
	for _, p := range c.snapshot() {
		if packettype.PacketType(p.Type.TypeInt()) == t {
			last = p
		}
	}
	return last
}

// newRelayConnect 构造绑定假传输层的 TypeRelay + GameVersionRelay
func newRelayConnect(t *testing.T, ip string) (*TypeRelay, *realize.GameVersionRelay, *relayCaptureWriter) {
	t.Helper()
	w := &relayCaptureWriter{}
	ca := core.NewUDPConnectionAgreement(w, NewRelayRwHps(core.RelayProtocol), ip+":9999", 5123)
	con := realize.NewGameVersionRelay(ca)
	return NewTypeRelay(con), con, w
}

// registerPlayerPacket 构造握手用的 REGISTER_PLAYER 包 (名字 + UUID-Hex)
func registerPlayerPacket(name, uuidHex string) *packet.Packet {
	o := gameio.NewGameOutputStream()
	o.WriteString("net.rwhps.server")
	o.WriteBytes(make([]byte, 12))
	o.WriteString(name)
	o.WriteIsString("")
	o.WriteString("extra")
	o.WriteString(uuidHex)
	return o.CreatePacket(packettype.REGISTER_PLAYER)
}

// typeReplyPacket 构造 RELAY_118_117_RETURN 包: 前 5 字节 + 明文输入
func typeReplyPacket(input string) *packet.Packet {
	o := gameio.NewGameOutputStream()
	_ = o.WriteByte(0)
	o.WriteInt(0) // 共 5 字节, 对应 relayServerTypeReplyInternalPacket 的 Skip(5)
	o.WriteString(input)
	return o.CreatePacket(packettype.RELAY_118_117_RETURN)
}

// powDigest 对应 algorithms 包内私有的同名函数:
// BigInteger(1, sha256(s)).toString(16).uppercase() 截前 14 字符
func powDigest(s string) string {
	hex := strings.ToUpper(algorithms.BigIntegerHex(algorithms.SHA256([]byte(s))))
	if len(hex) > 14 {
		return hex[:14]
	}
	return hex
}

// solvePow 解析 RELAY_POW 挑战包并构造正确应答.
// 5/6 型挑战的答案需要暴力哈希, 测试里改为以错误应答换新挑战, 返回 ok=false.
func solvePow(t *testing.T, powPacket *packet.Packet) (*packet.Packet, bool) {
	t.Helper()
	in := gameio.NewGameInputStreamFromPacket(powPacket, 0)
	resultInt := in.ReadInt()
	authType := int(in.ReadInt())

	var off string
	switch authType {
	case 0:
		if !in.ReadBoolean() {
			t.Fatal("type 0 挑战应带 initInt_1")
		}
		off = strconv.Itoa(int(in.ReadInt()))
	case 1:
		in.ReadBoolean()
		if !in.ReadBoolean() {
			t.Fatal("type 1 挑战应带 initInt_2")
		}
		off = strconv.Itoa(int(in.ReadInt()))
	case 3, 4:
		if !in.ReadBoolean() {
			t.Fatal("type 3/4 挑战应带 initInt_1")
		}
		i1 := in.ReadInt()
		if !in.ReadBoolean() {
			t.Fatal("type 3/4 挑战应带 initInt_2")
		}
		i2 := in.ReadInt()
		off = powDigest(strconv.Itoa(int(i1)) + "|" + strconv.Itoa(int(i2)))
	default:
		return nil, false
	}

	o := gameio.NewGameOutputStream()
	o.WriteInt(resultInt)
	o.WriteInt(int32(authType))
	o.WriteString(off)
	return o.CreatePacket(packettype.RELAY_POW_RECEIVE), true
}

// driveToCertifiedEnd 把连接推进到 CertifiedEnd
//
// 对应上游握手: PREREGISTER_INFO_RECEIVE -> REGISTER_PLAYER -> RELAY_POW_RECEIVE.
// 假传输层从连接自身取出 (构造时注入的 relayCaptureWriter).
func driveToCertifiedEnd(t *testing.T, tc *TypeRelay, con *realize.GameVersionRelay, name, uuidHex string) {
	t.Helper()
	w, ok := con.ConnectionAgreement.UDPOUT.(*relayCaptureWriter)
	if !ok {
		t.Fatal("连接未绑定假传输层")
	}

	// InitialConnection -> GetPlayerInfo
	_ = tc.ProcessConnect(packet.NewPacket(packettype.PREREGISTER_INFO_RECEIVE, []byte{}))
	if con.PermissionStatus() != core.RelayGetPlayerInfo {
		t.Fatalf("状态 = %v, want GetPlayerInfo", con.PermissionStatus())
	}

	// GetPlayerInfo -> WaitCertified
	_ = tc.ProcessConnect(registerPlayerPacket(name, uuidHex))
	if con.PermissionStatus() != core.RelayWaitCertified {
		t.Fatalf("状态 = %v, want WaitCertified", con.PermissionStatus())
	}
	if con.Name() != name || con.RegisterPlayerId() != uuidHex {
		t.Fatalf("注册信息 = (%q, %q)", con.Name(), con.RegisterPlayerId())
	}

	// WaitCertified -> CertifiedEnd: 回答 PoW 挑战 (5/6 型以错误应答换新挑战)
	for i := 0; i < 30; i++ {
		pow := w.lastOfType(packettype.RELAY_POW)
		if pow == nil {
			t.Fatal("应收到 RELAY_POW 挑战")
		}
		resp, ok := solvePow(t, pow)
		if !ok {
			bad := gameio.NewGameOutputStream()
			bad.WriteInt(0)
			bad.WriteInt(-1)
			bad.WriteString("bad")
			_ = tc.ProcessConnect(bad.CreatePacket(packettype.RELAY_POW_RECEIVE))
			if con.PermissionStatus() != core.RelayWaitCertified {
				t.Fatalf("错误应答后应保持 WaitCertified: %v", con.PermissionStatus())
			}
			continue
		}
		_ = tc.ProcessConnect(resp)
		if con.PermissionStatus() != core.RelayCertifiedEnd {
			t.Fatalf("状态 = %v, want CertifiedEnd", con.PermissionStatus())
		}
		return
	}
	t.Fatal("连续 30 次 5/6 型挑战, 超出重试上限")
}

// --- 握手状态机 ---

// TestRelayHandshakeInitialConnection 对应 relayCheck 的 InitialConnection 分支:
// 收到 PREREGISTER_INFO_RECEIVE 时缓存包并回送 RELAY 诱骗注册.
func TestRelayHandshakeInitialConnection(t *testing.T) {
	tc, con, w := newRelayConnect(t, "127.0.0.1")

	_ = tc.ProcessConnect(packet.NewPacket(packettype.PREREGISTER_INFO_RECEIVE, []byte{1}))

	if con.PermissionStatus() != core.RelayGetPlayerInfo {
		t.Fatalf("状态 = %v", con.PermissionStatus())
	}
	resp := w.lastOfType(packettype.PREREGISTER_INFO)
	if resp == nil {
		t.Fatal("应回送 PREREGISTER_INFO")
	}
	in := gameio.NewGameInputStreamFromPacket(resp, 0)
	if id := in.ReadString(); id != global.ServerIDRelayGet {
		t.Fatalf("诱骗 UUID = %q", id)
	}
	if v := in.ReadInt(); v != 1 {
		t.Fatalf("第二字段 = %d, want 1", v)
	}
}

// TestRelayHandshakeInitialOtherPacket InitialConnection 收到其它包走 exCommand,
// 状态不变也不断开.
func TestRelayHandshakeInitialOtherPacket(t *testing.T) {
	tc, con, w := newRelayConnect(t, "127.0.0.1")

	_ = tc.ProcessConnect(packet.NewPacket(packettype.CHAT_RECEIVE, []byte{}))

	if con.PermissionStatus() != core.RelayInitialConnection {
		t.Fatalf("状态 = %v", con.PermissionStatus())
	}
	if con.IsDis {
		t.Fatal("不应断开")
	}
	if len(w.snapshot()) != 0 {
		t.Fatalf("不应回包: %d", len(w.snapshot()))
	}
}

// TestRelayHandshakeFullFlow 完整握手: 四个状态的迁移与每步的发包
func TestRelayHandshakeFullFlow(t *testing.T) {
	tc, con, w := newRelayConnect(t, "127.0.0.1")

	driveToCertifiedEnd(t, tc, con, "tester", "00112233")

	// 握手期间的发包: PREREGISTER_INFO (诱骗) + RELAY_VERSION_INFO (自我介绍) + RELAY_POW
	if w.countType(packettype.PREREGISTER_INFO) != 1 {
		t.Fatalf("PREREGISTER_INFO 数量 = %d", w.countType(packettype.PREREGISTER_INFO))
	}
	if w.countType(packettype.RELAY_VERSION_INFO) != 1 {
		t.Fatalf("RELAY_VERSION_INFO 数量 = %d", w.countType(packettype.RELAY_VERSION_INFO))
	}
	if w.countType(packettype.RELAY_POW) < 1 {
		t.Fatal("应有 RELAY_POW 挑战")
	}

	// 空查询串 -> 回送服务端类型列表 (relay.hi)
	if w.countType(packettype.RELAY_117) != 1 {
		t.Fatalf("RELAY_117 数量 = %d, want 1 (空查询串)", w.countType(packettype.RELAY_117))
	}
	if !con.ConnectReceiveData.InputPassword {
		t.Fatal("sendRelayServerType 后 InputPassword 应为 true")
	}

	// CertifiedEnd: 118 回复空输入 -> idCustom("") -> 报错并再次弹输入框
	_ = tc.ProcessConnect(typeReplyPacket(""))
	if w.countType(packettype.RELAY_117) != 2 {
		t.Fatalf("空输入应再次弹出输入框: %d", w.countType(packettype.RELAY_117))
	}
	// sendRelayServerTypeReply 先把 InputPassword 置 false, 随后空输入触发的
	// sendRelayServerType 又把它置回 true (上游同款时序)
	if !con.ConnectReceiveData.InputPassword {
		t.Fatal("再次弹出输入框后 InputPassword 应为 true")
	}
	if con.PermissionStatus() != core.RelayCertifiedEnd {
		t.Fatalf("118 回复不改变状态: %v", con.PermissionStatus())
	}
}

// TestRelayHandshakeWaitCertifiedWrongPacket 对应 relayCheck 的 WaitCertified
// else 分支: 非 RELAY_POW_RECEIVE 包直接断开.
func TestRelayHandshakeWaitCertifiedWrongPacket(t *testing.T) {
	tc, con, _ := newRelayConnect(t, "127.0.0.1")

	_ = tc.ProcessConnect(packet.NewPacket(packettype.PREREGISTER_INFO_RECEIVE, []byte{}))
	_ = tc.ProcessConnect(registerPlayerPacket("tester", "00112233"))
	if con.PermissionStatus() != core.RelayWaitCertified {
		t.Fatalf("状态 = %v", con.PermissionStatus())
	}

	_ = tc.ProcessConnect(packet.NewPacket(packettype.CHAT_RECEIVE, []byte{}))

	if !con.IsDis {
		t.Fatal("WaitCertified 收到非 PoW 包应断开")
	}
}

// TestRelayHandshakePowWrongAnswer 对应校验失败重发挑战分支
func TestRelayHandshakePowWrongAnswer(t *testing.T) {
	tc, con, w := newRelayConnect(t, "127.0.0.1")

	_ = tc.ProcessConnect(packet.NewPacket(packettype.PREREGISTER_INFO_RECEIVE, []byte{}))
	_ = tc.ProcessConnect(registerPlayerPacket("tester", "00112233"))
	before := w.countType(packettype.RELAY_POW)

	bad := gameio.NewGameOutputStream()
	bad.WriteInt(0)
	bad.WriteInt(-1)
	bad.WriteString("bad")
	_ = tc.ProcessConnect(bad.CreatePacket(packettype.RELAY_POW_RECEIVE))

	if con.PermissionStatus() != core.RelayWaitCertified {
		t.Fatalf("状态 = %v, 应保持 WaitCertified", con.PermissionStatus())
	}
	if w.countType(packettype.RELAY_POW) != before+1 {
		t.Fatalf("错误应答后应重发挑战: %d -> %d", before, w.countType(packettype.RELAY_POW))
	}
}

// --- 握手后的房间分发 (经 118 回复走 idCustom) ---

// TestRelayHostCreateAndPlayerJoin 两个连接经完整握手: 一个建房成房主,
// 一个凭房间号加入; 之后普通玩家的 ACCEPT_START_GAME 置开局标志并转发给房主.
func TestRelayHostCreateAndPlayerJoin(t *testing.T) {
	hostTc, host, hostW := newRelayConnect(t, "127.0.0.1")
	driveToCertifiedEnd(t, hostTc, host, "alice", "aabbcc01")

	// 118 回复 "new" -> 建房并成为房主
	_ = hostTc.ProcessConnect(typeReplyPacket("new"))
	if host.Room() == nil {
		t.Fatal("new 应建房")
	}
	t.Cleanup(func() { host.Room().Re() })
	if host.PermissionStatus() != core.RelayHostPermission {
		t.Fatalf("房主状态 = %v", host.PermissionStatus())
	}

	// 第二个连接完整握手后凭房间号加入
	playerTc, player, _ := newRelayConnect(t, "127.0.0.2")
	driveToCertifiedEnd(t, playerTc, player, "bob", "ddeeff02")
	_ = playerTc.ProcessConnect(typeReplyPacket(host.Room().ID))

	if player.PermissionStatus() != core.RelayPlayerPermission {
		t.Fatalf("玩家状态 = %v", player.PermissionStatus())
	}
	if host.Room().GetSize() != 2 {
		t.Fatalf("size = %d", host.Room().GetSize())
	}

	// ACCEPT_START_GAME: 置开局标志并转发给房主
	before := hostW.countType(packettype.PACKET_FORWARD_CLIENT_FROM)
	_ = playerTc.ProcessConnect(packet.NewPacket(packettype.ACCEPT_START_GAME, []byte{}))
	if !host.Room().IsStartGame() {
		t.Fatal("ACCEPT_START_GAME 应开局")
	}
	if hostW.countType(packettype.PACKET_FORWARD_CLIENT_FROM) != before+1 {
		t.Fatal("ACCEPT_START_GAME 应转发给房主")
	}
}

// TestRelayNormalIgnoreList 对应 normalProcessing 的忽略名单:
// 普通玩家发服务端专用包 (如 SERVER_INFO) 不被转发也不断开.
func TestRelayNormalIgnoreList(t *testing.T) {
	hostTc, host, hostW := newRelayConnect(t, "127.0.0.1")
	driveToCertifiedEnd(t, hostTc, host, "alice", "aabbcc01")
	_ = hostTc.ProcessConnect(typeReplyPacket("new"))
	t.Cleanup(func() { host.Room().Re() })

	playerTc, player, _ := newRelayConnect(t, "127.0.0.2")
	driveToCertifiedEnd(t, playerTc, player, "bob", "ddeeff02")
	_ = playerTc.ProcessConnect(typeReplyPacket(host.Room().ID))

	before := hostW.countType(packettype.PACKET_FORWARD_CLIENT_FROM)
	_ = playerTc.ProcessConnect(packet.NewPacket(packettype.SERVER_INFO, []byte{1, 2, 3}))
	_ = playerTc.ProcessConnect(packet.NewPacket(packettype.TICK, []byte{}))
	_ = playerTc.ProcessConnect(packet.NewPacket(packettype.KICK, []byte{}))

	if hostW.countType(packettype.PACKET_FORWARD_CLIENT_FROM) != before {
		t.Fatal("忽略名单内的包不应被转发")
	}
	if player.IsDis {
		t.Fatal("忽略名单内的包不应导致断开")
	}
}

// TestRelayNormalUnknownTypeForwards 不在名单内的包默认转发给房主
func TestRelayNormalUnknownTypeForwards(t *testing.T) {
	hostTc, host, hostW := newRelayConnect(t, "127.0.0.1")
	driveToCertifiedEnd(t, hostTc, host, "alice", "aabbcc01")
	_ = hostTc.ProcessConnect(typeReplyPacket("new"))
	t.Cleanup(func() { host.Room().Re() })

	playerTc, player, _ := newRelayConnect(t, "127.0.0.2")
	driveToCertifiedEnd(t, playerTc, player, "bob", "ddeeff02")
	_ = playerTc.ProcessConnect(typeReplyPacket(host.Room().ID))

	before := hostW.countType(packettype.PACKET_FORWARD_CLIENT_FROM)
	// GAMECOMMAND_RECEIVE 走 receiveCommand -> 转发
	_ = playerTc.ProcessConnect(packet.NewPacket(packettype.GAMECOMMAND_RECEIVE, []byte{9}))
	if hostW.countType(packettype.PACKET_FORWARD_CLIENT_FROM) != before+1 {
		t.Fatal("GAMECOMMAND_RECEIVE 应转发给房主")
	}
}

// TestRelayNormalDisconnectForwardsThenCloses 对应 DISCONNECT 分支:
// 先转发给房主再断开.
func TestRelayNormalDisconnectForwardsThenCloses(t *testing.T) {
	hostTc, host, hostW := newRelayConnect(t, "127.0.0.1")
	driveToCertifiedEnd(t, hostTc, host, "alice", "aabbcc01")
	_ = hostTc.ProcessConnect(typeReplyPacket("new"))
	t.Cleanup(func() { host.Room().Re() })

	playerTc, player, _ := newRelayConnect(t, "127.0.0.2")
	driveToCertifiedEnd(t, playerTc, player, "bob", "ddeeff02")
	_ = playerTc.ProcessConnect(typeReplyPacket(host.Room().ID))

	before := hostW.countType(packettype.PACKET_FORWARD_CLIENT_FROM)
	_ = playerTc.ProcessConnect(packet.NewPacket(packettype.DISCONNECT, []byte{}))

	if hostW.countType(packettype.PACKET_FORWARD_CLIENT_FROM) < before+1 {
		t.Fatal("DISCONNECT 应先转发给房主")
	}
	if !player.IsDis {
		t.Fatal("DISCONNECT 后应断开")
	}
}

// --- HOST 分发 ---

// TestRelayHostChatCommand 对应 hostProcessing 的 CHAT 分支:
// 消息以 "." 开头且署名匹配时交给 RELAY_COMMAND.
func TestRelayHostChatCommand(t *testing.T) {
	hostTc, host, _ := newRelayConnect(t, "127.0.0.1")
	driveToCertifiedEnd(t, hostTc, host, "alice", "aabbcc01")
	_ = hostTc.ProcessConnect(typeReplyPacket("new"))
	t.Cleanup(func() { host.Room().Re() })
	if host.PermissionStatus() != core.RelayHostPermission {
		t.Fatalf("状态 = %v", host.PermissionStatus())
	}

	// 换上带测试命令的 RelayCommand, 避免污染全局
	old := global.RelayCommand
	global.RelayCommand = cmd.NewHandler(".")
	t.Cleanup(func() { global.RelayCommand = old })

	var got []string
	var gotParam any
	global.RelayCommand.RegisterWith("echo", "<msg>", "#test", func(args []string, param any) {
		got = args
		gotParam = param
	})

	o := gameio.NewGameOutputStream()
	o.WriteString(".echo hi")
	_ = o.WriteByte(0)
	o.WriteIsString(host.Name()) // 署名匹配
	_ = hostTc.ProcessConnect(o.CreatePacket(packettype.CHAT))

	if len(got) != 1 || got[0] != "hi" {
		t.Fatalf("echo 参数 = %v", got)
	}
	if gotParam != any(host) {
		t.Fatal("命令参数应为发起连接")
	}
}

// TestRelayHostChatNameMismatch CHAT 署名不匹配时不执行命令
func TestRelayHostChatNameMismatch(t *testing.T) {
	hostTc, host, _ := newRelayConnect(t, "127.0.0.1")
	driveToCertifiedEnd(t, hostTc, host, "alice", "aabbcc01")
	_ = hostTc.ProcessConnect(typeReplyPacket("new"))
	t.Cleanup(func() { host.Room().Re() })

	old := global.RelayCommand
	global.RelayCommand = cmd.NewHandler(".")
	t.Cleanup(func() { global.RelayCommand = old })

	called := false
	global.RelayCommand.Register("ping", "#test", func([]string, any) { called = true })

	o := gameio.NewGameOutputStream()
	o.WriteString(".ping")
	_ = o.WriteByte(0)
	o.WriteIsString("somebody-else")
	_ = hostTc.ProcessConnect(o.CreatePacket(packettype.CHAT))

	if called {
		t.Fatal("署名不匹配时不应执行命令")
	}
}

// TestRelayHostDisconnect 对应 hostProcessing 的 DISCONNECT 分支
func TestRelayHostDisconnect(t *testing.T) {
	hostTc, host, _ := newRelayConnect(t, "127.0.0.1")
	driveToCertifiedEnd(t, hostTc, host, "alice", "aabbcc01")
	_ = hostTc.ProcessConnect(typeReplyPacket("new"))
	t.Cleanup(func() { host.Room().Re() })

	_ = hostTc.ProcessConnect(packet.NewPacket(packettype.DISCONNECT, []byte{}))

	if !host.IsDis {
		t.Fatal("HOST DISCONNECT 后应断开")
	}
}

// --- 健壮性 ---

// TestRelayProcessConnectRecoversFromPanic 上游 @Throws(Exception) 由 netty 兜底;
// Go 侧入口 recover, 一条坏包不带走连接.
func TestRelayProcessConnectRecoversFromPanic(t *testing.T) {
	tc, con, _ := newRelayConnect(t, "127.0.0.1")

	// 无房间却处于 PlayerJoinPermission: ACCEPT_START_GAME 的 room!! 等价 panic
	con.SetPermissionStatus(core.RelayPlayerJoinPermission)

	if err := tc.ProcessConnect(packet.NewPacket(packettype.ACCEPT_START_GAME, []byte{})); err != nil {
		t.Fatalf("ProcessConnect 不应返回错误: %v", err)
	}
	if con.IsDis {
		t.Fatal("panic 被 recover 后连接不应断开")
	}
}

// TestRelayTypeConnectFactoryAndVersion 工厂形态与版本号
func TestRelayTypeConnectFactoryAndVersion(t *testing.T) {
	factory := NewTypeRelayFactory()

	if factory.Version() != "RELAY 1.1.0" {
		t.Fatalf("version = %q", factory.Version())
	}
	if factory.AbstractNetConnect() != nil {
		t.Fatal("工厂形态不应绑定连接")
	}

	w := &relayCaptureWriter{}
	ca := core.NewUDPConnectionAgreement(w, NewRelayRwHps(core.RelayProtocol), "127.0.0.1:9999", 5123)
	tc := factory.GetTypeConnect(ca)

	if tc == nil {
		t.Fatal("GetTypeConnect 应返回连接处理器")
	}
	if tc.AbstractNetConnect() == nil {
		t.Fatal("连接形态应绑定 AbstractNetConnect")
	}
	if tc.Version() != "RELAY 1.1.0" {
		t.Fatalf("version = %q", tc.Version())
	}
}

// TestRelayRwHpsShape RelayRwHps 的三件套装配
func TestRelayRwHpsShape(t *testing.T) {
	rw := NewRelayRwHps(core.RelayProtocol)

	if rw.NetType() != core.RelayProtocol {
		t.Fatalf("netType = %v", rw.NetType())
	}
	if rw.TypeConnect() == nil {
		t.Fatal("typeConnect 不应为 nil")
	}
	if rw.AbstractNetPacket() == nil {
		t.Fatal("abstractNetPacket 不应为 nil (回落 GameVersionPacket)")
	}
}
