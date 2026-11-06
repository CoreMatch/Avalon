// Package manage 对应 net.rwhps.server.game.manage.IRwHpsManage
//
// 原版通过 ServiceLoader 按协议名查找 IRwHps 实现, 找不到就退回全局默认实现.
// 本项目暂不引入插件系统, 因此用一个可替换的 Provider 作为接缝:
// 具体协议实现方 (后续批次的连接层) 调用 SetProvider 注入, 未注入时退回 NullRwHps.
package manage

import (
	"sync"

	"rwhps/internal/net/core"
)

// Provider 按 NetType 构造一个 IRwHps 实现
// 对应 ServiceLoader.getService(ServiceType.IRwHps, ...).newInstance(classLoader, value)
type Provider func(netType core.NetType) core.IRwHps

var (
	mu sync.RWMutex

	// rwHpsList 对应 IRwHpsManage.rwHpsList
	// 原版 key 为 classLoader.name + customId; Go 版无 classloader, 直接用 customId
	rwHpsList = map[string]core.IRwHps{}

	// order 记录注册顺序, 让 FirstServer 的结果稳定 (原版 ObjectMap 亦保序)
	order []string

	// provider 默认为 FakeRwHps 等价物
	provider Provider = func(netType core.NetType) core.IRwHps {
		return core.NewNullRwHps(netType)
	}
)

// SetProvider 注入协议实现工厂 (对应 ServiceLoader 的角色)
// 传 nil 恢复默认的 NullRwHps 工厂
func SetProvider(p Provider) {
	mu.Lock()
	defer mu.Unlock()
	if p == nil {
		provider = func(netType core.NetType) core.IRwHps {
			return core.NewNullRwHps(netType)
		}
		return
	}
	provider = p
}

// AddIRwHps 对应 IRwHpsManage.addIRwHps(value, customId)
//
// customID 为空时用 netType 的名字, 与原版默认参数 customId = value.name 一致.
// netType 为 NullProtocol 时 panic: 原版 require(value != NullProtocol) 抛 IllegalArgumentException,
// 这是调用方的编码错误, 不属于可恢复的运行时状况.
func AddIRwHps(netType core.NetType, customID string) core.IRwHps {
	if netType == core.NullProtocol {
		panic("manage: a valid agreement is required")
	}
	if customID == "" {
		customID = netType.String()
	}

	mu.Lock()
	defer mu.Unlock()

	inst := provider(netType)
	if _, exists := rwHpsList[customID]; !exists {
		order = append(order, customID)
	}
	rwHpsList[customID] = inst
	return inst
}

// Get 按 customID 取实现 (原版无对应, 供诊断/命令使用)
func Get(customID string) (core.IRwHps, bool) {
	mu.RLock()
	defer mu.RUnlock()
	v, ok := rwHpsList[customID]
	return v, ok
}

// All 按注册顺序返回全部实现
func All() []core.IRwHps {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]core.IRwHps, 0, len(order))
	for _, id := range order {
		out = append(out, rwHpsList[id])
	}
	return out
}

// FirstServer 对应 IRwHpsManage.firstServer
// 按注册顺序返回第一个"服务端协议"实现, 没有则返回 nil
func FirstServer() core.IRwHps {
	mu.RLock()
	defer mu.RUnlock()
	for _, id := range order {
		v := rwHpsList[id]
		if v != nil && core.CheckProtocolIsServer(v.NetType()) {
			return v
		}
	}
	return nil
}

// Clear 清空注册表并恢复默认工厂 (测试用, 原版无对应)
func Clear() {
	mu.Lock()
	defer mu.Unlock()
	rwHpsList = map[string]core.IRwHps{}
	order = nil
	provider = func(netType core.NetType) core.IRwHps {
		return core.NewNullRwHps(netType)
	}
}
