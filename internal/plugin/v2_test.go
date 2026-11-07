package plugin

// v2 测试: 三档事件契约 (observe / veto / deliver) 的投递、否决、失败路径
// 与房间事件总线接线.

import (
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CoreMatch/Avalon/internal/data/global"
	"github.com/CoreMatch/Avalon/internal/game"
	"github.com/CoreMatch/Avalon/internal/game/enums"
	"github.com/CoreMatch/Avalon/internal/game/event"
	"github.com/CoreMatch/Avalon/internal/game/player"
	"github.com/CoreMatch/Avalon/internal/game/room"
	"github.com/CoreMatch/Avalon/internal/io/packet"
	"github.com/CoreMatch/Avalon/internal/util/jsonutil"
	"github.com/CoreMatch/Avalon/internal/util/timeutil"
)

// --- 接线测试基建 ---

// wireConn 记录发包动作的连接假实现 (带锁: 准入协程与测试主线程并发)
type wireConn struct {
	mu     sync.Mutex
	kicks  []string
	system []string
	ip     string
}

func (c *wireConn) SendSystemMessage(text string) error {
	c.mu.Lock()
	c.system = append(c.system, text)
	c.mu.Unlock()
	return nil
}
func (c *wireConn) SendChatMessage(_, _ string, _ int) error { return nil }
func (c *wireConn) SendKick(reason string) error {
	c.mu.Lock()
	c.kicks = append(c.kicks, reason)
	c.mu.Unlock()
	return nil
}
func (c *wireConn) SendRelayServerType(string, func(string)) {}
func (c *wireConn) SendPacket(*packet.Packet)                {}
func (c *wireConn) SendJumpToAnotherServer(string)           {}
func (c *wireConn) IP() string                               { return c.ip }
func (c *wireConn) UseConnectionAgreement() string           { return "TCP" }

func (c *wireConn) kickReasons() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.kicks))
	copy(out, c.kicks)
	return out
}

// newWirePlayer 造一个带档案的玩家
func newWirePlayer(con player.PlayerConnect, name, hexID string) *player.Player {
	data := player.NewData()
	data.Apply(func(d *player.Data) {
		d.Name = name
		d.ConnectHexID = hexID
		d.Team = 2
	})
	return player.New(con, nil, data)
}

// withV2Globals 隔离全局状态: 自动开局禁用 (零值会启用)、回放落盘关闭、
// 干净的 Administration
func withV2Globals(t *testing.T) {
	t.Helper()
	oldCore, oldCfg := global.Core, global.ConfigServer
	global.Core = &global.Application{Admin: global.NewAdministration()}
	global.ConfigServer.AutoStartMinPlayerSize = -1
	global.ConfigServer.SaveRePlayFile = false
	t.Cleanup(func() {
		global.Core = oldCore
		global.ConfigServer = oldCfg
	})
}

// wireRoom 装配房间 + hub + 订阅, 返回房间与清理后的 hub
func wireRoom(t *testing.T, hub *Hub) *room.ServerRoom {
	t.Helper()
	old := Default()
	SetDefault(hub)
	t.Cleanup(func() { SetDefault(old) })

	r := room.NewServerRoom(game.NewGameSession("WIRE1", nil))
	RegisterRoomEvents(r)
	return r
}

// --- observe 档: 投递与载荷 ---

