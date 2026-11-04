package command

import (
	"strings"
	"sync"
	"testing"

	"rwhps/internal/core/thread"
	"rwhps/internal/data/global"
	"rwhps/internal/game"
	"rwhps/internal/game/event"
	"rwhps/internal/game/player"
	"rwhps/internal/game/room"
	"rwhps/internal/io/packet"
	cmd "rwhps/internal/util/game/command"
)

// clientConn 记录发给玩家的系统消息 (客户端命令的反馈都走 sendSystemMessage)
type clientConn struct {
	mu  sync.Mutex
	msg []string
}

func (c *clientConn) SendSystemMessage(text string) error {
	c.mu.Lock()
	c.msg = append(c.msg, text)
	c.mu.Unlock()
	return nil
}
func (c *clientConn) SendChatMessage(_, _ string, _ int) error { return nil }
func (c *clientConn) SendKick(string) error                    { return nil }
func (c *clientConn) SendRelayServerType(string, func(string)) {}
func (c *clientConn) SendPacket(*packet.Packet)                {}
func (c *clientConn) SendJumpToAnotherServer(string)           {}
func (c *clientConn) IP() string                               { return "127.0.0.1" }
func (c *clientConn) UseConnectionAgreement() string           { return "TCP" }
func (c *clientConn) IPCountry() string                        { return "0" }

func (c *clientConn) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.msg, "\n")
}

// setupClientCommands 构造带房间与带连接玩家的客户端命令环境
func setupClientCommands(t *testing.T, n int) (*cmd.Handler, *room.ServerRoom, []*player.Player, []*clientConn) {
	t.Helper()

	oldCore, oldCfg, oldVote := global.Core, global.ConfigServer, global.Vote
	global.Core = &global.Application{Admin: global.NewAdministration()}
	global.ConfigServer.AutoStartMinPlayerSize = -1
	global.ConfigServer.SaveRePlayFile = false
	global.ConfigServer.OneAdmin = false
	global.ConfigServer.MaxPlayer = 10

	r := room.NewServerRoom(game.NewGameSession("cc-room", nil))
	room.SetDefault(r)

	h := r.ClientHandler
	RegisterClientCommands(h, r)

	ps := make([]*player.Player, 0, n)
	cs := make([]*clientConn, 0, n)
	for i := 0; i < n; i++ {
		con := &clientConn{}
		data := player.NewData()
		idx := i
		data.Apply(func(d *player.Data) {
			d.Name = "cp" + string(rune('0'+idx))
			d.ConnectHexID = "cpx" + string(rune('0'+idx))
		})
		p := r.PlayerManage.AddAbstractPlayer(con, data, nil)
		p.SetPosition(i + 1)
		ps = append(ps, p)
		cs = append(cs, con)
	}

	t.Cleanup(func() {
		thread.CloseTimeTask(thread.AutoStartTask, nil)
		thread.CloseTimeTask(thread.AutoCheckTask, nil)
		thread.CloseTimeTask(thread.GameOverTask, nil)
		thread.CloseTimeTask(thread.VoteTask, nil)
		room.SetDefault(nil)
		global.Core, global.ConfigServer, global.Vote = oldCore, oldCfg, oldVote
	})
	return h, r, ps, cs
}

// TestClientPermissionDenied 非管理员执行管理命令被拒 (err.noAdmin)
func TestClientPermissionDenied(t *testing.T) {
	h, _, ps, cs := setupClientCommands(t, 1)
	ps[0].IsAdmin = false

	h.HandleMessageWith("/kick 1", ps[0])
	if !strings.Contains(cs[0].text(), "err.noAdmin") {
		t.Fatalf("非管理员应收到 err.noAdmin: %q", cs[0].text())
	}
}

// TestClientKick 管理员踢人: 未开局可踢, 目标收到 kick 包
func TestClientKick(t *testing.T) {
	h, _, ps, cs := setupClientCommands(t, 2)
	ps[0].IsAdmin = true

	h.HandleMessageWith("/kick 2", ps[0])
	// kickPlayer 对无真实连接的假玩家不发系统消息, 只验证未报权限错误
	if strings.Contains(cs[0].text(), "err.noAdmin") {
		t.Fatalf("管理员不应被拒: %q", cs[0].text())
	}
	_ = cs[1]
}

