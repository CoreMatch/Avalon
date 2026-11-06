package room

import (
	"strings"
	"sync"
	"testing"

	"rwhps/internal/core/thread"
	"rwhps/internal/data/global"
	"rwhps/internal/game/event"
	"rwhps/internal/game/player"
	"rwhps/internal/io/packet"
	"rwhps/internal/util/file"
	"rwhps/internal/util/timeutil"
)

// fakeConn 记录发包动作的连接假实现
//
// 带锁: 自动开局等定时器会在独立线程里广播, 与测试主线程并发写记录.
type fakeConn struct {
	mu     sync.Mutex
	system []string
	kicks  []string
	ip     string
}

func (f *fakeConn) SendSystemMessage(text string) error {
	f.mu.Lock()
	f.system = append(f.system, text)
	f.mu.Unlock()
	return nil
}
func (f *fakeConn) SendChatMessage(_, _ string, _ int) error { return nil }
func (f *fakeConn) SendKick(reason string) error {
	f.mu.Lock()
	f.kicks = append(f.kicks, reason)
	f.mu.Unlock()
	return nil
}
func (f *fakeConn) SendRelayServerType(string, func(string)) {}
func (f *fakeConn) SendPacket(*packet.Packet)                {}
func (f *fakeConn) SendJumpToAnotherServer(string)           {}
func (f *fakeConn) IP() string                               { return f.ip }
func (f *fakeConn) UseConnectionAgreement() string           { return "TCP" }

// messages 返回系统消息快照
func (f *fakeConn) messages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.system))
	copy(out, f.system)
	return out
}

// kickReasons 返回踢出原因快照
func (f *fakeConn) kickReasons() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.kicks))
	copy(out, f.kicks)
	return out
}

// newNamedPlayer 造一个带名字与 hexID 的玩家 (未入房间列表)
func newNamedPlayer(con player.PlayerConnect, name, hexID string) *player.Player {
	return player.New(con, nil, namedData(name, hexID))
}

// namedData 造玩家档案数据
func namedData(name, hexID string) *player.Data {
	data := player.NewData()
	data.Apply(func(d *player.Data) {
		d.Name = name
		d.ConnectHexID = hexID
	})
	return data
}

// withTestGlobals 准备事件监听依赖的全局状态:
//   - 干净的 Administration (不碰真实 Settings.bin)
//   - AutoStartMinPlayerSize = -1 (零值 0 会让自动开局永远启用)
//   - SaveRePlayFile = false (避免测试默认写文件)
func withTestGlobals(t *testing.T) *global.Application {
	t.Helper()
	oldCore, oldCfg := global.Core, global.ConfigServer
	app := &global.Application{Admin: global.NewAdministration()}
	global.Core = app
	global.ConfigServer.AutoStartMinPlayerSize = -1
	global.ConfigServer.SaveRePlayFile = false
	t.Cleanup(func() {
		global.Core = oldCore
		global.ConfigServer = oldCfg
	})
	return app
}

// TestJoinEventNameValidation 上游 Event.registerPlayerJoinEvent 的名字校验
func TestJoinEventNameValidation(t *testing.T) {
	withTestGlobals(t)
	r := newTestRoom(t, nil)

	for _, name := range []string{"", "   ", strings.Repeat("a", 31)} {
		con := &fakeConn{}
		p := newNamedPlayer(con, name, "hex-name")
		r.EventManage.Fire(&event.PlayerJoinEvent{Player: p})
		kicks := con.kickReasons()
		if len(kicks) != 1 || kicks[0] != "kick.name.failed" {
			t.Fatalf("名字 %q 应被踢出 (kick.name.failed), got %v", name, kicks)
		}
	}
}

// TestJoinEventBannedUUID 被封禁的 UUID 加入即踢
func TestJoinEventBannedUUID(t *testing.T) {
	app := withTestGlobals(t)
	r := newTestRoom(t, nil)
	app.Admin.BanUUID("hex-banned")

	con := &fakeConn{}
	p := newNamedPlayer(con, "someone", "hex-banned")
	r.EventManage.Fire(&event.PlayerJoinEvent{Player: p})

	kicks := con.kickReasons()
	if len(kicks) != 1 || kicks[0] != "kick.ban" {
		t.Fatalf("被封禁玩家应被踢出 (kick.ban), got %v", kicks)
	}
}

// TestJoinEventKickCache 离线档案: 踢出未到期拒绝进入, 到期后恢复禁言
func TestJoinEventKickCache(t *testing.T) {
	app := withTestGlobals(t)
	r := newTestRoom(t, nil)

	// 踢出未到期
	app.Admin.PlayerDataCache["hex-k"] = global.NewPlayerInfo("hex-k", timeutil.GetTimeFutureMillis(60*1000), 0)
	con := &fakeConn{}
	p := newNamedPlayer(con, "kicked", "hex-k")
	r.EventManage.Fire(&event.PlayerJoinEvent{Player: p})
	kicks := con.kickReasons()
	if len(kicks) != 1 || kicks[0] != "kick.you.time" {
		t.Fatalf("踢出未到期应拒绝进入, got %v", kicks)
	}

	// 踢出已到期, 恢复禁言时间
	muteUntil := timeutil.GetTimeFutureMillis(120 * 1000)
	app.Admin.PlayerDataCache["hex-m"] = global.NewPlayerInfo("hex-m", timeutil.ConcurrentMillis()-1000, muteUntil)
	p2 := newNamedPlayer(&fakeConn{}, "muted", "hex-m")
	r.EventManage.Fire(&event.PlayerJoinEvent{Player: p2})
	if p2.MuteTime != muteUntil {
		t.Fatalf("应恢复禁言时间 %d, got %d", muteUntil, p2.MuteTime)
	}
}

