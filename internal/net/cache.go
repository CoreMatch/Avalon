// Package net (Go-Server) 对应 net.rwhps.server.net
//
// 包含:
//   - NetService (TCP/RUDP 启动入口)
//   - StartMixProtocol (Game/RCON/HTTP 单端口嗅探)
//   - StartGameNetTcp / StartHttp / StartRemoteControl (协议启动器)
//   - Cache (iRwHpsCache 占位)
package net

import (
	"sync"

	"rwhps/internal/net/core"
)

// Cache 对应 net.rwhps.server.data.global.Cache
// Go 版只保留 IRwHps 注册表, 用于 mixProtocol 复用
type Cache struct {
	mu          sync.RWMutex
	iRwHpsCache map[string]core.IRwHps
}

// NewCache 创建 Cache
func NewCache() *Cache {
	return &Cache{iRwHpsCache: make(map[string]core.IRwHps)}
}

// GetOrCreate 对应 ObjectMap[key, { ... }] 原 Kotlin 双参数行为:
//   命中返回 cache 值; 未命中调用 loader 生成并存入.
func (c *Cache) GetOrCreate(key string, loader func() core.IRwHps) core.IRwHps {
	c.mu.RLock()
	if v, ok := c.iRwHpsCache[key]; ok {
		c.mu.RUnlock()
		return v
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.iRwHpsCache[key]; ok {
		return v
	}
	v := loader()
	c.iRwHpsCache[key] = v
	return v
}

// Put 显式写入 (供 IRwHpsManage 注册默认实现)
func (c *Cache) Put(key string, v core.IRwHps) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.iRwHpsCache[key] = v
}

// GlobalCache 全局 IRwHps 缓存 (用于 mixProtocol)
var GlobalCache = NewCache()
