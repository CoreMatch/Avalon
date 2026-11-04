package manage

import (
	"errors"
	"strings"
	"sync"

	"rwhps/internal/data/global"
	"rwhps/internal/game/player"
	"rwhps/internal/struct/list"
	"rwhps/internal/util/isutils"
)

// ErrNotPortable 表示该能力依赖 JVM 内核且当前 helper 协议没有对应操作,
// 无法在 Go 侧等价实现 (见 DECISIONS D-9).
var ErrNotPortable = errors.New("manage: not portable without helper protocol support")

// PlayerManage 对应 PlayerManage: Hess 下的玩家管理器
//
// 上游构造需要 gameModule (内核模块), 用于 addAI 与 cleanPlayerAllData 时
// 回调内核; Go 版内核在 helper 内:
//   - addAI 需要协议扩展 (DECISIONS D-9), 当前返回错误
//   - cleanPlayerAllData 的 killAllPlayer 委托给同包 CallManage
type PlayerManage struct {
	// PlayerGroup 对应 playerGroup: 在线玩家
	PlayerGroup *list.Seq[*player.Player]
	// PlayerAll 对应 playerAll: 全部玩家 (含对局中暂离的)
	PlayerAll *list.Seq[*player.Player]

	mu sync.Mutex

	// call 对应 gameModule.room.call 的可达性 (cleanPlayerAllData 用),
	// 由房间装配时注入, 避免 manage <-> game 成环
	call *CallManage
}

// NewPlayerManage 对应 PlayerManage(gameModule)
//
// 上游两个列表都是 Seq(16, threadSafety = true): 定时器任务 (自动开局倒计时 /
// 自动检查) 会在自己的线程里遍历它们, 与连接线程的增删并发.
func NewPlayerManage() *PlayerManage {
	return &PlayerManage{
		PlayerGroup: list.NewSeqCap[*player.Player](16, true),
		PlayerAll:   list.NewSeqCap[*player.Player](16, true),
	}
}

// attachCall 由房间装配时调用 (同包内接线)
func (m *PlayerManage) attachCall(c *CallManage) {
	m.mu.Lock()
	m.call = c
	m.mu.Unlock()
}

// AddAI 对应 addAI
//
// 上游: gameScriptMultiPlayer.addAi() 让引擎生成一个 AI, 然后把引擎回报的
// AI 玩家包装成 AiPlayer. Go 版引擎在 helper 内:
//
// 有意修正（作者已批准，2026-11-04）: 经房间转发到 helper 协议 v2-ext 的
// room.addai 操作 (DECISIONS D-9); AI 档案随后经既有的 player.join 事件
// 回到 Go 侧. 无后端时房间侧返回 ErrNotPortable, 与既有降级行为一致.
//
// 上游在 addAi 之后还会扫描位置把引擎回报的 AI 包装成 AiPlayer; Go 版该
// 包装由连接层的 player.join 事件路径完成, 不在本方法内.
func (m *PlayerManage) AddAI() error {
	m.mu.Lock()
	call := m.call
	m.mu.Unlock()
	if call == nil || call.room == nil {
		return ErrNotPortable
	}
	return call.room.AddAI()
}

// AddAbstractPlayer 对应 addAbstractPlayer(con, playerData, i18NBundle)
//
// 语义逐条照搬:
//  1. playerAll 里按 connectHexID 找到旧实体 -> 重连: 换绑连接, 不新建,
//     只进 playerGroup (playerAll 里已有)
//  2. 找不到 -> 新建; oneAdmin 配置下第一个在线玩家自动成为管理员;
//     已登记的管理员按 playerAdminData 恢复 admin/superAdmin
//  3. 新实体进 playerAll
//  4. 无论新旧都进 playerGroup
func (m *PlayerManage) AddAbstractPlayer(con player.PlayerConnect, data *player.Data, i18n func(key string, params ...any) string) *player.Player {
	var found *player.Player

	if old, ok := m.PlayerAll.Find(func(p *player.Player) bool {
		return p.ConnectHexID() == data.GetConnectHexID()
	}); ok {
		old.SetCon(con)
		found = old
	}

	if found == nil {
		found = player.New(con, i18n, data)

		// 上游: Data.configServer.oneAdmin —— 只自动分配一个管理员
		if global.ConfigServer.OneAdmin {
			if _, hasAutoAdmin := m.PlayerGroup.Find(func(p *player.Player) bool {
				return p.IsAdmin && p.AutoAdmin
			}); !hasAutoAdmin {
				found.IsAdmin = true
				found.AutoAdmin = true
			}
		}

		// 上游: Data.core.admin.isAdmin(player) -> 恢复登记过的权限
		if global.Core != nil && global.Core.Admin != nil && global.Core.Admin.IsAdmin(found.ConnectHexID(), found.Name()) {
			if info, ok := global.Core.Admin.AdminData(found.ConnectHexID()); ok {
				found.IsAdmin = info.Admin
				found.SuperAdmin = info.SuperAdmin
			}
		}

		m.PlayerAll.Add(found)
	}

	m.PlayerGroup.Add(found)
	return found
}

