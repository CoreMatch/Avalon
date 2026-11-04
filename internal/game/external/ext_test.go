package external

// v2-ext 扩展操作的 Backend 级测试 (DECISIONS D-9).
//
// 有意修正（作者已批准，2026-11-04）: 上游这些能力直接调用进程内引擎,
// Go 版经 helper 协议扩展操作完成. 手法沿用 backend_test.go 的假 helper.

import (
	"errors"
	"os"
	"testing"
	"time"

	"rwhps/internal/game"
)

// startRoom 走 hello + room.create + room.start, 让 Backend 持有房间
func startRoom(t *testing.T, steps ...step) *Backend {
	t.Helper()
	all := append([]step{
		{wantOp: OpHello, reply: ok()},
		{wantOp: OpRoomCreate, reply: ok()},
		{wantOp: OpRoomStart, reply: ok()},
	}, steps...)
	b, _, _, _ := setup(t, all...)
	if err := b.Start(game.MatchConfig{RoomID: "r1", MapName: "m1", MaxPlayer: 10}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return b
}

// TestBackendExtAddAI room.addai 往返: 应答带 AI 档案
func TestBackendExtAddAI(t *testing.T) {
	pos := 3
	b := startRoom(t,
		step{wantOp: OpRoomAddAI, reply: &Response{V: ProtocolVersion, OK: true, AI: &PlayerProfile{
			ID: "hex-ai", Name: "AI - Hard", Team: 2, Bot: true, Position: &pos,
		}}},
	)
	ai, err := b.AddAI()
	if err != nil {
		t.Fatalf("AddAI: %v", err)
	}
	if ai == nil {
		t.Fatal("应答带 ai 字段时档案不应为 nil")
	}
	if ai.ID != "hex-ai" || ai.Name != "AI - Hard" || ai.Team != 2 || ai.Position != 3 {
		t.Fatalf("AI 档案 = %+v", ai)
	}
}

// TestBackendExtAddAIPositionZero position=0 必须原样到达 (指针语义)
func TestBackendExtAddAIPositionZero(t *testing.T) {
	pos := 0
	b := startRoom(t,
		step{wantOp: OpRoomAddAI, reply: &Response{V: ProtocolVersion, OK: true, AI: &PlayerProfile{
			ID: "hex-ai", Name: "AI - Hard", Bot: true, Position: &pos,
		}}},
	)
	ai, err := b.AddAI()
	if err != nil {
		t.Fatalf("AddAI: %v", err)
	}
	if ai == nil || ai.Position != 0 {
		t.Fatalf("position 0 应原样到达, 得到 %+v", ai)
	}
}

// TestBackendExtAddAINoProfile 应答不带 ai 字段: 引擎未创建 (无空位), 档案为 nil
func TestBackendExtAddAINoProfile(t *testing.T) {
	b := startRoom(t,
		step{wantOp: OpRoomAddAI, reply: ok()},
	)
	ai, err := b.AddAI()
	if err != nil {
		t.Fatalf("AddAI: %v", err)
	}
	if ai != nil {
		t.Fatalf("无 ai 字段时档案应为 nil, 得到 %+v", ai)
	}
}

// TestBackendExtSyncAllPlayer room.sync 往返
func TestBackendExtSyncAllPlayer(t *testing.T) {
	b := startRoom(t,
		step{wantOp: OpRoomSync, reply: ok()},
	)
	if err := b.SyncAllPlayer(); err != nil {
		t.Fatalf("SyncAllPlayer: %v", err)
	}
}

// TestBackendExtRemovePlayer room.removeplayer 带位置字段
func TestBackendExtRemovePlayer(t *testing.T) {
	b := startRoom(t,
		step{wantOp: OpRoomRemovePlayer, reply: ok()},
	)
	if err := b.RemovePlayer(3); err != nil {
		t.Fatalf("RemovePlayer: %v", err)
	}
}

// TestBackendExtRemovePlayerPositionOnWire position 字段要上线且取值正确
func TestBackendExtRemovePlayerPositionOnWire(t *testing.T) {
	var got *int
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpRoomCreate, reply: ok()},
		step{wantOp: OpRoomStart, reply: ok()},
		step{wantOp: OpRoomRemovePlayer, before: func(req *Request, _ *os.File) {
			got = req.Position
		}, reply: ok()},
	)
	if err := b.Start(game.MatchConfig{RoomID: "r1", MapName: "m1", MaxPlayer: 10}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := b.RemovePlayer(0); err != nil {
		t.Fatalf("RemovePlayer(0): %v", err)
	}
	if got == nil || *got != 0 {
		t.Fatalf("线上 position = %v, 期望 0", got)
	}
}

