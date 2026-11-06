package event

import (
	"testing"

	"rwhps/internal/game/player"
)

// TestRegisterFire 基本注册与分发: 监听器收到自己类型的事件
func TestRegisterFire(t *testing.T) {
	m := NewManage()

	var got *PlayerJoinEvent
	Register(m, func(ev *PlayerJoinEvent) { got = ev })

	p := &player.Player{}
	ev := &PlayerJoinEvent{Player: p}
	m.Fire(ev)

	if got != ev {
		t.Fatalf("监听器未收到事件: %v", got)
	}
}

// TestFireDispatchByType 不同事件类型互不串扰
func TestFireDispatchByType(t *testing.T) {
	m := NewManage()

	joins, leaves := 0, 0
	Register(m, func(*PlayerJoinEvent) { joins++ })
	Register(m, func(*PlayerLeaveEvent) { leaves++ })

	m.Fire(&PlayerJoinEvent{})
	m.Fire(&PlayerJoinEvent{})
	m.Fire(&PlayerLeaveEvent{})

	if joins != 2 || leaves != 1 {
		t.Fatalf("分发计数错误: joins=%d leaves=%d", joins, leaves)
	}
}

// TestFireOrder 同一事件的多个监听器按注册顺序执行 (上游同类型监听器同样有序)
func TestFireOrder(t *testing.T) {
	m := NewManage()

	var order []int
	Register(m, func(*ServerGameStartEvent) { order = append(order, 1) })
	Register(m, func(*ServerGameStartEvent) { order = append(order, 2) })

	m.Fire(&ServerGameStartEvent{})

	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("监听器顺序错误: %v", order)
	}
}

// TestFirePanicRecover 单个监听器 panic 不影响后续监听器 (对应上游
// EventListenerHost.handleException 吞异常的行为)
func TestFirePanicRecover(t *testing.T) {
	m := NewManage()

	after := false
	Register(m, func(*PlayerChatEvent) { panic("boom") })
	Register(m, func(*PlayerChatEvent) { after = true })

	m.Fire(&PlayerChatEvent{Message: "hi"})

	if !after {
		t.Fatal("panic 监听器之后的监听器未执行")
	}
}

// TestFireNil Fire(nil) 与 nil 总线都不应 panic
func TestFireNil(t *testing.T) {
	m := NewManage()
	m.Fire(nil)

	var nilManage *Manage
	nilManage.Fire(&PlayerJoinEvent{})
}

// TestRegisterNil Register 对 nil 总线 / nil 函数静默降级
func TestRegisterNil(t *testing.T) {
	var m *Manage
	Register(m, func(*PlayerJoinEvent) {})

	m2 := NewManage()
	var nilFn func(*PlayerJoinEvent)
	Register(m2, nilFn)
	if n := m2.ListenerCount(&PlayerJoinEvent{}); n != 0 {
		t.Fatalf("nil 函数不应注册成功: %d", n)
	}
}

// TestListenerCount 诊断计数
func TestListenerCount(t *testing.T) {
	m := NewManage()
	if n := m.ListenerCount(&PlayerBanEvent{}); n != 0 {
		t.Fatalf("空总线计数应为 0: %d", n)
	}
	Register(m, func(*PlayerBanEvent) {})
	Register(m, func(*PlayerBanEvent) {})
	if n := m.ListenerCount(&PlayerBanEvent{}); n != 2 {
		t.Fatalf("计数应为 2: %d", n)
	}
}

// TestResultStatusDefaults 操作类事件的 ResultStatus 由触发方显式置 true,
// 监听器可改写 (对应上游 @JvmField resultStatus 的用法)
func TestResultStatusIntercept(t *testing.T) {
	m := NewManage()
	Register(m, func(ev *PlayerOperationUnitEvent) {
		ev.ResultStatus = false
	})

	ev := &PlayerOperationUnitEvent{ResultStatus: true}
	m.Fire(ev)
	if ev.ResultStatus {
		t.Fatal("监听器应能把 ResultStatus 改为 false")
	}
}
