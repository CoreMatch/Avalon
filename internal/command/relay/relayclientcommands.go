package relay

import (
	"math"
	"math/big"
	"strconv"
	"strings"

	"rwhps/internal/data/global"
	"rwhps/internal/game/player"
	"rwhps/internal/game/room"
	"rwhps/internal/net"
	"rwhps/internal/net/netconnectprotocol/realize"
	"rwhps/internal/util/algorithms"
	cmd "rwhps/internal/util/game/command"
	"rwhps/internal/util/isutils"
	"rwhps/internal/util/timeutil"
)

// RegisterRelayClientCommands 对应 RelayClientCommands(handler)
//
// 命令回调的第二参 (param) 即发起命令的连接 (*realize.GameVersionRelay),
// 由 GameVersionRelay.receiveChat / TypeRelay.hostProcessing 经
// RelayCommand.HandleMessageWith(message, con) 传入.
//
// 与上游一致: 本函数每次调用都向 handler 追加注册 (重复启动 Relay 时
// orderedCommands 会出现重复项, 上游同样如此).
func RegisterRelayClientCommands(handler *cmd.Handler) {
	handler.Register("help", "clientCommands.help", func(_ []string, param any) {
		con := asRelay(param)
		if con == nil {
			return
		}
		var str strings.Builder
		for _, command := range handler.CommandList() {
			paramText := ""
			if command.ParamText != "" {
				paramText = " " + command.ParamText
			}
			if strings.HasPrefix(command.Description, "#") {
				str.WriteString("   " + command.Text + paramText + " - " + command.Description[1:] + global.LineSeparator)
			} else {
				if command.Description == "HIDE" {
					continue
				}
				str.WriteString("   " + command.Text + paramText + " - " + i18n(command.Description) + global.LineSeparator)
			}
		}
		sendMsg(con, str.String())
	})

	handler.Register("myid", "clientCommands.-", func(_ []string, param any) {
		con := asRelay(param)
		if con == nil {
			return
		}
		// 上游: BigInteger(HexUtils.decodeHex(con.registerPlayerId!!)).toInt()
		//       .let { if (it < 0) -it else it }.toString()
		b, err := algorithms.DecodeHex(con.RegisterPlayerId())
		if err != nil || len(b) == 0 {
			return
		}
		// BigInteger(byte[]) 是有符号补码构造; toInt() 取低 32 位
		bi := new(big.Int).SetBytes(b)
		if b[0]&0x80 != 0 {
			mod := new(big.Int).Lsh(big.NewInt(1), uint(8*len(b)))
			bi.Sub(bi, mod)
		}
		v := int32(bi.Int64())
		if v < 0 {
			v = -v
		}
		sendMsg(con, strconv.FormatInt(int64(v), 10))
	})

	handler.RegisterWith("sync", "<on/off>", "#同步", func(args []string, param any) {
		con := asRelay(param)
		if con == nil {
			return
		}
		if isAdmin(con, false) {
			con.Room().SyncFlag = args[0] == "on"
			state := "启用"
			if !con.Room().SyncFlag {
				state = "禁止"
			}
			sendMsg(con, i18n("server.sync", state))
		}
	})

	handler.RegisterWith("rp", "<Name/Position>", "#取代", func(args []string, param any) {
		con := asRelay(param)
		if con == nil {
			return
		}
		if isAdmin(con, true) {
			conTg := findPlayerUUIDAll(con, args[0])
			if conTg[0] != "" {
				flag := false
				con.Room().AbstractNetConnectIntMap.EachAll(func(_ int32, u room.RelayConnect) {
					if conTg[1] == u.RegisterPlayerId() {
						flag = true
						sendMsg(con, "玩家 "+conTg[1]+", 还在线, 不能取代")
					}
				})

				if flag {
					return
				}

				con.Room().ReplacePlayerHex = conTg[0]
				sendMsg(con, "准备取代玩家 "+conTg[1])
			}
		}
	})

	handler.RegisterWith("kickx", "<Name/Position>", "#Kick Player", func(args []string, param any) {
		con := asRelay(param)
		if con == nil {
			return
		}
		if isAdmin(con, true) {
			if con.Room().IsStartGame() && con.Room().StartGameTime()+600 < timeutil.ConcurrentSecond() {
				sendMsg(con, "已经开局十分钟了 不能再BAN")
				return
			}
			if conTg := findPlayer(con, args[0]); conTg != nil {
				// 上游 playerRelay!!, 房间内的连接均已绑定 PlayerRelay
				con.Room().RelayKickData.Put("KICK"+conTg.PlayerRelay().UUID, timeutil.ConcurrentSecond()+60)

				conTg.Kick("你被踢出服务器")
				sendMsg(con, "Kick : "+args[0]+" OK")
			}
		}
	})

	handler.RegisterWith("ban", "<Name/Position>", "#Ban Player", func(args []string, param any) {
		con := asRelay(param)
		if con == nil {
			return
		}
		if isAdmin(con, true) {
			if conTg := findPlayer(con, args[0]); conTg != nil {
				con.Room().RelayKickData.Put("KICK"+conTg.PlayerRelay().UUID, math.MaxInt32)
				con.Room().RelayKickData.Put("BAN"+conTg.IP(), math.MaxInt32)
				conTg.Kick("你被服务器 BAN")
				sendMsg(con, "BAN : "+args[0]+" OK")
			}
		}
	})

	handler.Register("allmute", "#All Player mute", func(_ []string, param any) {
		con := asRelay(param)
		if con == nil {
			return
		}
		if isAdmin(con, true) {
			con.Room().AllMute = !con.Room().AllMute
			state := "关闭"
			if con.Room().AllMute {
				state = "开启"
			}
			sendMsg(con, "全局禁言状态是 :  "+state)
		}
	})

	// 上游此处还有 PluginManage.runRegisterRelayClientCommands(handler), 插件系统未移植
}

