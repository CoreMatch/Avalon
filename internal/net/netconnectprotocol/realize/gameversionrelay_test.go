package realize

// GameVersionRelay 单测: 房间生命周期 (创建/加入/房主迁移/断开清理/房间关闭)
// 与关键成员行为.
//
// 手法参照 gameserver_test.go: captureWriter 假传输层 (走 UDP 接缝, 不起真实
// TCP), fakeRwHps 提供包构造器. 每个测试结束前把创建的房间 Re() 掉,
// 避免污染 room 包的全局房间表.

import (
	"strings"
	"sync"
	"testing"

	"rwhps/internal/data/global"
	"rwhps/internal/game/room"
	gameio "rwhps/internal/io"
	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
	"rwhps/internal/net/core"
	netpacket "rwhps/internal/net/core/server/netpacket"
	"rwhps/internal/util/timeutil"
)

// --- 测试基建 ---

// captureWriter 捕获发包内容的假传输层
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

// types 返回已发包的类型序列
func (c *captureWriter) types() []packettype.PacketType {
	pkts := c.snapshot()
	out := make([]packettype.PacketType, 0, len(pkts))
	for _, p := range pkts {
		out = append(out, packettype.PacketType(p.Type.TypeInt()))
	}
	return out
}

// countType 统计某类型包的数量
func (c *captureWriter) countType(t packettype.PacketType) int {
	n := 0
	for _, p := range c.snapshot() {
		if packettype.PacketType(p.Type.TypeInt()) == t {
			n++
		}
	}
	return n
}

// lastOfType 返回某类型的最后一个包
func (c *captureWriter) lastOfType(t packettype.PacketType) *packet.Packet {
	var last *packet.Packet
	for _, p := range c.snapshot() {
		if packettype.PacketType(p.Type.TypeInt()) == t {
			last = p
		}
	}
	return last
}

// fakeRwHps 为连接提供包构造器 (对应 NetStaticData.RwHps 的测试替身)
type fakeRwHps struct{}

func (fakeRwHps) NetType() core.NetType                          { return core.RelayProtocol }
func (fakeRwHps) TypeConnect() core.TypeConnect                  { return nil }
func (fakeRwHps) AbstractNetPacket() netpacket.AbstractNetPacket { return NewGameVersionPacket() }

// newTestRelay 构造绑定假传输层的 GameVersionRelay
func newTestRelay(t *testing.T, ip string) (*GameVersionRelay, *captureWriter) {
	t.Helper()
	w := &captureWriter{}
	ca := core.NewUDPConnectionAgreement(w, fakeRwHps{}, ip+":9999", 5123)
	return NewGameVersionRelay(ca), w
}

// cleanupRoom 测试收尾: 关闭房间 (从全局房间表注销)
func cleanupRoom(t *testing.T, g *GameVersionRelay) {
	t.Helper()
	t.Cleanup(func() {
		if g.room != nil {
			g.room.Re()
		}
	})
}

// makeHost 走 idCustom("new") 建一个新房间并成为房主
func makeHost(t *testing.T, name, registerID string) (*GameVersionRelay, *captureWriter) {
	t.Helper()
	g, w := newTestRelay(t, "127.0.0.1")
	g.name = name
	g.registerPlayerId = registerID
	g.idCustom("new")
	if g.room == nil {
		t.Fatal("idCustom(new) 应创建房间")
	}
	cleanupRoom(t, g)
	return g, w
}

// joinRoom 以玩家身份加入指定房间 (走 idCustom(房间ID) -> addRelayConnect)
func joinRoom(t *testing.T, r *room.RelayRoom, name, registerID, ip string) (*GameVersionRelay, *captureWriter) {
	t.Helper()
	p, w := newTestRelay(t, ip)
	p.name = name
	p.registerPlayerId = registerID
	// addRelayConnect 会 sendPackageToHOST(cachePacket!!), 加入前先备好缓存包
	p.cachePacket = packet.NewPacket(packettype.PREREGISTER_INFO_RECEIVE, []byte{1, 2, 3})
	p.idCustom(r.ID)
	return p, w
}

// --- 初始状态 ---