// TestV2ObserveEventsDeliveredWithPayloads 观察档事件全量投递且载荷字段齐备
func TestV2ObserveEventsDeliveredWithPayloads(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)
	withServerIdentity(t, "SIDV2", "V2Server")

	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(), nil)
	observeAckHandlers(fake)
	hub := loadTestHub(t, configWith(entryFor("obs", fake, hex.EncodeToString(fake.secret))))
	r := wireRoom(t, hub)

	// join (无否决授权 -> 纯观察)
	con := &wireConn{ip: "203.0.113.9"}
	p := newWirePlayer(con, "alice", "aabbcc")
	r.EventManage.Fire(&event.PlayerJoinEvent{Player: p})

	// leave (带重进锁定窗口 + 对局中)
	p.KickTime = timeutil.ConcurrentMillis() + 60_000
	r.IsStartGame = true
	r.EventManage.Fire(&event.PlayerLeaveEvent{Player: p})
	r.IsStartGame = false

	// 封禁三件
	r.EventManage.Fire(&event.PlayerBanEvent{Player: p})
	r.EventManage.Fire(&event.PlayerUnBanEvent{Player: p})
	r.EventManage.Fire(&event.PlayerIpBanEvent{Player: p})

	// 开局快照
	r.Maps.MapName = "Crossing Large (10p)"
	r.Maps.MapPlayer = "[z;p10]"
	r.PlayerManage.PlayerGroup.Add(p)
	r.EventManage.Fire(&event.ServerGameStartEvent{})
	r.PlayerManage.RemovePlayer(p)

	// 聊天
	r.EventManage.Fire(&event.PlayerChatEvent{Player: p, Message: "hello world"})

	waitFor(t, 5*time.Second, "all observe events delivered", func() bool {
		return len(fake.requestsOf("/v1/events/player.join")) == 1 &&
			len(fake.requestsOf("/v1/events/player.leave")) == 1 &&
			len(fake.requestsOf("/v1/events/player.banned")) == 1 &&
			len(fake.requestsOf("/v1/events/player.unbanned")) == 1 &&
			len(fake.requestsOf("/v1/events/player.ipbanned")) == 1 &&
			len(fake.requestsOf("/v1/events/game.start")) == 1 &&
			len(fake.requestsOf("/v1/events/player.chat")) == 1
	})

	// player.join 载荷
	var join struct {
		Event string `json:"event"`
		Room  struct {
			Kind string `json:"kind"`
			Code string `json:"code"`
		} `json:"room"`
		Player struct {
			Name    string `json:"name"`
			UUID    string `json:"uuid"`
			Team    int    `json:"team"`
			Admin   bool   `json:"admin"`
			Address string `json:"address"`
		} `json:"player"`
	}
	_ = json.Unmarshal(fake.requestsOf("/v1/events/player.join")[0].Body, &join)
	if join.Event != EventPlayerJoin || join.Room.Kind != KindServer || join.Room.Code != "WIRE1" ||
		join.Player.Name != "alice" || join.Player.UUID != "aabbcc" || join.Player.Team != 2 ||
		join.Player.Address != "203.0.113.9" {
		t.Errorf("player.join 载荷不符: %+v", join)
	}

	// player.leave 载荷: 重进窗口 > 0, inGame=true
	var leave struct {
		BanRemainingMs int64 `json:"banRemainingMs"`
		InGame         bool  `json:"inGame"`
	}
	_ = json.Unmarshal(fake.requestsOf("/v1/events/player.leave")[0].Body, &leave)
	if leave.BanRemainingMs <= 0 || leave.BanRemainingMs > 60_000 {
		t.Errorf("banRemainingMs = %d", leave.BanRemainingMs)
	}
	if !leave.InGame {
		t.Error("inGame 应为 true")
	}

	// game.start 载荷: 快照字段
	var start struct {
		Match struct {
			MapName    string       `json:"mapName"`
			MapPlayer  string       `json:"mapPlayer"`
			MaxPlayers int          `json:"maxPlayers"`
			Players    []PlayerInfo `json:"players"`
		} `json:"match"`
	}
	_ = json.Unmarshal(fake.requestsOf("/v1/events/game.start")[0].Body, &start)
	if start.Match.MapName != "Crossing Large (10p)" || start.Match.MapPlayer != "[z;p10]" {
		t.Errorf("game.start match 段不符: %+v", start.Match)
	}
	if len(start.Match.Players) != 1 || start.Match.Players[0].Name != "alice" {
		t.Errorf("game.start players 段不符: %+v", start.Match.Players)
	}

	// chat 载荷
	var chat struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(fake.requestsOf("/v1/events/player.chat")[0].Body, &chat)
	if chat.Message != "hello world" {
		t.Errorf("chat message = %q", chat.Message)
	}

	// observe 档不落 WAL
	if n := hub.plugins[0].outbox.size(); n != 0 {
		t.Errorf("队列应已清空, got %d", n)
	}
	if data := readFileStr(t, outboxFile("obs")); strings.TrimSpace(data) != "" {
		t.Errorf("observe 事件不应写 WAL, 文件内容: %q", data)
	}
}

