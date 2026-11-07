package plugin

import (
	"sync"
	"time"

	"github.com/CoreMatch/Avalon/internal/game/event"
	"github.com/CoreMatch/Avalon/internal/game/player"
	"github.com/CoreMatch/Avalon/internal/game/room"
	"github.com/CoreMatch/Avalon/internal/util/jsonutil"
	"github.com/CoreMatch/Avalon/internal/util/log"
	"github.com/CoreMatch/Avalon/internal/util/timeutil"
)

// 全局槽位对应 room.SetDefault / game.SetBackend 的既有模式:
// 启动链装配, 各消费方 (网关 / relay / 命令层) 经包级函数取用.

var (
	defaultHubMu sync.RWMutex
	defaultHub   *Hub
)

// SetDefault 由启动链在 Load 成功后调用; 传 nil 清除
func SetDefault(h *Hub) {
	defaultHubMu.Lock()
	defaultHub = h
	defaultHubMu.Unlock()
}

// Default 取当前 hub; 未装配时为 nil
func Default() *Hub {
	defaultHubMu.RLock()
	defer defaultHubMu.RUnlock()
	return defaultHub
}

// AdmitRoomCreate 是各建房路径的统一准入入口; 无 hub 时立即放行
// (未配置插件 = 行为与现网完全一致).
func AdmitRoomCreate(req RoomCreateRequest) Decision {
	if h := Default(); h != nil {
		return h.AdmitRoomCreate(req)
	}
	return Decision{Allowed: true}
}

// EmitGameOver 是对局结束事件的唯一发射入口; 无 hub 时为空操作
func EmitGameOver(roomCode string, data *event.GameOverData) {
	if h := Default(); h != nil {
		h.EmitGameOver(roomCode, data)
	}
}

// PlayerInfoOf 从玩家实体提取契约 PlayerInfo 段.
//
// 只含 Avalon 实际持有的字段; 对局内数据 (战绩/位置/存活) 属引擎内部,
// 不在此列 (knowledge/0007 的 X 类判断).
func PlayerInfoOf(p *player.Player) PlayerInfo {
	info := PlayerInfo{
		Name:  p.Name(),
		UUID:  p.ConnectHexID(),
		Team:  p.Team(),
		Slot:  p.Index(),
		Admin: p.IsAdmin,
	}
	d := p.Data()
	info.Host = d.Host
	info.Bot = d.Bot
	info.Spectator = d.Spectator
	if con := p.Con(); con != nil {
		info.Address = con.IP()
	}
	return info
}

