package enums

import "testing"

// TestInternalUnitsTable 校验表长度与关键序号, 与 Kotlin GameInternalUnits 逐项对应
func TestInternalUnitsTable(t *testing.T) {
	if got := len(UnitAll()); got != 52 {
		t.Fatalf("GameInternalUnits len = %d, want 52", got)
	}
	cases := []struct {
		v    GameInternalUnits
		idx  int
		name string
	}{
		{UnitExtractor, 0, "extractor"},
		{UnitCommandCenter, 4, "commandCenter"},
		{UnitTank, 8, "tank"},
		{UnitNukeLaucher, 28, "NukeLaucher"}, // 原版拼写就是 Laucher
		{UnitAntiNukeLaucher, 29, "AntiNukeLaucher"},
		{UnitWallV, 34, "wall_v"},
		{UnitTurretArtillery, 41, "turret_artillery"},
		{UnitEditorOrBuilder, 50, "editorOrBuilder"},
		{UnitUNKNOWN, 51, "UNKNOWN"},
	}
	for _, c := range cases {
		if c.v.Ordinal() != c.idx {
			t.Errorf("%s ordinal = %d, want %d", c.name, c.v.Ordinal(), c.idx)
		}
		if c.v.Name() != c.name {
			t.Errorf("ordinal %d name = %q, want %q", c.idx, c.v.Name(), c.name)
		}
	}
}

// TestInternalUnitsLookup 校验 from(name)/from(index)
func TestInternalUnitsLookup(t *testing.T) {
	if got := UnitFromName("TANK"); got != UnitTank {
		t.Errorf("UnitFromName(TANK) = %v, want tank", got)
	}
	if got := UnitFromName("nope"); got != UnitUNKNOWN {
		t.Errorf("UnitFromName(nope) = %v, want UNKNOWN", got)
	}
	if got := UnitFromIndex(8); got != UnitTank {
		t.Errorf("UnitFromIndex(8) = %v, want tank", got)
	}
	// 原版越界会抛异常, Go 版按文档返回 UNKNOWN
	if got := UnitFromIndex(999); got != UnitUNKNOWN {
		t.Errorf("UnitFromIndex(999) = %v, want UNKNOWN", got)
	}
	if got := UnitFromIndex(-1); got != UnitUNKNOWN {
		t.Errorf("UnitFromIndex(-1) = %v, want UNKNOWN", got)
	}
}

// TestCommandActionsTable 校验动作序号 (协议里直接用 ordinal)
func TestCommandActionsTable(t *testing.T) {
	cases := []struct {
		v    GameCommandActions
		idx  int
		name string
	}{
		{ActionMove, 0, "MOVE"},
		{ActionBuild, 2, "BUILD"},
		{ActionReclaim, 6, "RECLAIM"},
		{ActionAttackMove, 7, "ATTACKMOVE"},
		{ActionGuardAt, 11, "GUARDAT"},
		{ActionTriggerActionWhenInRange, 15, "TRIGGERACTIONWHENINRANGE"},
		{ActionSetPassiveTarget, 16, "SETPASSIVETARGET"},
		{ActionUNKNOWN, 17, "UNKNOWN"},
	}
	for _, c := range cases {
		if c.v.Ordinal() != c.idx || c.v.Name() != c.name {
			t.Errorf("action %s = (%d,%q), want (%d,%q)", c.name, c.v.Ordinal(), c.v.Name(), c.idx, c.name)
		}
	}
	if got := len(ActionAll()); got != 18 {
		t.Fatalf("GameCommandActions len = %d, want 18", got)
	}
	if got := ActionFromName("attackmove"); got != ActionAttackMove {
		t.Errorf("ActionFromName(attackmove) = %v", got)
	}
}

// TestRemainingEnumSizes 其余三张表的长度与首尾项
func TestRemainingEnumSizes(t *testing.T) {
	if got := len(ExtAll()); got != 93 {
		t.Errorf("GameExternalUnits len = %d, want 93", got)
	}
	if ExtAaBeamGunship.Name() != "aaBeamGunship" || ExtUNKNOWN.Name() != "UNKNOWN" {
		t.Errorf("external units head/tail wrong: %q/%q", ExtAaBeamGunship.Name(), ExtUNKNOWN.Name())
	}
	if got := len(FactoryAll()); got != 65 {
		t.Errorf("GameFactoryBuildUnits len = %d, want 65", got)
	}
	if Factory0.Name() != "_0" || Factory1.Name() != "_1" {
		t.Errorf("factory nuke entries wrong: %q/%q", Factory0.Name(), Factory1.Name())
	}
	if got := len(PingAll()); got != 12 {
		t.Errorf("GamePingActions len = %d, want 12", got)
	}
	if PingNormal.Name() != "NORMAL" || PingRetreat.Name() != "RETREAT" {
		t.Errorf("ping actions wrong: %q/%q", PingNormal.Name(), PingRetreat.Name())
	}
	if got := len(StanceAll()); got != 8 {
		t.Errorf("GameUnitActions len = %d, want 8", got)
	}
	if StanceOutOfRange.Name() != "outOfRange" || StanceAggressive.Name() != "aggressive" {
		t.Errorf("unit actions wrong: %q/%q", StanceOutOfRange.Name(), StanceAggressive.Name())
	}
}
