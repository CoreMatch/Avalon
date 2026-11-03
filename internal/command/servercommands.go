package command

import (
	"strings"

	"rwhps/internal/data/global"
	"rwhps/internal/game/event"
	"rwhps/internal/game/manage"
	"rwhps/internal/game/player"
	"rwhps/internal/util/isutils"
	"rwhps/internal/util/timeutil"

	cmd "rwhps/internal/util/game/command"
)

// notPortable 对应"依赖游戏内核 (引擎) 且当前 helper 协议无对应操作"的命令的统一回复.
//
// 上游这些命令直接操作 rusted-warfare.jar 内的引擎对象 (gameLinkFunction /
// gameLinkServerData / netEngine 等), Go 版内核在外部 helper 内, 协议尚未定义
// 这些操作 (DECISIONS D-9), 因此如实报告不可用, 不伪造成功.
const notPortable = "This command needs the game engine (helper protocol), which is not available yet"

// registerServerCommands 对应 ServerCommands(handler): 服务端 (控制台) 命令.
//
// 上游 ServerCommands 挂在游戏模块的 CommandHandler 上; Go 版收敛进
// global.ServerCommand. 全部命令以 d.Session (单例房间) 为操作对象.
func registerServerCommands(h *cmd.Handler, d *Deps) {
	registerServerPlayerCommand(h, d)
	registerServerPlayerStatusCommand(h, d)
	registerServerPlayerCustomEx(h, d)
}

