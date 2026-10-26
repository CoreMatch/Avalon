package room

import (
	"errors"
	"sync"
	"testing"

	"rwhps/internal/game/player"
	"rwhps/internal/io/packet"
	"rwhps/internal/net/core"
	"rwhps/internal/util/javastr"
	"rwhps/internal/util/timeutil"
)

// fakeRelayConnect 是 RelayConnect 的测试实现
type fakeRelayConnect struct {
	name         string
	ip           string
	ipCountry    string
	useAgreement string
	clientVer    int
	site         int32
	lastRecv     int64

	status core.RelayStatus

	registerID string
	player     *player.PlayerRelay
	startFlag  bool

	mu          sync.Mutex
	packets     []*packet.Packet
	sentRelayID []bool
	reRelay     int
	disconnects int
	sendErr     error
}

func (f *fakeRelayConnect) Name() string                   { return f.name }
func (f *fakeRelayConnect) IP() string                     { return f.ip }
func (f *fakeRelayConnect) IPCountry() string              { return f.ipCountry }
func (f *fakeRelayConnect) UseConnectionAgreement() string { return f.useAgreement }
func (f *fakeRelayConnect) ClientVersion() int             { return f.clientVer }
func (f *fakeRelayConnect) Site() int32                    { return f.site }
func (f *fakeRelayConnect) LastReceivedTime() int64        { return f.lastRecv }

func (f *fakeRelayConnect) PermissionStatus() core.RelayStatus { return f.status }
func (f *fakeRelayConnect) SetPermissionStatus(s core.RelayStatus) {
	f.status = s
}

func (f *fakeRelayConnect) RegisterPlayerId() string         { return f.registerID }
func (f *fakeRelayConnect) PlayerRelay() *player.PlayerRelay { return f.player }
func (f *fakeRelayConnect) StartGameFlag() bool              { return f.startFlag }
func (f *fakeRelayConnect) SetStartGameFlag(v bool)          { f.startFlag = v }

func (f *fakeRelayConnect) SendPacket(p *packet.Packet) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return
	}
	f.packets = append(f.packets, p)
}

func (f *fakeRelayConnect) SendRelayServerId(multicast bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sentRelayID = append(f.sentRelayID, multicast)
}

func (f *fakeRelayConnect) AddReRelayConnect() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reRelay++
}

func (f *fakeRelayConnect) Disconnect() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disconnects++
}

func (f *fakeRelayConnect) sentPackets() []*packet.Packet {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*packet.Packet, len(f.packets))
	copy(out, f.packets)
	return out
}

// sentMessages 返回已发包里的消息原文 (配合 fakePacketBuilder)
func (f *fakeRelayConnect) sentMessages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.packets))
	for _, p := range f.packets {
		out = append(out, string(p.Bytes))
	}
	return out
}

// fakePacketBuilder 是 relayPacketBuilder 的测试实现
//
// 把消息原文放进 packet.Bytes, 这样单测不必依赖真实的包编码器 —— 原版走的是
// NetStaticData.RwHps.abstractNetPacket, 而那是 sync.Once 缓存的单例, 装不进去.
type fakePacketBuilder struct {
	mu        sync.Mutex
	systemMsg []string
	chatMsg   []string
}

func (b *fakePacketBuilder) GetSystemMessagePacket(msg string) (*packet.Packet, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.systemMsg = append(b.systemMsg, msg)
	return &packet.Packet{Bytes: []byte(msg)}, nil
}

func (b *fakePacketBuilder) GetChatMessagePacket(msg string, sendBy string, team int) (*packet.Packet, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.chatMsg = append(b.chatMsg, msg)
	return &packet.Packet{Bytes: []byte(msg)}, nil
}

// installFakeBuilder 装上假的包构造器, 返回恢复函数
func installFakeBuilder(t *testing.T) *fakePacketBuilder {
	t.Helper()
	b := &fakePacketBuilder{}
	old := abstractNetPacketProvider
	abstractNetPacketProvider = func() relayPacketBuilder { return b }
	t.Cleanup(func() { abstractNetPacketProvider = old })
	return b
}

