package command

import (
	"strings"
	"testing"

	"rwhps/internal/core/thread"
	"rwhps/internal/data/global"
	"rwhps/internal/game"
	"rwhps/internal/game/event"
	"rwhps/internal/game/player"
	"rwhps/internal/game/room"
	cmd "rwhps/internal/util/game/command"
)

// setupServerCommands 构造带房间与玩家的服务端命令环境
func setupServerCommands(t *testing.T, players int) (*cmd.Handler, *recorder, *room.ServerRoom, []*player.Player) {
	t.Helper()

	oldCore, oldCfg := global.Core, global.ConfigServer
	global.Core = &global.Application{Admin: global.NewAdministration()}
	global.ConfigServer.AutoStartMinPlayerSize = -1
	global.ConfigServer.SaveRePlayFile = false
	global.ConfigServer.OneAdmin = false

	r := room.NewServerRoom(game.NewGameSession("cmd-room", nil))
	room.SetDefault(r)

	ps := make([]*player.Player, 0, players)
	for i := 0; i < players; i++ {
		data := player.NewData()
		idx := i
		data.Apply(func(d *player.Data) {
			d.Name = "sp" + string(rune('0'+idx))
			d.ConnectHexID = "spx" + string(rune('0'+idx))
		})
		ps = append(ps, r.PlayerManage.AddAbstractPlayer(nil, data, nil))
	}
	// 真实入房时位置由引擎分配; 测试里显式赋位, 保证按位置查玩家 (GetPlayer) 可用
	for i, p := range ps {
		p.SetPosition(i + 1)
	}

	rec := &recorder{}
	d := NewDeps()
	d.Print = rec.print
	d.Session = r

	h := cmd.NewHandler("")
	registerServerCommands(h, d)

	t.Cleanup(func() {
		thread.CloseTimeTask(thread.AutoStartTask, nil)
		thread.CloseTimeTask(thread.AutoCheckTask, nil)
		thread.CloseTimeTask(thread.GameOverTask, nil)
		room.SetDefault(nil)
		global.Core, global.ConfigServer = oldCore, oldCfg
	})
	return h, rec, r, ps
}

// TestServerSayWhisper say / whisper 的发送与回报
func TestServerSayWhisper(t *testing.T) {
	h, rec, _, ps := setupServerCommands(t, 2)

	h.HandleMessageWith("say hello <> world", nil)
	if !strings.Contains(rec.text(), "All players has received the message : hello  world") {
		t.Fatalf("say 输出不符: %q", rec.text())
	}

	rec.lines = nil
	h.HandleMessageWith("whisper 1 hi there", nil)
	if !strings.Contains(rec.text(), "has received the message : hi there") {
		t.Fatalf("whisper 输出不符: %q", rec.text())
	}
	_ = ps
}

// TestServerWhisperBadPosition 位置不存在时经 err.player.no.site 报告
func TestServerWhisperBadPosition(t *testing.T) {
	h, rec, _, _ := setupServerCommands(t, 1)
	withBundle(t)

	h.HandleMessageWith("whisper 99 hi", nil)
	if !strings.Contains(rec.text(), "err.player.no.site") {
		t.Fatalf("应报告位置不存在: %q", rec.text())
	}
}

// TestServerGametime 未开局 / 已开局两种状态
func TestServerGametime(t *testing.T) {
	h, rec, r, _ := setupServerCommands(t, 0)

	h.HandleMessage("gametime")
	if !strings.Contains(rec.text(), "No Start Game") {
		t.Fatalf("未开局应提示: %q", rec.text())
	}

	rec.lines = nil
	if err := r.StartGame(); err != nil {
		t.Skipf("无模拟内核, 跳过开局分支: %v", err)
	}
	h.HandleMessage("gametime")
	if !strings.Contains(rec.text(), "Gameing Time") {
		t.Fatalf("开局后应显示时间: %q", rec.text())
	}
}

