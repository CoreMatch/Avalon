package relay

// RelayClientCommands 单测.
//
// 连接与房间全部经 realize 的导出 API 构建 (握手解析 + RelayDirectInspection),
// 不触碰私有字段; 消息回送经 manage.SetProvider 注入假 IRwHps, 让
// net.NetStaticData.RwHps().AbstractNetPacket() 把消息原文直接放进包字节,
// 断言不必解析真实聊天包布局.

import (
	"math"
	"strings"
	"sync"
	"testing"

	"rwhps/internal/data/global"
	"rwhps/internal/game/manage"
	"rwhps/internal/game/room"
	gameio "rwhps/internal/io"
	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
	"rwhps/internal/net"
	"rwhps/internal/net/core"
	netpacket "rwhps/internal/net/core/server/netpacket"
	"rwhps/internal/net/netconnectprotocol/realize"
	"rwhps/internal/struct/list"
	cmd "rwhps/internal/util/game/command"
	"rwhps/internal/util/timeutil"
)

// --- 测试基建 ---

type cmdCaptureWriter struct {
	mu      sync.Mutex
	packets []*packet.Packet
	closed  bool
}

func (c *cmdCaptureWriter) WritePacket(p *packet.Packet) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.packets = append(c.packets, p.Clone())
	return nil
}

func (c *cmdCaptureWriter) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return nil
}

func (c *cmdCaptureWriter) IsClosedUDP() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *cmdCaptureWriter) snapshot() []*packet.Packet {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*packet.Packet, len(c.packets))
	copy(out, c.packets)
	return out
}

// messages 返回已发包的字节 (fakePacketBuilder 把消息原文直接当包体)
func (c *cmdCaptureWriter) messages() []string {
	return c.messagesAfter(0)
}

// messagesAfter 返回第 offset 个包之后的包字节 (跳过命令执行前的握手/建房噪声)
func (c *cmdCaptureWriter) messagesAfter(offset int) []string {
	pkts := c.snapshot()
	out := make([]string, 0, len(pkts))
	for i, p := range pkts {
		if i < offset {
			continue
		}
		out = append(out, string(p.Bytes))
	}
	return out
}

// count 当前已收到的包数 (用作 messagesAfter 的 offset)
func (c *cmdCaptureWriter) count() int { return len(c.snapshot()) }

func (c *cmdCaptureWriter) countType(t packettype.PacketType) int {
	n := 0
	for _, p := range c.snapshot() {
		if packettype.PacketType(p.Type.TypeInt()) == t {
			n++
		}
	}
	return n
}

// fakePacketBuilder 把消息原文放进包字节, 断言不必解析真实布局
type fakePacketBuilder struct{}

func (fakePacketBuilder) GetSystemMessagePacket(msg string) (*packet.Packet, error) {
	return packet.NewPacket(packettype.CHAT, []byte(msg)), nil
}
func (fakePacketBuilder) GetChatMessagePacket(msg, _ string, _ int) (*packet.Packet, error) {
	return packet.NewPacket(packettype.CHAT, []byte(msg)), nil
}
func (fakePacketBuilder) GetPingPacket(any) (*packet.Packet, error) {
	return packet.NewEmptyPacket(), nil
}
func (fakePacketBuilder) GetTickPacket(int) (*packet.Packet, error) {
	return packet.NewEmptyPacket(), nil
}
func (fakePacketBuilder) GetGameTickCommandPacket(int, *packet.GameCommandPacket) (*packet.Packet, error) {
	return packet.NewEmptyPacket(), nil
}
func (fakePacketBuilder) GetGameTickCommandsPacket(int, *list.Seq[*packet.GameCommandPacket]) (*packet.Packet, error) {
	return packet.NewEmptyPacket(), nil
}
func (fakePacketBuilder) GetPacketServerInfo([]byte) (*packet.ServerInfoPacket, error) {
	return nil, core.ErrNotImplemented
}
func (fakePacketBuilder) GetDeceiveGameSave() *packet.Packet { return packet.NewEmptyPacket() }
func (fakePacketBuilder) GameSummonPacket(int, string, float32, float32, int) *packet.GameCommandPacket {
	return nil
}
func (fakePacketBuilder) GetExitPacket() (*packet.Packet, error) {
	return packet.NewEmptyPacket(), nil
}

