// Package service 对应 net.rwhps.server.core.ServiceLoader.
//
// 原版用 反射 + ClassLoader 键 做服务注册; Go 没有类加载器维度, 这里用显式的
// (Kind, Name) 键, 键的拼接规则与原版一致: Kind + Name.
package service

import (
	"fmt"
	"sync"

	"rwhps/internal/net/core"
	netpacket "rwhps/internal/net/core/server/netpacket"
)

// Kind 对应 ServiceLoader.ServiceType
type Kind string

const (
	// KindCore 对应 ServiceType.Core
	KindCore Kind = "Core"
	// KindIRwHps 对应 ServiceType.IRwHps
	KindIRwHps Kind = "IRwHps"
	// KindProtocolType 对应 ServiceType.ProtocolType
	KindProtocolType Kind = "ProtocolType"
	// KindProtocol 对应 ServiceType.Protocol
	KindProtocol Kind = "Protocol"
	// KindProtocolPacket 对应 ServiceType.ProtocolPacket
	KindProtocolPacket Kind = "ProtocolPacket"
	// KindPacketType 对应 ServiceType.PacketType
	KindPacketType Kind = "PacketType"
)

// Key 对应 addService 里的 name 拼接 (不含 classloader 后缀)
func Key(k Kind, name string) string { return string(k) + name }

// ErrNotRegistered 对应 ImplementedException("${serviceType.name}:$serviceName")
type ErrNotRegistered struct {
	Kind Kind
	Name string
}

func (e *ErrNotRegistered) Error() string {
	return fmt.Sprintf("%s:%s", e.Kind, e.Name)
}

// ErrTypeMismatch 对应 VariableException.TypeMismatchException
type ErrTypeMismatch struct {
	Kind Kind
	Name string
}

func (e *ErrTypeMismatch) Error() string {
	return fmt.Sprintf("[AddService] %s : %s type mismatch", e.Kind, e.Name)
}

// Registry 服务注册表
type Registry struct {
	mu      sync.RWMutex
	objects map[string]any
}

// New 创建注册表
func New() *Registry {
	return &Registry{objects: make(map[string]any, 16)}
}

// Default 全局注册表, 对应 ServiceLoader 的伴生对象
var Default = New()

// Register 对应 addServiceObject; 已存在且 cover=false 时跳过 (与原版一致)
func (r *Registry) Register(k Kind, name string, v any, cover bool) error {
	if err := checkKind(k, v); err != nil {
		return err
	}
	key := Key(k, name)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.objects[key]; ok && !cover {
		return nil
	}
	r.objects[key] = v
	return nil
}

// RegisterDefault 对应 addServiceObject 的默认 cover=false
func (r *Registry) RegisterDefault(k Kind, name string, v any) error {
	return r.Register(k, name, v, false)
}

// Lookup 对应 getServiceObject 的非抛异常版本
func (r *Registry) Lookup(k Kind, name string) (any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.objects[Key(k, name)]
	return v, ok
}

// MustLookup 对应 getServiceObject, 未注册返回 ErrNotRegistered
func (r *Registry) MustLookup(k Kind, name string) (any, error) {
	if v, ok := r.Lookup(k, name); ok {
		return v, nil
	}
	return nil, &ErrNotRegistered{Kind: k, Name: name}
}

// Remove 移除一个注册项 (测试与热重载用)
func (r *Registry) Remove(k Kind, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.objects, Key(k, name))
}

// Names 返回某类服务已注册的名字 (无序)
func (r *Registry) Names(k Kind) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	prefix := string(k)
	out := make([]string, 0, len(r.objects))
	for key := range r.objects {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			out = append(out, key[len(prefix):])
		}
	}
	return out
}

// checkKind 对应 addService 里的 findSuperClass 类型校验
func checkKind(k Kind, v any) error {
	if v == nil {
		return &ErrTypeMismatch{Kind: k}
	}
	switch k {
	case KindIRwHps:
		if _, ok := v.(core.IRwHps); !ok {
			return &ErrTypeMismatch{Kind: k}
		}
	case KindProtocolType:
		if _, ok := v.(core.TypeConnect); !ok {
			return &ErrTypeMismatch{Kind: k}
		}
	case KindProtocol:
		if _, ok := v.(*core.AbstractNetConnect); !ok {
			return &ErrTypeMismatch{Kind: k}
		}
	case KindProtocolPacket:
		if _, ok := v.(netpacket.AbstractNetPacket); !ok {
			return &ErrTypeMismatch{Kind: k}
		}
	}
	return nil
}
