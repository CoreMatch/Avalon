// Package global 对应 net.rwhps.server.data.global.Cache
package global

import (
	"sync"

	"rwhps/internal/io/packet"
	"rwhps/internal/net/core"
)

// packetCacheEntry 显式带创建回调的版本 (对应 ObjectMap.get(key, createFn))
type packetCacheEntry struct {
	packet *packet.Packet
	fn     func() *packet.Packet
}

// PacketCache 对应 Cache.packetCache
// 按 key 缓存 Packet, 首次调用 GetOrCreate 时通过 fn 创建并缓存
type PacketCache struct {
	mu    sync.RWMutex
	cache map[string]*packetCacheEntry
}

// NewPacketCache 创建空的 PacketCache
func NewPacketCache(initial int) *PacketCache {
	return &PacketCache{cache: make(map[string]*packetCacheEntry, initial)}
}

// Put 直接写入, 覆盖已有值
func (p *PacketCache) Put(key string, pkt *packet.Packet) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cache[key] = &packetCacheEntry{packet: pkt}
}

// GetOrCreate 缓存命中则返回 cached, 否则调用 fn 创建并缓存
func (p *PacketCache) GetOrCreate(key string, fn func() *packet.Packet) *packet.Packet {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.cache[key]; ok {
		if e.packet != nil {
			return e.packet
		}
		if e.fn != nil {
			// 与 Kotlin 语义一致: 在 mutex 下计算 fn
			res := e.fn()
			e.packet = res
			e.fn = nil
			return res
		}
	}
	res := fn()
	p.cache[key] = &packetCacheEntry{packet: res}
	return res
}

// 全局默认实例 (对应 object Cache)
var (
	PacketCacheInstance = NewPacketCache(8)
)

// 编译期引用 core, 防止以后回退时缺依赖
var _ = core.NewChannel