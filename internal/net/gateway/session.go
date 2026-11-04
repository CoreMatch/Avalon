package gateway

import (
	"errors"
	"sync"
	"time"

	"rwhps/internal/io/packettype"
	"rwhps/internal/util/log"
)

// 契约 "Session state machine" (Cascade-Backend-v1.md :45-59):
//
//	PENDING: 已接受前导, 等待身份绑定 (5 秒期限)
//	BOUND:   校验并原子消费了有效 JWT, 此后才允许玩家的注册与游戏流量
//	CLOSED:  连接结束 / 绑定超时 / 致命协议错误
//
// 有意修正（作者已批准，2026-11-04）: 任务文案"扣住首包"按契约执行为
// "160 放行应答、110 及后续门控到 BOUND"——客户端要先收到 161 才会发
// RegisterPlayer, 代理也才因此触发绑定, 扣住 160 会死锁 (DECISIONS D-18).

// 绑定/查询错误, 由控制面映射成契约错误码
var (
	// ErrSessionNotFound 会话不存在 (SESSION_NOT_FOUND)
	ErrSessionNotFound = errors.New("gateway: session not found")
	// ErrSessionExpired 会话已因超时/关闭而消失 (SESSION_EXPIRED)
	ErrSessionExpired = errors.New("gateway: session expired")
	// ErrSessionNotPending 绑定目标不在 PENDING (换身份/换 jti 重试时为冲突)
	ErrSessionNotPending = errors.New("gateway: session is not pending")
	// ErrSessionConflict 已绑定会话收到不同身份或不同 jti (SESSION_IDENTITY_CONFLICT)
	ErrSessionConflict = errors.New("gateway: session identity conflict")
)

// SessionState 会话状态
type SessionState int

const (
	// StatePending 等待身份绑定
	StatePending SessionState = iota
	// StateBound 身份已绑定
	StateBound
	// StateClosed 已结束
	StateClosed
)

// Session 是一条代理转接连接的后端侧会话
type Session struct {
	// ID 前导里的 UUIDv4 会话 ID
	ID string

	mu       sync.Mutex
	state    SessionState
	playerID string // BOUND 后的游戏 UUID (JWT sub)
	jti      string // 消费掉的令牌 ID (幂等重试用)

	// stopTimer 取消 pending 超时定时器 (绑定时调用)
	stopTimer func()
	// onClose 超时/失效时关闭底层连接, 由注册方注入
	onClose func()
}

// State 当前状态
func (s *Session) State() SessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// PlayerID BOUND 后返回游戏 UUID, 其余状态返回空
func (s *Session) PlayerID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateBound {
		return ""
	}
	return s.playerID
}

// AllowPacket 实现 core.PacketGate: 绑定前的包放行裁决
//
// PENDING 只放行 Preregister(160): 引擎照常应答 161, 客户端随后才会发
// RegisterPlayer; 其它任何包在 BOUND 前出现都是协议违例 (契约 :54-57).
func (s *Session) AllowPacket(typeInt int) bool {
	s.mu.Lock()
	state := s.state
	s.mu.Unlock()
	switch state {
	case StateBound:
		return true
	case StatePending:
		return typeInt == int(packettype.PREREGISTER_INFO_RECEIVE)
	default:
		return false
	}
}

// bind PENDING -> BOUND 的状态迁移 (注册表持锁调用前先完成 jti 消费)
func (s *Session) bind(playerID, jti string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.state {
	case StatePending:
		s.state = StateBound
		s.playerID = playerID
		s.jti = jti
		if s.stopTimer != nil {
			s.stopTimer()
		}
		return nil
	case StateBound:
		// 幂等重试: 完全相同的身份 + jti 接受 (契约 :99-101)
		if s.playerID == playerID && s.jti == jti {
			return nil
		}
		return ErrSessionConflict
	default:
		return ErrSessionExpired
	}
}

// close 标记 CLOSED 并断开底层连接 (超时路径)
func (s *Session) close() {
	s.mu.Lock()
	if s.state == StateClosed {
		s.mu.Unlock()
		return
	}
	s.state = StateClosed
	onClose := s.onClose
	s.mu.Unlock()
	if onClose != nil {
		onClose()
	}
}

// Registry 按会话 ID 管理全部代理转接会话
//
// 单进程单实例 (与 room.Default 同款单例语义): 一个游戏监听器对应一个
// 注册表, 由网关装配时创建并注入控制面.
type Registry struct {
	mu             sync.Mutex
	sessions       map[string]*Session
	pendingTimeout time.Duration
}

// NewRegistry 构造注册表; pendingTimeout 为 PENDING 绑定限期 (契约 :58, 默认 5 秒)
func NewRegistry(pendingTimeout time.Duration) *Registry {
	if pendingTimeout <= 0 {
		pendingTimeout = 5 * time.Second
	}
	return &Registry{
		sessions:       make(map[string]*Session),
		pendingTimeout: pendingTimeout,
	}
}

// Register 登记一个新的 PENDING 会话并挂上绑定限期.
//
// onClose 在超时时被调用以断开底层连接; 同 ID 重复登记 (理论上 UUIDv4
// 不会碰撞) 返回已存在的会话而不重置.
func (r *Registry) Register(id string, onClose func()) *Session {
	r.mu.Lock()
	if existing, ok := r.sessions[id]; ok {
		r.mu.Unlock()
		return existing
	}
	s := &Session{ID: id, state: StatePending, onClose: onClose}
	r.sessions[id] = s
	timeout := r.pendingTimeout
	r.mu.Unlock()

	timer := time.AfterFunc(timeout, func() {
		r.mu.Lock()
		current, ok := r.sessions[id]
		if ok && current == s {
			delete(r.sessions, id)
		}
		r.mu.Unlock()
		if ok && current == s {
			log.Warnf("[Cascade]", "pending session {0} expired after {1}, closing", id, timeout)
			s.close()
		}
	})
	s.mu.Lock()
	// 定时器可能在极短窗口内已触发 (不太可能但防御): 状态已变则停掉
	if s.state != StatePending {
		timer.Stop()
	} else {
		s.stopTimer = func() { timer.Stop() }
	}
	s.mu.Unlock()
	return s
}

// Get 按 ID 查会话 (不存在返回 nil)
func (r *Registry) Get(id string) *Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessions[id]
}

// Remove 连接正常结束时清理 (幂等)
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	if s, ok := r.sessions[id]; ok {
		delete(r.sessions, id)
		r.mu.Unlock()
		s.mu.Lock()
		if s.state != StateClosed {
			s.state = StateClosed
			if s.stopTimer != nil {
				s.stopTimer()
			}
		}
		s.mu.Unlock()
		return
	}
	r.mu.Unlock()
}

// Bind 把验证通过的令牌绑定到会话 (控制面 PUT /v1/sessions/{sid}/identity).
//
// jti 的"一次性消费"由调用方先经 JtiStore 完成 (重放返回 ErrJtiReplayed);
// 本方法负责会话状态机部分:
//
//	会话不存在        -> ErrSessionNotFound
//	PENDING           -> BOUND, 返回 nil
//	BOUND 同身份同 jti -> nil (幂等, 契约 :99-101)
//	BOUND 其它情况     -> ErrSessionConflict
//	CLOSED            -> ErrSessionExpired
func (r *Registry) Bind(id, playerID, jti string) error {
	s := r.Get(id)
	if s == nil {
		return ErrSessionNotFound
	}
	return s.bind(playerID, jti)
}

// Size 当前会话数 (诊断/测试)
func (r *Registry) Size() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sessions)
}
