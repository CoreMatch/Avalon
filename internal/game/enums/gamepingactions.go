// Package enums 对应 net.rwhps.server.game.enums
package enums

// GamePingActions 对应 GamePingActions 枚举 (地图 Ping 动作)
type GamePingActions int

const (
	// PingNormal 对应 NORMAL
	PingNormal GamePingActions = iota
	// PingAttack 对应 ATTACK
	PingAttack
	// PingDefend 对应 DEFEND
	PingDefend
	// PingNuke 对应 NUKE
	PingNuke
	// PingBuild 对应 BUILD
	PingBuild
	// PingUpgrade 对应 UPGRADE
	PingUpgrade
	// PingOk 对应 OK
	PingOk
	// PingNo 对应 NO
	PingNo
	// PingHappy 对应 HAPPY
	PingHappy
	// PingSad 对应 SAD
	PingSad
	// PingRetreat 对应 RETREAT
	PingRetreat
	// PingUNKNOWN 对应 UNKNOWN
	PingUNKNOWN
)

// pingNames 对应 Kotlin enum.name, 下标即 Ordinal
var pingNames = [...]string{
	"NORMAL",
	"ATTACK",
	"DEFEND",
	"NUKE",
	"BUILD",
	"UPGRADE",
	"OK",
	"NO",
	"HAPPY",
	"SAD",
	"RETREAT",
	"UNKNOWN",
}

// Name 对应 Kotlin enum.name
func (v GamePingActions) Name() string { return enumName(pingNames[:], int(v)) }

// Ordinal 对应 Kotlin enum.ordinal
func (v GamePingActions) Ordinal() int { return int(v) }

// String 实现 fmt.Stringer
func (v GamePingActions) String() string { return v.Name() }

// PingAll 按 Ordinal 顺序返回全部枚举值
func PingAll() []GamePingActions {
	out := make([]GamePingActions, 0, len(pingNames))
	for i := range pingNames {
		out = append(out, GamePingActions(i))
	}
	return out
}

// PingFromName 对应 GamePingActions.from(name), 大小写不敏感, 未找到返回 UNKNOWN
func PingFromName(name string) GamePingActions {
	return GamePingActions(lookupEnumIndex(pingNames[:], name, int(PingUNKNOWN)))
}

// PingFromIndex 对应 GamePingActions.from(index), 越界返回 UNKNOWN
func PingFromIndex(index int) GamePingActions {
	return GamePingActions(lookupEnumIndexByIndex(len(pingNames), index, int(PingUNKNOWN)))
}