func (f *fakeRelayConnect) disconnectCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.disconnects
}

func newFakeCon(site int32, name string) *fakeRelayConnect {
	return &fakeRelayConnect{
		name:         name,
		ip:           "127.0.0.1",
		ipCountry:    "EN",
		useAgreement: "UDP",
		clientVer:    151,
		site:         site,
		lastRecv:     timeutil.ConcurrentMillis(),
		startFlag:    true,
	}
}

func TestCoverRelayIDNumeric(t *testing.T) {
	cases := map[string]int32{
		"1":      1,
		"42":     42,
		"1000":   1000,
		"999999": 999999,
		"+7":     7, // isNumeric 允许前导 +, Kotlin 的 toInt 同样接受
		"-7":     -7,
	}
	for in, want := range cases {
		got, err := CoverRelayID(in)
		if err != nil {
			t.Fatalf("CoverRelayID(%q) returned error: %v", in, err)
		}
		if got != want {
			t.Errorf("CoverRelayID(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestCoverRelayIDNonNumericUsesHashCode(t *testing.T) {
	// 非数字走 hashCode, 且结果为正时要取负
	// (注意 "1.5" 不在这里: isNumeric 的正则允许小数, 所以它走的是 toInt 分支并报错)
	for _, in := range []string{"abc", "RW-HPS", "room-1", "9999999999", "1.2.3"} {
		got, err := CoverRelayID(in)
		if err != nil {
			t.Fatalf("CoverRelayID(%q) returned error: %v", in, err)
		}
		h := javastr.HashCode(in)
		want := h
		if h > 0 {
			want = -h
		}
		if got != want {
			t.Errorf("CoverRelayID(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestCoverRelayIDDecimalIsAnError(t *testing.T) {
	// isNumeric 的正则允许小数, 而原版的 id.toInt() 解析不了 "1.5",
	// 会在 coverRelayID 里抛 NumberFormatException. Go 侧同一个失败变成 error.
	if _, err := CoverRelayID("1.5"); err == nil {
		t.Fatal(`CoverRelayID("1.5") should fail (Kotlin: NumberFormatException)`)
	}
	// 长度 >= 10 的数字不走 toInt 分支, 因此不报错
	if _, err := CoverRelayID("9999999999"); err != nil {
		t.Fatalf(`CoverRelayID("9999999999") should use the hashCode branch, got %v`, err)
	}
}

func TestParseJavaInt(t *testing.T) {
	cases := map[string]int32{"0": 0, "7": 7, "+7": 7, "-7": -7, "2147483647": 2147483647, "-2147483648": -2147483648}
	for in, want := range cases {
		got, err := parseJavaInt(in)
		if err != nil {
			t.Fatalf("parseJavaInt(%q) error: %v", in, err)
		}
		if got != want {
			t.Errorf("parseJavaInt(%q) = %d, want %d", in, got, want)
		}
	}
	for _, in := range []string{"", "+", "-", "1.5", "1a", "2147483648", "-2147483649"} {
		if _, err := parseJavaInt(in); err == nil {
			t.Errorf("parseJavaInt(%q) should fail", in)
		}
	}
}

func TestItoa(t *testing.T) {
	cases := map[int]string{0: "0", 7: "7", -7: "-7", 1000: "1000", 100000: "100000"}
	for in, want := range cases {
		if got := itoa(in); got != want {
			t.Errorf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestSetAdminForcesHostPermission(t *testing.T) {
	r := newSingleRoom("t", nil)
	con := newFakeCon(0, "host")
	con.status = core.RelayInitialConnection

	r.SetAdmin(con)

	if got := con.PermissionStatus(); got != core.RelayHostPermission {
		t.Fatalf("admin permission = %v, want RelayHostPermission", got)
	}
	if r.Admin() != con {
		t.Fatal("Admin() did not return the assigned connection")
	}
}

func TestSetAdminNil(t *testing.T) {
	r := newSingleRoom("t", nil)
	r.SetAdmin(newFakeCon(0, "host"))
	r.SetAdmin(nil)
	if r.Admin() != nil {
		t.Fatal("Admin() should be nil after setting nil")
	}
}

func TestIsStartGameStampsStartGameTime(t *testing.T) {
	r := newSingleRoom("t", nil)

	if r.IsStartGame() {
		t.Fatal("a fresh room must not be started")
	}
	if r.StartGameTime() != 0 {
		t.Fatalf("StartGameTime = %d, want 0 before start", r.StartGameTime())
	}

	before := timeutil.ConcurrentSecond()
	r.SetIsStartGame(true)
	if !r.IsStartGame() {
		t.Fatal("IsStartGame did not flip")
	}
	got := r.StartGameTime()
	if got < before+300 || got > timeutil.ConcurrentSecond()+300 {
		t.Fatalf("StartGameTime = %d, want ~%d", got, before+300)
	}

	// 已开局再设为开局: 原版提前 return, 不重算 startGameTime
	r.SetIsStartGame(true)
	if r.StartGameTime() != got {
		t.Fatalf("StartGameTime changed to %d on a redundant start, want %d", r.StartGameTime(), got)
	}

	r.SetIsStartGame(false)
	if r.StartGameTime() != 0 {
		t.Fatalf("StartGameTime = %d after stop, want 0", r.StartGameTime())
	}
}

func TestSiteAndSizeCounters(t *testing.T) {
	r := newSingleRoom("t", nil)

	if r.GetPosition() != 0 || r.GetSize() != 0 {
		t.Fatal("counters must start at 0")
	}
	if got := r.SetAddPosition(); got != 1 {
		t.Fatalf("SetAddPosition() = %d, want 1", got)
	}
	if got := r.GetPosition(); got != 1 {
		t.Fatalf("GetPosition() = %d, want 1", got)
	}

	r.SetAddSize()
	r.SetAddSize()
	if r.GetSize() != 2 {
		t.Fatalf("GetSize() = %d, want 2", r.GetSize())
	}
	r.SetRemoveSize()
	if r.GetSize() != 1 {
		t.Fatalf("GetSize() = %d, want 1", r.GetSize())
	}
}

func TestAbstractNetConnectMap(t *testing.T) {
	r := newSingleRoom("t", nil)
	a := newFakeCon(0, "a")
	b := newFakeCon(1, "b")

	r.SetAbstractNetConnect(a)
	r.SetAbstractNetConnect(b)

	if r.GetActiveConnectionSize() != 2 {
		t.Fatalf("GetActiveConnectionSize() = %d, want 2", r.GetActiveConnectionSize())
	}
	if got, ok := r.GetAbstractNetConnect(1); !ok || got != b {
		t.Fatal("GetAbstractNetConnect(1) did not return b")
	}
	if _, ok := r.GetAbstractNetConnect(2); ok {
		t.Fatal("GetAbstractNetConnect(2) should miss")
	}

	r.RemoveAbstractNetConnect(0)
	if r.GetActiveConnectionSize() != 1 {
		t.Fatalf("GetActiveConnectionSize() = %d after remove, want 1", r.GetActiveConnectionSize())
	}
}

func TestUpdateMinSizePicksLowestLatencySite(t *testing.T) {
	r := newSingleRoom("t", nil)
	now := timeutil.ConcurrentMillis()

	// site 3 延迟最低
	for _, c := range []struct {
		site int32
		lag  int64
	}{{0, 500}, {1, 200}, {3, 10}, {2, 900}} {
		con := newFakeCon(c.site, "x")
		con.lastRecv = now - c.lag
		r.SetAbstractNetConnect(con)
	}

	r.UpdateMinSize()
	if got := r.MinSize(); got != 3 {
		t.Fatalf("MinSize() = %d, want 3 (the lowest-latency site)", got)
	}
}

func TestUpdateMinSizeEmptyRoomIsNoop(t *testing.T) {
	r := newSingleRoom("t", nil)
	r.UpdateMinSize() // 原版 size < 1 时直接 return, 不得 panic
	if r.MinSize() != 0 {
		t.Fatalf("MinSize() = %d, want 0", r.MinSize())
	}
}

func TestUpdateMinSizeLatencyCollisionKeepsLast(t *testing.T) {
	// 延迟相同的连接会互相覆盖, 只留 EachAll 顺序里的最后一条
	r := newSingleRoom("t", nil)
	now := timeutil.ConcurrentMillis()

	for _, site := range []int32{0, 1, 2} {
		con := newFakeCon(site, "x")
		con.lastRecv = now - 100
		r.SetAbstractNetConnect(con)
	}

	r.UpdateMinSize()
	if got := r.MinSize(); got < 0 || got > 2 {
		t.Fatalf("MinSize() = %d, want one of 0..2", got)
	}
}

func TestSendMsgUsesAdmin(t *testing.T) {
	installFakeBuilder(t)

	r := newSingleRoom("t", nil)
	admin := newFakeCon(0, "host")
	r.SetAdmin(admin)

	r.SendMsg("hello")
	msgs := admin.sentMessages()
	if len(msgs) != 1 || msgs[0] != "hello" {
		t.Fatalf("SendMsg delivered %v, want [hello]", msgs)
	}

	r.SendMsgChat("hi", "bob", 1)
	msgs = admin.sentMessages()
	if len(msgs) != 2 || msgs[1] != "hi" {
		t.Fatalf("SendMsgChat delivered %v, want [hello hi]", msgs)
	}
}

// TestSendMsgWithoutBuilderIsNotFatal 固定"没有可用协议实现"时的行为:
// 原版 NetStaticData.RwHps 是 FakeRwHps, 它的 abstractNetPacket 会抛
// ImplementedException, 被 sendMsg 的 catch(Exception) 吞掉. Go 侧对应的是
// builder 为 nil, 记一条错误日志后返回, 不 panic.
func TestSendMsgWithoutBuilderIsNotFatal(t *testing.T) {
	old := abstractNetPacketProvider
	abstractNetPacketProvider = func() relayPacketBuilder { return nil }
	t.Cleanup(func() { abstractNetPacketProvider = old })

	r := newSingleRoom("t", nil)
	admin := newFakeCon(0, "host")
	r.SetAdmin(admin)

	r.SendMsg("hello")
	r.SendMsgChat("hi", "bob", 1)

	if len(admin.sentMessages()) != 0 {
		t.Fatal("没有包构造器时不应发出任何包")
	}
}

func TestSendMsgLogsSendFailure(t *testing.T) {
	installFakeBuilder(t)

	r := newSingleRoom("t", nil)
	admin := newFakeCon(0, "host")
	admin.sendErr = errors.New("broken pipe")
	r.SetAdmin(admin)

	// 原版 catch IOException 后只 printStackTrace; Go 侧记日志后返回, 不 panic
	r.SendMsg("hello")
}

func TestRemoveRoomClearsEverything(t *testing.T) {
	resetForTest()

	r := newRelayRoom(7, "7", "host", false, false, 151, 10)
	admin := newFakeCon(0, "host")
	r.SetAdmin(admin)
	player := newFakeCon(1, "player")
	r.SetAbstractNetConnect(player)
	r.SetAddSize()

	ran := false
	r.RemoveRoom(func() { ran = true })

	if !ran {
		t.Fatal("RemoveRoom did not run the callback")
	}
	if !r.CloseRoom() {
		t.Fatal("closeRoom was not set")
	}
	if r.Admin() != nil {
		t.Fatal("admin was not cleared")
	}
	if r.IsStartGame() {
		t.Fatal("isStartGame was not reset")
	}
	if r.GetActiveConnectionSize() != 0 {
		t.Fatal("connection map was not cleared")
	}
	if r.GetPosition() != 0 {
		t.Fatal("site counter was not reset")
	}
	if admin.disconnectCount() != 1 {
		t.Fatalf("admin disconnected %d times, want 1", admin.disconnectCount())
	}
	if player.disconnectCount() != 1 {
		t.Fatalf("player disconnected %d times, want 1", player.disconnectCount())
	}
	if _, ok := serverRelayDataRoom.Get(7); ok {
		t.Fatal("room was not unregistered from serverRelayDataRoom")
	}
}

func TestRemoveRoomSwallowsPanic(t *testing.T) {
	// 原版 catch (e: Exception) { // Ignore }: 清理时抛异常也要继续走 finally
	r := newSingleRoom("t", nil)
	r.SetAdmin(&panickyConnect{})

	ran := false
	r.RemoveRoom(func() { ran = true })
	if !ran {
		t.Fatal("callback must run even when cleanup panics")
	}
}

type panickyConnect struct{ fakeRelayConnect }

func (p *panickyConnect) Disconnect() { panic("boom") }

func TestEqualsUsesOtherID(t *testing.T) {
	// 原版: this.internalID == coverRelayID(other.id) —— 完全不比 this.id,
	// 因此两个 id 完全不同的房间也可能"相等", 这是照搬的怪行为.
	alphaID, err := CoverRelayID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	betaID, err := CoverRelayID("beta")
	if err != nil {
		t.Fatal(err)
	}

	a := newSingleRoom("alpha", nil)
	a.InternalID = betaID
	b := newSingleRoom("beta", nil)
	b.InternalID = alphaID

	if !a.Equals(b) {
		t.Fatal("a.Equals(b) should be true: a.internalID == coverRelayID(b.id)")
	}
	if !b.Equals(a) {
		t.Fatal("b.Equals(a) should be true: b.internalID == coverRelayID(a.id)")
	}
	if a.ID == b.ID {
		t.Fatal("the two rooms must have different ids for this test to mean anything")
	}

	c := newSingleRoom("gamma", nil)
	c.InternalID = 12345
	if a.Equals(c) {
		t.Fatal("a.Equals(c) should be false")
	}
	if a.Equals(nil) {
		t.Fatal("Equals(nil) should be false")
	}
	if a.Equals("not a room") {
		t.Fatal("Equals(non-room) should be false")
	}
	if !a.Equals(a) {
		t.Fatal("Equals(self) should be true")
	}
}

func TestHashCodeMatchesObjectsHash(t *testing.T) {
	r := newSingleRoom("room-1", nil)
	// Objects.hash(单参) = 31 * 1 + id.hashCode()
	want := int32(31) + javastr.HashCode("room-1")
	if got := r.HashCode(); got != want {
		t.Fatalf("HashCode() = %d, want %d", got, want)
	}
}

func TestAllIP(t *testing.T) {
	r := newSingleRoom("t", nil)
	admin := newFakeCon(0, "host")
	admin.ip = "10.0.0.1"
	admin.useAgreement = "UDP"
	r.SetAdmin(admin)

	member := newFakeCon(1, "bob")
	member.ip = "10.0.0.2"
	member.useAgreement = "TCP"
	r.SetAbstractNetConnect(member)

	got := r.AllIP()
	if !contains(got, "host / IP: 10.0.0.1 / Protocol: UDP / Admin: true") {
		t.Fatalf("AllIP() missing the admin line: %q", got)
	}
	if !contains(got, "bob / IP: 10.0.0.2 / Protocol: TCP / Admin: false") {
		t.Fatalf("AllIP() missing the member line: %q", got)
	}
}

func TestAllIPPanicsWithoutAdmin(t *testing.T) {
	// 原版 admin!! 在 admin 为空时抛 NPE
	r := newSingleRoom("t", nil)
	defer func() {
		if recover() == nil {
			t.Fatal("AllIP() must panic when admin is null (Kotlin: admin!!)")
		}
	}()
	r.AllIP()
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