// TestJoinEventBroadcastAndAd 正常加入: 全场广播 + 进场广告
func TestJoinEventBroadcastAndAd(t *testing.T) {
	withTestGlobals(t)
	global.ConfigServer.EnterAd = "welcome ad"
	r := newTestRoom(t, nil)

	watcher := &fakeConn{}
	r.PlayerManage.AddAbstractPlayer(watcher, namedData("watcher", "hex-w"), nil)

	// 上游时序: addAbstractPlayer 之后立刻对新玩家触发 join 事件
	con := &fakeConn{}
	p := r.PlayerManage.AddAbstractPlayer(con, namedData("newbie", "hex-n"), nil)
	r.EventManage.Fire(&event.PlayerJoinEvent{Player: p})

	watcherMsgs := watcher.messages()
	if len(watcherMsgs) == 0 || !strings.Contains(watcherMsgs[0], "player.ent") {
		t.Fatalf("在场玩家应收到 player.ent 广播, got %v", watcherMsgs)
	}
	// 新玩家先收到广播再收到进场广告 (上游顺序)
	var gotAd bool
	for _, m := range con.messages() {
		if m == "welcome ad" {
			gotAd = true
		}
	}
	if !gotAd {
		t.Fatalf("新玩家应收到进场广告, got %v", con.messages())
	}
}

// TestJoinEventAutoStart 人数达到 autoStartMinPlayerSize 时挂自动开局倒计时
func TestJoinEventAutoStart(t *testing.T) {
	withTestGlobals(t)
	global.ConfigServer.AutoStartMinPlayerSize = 2
	r := newTestRoom(t, nil)

	p1 := r.PlayerManage.AddAbstractPlayer(&fakeConn{}, namedData("p1", "hex-1"), nil)
	r.EventManage.Fire(&event.PlayerJoinEvent{Player: p1})
	if thread.ContainsTimeTask(thread.AutoStartTask) {
		t.Fatal("1 人时不应启动自动开局")
	}

	p2 := r.PlayerManage.AddAbstractPlayer(&fakeConn{}, namedData("p2", "hex-2"), nil)
	r.EventManage.Fire(&event.PlayerJoinEvent{Player: p2})
	if !thread.ContainsTimeTask(thread.AutoStartTask) {
		t.Fatal("达到 2 人后应启动自动开局倒计时")
	}

	// 人数回落到阈值以下时取消倒计时 (上游 leave 监听)
	r.PlayerManage.RemoveFromGroup(p2)
	r.EventManage.Fire(&event.PlayerLeaveEvent{Player: p2})
	if thread.ContainsTimeTask(thread.AutoStartTask) {
		t.Fatal("人数回落后应取消自动开局倒计时")
	}
}

// TestLeaveEventOneAdminTransfer oneAdmin: 自动管理员离开时移交权限
func TestLeaveEventOneAdminTransfer(t *testing.T) {
	withTestGlobals(t)
	global.ConfigServer.OneAdmin = true
	r := newTestRoom(t, nil)

	admin := r.PlayerManage.AddAbstractPlayer(&fakeConn{}, namedData("admin", "hex-a"), nil)
	next := r.PlayerManage.AddAbstractPlayer(&fakeConn{}, namedData("next", "hex-b"), nil)
	admin.IsAdmin, admin.AutoAdmin = true, true

	// 上游时序: 先移出列表再触发事件
	r.PlayerManage.RemoveFromGroup(admin)
	r.EventManage.Fire(&event.PlayerLeaveEvent{Player: admin})

	if !next.IsAdmin || !next.AutoAdmin {
		t.Fatal("权限应移交给下一个非管理员玩家")
	}
	if admin.IsAdmin || admin.AutoAdmin {
		t.Fatal("离开者的管理员标记应被清除")
	}
}

// TestLeaveEventWritesCache 离开时写入离线档案
func TestLeaveEventWritesCache(t *testing.T) {
	app := withTestGlobals(t)
	r := newTestRoom(t, nil)

	p := newNamedPlayer(&fakeConn{}, "leaver", "hex-l")
	p.KickTime = 111
	p.MuteTime = 222
	r.EventManage.Fire(&event.PlayerLeaveEvent{Player: p})

	info, ok := app.Admin.PlayerDataCache["hex-l"]
	if !ok || info.TimesKicked != 111 || info.TimeMute != 222 {
		t.Fatalf("离线档案写入错误: %v", info)
	}
}