// RegisterRoomEvents 把全部事件订阅挂到房间事件总线上, 接线方式与
// 内建订阅者一致 (httpapi.RegisterRoomEvents / uplist.RegisterRoomEvents).
//
// v1: game.over (deliver 档).
// v2: player.join (观察 + 授权否决), player.leave / player.banned /
// player.unbanned / player.ipbanned / game.start / player.chat /
// unit.operation / factory.build (观察档; 后两者授权后可否决).
func RegisterRoomEvents(r *room.ServerRoom) {
	if r == nil || r.EventManage == nil {
		return
	}

	// --- v1: game.over (deliver 档) ---
	event.Register(r.EventManage, func(ev *event.ServerGameOverEvent) {
		if ev == nil {
			return
		}
		EmitGameOver(r.GameSession.RoomID, ev.GameOverData)
	})

	// --- v2: player.join (观察 + 授权否决) ---
	//
	// helper 事件分发跑在单一 pump goroutine 上 (同时承载请求应答与全部
	// 游戏包路由), 准入绝不能在该线上同步等待网络 —— 卸到独立 goroutine:
	// 玩家此时已注册, 异步踢出与同步踢出的可观察语义一致 (否决表现为
	// "进入后被立即踢出并附理由", 契约已如此定义).
	event.Register(r.EventManage, func(ev *event.PlayerJoinEvent) {
		h := Default()
		if h == nil || ev == nil || ev.Player == nil {
			return
		}
		roomCode := r.GameSession.RoomID
		info := PlayerInfoOf(ev.Player)

		if !h.HasVetoConsult(EventPlayerJoin) {
			h.EmitPlayerJoinObserve(roomCode, info)
			return
		}
		// 预构造: eventId/occurredAt 锚定玩家注册时刻
		je := buildPlayerJoinEvent(roomCode, info, time.Now())
		body := jsonutil.ToJSON(je)
		go func(p *player.Player) {
			defer func() {
				if x := recover(); x != nil {
					log.Errorf("[Plugin]", "player.join admission panic: {0}", x)
				}
			}()
			if d := h.AdmitPlayerJoinBody([]byte(body)); !d.Allowed {
				p.KickPlayer(d.Reason, 0)
				return
			}
			h.EmitPlayerJoinPrebuilt(je.EventID, body)
		}(ev.Player)
	})

	// --- v2: player.leave (观察) ---
	//
	// 断开原因 (自愿/被踢/网络断开) 取不到: 只携带重进锁定剩余窗口
	// (>0 说明带着踢/封窗口离开), 契约已如实声明.
	event.Register(r.EventManage, func(ev *event.PlayerLeaveEvent) {
		h := Default()
		if h == nil || ev == nil || ev.Player == nil {
			return
		}
		p := ev.Player
		banRemaining := p.KickTime - timeutil.ConcurrentMillis()
		if banRemaining < 0 {
			banRemaining = 0
		}
		h.EmitPlayerLeave(r.GameSession.RoomID, PlayerInfoOf(p), banRemaining, r.IsStartGame)
	})

	// --- v2: 封禁/解封 (观察) ---
	event.Register(r.EventManage, func(ev *event.PlayerBanEvent) {
		if h := Default(); h != nil && ev != nil && ev.Player != nil {
			h.EmitPlayerAction(EventPlayerBanned, r.GameSession.RoomID, PlayerInfoOf(ev.Player))
		}
	})
	event.Register(r.EventManage, func(ev *event.PlayerUnBanEvent) {
		if h := Default(); h != nil && ev != nil && ev.Player != nil {
			h.EmitPlayerAction(EventPlayerUnbanned, r.GameSession.RoomID, PlayerInfoOf(ev.Player))
		}
	})
	event.Register(r.EventManage, func(ev *event.PlayerIpBanEvent) {
		if h := Default(); h != nil && ev != nil && ev.Player != nil {
			h.EmitPlayerAction(EventPlayerIpBanned, r.GameSession.RoomID, PlayerInfoOf(ev.Player))
		}
	})

	// --- v2: game.start (观察, 开局时刻房间快照) ---
	event.Register(r.EventManage, func(*event.ServerGameStartEvent) {
		h := Default()
		if h == nil {
			return
		}
		match := MatchInfo{
			MaxPlayers: r.MaxPlayer,
			StartTime:  r.GetStartTime(),
		}
		if r.Maps != nil {
			match.MapName = r.Maps.MapName
			match.MapPlayer = r.Maps.MapPlayer
		}
		// PlayerGroup = 当前在房的玩家 (PlayerAll 还含对局中途离开的残留档案)
		r.PlayerManage.PlayerGroup.EachAll(func(p *player.Player) {
			match.Players = append(match.Players, PlayerInfoOf(p))
		})
		h.EmitGameStart(r.GameSession.RoomID, match)
	})

	// --- v2: player.chat (观察; 消息已过内置过滤, 禁言消息不会到达) ---
	event.Register(r.EventManage, func(ev *event.PlayerChatEvent) {
		h := Default()
		if h == nil || ev == nil || ev.Player == nil {
			return
		}
		h.EmitPlayerChat(r.GameSession.RoomID, PlayerInfoOf(ev.Player), ev.Message)
	})

	// --- v2: unit.operation (观察; 授权后同步否决, 紧超时档) ---
	//
	// 本监听器跑在连接的收包 goroutine 上, 否决调用以 gameplayVetoTimeoutMs
	// 封顶 (默认 250ms), 只阻塞发起者自己的指令流. 否决 = 置 ResultStatus
	// 为假, 该指令包不再转给引擎; 理由只进日志 (对局内无玩家侧呈现通道).
	event.Register(r.EventManage, func(ev *event.PlayerOperationUnitEvent) {
		h := Default()
		if h == nil || ev == nil || ev.Player == nil {
			return
		}
		roomCode := r.GameSession.RoomID
		info := PlayerInfoOf(ev.Player)
		ue := buildUnitOperationEvent(roomCode, info, ev.Actions.Name(), ev.UnitName, ev.X, ev.Y, time.Now())
		body := jsonutil.ToJSON(ue)
		if h.HasVetoConsult(EventUnitOperation) {
			if d := h.AdmitUnitOperationBody([]byte(body)); !d.Allowed {
				ev.ResultStatus = false
				log.Warnf("[Plugin]", "unit.operation vetoed: {0}", d.Reason)
			}
		}
		h.enqueueEvent(EventUnitOperation, ue.EventID, body, TierObserve)
	})

	// --- v2: factory.build (观察; 授权后同步否决, 紧超时档) ---
	event.Register(r.EventManage, func(ev *event.PlayerOperationFactoryBuildUnitEvent) {
		h := Default()
		if h == nil || ev == nil || ev.Player == nil {
			return
		}
		roomCode := r.GameSession.RoomID
		info := PlayerInfoOf(ev.Player)
		fe := buildFactoryBuildEvent(roomCode, info, ev.ActionID, time.Now())
		body := jsonutil.ToJSON(fe)
		if h.HasVetoConsult(EventFactoryBuild) {
			if d := h.AdmitFactoryBuildBody([]byte(body)); !d.Allowed {
				ev.ResultStatus = false
				log.Warnf("[Plugin]", "factory.build vetoed: {0}", d.Reason)
			}
		}
		h.enqueueEvent(EventFactoryBuild, fe.EventID, body, TierObserve)
	})
}
