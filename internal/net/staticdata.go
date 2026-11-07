// Package net 对应 上游 data.global.NetStaticData
//
// Kotlin 版是 object NetStaticData, 持有 GroupNet / RelayRoom / BlackList / netService 列表
// 以及惰性的 ProtocolProvider. Go 版用包级单例 NetStaticData 承载同一组状态.
//
// 与原版差异:
//   - RelayRoom 属于 Relay 批次, 此处未引入
//   - netService 由 Seq<NetService> 换成带读写锁的切片 (并发 Add/Remove 更安全)
package net

import (
	"sync"

	"github.com/CoreMatch/Avalon/internal/game/manage"
	"github.com/CoreMatch/Avalon/internal/net/core"
)

// NetStatic 对应 NetStaticData 单例的类型
type NetStatic struct {
	// GroupNet 对应 NetStaticData.groupNet
	GroupNet *GroupNet

	// BlackList 对应 NetStaticData.blackList
	BlackList *BlackList

	mu       sync.RWMutex
	services []*NetService

	protocolProviderOnce sync.Once
	protocolProvider     core.IProtocolProvider
}

// NetStaticData 对应 object NetStaticData
var NetStaticData = &NetStatic{
	GroupNet:  NewGroupNet(),
	BlackList: NewBlackList(),
}

// AddService 对应 NetService.init { NetStaticData.netService.add(this) }
func (n *NetStatic) AddService(s *NetService) {
	if s == nil {
		return
	}
	n.mu.Lock()
	n.services = append(n.services, s)
	n.mu.Unlock()
}

// RemoveService 对应 NetService.stop() 里的 netService.remove(this)
func (n *NetStatic) RemoveService(s *NetService) {
	if s == nil {
		return
	}
	n.mu.Lock()
	for i, v := range n.services {
		if v == s {
			n.services = append(n.services[:i], n.services[i+1:]...)
			break
		}
	}
	n.mu.Unlock()
}

// Services 返回当前存活的 NetService 快照
func (n *NetStatic) Services() []*NetService {
	n.mu.RLock()
	defer n.mu.RUnlock()
	out := make([]*NetService, len(n.services))
	copy(out, n.services)
	return out
}

// ProtocolProvider 对应 NetStaticData.ProtocolProvider (by lazy)
//
// 原版: ProtocolManage.firstServer ?: FakeProtocolProvider(classLoader, NullProtocol)
// 结果缓存一次; 若首次调用时还没有任何服务端协议注册, 后续注册也不会再刷新 —— 与原版 lazy 语义一致.
func (n *NetStatic) ProtocolProvider() core.IProtocolProvider {
	n.protocolProviderOnce.Do(func() {
		if first := manage.FirstServer(); first != nil {
			n.protocolProvider = first
			return
		}
		n.protocolProvider = core.NewNullProtocolProvider(core.NullProtocol)
	})
	return n.protocolProvider
}

// ResetProtocolProvider 清掉 ProtocolProvider 的惰性缓存 (测试与热重载用, 原版无对应)
func (n *NetStatic) ResetProtocolProvider() {
	n.protocolProviderOnce = sync.Once{}
	n.protocolProvider = nil
}

// CheckServerStartNet 对应 NetStaticData.checkServerStartNet
//
// 没有可用的服务端协议时返回 false 且不执行 run; 否则执行 run 并返回 true.
func CheckServerStartNet(run func()) bool {
	if _, ok := NetStaticData.ProtocolProvider().(*core.NullProtocolProvider); ok {
		return false
	}
	if run != nil {
		run()
	}
	return true
}
