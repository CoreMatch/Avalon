package room

import (
	"strings"
	"sync"
	"testing"
	"time"

	"rwhps/internal/net/core"
	"rwhps/internal/util/timeutil"
)

func TestGetRelayAndGetCheckRelay(t *testing.T) {
	resetForTest()

	r := newRelayRoom(4242, "4242", "host", false, false, 151, 10)
	r.SetAdmin(newFakeCon(0, "host"))

	got, err := GetRelay("4242")
	if err != nil {
		t.Fatal(err)
	}
	if got != r {
		t.Fatalf("GetRelay(\"4242\") = %v, want the registered room", got)
	}

	ok, err := GetCheckRelay("4242")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("GetCheckRelay(\"4242\") = false, want true")
	}

	missing, err := GetRelay("nope")
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Fatal("GetRelay(\"nope\") should return nil")
	}
	ok, err = GetCheckRelay("nope")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("GetCheckRelay(\"nope\") = true, want false")
	}
}

func TestGetRelayPropagatesParseError(t *testing.T) {
	resetForTest()

	// 原版在此处抛 NumberFormatException; Go 侧返回 error
	if _, err := GetRelay("1.5"); err == nil {
		t.Fatal(`GetRelay("1.5") should return an error`)
	}
	if _, err := GetCheckRelay("1.5"); err == nil {
		t.Fatal(`GetCheckRelay("1.5") should return an error`)
	}
}