// TestBackendExtGameResult game.result 应答带结算数据
func TestBackendExtGameResult(t *testing.T) {
	b := startRoom(t,
		step{wantOp: OpGameResult, reply: &Response{V: ProtocolVersion, OK: true, Result: &GameResult{
			GameTime:      600,
			AllPlayerList: []string{"a", "b"},
			WinPlayerList: []string{"a"},
			MapName:       "m1",
			PlayerData: map[string]map[string]int{
				"a": {"unitsKilled": 7, "buildingsKilled": 0, "experimentalsKilled": 0,
					"unitsLost": 1, "buildingsLost": 2, "experimentalsLost": 3},
			},
			ReplayName: "rep1",
		}}},
	)

	data, ok, err := b.GameResult()
	if err != nil {
		t.Fatalf("GameResult: %v", err)
	}
	if !ok {
		t.Fatal("有结算数据时 ok 应为 true")
	}
	if data.GameTime != 600 || data.MapName != "m1" || data.ReplayName != "rep1" {
		t.Fatalf("标量字段 = %+v", data)
	}
	if len(data.WinPlayerList) != 1 || data.WinPlayerList[0] != "a" {
		t.Fatalf("winPlayerList = %v", data.WinPlayerList)
	}
	if data.PlayerData["a"]["unitsKilled"] != 7 || data.PlayerData["a"]["unitsLost"] != 1 {
		t.Fatalf("playerData = %v", data.PlayerData)
	}
}

// TestBackendExtGameResultNotOver 尚未分出胜负: 应答无 result, ok=false
func TestBackendExtGameResultNotOver(t *testing.T) {
	b := startRoom(t,
		step{wantOp: OpGameResult, reply: ok()},
	)

	data, ok, err := b.GameResult()
	if err != nil {
		t.Fatalf("GameResult: %v", err)
	}
	if ok || data != nil {
		t.Fatalf("未分胜负应 ok=false, 得到 ok=%v data=%+v", ok, data)
	}
}

// TestBackendExtUnsupported v2-only helper 回 unsupported 时归一为 game.ErrUnsupported
func TestBackendExtUnsupported(t *testing.T) {
	b := startRoom(t,
		step{wantOp: OpRoomAddAI, reply: &Response{V: ProtocolVersion, OK: false, Error: "unsupported"}},
		step{wantOp: OpRoomSync, reply: &Response{V: ProtocolVersion, OK: false, Error: "unsupported"}},
		step{wantOp: OpRoomRemovePlayer, reply: &Response{V: ProtocolVersion, OK: false, Error: "unsupported"}},
		step{wantOp: OpGameResult, reply: &Response{V: ProtocolVersion, OK: false, Error: "unsupported"}},
	)

	if _, err := b.AddAI(); !errors.Is(err, game.ErrUnsupported) {
		t.Fatalf("AddAI = %v, 期望 game.ErrUnsupported", err)
	}
	if err := b.SyncAllPlayer(); !errors.Is(err, game.ErrUnsupported) {
		t.Fatalf("SyncAllPlayer = %v, 期望 game.ErrUnsupported", err)
	}
	if err := b.RemovePlayer(1); !errors.Is(err, game.ErrUnsupported) {
		t.Fatalf("RemovePlayer = %v, 期望 game.ErrUnsupported", err)
	}
	if _, _, err := b.GameResult(); !errors.Is(err, game.ErrUnsupported) {
		t.Fatalf("GameResult = %v, 期望 game.ErrUnsupported", err)
	}
}

// TestBackendExtNoRoom 未创建房间时扩展操作返回 ErrNoRoom
func TestBackendExtNoRoom(t *testing.T) {
	b := newTestBackend(t, 2*time.Second)

	if _, err := b.AddAI(); err != ErrNoRoom {
		t.Fatalf("AddAI = %v, 期望 ErrNoRoom", err)
	}
	if err := b.SyncAllPlayer(); err != ErrNoRoom {
		t.Fatalf("SyncAllPlayer = %v, 期望 ErrNoRoom", err)
	}
	if err := b.RemovePlayer(0); err != ErrNoRoom {
		t.Fatalf("RemovePlayer = %v, 期望 ErrNoRoom", err)
	}
	if _, _, err := b.GameResult(); err != ErrNoRoom {
		t.Fatalf("GameResult = %v, 期望 ErrNoRoom", err)
	}
}
