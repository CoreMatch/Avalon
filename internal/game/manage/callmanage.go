package manage

import (
	"time"

	"rwhps/internal/core/thread"
	"rwhps/internal/data/global"
	"rwhps/internal/data/temp"
	"rwhps/internal/game/player"
	"rwhps/internal/util/log"
	"rwhps/internal/util/timeutil"
)

// RoomHooks 是 CallManage 需要的房间能力 (消费方定义的接缝).
//
// 上游经 gameModule.room 动态取房间; Go 里 manage 与 game 不能互相导入,
// 由房间在装配时把自己传进来. 对应上游 CallManage 读到的房间成员:
// roomID / endTime / flagData / gr(). 方法名带 Get 前缀是为了避免与
// 实现方 (嵌入了 GameSession 的 ServerRoom) 的推广字段重名.
type RoomHooks interface {
	GetRoomID() string
	GetEndTime() int64
	GetFlagData() *temp.ServerCacheFlag
	Gr()
}

// CallManage 对应 CallManage: 房间的广播与自动检查
type CallManage struct {
	pm   *PlayerManage
	room RoomHooks
}

// NewCallManage 对应 CallManage(gameModule); 同时完成与 PlayerManage 的互指
// (上游两者都挂在 room 上, 互相经 room 可达).
func NewCallManage(pm *PlayerManage, room RoomHooks) *CallManage {
	c := &CallManage{pm: pm, room: room}
	pm.attachCall(c)
	return c
}

// SendSystemMessage 对应 sendSystemMessage(text)
func (c *CallManage) SendSystemMessage(text string) {
	c.pm.PlayerGroup.EachAll(func(p *player.Player) {
		if err := p.SendSystemMessage(text); err != nil {
			log.Errorf("[Call]", "send system message: {0}", err)
		}
	})
}

// SendSystemMessageLocal 对应 sendSystemMessageLocal(text, obj)
//
// 上游另有同体的 sendSystemMessage(text, obj) 重载, Go 无重载, 统一为本方法.
// 每个玩家用自己的语言包取词 (上游即如此).
func (c *CallManage) SendSystemMessageLocal(key string, obj ...any) {
	c.pm.PlayerGroup.EachAll(func(p *player.Player) {
		if err := p.SendSystemMessage(p.GetInput(key, obj...)); err != nil {
			log.Errorf("[Call]", "send system message: {0}", err)
		}
	})
}

// SendSystemTeamMessageLocal 对应 sendSystemTeamMessageLocal(team, text, obj)
func (c *CallManage) SendSystemTeamMessageLocal(team int, key string, obj ...any) {
	c.pm.PlayerGroup.EachAll(func(p *player.Player) {
		if p.Team() != team {
			return
		}
		if err := p.SendSystemMessage("[TEAM] " + p.GetInput(key, obj...)); err != nil {
			log.Errorf("[Call]", "send team message: {0}", err)
		}
	})
}

// SyncAllPlayer 对应 syncAllPlayer
//
// 上游: gameModule.gameLinkFunction.allPlayerSync() —— 引擎级全量同步.
// Go 版需要 helper 协议操作支持, 当前未定义 (DECISIONS D-9), 先留空操作.
func (c *CallManage) SyncAllPlayer() {}

// KillAllPlayer 对应 killAllPlayer(msg)
//
// 上游遍历的是 playerGroup.clone() —— 踢人会把玩家移出原列表,
// 不克隆会漏. Go 版用 Elements() 快照同理.
func (c *CallManage) KillAllPlayer(msg string) {
	for _, p := range c.pm.PlayerGroup.Elements() {
		p.KickPlayer(msg, 0)
	}
}

// StartCheckThread 对应 startCheckThread: 每秒一次的房间自动检查
//
// 分支与上游逐条一致:
//  1. neverEnd 插件模式下直接跳过
//  2. 超过 maxGameIngTime: 提醒一次 (forcedCloseSendMsg), 再超 60s 强制 gr()
//  3. 全 AI 局 (flagData.ai): 提醒一次 (aiWarn), 无人后按 maxOnlyAIGameIngTime 倒计时 gr()
//  4. 无 AI: 0 人立即 gr(); 1 人提醒一次并挂 1 分钟 gr() 倒计时; 回到多人则取消倒计时
//  5. sendGameStatusFlag: 播报结算数据 —— 数据来自引擎 (gameHessData.getGameOverData),
//     helper 协议尚未推送该数据 (DECISIONS D-9), 与上游 `?: return` 等价地跳过
func (c *CallManage) StartCheckThread() {
	var aiEndTime int64

	thread.NewTimedTask(thread.AutoCheckTask, 0, 1*time.Second, func() {
		if global.NeverEnd {
			return
		}

		cfg := global.ConfigServer
		now := int64(timeutil.ConcurrentSecond())

		if cfg.MaxGameIngTime != -1 && now > c.room.GetEndTime() {
			if c.room.GetFlagData().ForcedCloseSendMsg() {
				c.SendSystemMessageLocal("gameOver.forced")
			}
			c.room.GetFlagData().SetForcedCloseSendMsg(false)
			if now > c.room.GetEndTime()+60 {
				c.room.Gr()
				return
			}
		}

		if c.room.GetFlagData().Ai() {
			if c.room.GetFlagData().AiWarn() {
				c.room.GetFlagData().SetAiWarn(false)
				c.SendSystemMessageLocal("gameOver.ai")
			}
			if c.pm.PlayerGroup.Size() == 0 {
				if aiEndTime == 0 {
					aiEndTime = now + int64(cfg.MaxOnlyAIGameIngTime)
				} else if cfg.MaxOnlyAIGameIngTime != -1 && now > aiEndTime {
					c.room.Gr()
					return
				}
			} else {
				aiEndTime = 0
			}
		} else {
			switch c.pm.PlayerGroup.Size() {
			case 0:
				c.room.Gr()
			case 1:
				if c.room.GetFlagData().OneSay() {
					c.room.GetFlagData().SetOneSay(false)
					c.SendSystemMessageLocal("gameOver.oneMin")
					thread.NewCountdown(thread.GameOverTask, 1*time.Minute, func() {
						c.room.Gr()
					})
				}
			default:
				if thread.ContainsTimeTask(thread.GameOverTask) {
					c.room.GetFlagData().SetOneSay(true)
					thread.CloseTimeTask(thread.GameOverTask, nil)
				}
			}
		}

		if c.room.GetFlagData().SendGameStatusFlag() {
			// 上游: gameOverData = gameHessData.getGameOverData() ?: return
			// Go 侧拿不到引擎结算数据, 等价于上游拿到 nil 后直接返回.
			return
		}
	})
}