func TestGetRelayNewGeneratesUniqueNumericID(t *testing.T) {
	resetForTest()

	// id 为空时自动生成 1000..100000 之间未被占用的数字 ID
	r := GetRelayNew("", "host", false, false, 151, 10)
	if r.ID == "" {
		t.Fatal("GetRelayNew(\"\") did not generate an id")
	}
	if len(r.ID) < 4 || len(r.ID) > 6 {
		t.Fatalf("generated id %q is outside 1000..100000", r.ID)
	}
	for _, c := range r.ID {
		if c < '0' || c > '9' {
			t.Fatalf("generated id %q is not numeric", r.ID)
		}
	}

	// 生成的那个 ID 必须真的可用
	got, err := GetRelay(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != r {
		t.Fatal("the generated id does not resolve back to the room")
	}

	// 原版会一直循环到撞不到已占用的 ID 为止
	for i := 0; i < 50; i++ {
		next := GetRelayNew("", "host", false, false, 151, 10)
		if next.ID == r.ID {
			t.Fatal("GetRelayNew reused an existing id")
		}
	}
}

func TestGetRelayNewUsesGivenID(t *testing.T) {
	resetForTest()

	r := GetRelayNew("my-room", "host", true, true, 151, 10)
	if r.ID != "my-room" {
		t.Fatalf("ID = %q, want \"my-room\"", r.ID)
	}
	if !r.IsMod {
		t.Fatal("IsMod was not carried over")
	}
	if r.InternalID == 0 {
		t.Fatal("InternalID was not derived from the id")
	}
}

func TestGetRelayNewIgnoresVestigialParams(t *testing.T) {
	// 原版私有构造器的 playerName / betaGameVersion / version / maxPlayer
	// 四个参数没有被存下来, 是遗留的无用参数. 照搬: 签名保留, 行为忽略.
	resetForTest()
	r := GetRelayNew("x", "playerName", true, true, 999, 64)
	if r.InternalID == 0 || r.ID != "x" {
		t.Fatalf("unexpected room: %+v", r)
	}
}

func TestCleanRoomRemovesStaleAndClosed(t *testing.T) {
	resetForTest()

	// 超过 12 小时的房间
	stale := newRelayRoom(1, "1", "h", false, false, 151, 10)
	stale.RoomCreateTime = timeutil.ConcurrentSecond() - 13*60*60

	// 已关闭的房间 (创建时间很新)
	closed := newRelayRoom(2, "2", "h", false, false, 151, 10)
	closed.SetCloseRoom(true)

	// 正常房间
	fresh := newRelayRoom(3, "3", "h", false, false, 151, 10)

	CleanRoom()

	for _, c := range []struct {
		id   int32
		room *RelayRoom
		gone bool
	}{
		{1, stale, true},
		{2, closed, true},
		{3, fresh, false},
	} {
		_, ok := serverRelayDataRoom.Get(c.id)
		if ok == c.gone {
			t.Fatalf("room %d presence = %v, want gone=%v", c.id, ok, c.gone)
		}
	}
}

func TestSendAllMsgReachesEveryRoom(t *testing.T) {
	resetForTest()
	installFakeBuilder(t)

	// sendAllMsg 走 Threads.newThreadOther, 是异步的; 每条消息由各房间自己的 admin 接收
	admins := map[string]*fakeRelayConnect{}
	for _, id := range []string{"a", "b", "c"} {
		r := newRelayRoom(coverForTest(t, id), id, "h", false, false, 151, 10)
		admin := newFakeCon(0, "host-"+id)
		admins[id] = admin
		r.SetAdmin(admin)
	}

	SendAllMsg("broadcast")

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		all := true
		for _, a := range admins {
			if len(a.sentMessages()) == 0 {
				all = false
			}
		}
		if all {
			// 每个房间恰好收到一条, 且内容一致
			for id, a := range admins {
				msgs := a.sentMessages()
				if len(msgs) != 1 || msgs[0] != "broadcast" {
					t.Fatalf("room %s received %v, want [broadcast]", id, msgs)
				}
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("SendAllMsg did not reach every room within 3s")
}

// coverForTest 是测试辅助: 把房间 ID 换算成 internalID
func coverForTest(t *testing.T, id string) int32 {
	t.Helper()
	v, err := CoverRelayID(id)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRelayAllIPAndCounters(t *testing.T) {
	resetForTest()

	r1 := newRelayRoom(11, "11", "h", false, false, 151, 10)
	r1.SetAdmin(newFakeCon(0, "host1"))
	r1.SetAddSize()
	r1.SetAddSize()

	r2 := newRelayRoom(22, "22", "h", false, false, 151, 10)
	r2.SetAdmin(newFakeCon(0, "host2"))
	r2.SetAddSize()
	r2.SetIsStartGame(true)

	all := RelayAllIP()
	if !strings.Contains(all, "11") || !strings.Contains(all, "22") {
		t.Fatalf("RelayAllIP() = %q, want both room ids", all)
	}
	if !strings.Contains(all, "host1") || !strings.Contains(all, "host2") {
		t.Fatalf("RelayAllIP() = %q, want both admin names", all)
	}

	if got := AllSize(); got != 3 {
		t.Fatalf("AllSize() = %d, want 3", got)
	}
	if got := RoomAllSize(); got != 2 {
		t.Fatalf("RoomAllSize() = %d, want 2", got)
	}
	if got := RoomNoStartSize(); got != 1 {
		t.Fatalf("RoomNoStartSize() = %d, want 1", got)
	}
	if got := RoomPublicSize(); got != 0 {
		t.Fatalf("RoomPublicSize() = %d, want 0 (原版恒返回 0)", got)
	}
}

func TestGetAllRelayVersion(t *testing.T) {
	resetForTest()

	r := newRelayRoom(31, "31", "h", false, false, 151, 10)
	admin := newFakeCon(0, "host")
	admin.clientVer = 151
	r.SetAdmin(admin)

	for _, c := range []struct {
		site    int32
		version int
	}{
		{0, 152},
		{1, 151},
		{2, 152},
	} {
		con := newFakeCon(c.site, "p")
		con.clientVer = c.version
		r.SetAbstractNetConnect(con)
	}

	// admin 151 + 成员 152/151/152 => 151:2, 152:2
	versions := GetAllRelayVersion()
	if versions[151] != 2 || versions[152] != 2 {
		t.Fatalf("GetAllRelayVersion() = %v, want 151=2 152=2", versions)
	}
}

func TestRandPowIsSerializedAndFresh(t *testing.T) {
	resetForTest()

	// 原版 @get:Synchronized: 每次访问新建一个, 由 JVM 锁串行化
	a := RandPow()
	b := RandPow()
	if a == b {
		t.Fatal("RandPow() must return a fresh instance each time")
	}

	// 并发调用不应把内部状态搞乱
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if p := RandPow(); p == nil {
				t.Error("RandPow() returned nil")
			}
		}()
	}
	wg.Wait()
}

func TestServerRelayOldAndIpDataAreShared(t *testing.T) {
	resetForTest()

	// 这两个字段的消费者是 GameVersionRelay (尚未移植), 这里固定它们可共享
	ServerRelayOld.Put("uuid-1", "room-1")
	if got, ok := ServerRelayOld.Get("uuid-1"); !ok || got != "room-1" {
		t.Fatalf("ServerRelayOld lookup = %q, %v", got, ok)
	}

	ServerRelayIpData.Add("10.0.0.1")
	if ServerRelayIpData.Size() != 1 {
		t.Fatalf("ServerRelayIpData size = %d, want 1", ServerRelayIpData.Size())
	}
}

func TestConcurrentRoomAccess(t *testing.T) {
	resetForTest()

	r := newRelayRoom(77, "77", "h", false, false, 151, 10)
	r.SetAdmin(newFakeCon(0, "host"))

	// 只压原版就保证线程安全的那些路径: abstractNetConnectIntMap 是
	// IntMap(16, true) (加锁), admin 走 ReadWriteConcurrentSynchronize.
	// UpdateMinSize 刻意不在其中: 原版的 minSize 是普通 var, 写入不加锁,
	// 并发调用是与原版相同的数据竞争 (见字段注释).
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				con := newFakeCon(int32(i*100+j), "p")
				r.SetAbstractNetConnect(con)
				_ = r.AllIP()
				_ = r.GetActiveConnectionSize()
				r.RemoveAbstractNetConnect(con.Site())
			}
		}(i)
	}
	wg.Wait()

	if r.GetActiveConnectionSize() != 0 {
		t.Fatalf("connection map not drained: %d", r.GetActiveConnectionSize())
	}

	// 单goroutine下 UpdateMinSize 仍然正常
	con := newFakeCon(5, "p")
	r.SetAbstractNetConnect(con)
	r.UpdateMinSize()
	if r.MinSize() != 5 {
		t.Fatalf("MinSize() = %d, want 5", r.MinSize())
	}
	_ = core.RelayHostPermission
}

// TestSingleRoomIsSingletonAndReopens 固定 NetStaticData.relayRoom 的两个语义:
// 1. 全局只有一个实例;
// 2. 每次访问都会把 closeRoom 重置为 false —— 房间被关掉后再取一次等于重新开放.
func TestSingleRoomIsSingletonAndReopens(t *testing.T) {
	a := SingleRoom()
	b := SingleRoom()
	if a != b {
		t.Fatal("SingleRoom 必须返回同一个实例")
	}
	if a.ID != "RW-HPS Beta Relay" {
		t.Fatalf("SingleRoom().ID = %q, want %q", a.ID, "RW-HPS Beta Relay")
	}
	if a.InternalID != 0 {
		t.Fatalf("SingleRoom().InternalID = %d, want 0", a.InternalID)
	}
	if a.GroupNet == nil {
		t.Fatal("SingleRoom().GroupNet 不应为 nil")
	}

	a.SetCloseRoom(true)
	if !a.CloseRoom() {
		t.Fatal("closeRoom 未被置为 true")
	}
	_ = SingleRoom()
	if a.CloseRoom() {
		t.Fatal("再次访问 SingleRoom 应把 closeRoom 重置为 false")
	}
}
