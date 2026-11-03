// Package ex 对应 net.rwhps.server.command.ex
package ex

import (
	"errors"
	"math"
	"sync"
	"time"

	"rwhps/internal/core/thread"
	"rwhps/internal/data/global"
	"rwhps/internal/game/player"
	"rwhps/internal/game/room"
	"rwhps/internal/struct/list"
	"rwhps/internal/util/log"
)

// Vote 对应 Vote: 为游戏提供一个默认的 Vote 接口
//
// 上游字段逐一保留. 并发模型与上游一致: 投票路径 (聊天线程) 与 VoteTask
// 定时器会并发读写本对象, 上游没有加锁, Go 版同样不加 —— 照搬其语义.
type Vote struct {
	// command 对应 command
	command string
	// Player 对应 player: 发起投票的玩家
	Player *player.Player
	// TargetPlayer 对应 targetPlayer
	TargetPlayer *player.Player
	// isTeam 对应 isTeam
	isTeam bool

	// require 对应 require: 通过所需票数
	require int
	// pass 对应 pass: 当前赞成票数
	pass int

	// 四个回调对应上游同名字段, 预处理时按投票类型填充
	endNoMsg      func()
	endYesMsg     func()
	votePlayerIng func()
	voteIng       func()

	// reciprocal 对应 reciprocal: 剩余倒计时秒数
	reciprocal int

	// playerList 对应 playerList: 已投票玩家的 connectHexID
	playerList *list.Seq[string]
}

// NewVote 对应 Vote(command, hostPlayer): 全体参与型投票 (如 gameover)
func NewVote(command string, hostPlayer *player.Player) *Vote {
	v := newVote(command, hostPlayer, nil)
	return v
}

// NewVoteWithTarget 对应 Vote(command, hostPlayer, targetPlayer): 针对某玩家的投票
func NewVoteWithTarget(command string, hostPlayer, targetPlayer *player.Player) *Vote {
	return newVote(command, hostPlayer, targetPlayer)
}

func newVote(command string, hostPlayer, targetPlayer *player.Player) *Vote {
	v := &Vote{
		command:      command,
		Player:       hostPlayer,
		TargetPlayer: targetPlayer,
		reciprocal:   60,
		playerList:   list.NewSeq[string](),
		endNoMsg:     func() {},
		endYesMsg:    func() {},
		votePlayerIng: func() {},
		voteIng:       func() {},
	}
	v.preprocessing()
	return v
}

// ToVote 对应 toVote(votePlayer, playerPick)
func (v *Vote) ToVote(votePlayer *player.Player, playerPick string) {
	if v.playerList.Contains(votePlayer.ConnectHexID(), func(a, b string) bool { return a == b }) {
		_ = votePlayer.SendSystemMessage(votePlayer.GetInput("vote.rey"))
		return
	}

	const accapt = "y"
	const noAccapt = "n"

	switch playerPick {
	case accapt:
		if v.isTeam {
			if votePlayer.Team() == v.Player.Team() {
				v.pass++
				v.playerList.Add(votePlayer.ConnectHexID())
				_ = votePlayer.SendSystemMessage(votePlayer.GetInput("vote.y"))
				v.votePlayerIng()
			} else {
				_ = votePlayer.SendSystemMessage(votePlayer.GetInput("vote.team"))
			}
		} else {
			v.pass++
			v.playerList.Add(votePlayer.ConnectHexID())
			_ = votePlayer.SendSystemMessage(votePlayer.GetInput("vote.y"))
			v.votePlayerIng()
		}
	case noAccapt:
		if v.isTeam {
			if votePlayer.Team() == v.Player.Team() {
				v.playerList.Add(votePlayer.ConnectHexID())
				_ = votePlayer.SendSystemMessage(votePlayer.GetInput("vote.n"))
			} else {
				_ = votePlayer.SendSystemMessage(votePlayer.GetInput("vote.team"))
			}
		} else {
			v.playerList.Add(votePlayer.ConnectHexID())
			_ = votePlayer.SendSystemMessage(votePlayer.GetInput("vote.n"))
		}
	}
	v.inspectEnd()
}

// preprocessing 对应 preprocessing: 按命令名分发到注册的启动逻辑
func (v *Vote) preprocessing() {
	commandStartMu.RLock()
	fn, ok := commandStartData[v.command]
	commandStartMu.RUnlock()

	if ok {
		fn(v)
	} else {
		_ = v.Player.SendSystemMessage(v.Player.GetInput("vote.end.err", v.command))
		v.clearUp()
	}
}

// targetName 对应 targetPlayer?.name ?: ""
func (v *Vote) targetName() string {
	if v.TargetPlayer == nil {
		return ""
	}
	return v.TargetPlayer.Name()
}

// normalDistribution 对应 normalDistribution: 正常 (全体) 投票
//
// 上游在回调里读 this.require 是"调用时求值" (start 会改写 require),
// Go 闭包经 v 引用同样在调用时读取.
func (v *Vote) normalDistribution() {
	r := room.Default()
	if r == nil {
		return
	}
	v.require = r.PlayerManage.PlayerGroup.Size()
	v.endNoMsg = func() {
		r.Call.SendSystemMessageLocal("vote.done.no", v.command+" "+v.targetName(), v.pass, v.require)
	}
	v.endYesMsg = func() { r.Call.SendSystemMessageLocal("vote.ok") }
	v.votePlayerIng = func() { r.Call.SendSystemMessageLocal("vote.y.ing", v.command, v.pass, v.require) }
	v.voteIng = func() { r.Call.SendSystemMessageLocal("vote.ing", v.reciprocal) }
	v.start(func() {
		r.Call.SendSystemMessageLocal("vote.start", v.Player.Name(), v.command+" "+v.targetName())
	})
}