// FindPlayer 对应 findPlayer(writeConsole, findIn)
//
// findIn 为纯数字时按位置查 (上游 position = 数字 - 1), 否则按名字包含匹配
// (忽略大小写); 多个命中或找不到时经 writeConsole 报告并返回 nil.
func (m *PlayerManage) FindPlayer(writeConsole func(string), findIn string) *player.Player {
	var conTg *player.Player

	var findNameIn string
	findPositionIn := -1

	if isutils.IsNumeric(findIn) {
		n := 0
		for _, c := range findIn {
			n = n*10 + int(c-'0')
		}
		findPositionIn = n - 1
	} else {
		findNameIn = findIn
	}

	if findNameIn != "" {
		count := 0
		m.PlayerGroup.EachAll(func(p *player.Player) {
			if strings.Contains(strings.ToLower(p.Name()), strings.ToLower(findNameIn)) {
				conTg = p
				count++
			}
		})
		if count > 1 {
			writeConsole("目标不止一个, 请不要输入太短的玩家名")
			return nil
		}
		if conTg == nil {
			writeConsole("找不到玩家")
			return nil
		}
	}

	if findPositionIn >= 0 {
		conTg = nil
		m.PlayerGroup.EachAll(func(p *player.Player) {
			if p.Index() == findPositionIn {
				conTg = p
			}
		})
		if conTg == nil {
			writeConsole("找不到玩家")
			return nil
		}
	}

	return conTg
}

// RunPlayerArrayDataRunnable 对应 runPlayerArrayDataRunnable: 遍历 playerAll
func (m *PlayerManage) RunPlayerArrayDataRunnable(run func(*player.Player)) {
	m.PlayerAll.EachAll(run)
}

// GetPlayersNameOnTheSameTeam 对应 getPlayersNameOnTheSameTeam
func (m *PlayerManage) GetPlayersNameOnTheSameTeam(team int) []string {
	result := make([]string, 0)
	m.PlayerAll.EachAll(func(p *player.Player) {
		if p.Team() == team {
			result = append(result, p.Name())
		}
	})
	return result
}

// GetPlayer 对应 getPlayer(index): 在 playerAll 里按 index 查
func (m *PlayerManage) GetPlayer(index int) *player.Player {
	found, _ := m.PlayerAll.Find(func(p *player.Player) bool { return p.Index() == index })
	return found
}

// RemovePlayer 从两个列表移除 (上游在 disconnect / AI kick 里的移除动作)
func (m *PlayerManage) RemovePlayer(p *player.Player) {
	eq := func(a, b *player.Player) bool { return a == b }
	m.PlayerGroup.Remove(p, eq)
	m.PlayerAll.Remove(p, eq)
}

// RemoveFromGroup 只从在线列表移除 (对局中玩家断线: 上游保留在 playerAll)
func (m *PlayerManage) RemoveFromGroup(p *player.Player) {
	m.PlayerGroup.Remove(p, func(a, b *player.Player) bool { return a == b })
}

// RemoveFromAll 只从全量列表移除 (配合 RemoveFromGroup 完成上游的两步移除)
func (m *PlayerManage) RemoveFromAll(p *player.Player) {
	m.PlayerAll.Remove(p, func(a, b *player.Player) bool { return a == b })
}

// CleanPlayerAllData 对应 cleanPlayerAllData
//
// 上游: room.call.killAllPlayer("CleanAllPlayer") -> 清空两个列表.
func (m *PlayerManage) CleanPlayerAllData() {
	m.mu.Lock()
	call := m.call
	m.mu.Unlock()

	if call != nil {
		call.KillAllPlayer("CleanAllPlayer")
	}
	m.PlayerAll.Clear()
	m.PlayerGroup.Clear()
}
