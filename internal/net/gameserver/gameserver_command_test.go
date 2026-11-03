package gameserver

import (
	"testing"

	"rwhps/internal/game/enums"
	"rwhps/internal/game/event"
	gameio "rwhps/internal/io"
	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
	"rwhps/internal/util/control"
)

// buildOnePacketBytes 按 GameCommandOnePacket 的解析顺序组装命令字节.
// 只覆盖测试需要的字段, 其余按解析顺序补零/补 false.
func buildOnePacketBytes(t *testing.T, opt struct {
	withOperation bool
	opAction      enums.GameCommandActions
	opUnitName    string // 空则按 -1 (无单位名)
	opX, opY      float32
	withMapPoint  bool
	mapX, mapY    float32
	actionID      string
}) []byte {
	t.Helper()
	o := gameio.NewGameOutputStream()

	_ = o.WriteByte(0) // sendBy

	o.WriteBoolean(opt.withOperation)
	if opt.withOperation {
		o.WriteInt(int32(opt.opAction))
		if opt.opUnitName == "" {
			o.WriteInt(-1)
		} else {
			o.WriteInt(-2)
			o.WriteString(opt.opUnitName)
		}
		o.WriteFloat(opt.opX)
		o.WriteFloat(opt.opY)
		o.WriteBytes(make([]byte, 20))
		o.WriteIsString("") // op actionId
	}

	o.WriteBoolean(false) // N_Boolean_1
	o.WriteBoolean(false) // N_Boolean_2
	o.WriteInt(0)         // N_Int_1
	o.WriteInt(0)         // unitAction

	o.WriteBoolean(false) // rallyPoint
	o.WriteBoolean(false) // N_removeUnit
	o.WriteInt(0)         // opsQty
	o.WriteBoolean(false) // N_player

	o.WriteBoolean(opt.withMapPoint)
	if opt.withMapPoint {
		o.WriteFloat(opt.mapX)
		o.WriteFloat(opt.mapY)
	}

	o.WriteBytes(make([]byte, 8)) // skip(8)
	o.WriteString(opt.actionID)
	o.WriteBoolean(false) // N_Boolean_3
	o.WriteShort(0)       // sharedControlCheck
	o.WriteBoolean(false) // N_Boolean_4

	return o.GetByteArray()
}

// wrapCommandPacket 把命令字节包成 GAMECOMMAND_RECEIVE 包
// (包体 = writeString + writeBytesAndLength, 与 GetDecodeBytes 的读取对应)
func wrapCommandPacket(t *testing.T, one []byte) *packet.Packet {
	t.Helper()
	o := gameio.NewGameOutputStream()
	o.WriteString("")
	o.WriteBytesAndLength(one)
	return o.CreatePacket(packettype.GAMECOMMAND_RECEIVE)
}

// TestReceiveCommandUnitEventIntercept operationUnit 事件被监听器拦截 -> STOPPED
func TestReceiveCommandUnitEventIntercept(t *testing.T) {
	h := &mockHelper{}
	g, _ := newTestServer(t, h)
	bindTestPlayer(t, g, "hex-op", "opper")

	// 监听器把操作判为无效
	event.Register(g.room.EventManage, func(ev *event.PlayerOperationUnitEvent) {
		ev.ResultStatus = false
	})

	one := buildOnePacketBytes(t, struct {
		withOperation bool
		opAction      enums.GameCommandActions
		opUnitName    string
		opX, opY      float32
		withMapPoint  bool
		mapX, mapY    float32
		actionID      string
	}{withOperation: true, opAction: enums.ActionAttack, opUnitName: "tank", opX: 1, opY: 2, actionID: "_x"})

	p := wrapCommandPacket(t, one)
	g.ReceiveCommand(p)

	if p.Status != control.STOPPED {
		t.Fatal("被拦截的操作应置 STOPPED")
	}
	h.mu.Lock()
	n := len(h.packets)
	h.mu.Unlock()
	if n != 0 {
		t.Fatalf("被拦截的操作不应转引擎, 转发了 %d 个包", n)
	}
}