// TestV2ObserveFailureDropsWithoutRetry 观察档失败即弃, 不重试
func TestV2ObserveFailureDropsWithoutRetry(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(), nil)
	observeAckHandlers(fake)
	fake.setHandler(EventPlayerChat, func(_ []byte) (int, []byte) {
		return http.StatusInternalServerError, nil
	})
	hub := loadTestHub(t, configWith(entryFor("flaky", fake, hex.EncodeToString(fake.secret))))
	r := wireRoom(t, hub)

	con := &wireConn{}
	p := newWirePlayer(con, "bob", "ddeeff")
	r.EventManage.Fire(&event.PlayerChatEvent{Player: p, Message: "x"})

	waitFor(t, 5*time.Second, "chat dropped after failure", func() bool {
		st := hub.Status()
		return st[0].Dropped == 1
	})
	if n := len(fake.requestsOf("/v1/events/player.chat")); n != 1 {
		t.Fatalf("观察档失败不应重试, 请求数 = %d", n)
	}
	if st := hub.Status(); st[0].Retries != 0 {
		t.Errorf("观察档不应产生重试计数: %d", st[0].Retries)
	}
}

// TestV2ObserveNotReplayedAcrossRestart 崩溃重启后: deliver 档重放,
// observe 档不重放 (模拟: 插件不可达期间积压 -> 进程退出 -> 插件恢复 -> 重启)
func TestV2ObserveNotReplayedAcrossRestart(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	hexSecret, raw := testSecret(t)
	writePluginsJSON(t, configWith(Entry{Name: "mix", BaseURL: "http://" + addr, Secrets: []string{hexSecret}}))

	// 第一代: 插件不可达, 两类事件都积压; 直接退出 (模拟崩溃, 均未投递)
	hub1, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	hub1.EmitGameOver("R1", nil) // deliver -> WAL
	hub1.enqueueEvent(EventPlayerChat, newEventID(time.Now()), `{"event":"player.chat"}`, TierObserve)
	waitFor(t, 5*time.Second, "both buffered", func() bool {
		return hub1.plugins[0].outbox.size() == 2
	})
	hub1.Close()

	// 插件恢复 + 第二代启动: 只重放 deliver 档
	fake := newFakePluginOnAddr(t, addr, raw)
	hub2, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub2.Close)
	waitFor(t, 30*time.Second, "game.over replayed", func() bool {
		return len(fake.requestsOf("/v1/events/game.over")) == 1
	})
	time.Sleep(300 * time.Millisecond) // 给 (不应发生的) observe 重放一个窗口
	if n := len(fake.requestsOf("/v1/events/player.chat")); n != 0 {
		t.Fatalf("observe 档不得重放, player.chat 请求数 = %d", n)
	}
}

// --- veto 档: player.join 准入 ---

