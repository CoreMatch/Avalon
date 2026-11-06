package command

import (
	"math/big"
	"strings"

	"rwhps/internal/command/ex"
	"rwhps/internal/data/global"
	"rwhps/internal/game"
	"rwhps/internal/game/event"
	"rwhps/internal/game/manage"
	"rwhps/internal/game/player"
	"rwhps/internal/game/room"
	"rwhps/internal/util/isutils"

	cmd "rwhps/internal/util/game/command"
)

// RegisterClientCommands 对应 ClientCommands(handler): 客户端 (游戏内聊天 "/") 命令.
//
// 上游在引擎模块加载时 ClientCommands(room.clientHandler) 注册; Go 版由启动链
// 在建房后调用本函数, 把命令挂到 room.ClientHandler. 命令回调的 param 即
// 发起命令的玩家 (*player.Player), 对应上游 { args, player -> } 的第二参.
func RegisterClientCommands(h *cmd.Handler, r *room.ServerRoom) {
	c := &clientCommands{room: r}
	c.registerGameCoreCommand(h)
	c.registerGameCommandX(h)
	c.registerGameCustomCommand(h)
}

// clientCommands 对应 ClientCommands 实例, 持有房间引用
type clientCommands struct {
	room *room.ServerRoom
}

// asPlayer 把命令回调的 param 还原为玩家; 非玩家 (如自动开局传 nil) 返回 nil
func asPlayer(param any) *player.Player {
	p, _ := param.(*player.Player)
	return p
}

// isAdmin 对应 ClientCommands.isAdmin: 非管理员时提示并返回 false
func (c *clientCommands) isAdmin(p *player.Player) bool {
	if p == nil {
		return false
	}
	if p.IsAdmin {
		return true
	}
	_ = p.SendSystemMessage(p.GetInput("err.noAdmin"))
	return false
}

// checkPositionNumb 对应 ClientCommands.checkPositionNumb
func (c *clientCommands) checkPositionNumb(s string, p *player.Player) bool {
	if isutils.NotIsNumeric(s) {
		_ = p.SendSystemMessage(p.GetInput("err.noNumber"))
		return false
	}
	n := atoiOr(s, 0)
	if n < 0 {
		_ = p.SendSystemMessage(p.GetInput("err.noInt"))
		return false
	}
	if n > global.ConfigServer.MaxPlayer {
		_ = p.SendSystemMessage(p.GetInput("err.maxPlayer"))
		return false
	}
	return true
}

// findPlayer 对应 ClientCommands.findPlayer: 在 playerAll 里按名字包含或位置查,
// 命中管理员时拒绝 (不能对管理员使用).
func (c *clientCommands) findPlayer(p *player.Player, findIn string) *player.Player {
	var conTg *player.Player

	var findNameIn string
	findPositionIn := -1
	if isutils.IsNumeric(findIn) {
		findPositionIn = atoiOr(findIn, 0)
	} else {
		findNameIn = findIn
	}

	if findNameIn != "" {
		count := 0
		c.room.PlayerManage.PlayerAll.EachAll(func(it *player.Player) {
			if strings.Contains(strings.ToLower(it.Name()), strings.ToLower(findNameIn)) {
				conTg = it
				count++
			}
		})
		if count > 1 {
			_ = p.SendSystemMessage("目标不止一个, 请不要输入太短的玩家名")
			return nil
		}
		if conTg == nil {
			_ = p.SendSystemMessage("找不到玩家")
			return nil
		}
	}

	if findPositionIn >= 0 {
		c.room.PlayerManage.PlayerAll.EachAll(func(it *player.Player) {
			if it.Position() == findPositionIn {
				conTg = it
			}
		})
		if conTg == nil {
			_ = p.SendSystemMessage("找不到玩家")
			return nil
		}
	}

	if conTg != nil && conTg.IsAdmin {
		_ = p.SendSystemMessage("不能踢出管理员")
		return nil
	}
	return conTg
}