// TestNewGameVersionRelayDefaults 对应上游字段初始值
func TestNewGameVersionRelayDefaults(t *testing.T) {
	g, _ := newTestRelay(t, "127.0.0.1")

	if g.Name() != "NOT NAME" {
		t.Fatalf("name = %q", g.Name())
	}
	if g.ClientVersion() != 151 {
		t.Fatalf("clientVersion = %d", g.ClientVersion())
	}
	if g.Site() != -1 {
		t.Fatalf("site = %d", g.Site())
	}
	if g.PermissionStatus() != core.RelayInitialConnection {
		t.Fatalf("permissionStatus = %v", g.PermissionStatus())
	}
	if g.Version() != "1.15 RELAY" {
		t.Fatalf("version = %q", g.Version())
	}
	if g.Room() != nil {
		t.Fatal("新连接不应有房间")
	}
	if g.StartGameFlag() != true {
		t.Fatal("startGameFlag 初始应为 true")
	}
}

// --- 创建房间 / 成为房主 ---

// TestRelayRoomCreateAndBecomeHost 对应 idCustom 的 newRoom 分支 +
// sendRelayServerId: 建房者成为房主 (HostPermission), 收到 RELAY_BECOME_SERVER
// 与两条管理员聊天.
func TestRelayRoomCreateAndBecomeHost(t *testing.T) {
	g, w := makeHost(t, "alice", "aabbcc")

	if g.PermissionStatus() != core.RelayHostPermission {
		t.Fatalf("房主状态 = %v, want HostPermission", g.PermissionStatus())
	}
	if g.room.Admin() != room.RelayConnect(g) {
		t.Fatal("admin 应为建房者自己")
	}
	if g.Site() != -1 {
		t.Fatalf("首任房主 site 应保持 -1, got %d", g.Site())
	}
	if g.room.GetSize() != 1 {
		t.Fatalf("size = %d, want 1 (setAddSize)", g.room.GetSize())
	}
	if g.room.IsMod {
		t.Fatal("new (无 mod 前缀) 建房 isMod 应为 false")
	}

	if w.countType(packettype.RELAY_BECOME_SERVER) != 1 {
		t.Fatalf("RELAY_BECOME_SERVER 数量 = %d", w.countType(packettype.RELAY_BECOME_SERVER))
	}
	if w.countType(packettype.CHAT) != 2 {
		t.Fatalf("管理员聊天包数量 = %d, want 2", w.countType(packettype.CHAT))
	}

	// RELAY_BECOME_SERVER 包布局 (clientVersion 151 < 172 走旧协议):
	// byte(1) + allowForwarding + removeConnection + IsString(serverUuid) + mod + public + msg + multicast
	b := w.lastOfType(packettype.RELAY_BECOME_SERVER)
	in := gameio.NewGameInputStreamFromPacket(b, 0)
	if v := in.ReadSignedByte(); v != 1 {
		t.Fatalf("packetVersion = %d, want 1 (151 < 172)", v)
	}
	if !in.ReadBoolean() || !in.ReadBoolean() {
		t.Fatal("allowThisConnectionForwarding/removeThisConnection 应为 true")
	}
	if uuid := in.ReadIsString(); uuid != g.room.ServerUUID {
		t.Fatalf("serverUuid = %q", uuid)
	}
	if in.ReadBoolean() {
		t.Fatal("useMods 应为 false")
	}
	if in.ReadBoolean() {
		t.Fatal("showPublicly 应为 false (上游恒 false)")
	}
	msg := in.ReadString()
	if !strings.Contains(msg, global.ConfigRelay.MainID+g.room.ID) {
		t.Fatalf("relayMessageOnServer 缺少房间号: %q", msg)
	}
	if in.ReadBoolean() {
		t.Fatal("multicast 应为 false")
	}
}