// registerServerPlayerCommand 对应 ServerCommands.registerPlayerCommand
func registerServerPlayerCommand(h *cmd.Handler, d *Deps) {
	h.RegisterWith("say", "<text...>", "serverCommands.say", func(arg []string, _ any) {
		msg := strings.ReplaceAll(arg[0], "<>", "")
		if d.Session != nil {
			d.Session.Call.SendSystemMessage(msg)
		}
		d.printf("All players has received the message : {0}", msg)
	})

	h.RegisterWith("whisper", "<PlayerPositionNoAI> <text...>", "serverCommands.whisper", func(arg []string, _ any) {
		if p := playerPosition(d.Session, arg[0], d.print); p != nil {
			msg := strings.ReplaceAll(arg[1], "<>", "")
			_ = p.SendSystemMessage(msg)
			d.printf("{0} has received the message : {1}", p.Name(), msg)
		}
	})

	h.Register("gametime", "serverCommands.gametime", func(_ []string, _ any) {
		if d.Session == nil {
			return
		}
		if d.Session.IsStartGame {
			// 上游: Time.format(Time.getTimeSinceSecond(room.startTime) * 1000L, 6)
			d.printf("Gameing Time : {0}", timeutil.Format(int64(timeutil.GetTimeSinceSecond(int(d.Session.GetStartTime()))*1000), 6))
		} else {
			d.print("No Start Game")
		}
	})

	h.Register("gameover", "serverCommands.gameover", func(_ []string, _ any) {
		if d.Session != nil {
			d.Session.Gr()
		}
	})

	h.Register("save", "serverCommands.save", func(_ []string, _ any) {
		if d.Session == nil {
			return
		}
		if d.Session.IsStartGame {
			// 上游: gameModule.gameLinkFunction.saveGame() —— 引擎侧存档
			d.print(notPortable)
		} else {
			d.print("No Start Game")
		}
	})

	h.RegisterWith("admin", "<add/remove> <PlayerPositionNoAI>", "serverCommands.admin", func(arg []string, _ any) {
		if d.Session == nil {
			return
		}
		if d.Session.IsStartGame {
			d.print(tr("err.startGame"))
			return
		}
		if !(arg[0] == "add" || arg[0] == "remove") {
			d.print("Second parameter must be either 'add' or 'remove'.")
			return
		}
		add := arg[0] == "add"
		if p := playerPosition(d.Session, arg[1], d.print); p != nil {
			if global.Core != nil && global.Core.Admin != nil {
				if add {
					global.Core.Admin.AddAdmin(p.ConnectHexID(), p.Name(), true)
				} else {
					global.Core.Admin.RemoveAdmin(p.ConnectHexID())
				}
			}
			p.IsAdmin = add
			p.SuperAdmin = true
			// 上游随后 player.con!!.sendServerInfo(false) 刷新客户端管理员界面;
			// 该包属引擎握手, Go 版 GameVersionServer 未实现 (见接口延后清单), 跳过.
			d.printf("Changed admin status of player: {0}", p.Name())
		}
	})

	h.Register("clearbanall", "serverCommands.clearbanall", func(_ []string, _ any) {
		if global.Core != nil && global.Core.Admin != nil {
			global.Core.Admin.ClearBanAll()
		}
	})

	h.RegisterWith("ban", "<PlayerPositionNoAI>", "serverCommands.ban", func(arg []string, _ any) {
		if d.Session == nil {
			return
		}
		if p := playerPosition(d.Session, arg[0], d.print); p != nil {
			d.Session.EventManage.Fire(&event.PlayerBanEvent{Player: p})
		}
	})

	h.Register("mapchange", "#Change Maps", func(_ []string, _ any) {
		if d.Session == nil {
			return
		}
		// 上游取 mapsData 第一个自定义地图. Go 的 map 无序, 上游 ObjectMap 有序,
		// 这里不保证与上游同一张 (无稳定首元素), 仅在有地图时落到房间.
		if len(manage.MapsData) == 0 {
			d.print("No custom maps loaded")
			return
		}
		var nameNoPx string
		for k := range manage.MapsData {
			nameNoPx = k
			break
		}
		data := manage.MapsData[nameNoPx]
		maps := d.Session.Maps
		maps.MapData = data
		maps.MapType = data.MapType
		maps.MapName = nameNoPx
		maps.MapPlayer = ""
		// 上游随后写 netEngine.az / netEngine.ay 并 netEngine.L() 同步引擎, 属引擎侧.
		d.Session.Call.SendSystemMessage(tr("map.to", "Admin", maps.MapName))
	})

	// 上游 unban 与 ban 一样 fire PlayerBanEvent (上游原样如此, 疑似笔误); 照搬.
	// 若后续要真正解禁, 应改为 PlayerUnBanEvent 并补默认监听 (见 DECISIONS).
	h.RegisterWith("unban", "<PlayerPositionNoAI>", "serverCommands.ban", func(arg []string, _ any) {
		if d.Session == nil {
			return
		}
		if p := playerPosition(d.Session, arg[0], d.print); p != nil {
			d.Session.EventManage.Fire(&event.PlayerBanEvent{Player: p})
		}
	})

	h.RegisterWith("mute", "<PlayerPositionNoAI> [Time(s)]", "serverCommands.mute", func(arg []string, _ any) {
		if p := playerPosition(d.Session, arg[0], d.print); p != nil {
			// 上游固定 43200s (未使用可选的 Time 参数), 照搬
			p.MuteTime = timeutil.GetTimeFutureMillis(43200 * 1000)
		}
	})

	h.RegisterWith("kick", "<PlayerPosition> [time]", "serverCommands.kick", func(arg []string, _ any) {
		if p := playerPosition(d.Session, arg[0], d.print); p != nil {
			seconds := int64(60)
			if len(arg) > 1 {
				seconds = int64(atoiOr(arg[1], 60))
			}
			p.KickTime = timeutil.GetTimeFutureMillis(seconds * 1000)
			// 上游随后 kickPlayer(kick.you) 会再发 KICK+DISCONNECT 两连包
			p.KickPlayer(tr("kick.you"), 0)
		}
	})

	h.RegisterWith("kill", "<PlayerPosition>", "serverCommands.kill", func(arg []string, _ any) {
		if d.Session == nil {
			return
		}
		if d.Session.IsStartGame {
			// 上游: player.con!!.sendSurrender() —— 引擎侧投降包.
			// 保留 playerPosition 的查找与报错 (边界判断照搬), 投降动作需引擎.
			if playerPosition(d.Session, arg[0], d.print) != nil {
				d.print(notPortable)
			}
		} else {
			d.print(tr("err.noStartGame"))
		}
	})

	h.RegisterWith("giveadmin", "<PlayerPositionNoAI>", "serverCommands.giveadmin", func(arg []string, _ any) {
		if d.Session == nil {
			return
		}
		pm := d.Session.PlayerManage
		pm.PlayerGroup.EachAll(func(i *player.Player) {
			if !i.IsAdmin {
				return
			}
			target := pm.GetPlayer(atoiOr(arg[0], -1))
			if target != nil {
				i.IsAdmin = false
				target.IsAdmin = true
				d.Session.Call.SendSystemMessageLocal("give.ok", target.Name())
			}
		})
	})

	h.Register("clearmuteall", "serverCommands.clearmuteall", func(_ []string, _ any) {
		if d.Session == nil {
			return
		}
		d.Session.PlayerManage.PlayerGroup.EachAll(func(p *player.Player) {
			p.MuteTime = 0
		})
	})

	h.RegisterWith("team", "<PlayerPosition> <Team>", "serverCommands.team", func(arg []string, _ any) {
		if d.Session == nil {
			return
		}
		if d.Session.IsStartGame {
			d.print(tr("err.startGame"))
			return
		}
		if isutils.NotIsNumeric(arg[0]) && isutils.NotIsNumeric(arg[1]) {
			d.print(tr("err.noNumber"))
			return
		}
		// 上游: n.k(playerPosition).r = newPosition —— 直接改引擎玩家对象的队伍,
		// 属引擎状态, Go 版无协议通道.
		d.print(notPortable)
	})
}