// TestReceiveCommandUnitEventPass operationUnit 事件放行 -> 继续转引擎
func TestReceiveCommandUnitEventPass(t *testing.T) {
	h := &mockHelper{}
	g, _ := newTestServer(t, h)
	bindTestPlayer(t, g, "hex-op2", "opper2")

	var saw *event.PlayerOperationUnitEvent
	event.Register(g.room.EventManage, func(ev *event.PlayerOperationUnitEvent) { saw = ev })

	one := buildOnePacketBytes(t, struct {
		withOperation bool
		opAction      enums.GameCommandActions
		opUnitName    string
		opX, opY      float32
		withMapPoint  bool
		mapX, mapY    float32
		actionID      string
	}{withOperation: true, opAction: enums.ActionMove, opUnitName: "bot", opX: 3, opY: 4, actionID: "_mv"})

	p := wrapCommandPacket(t, one)
	tc := &HeadlessTypeConnect{con: g}
	if err := tc.ProcessConnect(p); err != nil {
		t.Fatalf("ProcessConnect: %v", err)
	}

	if saw == nil {
		t.Fatal("应触发 PlayerOperationUnitEvent")
	}
	if saw.Actions != enums.ActionMove || saw.UnitName != "bot" || saw.X != 3 || saw.Y != 4 {
		t.Fatalf("事件负载错误: %v %q %v %v", saw.Actions, saw.UnitName, saw.X, saw.Y)
	}
	h.mu.Lock()
	n := len(h.packets)
	h.mu.Unlock()
	if n != 1 {
		t.Fatalf("放行的操作应转引擎, 转发了 %d 个包", n)
	}
}

// TestReceiveCommandFactoryBuildEvent actionId 非 "_" 前缀触发工厂建造事件
func TestReceiveCommandFactoryBuildEvent(t *testing.T) {
	h := &mockHelper{}
	g, _ := newTestServer(t, h)
	bindTestPlayer(t, g, "hex-fb", "faber")

	var sawAction string
	event.Register(g.room.EventManage, func(ev *event.PlayerOperationFactoryBuildUnitEvent) {
		sawAction = ev.ActionID
	})

	one := buildOnePacketBytes(t, struct {
		withOperation bool
		opAction      enums.GameCommandActions
		opUnitName    string
		opX, opY      float32
		withMapPoint  bool
		mapX, mapY    float32
		actionID      string
	}{actionID: "c_9_landFactory"})

	g.ReceiveCommand(wrapCommandPacket(t, one))
	if sawAction != "c_9_landFactory" {
		t.Fatalf("应触发工厂建造事件, got %q", sawAction)
	}

	// "_" 前缀不触发
	sawAction = ""
	one2 := buildOnePacketBytes(t, struct {
		withOperation bool
		opAction      enums.GameCommandActions
		opUnitName    string
		opX, opY      float32
		withMapPoint  bool
		mapX, mapY    float32
		actionID      string
	}{actionID: "_idle"})
	g.ReceiveCommand(wrapCommandPacket(t, one2))
	if sawAction != "" {
		t.Fatalf("_ 前缀不应触发工厂建造事件, got %q", sawAction)
	}
}

// TestReceiveCommandPingCallback mapPoint + c_6_ 前缀触发玩家 Ping 回调
func TestReceiveCommandPingCallback(t *testing.T) {
	h := &mockHelper{}
	g, _ := newTestServer(t, h)
	bindTestPlayer(t, g, "hex-ping", "pinger")

	var gotX, gotY float32
	var gotAction enums.GamePingActions
	calls := 0
	g.player.AddData("Ping", func(_ *GameVersionServer, a enums.GamePingActions, x, y float32) {
		calls++
		gotAction, gotX, gotY = a, x, y
	})

	one := buildOnePacketBytes(t, struct {
		withOperation bool
		opAction      enums.GameCommandActions
		opUnitName    string
		opX, opY      float32
		withMapPoint  bool
		mapX, mapY    float32
		actionID      string
	}{withMapPoint: true, mapX: 11.5, mapY: 22.5, actionID: "c_6_NUKE"})

	g.ReceiveCommand(wrapCommandPacket(t, one))

	if calls != 1 {
		t.Fatalf("Ping 回调应触发一次, got %d", calls)
	}
	if gotAction != enums.PingNuke || gotX != 11.5 || gotY != 22.5 {
		t.Fatalf("Ping 参数错误: %v %v %v", gotAction, gotX, gotY)
	}
	// 回调后 Ping 数据被移除
	if _, ok := g.player.GetData("Ping"); ok {
		t.Fatal("Ping 回调后应移除 Ping 数据")
	}
}