// TestIdCustomCustomModePackets 对应 newRelayId 的自定义参数分支:
// "newP5" 触发 customModePlayerSize, 在 RELAY_BECOME_SERVER 之前
// 先发 PREREGISTER_INFO / SERVER_INFO / TEAM_LIST 三个伪造握手包.
func TestIdCustomCustomModePackets(t *testing.T) {
	g, w := newTestRelay(t, "127.0.0.1")
	g.name = "alice"
	g.registerPlayerId = "aabbcc"

	g.idCustom("newP5")
	if g.room == nil {
		t.Fatal("idCustom(newP5) 应创建房间")
	}
	cleanupRoom(t, g)

	types := w.types()
	idxBecome := -1
	for i, tp := range types {
		if tp == packettype.RELAY_BECOME_SERVER {
			idxBecome = i
			break
		}
	}
	if idxBecome < 0 {
		t.Fatalf("缺少 RELAY_BECOME_SERVER: %v", types)
	}
	// 三个自定义包必须出现在 RELAY_BECOME_SERVER 之前
	want := []packettype.PacketType{packettype.PREREGISTER_INFO, packettype.SERVER_INFO, packettype.TEAM_LIST}
	for _, tp := range want {
		found := false
		for i := 0; i < idxBecome; i++ {
			if types[i] == tp {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("自定义包 %v 未出现在 RELAY_BECOME_SERVER 之前: %v", tp, types)
		}
	}

	// TEAM_LIST 包里的 maxPlayer 应为 5
	team := w.lastOfType(packettype.TEAM_LIST)
	in := gameio.NewGameInputStreamFromPacket(team, 0)
	in.ReadInt()          // 0
	in.ReadBoolean()      // false
	if n := in.ReadInt(); n != 5 {
		t.Fatalf("maxPlayerSize = %d, want 5", n)
	}
}

// TestIdCustomRejectsEmoji 对应 EmojiManager.containsEmoji 检查
func TestIdCustomRejectsEmoji(t *testing.T) {
	g, w := newTestRelay(t, "127.0.0.1")
	g.name = "alice"
	g.registerPlayerId = "aabbcc"

	g.idCustom("abc😀")

	if g.room != nil {
		t.Fatal("含 Emoji 的 ID 不应建房")
	}
	if w.countType(packettype.RELAY_117) != 1 {
		t.Fatalf("应回送 RELAY_117 错误提示, got %d", w.countType(packettype.RELAY_117))
	}
}

// --- 加入房间 ---

// TestRelayJoinExistingRoom 对应 idCustom 的加入分支 + addRelayConnect:
// 玩家拿到 site, 房主收到 FORWARD_CLIENT_ADD, 玩家收到欢迎聊天.
func TestRelayJoinExistingRoom(t *testing.T) {
	host, hostW := makeHost(t, "alice", "aabbcc")

	p, pw := joinRoom(t, host.room, "bob", "ddeeff", "127.0.0.2")

	if p.PermissionStatus() != core.RelayPlayerPermission {
		t.Fatalf("玩家状态 = %v, want PlayerPermission", p.PermissionStatus())
	}
	if p.Site() != 1 {
		t.Fatalf("玩家 site = %d, want 1", p.Site())
	}
	if host.room.GetSize() != 2 {
		t.Fatalf("size = %d, want 2", host.room.GetSize())
	}
	if _, ok := host.room.GetAbstractNetConnect(1); !ok {
		t.Fatal("房间表里应有 site=1 的玩家")
	}

	// 房主收到 FORWARD_CLIENT_ADD (旧协议: byte(0) + site + uuid + IsString(null))
	add := hostW.lastOfType(packettype.FORWARD_CLIENT_ADD)
	if add == nil {
		t.Fatal("房主应收到 FORWARD_CLIENT_ADD")
	}
	in := gameio.NewGameInputStreamFromPacket(add, 0)
	if v := in.ReadSignedByte(); v != 0 {
		t.Fatalf("FORWARD_CLIENT_ADD 版本字节 = %d, want 0 (clientVersion 151)", v)
	}
	if site := in.ReadInt(); site != 1 {
		t.Fatalf("FORWARD_CLIENT_ADD site = %d", site)
	}
	if uuid := in.ReadString(); uuid != "ddeeff" {
		t.Fatalf("FORWARD_CLIENT_ADD uuid = %q", uuid)
	}
	if in.ReadBoolean() {
		t.Fatal("User UUID 位应为 false (writeIsString(null))")
	}

	// 玩家自己收到欢迎聊天 (relay 语言包)
	if pw.countType(packettype.CHAT) != 1 {
		t.Fatalf("玩家欢迎聊天数量 = %d, want 1", pw.countType(packettype.CHAT))
	}

	// 玩家的注册包被转发给房主 (sendPackageToHOST(cachePacket))
	if hostW.countType(packettype.PACKET_FORWARD_CLIENT_FROM) != 1 {
		t.Fatalf("房主应收到 1 个转发包, got %d", hostW.countType(packettype.PACKET_FORWARD_CLIENT_FROM))
	}
}

// --- 断开清理 ---

// TestPlayerDisconnectCleanup 对应 disconnect 的非房主分支:
// 移出房间表 + 通知房主删除转发 ID + 未开局时清理玩家数据.
func TestPlayerDisconnectCleanup(t *testing.T) {
	host, hostW := makeHost(t, "alice", "aabbcc")
	p, _ := joinRoom(t, host.room, "bob", "ddeeff", "127.0.0.2")

	p.Disconnect()

	if !p.IsDis {
		t.Fatal("玩家应已断开")
	}
	if _, ok := host.room.GetAbstractNetConnect(1); ok {
		t.Fatal("房间表里不应再有 site=1")
	}
	if host.room.GetSize() != 1 {
		t.Fatalf("size = %d, want 1", host.room.GetSize())
	}
	// 房主收到 FORWARD_CLIENT_REMOVE (经 sendPackageToHOST 包成 PACKET_FORWARD_CLIENT_FROM)
	fwd := hostW.lastOfType(packettype.PACKET_FORWARD_CLIENT_FROM)
	if fwd == nil {
		t.Fatal("房主应收到玩家断开的转发通知")
	}
	in := gameio.NewGameInputStreamFromPacket(fwd, 0)
	in.ReadInt() // site
	in.ReadInt() // len+8
	in.ReadInt() // len
	if tp := in.ReadInt(); tp != int32(packettype.FORWARD_CLIENT_REMOVE) {
		t.Fatalf("转发包内层类型 = %d, want FORWARD_CLIENT_REMOVE", tp)
	}
	if host.room.CloseRoom() {
		t.Fatal("还有房主, 房间不应关闭")
	}
}

// TestHostMigrationOnHostDisconnect 对应 disconnect 的房主分支 + adminMoveNew:
// 房主断开且房间非空时, 延迟最低的玩家接任房主.
func TestHostMigrationOnHostDisconnect(t *testing.T) {
	host, _ := makeHost(t, "alice", "aabbcc")
	p, pw := joinRoom(t, host.room, "bob", "ddeeff", "127.0.0.2")
	// 房主迁移时旧房主的 addReRelayConnect 需要 cachePacket
	host.cachePacket = packet.NewPacket(packettype.PREREGISTER_INFO_RECEIVE, []byte{9})

	host.Disconnect()

	if !host.IsDis {
		t.Fatal("旧房主应已断开")
	}
	// 新房主: 状态打成 HostPermission, site 置 -2 (第二任)
	if p.PermissionStatus() != core.RelayHostPermission {
		t.Fatalf("新房主状态 = %v", p.PermissionStatus())
	}
	if p.Site() != -2 {
		t.Fatalf("新房主 site = %d, want -2", p.Site())
	}
	if host.room.Admin() != room.RelayConnect(p) {
		t.Fatal("admin 应迁移给玩家")
	}
	if host.room.CloseRoom() {
		t.Fatal("仍有玩家, 房间不应关闭")
	}
	// 新房主收到 RELAY_BECOME_SERVER (sendRelayServerId)
	if pw.countType(packettype.RELAY_BECOME_SERVER) != 1 {
		t.Fatalf("新房主应收到 RELAY_BECOME_SERVER, got %d", pw.countType(packettype.RELAY_BECOME_SERVER))
	}
	// 新房主收到旧房主的重新注册 (addReRelayConnect: FORWARD_CLIENT_ADD x2 + 缓存包转发)
	if pw.countType(packettype.FORWARD_CLIENT_ADD) < 2 {
		t.Fatalf("新房主应收到旧房主的重新注册包, got %d", pw.countType(packettype.FORWARD_CLIENT_ADD))
	}
}

// TestRoomCloseWhenLastConnectionLeaves 对应 disconnect 房主分支的房间关闭:
// 最后一个连接离开时 re() 关房并从全局房间表注销.
func TestRoomCloseWhenLastConnectionLeaves(t *testing.T) {
	host, _ := makeHost(t, "alice", "aabbcc")
	r := host.room
	id := r.ID

	host.Disconnect()

	if !r.CloseRoom() {
		t.Fatal("房间应已关闭")
	}
	if got, _ := room.GetRelay(id); got != nil {
		t.Fatal("关闭的房间应从全局房间表注销")
	}
}

// TestDisconnectBeforeJoin 未加入房间的连接断开走 close(null) 分支
func TestDisconnectBeforeJoin(t *testing.T) {
	g, w := newTestRelay(t, "127.0.0.1")

	g.Disconnect()

	if !g.IsDis {
		t.Fatal("应已断开")
	}
	if !w.IsClosedUDP() {
		t.Fatal("传输层应关闭")
	}
}

// --- 玩家注册 (relayRegisterConnection) ---

// TestRelayRegisterConnectionFirstParse 对应首次调用: 从 REGISTER_PLAYER 包
// 解析名字与 UUID-Hex.
func TestRelayRegisterConnectionFirstParse(t *testing.T) {
	g, _ := newTestRelay(t, "127.0.0.1")

	o := gameio.NewGameOutputStream()
	o.WriteString("net.rwhps.server")
	o.WriteBytes(make([]byte, 12))
	o.WriteString("tester")
	o.WriteIsString("")
	o.WriteString("extra")
	o.WriteString("00112233")
	g.RelayRegisterConnection(o.CreatePacket(packettype.REGISTER_PLAYER))

	if g.Name() != "tester" {
		t.Fatalf("name = %q", g.Name())
	}
	if g.RegisterPlayerId() != "00112233" {
		t.Fatalf("registerPlayerId = %q", g.RegisterPlayerId())
	}
	if g.PermissionStatus() != core.RelayInitialConnection {
		t.Fatalf("首次解析不应推进状态: %v", g.PermissionStatus())
	}
}

// TestRelayRegisterUUIDDuplicateKick 对应 UUID 重复检测:
// 房间内已有 PlayerJoinPermission 的同 UUID 连接时, 新连接被踢.
func TestRelayRegisterUUIDDuplicateKick(t *testing.T) {
	host, _ := makeHost(t, "alice", "aabbcc")
	p1, _ := joinRoom(t, host.room, "bob", "ddeeff", "127.0.0.2")
	// p1 完成游戏注册
	p1.permissionStatus = core.RelayPlayerJoinPermission

	p2, w2 := joinRoom(t, host.room, "carol", "ddeeff", "127.0.0.3")
	if p2.PermissionStatus() != core.RelayPlayerPermission {
		t.Fatalf("p2 状态 = %v", p2.PermissionStatus())
	}

	// p2 再次发 REGISTER_PLAYER (重连场景) 触发 UUID 检查
	o := gameio.NewGameOutputStream()
	o.WriteString("x")
	p2.RelayRegisterConnection(o.CreatePacket(packettype.REGISTER_PLAYER))

	if w2.countType(packettype.KICK) != 1 {
		t.Fatalf("重复 UUID 应收到 KICK, got %d", w2.countType(packettype.KICK))
	}
	if !p2.IsDis {
		t.Fatal("重复 UUID 的连接应被断开")
	}
}

// TestRelayRegisterUUIDKickWait 对应踢出等待检查: KICK$uuid 命中且未过期时,
// 重连被踢.
//
// 有意修正（作者已批准，2026-11-04）: 上游 ifNullResult 的 blockNotNull 恒返回
// null, KICK$uuid 命中时 time 反而为 null, UUID 踢出记录被吞掉
// (GameVersionRelay.kt relayRegisterConnection). 修正为两级取值后,
// KICK$uuid 命中应真正触发踢出等待 (DECISIONS D-12).
func TestRelayRegisterUUIDKickWait(t *testing.T) {
	host, _ := makeHost(t, "alice", "aabbcc")
	p, w := joinRoom(t, host.room, "bob", "ddeeff", "127.0.0.2")

	// 模拟 kickx 留下的记录: KICK$uuid 未过期 (now+60)
	host.room.RelayKickData.Put("KICK"+p.registerPlayerId, timeutil.ConcurrentSecond()+60)

	// 重连: 再发 REGISTER_PLAYER 触发踢出检查
	o := gameio.NewGameOutputStream()
	o.WriteString("x")
	p.RelayRegisterConnection(o.CreatePacket(packettype.REGISTER_PLAYER))

	k := w.lastOfType(packettype.KICK)
	if k == nil {
		t.Fatal("KICK$uuid 命中应发 KICK 包")
	}
	in := gameio.NewGameInputStreamFromPacket(k, 0)
	if msg := in.ReadString(); !strings.Contains(msg, "踢出等待") {
		t.Fatalf("KICK 消息 = %q, 应含踢出等待", msg)
	}
	if !p.IsDis {
		t.Fatal("应被断开")
	}
}

// TestRelayRegisterKickExpiredCleansUp 对应踢出记录过期分支: 过期后放行并
// 清理 KICK$uuid 与 KICK$ipLong24 两条记录 (上游意图).
func TestRelayRegisterKickExpiredCleansUp(t *testing.T) {
	host, _ := makeHost(t, "alice", "aabbcc")
	p, _ := joinRoom(t, host.room, "bob", "ddeeff", "127.0.0.2")

	host.room.RelayKickData.Put("KICK"+p.registerPlayerId, timeutil.ConcurrentSecond()-1)
	host.room.RelayKickData.Put("KICK"+p.IPLong24(), timeutil.ConcurrentSecond()-1)

	o := gameio.NewGameOutputStream()
	o.WriteString("x")
	p.RelayRegisterConnection(o.CreatePacket(packettype.REGISTER_PLAYER))

	if p.IsDis {
		t.Fatal("踢出记录过期不应断开")
	}
	if host.room.RelayKickData.ContainsKey("KICK" + p.registerPlayerId) {
		t.Fatal("过期的 KICK$uuid 记录应被清理")
	}
	if host.room.RelayKickData.ContainsKey("KICK" + p.IPLong24()) {
		t.Fatal("过期的 KICK$ip 记录应被清理")
	}
}

// --- 转发路径 ---

// TestSendPackageToHOSTLayout 对应 sendPackageToHOST 的包布局:
// site + (len+8) + len + 类型字节 + 原始字节, 以 PACKET_FORWARD_CLIENT_FROM 发给房主.
func TestSendPackageToHOSTLayout(t *testing.T) {
	host, hostW := makeHost(t, "alice", "aabbcc")
	p, _ := joinRoom(t, host.room, "bob", "ddeeff", "127.0.0.2")
	before := hostW.countType(packettype.PACKET_FORWARD_CLIENT_FROM)

	inner := packet.NewPacket(packettype.CHAT_RECEIVE, []byte{0xCA, 0xFE})
	p.SendPackageToHOST(inner)

	if hostW.countType(packettype.PACKET_FORWARD_CLIENT_FROM) != before+1 {
		t.Fatal("房主应收到一个转发包")
	}
	fwd := hostW.lastOfType(packettype.PACKET_FORWARD_CLIENT_FROM)
	in := gameio.NewGameInputStreamFromPacket(fwd, 0)
	if site := in.ReadInt(); site != 1 {
		t.Fatalf("site = %d", site)
	}
	if n := in.ReadInt(); n != int32(len(inner.Bytes)+8) {
		t.Fatalf("len+8 = %d", n)
	}
	if n := in.ReadInt(); n != int32(len(inner.Bytes)) {
		t.Fatalf("len = %d", n)
	}
	if tp := in.ReadInt(); tp != int32(packettype.CHAT_RECEIVE) {
		t.Fatalf("内层类型 = %d", tp)
	}
	if got := in.ReadAllBytes(); len(got) != 2 || got[0] != 0xCA || got[1] != 0xFE {
		t.Fatalf("内层字节 = %v", got)
	}
}

// TestAddRelaySendDeliversToTarget 对应 addRelaySend: HOST 经
// PACKET_FORWARD_CLIENT_TO 把包投递给目标 site 的连接.
func TestAddRelaySendDeliversToTarget(t *testing.T) {
	host, _ := makeHost(t, "alice", "aabbcc")
	p, pw := joinRoom(t, host.room, "bob", "ddeeff", "127.0.0.2")

	o := gameio.NewGameOutputStream()
	o.WriteInt(p.Site())
	o.WriteInt(int32(packettype.CHAT))
	o.WriteBytesAndLength([]byte{1, 2, 3})
	host.AddRelaySend(o.CreatePacket(packettype.PACKET_FORWARD_CLIENT_TO))

	got := pw.lastOfType(packettype.CHAT)
	if got == nil {
		t.Fatalf("目标玩家应收到转发包: %v", pw.types())
	}
	if len(got.Bytes) != 3 {
		t.Fatalf("转发包字节 = %v", got.Bytes)
	}
}

// TestSendPacketExtractInformationStartGame 对应 START_GAME / RETURN_TO_BATTLEROOM
// 两个分支对房间开局标志的维护.
func TestSendPacketExtractInformationStartGame(t *testing.T) {
	host, _ := makeHost(t, "alice", "aabbcc")
	joinRoom(t, host.room, "bob", "ddeeff", "127.0.0.2")

	// START_GAME: skip(1) 后的 Int 非 0 时缓存开局包
	o := gameio.NewGameOutputStream()
	_ = o.WriteByte(0)
	o.WriteInt(1)
	startPacket := o.CreatePacket(packettype.START_GAME)
	host.SendPacketExtractInformation(startPacket, nil)

	if !host.room.IsStartGame() {
		t.Fatal("START_GAME 后房间应开局")
	}
	if host.room.GamePacket.StartGamePacket != startPacket {
		t.Fatal("开局包应被缓存")
	}

	// RETURN_TO_BATTLEROOM: 重置
	host.SendPacketExtractInformation(packet.NewPacket(packettype.RETURN_TO_BATTLEROOM, nil), nil)
	if host.room.IsStartGame() {
		t.Fatal("返回战场后应重置开局标志")
	}
	if host.room.GamePacket.StartGamePacket != nil {
		t.Fatal("返回战场后应清空开局包缓存")
	}
}

// --- PoW 收发 ---

// TestSendVerifyClientValidityPacket 对应 sendVerifyClientValidity 的包布局:
// resultInt + authenticateType + 两个条件块 + 结尾 false.
func TestSendVerifyClientValidityPacket(t *testing.T) {
	g, w := newTestRelay(t, "127.0.0.1")

	g.SendVerifyClientValidity()

	p := w.lastOfType(packettype.RELAY_POW)
	if p == nil {
		t.Fatal("应发出 RELAY_POW 挑战包")
	}
	in := gameio.NewGameInputStreamFromPacket(p, 0)
	in.ReadInt() // resultInt
	authType := int(in.ReadInt())
	if authType < 0 || authType > 6 || authType == 2 {
		t.Fatalf("默认构造不会产生类型 %d", authType)
	}
	// 结尾必须是 false (上游最后一行)
	b := p.Bytes
	if b[len(b)-1] != 0 {
		t.Fatal("RELAY_POW 结尾应为 false")
	}
}

// TestReceiveVerifyClientValidityNoChallenge 没有挑战时恒返回 false
func TestReceiveVerifyClientValidityNoChallenge(t *testing.T) {
	g, _ := newTestRelay(t, "127.0.0.1")

	o := gameio.NewGameOutputStream()
	o.WriteInt(0)
	o.WriteInt(0)
	o.WriteString("0")
	if g.ReceiveVerifyClientValidity(o.CreatePacket(packettype.RELAY_POW_RECEIVE)) {
		t.Fatal("没有挑战时校验必须失败")
	}
}

// --- Kick ---

// TestKickSendsKickAndDisconnects 对应 kick(msg): KICK 包 + 断开
func TestKickSendsKickAndDisconnects(t *testing.T) {
	g, w := newTestRelay(t, "127.0.0.1")

	g.Kick("测试踢出")

	k := w.lastOfType(packettype.KICK)
	if k == nil {
		t.Fatal("应发出 KICK 包")
	}
	in := gameio.NewGameInputStreamFromPacket(k, 0)
	if msg := in.ReadString(); msg != "测试踢出" {
		t.Fatalf("KICK 消息 = %q", msg)
	}
	if !g.IsDis {
		t.Fatal("kick 后应断开")
	}
}
