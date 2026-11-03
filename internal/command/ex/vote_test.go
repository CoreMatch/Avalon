package ex

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
)

// recConn 记录系统消息的连接假实现 (带锁: 投票定时器可能并发广播)
type recConn struct {
	mu  sync.Mutex
	msg []string
}

func (c *recConn) SendSystemMessage(text string) error {
	c.mu.Lock()
	c.msg = append(c.msg, text)
	c.mu.Unlock()
	return nil
}
func (c *recConn) SendChatMessage(_, _ string, _ int) error { return nil }
func (c *recConn) SendKick(string) error                    { return nil }
func (c *recConn) SendRelayServerType(string, func(string)) {}
func (c *recConn) SendPacket(*packet.Packet)                {}
func (c *recConn) SendJumpToAnotherServer(string)           {}
func (c *recConn) IP() string                               { return "127.0.0.1" }
func (c *recConn) UseConnectionAgreement() string           { return "TCP" }
func (c *recConn) IPCountry() string                        { return "0" }

func (c *recConn) messages() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.msg))
	copy(out, c.msg)
	return out
}

// setupVoteRoom 造一个带 n 名玩家的单例房间, 并负责收尾全局状态
func setupVoteRoom(t *testing.T, n int) (*room.ServerRoom, []*player.Player, []*recConn) {
	t.Helper()

	oldCore, oldCfg, oldVote := global.Core, global.ConfigServer, global.Vote
	global.Core = &global.Application{Admin: global.NewAdministration()}
	global.ConfigServer.AutoStartMinPlayerSize = -1
	global.ConfigServer.SaveRePlayFile = false
	global.ConfigServer.OneAdmin = false

	r := room.NewServerRoom(game.NewGameSession("vote-room", nil))
	room.SetDefault(r)

	players := make([]*player.Player, 0, n)
	conns := make([]*recConn, 0, n)
	for i := 0; i < n; i++ {
		con := &recConn{}
		data := player.NewData()
		idx := i
		data.Apply(func(d *player.Data) {
			d.Name = "p" + string(rune('0'+idx))
			d.ConnectHexID = "hex-v" + string(rune('0'+idx))
		})
		p := r.PlayerManage.AddAbstractPlayer(con, data, nil)
		players = append(players, p)
		conns = append(conns, con)
	}

	t.Cleanup(func() {
		thread.CloseTimeTask(thread.VoteTask, nil)
		thread.CloseTimeTask(thread.AutoStartTask, nil)
		room.SetDefault(nil)
		global.Core, global.ConfigServer, global.Vote = oldCore, oldCfg, oldVote
	})
	return r, players, conns
}

// TestVoteThresholdAndPass 3 人局: 阈值 2, 发起人计票, 第二票通过并执行 gameover
func TestVoteThresholdAndPass(t *testing.T) {
	r, players, conns := setupVoteRoom(t, 3)

	gameOverFired := false
	event.Register(r.EventManage, func(*event.ServerGameOverEvent) { gameOverFired = true })

	v := NewVote("gameover", players[0])
	global.Vote = v

	// 3 人 -> ceil(3/2) = 2 票通过; 发起人自动 1 票
	if v.require != 2 || v.pass != 1 {
		t.Fatalf("require=%d pass=%d, 期望 2/1", v.require, v.pass)
	}
	if !thread.ContainsTimeTask(thread.VoteTask) {
		t.Fatal("未达标时应挂 VoteTask 倒计时")
	}

	v.ToVote(players[1], "y")

	if gameOverFired != true {
		t.Fatal("票数达标应执行 gameover (gr)")
	}
	if global.Vote != nil {
		t.Fatal("投票结束后 Data.vote 应清空")
	}
	// 通过的广播应送达 (vote.ok 经 i18n 回落为 key)
	var sawOK bool
	for _, m := range conns[2].messages() {
		if strings.Contains(m, "vote.ok") {
			sawOK = true
		}
	}
	if !sawOK {
		t.Fatalf("在场玩家应收到 vote.ok, got %v", conns[2].messages())
	}
}

// TestVoteRejectAndDuplicate n 票与重复投票
func TestVoteRejectAndDuplicate(t *testing.T) {
	_, players, conns := setupVoteRoom(t, 5)

	v := NewVote("gameover", players[0])
	global.Vote = v
	// 5 人 -> ceil(5/2) = 3
	if v.require != 3 {
		t.Fatalf("require=%d, 期望 3", v.require)
	}

	v.ToVote(players[1], "n")
	if v.pass != 1 {
		t.Fatalf("反对票不应计入 pass: %d", v.pass)
	}

	// 上游: 投过票 (含反对) 即进 playerList, 再投一律拒绝
	v.ToVote(players[1], "y")
	if v.pass != 1 {
		t.Fatalf("已投票玩家再次投票应被拒绝: %d", v.pass)
	}
	var sawRey bool
	for _, m := range conns[1].messages() {
		if strings.Contains(m, "vote.rey") {
			sawRey = true
		}
	}
	if !sawRey {
		t.Fatalf("重复投票应收到 vote.rey, got %v", conns[1].messages())
	}

	v.ToVote(players[2], "y")
	v.ToVote(players[3], "y")
	if global.Vote != nil {
		t.Fatal("3 票达标后投票应结束")
	}
}

