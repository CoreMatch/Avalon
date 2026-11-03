// Package event 对应 net.rwhps.server.game.event (EventManage + game 事件类).
//
// # 与上游的架构差异
//
// 上游事件总线由三件套组成:
//   - Events (util/game): 按 Class 维护监听表, fire 时用协程并行跑监听, 再 runBlocking 等全部结束
//   - AbstractEventManage: 反射扫描 EventListenerHost 上带 @EventListenerHandler 注解的方法
//   - EventManage / EventGlobalManage: 分别持有游戏事件与全局事件两条总线
//
// Go 没有注解反射, 也不需要同 JVM 语义的协程调度, 因此收敛为:
//   - 显式泛型注册 (Register[T]), 免去反射扫描
//   - Fire 同步逐个执行监听 (上游并行后 join, 可观察语义等同同步)
//   - 全局事件总线在 app.Events (已存在), 本包只承担游戏内事件 (对应 EventManage)
//
// 上游事件的 gameModule 字段一律不带: 上游监听器经 event.gameModule.room 取房间,
// Go 版监听器是闭包, 房间在注册时捕获, 事件只携带最小负载 —— 这样 event 包
// 不必反向依赖 room, 避免循环导入.
//
// 上游 AbstractEvent.status() 恒返回 CONTINUE (无任何事件覆盖它), 因此 Go 版
// 不复刻该开关; 若将来出现可拦截事件, 再按需补充.
package event

import (
	"reflect"
	"sync"

	"rwhps/internal/util/log"
)

// Manage 对应 EventManage: 一个房间的游戏事件总线
//
// 上游每个 gameModule 持有自己的 EventManage; Go 版每个 ServerRoom 持有一个.
type Manage struct {
	mu        sync.RWMutex
	listeners map[reflect.Type][]func(any)
}

// NewManage 对应 EventManage()
func NewManage() *Manage {
	return &Manage{listeners: make(map[reflect.Type][]func(any), 16)}
}

// Register 对应 EventManage.registerListener(eventClass, consumer)
//
// 上游用 Class + Consumer 注册; Go 版以泛型参数 T 表达事件类型.
// 注册表按事件的具体类型索引, Fire 时按运行时类型分发.
func Register[T any](m *Manage, fn func(T)) {
	if m == nil || fn == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	t := reflect.TypeFor[T]()
	m.listeners[t] = append(m.listeners[t], func(v any) {
		ev, ok := v.(T)
		if !ok {
			return
		}
		fn(ev)
	})
}

// Fire 对应 EventManage.fire: 同步分发给该事件类型的全部监听器
//
// 上游把每个监听器丢进协程再 runBlocking join, 可观察行为与同步逐个执行一致;
// Go 版直接同步执行. 单个监听器 panic 不应带走整条分发链, 与上游
// EventListenerHost.handleException 捕获异常的语义对齐, 逐个 recover 并记录.
func (m *Manage) Fire(ev any) {
	if m == nil || ev == nil {
		return
	}

	m.mu.RLock()
	handlers := m.listeners[reflect.TypeOf(ev)]
	snapshot := make([]func(any), len(handlers))
	copy(snapshot, handlers)
	m.mu.RUnlock()

	for _, fn := range snapshot {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Errorf("[Event]", "{0} listener panic: {1}", reflect.TypeOf(ev), r)
				}
			}()
			fn(ev)
		}()
	}
}

// ListenerCount 返回某事件类型的监听器数量 (诊断与测试用)
func (m *Manage) ListenerCount(ev any) int {
	if m == nil || ev == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.listeners[reflect.TypeOf(ev)])
}
