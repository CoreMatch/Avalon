// Package enums 对应 net.rwhps.server.game.enums
package enums

// GameCommandActions 对应 GameCommandActions 枚举 (游戏命令动作, 序号即协议中的 action ordinal)
type GameCommandActions int

const (
	// ActionMove 对应 MOVE
	ActionMove GameCommandActions = iota
	// ActionAttack 对应 ATTACK
	ActionAttack
	// ActionBuild 对应 BUILD
	ActionBuild
	// ActionRepair 对应 REPAIR
	ActionRepair
	// ActionLoadInto 对应 LOADINTO
	ActionLoadInto
	// ActionUnloadAt 对应 UNLOADAT
	ActionUnloadAt
	// ActionReclaim 对应 RECLAIM
	ActionReclaim
	// ActionAttackMove 对应 ATTACKMOVE
	ActionAttackMove
	// ActionLoadUp 对应 LOADUP
	ActionLoadUp
	// ActionPatrol 对应 PATROL
	ActionPatrol
	// ActionGuard 对应 GUARD
	ActionGuard
	// ActionGuardAt 对应 GUARDAT
	ActionGuardAt
	// ActionTouchTarget 对应 TOUCHTARGET
	ActionTouchTarget
	// ActionFollow 对应 FOLLOW
	ActionFollow
	// ActionTriggerAction 对应 TRIGGERACTION
	ActionTriggerAction
	// ActionTriggerActionWhenInRange 对应 TRIGGERACTIONWHENINRANGE
	ActionTriggerActionWhenInRange
	// ActionSetPassiveTarget 对应 SETPASSIVETARGET
	ActionSetPassiveTarget
	// ActionUNKNOWN 对应 UNKNOWN
	ActionUNKNOWN
)

// actionNames 对应 Kotlin enum.name, 下标即 Ordinal
var actionNames = [...]string{
	"MOVE",
	"ATTACK",
	"BUILD",
	"REPAIR",
	"LOADINTO",
	"UNLOADAT",
	"RECLAIM",
	"ATTACKMOVE",
	"LOADUP",
	"PATROL",
	"GUARD",
	"GUARDAT",
	"TOUCHTARGET",
	"FOLLOW",
	"TRIGGERACTION",
	"TRIGGERACTIONWHENINRANGE",
	"SETPASSIVETARGET",
	"UNKNOWN",
}

// Name 对应 Kotlin enum.name
func (v GameCommandActions) Name() string { return enumName(actionNames[:], int(v)) }

// Ordinal 对应 Kotlin enum.ordinal
func (v GameCommandActions) Ordinal() int { return int(v) }

// String 实现 fmt.Stringer
func (v GameCommandActions) String() string { return v.Name() }

// ActionAll 按 Ordinal 顺序返回全部枚举值
func ActionAll() []GameCommandActions {
	out := make([]GameCommandActions, 0, len(actionNames))
	for i := range actionNames {
		out = append(out, GameCommandActions(i))
	}
	return out
}

// ActionFromName 对应 GameCommandActions.from(name), 大小写不敏感, 未找到返回 UNKNOWN
func ActionFromName(name string) GameCommandActions {
	return GameCommandActions(lookupEnumIndex(actionNames[:], name, int(ActionUNKNOWN)))
}

// ActionFromIndex 对应 GameCommandActions.from(index), 越界返回 UNKNOWN
func ActionFromIndex(index int) GameCommandActions {
	return GameCommandActions(lookupEnumIndexByIndex(len(actionNames), index, int(ActionUNKNOWN)))
}