// TestV2JoinAdmissionVetoKicks 授权 + 声明 -> 否决生效, 理由随踢出送达
func TestV2JoinAdmissionVetoKicks(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(EventPlayerJoin), nil)
	observeAckHandlers(fake)
	fake.setHandler(EventPlayerJoin, func(_ []byte) (int, []byte) {
		return http.StatusOK, []byte(`{"allowed":false,"reason":"ban-evasion detected"}`)
	})
	entry := entryFor("gate", fake, hex.EncodeToString(fake.secret))
	entry.VetoEvents = []string{EventPlayerJoin}
	hub := loadTestHub(t, configWith(entry))
	r := wireRoom(t, hub)

	con := &wireConn{}
	p := newWirePlayer(con, "mallory", "beef01")
	r.EventManage.Fire(&event.PlayerJoinEvent{Player: p})

	waitFor(t, 5*time.Second, "veto kick delivered", func() bool {
		return len(con.kickReasons()) == 1
	})
	if got := con.kickReasons()[0]; !strings.Contains(got, "ban-evasion detected") {
		t.Fatalf("踢出理由应含插件理由: %q", got)
	}
	// 否决调用 1 次, 且否决后不再发观察通知
	waitFor(t, 3*time.Second, "exactly one join request", func() bool {
		return len(fake.requestsOf("/v1/events/player.join")) == 1
	})
	time.Sleep(150 * time.Millisecond)
	if n := len(fake.requestsOf("/v1/events/player.join")); n != 1 {
		t.Fatalf("否决后不应再发观察通知, 请求数 = %d", n)
	}
	if st := hub.Status(); st[0].Denied != 1 {
		t.Errorf("denied 计数 = %d", st[0].Denied)
	}
}

// TestV2JoinAdmissionAllowsAndNotifies 放行 -> 观察通知随后投递
func TestV2JoinAdmissionAllowsAndNotifies(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(EventPlayerJoin), nil)
	observeAckHandlers(fake)
	entry := entryFor("gate", fake, hex.EncodeToString(fake.secret))
	entry.VetoEvents = []string{EventPlayerJoin}
	hub := loadTestHub(t, configWith(entry))
	r := wireRoom(t, hub)

	con := &wireConn{}
	p := newWirePlayer(con, "alice", "aabbcc")
	r.EventManage.Fire(&event.PlayerJoinEvent{Player: p})

	waitFor(t, 5*time.Second, "join observe delivered after allow", func() bool {
		return len(fake.requestsOf("/v1/events/player.join")) == 2 // 1 否决咨询 + 1 观察通知
	})
	if len(con.kickReasons()) != 0 {
		t.Fatal("放行不应踢人")
	}
}

// TestV2JoinVetoRequiresBothGrantAndDeclaration 授权与声明缺一不可,
// 缺任一都退回观察模式
func TestV2JoinVetoRequiresBothGrantAndDeclaration(t *testing.T) {
	// 情形一: 有授权、manifest 未声明 veto -> 观察模式
	isolateBase(t)
	withV2Globals(t)
	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(), nil) // 不声明 veto
	observeAckHandlers(fake)
	entry := entryFor("g1", fake, hex.EncodeToString(fake.secret))
	entry.VetoEvents = []string{EventPlayerJoin}
	hub := loadTestHub(t, configWith(entry))
	r := wireRoom(t, hub)
	con := &wireConn{}
	r.EventManage.Fire(&event.PlayerJoinEvent{Player: newWirePlayer(con, "u1", "h1")})
	waitFor(t, 5*time.Second, "observe delivered", func() bool {
		return len(fake.requestsOf("/v1/events/player.join")) == 1
	})
	if len(con.kickReasons()) != 0 {
		t.Fatal("未声明 veto 不应走否决")
	}
	hub.Close()

	// 情形二: manifest 声明 veto、无运营授权 -> 观察模式
	isolateBase(t)
	withV2Globals(t)
	fake2 := newFakePlugin(t)
	fake2.setManifest(v2TestManifest(EventPlayerJoin), nil)
	observeAckHandlers(fake2)
	hub2 := loadTestHub(t, configWith(entryFor("g2", fake2, hex.EncodeToString(fake2.secret))))
	r2 := wireRoom(t, hub2)
	con2 := &wireConn{}
	r2.EventManage.Fire(&event.PlayerJoinEvent{Player: newWirePlayer(con2, "u2", "h2")})
	waitFor(t, 5*time.Second, "observe delivered without grant", func() bool {
		return len(fake2.requestsOf("/v1/events/player.join")) == 1
	})
	if len(con2.kickReasons()) != 0 {
		t.Fatal("未授权 veto 不应走否决")
	}
}