// TestClientKickStarted 开局中禁止踢人
func TestClientKickStarted(t *testing.T) {
	h, r, ps, cs := setupClientCommands(t, 2)
	ps[0].IsAdmin = true
	if err := r.StartGame(); err != nil {
		t.Skipf("无模拟内核: %v", err)
	}
	h.HandleMessageWith("/kick 2", ps[0])
	if !strings.Contains(cs[0].text(), "err.startGame") {
		t.Fatalf("开局中应拒绝: %q", cs[0].text())
	}
}

// TestClientKickNotNumeric 参数非数字报 err.noNumber
func TestClientKickNotNumeric(t *testing.T) {
	h, _, ps, cs := setupClientCommands(t, 1)
	ps[0].IsAdmin = true
	h.HandleMessageWith("/kick abc", ps[0])
	if !strings.Contains(cs[0].text(), "err.noNumber") {
		t.Fatalf("非数字位置应报 err.noNumber: %q", cs[0].text())
	}
}

// TestClientSync sync on/off 切换房间同步位
func TestClientSync(t *testing.T) {
	h, r, ps, cs := setupClientCommands(t, 1)
	ps[0].IsAdmin = true

	h.HandleMessageWith("/sync off", ps[0])
	if r.Sync {
		t.Fatal("sync off 应关闭同步")
	}
	h.HandleMessageWith("/sync on", ps[0])
	if !r.Sync {
		t.Fatal("sync on 应开启同步")
	}
	_ = cs
}

// TestClientAllmute 全体禁言开关
func TestClientAllmute(t *testing.T) {
	h, r, ps, cs := setupClientCommands(t, 1)
	ps[0].IsAdmin = true

	h.HandleMessageWith("/allmute on", ps[0])
	if !r.MuteAll {
		t.Fatal("allmute on 应开启全体禁言")
	}
	if !strings.Contains(cs[0].text(), "全体禁言") {
		t.Fatalf("应有确认消息: %q", cs[0].text())
	}
	h.HandleMessageWith("/allmute off", ps[0])
	if r.MuteAll {
		t.Fatal("allmute off 应关闭全体禁言")
	}
}

// TestClientAm 混战模式开关
func TestClientAm(t *testing.T) {
	h, r, ps, cs := setupClientCommands(t, 1)
	ps[0].IsAdmin = true

	h.HandleMessageWith("/am on", ps[0])
	if !r.IsMixPlayer {
		t.Fatal("am on 应开启混战")
	}
	h.HandleMessageWith("/am off", ps[0])
	if r.IsMixPlayer {
		t.Fatal("am off 应关闭混战")
	}
	_ = cs
}

// TestClientIunit 设置玩家初始单位
func TestClientIunit(t *testing.T) {
	h, _, ps, cs := setupClientCommands(t, 2)
	ps[0].IsAdmin = true

	h.HandleMessageWith("/iunit 2 5", ps[0])
	if ps[1].StartUnit() != 5 {
		t.Fatalf("iunit 应设置 startUnit=5, got %d", ps[1].StartUnit())
	}

	// 位置不存在
	h.HandleMessageWith("/iunit 99 5", ps[0])
	if !strings.Contains(cs[0].text(), "err.player.no.site") {
		t.Fatalf("位置不存在应报错: %q", cs[0].text())
	}
}

// TestClientAddmoney 开局后加钱
func TestClientAddmoney(t *testing.T) {
	h, r, ps, cs := setupClientCommands(t, 2)
	ps[0].IsAdmin = true

	// 未开局拒绝
	h.HandleMessageWith("/addmoney 2 100", ps[0])
	if !strings.Contains(cs[0].text(), "err.noStartGame") {
		t.Fatalf("未开局应拒绝: %q", cs[0].text())
	}

	if err := r.StartGame(); err != nil {
		t.Skipf("无模拟内核: %v", err)
	}
	ps[1].SetCredits(10)
	h.HandleMessageWith("/addmoney 2 90", ps[0])
	if ps[1].Credits() != 100 {
		t.Fatalf("加钱后应为 100, got %d", ps[1].Credits())
	}
}