// registerGameCoreCommand 对应 ClientCommands.registerGameCoreCommand
func (c *clientCommands) registerGameCoreCommand(h *cmd.Handler) {
	h.RegisterWith("credits", "<money>", "HIDE", func(args []string, param any) {
		p := asPlayer(param)
		if !c.isAdmin(p) {
			return
		}
		if isutils.NotIsNumeric(args[0]) {
			_ = p.SendSystemMessage(tr("err.noNumber"))
			return
		}
		// 上游: gameLinkServerData.credits = <money 映射> —— 引擎开局资金档位
		_ = p.SendSystemMessage(notPortable)
	})

	h.RegisterWith("nukes", "<boolean>", "HIDE", func(args []string, param any) {
		p := asPlayer(param)
		if !c.isAdmin(p) {
			return
		}
		// 上游: gameLinkServerData.nukes —— 引擎设置
		_ = p.SendSystemMessage(notPortable)
	})

	h.Register("addai", "HIDE", func(_ []string, param any) {
		p := asPlayer(param)
		if !c.isAdmin(p) {
			return
		}
		if global.ConfigServer.EnableAI {
			// 上游: playerManage.addAI() —— 需引擎生成 AI (DECISIONS D-9)
			if err := c.room.PlayerManage.AddAI(); err != nil {
				_ = p.SendSystemMessage(notPortable)
			}
		} else {
			_ = p.SendSystemMessage("请在配置文件启用AI")
		}
	})

	h.RegisterWith("fog", "<type>", "HIDE", func(args []string, param any) {
		p := asPlayer(param)
		if !c.isAdmin(p) {
			return
		}
		_ = p.SendSystemMessage(notPortable) // 上游: gameLinkServerData.fog
	})

	h.RegisterWith("sharedcontrol", "<boolean>", "HIDE", func(args []string, param any) {
		p := asPlayer(param)
		if !c.isAdmin(p) {
			return
		}
		_ = p.SendSystemMessage(notPortable) // 上游: gameLinkServerData.sharedcontrol
	})

	h.RegisterWith("startingunits", "<type>", "HIDE", func(args []string, param any) {
		p := asPlayer(param)
		if !c.isAdmin(p) {
			return
		}
		_ = p.SendSystemMessage(notPortable) // 上游: gameLinkServerData.startingunits
	})

	h.RegisterWith("income", "<income>", "clientCommands.income", func(args []string, param any) {
		p := asPlayer(param)
		if !c.isAdmin(p) {
			return
		}
		if c.room.IsStartGame {
			_ = p.SendSystemMessage(p.GetInput("err.startGame.warn"))
		}
		_ = p.SendSystemMessage(notPortable) // 上游: gameLinkServerData.income
	})

	h.RegisterWith("ai", "<difficuld> [PlayerPositionAI]", "clientCommands.income", func(args []string, param any) {
		p := asPlayer(param)
		if !c.isAdmin(p) {
			return
		}
		if c.room.IsStartGame {
			_ = p.SendSystemMessage(p.GetInput("err.startGame"))
			return
		}
		if !isutils.IsNumeric(args[0]) {
			_ = p.SendSystemMessage(p.GetInput("err.noInt"))
			return
		}
		if len(args) > 1 {
			// 指定玩家: 直接改该玩家的 aiDifficulty (可移植)
			if !isutils.IsNumeric(args[1]) {
				_ = p.SendSystemMessage(p.GetInput("err.noInt.check"))
				return
			}
			playerAi := c.room.PlayerManage.GetPlayer(atoiOr(args[1], 0) - 1)
			if playerAi == nil {
				_ = p.SendSystemMessage(p.GetInput("no.findPlayer"))
			} else {
				playerAi.SetAiDifficulty(atoiOr(args[0], 0))
			}
		} else {
			// 全局: 上游写 gameLinkServerData.aiDifficuld (引擎) 并把现有 AI 的难度改掉.
			// 现有 AI 难度可移植, 引擎默认值不可移植.
			c.room.PlayerManage.PlayerGroup.EachAll(func(it *player.Player) {
				if it.IsAi() {
					it.SetAiDifficulty(atoiOr(args[0], 0))
				}
			})
			_ = p.SendSystemMessage(notPortable)
		}
	})

	h.Register("start", "clientCommands.start", func(_ []string, param any) {
		p := asPlayer(param)
		if p != nil {
			if c.isAdmin(p) {
				if c.room.IsStartGame {
					_ = p.SendSystemMessage(p.GetInput("err.startGame"))
					return
				}
				if global.ConfigServer.StartMinPlayerSize != -1 &&
					global.ConfigServer.StartMinPlayerSize > c.room.PlayerManage.PlayerGroup.Size() {
					_ = p.SendSystemMessage(p.GetInput("start.playerNo", global.ConfigServer.StartMinPlayerSize))
					return
				}
			} else {
				return
			}
		}

		// 上游: 自定义地图先 readMap, 再 multiplayerStart (引擎), 最后 fire 开局事件.
		// Go 版 room.StartGame 承担 readMap + 开局 + 房间侧动作.
		if err := c.room.StartGame(); err != nil {
			if p != nil {
				_ = p.SendSystemMessage(notPortable)
			}
			return
		}
		c.room.EventManage.Fire(&event.ServerGameStartEvent{})
	})

	h.RegisterWith("kick", "<PlayerPosition>", "clientCommands.kick", func(args []string, param any) {
		p := asPlayer(param)
		if c.room.IsStartGame {
			_ = p.SendSystemMessage(p.GetInput("err.startGame"))
			return
		}
		if !c.isAdmin(p) {
			return
		}
		if isutils.NotIsNumeric(args[0]) {
			_ = p.SendSystemMessage(p.GetInput("err.noNumber"))
			return
		}
		site := atoiOr(args[0], 0) - 1
		if kickPlayer := c.room.PlayerManage.GetPlayer(site); kickPlayer != nil {
			kickPlayer.KickPlayer(tr("kick.you"), 60)
		}
	})

	h.RegisterWith("move", "<PlayerPosition> <ToSerialNumber> <Team>", "HIDE", func(args []string, param any) {
		p := asPlayer(param)
		if c.room.IsStartGame {
			_ = p.SendSystemMessage(p.GetInput("err.startGame"))
			return
		}
		if !c.isAdmin(p) {
			return
		}
		if isutils.NotIsNumeric(args[0]) && isutils.NotIsNumeric(args[1]) && isutils.NotIsNumeric(args[2]) {
			_ = p.SendSystemMessage(p.GetInput("err.noNumber"))
			return
		}
		// 上游: netEngine.a(playerTarget, site) + 改队伍 —— 引擎玩家对象操作
		_ = p.SendSystemMessage(notPortable)
	})

	h.RegisterWith("team", "<PlayerPosition> <ToTeamNumber>", "HIDE", func(args []string, param any) {
		p := asPlayer(param)
		if c.room.IsStartGame {
			_ = p.SendSystemMessage(p.GetInput("err.startGame"))
			return
		}
		if !c.isAdmin(p) {
			return
		}
		if isutils.NotIsNumeric(args[0]) && isutils.NotIsNumeric(args[1]) {
			_ = p.SendSystemMessage(p.GetInput("err.noNumber"))
			return
		}
		// 上游: n.k(playerPosition).r = newPosition —— 引擎玩家队伍
		_ = p.SendSystemMessage(notPortable)
	})

	h.RegisterWith("am", "<on/off>", "clientCommands.am", func(args []string, param any) {
		p := asPlayer(param)
		if c.room.IsStartGame {
			_ = p.SendSystemMessage(p.GetInput("err.startGame"))
			return
		}
		if !c.isAdmin(p) {
			return
		}
		c.room.IsMixPlayer = strings.EqualFold(args[0], "on")
		if c.room.IsMixPlayer {
			// 上游: for site in 0..maxPlayer { n.k(site)?.r = site } —— 引擎队伍重置
		}
		_ = p.SendSystemMessage(p.GetInput("server.amTeam", c.room.IsMixPlayer))
	})

	h.Register("battleroom", "clientCommands.battleroom", func(_ []string, param any) {
		p := asPlayer(param)
		if !c.room.IsStartGame {
			_ = p.SendSystemMessage(p.GetInput("err.noStartGame"))
			return
		}
		if !c.isAdmin(p) {
			return
		}
		// 上游: gameLinkFunction.battleRoom() —— 引擎侧; Go 版房间侧等价清理
		c.room.BattleRoom()
	})

	h.Register("pause", "clientCommands.pause", func(_ []string, param any) {
		p := asPlayer(param)
		if !c.room.IsStartGame {
			_ = p.SendSystemMessage(p.GetInput("err.noStartGame"))
			return
		}
		if !c.isAdmin(p) {
			return
		}
		_ = p.SendSystemMessage(notPortable) // 上游: gameLinkFunction.pauseGame(true)
	})

	h.Register("unpause", "clientCommands.unpause", func(_ []string, param any) {
		p := asPlayer(param)
		if !c.room.IsStartGame {
			_ = p.SendSystemMessage(p.GetInput("err.noStartGame"))
			return
		}
		if !c.isAdmin(p) {
			return
		}
		_ = p.SendSystemMessage(notPortable) // 上游: gameLinkFunction.pauseGame(false)
	})
}

