// Package app 对应 net.rwhps.server.game.EventGlobal 与 PluginManage 里的全局事件总线.
//
// 原版的全局事件由 PluginManage 统一持有: addGlobalEventManage 把 EventListenerHost
// 注册进 EventManage, 再用 @EventListenerHandler 注解的方法名做反射分发.
// Go 没有反射注解, 也没有插件系统 (见 batch-1 的决定), 因此这里只保留最小可用形态:
// 显式注册"事件类型 -> 处理函数", 事件本身用带名字的结构体表示.
//
// EventGlobal 自身没有移植: 它的四个处理器全都依赖尚未移植的子系统
// (HeadlessModuleManage / PlayerHess / RelayRoom / 客户端命令注册),
// 现在抄过来只能写成空函数, 反而会掩盖"事件系统还不完整"这件事.
package app

import (
	"fmt"
	"sync"

	"rwhps/internal/net/core"
	"rwhps/internal/util/log"
)

// GlobalEvent 对应 game.event.global 下的各类全局事件
type GlobalEvent interface {
	// EventName 对应事件类的简单类名, 也是分发用的键
	EventName() string
}

// ServerLoadEvent 对应 ServerLoadEvent: 服务器加载完毕
type ServerLoadEvent struct{}

// EventName 实现 GlobalEvent
func (ServerLoadEvent) EventName() string { return "ServerLoadEvent" }

// ServerStartTypeEvent 对应 ServerStartTypeEvent: 以某种协议启动了服务
type ServerStartTypeEvent struct {
	// ServerNetType 对应 serverNetType
	ServerNetType core.NetType
}

// EventName 实现 GlobalEvent
func (ServerStartTypeEvent) EventName() string { return "ServerStartTypeEvent" }

// Listener 对应 EventListenerHost
//
// 实现方在 RegisterGlobalEvents 里决定自己关心哪些事件.
type Listener interface {
	RegisterGlobalEvents(bus *Events)
}

// Events 对应 EventManage 里的全局事件表
type Events struct {
	mu        sync.RWMutex
	listeners map[string][]func(GlobalEvent)
}

// NewEvents 创建空的事件总线
func NewEvents() *Events {
	return &Events{listeners: make(map[string][]func(GlobalEvent), 8)}
}

// AddGlobalEventManager 对应 PluginManage.addGlobalEventManage
func (e *Events) AddGlobalEventManager(l Listener) {
	if l == nil {
		return
	}
	l.RegisterGlobalEvents(e)
}

// Register 注册一个事件处理函数
func (e *Events) Register(eventName string, fn func(GlobalEvent)) {
	if fn == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.listeners[eventName] = append(e.listeners[eventName], fn)
}

// Fire 对应 EventManage.fire: 同步分发给全部处理函数
//
// 原版 fire 返回 EventFuture 供调用方 await; 这里的事件处理都是同步的, 不需要 Future.
// 单个处理函数 panic 不应该带走整个分发流程, 因此逐个 recover 并记录.
func (e *Events) Fire(ev GlobalEvent) {
	if ev == nil {
		return
	}
	name := ev.EventName()

	e.mu.RLock()
	handlers := make([]func(GlobalEvent), len(e.listeners[name]))
	copy(handlers, e.listeners[name])
	e.mu.RUnlock()

	for _, fn := range handlers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error(fmt.Sprintf("[Event] %s handler panic: %v", name, r))
				}
			}()
			fn(ev)
		}()
	}
}

// ServerLoad 对应 runGlobalEventManage(ServerLoadEvent()).await()
func (e *Events) ServerLoad() { e.Fire(ServerLoadEvent{}) }

// ServerStartType 对应 runGlobalEventManage(ServerStartTypeEvent(type)).await()
func (e *Events) ServerStartType(t core.NetType) {
	e.Fire(ServerStartTypeEvent{ServerNetType: t})
}

// ListenerCount 返回某类事件的处理函数数量 (诊断用)
func (e *Events) ListenerCount(eventName string) int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.listeners[eventName])
}