// teamOnly 对应 teamOnly: 团队投票
func (v *Vote) teamOnly() {
	r := room.Default()
	if r == nil {
		return
	}
	require := 0
	r.PlayerManage.PlayerGroup.EachAll(func(e *player.Player) {
		if e.Team() == v.Player.Team() {
			require++
		}
	})
	v.require = require
	v.endNoMsg = func() {
		r.Call.SendSystemTeamMessageLocal(v.Player.Team(), "vote.done.no", v.command+" "+v.targetName(), v.pass, v.require)
	}
	v.endYesMsg = func() { r.Call.SendSystemTeamMessageLocal(v.Player.Team(), "vote.ok") }
	v.votePlayerIng = func() {
		r.Call.SendSystemTeamMessageLocal(v.Player.Team(), "vote.y.ing", v.command, v.pass, v.require)
	}
	v.voteIng = func() { r.Call.SendSystemTeamMessageLocal(v.Player.Team(), "vote.ing", v.reciprocal) }
	v.start(func() {
		r.Call.SendSystemTeamMessageLocal(v.Player.Team(), "vote.start", v.Player.Name(), v.command+" "+v.targetName())
	})
}

// start 对应 start(run): 计算通过阈值, 发起人自动计票, 未达标则挂 10 秒播报
func (v *Vote) start(run func()) {
	temp := v.require
	switch {
	case temp <= 1:
		// 上游此处发的是字面量 "vote.no1" (未走 i18n), 照搬
		_ = v.Player.SendSystemMessage("vote.no1")
		v.require = 1
	case temp <= 3:
		v.require = 2
	default:
		v.require = int(math.Ceil(float64(temp) / 2))
	}

	v.playerList.Add(v.Player.ConnectHexID())

	v.pass++

	if v.pass >= v.require {
		v.end()
	} else {
		thread.NewTimedTask(thread.VoteTask, 10*time.Second, 10*time.Second, func() {
			v.reciprocal -= 10
			v.voteIng()
			if v.reciprocal <= 0 {
				v.end()
				thread.CloseTimeTask(thread.VoteTask, nil)
			}
		})
		run()
	}
}

// inspectEnd 对应 inspectEnd
func (v *Vote) inspectEnd() {
	if v.pass >= v.require {
		v.end()
	}
}

// end 对应 end
func (v *Vote) end() {
	var voteErr error
	if v.pass >= v.require {
		v.endYesMsg()

		commandEndMu.RLock()
		fn, ok := commandEndData[v.command]
		commandEndMu.RUnlock()

		if ok {
			fn(v)
		} else {
			voteErr = errors.New("[Vote End] Server does not implement command: " + v.command)
			v.clearUp()
		}
	} else {
		v.endNoMsg()
	}
	v.clearUp()
	if voteErr != nil {
		log.Error(voteErr)
	}
}

// clearUp 对应 clearUp: 清理引用
func (v *Vote) clearUp() {
	thread.CloseTimeTask(thread.VoteTask, nil)

	v.playerList.Clear()

	noop := func() {}
	v.endNoMsg = noop
	v.endYesMsg = noop
	v.votePlayerIng = noop
	v.voteIng = noop

	v.pass = 0
	v.reciprocal = 60

	global.Vote = nil
}

// StopVote 对应 stopVote
func (v *Vote) StopVote() { v.clearUp() }

// --- companion ---

var (
	commandStartMu sync.RWMutex
	commandStartData = map[string]func(*Vote){}

	commandEndMu sync.RWMutex
	commandEndData = map[string]func(*Vote){}
)

func init() {
	commandStartData["gameover"] = func(v *Vote) { v.normalDistribution() }

	// 上游: commandEndData["gameover"] = { HeadlessModuleManage.hps.room.gr() }
	commandEndData["gameover"] = func(*Vote) {
		if r := room.Default(); r != nil {
			r.Gr()
		}
	}
}

// AddVoteFullParticipation 对应 addVoteFullParticipation: 注册全体参与型投票命令
func AddVoteFullParticipation(command string, run func(v *Vote)) bool {
	commandStartMu.Lock()
	defer commandStartMu.Unlock()
	if _, ok := commandStartData[command]; ok {
		return false
	}
	commandStartData[command] = func(v *Vote) {
		run(v)
		v.normalDistribution()
	}
	return true
}

// AddVoteTeamOnly 对应 addVoteTeamOnly: 注册团队投票命令
func AddVoteTeamOnly(command string, run func(v *Vote)) bool {
	commandStartMu.Lock()
	defer commandStartMu.Unlock()
	if _, ok := commandStartData[command]; ok {
		return false
	}
	commandStartData[command] = func(v *Vote) {
		run(v)
		v.isTeam = true
		v.teamOnly()
	}
	return true
}