// TestVoteSinglePlayer 1 人局: 阈值压到 1, 发起人直接通过
func TestVoteSinglePlayer(t *testing.T) {
	r, players, conns := setupVoteRoom(t, 1)

	gameOverFired := false
	event.Register(r.EventManage, func(*event.ServerGameOverEvent) { gameOverFired = true })

	_ = NewVote("gameover", players[0])

	// 发起人 1 票即达标, 构造期内即完成 end -> clearUp
	if !gameOverFired {
		t.Fatal("1 人局发起投票应立即通过")
	}
	if thread.ContainsTimeTask(thread.VoteTask) {
		t.Fatal("立即通过的投票不应挂 VoteTask")
	}
	// 上游: 人数 <= 1 时先发字面量 vote.no1
	var sawNo1 bool
	for _, m := range conns[0].messages() {
		if m == "vote.no1" {
			sawNo1 = true
		}
	}
	if !sawNo1 {
		t.Fatalf("1 人局应收到 vote.no1, got %v", conns[0].messages())
	}
}

// TestVoteUnknownCommand 未注册的命令: 报错并清理
func TestVoteUnknownCommand(t *testing.T) {
	_, players, conns := setupVoteRoom(t, 2)

	v := NewVote("nosuchcmd", players[0])
	global.Vote = v

	var sawErr bool
	for _, m := range conns[0].messages() {
		if strings.Contains(m, "vote.end.err") {
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatalf("未注册命令应收到 vote.end.err, got %v", conns[0].messages())
	}
	// 上游: 构造期 clearUp 已把内部状态清空 (回调为空 / 无 VoteTask),
	// 随后 Data.vote = Vote(...) 的赋值把一个死对象挂回去 —— 照搬该时序
	if thread.ContainsTimeTask(thread.VoteTask) {
		t.Fatal("未注册命令不应挂 VoteTask")
	}
	if v.require != 0 || v.pass != 0 {
		t.Fatal("未注册命令的投票对象应为清零状态")
	}
}

// TestStopVote stopVote 清理
func TestStopVote(t *testing.T) {
	_, players, _ := setupVoteRoom(t, 3)

	v := NewVote("gameover", players[0])
	global.Vote = v
	v.StopVote()

	if global.Vote != nil {
		t.Fatal("StopVote 后 Data.vote 应清空")
	}
	if thread.ContainsTimeTask(thread.VoteTask) {
		t.Fatal("StopVote 后 VoteTask 应收掉")
	}
}

// TestAddVoteFullParticipation 插件注册全体投票命令
func TestAddVoteFullParticipation(t *testing.T) {
	r, players, _ := setupVoteRoom(t, 3)

	ran := false
	if !AddVoteFullParticipation("customvote", func(*Vote) { ran = true }) {
		t.Fatal("首次注册应成功")
	}
	t.Cleanup(func() {
		commandStartMu.Lock()
		delete(commandStartData, "customvote")
		commandStartMu.Unlock()
	})
	if AddVoteFullParticipation("customvote", func(*Vote) {}) {
		t.Fatal("重复注册应返回 false")
	}

	grFired := false
	event.Register(r.EventManage, func(*event.ServerGameOverEvent) { grFired = true })

	v := NewVote("customvote", players[0])
	global.Vote = v
	if !ran {
		t.Fatal("注册时的 run 回调应执行")
	}
	// customvote 没有 end 实现: 达标后走 "does not implement" 分支
	v.ToVote(players[1], "y")
	if global.Vote != nil {
		t.Fatal("投票应结束")
	}
	if grFired {
		t.Fatal("customvote 不应触发 gameover")
	}
}

// TestAddVoteTeamOnly 团队投票: 只统计同队成员
func TestAddVoteTeamOnly(t *testing.T) {
	_, players, conns := setupVoteRoom(t, 3)

	if !AddVoteTeamOnly("teamvote", func(*Vote) {}) {
		t.Fatal("首次注册应成功")
	}
	t.Cleanup(func() {
		commandStartMu.Lock()
		delete(commandStartData, "teamvote")
		commandStartMu.Unlock()
	})

	// p0/p1 同队, p2 另一队
	players[0].SetTeam(1)
	players[1].SetTeam(1)
	players[2].SetTeam(2)

	v := NewVote("teamvote", players[0])
	global.Vote = v
	// 同队 2 人 -> 阈值 2; 发起人 1 票
	if v.require != 2 || v.pass != 1 {
		t.Fatalf("require=%d pass=%d, 期望 2/1", v.require, v.pass)
	}

	// 异队投票被拒
	v.ToVote(players[2], "y")
	if v.pass != 1 {
		t.Fatal("异队玩家不能参与团队投票")
	}
	var sawTeam bool
	for _, m := range conns[2].messages() {
		if strings.Contains(m, "vote.team") {
			sawTeam = true
		}
	}
	if !sawTeam {
		t.Fatalf("异队玩家应收到 vote.team, got %v", conns[2].messages())
	}

	// 同队通过
	v.ToVote(players[1], "y")
	if global.Vote != nil {
		t.Fatal("同队 2 票应通过")
	}
}