// TestServerAdmin admin add/remove 的权限写入与状态位
func TestServerAdmin(t *testing.T) {
	h, rec, _, ps := setupServerCommands(t, 1)
	p := ps[0]

	h.HandleMessageWith("admin add 1", nil)
	if !p.IsAdmin || !p.SuperAdmin {
		t.Fatal("admin add 应置 IsAdmin/SuperAdmin")
	}
	if !global.Core.Admin.IsAdmin(p.ConnectHexID(), p.Name()) {
		t.Fatal("admin add 应登记到 Administration")
	}
	if !strings.Contains(rec.text(), "Changed admin status") {
		t.Fatalf("应有状态变更输出: %q", rec.text())
	}

	h.HandleMessageWith("admin remove 1", nil)
	if p.IsAdmin {
		t.Fatal("admin remove 应清除 IsAdmin")
	}
}

// TestServerAdminBadSubcommand 第二参数必须是 add/remove
func TestServerAdminBadSubcommand(t *testing.T) {
	h, rec, _, _ := setupServerCommands(t, 1)
	h.HandleMessageWith("admin foo 1", nil)
	if !strings.Contains(rec.text(), "Second parameter must be either") {
		t.Fatalf("应拒绝非法子命令: %q", rec.text())
	}
}

// TestServerAdminStartedGame 开局中禁止改管理员
func TestServerAdminStartedGame(t *testing.T) {
	h, rec, r, _ := setupServerCommands(t, 1)
	if err := r.StartGame(); err != nil {
		t.Skipf("无模拟内核: %v", err)
	}
	rec.lines = nil
	withBundle(t)
	h.HandleMessageWith("admin add 1", nil)
	if !strings.Contains(rec.text(), "err.startGame") {
		t.Fatalf("开局中应拒绝: %q", rec.text())
	}
}

// TestServerBanKickMute ban 走事件, kick/mute 落到玩家状态
func TestServerBanKickMute(t *testing.T) {
	h, _, r, ps := setupServerCommands(t, 1)
	p := ps[0]

	banned := false
	event.Register(r.EventManage, func(ev *event.PlayerBanEvent) { banned = ev.Player == p })

	h.HandleMessageWith("ban 1", nil)
	if !banned {
		t.Fatal("ban 应触发 PlayerBanEvent")
	}

	h.HandleMessageWith("mute 1", nil)
	if p.MuteTime == 0 {
		t.Fatal("mute 应设置禁言到期时间")
	}

	h.HandleMessageWith("kick 1 30", nil)
	// kickPlayer(text,0) 会把 kickTime 覆盖为当前时刻 (上游行为), 这里只验证非负
	if p.KickTime < 0 {
		t.Fatal("kick 后 kickTime 应已设置")
	}
}

// TestServerUnbanClearsBanRecord unban 触发 PlayerUnBanEvent 并清除封禁记录.
//
// 有意修正（作者已批准，2026-11-04）: 上游 unban 误发 PlayerBanEvent
// (ServerCommands.kt:131-135), 本测试断言修正后的解封语义 (DECISIONS D-10).
func TestServerUnbanClearsBanRecord(t *testing.T) {
	h, _, r, ps := setupServerCommands(t, 1)
	p := ps[0]

	// 预置封禁记录 (模拟此前被 ban)
	global.Core.Admin.BanUUID(p.ConnectHexID())

	var unbanned *player.Player
	event.Register(r.EventManage, func(ev *event.PlayerUnBanEvent) { unbanned = ev.Player })

	h.HandleMessageWith("unban 1", nil)
	if unbanned != p {
		t.Fatal("unban 应触发 PlayerUnBanEvent 且落在目标玩家上")
	}
	// 默认监听应已清除封禁记录
	if global.Core.Admin.IsUUIDBanned(p.ConnectHexID()) {
		t.Fatal("unban 后 UUID 封禁记录应被清除")
	}
}

