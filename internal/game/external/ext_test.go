package external

// v2-ext 扩展操作的 Backend 级测试 (DECISIONS D-9).
//
// 有意修正（作者已批准，2026-11-04）: 上游这些能力直接调用进程内引擎,
// Go 版经 helper 协议扩展操作完成. 手法沿用 backend_test.go 的假 helper.

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/CoreMatch/Avalon/internal/game"
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

// TestBackendStartLobbySkipsRoomStart 大厅模式 (MatchConfig.Lobby): 只建房
// (room.create) 不开局 (room.start), 也不置 running. 脚本里故意不放
// OpRoomStart 步骤 —— 若误发 room.start, 该请求无人应答, Start 将超时失败.
func TestBackendStartLobbySkipsRoomStart(t *testing.T) {
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpRoomCreate, reply: ok()},
	)
	if err := b.Start(game.MatchConfig{RoomID: "r1", MaxPlayer: 10, Lobby: true}); err != nil {
		t.Fatalf("Start(lobby): %v", err)
	}
	if b.localRunning() {
		t.Fatal("大厅模式不应置 running")
	}
	if got := b.currentRoom(); got != "r1" {
		t.Fatalf("currentRoom = %q, want r1", got)
	}
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

// TestBackendExtApplySettings room.settings 携带负载往返
func TestBackendExtApplySettings(t *testing.T) {
	var got *Request
	credits := int32(5000)
	fog := int32(1)
	income := float32(2.0)
	disableNuke := true
	b := startRoom(t,
		step{wantOp: OpRoomSettings, before: func(req *Request, _ *os.File) {
			got = req
		}, reply: ok()},
	)
	err := b.ApplySettings(game.RoomSettings{
		Credits:     &credits,
		Fog:         &fog,
		Income:      &income,
		DisableNuke: &disableNuke,
	})
	if err != nil {
		t.Fatalf("ApplySettings: %v", err)
	}
	if got == nil || got.Settings == nil {
		t.Fatal("线上应携带 settings 负载")
	}
	s := got.Settings
	if s.Credits == nil || *s.Credits != 5000 || s.Fog == nil || *s.Fog != 1 ||
		s.Income == nil || *s.Income != 2.0 || s.DisableNuke == nil || !*s.DisableNuke {
		t.Fatalf("settings 负载 = %+v", s)
	}
	if s.SharedControl != nil || s.StartingUnits != nil || s.MaxUnits != nil {
		t.Fatal("未提供的字段应保持 nil")
	}
}

// TestBackendExtApplySettingsEmpty 无字段时不发请求
func TestBackendExtApplySettingsEmpty(t *testing.T) {
	b := startRoom(t) // 不期望任何后续请求
	if err := b.ApplySettings(game.RoomSettings{}); err != nil {
		t.Fatalf("ApplySettings(空): %v", err)
	}
}

// TestRecreateRoomIssuesRoomCreate RecreateRoom: 在当前一代发 room.create
// 并登记房间; 同一世代同名房间第二次调用不重发.
func TestRecreateRoomIssuesRoomCreate(t *testing.T) {
	var creates int
	var gotMax int
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpRoomCreate, before: func(req *Request, _ *os.File) {
			creates++
			gotMax = req.MaxPlayer
		}, reply: ok()},
	)

	if err := b.RecreateRoom("r1", 7); err != nil {
		t.Fatalf("RecreateRoom: %v", err)
	}
	if b.currentRoom() != "r1" {
		t.Fatalf("currentRoom = %q, 期望 r1", b.currentRoom())
	}
	if creates != 1 || gotMax != 7 {
		t.Fatalf("room.create 次数/参数 = %d/%d, 期望 1/7", creates, gotMax)
	}

	// 同一世代已持有该房间: 短路, 不重发
	if err := b.RecreateRoom("r1", 7); err != nil {
		t.Fatalf("RecreateRoom(重复): %v", err)
	}
	if creates != 1 {
		t.Fatalf("同世代不应重发 room.create, creates = %d", creates)
	}
}

// TestHelperReadyRolloverRecreatesRoom helper 崩溃重启后: HelperReady 在
// 新一代上触发, RecreateRoom 把房间重建到新一代 (世代更替恢复全链路).
//
// 修复前缺陷: 房间只在进程启动时对第一代创建, 新一代无房, 后续
// conn.* 全部 "packet without room" 协议自杀.
func TestHelperReadyRolloverRecreatesRoom(t *testing.T) {
	first, read1, write1 := newFakeProcess()
	second, read2, write2 := newFakeProcess()
	t.Cleanup(func() {
		_ = read1.Close()
		_ = write1.Close()
		_ = read2.Close()
		_ = write2.Close()
	})

	old := newProcess
	var calls int
	newProcess = func(string, []string, string, int) process {
		calls++
		if calls == 1 {
			return first
		}
		return second
	}
	t.Cleanup(func() { newProcess = old })

	b := newTestBackend(t, 2*time.Second)

	var mu sync.Mutex
	var readyGens []uint64
	recreated := make(chan uint64, 2)
	b.SetHandlers(Handlers{
		HelperReady: func(gen uint64) {
			mu.Lock()
			readyGens = append(readyGens, gen)
			mu.Unlock()
			// 上层接线 (gameserver.HelperReady) 的同款动作
			if err := b.RecreateRoom("r1", 10); err != nil {
				t.Errorf("RecreateRoom(gen %d): %v", gen, err)
			}
			recreated <- gen
		},
	})

	// 第一代: hello + 重建触发的 room.create
	go serve(t, read1, write1, []step{
		{wantOp: OpHello, reply: ok()},
		{wantOp: OpRoomCreate, reply: ok()},
	})
	if err := b.EnsureRunning(); err != nil {
		t.Fatalf("EnsureRunning(gen1): %v", err)
	}

	// 第二代: 崩溃重启, 脚本同样要应答 hello + room.create
	go serve(t, read2, write2, []step{
		{wantOp: OpHello, reply: ok()},
		{wantOp: OpRoomCreate, reply: ok()},
	})
	first.Kill()

	deadline := time.Now().Add(3 * time.Second)
	for b.Generation() == 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if b.Generation() != 2 {
		t.Fatalf("重启后 Generation = %d, 期望 2", b.Generation())
	}

	// 等两代的重建都跑完: gen 2 的 HelperReady 在重启 goroutine 里执行,
	// readyGens 只证明回调开始过, 不代表 RecreateRoom 已完成 —— 不等完成
	// 就断言 currentRoom 会与回调竞态 (CI 负载下表现为 currentRoom 尚为空).
	timeout := time.After(3 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case <-recreated:
		case <-timeout:
			t.Fatalf("等待第 %d 代重建完成超时", i+1)
		}
	}

	// HelperReady 对两代各触发一次, 房间最终登记在新一代上
	mu.Lock()
	gens := append([]uint64{}, readyGens...)
	mu.Unlock()
	if len(gens) != 2 || gens[0] != 1 || gens[1] != 2 {
		t.Fatalf("HelperReady 触发世代 = %v, 期望 [1 2]", gens)
	}
	if got := b.currentRoom(); got != "r1" {
		t.Fatalf("新一代 currentRoom = %q, 期望 r1", got)
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
	credits := int32(1)
	if err := b.ApplySettings(game.RoomSettings{Credits: &credits}); err != ErrNoRoom {
		t.Fatalf("ApplySettings = %v, 期望 ErrNoRoom", err)
	}
}
