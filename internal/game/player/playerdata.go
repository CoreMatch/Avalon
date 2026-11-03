// Package player 对应 net.rwhps.server.game.player.
//
// 上游 PlayerHess 的私有数据来自 AbstractLinkPlayerData —— 一个指向 JVM 内
// 游戏引擎的"活链接", 字段实时反映引擎状态. Go 版引擎外置为 helper,
// 链接变成镜像: Data 持有同一组字段, 由 helper 事件刷新 (协议见
// game/external 的 PlayerProfile), 命令层读取.
//
// 权威性约定: 游戏内数据 (名字/队伍/战绩) 以 helper 为准, Go 侧只镜像;
// 服务端管理数据 (admin/禁言/踢出) 以 Go 为准, 不经 helper.
package player

import "sync"

// Data 对应 AbstractLinkPlayerData: 引擎侧持有权威的玩家数据
//
// 与上游的差异: 上游是接口 (每次读都穿透到引擎), Go 版是带锁的镜像结构.
// 字段集与上游接口逐一对应; 其中战绩类字段 (unitsKilled 等) 当前的
// helper 协议尚未携带, 保持零值, 待协议扩展后由事件刷新 (DECISIONS D-9).
type Data struct {
	mu sync.RWMutex

	// Name 对应 name
	Name string
	// ConnectHexID 对应 connectHexID: 玩家在引擎内的唯一标识
	ConnectHexID string

	// Index 对应 index: 引擎内玩家槽位 (0 起)
	Index int
	// Team 对应 team
	Team int
	// Credits 对应 credits
	Credits int
	// StartUnit 对应 startUnit
	StartUnit int
	// Color 对应 color
	Color int
	// AiDifficulty 对应 aiDifficulty
	AiDifficulty int

	// Ready/Host/Bot/Spectator 来自 helper 的 PlayerProfile
	// (引擎侧的玩家档案, 上游无直接对应字段, 属 Go 架构新增镜像面)
	Ready     bool
	Host      bool
	Bot       bool
	Spectator bool

	// Survive 对应 survive: 玩家是否存活
	Survive bool

	// 战绩字段, 对应 unitsKilled / buildingsKilled / experimentalsKilled /
	// unitsLost / buildingsLost / experimentalsLost
	UnitsKilled         int
	BuildingsKilled     int
	ExperimentalsKilled int
	UnitsLost           int
	BuildingsLost       int
	ExperimentalsLost   int
}

// NewData 构造空镜像
func NewData() *Data { return &Data{} }

// UpdateDate 对应 updateDate
//
// 上游: 从引擎重新拉取一遍数据 (roomStartGame 前调用). Go 版镜像由
// helper 事件推送刷新, 开局前没有可主动拉取的通道, 因此是空操作 ——
// 保留方法是为了让调用点与上游一一对应.
func (d *Data) UpdateDate() {}

// RemovePlayer 对应 removePlayer
//
// 上游: 让引擎把这个玩家从对局里移除 (AI 被踢时调用). Go 版引擎在
// helper 内, 移除动作需要协议操作支持, 当前协议未定义 —— 见 DECISIONS D-9,
// 此处先留空操作, 不假装移除了引擎侧的玩家.
func (d *Data) RemovePlayer() {}

// Apply 在持锁状态下执行一组字段更新 (事件线程刷新镜像用)
func (d *Data) Apply(fn func(*Data)) {
	d.mu.Lock()
	fn(d)
	d.mu.Unlock()
}

// --- 带锁访问器 (战绩/档案类字段由事件线程写, 命令线程读) ---

// GetName 对应 name
func (d *Data) GetName() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.Name
}

// GetConnectHexID 对应 connectHexID
func (d *Data) GetConnectHexID() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.ConnectHexID
}

// GetIndex 对应 index
func (d *Data) GetIndex() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.Index
}

// SetIndex 对应 index (可写)
func (d *Data) SetIndex(v int) {
	d.mu.Lock()
	d.Index = v
	d.mu.Unlock()
}

// GetTeam 对应 team
func (d *Data) GetTeam() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.Team
}

// SetTeam 对应 team (可写)
func (d *Data) SetTeam(v int) {
	d.mu.Lock()
	d.Team = v
	d.mu.Unlock()
}

// GetCredits 对应 credits
func (d *Data) GetCredits() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.Credits
}

// SetCredits 对应 credits (可写)
func (d *Data) SetCredits(v int) {
	d.mu.Lock()
	d.Credits = v
	d.mu.Unlock()
}

// GetStartUnit 对应 startUnit
func (d *Data) GetStartUnit() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.StartUnit
}

// SetStartUnit 对应 startUnit (可写)
func (d *Data) SetStartUnit(v int) {
	d.mu.Lock()
	d.StartUnit = v
	d.mu.Unlock()
}

// GetAiDifficulty 对应 aiDifficulty
func (d *Data) GetAiDifficulty() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.AiDifficulty
}

// SetAiDifficulty 对应 aiDifficulty (可写)
func (d *Data) SetAiDifficulty(v int) {
	d.mu.Lock()
	d.AiDifficulty = v
	d.mu.Unlock()
}

// GetSurvive 对应 survive
func (d *Data) GetSurvive() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.Survive
}

// Stats 对应 statusData 读取的六个战绩字段
type Stats struct {
	UnitsKilled         int
	BuildingsKilled     int
	ExperimentalsKilled int
	UnitsLost           int
	BuildingsLost       int
	ExperimentalsLost   int
}

// GetStats 取战绩快照
func (d *Data) GetStats() Stats {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return Stats{
		UnitsKilled:         d.UnitsKilled,
		BuildingsKilled:     d.BuildingsKilled,
		ExperimentalsKilled: d.ExperimentalsKilled,
		UnitsLost:           d.UnitsLost,
		BuildingsLost:       d.BuildingsLost,
		ExperimentalsLost:   d.ExperimentalsLost,
	}
}
