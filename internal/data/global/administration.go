// Package global 对应 net.rwhps.server.data.global
package global

import (
	"strings"
	"sync"

	"rwhps/internal/struct/list"
)

// PlayerAdminInfo 对应 Administration.PlayerAdminInfo
type PlayerAdminInfo struct {
	UUID       string
	Name       string
	Admin      bool
	SuperAdmin bool
}

// Administration 对应 net.rwhps.server.net.Administration.
//
// 原版把 bannedIPs/bannedIP24/bannedUUIDs/whitelist/banQQ/playerAdminData 以
// PluginData 的 tag 5 (JVM 序列化对象) 存进 Settings.bin. Go 侧无法解码这种格式,
// 因此本批只在内存里维护这些集合, 并且不覆写 Settings.bin 中已有的 tag 5 数据;
// 持久化留到实现 JVM 序列化格式之后再补.
type Administration struct {
	mu sync.RWMutex

	BannedIPs   *list.Seq[string]
	BannedIP24  *list.Seq[string]
	BannedUUIDs *list.Seq[string]
	Whitelist   *list.Seq[string]
	BanQQ       *list.Seq[int64]

	playerAdminData map[string]*PlayerAdminInfo
}

// NewAdministration 对应 Administration(pluginData)
func NewAdministration() *Administration {
	return &Administration{
		BannedIPs:       list.NewSeq[string](),
		BannedIP24:      list.NewSeq[string](),
		BannedUUIDs:     list.NewSeq[string](),
		Whitelist:       list.NewSeq[string](),
		BanQQ:           list.NewSeq[int64](),
		playerAdminData: map[string]*PlayerAdminInfo{},
	}
}

// AddAdmin 对应 addAdmin(uuid, supAdmin)
func (a *Administration) AddAdmin(uuid, name string, superAdmin bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.playerAdminData[uuid] = &PlayerAdminInfo{UUID: uuid, Name: name, Admin: true, SuperAdmin: superAdmin}
}

// RemoveAdmin 对应 removeAdmin(uuid)
func (a *Administration) RemoveAdmin(uuid string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.playerAdminData, uuid)
}

// IsAdmin 对应 isAdmin(player); 命中时回写玩家名
func (a *Administration) IsAdmin(uuid, name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	info, ok := a.playerAdminData[uuid]
	if !ok {
		return false
	}
	info.Name = name
	return true
}

// AdminData 对应 playerAdminData[uuid] 读取
//
// 返回副本避免调用方绕过锁修改; 不存在时 ok 为 false.
func (a *Administration) AdminData(uuid string) (PlayerAdminInfo, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	info, ok := a.playerAdminData[uuid]
	if !ok {
		return PlayerAdminInfo{}, false
	}
	return *info, true
}

// Admins 返回全部管理员快照
func (a *Administration) Admins() []PlayerAdminInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]PlayerAdminInfo, 0, len(a.playerAdminData))
	for _, v := range a.playerAdminData {
		out = append(out, *v)
	}
	return out
}

// BanIP 加入 IP 黑名单
func (a *Administration) BanIP(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !containsString(a.BannedIPs, ip) {
		a.BannedIPs.Add(ip)
	}
}

// BanIP24 加入 IP 段黑名单 (/24)
func (a *Administration) BanIP24(ip24 string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !containsString(a.BannedIP24, ip24) {
		a.BannedIP24.Add(ip24)
	}
}

// IsIP24Banned 判断 IP 段是否被封
func (a *Administration) IsIP24Banned(ip24 string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return containsString(a.BannedIP24, ip24)
}

// BanUUID 加入 UUID 黑名单
func (a *Administration) BanUUID(uuid string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !containsString(a.BannedUUIDs, uuid) {
		a.BannedUUIDs.Add(uuid)
	}
}

// IsUUIDBanned 判断 UUID 是否被封
func (a *Administration) IsUUIDBanned(uuid string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return containsString(a.BannedUUIDs, uuid)
}

// ClearBanAll 对应 clearbanall
func (a *Administration) ClearBanAll() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.BannedIPs = list.NewSeq[string]()
	a.BannedIP24 = list.NewSeq[string]()
	a.BannedUUIDs = list.NewSeq[string]()
}

// containsString 在 Seq 里查找 (忽略大小写, 对应原版 String 比较习惯)
func containsString(seq *list.Seq[string], v string) bool {
	for i := 0; i < seq.Size(); i++ {
		if strings.EqualFold(seq.Get(i), v) {
			return true
		}
	}
	return false
}