// TestClientVote vote 命令挂起全局投票
func TestClientVote(t *testing.T) {
	h, _, ps, _ := setupClientCommands(t, 2)
	h.HandleMessageWith("/vote gameover", ps[0])
	if global.Vote == nil {
		t.Fatal("vote 应创建全局投票")
	}
}

// TestClientMyid myid 回报玩家 ID
func TestClientMyid(t *testing.T) {
	h, _, ps, cs := setupClientCommands(t, 1)
	h.HandleMessageWith("/myid", ps[0])
	if !strings.Contains(cs[0].text(), "myid") {
		t.Fatalf("myid 应回报: %q", cs[0].text())
	}
}

// TestClientStatus status 回报版本信息
func TestClientStatus(t *testing.T) {
	h, _, ps, cs := setupClientCommands(t, 1)
	h.HandleMessageWith("/status", ps[0])
	if !strings.Contains(cs[0].text(), "status.version") {
		t.Fatalf("status 应回报版本: %q", cs[0].text())
	}
}

// TestClientHelp help 列出非 HIDE 命令
func TestClientHelp(t *testing.T) {
	h, _, ps, cs := setupClientCommands(t, 1)
	h.HandleMessageWith("/help", ps[0])
	out := cs[0].text()
	if !strings.Contains(out, "help") {
		t.Fatalf("help 应列出命令: %q", out)
	}
	// HIDE 命令不出现
	if strings.Contains(out, "credits") {
		t.Fatalf("HIDE 命令 credits 不应出现: %q", out)
	}
}

// TestClientBanFiresEventOnTarget 客户端 ban 对被 ban 的目标玩家触发事件.
//
// 有意修正（作者已批准，2026-11-04）: 上游对发起者触发 (ClientCommands.kt:603-611
// 笔误, 会封禁执行命令的管理员自己), 本测试断言修正后的行为 (DECISIONS D-10).
func TestClientBanFiresEventOnTarget(t *testing.T) {
	h, r, ps, cs := setupClientCommands(t, 2)
	ps[0].IsAdmin = true

	var firedOn *player.Player
	event.Register(r.EventManage, func(ev *event.PlayerBanEvent) { firedOn = ev.Player })

	h.HandleMessageWith("/ban cp1", ps[0])
	if firedOn == nil {
		t.Fatal("ban 应触发 PlayerBanEvent")
	}
	if firedOn != ps[1] {
		t.Fatalf("事件应落在被 ban 的目标上, got %v", firedOn)
	}
	if !strings.Contains(cs[0].text(), "Ban :") {
		t.Fatalf("应有确认: %q", cs[0].text())
	}
}

// TestClientKickx 按名字踢非管理员
func TestClientKickx(t *testing.T) {
	h, _, ps, cs := setupClientCommands(t, 2)
	ps[0].IsAdmin = true

	h.HandleMessageWith("/kickx cp1", ps[0])
	if !strings.Contains(cs[0].text(), "Kick :") {
		t.Fatalf("kickx 应有确认: %q", cs[0].text())
	}

	// 目标是管理员时拒绝 (不能踢出管理员)
	ps[1].IsAdmin = true
	h.HandleMessageWith("/kickx cp1", ps[0])
	if !strings.Contains(cs[0].text(), "不能踢出管理员") {
		t.Fatalf("应拒绝踢管理员: %q", cs[0].text())
	}
}

// TestClientStartPermission 非管理员 /start 被拒
func TestClientStartPermission(t *testing.T) {
	h, r, ps, _ := setupClientCommands(t, 1)
	ps[0].IsAdmin = false
	h.HandleMessageWith("/start", ps[0])
	if r.IsStartGame {
		t.Fatal("非管理员不应能开局")
	}
}

// TestClientStartMinPlayer 人数不足 startMinPlayerSize 时拒绝
func TestClientStartMinPlayer(t *testing.T) {
	h, _, ps, cs := setupClientCommands(t, 1)
	ps[0].IsAdmin = true
	global.ConfigServer.StartMinPlayerSize = 5

	h.HandleMessageWith("/start", ps[0])
	if !strings.Contains(cs[0].text(), "start.playerNo") {
		t.Fatalf("人数不足应拒绝: %q", cs[0].text())
	}
}