// TestBanEvent 封禁事件: 拉黑 UUID+IP, 踢人并广播
func TestBanEvent(t *testing.T) {
	app := withTestGlobals(t)
	r := newTestRoom(t, nil)

	con := &fakeConn{ip: "1.2.3.4"}
	p := newNamedPlayer(con, "bad", "hex-bad")
	watcher := &fakeConn{}
	r.PlayerManage.AddAbstractPlayer(watcher, namedData("w", "hex-w2"), nil)

	r.EventManage.Fire(&event.PlayerBanEvent{Player: p})

	if !app.Admin.IsUUIDBanned("hex-bad") {
		t.Fatal("UUID 应进黑名单")
	}
	kicks := con.kickReasons()
	if len(kicks) != 1 || kicks[0] != "kick.ban" {
		t.Fatalf("被封禁者应被踢出, got %v", kicks)
	}
	watcherMsgs := watcher.messages()
	if len(watcherMsgs) == 0 || !strings.Contains(watcherMsgs[0], "ban.yes") {
		t.Fatalf("全场应收到 ban.yes 广播, got %v", watcherMsgs)
	}
}

// TestIpBanEvent IP 封禁事件: 只拉黑 IP, 不动 UUID
func TestIpBanEvent(t *testing.T) {
	app := withTestGlobals(t)
	r := newTestRoom(t, nil)

	con := &fakeConn{ip: "5.6.7.8"}
	p := newNamedPlayer(con, "bad2", "hex-bad2")
	r.EventManage.Fire(&event.PlayerIpBanEvent{Player: p})

	if app.Admin.IsUUIDBanned("hex-bad2") {
		t.Fatal("IP 封禁不应拉黑 UUID")
	}
	if kicks := con.kickReasons(); len(kicks) != 1 {
		t.Fatalf("被封禁者应被踢出, got %v", kicks)
	}
}

// TestStartEvent 开局事件: 清空离线档案 + 进场广播 + RePlay 快照
func TestStartEvent(t *testing.T) {
	app := withTestGlobals(t)
	global.ConfigServer.StartAd = "good luck"
	global.ConfigServer.SaveRePlayFile = true
	r := newTestRoom(t, nil)
	app.Admin.PlayerDataCache["stale"] = global.NewPlayerInfo("stale", 0, 0)

	watcher := &fakeConn{}
	r.PlayerManage.AddAbstractPlayer(watcher, namedData("w", "hex-w3"), nil)
	r.PlayerManage.AddAbstractPlayer(&fakeConn{}, namedData("starter", "hex-s"), nil)

	r.EventManage.Fire(&event.ServerGameStartEvent{})

	if len(app.Admin.PlayerDataCache) != 0 {
		t.Fatal("开局后离线档案应清空")
	}
	watcherMsgs := watcher.messages()
	if len(watcherMsgs) == 0 || watcherMsgs[0] != "good luck" {
		t.Fatalf("开局广告应广播, got %v", watcherMsgs)
	}
	if len(r.PlayerData) != 2 {
		t.Fatalf("RePlay 快照应有 2 名非 AI 玩家, got %d", len(r.PlayerData))
	}
}

// TestGameOverEventFired gr() 触发 ServerGameOverEvent (cleanData 链)
func TestGameOverEventFired(t *testing.T) {
	withTestGlobals(t)
	quietCheckThread(t)
	r := newTestRoom(t, &fakeSim{})

	fired := false
	event.Register(r.EventManage, func(*event.ServerGameOverEvent) { fired = true })

	if err := r.StartGame(); err != nil {
		t.Fatalf("StartGame: %v", err)
	}
	r.Gr()

	if !fired {
		t.Fatal("gr() 应触发 ServerGameOverEvent")
	}
}

// TestGameOverReplayZip saveRePlayFile 启用时, 结算事件把玩家档案打包成 zip
func TestGameOverReplayZip(t *testing.T) {
	withTestGlobals(t)
	quietCheckThread(t)
	global.ConfigServer.SaveRePlayFile = true
	file.SetBasePath(t.TempDir())

	r := newTestRoom(t, &fakeSim{})
	r.ReplayFileName = "replay-1.json"
	r.PlayerData = []map[string]any{{"Position": 1, "Name": "p1"}}

	if err := r.StartGame(); err != nil {
		t.Fatalf("StartGame: %v", err)
	}
	r.Gr()

	zipFile := file.GetFolder(global.ServerPlayerPath).ToFile("replay-1.zip")
	if !zipFile.Exists() {
		t.Fatal("结算后应写出 replay-1.zip")
	}
}

// TestVoteStoppedOnCleanThread cleanThread 应停止进行中的投票
// (经 global.Vote 槽位 + 窄接口)
func TestVoteStoppedOnCleanThread(t *testing.T) {
	withTestGlobals(t)
	r := newTestRoom(t, nil)

	v := &recordingVote{}
	global.Vote = v
	t.Cleanup(func() { global.Vote = nil })

	r.cleanThread()
	if !v.stopped {
		t.Fatal("cleanThread 应调用 StopVote")
	}
}

// recordingVote 实现 voteStopper 窄接口
type recordingVote struct{ stopped bool }

func (v *recordingVote) StopVote() { v.stopped = true }
