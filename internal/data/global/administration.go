// Package global 对应 net.rwhps.server.data.global
package global

import (
	"strings"
	"sync"

	"rwhps/internal/game/player"
	"rwhps/internal/struct/list"
	"rwhps/internal/util/timeutil"
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

	// PlayerDataCache 对应 playerDataCache: connectHexID -> 玩家离線档案
	// (踢出到期时间 / 禁言到期时间等), 玩家离开时写入, 加入时恢复
	PlayerDataCache map[string]*PlayerInfo

	// chatFilters 对应 chatFilters
	chatFilters []ChatFilter
}

// PlayerInfo 对应 Administration.PlayerInfo
type PlayerInfo struct {
	UUID        string
	TimesKicked int64
	TimesJoined int64
	TimeMute    int64
	Admin       bool
	SuperAdmin  bool
}

// NewPlayerInfo 对应 PlayerInfo(uuid, timesKicked, timeMute) 构造器
func NewPlayerInfo(uuid string, timesKicked, timeMute int64) *PlayerInfo {
	return &PlayerInfo{UUID: uuid, TimesKicked: timesKicked, TimeMute: timeMute}
}

// ChatFilter 对应 Administration.ChatFilter
//
// 上游签名: filter(player, message): String?, 返回 null 表示丢弃该消息.
// Go 版以 (过滤后的消息, 是否放行) 表达.
type ChatFilter interface {
	Filter(p *player.Player, message string) (string, bool)
}

// ChatFilterFunc 对应以匿名对象实现的 ChatFilter
type ChatFilterFunc func(p *player.Player, message string) (string, bool)

// Filter 实现 ChatFilter
func (f ChatFilterFunc) Filter(p *player.Player, message string) (string, bool) {
	return f(p, message)
}

// NewAdministration 对应 Administration(pluginData)
func NewAdministration() *Administration {
	a := &Administration{
		BannedIPs:       list.NewSeq[string](),
		BannedIP24:      list.NewSeq[string](),
		BannedUUIDs:     list.NewSeq[string](),
		Whitelist:       list.NewSeq[string](),
		BanQQ:           list.NewSeq[int64](),
		playerAdminData: map[string]*PlayerAdminInfo{},
		PlayerDataCache: map[string]*PlayerInfo{},
	}

	// 上游 Administration.init 里注册的默认过滤器:
	// 非管理员 30 秒内不允许发送两条相同消息
	a.AddChatFilter(ChatFilterFunc(func(p *player.Player, message string) (string, bool) {
		if !p.IsAdmin {
			if message == p.LastSentMessage && timeutil.GetTimeSinceMillis(p.LastMessageTime) < 1000*30 {
				_ = p.SendSystemMessage("You may not send the same message twice.")
				return "", false
			}
			p.LastSentMessage = message
			p.LastMessageTime = timeutil.ConcurrentMillis()
		}
		return message, true
	}))
	return a
}

// AddChatFilter 对应 addChatFilter
func (a *Administration) AddChatFilter(filter ChatFilter) {
	if filter == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.chatFilters = append(a.chatFilters, filter)
}

// FilterMessage 对应 filterMessage(player, message)
//
// 上游逐过滤器执行, 任一返回 null 即整体返回 null (消息被丢弃).
// 注意上游把"原始 message"传给每个过滤器而不是上一步的结果, 照搬该行为.
func (a *Administration) FilterMessage(p *player.Player, message string) (string, bool) {
	a.mu.RLock()
	filters := make([]ChatFilter, len(a.chatFilters))
	copy(filters, a.chatFilters)
	a.mu.RUnlock()

	current := message
	for _, f := range filters {
		var ok bool
		current, ok = f.Filter(p, message)
		if !ok {
			return "", false
		}
	}
	return current, true
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

// UnBanUUID 从 UUID 黑名单移除.
//
// 有意修正（作者已批准，2026-11-04）: 上游没有单条解封接口 (只有 clearbanall
// 全清), 为支撑 unban 命令的解封语义 (DECISIONS D-10) 新增.
func (a *Administration) UnBanUUID(uuid string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.BannedUUIDs.Remove(uuid, strings.EqualFold)
}

// UnBanIP 从 IP 黑名单移除.
//
// 有意修正（作者已批准，2026-11-04）: 同 UnBanUUID, 为 unban 解封语义新增.
func (a *Administration) UnBanIP(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.BannedIPs.Remove(ip, strings.EqualFold)
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