type fakeRwHps struct{}

func (fakeRwHps) NetType() core.NetType                          { return core.RelayProtocol }
func (fakeRwHps) TypeConnect() core.TypeConnect                  { return nil }
func (fakeRwHps) AbstractNetPacket() netpacket.AbstractNetPacket { return fakePacketBuilder{} }

// installFakeRwHps 让 net.NetStaticData.RwHps() 返回假实现 (sendMsg 的消费者)
func installFakeRwHps(t *testing.T) {
	t.Helper()
	manage.SetProvider(func(core.NetType) core.IRwHps { return fakeRwHps{} })
	manage.AddIRwHps(core.RelayProtocol, "")
	net.NetStaticData.ResetRwHps()
	t.Cleanup(func() {
		manage.Clear()
		net.NetStaticData.ResetRwHps()
	})
}

// installRelayCommand 换上干净的 RelayCommand, 避免全局污染
func installRelayCommand(t *testing.T) *cmd.Handler {
	t.Helper()
	old := global.RelayCommand
	global.RelayCommand = cmd.NewHandler(".")
	t.Cleanup(func() { global.RelayCommand = old })
	return global.RelayCommand
}

// newRelay 构造绑定假传输层的连接
func newRelay(t *testing.T, ip string) (*realize.GameVersionRelay, *cmdCaptureWriter) {
	t.Helper()
	w := &cmdCaptureWriter{}
	ca := core.NewUDPConnectionAgreement(w, fakeRwHps{}, ip+":9999", 5123)
	return realize.NewGameVersionRelay(ca), w
}

// registerPlayerPacket 构造握手用的 REGISTER_PLAYER 包
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

// cachePacketWithQuery 构造 relayDirectInspection 用的缓存包
// (packetVersion=2: 带查询串)
func cachePacketWithQuery(query string) *packet.Packet {
	o := gameio.NewGameOutputStream()
	o.WriteString("x")
	o.WriteInt(2) // packetVersion
	o.WriteInt(151)
	o.WriteInt(0) // packetVersion >= 1 时被 skip(4)
	o.WriteIsString(query)
	return o.CreatePacket(packettype.PREREGISTER_INFO_RECEIVE)
}

// makeHost 经导出 API 建一个新房间并成为房主
func makeHost(t *testing.T, name, uuidHex string) (*realize.GameVersionRelay, *cmdCaptureWriter) {
	t.Helper()
	g, w := newRelay(t, "127.0.0.1")
	g.RelayRegisterConnection(registerPlayerPacket(name, uuidHex))
	g.SetCachePacket(cachePacketWithQuery("new"))
	if err := g.RelayDirectInspection(nil); err != nil {
		t.Fatalf("RelayDirectInspection: %v", err)
	}
	if g.Room() == nil {
		t.Fatal("new 应建房")
	}
	t.Cleanup(func() { g.Room().Re() })
	return g, w
}

// joinPlayer 经导出 API 加入房间并完成游戏注册 (PlayerJoinPermission + PlayerRelay)
func joinPlayer(t *testing.T, r *room.RelayRoom, name, uuidHex, ip string) (*realize.GameVersionRelay, *cmdCaptureWriter) {
	t.Helper()
	p, w := newRelay(t, ip)
	p.RelayRegisterConnection(registerPlayerPacket(name, uuidHex))
	p.SetCachePacket(cachePacketWithQuery(r.ID))
	if err := p.RelayDirectInspection(nil); err != nil {
		t.Fatalf("RelayDirectInspection: %v", err)
	}
	// 第二次 REGISTER_PLAYER: 完成游戏注册 (绑定 PlayerRelay, 推进到 PlayerJoinPermission)
	p.RelayRegisterConnection(packet.NewPacket(packettype.REGISTER_PLAYER, []byte{0}))
	return p, w
}

// --- 注册 ---