// TestV2JoinAdmissionTimeoutFailsOpen 准入超时 -> 放行 + 观察通知 + 计数
func TestV2JoinAdmissionTimeoutFailsOpen(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(EventPlayerJoin), nil)
	observeAckHandlers(fake)
	release := make(chan struct{})
	fake.setHandler(EventPlayerJoin, func(_ []byte) (int, []byte) {
		<-release
		return http.StatusOK, []byte(`{"allowed":false,"reason":"late"}`)
	})
	t.Cleanup(func() { close(release) })

	cfg := NewConfig()
	cfg.AdmissionTimeoutMs = 80
	entry := entryFor("slow", fake, hex.EncodeToString(fake.secret))
	entry.VetoEvents = []string{EventPlayerJoin}
	cfg.Entries = []Entry{entry}
	hub := loadTestHub(t, jsonutil.ToJSON(cfg))
	r := wireRoom(t, hub)

	con := &wireConn{}
	r.EventManage.Fire(&event.PlayerJoinEvent{Player: newWirePlayer(con, "pat", "cc99")})

	waitFor(t, 5*time.Second, "fail-open observe delivered", func() bool {
		// 1 次超时的否决咨询 + 1 次放行后的观察通知
		return len(fake.requestsOf("/v1/events/player.join")) == 2
	})
	if len(con.kickReasons()) != 0 {
		t.Fatal("超时必须 fail-open, 不得踢人")
	}
	if st := hub.Status(); st[0].FailOpen != 1 {
		t.Errorf("failOpen 计数 = %d", st[0].FailOpen)
	}
}

// --- veto 档: 对局内指令否决 (紧超时) ---

// TestV2UnitOperationVetoStopsPacket 否决 -> ResultStatus 置假 (指令不进引擎)
func TestV2UnitOperationVetoStopsPacket(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(EventUnitOperation), nil)
	observeAckHandlers(fake)
	fake.setHandler(EventUnitOperation, func(_ []byte) (int, []byte) {
		return http.StatusOK, []byte(`{"allowed":false,"reason":"no nukes"}`)
	})
	entry := entryFor("ref", fake, hex.EncodeToString(fake.secret))
	entry.VetoEvents = []string{EventUnitOperation}
	hub := loadTestHub(t, configWith(entry))
	r := wireRoom(t, hub)

	con := &wireConn{}
	p := newWirePlayer(con, "eve", "ee01")
	ev := &event.PlayerOperationUnitEvent{
		Player: p, Actions: enums.ActionBuild, UnitName: "nukeLauncher",
		X: 12.5, Y: 34.25, ResultStatus: true,
	}
	r.EventManage.Fire(ev) // 同步: 返回即已裁决

	if ev.ResultStatus {
		t.Fatal("否决后 ResultStatus 应为 false (指令包不再转给引擎)")
	}
	// 否决咨询 + 观察通知各一次
	waitFor(t, 5*time.Second, "observe copy delivered", func() bool {
		return len(fake.requestsOf("/v1/events/unit.operation")) == 2
	})
	// 载荷字段
	var payload struct {
		Action   string  `json:"action"`
		UnitName string  `json:"unitName"`
		X        float32 `json:"x"`
		Y        float32 `json:"y"`
	}
	_ = json.Unmarshal(fake.requestsOf("/v1/events/unit.operation")[0].Body, &payload)
	if payload.Action != enums.ActionBuild.Name() || payload.UnitName != "nukeLauncher" ||
		payload.X != 12.5 || payload.Y != 34.25 {
		t.Errorf("unit.operation 载荷不符: %+v", payload)
	}
}

