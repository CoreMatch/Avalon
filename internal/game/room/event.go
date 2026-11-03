package room

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"rwhps/internal/core/thread"
	"rwhps/internal/data/global"
	"rwhps/internal/game/event"
	"rwhps/internal/game/player"
	"rwhps/internal/util/file"
	"rwhps/internal/util/log"
	"rwhps/internal/util/timeutil"
)

// tr 对应 Data.i18NBundle.getinput (房间层取词)
func tr(key string, params ...any) string {
	if global.I18NBundle == nil {
		return key
	}
	return global.I18NBundle.GetInput(key, params...)
}

// registerDefaultEvents 对应上游 Event() 注册进 eventManage 的全部默认监听.
//
// 上游注册点: EventGlobal.registerServerHessLoadEvent (游戏模块加载时
// eventManage.registerListener(Event())); Go 版在房间构造时完成同一动作.
//
// 上游监听器经 event.gameModule.room 取房间; Go 版监听器是闭包, 直接捕获 r.
func (r *ServerRoom) registerDefaultEvents() {
	em := r.EventManage

	// 上游 registerServerHessStartPort: maxUnit/income 写入 gameLinkServerData.
	// 这两项是引擎侧开局配置, 位于 helper 内, 当前协议无对应操作
	// (DECISIONS D-9), 不注册空监听; 事件触发点保留在 start 命令里.

	// --- registerPlayerJoinEvent ---
	event.Register(em, func(ev *event.PlayerJoinEvent) {
		p := ev.Player

		// 上游: name.isBlank() || name.length > 30 (Kotlin length = UTF-16 单元,
		// 对 BMP 字符与 rune 一致)
		if strings.TrimSpace(p.Name()) == "" || utf8.RuneCountInString(p.Name()) > 30 {
			p.KickPlayer(p.GetInput("kick.name.failed"), 0)
			return
		}

		if global.Core != nil && global.Core.Admin.IsUUIDBanned(p.ConnectHexID()) {
			p.KickPlayer(p.GetInput("kick.ban"), 0)
			return
		}

		if global.Core != nil {
			if info, ok := global.Core.Admin.PlayerDataCache[p.ConnectHexID()]; ok {
				if info.TimesKicked > timeutil.ConcurrentMillis() {
					p.KickPlayer(p.GetInput("kick.you.time"), 0)
					return
				}
				p.MuteTime = info.TimeMute
			}
		}

		r.Call.SendSystemMessage(tr("player.ent", p.Name()))
		log.Clog("&c" + tr("player.ent", p.Name()))

		// 自动开局倒计时: 人数达标且没有挂着的 AutoStartTask 时启动
		cfg := global.ConfigServer
		if cfg.AutoStartMinPlayerSize != -1 &&
			r.PlayerManage.PlayerGroup.Size() >= cfg.AutoStartMinPlayerSize &&
			!thread.ContainsTimeTask(thread.AutoStartTask) {
			flagCount := 10
			thread.NewTimedTask(thread.AutoStartTask, 0, 1*time.Second, func() {
				if r.IsStartGame {
					thread.CloseTimeTask(thread.AutoStartTask, nil)
					return
				}

				flagCount--

				if flagCount > 0 {
					r.Call.SendSystemMessage(tr("auto.start", flagCount))
					return
				}

				thread.CloseTimeTask(thread.AutoStartTask, nil)
				thread.CloseTimeTask(thread.PlayerAfkTask, nil)

				r.ClientHandler.HandleMessageWith("/start", nil)
			})
		}

		if strings.TrimSpace(cfg.EnterAd) != "" {
			_ = p.SendSystemMessage(cfg.EnterAd)
		}

		if cfg.SaveRePlayFile {
			info := p.InfoObject()
			if b, err := json.MarshalIndent(info, "", "  "); err == nil {
				file.GetFolder(global.ServerPlayerPath).
					ToFile(fmt.Sprintf("%v.json", info["Position"])).WriteFileBytes(b)
			}
		}
	})

	// --- registerPlayerLeaveEvent ---
	event.Register(em, func(ev *event.PlayerLeaveEvent) {
		p := ev.Player
		cfg := global.ConfigServer

		// oneAdmin: 离开的正是自动管理员时, 移交给第一个非管理员玩家
		// (上游触发时玩家已移出 playerGroup, 遍历的是剩余玩家, Go 侧同序)
		if cfg.OneAdmin && p.IsAdmin && p.AutoAdmin && r.PlayerManage.PlayerGroup.Size() > 0 {
			if np, ok := r.PlayerManage.PlayerGroup.Find(func(x *player.Player) bool {
				return !x.IsAdmin && !x.IsAi() && !global.NeverEnd
			}); ok {
				np.IsAdmin = true
				np.AutoAdmin = true
				p.IsAdmin = false
				p.AutoAdmin = false
				r.Call.SendSystemMessageLocal("give.ok", np.Name())
			}
		}

		if global.Core != nil {
			global.Core.Admin.PlayerDataCache[p.ConnectHexID()] =
				global.NewPlayerInfo(p.ConnectHexID(), p.KickTime, p.MuteTime)
		}

		if r.IsStartGame {
			r.Call.SendSystemMessageLocal("player.dis", p.Name())
		} else {
			r.Call.SendSystemMessageLocal("player.disNoStart", p.Name())
		}
		log.Clog("&c" + tr("player.dis", p.Name()))

		if cfg.AutoStartMinPlayerSize != -1 &&
			r.PlayerManage.PlayerGroup.Size() <= cfg.AutoStartMinPlayerSize &&
			thread.ContainsTimeTask(thread.AutoStartTask) {
			thread.CloseTimeTask(thread.AutoStartTask, nil)
		}

		if cfg.SaveRePlayFile {
			file.GetFolder(global.ServerPlayerPath).
				ToFile(strconv.Itoa(p.Position()) + ".json").Delete()
		}
	})

	// --- registerGameStartEvent ---
	event.Register(em, func(*event.ServerGameStartEvent) {
		if global.Core != nil {
			global.Core.Admin.PlayerDataCache = map[string]*global.PlayerInfo{}
		}

		if strings.TrimSpace(global.ConfigServer.StartAd) != "" {
			r.Call.SendSystemMessage(global.ConfigServer.StartAd)
		}

		if global.ConfigServer.SaveRePlayFile {
			data := make([]map[string]any, 0)
			r.PlayerManage.PlayerAll.EachAll(func(p *player.Player) {
				if !p.IsAi() {
					data = append(data, p.InfoObject())
				}
			})
			r.PlayerData = data
		}

		log.Clog("[Start New Game]")
	})

	// --- registerGameOverEvent ---
	event.Register(em, func(*event.ServerGameOverEvent) {
		// 上游: playerData!! 逐个加进 zip 流, 写入 {replayFileName}.zip, 然后 System.gc().
		// playerData 只在 saveRePlayFile 且开局事件里被赋值; 上游为 null 时在此 NPE
		// (被事件总线的异常捕获记日志), Go 版等价为跳过.
		if r.PlayerData == nil {
			return
		}

		buf := &bytes.Buffer{}
		zw := zip.NewWriter(buf)
		for _, info := range r.PlayerData {
			w, err := zw.Create(fmt.Sprintf("%v.json", info["Position"]))
			if err != nil {
				continue
			}
			if b, err := json.MarshalIndent(info, "", "  "); err == nil {
				_, _ = w.Write(b)
			}
		}
		if err := zw.Close(); err != nil {
			log.Errorf("[Room]", "zip replay: {0}", err)
			return
		}

		base := filepath.Base(r.ReplayFileName)
		name := strings.TrimSuffix(base, filepath.Ext(base)) + ".zip"
		file.GetFolder(global.ServerPlayerPath).ToFile(name).WriteFileBytes(buf.Bytes())
	})

	// --- registerPlayerBanEvent ---
	event.Register(em, func(ev *event.PlayerBanEvent) {
		p := ev.Player
		if global.Core != nil {
			global.Core.Admin.BanUUID(p.ConnectHexID())
			if con := p.Con(); con != nil {
				global.Core.Admin.BanIP(con.IP())
			}
		}
		p.KickPlayer(p.GetInput("kick.ban"), 0)
		r.Call.SendSystemMessageLocal("ban.yes", p.Name())
	})

	// --- registerPlayerIpBanEvent ---
	event.Register(em, func(ev *event.PlayerIpBanEvent) {
		p := ev.Player
		if global.Core != nil {
			if con := p.Con(); con != nil {
				global.Core.Admin.BanIP(con.IP())
			}
		}
		p.KickPlayer(p.GetInput("kick.ban"), 0)
		r.Call.SendSystemMessageLocal("ban.yes", p.Name())
	})
}