// TestRegisterRelayCommandsRegistersClientSet 对应 RelayCommands 的注册链:
// 客户端命令挂到 RELAY_COMMAND.
func TestRegisterRelayCommandsRegistersClientSet(t *testing.T) {
	handler := installRelayCommand(t)

	RegisterRelayCommands(cmd.NewHandler(""))

	want := []string{"help", "myid", "sync", "rp", "kickx", "ban", "allmute"}
	have := map[string]bool{}
	for _, c := range handler.CommandList() {
		have[c.Text] = true
	}
	for _, name := range want {
		if !have[name] {
			t.Fatalf("缺少 Relay 客户端命令 %q", name)
		}
	}
}

// --- 命令行为 ---

// TestRelayCmdHelp 对应 help: 列出全部命令
func TestRelayCmdHelp(t *testing.T) {
	installFakeRwHps(t)
	handler := installRelayCommand(t)
	RegisterRelayCommands(cmd.NewHandler(""))

	host, w := makeHost(t, "alice", "aabbcc")
	before := w.count()
	handler.HandleMessageWith(".help", host)

	msgs := w.messagesAfter(before)
	if len(msgs) != 1 {
		t.Fatalf("help 应发一条消息: %v", msgs)
	}
	for _, name := range []string{"myid", "sync", "kickx", "ban", "allmute"} {
		if !strings.Contains(msgs[0], name) {
			t.Fatalf("help 缺少 %q: %q", name, msgs[0])
		}
	}
}

// TestRelayCmdMyId 对应 myid: UUID-Hex 补码换算后取绝对值
//
// "aabbcc" -> BigInteger(补码) = -5588020 -> 取反 5588020
func TestRelayCmdMyId(t *testing.T) {
	installFakeRwHps(t)
	handler := installRelayCommand(t)
	RegisterRelayCommands(cmd.NewHandler(""))

	host, w := makeHost(t, "alice", "aabbcc")
	before := w.count()
	handler.HandleMessageWith(".myid", host)

	msgs := w.messagesAfter(before)
	if len(msgs) != 1 || msgs[0] != "5588020" {
		t.Fatalf("myid = %v, want [5588020]", msgs)
	}
}

// TestRelayCmdSync 对应 sync: 房主切换重连开关
func TestRelayCmdSync(t *testing.T) {
	installFakeRwHps(t)
	handler := installRelayCommand(t)
	RegisterRelayCommands(cmd.NewHandler(""))

	host, w := makeHost(t, "alice", "aabbcc")
	if !host.Room().SyncFlag {
		t.Fatal("SyncFlag 初始应为 true")
	}

	before := w.count()
	handler.HandleMessageWith(".sync off", host)
	if host.Room().SyncFlag {
		t.Fatal(".sync off 后 SyncFlag 应为 false")
	}
	if msgs := w.messagesAfter(before); len(msgs) != 1 || msgs[0] != "server.sync" {
		t.Fatalf("应回 server.sync 消息: %v", msgs)
	}

	handler.HandleMessageWith(".sync on", host)
	if !host.Room().SyncFlag {
		t.Fatal(".sync on 后 SyncFlag 应为 true")
	}
}

// TestRelayCmdSyncNonAdminSilent 对应 sync 的 isAdmin(con, false):
// 非房主使用时静默拒绝 (不发消息).
func TestRelayCmdSyncNonAdminSilent(t *testing.T) {
	installFakeRwHps(t)
	handler := installRelayCommand(t)
	RegisterRelayCommands(cmd.NewHandler(""))

	host, _ := makeHost(t, "alice", "aabbcc")
	player, w := joinPlayer(t, host.Room(), "bob", "ddeeff", "127.0.0.2")
	before := w.count()

	handler.HandleMessageWith(".sync off", player)

	if !host.Room().SyncFlag {
		t.Fatal("非房主不应改动 SyncFlag")
	}
	if msgs := w.messagesAfter(before); len(msgs) != 0 {
		t.Fatalf("isAdmin(_, false) 不应发消息: %v", msgs)
	}
}