// asRelay 把命令回调的 param 还原为 Relay 连接; 非连接调用返回 nil
func asRelay(param any) *realize.GameVersionRelay {
	con, _ := param.(*realize.GameVersionRelay)
	return con
}

// isAdmin 对应 private isAdmin(con, sendMsg = true)
func isAdmin(con *realize.GameVersionRelay, sendMsgFlag bool) bool {
	if con.Room() == nil || con.Room().Admin() == nil {
		return false
	}
	admin := con.Room().Admin()
	if admin.Name() == con.Name() && admin.RegisterPlayerId() == con.RegisterPlayerId() {
		return true
	}
	if sendMsgFlag {
		sendMsg(con, i18n("err.noAdmin"))
	}
	return false
}

// sendMsg 对应 private sendMsg(con, msg)
//
// 上游: con.sendPacket(NetStaticData.RwHps.abstractNetPacket.getChatMessagePacket(msg, "RELAY-CN", 5))
func sendMsg(con *realize.GameVersionRelay, msg string) {
	rw := net.NetStaticData.RwHps()
	if rw == nil || rw.AbstractNetPacket() == nil {
		return
	}
	p, err := rw.AbstractNetPacket().GetChatMessagePacket(msg, "RELAY-CN", 5)
	if err != nil {
		return
	}
	con.SendPacket(p)
}

// findPlayer 对应 private findPlayer(con, findIn): 按名字包含或位置查在线连接
//
// 上游两处 `return@let` 只退出 let 块, 函数继续执行并最终返回 conTg ——
// 即"目标不止一个"时仍返回最后一个命中. Go 照搬该行为.
func findPlayer(con *realize.GameVersionRelay, findIn string) *realize.GameVersionRelay {
	var conTg *realize.GameVersionRelay

	var findNameIn string
	hasPosition := false
	findPositionIn := 0

	if isutils.IsNumeric(findIn) {
		findPositionIn = atoiOr(findIn) - 1
		hasPosition = true
	} else {
		findNameIn = findIn
	}

	if findNameIn != "" {
		count := 0
		con.Room().AbstractNetConnectIntMap.EachAll(func(_ int32, it room.RelayConnect) {
			// 上游 playerRelay!!.name.contains(findName, ignoreCase = true)
			if strings.Contains(strings.ToLower(it.PlayerRelay().Name), strings.ToLower(findNameIn)) {
				if gc, ok := it.(*realize.GameVersionRelay); ok {
					conTg = gc
				}
				count++
			}
		})
		if count > 1 {
			sendMsg(con, "目标不止一个, 请不要输入太短的玩家名")
		} else if conTg == nil {
			sendMsg(con, "找不到玩家")
		}
	}

	if hasPosition {
		con.Room().AbstractNetConnectIntMap.EachAll(func(_ int32, it room.RelayConnect) {
			// 上游 playerRelay?.site == findPosition (安全调用, nil 不命中)
			if pr := it.PlayerRelay(); pr != nil && pr.Site == findPositionIn {
				if gc, ok := it.(*realize.GameVersionRelay); ok {
					conTg = gc
				}
			}
		})
		if conTg == nil {
			sendMsg(con, "找不到玩家")
		}
	}

	return conTg
}

// findPlayerUUIDAll 对应 private findPlayerUUIDAll(con, findIn):
// 在开局后的玩家数据里查 [UUID-Hex, 连接名]
//
// 与 findPlayer 同样照搬 `return@let` 语义: 警告后函数继续返回当前结果.
func findPlayerUUIDAll(con *realize.GameVersionRelay, findIn string) [2]string {
	uuidHexTg := [2]string{"", ""}

	if !con.Room().IsStartGame() {
		sendMsg(con, "房间未开始游戏")
		return uuidHexTg
	}

	var findNameIn string
	hasPosition := false
	findPositionIn := 0

	if isutils.IsNumeric(findIn) {
		findPositionIn = atoiOr(findIn) - 1
		hasPosition = true
	} else {
		findNameIn = findIn
	}

	if findNameIn != "" {
		count := 0
		con.Room().RelayPlayersData.EachAll(func(_ string, it *player.PlayerRelay) {
			if strings.Contains(strings.ToLower(it.Name), strings.ToLower(findNameIn)) {
				// 上游 it.con.registerPlayerId!! / it.con.name
				conOf := it.Con.(*realize.GameVersionRelay)
				uuidHexTg[0] = conOf.RegisterPlayerId()
				uuidHexTg[1] = conOf.Name()
				count++
			}
		})
		if count > 1 {
			sendMsg(con, "目标不止一个, 请不要输入太短的玩家名")
		} else if uuidHexTg[0] == "" {
			sendMsg(con, "找不到玩家")
		}
	}

	if hasPosition {
		con.Room().RelayPlayersData.EachAll(func(_ string, it *player.PlayerRelay) {
			if it.Site == findPositionIn {
				conOf := it.Con.(*realize.GameVersionRelay)
				uuidHexTg[0] = conOf.RegisterPlayerId()
				uuidHexTg[1] = conOf.Name()
			}
		})
		if uuidHexTg[0] == "" {
			sendMsg(con, "找不到玩家")
		}
	}

	return uuidHexTg
}

// i18n 对应 Data.i18NBundle.getinput
func i18n(key string, params ...any) string {
	if global.I18NBundle == nil {
		return key
	}
	return global.I18NBundle.GetInput(key, params...)
}

// atoiOr 对应 findIn.toInt(), 解析失败返回 0 (上游会抛异常, 但入参已被 isNumeric 校验)
func atoiOr(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return n
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}
