package plugin

import (
	"sync"

	"github.com/CoreMatch/Avalon/internal/game/event"
	"github.com/CoreMatch/Avalon/internal/game/room"
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

// RegisterRoomEvents 把 game.over 订阅挂到房间事件总线上, 接线方式与
// 内建订阅者一致 (httpapi.RegisterRoomEvents / uplist.RegisterRoomEvents).
func RegisterRoomEvents(r *room.ServerRoom) {
	if r == nil || r.EventManage == nil {
		return
	}
	event.Register(r.EventManage, func(ev *event.ServerGameOverEvent) {
		if ev == nil {
			return
		}
		EmitGameOver(r.GameSession.RoomID, ev.GameOverData)
	})
}