// registerGameCommandX 对应 ClientCommands.registerGameCommandX
func (c *clientCommands) registerGameCommandX(h *cmd.Handler) {
	h.Register("help", "clientCommands.help", func(_ []string, param any) {
		p := asPlayer(param)
		if p == nil {
			return
		}
		var str strings.Builder
		for _, command := range h.CommandList() {
			if strings.HasPrefix(command.Description, "#") {
				str.WriteString("   ")
				str.WriteString(command.Text)
				if command.ParamText != "" {
					str.WriteString(" ")
				}
				str.WriteString(command.ParamText)
				str.WriteString(" - ")
				str.WriteString(command.Description[1:])
			} else {
				if command.Description == "HIDE" {
					continue
				}
				str.WriteString("   ")
				str.WriteString(command.Text)
				if command.ParamText != "" {
					str.WriteString(" ")
				}
				str.WriteString(command.ParamText)
				str.WriteString(" - ")
				str.WriteString(p.GetInput(command.Description))
				str.WriteString(global.LineSeparator)
			}
		}
		_ = p.SendSystemMessage(str.String())
	})

	h.RegisterWith("map", "<MapNumber...>", "clientCommands.map", func(args []string, param any) {
		p := asPlayer(param)
		if !c.isAdmin(p) {
			return
		}
		if c.room.IsStartGame {
			_ = p.SendSystemMessage(p.GetInput("err.startGame"))
			return
		}
		// 合并多参数为地图名
		var response strings.Builder
		response.WriteString(args[0])
		for i := 1; i < len(args); i++ {
			response.WriteString(" ")
			response.WriteString(args[i])
		}
		inputMapName := response.String()
		inputMapName = strings.ReplaceAll(inputMapName, "'", "")
		inputMapName = strings.ReplaceAll(inputMapName, " ", "")
		inputMapName = strings.ReplaceAll(inputMapName, "-", "")
		inputMapName = strings.ReplaceAll(inputMapName, "_", "")

		maps := c.room.Maps
		if strings.EqualFold(inputMapName, "DEF") {
			maps.MapName = "Crossing Large (10p)"
			maps.MapType = game.DefaultMap
			// 上游还写 netEngine.az/ay (引擎地图路径), 属引擎侧
		} else if mapPlayer, ok := manage.GameMaps[inputMapName]; ok {
			data := strings.Split(mapPlayer, "@")
			maps.MapName = data[0]
			maps.MapType = game.DefaultMap
		} else {
			if len(manage.MapsData) == 0 {
				_ = p.SendSystemMessage(tr("map.no"))
				return
			}
			if isutils.NotIsNumeric(inputMapName) {
				_ = p.SendSystemMessage(tr("err.noNumber"))
				return
			}
			// 上游按序号取第 N 张自定义地图; Go map 无序, 无法稳定对应同一张
			_ = p.SendSystemMessage(notPortable)
			return
		}
		c.room.Call.SendSystemMessage(tr("map.to", p.Name(), maps.MapName))
	})

	h.RegisterWith("maps", "[page]", "clientCommands.maps", func(_ []string, param any) {
		p := asPlayer(param)
		if c.room.IsStartGame {
			_ = p.SendSystemMessage(tr("err.startGame"))
			return
		}
		if len(manage.MapsData) == 0 {
			return
		}
		_ = p.SendSystemMessage(tr("maps.top"))
		i := 0
		for k := range manage.MapsData {
			_ = p.SendSystemMessage(tr("maps.info", i, k))
			i++
		}
	})

	h.Register("afk", "clientCommands.afk", func(_ []string, param any) {
		p := asPlayer(param)
		if !c.room.IsAfk() {
			_ = p.SendSystemMessage(p.GetInput("ban.comm", "afk"))
			return
		}
		if p.IsAdmin {
			_ = p.SendSystemMessage(p.GetInput("afk.adminNo"))
			return
		}
		if c.room.IsStartGame {
			_ = p.SendSystemMessage(p.GetInput("err.startGame"))
			return
		}
		// AFK 权力移交逻辑依赖 CallTimeTask.PlayerAfkTask 与引擎同步,
		// 核心倒计时可移植, 但上游末尾的 netEngine.L() 属引擎.
		_ = p.SendSystemMessage(notPortable)
	})

	h.Register("status", "clientCommands.status", func(_ []string, param any) {
		p := asPlayer(param)
		if p == nil {
			return
		}
		_ = p.SendSystemMessage(p.GetInput("status.version",
			c.room.PlayerManage.PlayerGroup.Size(), 0, global.ServerCoreVersion, "RW-HPS-Hess"))
	})

	h.RegisterWith("sync", "<on/off>", "clientCommands.sync", func(args []string, param any) {
		p := asPlayer(param)
		if !c.isAdmin(p) {
			return
		}
		c.room.Sync = strings.EqualFold(args[0], "on")
		state := "禁止"
		if c.room.Sync {
			state = "启用"
		}
		_ = p.SendSystemMessage(tr("server.sync", state))
	})

	h.RegisterWith("vote", "<gameover>", "clientCommands.vote", func(_ []string, param any) {
		p := asPlayer(param)
		if p == nil {
			return
		}
		global.Vote = ex.NewVote("gameover", p)
	})

	h.RegisterWith("setmaxplayer", "<number>", "clientCommands.vote", func(args []string, param any) {
		p := asPlayer(param)
		// 上游: n.b(args[0].toInt(), true) —— 引擎最大人数
		_ = p.SendSystemMessage(notPortable)
	})

	h.RegisterWith("iunit", "<PlayerPosition> <unitID>", "clientCommands.iunit", func(args []string, param any) {
		p := asPlayer(param)
		if c.room.IsStartGame {
			_ = p.SendSystemMessage(p.GetInput("err.startGame"))
			return
		}
		if !c.isAdmin(p) {
			return
		}
		if isutils.NotIsNumeric(args[1]) {
			_ = p.SendSystemMessage(p.GetInput("err.noNumber"))
			return
		}
		site := atoiOr(args[0], 0) - 1
		if playerUnit := c.room.PlayerManage.GetPlayer(site); playerUnit != nil {
			playerUnit.SetStartUnit(atoiOr(args[1], 0))
		} else {
			_ = p.SendSystemMessage(p.GetInput("err.player.no.site", args[0]))
		}
	})
}

