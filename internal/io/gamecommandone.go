package gameio

import (
	"rwhps/internal/game/enums"
)

// 本文件对应 net.rwhps.server.io.packet.GameCommandOnePacket 与
// io.packet.block.GameCommandBlockOperationUnitPacket.
//
// 放置位置说明: 上游这两个类在 io.packet 子包, 但 Go 版 internal/io 已导入
// internal/io/packet (GameInputStream 读 Packet), 反向导入会成环, 因此解析器
// 放在本包 (与 GameInputStream 同包), 类型归属不变, 仅包级位置不同.
//
// 上游 ActionIdData 是按 actionId 驻留的包装类 (仅用于身份比较), Go 版直接用
// string 表达 actionId, 语义等价.

// GameCommandBlockOperationUnit 对应 GameCommandBlockOperationUnitPacket
type GameCommandBlockOperationUnit struct {
	// GameCommandActions 对应 gameCommandActions
	GameCommandActions enums.GameCommandActions
	// UnitName 对应 unitName: unitType=-1 时无单位名
	UnitName string
	// HasUnitName 区分上游 unitName 的 null 与非空
	HasUnitName bool
	// X / Y 对应 x / y
	X float32
	Y float32
	// ActionID 对应 actionIdData.actionId
	ActionID string
}

// decodeGameCommandBlockOperationUnit 对应 GameCommandBlockOperationUnitPacket 的 init
func decodeGameCommandBlockOperationUnit(s *GameInputStream) *GameCommandBlockOperationUnit {
	o := &GameCommandBlockOperationUnit{}

	o.GameCommandActions = enums.ActionFromIndex(int(s.ReadInt()))

	switch unitType := int(s.ReadInt()); unitType {
	case -1:
		// 上游: unitName = null
	case -2:
		o.UnitName = s.ReadString()
		o.HasUnitName = true
	default:
		o.UnitName = enums.UnitFromIndex(unitType).Name()
		o.HasUnitName = true
	}

	o.X = s.ReadFloat()
	o.Y = s.ReadFloat()

	// 上游: 未知 20 字节
	s.Skip(20)

	o.ActionID = s.ReadIsString()
	return o
}

// GameCommandOnePacket 对应 GameCommandOnePacket
//
// 上游 sendBy 在引擎就绪时解析为 PlayerHess, 否则只读丢弃; Go 版统一读出
// 位置下标 (SendBy), 由调用方按需解析 —— 对应上游两个分支的共同读取动作.
type GameCommandOnePacket struct {
	// SendBy 对应 sendBy 的读字节 (玩家位置下标)
	SendBy int
	// OperationUnit 对应 operationUnit, 无则 nil
	OperationUnit *GameCommandBlockOperationUnit
	// NBoolean1 / NBoolean2 对应 N_Boolean_1 / N_Boolean_2
	NBoolean1 bool
	NBoolean2 bool
	// NInt1 对应 N_Int_1
	NInt1 int32
	// UnitAction 对应 unitAction
	UnitAction enums.GameUnitActions
	// RallyPoint 对应 rallyPoint, 无则 nil
	RallyPoint []float32
	// NRemoveUnit 对应 N_removeUnit
	NRemoveUnit bool
	// OpsUnitList 对应 opsUnitList
	OpsUnitList []int64
	// MapPoint 对应 mapPoint, 无则 nil
	MapPoint []float32
	// ActionID 对应 actionIdData.actionId
	ActionID string
	// NBoolean3 对应 N_Boolean_3
	NBoolean3 bool
	// SharedControlCheck 对应 sharedControlCheck
	SharedControlCheck int16
	// NBoolean4 对应 N_Boolean_4
	NBoolean4 bool
	// StepRate / NFloat1 / SystemAction 对应同名字段 (N_Boolean_4 为 false 时为零值)
	StepRate     float32
	NFloat1      float32
	SystemAction int32
}

// NewGameCommandOnePacket 对应 GameCommandOnePacket(bytes) 的 init 解析
func NewGameCommandOnePacket(bytes []byte) *GameCommandOnePacket {
	s := NewGameInputStreamFromBytes(bytes, 0)
	c := &GameCommandOnePacket{}

	// 上游: 引擎就绪时 getPlayer(readByte()), 否则 readByte() 丢弃; 都读一个字节
	c.SendBy = s.ReadSignedByte()

	if s.ReadBoolean() {
		c.OperationUnit = decodeGameCommandBlockOperationUnit(s)
	}

	c.NBoolean1 = s.ReadBoolean()
	c.NBoolean2 = s.ReadBoolean()
	c.NInt1 = s.ReadInt()
	c.UnitAction = enums.StanceFromIndex(int(s.ReadInt()))

	if s.ReadBoolean() {
		c.RallyPoint = []float32{s.ReadFloat(), s.ReadFloat()}
	}

	c.NRemoveUnit = s.ReadBoolean()

	opsQty := int(s.ReadInt())
	c.OpsUnitList = make([]int64, 0, opsQty)
	for i := 0; i < opsQty; i++ {
		c.OpsUnitList = append(c.OpsUnitList, s.ReadLong())
	}

	if s.ReadBoolean() {
		// 上游: readByte() 后 N_player = null (getPlayer 被注释掉)
		s.ReadSignedByte()
	}

	if s.ReadBoolean() {
		c.MapPoint = []float32{s.ReadFloat(), s.ReadFloat()}
	}

	// 上游: 一个 String (可能是单位名称), 目前无法实现 [C(I)] —— skip(8)
	s.Skip(8)

	c.ActionID = s.ReadString()
	c.NBoolean3 = s.ReadBoolean()
	c.SharedControlCheck = s.ReadShort()
	c.NBoolean4 = s.ReadBoolean()

	if c.NBoolean4 {
		s.ReadSignedByte()
		c.StepRate = s.ReadFloat()
		c.NFloat1 = s.ReadFloat()
		c.SystemAction = s.ReadInt()
	}

	return c
}
