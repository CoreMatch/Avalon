package gameio

import (
	"testing"

	"rwhps/internal/game/enums"
)

// commandCfg 描述要组装的 GameCommandOnePacket 字段
type commandCfg struct {
	sendBy         byte
	withOperation  bool
	opAction       enums.GameCommandActions
	opUnitType     int32
	opUnitName     string
	opX, opY       float32
	opActionID     string
	n1, n2         bool
	nInt1          int32
	unitAction     enums.GameUnitActions
	withRally      bool
	rallyX, rallyY float32
	removeUnit     bool
	ops            []int64
	withNPlayer    bool
	withMapPoint   bool
	mapX, mapY     float32
	actionID       string
	n3             bool
	sharedControl  int16
	n4             bool
	stepRate, nf1  float32
	systemAction   int32
}

// buildCommandBytes 按 GameCommandOnePacket 的布局组装字节, 供解析测试使用
func buildCommandBytes(t *testing.T, cfg commandCfg) []byte {
	t.Helper()
	o := NewGameOutputStream()

	_ = o.WriteByte(cfg.sendBy)

	o.WriteBoolean(cfg.withOperation)
	if cfg.withOperation {
		o.WriteInt(int32(cfg.opAction))
		o.WriteInt(cfg.opUnitType)
		if cfg.opUnitType == -2 {
			o.WriteString(cfg.opUnitName)
		}
		o.WriteFloat(cfg.opX)
		o.WriteFloat(cfg.opY)
		o.WriteBytes(make([]byte, 20)) // 未知 20 字节
		o.WriteIsString(cfg.opActionID)
	}

	o.WriteBoolean(cfg.n1)
	o.WriteBoolean(cfg.n2)
	o.WriteInt(cfg.nInt1)
	o.WriteInt(int32(cfg.unitAction))

	o.WriteBoolean(cfg.withRally)
	if cfg.withRally {
		o.WriteFloat(cfg.rallyX)
		o.WriteFloat(cfg.rallyY)
	}

	o.WriteBoolean(cfg.removeUnit)
	o.WriteInt(int32(len(cfg.ops)))
	for _, v := range cfg.ops {
		o.WriteLong(v)
	}

	o.WriteBoolean(cfg.withNPlayer)
	if cfg.withNPlayer {
		_ = o.WriteByte(0)
	}

	o.WriteBoolean(cfg.withMapPoint)
	if cfg.withMapPoint {
		o.WriteFloat(cfg.mapX)
		o.WriteFloat(cfg.mapY)
	}

	o.WriteBytes(make([]byte, 8)) // 上游 skip(8)
	o.WriteString(cfg.actionID)
	o.WriteBoolean(cfg.n3)
	o.WriteShort(cfg.sharedControl)
	o.WriteBoolean(cfg.n4)
	if cfg.n4 {
		_ = o.WriteByte(0)
		o.WriteFloat(cfg.stepRate)
		o.WriteFloat(cfg.nf1)
		o.WriteInt(cfg.systemAction)
	}

	return o.GetByteArray()
}

// TestGameCommandOnePacketMinimal 无 operationUnit / mapPoint 的最小包
func TestGameCommandOnePacketMinimal(t *testing.T) {
	b := buildCommandBytes(t, commandCfg{sendBy: 3, actionID: "_idle"})

	c := NewGameCommandOnePacket(b)
	if c.SendBy != 3 {
		t.Fatalf("SendBy = %d, 期望 3", c.SendBy)
	}
	if c.OperationUnit != nil {
		t.Fatal("不应有 operationUnit")
	}
	if c.MapPoint != nil {
		t.Fatal("不应有 mapPoint")
	}
	if c.ActionID != "_idle" {
		t.Fatalf("ActionID = %q, 期望 _idle", c.ActionID)
	}
}

// TestGameCommandOnePacketOperationUnit 带 BUILD 操作块的包
func TestGameCommandOnePacketOperationUnit(t *testing.T) {
	b := buildCommandBytes(t, commandCfg{
		sendBy:        1,
		withOperation: true,
		opAction:      enums.ActionBuild,
		opUnitType:    -2,
		opUnitName:    "tank",
		opX:           10.5,
		opY:           20.25,
		opActionID:    "c_build",
		actionID:      "c_1_x",
	})

	c := NewGameCommandOnePacket(b)
	if c.OperationUnit == nil {
		t.Fatal("应有 operationUnit")
	}
	ou := c.OperationUnit
	if ou.GameCommandActions != enums.ActionBuild {
		t.Fatalf("action = %v, 期望 BUILD", ou.GameCommandActions)
	}
	if !ou.HasUnitName || ou.UnitName != "tank" {
		t.Fatalf("unitName = %q (%v), 期望 tank", ou.UnitName, ou.HasUnitName)
	}
	if ou.X != 10.5 || ou.Y != 20.25 {
		t.Fatalf("坐标 = (%v,%v), 期望 (10.5,20.25)", ou.X, ou.Y)
	}
	if ou.ActionID != "c_build" {
		t.Fatalf("op actionID = %q", ou.ActionID)
	}
	if c.ActionID != "c_1_x" {
		t.Fatalf("ActionID = %q", c.ActionID)
	}
}

// TestGameCommandOnePacketMapPoint 带 mapPoint / opsUnitList / NBoolean4 的包
func TestGameCommandOnePacketMapPoint(t *testing.T) {
	b := buildCommandBytes(t, commandCfg{
		sendBy:       2,
		ops:          []int64{111, 222},
		withMapPoint: true,
		mapX:         3.5,
		mapY:         4.5,
		actionID:     "c_6_ATTACK",
		n4:           true,
		stepRate:     1.5,
		nf1:          2.5,
		systemAction: 7,
	})

	c := NewGameCommandOnePacket(b)
	if len(c.OpsUnitList) != 2 || c.OpsUnitList[0] != 111 || c.OpsUnitList[1] != 222 {
		t.Fatalf("opsUnitList = %v", c.OpsUnitList)
	}
	if c.MapPoint == nil || c.MapPoint[0] != 3.5 || c.MapPoint[1] != 4.5 {
		t.Fatalf("mapPoint = %v", c.MapPoint)
	}
	if c.ActionID != "c_6_ATTACK" {
		t.Fatalf("ActionID = %q", c.ActionID)
	}
	if !c.NBoolean4 || c.StepRate != 1.5 || c.NFloat1 != 2.5 || c.SystemAction != 7 {
		t.Fatalf("NBoolean4 分支字段解析错误: %v %v %v %v", c.NBoolean4, c.StepRate, c.NFloat1, c.SystemAction)
	}
}

// TestGameCommandOnePacketInternalUnit unitType >= 0 时取内部单位名
func TestGameCommandOnePacketInternalUnit(t *testing.T) {
	b := buildCommandBytes(t, commandCfg{
		sendBy:        0,
		withOperation: true,
		opAction:      enums.ActionMove,
		opUnitType:    0, // GameInternalUnits 第 0 个
		actionID:      "x",
	})

	c := NewGameCommandOnePacket(b)
	if c.OperationUnit == nil {
		t.Fatal("应有 operationUnit")
	}
	want := enums.UnitFromIndex(0).Name()
	if !c.OperationUnit.HasUnitName || c.OperationUnit.UnitName != want {
		t.Fatalf("unitName = %q, 期望内部单位 %q", c.OperationUnit.UnitName, want)
	}
}