// TestRelayCmdNonAdminGetsNoAdmin 对应 isAdmin 的 err.noAdmin 提示
func TestRelayCmdNonAdminGetsNoAdmin(t *testing.T) {
	installFakeRwHps(t)
	handler := installRelayCommand(t)
	RegisterRelayCommands(cmd.NewHandler(""))

	host, _ := makeHost(t, "alice", "aabbcc")
	player, w := joinPlayer(t, host.Room(), "bob", "ddeeff", "127.0.0.2")
	before := w.count()

	handler.HandleMessageWith(".allmute", player)

	if msgs := w.messagesAfter(before); len(msgs) != 1 || msgs[0] != "err.noAdmin" {
		t.Fatalf("非房主应收到 err.noAdmin: %v", msgs)
	}
	if host.Room().AllMute {
		t.Fatal("非房主不应改动禁言状态")
	}
}

// TestRelayCmdAllMute 对应 allmute: 房主切换全员禁言
func TestRelayCmdAllMute(t *testing.T) {
	installFakeRwHps(t)
	handler := installRelayCommand(t)
	RegisterRelayCommands(cmd.NewHandler(""))

	host, _ := makeHost(t, "alice", "aabbcc")

	handler.HandleMessageWith(".allmute", host)
	if !host.Room().AllMute {
		t.Fatal("allmute 后应禁言")
	}
	handler.HandleMessageWith(".allmute", host)
	if host.Room().AllMute {
		t.Fatal("再次 allmute 应取消禁言")
	}
}

// TestRelayCmdKickx 对应 kickx: 踢出目标并记录 60 秒踢出时间
func TestRelayCmdKickx(t *testing.T) {
	installFakeRwHps(t)
	handler := installRelayCommand(t)
	RegisterRelayCommands(cmd.NewHandler(""))

	host, _ := makeHost(t, "alice", "aabbcc")
	player, pw := joinPlayer(t, host.Room(), "bob", "ddeeff", "127.0.0.2")

	handler.HandleMessageWith(".kickx bob", host)

	if !player.IsDis {
		t.Fatal("目标应被断开")
	}
	if pw.countType(packettype.KICK) != 1 {
		t.Fatalf("目标应收到 KICK: %d", pw.countType(packettype.KICK))
	}
	v, ok := host.Room().RelayKickData.Get("KICK" + player.PlayerRelay().UUID)
	if !ok {
		t.Fatal("应记录 KICK<uuid>")
	}
	if v <= timeutil.ConcurrentSecond() || v > timeutil.ConcurrentSecond()+61 {
		t.Fatalf("KICK 过期时间 = %d, 应为 now+60", v)
	}
}

// TestRelayCmdKickxRightAfterStart 对应 kickx 的时间分支: 刚开局 (10 分钟内)
// 仍可踢人. (开局超过 10 分钟的拒绝分支依赖私有 startGameTime 的时效,
// 无法在单测内快进, 由代码走查覆盖.)
func TestRelayCmdKickxRightAfterStart(t *testing.T) {
	installFakeRwHps(t)
	handler := installRelayCommand(t)
	RegisterRelayCommands(cmd.NewHandler(""))

	host, _ := makeHost(t, "alice", "aabbcc")
	player, _ := joinPlayer(t, host.Room(), "bob", "ddeeff", "127.0.0.2")

	// set 回调把 startGameTime 记为 now+300, startGameTime+600 > now, 不触发拒绝
	host.Room().SetIsStartGame(true)

	handler.HandleMessageWith(".kickx bob", host)

	if !player.IsDis {
		t.Fatal("开局 10 分钟内应仍可踢人")
	}
}

// TestRelayCmdBan 对应 ban: 永久踢出并封禁 IP
func TestRelayCmdBan(t *testing.T) {
	installFakeRwHps(t)
	handler := installRelayCommand(t)
	RegisterRelayCommands(cmd.NewHandler(""))

	host, _ := makeHost(t, "alice", "aabbcc")
	player, pw := joinPlayer(t, host.Room(), "bob", "ddeeff", "127.0.0.2")

	handler.HandleMessageWith(".ban bob", host)

	if !player.IsDis {
		t.Fatal("目标应被断开")
	}
	if pw.countType(packettype.KICK) != 1 {
		t.Fatal("目标应收到 KICK")
	}
	if v, ok := host.Room().RelayKickData.Get("KICK" + player.PlayerRelay().UUID); !ok || v != math.MaxInt32 {
		t.Fatalf("KICK<uuid> = %d, want MaxInt32", v)
	}
	if v, ok := host.Room().RelayKickData.Get("BAN" + player.IP()); !ok || v != math.MaxInt32 {
		t.Fatalf("BAN<ip> = %d, want MaxInt32", v)
	}
}

