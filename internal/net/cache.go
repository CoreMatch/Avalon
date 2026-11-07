// Package net 对应 上游 net
//
// 包含:
//   - NetService (TCP/RUDP 启动入口)
//   - StartMixProtocol (Game/RCON/HTTP 单端口嗅探)
//   - StartGameNetTcp / StartHttp / StartRemoteControl (协议启动器)
//   - Cache (protocolProviderCache 占位)
package net

import (
	"sync"

	"github.com/CoreMatch/Avalon/internal/net/core"
)

// Cache 对应 上游 data.global.Cache
// Go 版只保留 IProtocolProvider 注册表, 用于 mixProtocol 复用
type Cache struct {
	mu                    sync.RWMutex
	protocolProviderCache map[string]core.IProtocolProvider
}

// NewCache 创建 Cache
func NewCache() *Cache {
	return &Cache{protocolProviderCache: make(map[string]core.IProtocolProvider)}
}

// GetOrCreate 对应 ObjectMap[key, { ... }] 原 Kotlin 双参数行为:
//
//	命中返回 cache 值; 未命中调用 loader 生成并存入.
func (c *Cache) GetOrCreate(key string, loader func() core.IProtocolProvider) core.IProtocolProvider {
	c.mu.RLock()
	if v, ok := c.protocolProviderCache[key]; ok {
		c.mu.RUnlock()
		return v
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.protocolProviderCache[key]; ok {
		return v
	}
	v := loader()
	c.protocolProviderCache[key] = v
	return v
}

// Put 显式写入 (供 ProtocolManage 注册默认实现)
func (c *Cache) Put(key string, v core.IProtocolProvider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.protocolProviderCache[key] = v
}

// GlobalCache 全局 IProtocolProvider 缓存 (用于 mixProtocol)
var GlobalCache = NewCache()