// registerServerPlayerStatusCommand 对应 ServerCommands.registerPlayerStatusCommand
func registerServerPlayerStatusCommand(h *cmd.Handler, d *Deps) {
	h.Register("players", "serverCommands.players", func(_ []string, _ any) {
		if d.Session == nil {
			return
		}
		group := d.Session.PlayerManage.PlayerGroup
		if group.Size() == 0 {
			d.print("No players are currently in the server.")
			return
		}
		d.printf("Players: {0}", group.Size())
		var data strings.Builder
		group.EachAll(func(p *player.Player) {
			data.WriteString(global.LineSeparator)
			data.WriteString(p.PlayerInfo())
		})
		d.print(data.String())
	})

	h.Register("admins", "serverCommands.admins", func(_ []string, _ any) {
		if global.Core == nil || global.Core.Admin == nil {
			return
		}
		admins := global.Core.Admin.Admins()
		if len(admins) == 0 {
			d.print("No admins are currently in the server.")
			return
		}
		d.printf("Admins: {0}", len(admins))
		var data strings.Builder
		for _, p := range admins {
			data.WriteString(global.LineSeparator)
			data.WriteString(p.Name)
			data.WriteString(" / ")
			data.WriteString("ID: ")
			data.WriteString(p.UUID)
			data.WriteString(" / ")
			data.WriteString("Admin: ")
			data.WriteString(boolStr(p.Admin))
			data.WriteString(" / ")
			data.WriteString("SuperAdmin: ")
			data.WriteString(boolStr(p.SuperAdmin))
		}
		d.print(data.String())
	})

	h.Register("reloadmods", "serverCommands.reloadmods", func(_ []string, _ any) {
		if d.Session != nil && d.Session.IsStartGame {
			d.print(tr("err.startGame"))
			return
		}
		// 上游: ModManage.reLoadMods() —— JVM 模组加载, Go 无对应
		d.print(notPortable)
	})

	h.Register("reloadmaps", "serverCommands.reloadmaps", func(_ []string, _ any) {
		size := len(manage.MapsData)
		for k := range manage.MapsData {
			delete(manage.MapsData, k)
		}
		manage.ReadMapAndSave()
		d.printf("Reload Old Size:New Size is {0}:{1}", size, len(manage.MapsData))
	})

	h.Register("mods", "serverCommands.mods", func(_ []string, _ any) {
		// 上游: ModManage.getModsList() —— JVM 模组, Go 无对应
		d.print(notPortable)
	})

	h.Register("maps", "serverCommands.maps", func(_ []string, _ any) {
		var response strings.Builder
		i := 0
		for k := range manage.MapsData {
			response.WriteString(tr("maps.info", i, k))
			response.WriteString(global.LineSeparator)
			i++
		}
		d.print(response.String())
	})
}

// registerServerPlayerCustomEx 对应 ServerCommands.registerPlayerCustomEx
func registerServerPlayerCustomEx(h *cmd.Handler, d *Deps) {
	h.RegisterWith("addmoney", "<PlayerPosition> <money>", "serverCommands.addmoney", func(arg []string, _ any) {
		if d.Session == nil {
			return
		}
		if !d.Session.IsStartGame {
			d.print(tr("err.noStartGame"))
			return
		}
		site := atoiOr(arg[0], 0) - 1
		if p := d.Session.PlayerManage.GetPlayer(site); p != nil {
			p.SetCredits(p.Credits() + atoiOr(arg[1], 0))
		}
		// 上游: gameModule.gameLinkFunction.allPlayerSync() —— 引擎全量同步 (D-9)
	})

	h.RegisterWith("textbuild", "<UnitName> <Text> [index(NeutralByDefault)]", "serverCommands.textbuild", func(_ []string, _ any) {
		// 上游: 把文本按 Font16 点阵展开成多个 gameSummonPacket 塞进引擎指令队列,
		// 完全依赖引擎, Go 无对应.
		d.print(notPortable)
	})

	h.Register("test", "serverCommands.addmoney", func(_ []string, _ any) {
		// 上游: 引擎截图 (GameEngine.mainObject.j!!.a), Go 无对应
		d.print(notPortable)
	})

	h.Register("aaa", "serverCommands.clearbanall", func(_ []string, _ any) {
		// 上游: 用一个伪造的 Cherry 玩家执行 "/am on" (内部调试). Go 版以 nil 玩家
		// 触发同款客户端命令路径.
		if d.Session != nil {
			d.Session.ClientHandler.HandleMessageWith("/am on", nil)
		}
	})
}

// boolStr 对应 Kotlin Boolean 的 toString ("true"/"false")
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
