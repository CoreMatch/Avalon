// Package enums 对应 net.rwhps.server.game.enums
package enums

// GameUnitActions 对应 GameUnitActions 枚举 (单位行为模式 (开火策略))
type GameUnitActions int

const (
	// StanceOutOfRange 对应 outOfRange
	StanceOutOfRange GameUnitActions = iota
	// StanceOnlyInRange 对应 onlyInRange
	StanceOnlyInRange
	// StanceReturnFire 对应 returnFire
	StanceReturnFire
	// StanceHoldFire 对应 holdFire
	StanceHoldFire
	// StanceGuardArea 对应 guardArea
	StanceGuardArea
	// StanceAggressive 对应 aggressive
	StanceAggressive
	// StanceMixed 对应 mixed
	StanceMixed
	// StanceUNKNOWN 对应 UNKNOWN
	StanceUNKNOWN
)

// stanceNames 对应 Kotlin enum.name, 下标即 Ordinal
var stanceNames = [...]string{
	"outOfRange",
	"onlyInRange",
	"returnFire",
	"holdFire",
	"guardArea",
	"aggressive",
	"mixed",
	"UNKNOWN",
}

// Name 对应 Kotlin enum.name
func (v GameUnitActions) Name() string { return enumName(stanceNames[:], int(v)) }

// Ordinal 对应 Kotlin enum.ordinal
func (v GameUnitActions) Ordinal() int { return int(v) }

// String 实现 fmt.Stringer
func (v GameUnitActions) String() string { return v.Name() }

// StanceAll 按 Ordinal 顺序返回全部枚举值
func StanceAll() []GameUnitActions {
	out := make([]GameUnitActions, 0, len(stanceNames))
	for i := range stanceNames {
		out = append(out, GameUnitActions(i))
	}
	return out
}

// StanceFromName 对应 GameUnitActions.from(name), 大小写不敏感, 未找到返回 UNKNOWN
func StanceFromName(name string) GameUnitActions {
	return GameUnitActions(lookupEnumIndex(stanceNames[:], name, int(StanceUNKNOWN)))
}

// StanceFromIndex 对应 GameUnitActions.from(index), 越界返回 UNKNOWN
func StanceFromIndex(index int) GameUnitActions {
	return GameUnitActions(lookupEnumIndexByIndex(len(stanceNames), index, int(StanceUNKNOWN)))
}
