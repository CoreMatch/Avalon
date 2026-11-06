// Package net (Go-Server) 对应 net.rwhps.server.data.global.NetStaticData
//
// Kotlin 版是 object NetStaticData, 持有 GroupNet / RelayRoom / BlackList / netService 列表
// 以及惰性的 RwHps. Go 版用包级单例 NetStaticData 承载同一组状态.
//
// 与原版差异:
//   - RelayRoom 属于 Relay 批次, 此处未引入
//   - netService 由 Seq<NetService> 换成带读写锁的切片 (并发 Add/Remove 更安全)
package net

import (
	"sync"

	"rwhps/internal/game/manage"
	"rwhps/internal/net/core"
)

// NetStatic 对应 NetStaticData 单例的类型
type NetStatic struct {
	// GroupNet 对应 NetStaticData.groupNet
	GroupNet *GroupNet

	// BlackList 对应 NetStaticData.blackList
	BlackList *BlackList

	mu       sync.RWMutex
	services []*NetService

	rwHpsOnce sync.Once
	rwHps     core.IRwHps
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

// RwHps 对应 NetStaticData.RwHps (by lazy)
//
// 原版: IRwHpsManage.firstServer ?: FakeRwHps(classLoader, NullProtocol)
// 结果缓存一次; 若首次调用时还没有任何服务端协议注册, 后续注册也不会再刷新 —— 与原版 lazy 语义一致.
func (n *NetStatic) RwHps() core.IRwHps {
	n.rwHpsOnce.Do(func() {
		if first := manage.FirstServer(); first != nil {
			n.rwHps = first
			return
		}
		n.rwHps = core.NewNullRwHps(core.NullProtocol)
	})
	return n.rwHps
}

// ResetRwHps 清掉 RwHps 的惰性缓存 (测试与热重载用, 原版无对应)
func (n *NetStatic) ResetRwHps() {
	n.rwHpsOnce = sync.Once{}
	n.rwHps = nil
}

// CheckServerStartNet 对应 NetStaticData.checkServerStartNet
//
// 没有可用的服务端协议时返回 false 且不执行 run; 否则执行 run 并返回 true.
func CheckServerStartNet(run func()) bool {
	if _, ok := NetStaticData.RwHps().(*core.NullRwHps); ok {
		return false
	}
	if run != nil {
		run()
	}
	return true
}
