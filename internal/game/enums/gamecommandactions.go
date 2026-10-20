// Package enums 对应 net.rwhps.server.game.enums
package enums

// GameCommandActions 对应 GameCommandActions 枚举
type GameCommandActions int

const (
	ActionMove GameCommandActions = iota
	ActionAttack
	ActionBuild
	ActionRepair
	ActionStop
	ActionDelete
	ActionPause
	ActionResume
	ActionSetGroup
	ActionJump
	ActionSelect
	ActionSelectType
	ActionSetTarget
	ActionMilitia
	ActionHelp
	ActionGuard
	ActionReclaim
	ActionUNKNOWN
)

// Name 对应 Kotlin enum.name
func (a GameCommandActions) Name() string {
	switch a {
	case ActionMove:
		return "MOVE"
	case ActionAttack:
		return "ATTACK"
	case ActionBuild:
		return "BUILD"
	case ActionRepair:
		return "REPAIR"
	case ActionStop:
		return "STOP"
	case ActionDelete:
		return "DELETE"
	case ActionPause:
		return "PAUSE"
	case ActionResume:
		return "RESUME"
	case ActionSetGroup:
		return "SET_GROUP"
	case ActionJump:
		return "JUMP"
	case ActionSelect:
		return "SELECT"
	case ActionSelectType:
		return "SELECT_TYPE"
	case ActionSetTarget:
		return "SET_TARGET"
	case ActionMilitia:
		return "MILITIA"
	case ActionHelp:
		return "HELP"
	case ActionGuard:
		return "GUARD"
	case ActionReclaim:
		return "RECLAIM"
	case ActionUNKNOWN:
		return "UNKNOWN"
	}
	return ""
}

// Ordinal 对应 Kotlin enum.ordinal
func (a GameCommandActions) Ordinal() int { return int(a) }