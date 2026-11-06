// Package player 对应 net.rwhps.server.game.player
//
// 原版这个包下有 AiPlayer / PlayerHess / PlayerRelay 与 unofficial 子包.
// 本包目前只移植 PlayerRelay: 它是 Relay 依赖图里的叶子类, 而 PlayerHess 依赖
// 尚未移植的游戏内核 (见 internal/game/seam.go), AiPlayer 又依赖 PlayerHess.
package player

import (
	"fmt"

	"rwhps/internal/data/totalizer"
	"rwhps/internal/util/timeutil"
)

// PlayerRelay 对应 PlayerRelay
//
// 原注释: "RELAY Detailed stats for players inside"
//
// 一个纯粹的统计数据持有者: 没有网络操作, 没有游戏逻辑, 全部字段都是可变的
// 值对象. 原版 85 行里唯一的"行为"是三个属性 setter 的副作用.
type PlayerRelay struct {
	// Con 对应 con: GameVersionRelay
	//
	// 刻意用 any 而不是具体类型, 原因是导入环:
	//
	//	game/player -> netconnectprotocol/realize (GameVersionRelay)
	//	                        -> game/room (RelayRoom)
	//	                        -> game/player (PlayerRelay)   <- 环
	//
	// Kotlin 跑在同一个 JVM 上不在乎这个环, Go 必须在编译期断开. 项目里已有同样
	// 的处理先例: netpacket.AbstractNetPacket.GetPingPacket(player any) 也是为了
	// 绕开尚未移植的 PlayerHess.
	//
	// 更重要的一点: 原版全代码库里 con 只被赋值 (GameVersionRelay.kt:522 的
	// playerRelay!!.con = this), 从未有人读取它或调用其方法. 所以这里不需要
	// 先定义一个 seam 接口 —— 空方法集的接口等价于 any, 凭空造一个反而是噪音.
	// 等 RelayRoom / GameVersionRelay 移植过来、真的需要调用时再收紧类型.
	Con any

	// UUID 对应 uuid (构造参数, val)
	UUID string

	// Name 对应 name (构造参数, var)
	Name string

	// NowName 对应 nowName
	NowName string

	// Team 对应 team
	//
	// 注意: 设值必须走 SetTeam, 直接给字段赋值会丢掉 watch 副作用.
	Team int

	// Site 对应 site: List position
	Site int

	// Watch 对应 watch
	//
	// Kotlin 是 `private set` (只有类内能改), 由 SetTeam 维护.
	Watch bool

	// Mute 对应 mute: Mute expiration time (Unix 秒)
	Mute int

	// LastSentMessage 对应 lastSentMessage
	//
	// 设值必须走 SetLastSentMessage, 直接赋值会丢掉 lastMessageTime 副作用.
	LastSentMessage string

	// LastMessageTime 对应 lastMessageTime, Kotlin private set, 由 SetLastSentMessage 维护
	LastMessageTime int

	// LastMessageCount 对应 lastMessageCount
	LastMessageCount *totalizer.TimeAndNumber

	// LastShortMessageCount 对应 lastShortMessageCount
	LastShortMessageCount *totalizer.TimeAndNumber

	// PlayerSyncCount 对应 playerSyncCount
	PlayerSyncCount *totalizer.TimeAndNumber

	// Disconnect 对应 disconnect
	//
	// 设值必须走 SetDisconnect, 直接赋值会丢掉 disconnectTime 副作用.
	Disconnect bool

	// DisconnectTime 对应 disconnectTime, Kotlin private set, 由 SetDisconnect 维护
	DisconnectTime int
}

// NewPlayerRelay 对应 PlayerRelay(conIn, uuid, name)
func NewPlayerRelay(con any, uuid, name string) *PlayerRelay {
	return &PlayerRelay{
		Con:                   con,
		UUID:                  uuid,
		Name:                  name,
		LastMessageCount:      totalizer.NewTimeAndNumber(10, 3),
		LastShortMessageCount: totalizer.NewTimeAndNumber(10, 5),
		PlayerSyncCount:       totalizer.NewTimeAndNumber(60, 3),
	}
}

// SetTeam 对应 team 的 setter
//
// 原版:
//
//	var team = 0
//	    set(value) {
//	        watch = (value == -3)
//	        field = site
//	    }
//
// 最后一行是 field = site 而不是 field = value. 这不是笔误的复刻猜测, 而是原版
// 实实在在的行为: 给 team 赋值**不会**改变 team, 只会顺带更新 watch, 而 team
// 始终等于 site.
//
// 复刻时最容易犯的错就是"顺手修正"成 field = value. 那会让 team 真的存下调用方
// 给的值, 与 site 脱钩 —— 而 getPacketTeamData 里读回 team 的下游会看到完全
// 不同的数据. 修不修原版的 bug 是另一个决定, 不该藏在一次机械复刻里.
func (p *PlayerRelay) SetTeam(value int) {
	p.Watch = value == -3
	p.Team = p.Site
}

// SetLastSentMessage 对应 lastSentMessage 的 setter
//
// 原版在写入值的同时刷新 lastMessageTime, 因此直接给字段赋值会丢掉这个副作用.
func (p *PlayerRelay) SetLastSentMessage(value string) {
	p.LastSentMessage = value
	p.LastMessageTime = timeutil.ConcurrentSecond()
}

// SetDisconnect 对应 disconnect 的 setter
func (p *PlayerRelay) SetDisconnect(value bool) {
	p.Disconnect = value
	p.DisconnectTime = timeutil.ConcurrentSecond()
}

// String 对应 toString()
//
// 原版用 Kotlin 的 trimIndent() 多行字符串, Go 侧用等价的手工换行.
func (p *PlayerRelay) String() string {
	return fmt.Sprintf("Player :\nName : %s\nuuid-Hash : %s\n\nPosition : %d\nTeam : %d\n",
		p.Name, p.UUID, p.Site, p.Team)
}