// TestV2FactoryBuildVeto 工厂建造否决同形
func TestV2FactoryBuildVeto(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(EventFactoryBuild), nil)
	observeAckHandlers(fake)
	fake.setHandler(EventFactoryBuild, func(_ []byte) (int, []byte) {
		return http.StatusOK, []byte(`{"allowed":false,"reason":"restricted"}`)
	})
	entry := entryFor("ref", fake, hex.EncodeToString(fake.secret))
	entry.VetoEvents = []string{EventFactoryBuild}
	hub := loadTestHub(t, configWith(entry))
	r := wireRoom(t, hub)

	ev := &event.PlayerOperationFactoryBuildUnitEvent{
		Player:   newWirePlayer(&wireConn{}, "eve", "ee01"),
		ActionID: "heavyTank", ResultStatus: true,
	}
	r.EventManage.Fire(ev)
	if ev.ResultStatus {
		t.Fatal("否决后 ResultStatus 应为 false")
	}
}

// TestV2UnitOperationVetoTimeoutFailsOpen 对局内否决超时 -> 操作放行 (紧超时档)
func TestV2UnitOperationVetoTimeoutFailsOpen(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(EventUnitOperation), nil)
	observeAckHandlers(fake)
	release := make(chan struct{})
	fake.setHandler(EventUnitOperation, func(_ []byte) (int, []byte) {
		<-release
		return http.StatusOK, []byte(`{"allowed":false,"reason":"late"}`)
	})
	t.Cleanup(func() { close(release) })

	cfg := NewConfig()
	cfg.GameplayVetoTimeoutMs = 60
	entry := entryFor("slow", fake, hex.EncodeToString(fake.secret))
	entry.VetoEvents = []string{EventUnitOperation}
	cfg.Entries = []Entry{entry}
	hub := loadTestHub(t, jsonutil.ToJSON(cfg))
	r := wireRoom(t, hub)

	ev := &event.PlayerOperationUnitEvent{
		Player:  newWirePlayer(&wireConn{}, "eve", "ee01"),
		Actions: enums.ActionMove, UnitName: "tank", ResultStatus: true,
	}
	start := time.Now()
	r.EventManage.Fire(ev)
	elapsed := time.Since(start)

	if !ev.ResultStatus {
		t.Fatal("超时必须 fail-open, 操作应放行")
	}
	if elapsed > 3*time.Second {
		t.Errorf("对局内否决阻塞过久: %v", elapsed)
	}
	if st := hub.Status(); st[0].FailOpen != 1 {
		t.Errorf("failOpen 计数 = %d", st[0].FailOpen)
	}
}

// TestV2UnitOperationObserveOnlyWithoutGrant 无授权 -> 纯观察, 不产生否决调用
func TestV2UnitOperationObserveOnlyWithoutGrant(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(EventUnitOperation), nil) // 声明了 veto
	observeAckHandlers(fake)
	// 但配置不授权
	hub := loadTestHub(t, configWith(entryFor("watch", fake, hex.EncodeToString(fake.secret))))
	r := wireRoom(t, hub)

	ev := &event.PlayerOperationUnitEvent{
		Player:  newWirePlayer(&wireConn{}, "eve", "ee01"),
		Actions: enums.ActionMove, UnitName: "tank", ResultStatus: true,
	}
	r.EventManage.Fire(ev)

	waitFor(t, 5*time.Second, "observe delivered", func() bool {
		return len(fake.requestsOf("/v1/events/unit.operation")) == 1 // 仅观察通知
	})
	if !ev.ResultStatus {
		t.Fatal("未授权不得否决")
	}
}

// --- 版本协商失败路径 ---