// TestRelayCmdRpNotStarted 对应 rp: 房间未开始时拒绝
func TestRelayCmdRpNotStarted(t *testing.T) {
	installFakeRwHps(t)
	handler := installRelayCommand(t)
	RegisterRelayCommands(cmd.NewHandler(""))

	host, w := makeHost(t, "alice", "aabbcc")
	joinPlayer(t, host.Room(), "bob", "ddeeff", "127.0.0.2")
	before := w.count()

	handler.HandleMessageWith(".rp bob", host)

	if msgs := w.messagesAfter(before); len(msgs) != 1 || msgs[0] != "房间未开始游戏" {
		t.Fatalf("未开局应提示: %v", msgs)
	}
	if host.Room().ReplacePlayerHex != "" {
		t.Fatal("未开局不应设置取代目标")
	}
}

// TestRelayCmdRpOnlineTarget 对应 rp 的在线检查分支: 在线目标不可取代.
//
// 有意修正（作者已批准，2026-11-04）: 上游在线检查用连接名比 UUID
// (RelayClientCommands.kt rp), 恒不命中, 在线目标也会被标记取代.
// 按作者决定改用 conTg[0] (UUID) 后, 在线目标应被拒绝 (DECISIONS D-12).
func TestRelayCmdRpOnlineTarget(t *testing.T) {
	installFakeRwHps(t)
	handler := installRelayCommand(t)
	RegisterRelayCommands(cmd.NewHandler(""))

	host, w := makeHost(t, "alice", "aabbcc")
	joinPlayerForRp(t, host)
	host.Room().SetIsStartGame(true)
	before := w.count()

	handler.HandleMessageWith(".rp bob", host)

	// 修正后: 目标在线, 在线检查命中, 不设置 replacePlayerHex
	if host.Room().ReplacePlayerHex != "" {
		t.Fatalf("在线目标不应被取代, replacePlayerHex = %q", host.Room().ReplacePlayerHex)
	}
	found := false
	for _, m := range w.messagesAfter(before) {
		if strings.Contains(m, "还在线, 不能取代") {
			found = true
		}
	}
	if !found {
		t.Fatalf("应提示目标在线: %v", w.messagesAfter(before))
	}
}

// joinPlayerForRp 供 rp 测试复用: 加入房间并完成游戏注册
func joinPlayerForRp(t *testing.T, host *realize.GameVersionRelay) *realize.GameVersionRelay {
	t.Helper()
	p, _ := joinPlayer(t, host.Room(), "bob", "ddeeff", "127.0.0.2")
	return p
}

// TestRelayCmdRpOfflineTarget 对应 rp: 开局后离线的目标可被取代,
// 其 UUID-Hex 记入 replacePlayerHex.
func TestRelayCmdRpOfflineTarget(t *testing.T) {
	installFakeRwHps(t)
	handler := installRelayCommand(t)
	RegisterRelayCommands(cmd.NewHandler(""))

	host, w := makeHost(t, "alice", "aabbcc")
	player, _ := joinPlayer(t, host.Room(), "bob", "ddeeff", "127.0.0.2")
	host.Room().SetIsStartGame(true)

	// 开局后断开: relayPlayersData 保留 (上游同语义), 房间表里移除
	player.Disconnect()

	before := w.count()
	handler.HandleMessageWith(".rp bob", host)

	if host.Room().ReplacePlayerHex != player.RegisterPlayerId() {
		t.Fatalf("replacePlayerHex = %q, want %q", host.Room().ReplacePlayerHex, player.RegisterPlayerId())
	}
	found := false
	for _, m := range w.messagesAfter(before) {
		if strings.Contains(m, "准备取代玩家") {
			found = true
		}
	}
	if !found {
		t.Fatalf("应提示准备取代: %v", w.messagesAfter(before))
	}
}