// registerGameCustomCommand 对应 ClientCommands.registerGameCustomCommand
func (c *clientCommands) registerGameCustomCommand(h *cmd.Handler) {
	h.RegisterWith("summon", "<unitName>", "clientCommands.summon", func(args []string, param any) {
		p := asPlayer(param)
		if !c.room.IsStartGame {
			_ = p.SendSystemMessage(p.GetInput("err.noStartGame"))
			return
		}
		if !p.IsAdmin {
			return
		}
		// 上游: 注册 Ping 回调, 下一次地图 ping 时 gameSummon (引擎指令队列)
		_ = p.SendSystemMessage(notPortable)
	})

	h.RegisterWith("addmoney", "<PlayerPosition> <money>", "clientCommands.addmoney", func(args []string, param any) {
		p := asPlayer(param)
		if !c.room.IsStartGame {
			_ = p.SendSystemMessage(p.GetInput("err.noStartGame"))
			return
		}
		if !c.isAdmin(p) {
			return
		}
		if isutils.NotIsNumeric(args[0]) || isutils.NotIsNumeric(args[1]) {
			_ = p.SendSystemMessage(p.GetInput("err.noNumber"))
			return
		}
		site := atoiOr(args[0], 0) - 1
		if addMoneyPlayer := c.room.PlayerManage.GetPlayer(site); addMoneyPlayer != nil {
			addMoneyPlayer.SetCredits(addMoneyPlayer.Credits() + atoiOr(args[1], 0))
		}
	})

	h.Register("nuke", "clientCommands.nuke", func(_ []string, param any) {
		p := asPlayer(param)
		if !c.room.IsStartGame {
			_ = p.SendSystemMessage(p.GetInput("err.noStartGame"))
			return
		}
		if !p.IsAdmin {
			return
		}
		// 上游: 反射造核弹单位 —— 纯引擎操作
		_ = p.SendSystemMessage(notPortable)
	})

	h.Register("clone", "clientCommands.clone", func(_ []string, param any) {
		p := asPlayer(param)
		if !c.room.IsStartGame {
			_ = p.SendSystemMessage(p.GetInput("err.noStartGame"))
			return
		}
		if !p.IsAdmin {
			return
		}
		// 上游: 反射克隆单位 —— 纯引擎操作
		_ = p.SendSystemMessage(notPortable)
	})

	h.Register("myid", "clientCommands.-", func(_ []string, param any) {
		p := asPlayer(param)
		if p == nil {
			return
		}
		// 上游: BigInteger(decodeHex(connectHexID)).toInt() 取绝对值
		id := int64(0)
		if n, ok := new(big.Int).SetString(p.ConnectHexID(), 16); ok {
			id = n.Abs(n).Int64()
		}
		_ = p.SendSystemMessage(p.GetInput("myid", id))
	})

	// 有意修正（作者已批准，2026-11-04）: 上游 client ban 在 findPlayer 找到目标后,
	// fire 的却是发起者 player (ClientCommands.kt:603-611, 上游笔误, 实际会封禁执行
	// 命令的管理员自己). 按作者决定改为对被 ban 的目标玩家触发 (DECISIONS D-10).
	h.RegisterWith("ban", "<Name/Position>", "serverCommands.ban", func(args []string, param any) {
		p := asPlayer(param)
		if !c.isAdmin(p) {
			return
		}
		if conTg := c.findPlayer(p, args[0]); conTg != nil {
			c.room.EventManage.Fire(&event.PlayerBanEvent{Player: conTg})
			_ = p.SendSystemMessage("Ban : " + args[0] + " OK")
		}
	})

	h.RegisterWith("kickx", "<Name/Position>", "#Kick Player", func(args []string, param any) {
		p := asPlayer(param)
		if !c.isAdmin(p) {
			return
		}
		if conTg := c.findPlayer(p, args[0]); conTg != nil {
			conTg.KickPlayer("你被踢出服务器", 120)
			_ = p.SendSystemMessage("Kick : " + args[0] + " OK")
		}
	})

	h.RegisterWith("allmute", "<on/off>", "#Mute All", func(args []string, param any) {
		p := asPlayer(param)
		if !c.isAdmin(p) {
			return
		}
		c.room.MuteAll = strings.EqualFold(args[0], "on")
		state := "禁用"
		if c.room.MuteAll {
			state = "启用"
		}
		_ = p.SendSystemMessage("全体禁言 : " + state + " OK")
	})
}