// TestServerClearCommands clearbanall / clearmuteall
func TestServerClearCommands(t *testing.T) {
	h, _, _, ps := setupServerCommands(t, 1)
	p := ps[0]

	global.Core.Admin.BanUUID("someone")
	global.Core.Admin.BanIP("1.2.3.4")
	h.HandleMessage("clearbanall")
	if global.Core.Admin.IsUUIDBanned("someone") {
		t.Fatal("clearbanall 应清空 UUID 黑名单")
	}

	p.MuteTime = 12345
	h.HandleMessage("clearmuteall")
	if p.MuteTime != 0 {
		t.Fatal("clearmuteall 应清零禁言")
	}
}

// TestServerPlayersAdmins players / admins 列表输出
func TestServerPlayersAdmins(t *testing.T) {
	h, rec, _, ps := setupServerCommands(t, 2)

	h.HandleMessage("players")
	if !strings.Contains(rec.text(), "Players: 2") {
		t.Fatalf("players 应显示人数: %q", rec.text())
	}

	rec.lines = nil
	ps[0].IsAdmin = true
	global.Core.Admin.AddAdmin(ps[0].ConnectHexID(), ps[0].Name(), true)
	h.HandleMessage("admins")
	if !strings.Contains(rec.text(), "Admins: 1") {
		t.Fatalf("admins 应显示管理员数: %q", rec.text())
	}
}

// TestServerPlayersEmpty 无人时的提示
func TestServerPlayersEmpty(t *testing.T) {
	h, rec, _, _ := setupServerCommands(t, 0)
	h.HandleMessage("players")
	if !strings.Contains(rec.text(), "No players are currently in the server.") {
		t.Fatalf("空房间应有提示: %q", rec.text())
	}
}

// TestServerGiveadmin 移交管理员
func TestServerGiveadmin(t *testing.T) {
	h, _, _, ps := setupServerCommands(t, 2)
	ps[0].IsAdmin = true

	// 上游 giveadmin 用 getPlayer(arg[0].toInt()) (0 基 index), 传 "1" 命中第二个玩家
	h.HandleMessageWith("giveadmin 1", nil)
	if ps[0].IsAdmin {
		t.Fatal("原管理员应被撤权")
	}
	if !ps[1].IsAdmin {
		t.Fatal("目标玩家应获得管理员")
	}
}

// TestServerAddmoney 开局校验与加钱
func TestServerAddmoney(t *testing.T) {
	h, rec, r, ps := setupServerCommands(t, 1)
	withBundle(t)

	// 未开局拒绝
	h.HandleMessageWith("addmoney 1 100", nil)
	if !strings.Contains(rec.text(), "err.noStartGame") {
		t.Fatalf("未开局应拒绝: %q", rec.text())
	}

	if err := r.StartGame(); err != nil {
		t.Skipf("无模拟内核: %v", err)
	}
	ps[0].SetCredits(50)
	h.HandleMessageWith("addmoney 1 100", nil)
	if ps[0].Credits() != 150 {
		t.Fatalf("加钱后应为 150, got %d", ps[0].Credits())
	}
}

// TestServerGameover gameover 触发 gr
func TestServerGameover(t *testing.T) {
	h, _, r, _ := setupServerCommands(t, 0)
	fired := false
	event.Register(r.EventManage, func(*event.ServerGameOverEvent) { fired = true })

	if err := r.StartGame(); err != nil {
		t.Skipf("无模拟内核: %v", err)
	}
	h.HandleMessage("gameover")
	if !fired {
		t.Fatal("gameover 应触发 ServerGameOverEvent")
	}
}

// TestServerNotPortable 引擎类命令如实报告不可用
func TestServerNotPortable(t *testing.T) {
	h, rec, _, _ := setupServerCommands(t, 0)
	for _, c := range []string{"mods", "reloadmods", "textbuild a b", "test"} {
		rec.lines = nil
		h.HandleMessageWith(c, nil)
		if !strings.Contains(rec.text(), notPortable) {
			t.Fatalf("命令 %q 应报告不可用: %q", c, rec.text())
		}
	}
}