// TestV2VersionMismatchNotDelivered 版本无交集的事件不投递; 积压在协商后清理
func TestV2VersionMismatchNotDelivered(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	// 就绪插件声明 player.chat 版本 [2] (与 Avalon 的 v1 无交集)
	fake := newFakePlugin(t)
	m := v2TestManifest()
	m.Events[EventPlayerChat] = EventCapability{Versions: []int{2}}
	fake.setManifest(m, nil)
	observeAckHandlers(fake)
	hub := loadTestHub(t, configWith(entryFor("ver", fake, hex.EncodeToString(fake.secret))))
	r := wireRoom(t, hub)

	r.EventManage.Fire(&event.PlayerChatEvent{
		Player: newWirePlayer(&wireConn{}, "x", "y"), Message: "m",
	})
	time.Sleep(200 * time.Millisecond)
	if n := len(fake.requestsOf("/v1/events/player.chat")); n != 0 {
		t.Fatalf("版本无交集不应投递, got %d", n)
	}
	hub.Close()

	// 先积压后协商出无交集: 启动时 manifest 500 (unready), 入队, 再恢复
	// 为不含 player.chat 的 manifest -> 积压被丢弃并计数
	isolateBase(t)
	withV2Globals(t)
	fake2 := newFakePlugin(t)
	fake2.setManifest(nil, nil) // 500 -> unready
	observeAckHandlers(fake2)
	hub2 := loadTestHub(t, configWith(entryFor("ver2", fake2, hex.EncodeToString(fake2.secret))))
	r2 := wireRoom(t, hub2)
	r2.EventManage.Fire(&event.PlayerChatEvent{
		Player: newWirePlayer(&wireConn{}, "x", "y"), Message: "m",
	})
	waitFor(t, 5*time.Second, "buffered while unready", func() bool {
		return hub2.plugins[0].outbox.size() == 1
	})
	m2 := v2TestManifest()
	delete(m2.Events, EventPlayerChat) // 协商恢复但不含 chat
	fake2.setManifest(m2, nil)
	waitFor(t, 15*time.Second, "buffered dropped after negotiation", func() bool {
		st := hub2.Status()
		return st[0].Ready && st[0].Dropped == 1 && hub2.plugins[0].outbox.size() == 0
	})
}

// --- 配置校验 ---

// TestV2ConfigVetoEventsValidation 授权白名单严格校验
func TestV2ConfigVetoEventsValidation(t *testing.T) {
	hexSecret, _ := testSecret(t)
	base := Entry{Name: "p", BaseURL: "http://127.0.0.1:1", Secrets: []string{hexSecret}}

	cases := []struct {
		name string
		veto []string
	}{
		{"unknown event", []string{"nope.event"}},
		{"room.create in vetoEvents", []string{EventRoomCreate}},
		{"observe-only event", []string{EventPlayerChat}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateBase(t)
			e := base
			e.VetoEvents = tc.veto
			writePluginsJSON(t, configWith(e))
			if _, err := Load(); err == nil {
				t.Fatalf("vetoEvents=%v 应使 Load 失败", tc.veto)
			}
		})
	}
}

// --- 零回归 ---

// TestV2ZeroRegressionWithoutPlugins 无插件配置: 全部 v2 监听器空转无副作用
func TestV2ZeroRegressionWithoutPlugins(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	hub, err := Load() // 空配置
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()
	r := wireRoom(t, hub)

	p := newWirePlayer(&wireConn{}, "solo", "ff00")
	r.EventManage.Fire(&event.PlayerJoinEvent{Player: p})
	r.EventManage.Fire(&event.PlayerLeaveEvent{Player: p})
	r.EventManage.Fire(&event.PlayerBanEvent{Player: p})
	r.EventManage.Fire(&event.ServerGameStartEvent{})
	r.EventManage.Fire(&event.PlayerChatEvent{Player: p, Message: "hi"})
	r.EventManage.Fire(&event.PlayerOperationUnitEvent{Player: p, ResultStatus: true})
	r.EventManage.Fire(&event.PlayerOperationFactoryBuildUnitEvent{Player: p, ResultStatus: true})
	// 无 panic、无网络、无队列即通过
	if len(hub.plugins) != 0 {
		t.Fatal("空配置不应有插件")
	}
}

// --- 辅助 ---

func readFileStr(t *testing.T, f interface{ ReadFileStringData() string }) string {
	t.Helper()
	return f.ReadFileStringData()
}
