package external

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"rwhps/internal/game"
)

// matchCfg 构造一个与上游默认值一致的开局参数
//
// 地图默认值来自 game.DefaultMapName / game.DefaultMapPlayer
// (对应 GameMaps 的默认 "Crossing Large (10p)" / "[z;p10]").
func matchCfg(room string) game.MatchConfig {
	return game.MatchConfig{
		RoomID:    room,
		MapName:   game.DefaultMapName,
		MapPlayer: game.DefaultMapPlayer,
		MaxPlayer: 10,
	}
}

// TestSimulationStartCreatesRoomThenStarts 开局 = 先建房再开局, 字段完整下发
func TestSimulationStartCreatesRoomThenStarts(t *testing.T) {
	var createReq, startReq Request

	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpRoomCreate, before: func(req *Request, _ *os.File) {
			createReq = *req
		}, reply: ok()},
		step{wantOp: OpRoomStart, before: func(req *Request, _ *os.File) {
			startReq = *req
		}, reply: ok()},
		step{wantOp: OpRoomStatus, reply: &Response{V: ProtocolVersion, OK: true, Running: true, Players: 3}},
	)

	if err := b.Start(matchCfg("r1")); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if createReq.Room != "r1" || createReq.MaxPlayer != 10 {
		t.Fatalf("room.create = %+v, 期望 room=r1 maxPlayer=10", createReq)
	}
	if startReq.Room != "r1" || startReq.Map != game.DefaultMapName || startReq.MapPlayer != game.DefaultMapPlayer {
		t.Fatalf("room.start = %+v, 期望携带默认地图参数", startReq)
	}

	// Running 以 helper 的 room.status 应答为准
	if !b.Running() {
		t.Fatal("helper 报告 running 时 Running() 应为 true")
	}
}

// TestSimulationStartSkipsCreateWhenRoomExists 同一房间第二次开局不再重复建房
//
// 对应原版语义: 房间先于对局存在, 对局结束后房间保留, 可以再次开局.
func TestSimulationStartSkipsCreateWhenRoomExists(t *testing.T) {
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpRoomCreate, reply: ok()},
		step{wantOp: OpRoomStart, reply: ok()},
		// 第二次开局: 只有 room.start, 没有 room.create
		// (若误发 create, serve 会因 op 不匹配报错)
		step{wantOp: OpRoomStart, reply: ok()},
		step{wantOp: OpRoomStop, reply: ok()},
	)

	if err := b.Start(matchCfg("r1")); err != nil {
		t.Fatalf("第一次 Start: %v", err)
	}
	if err := b.Start(matchCfg("r1")); err != nil {
		t.Fatalf("第二次 Start: %v", err)
	}
	if err := b.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestSimulationStopWithoutRoom 未开局过就没有可结束的对局
func TestSimulationStopWithoutRoom(t *testing.T) {
	b, _, _, _ := setup(t)

	if err := b.Stop(); err != ErrNoRoom {
		t.Fatalf("Stop = %v, 期望 ErrNoRoom", err)
	}
}

// TestSimulationStatusWithoutProcess 未拉起进程时状态查询必须无副作用
//
// 惰性启动是 Backend 的核心约定: 查状态不该把 JVM 拉起来.
func TestSimulationStatusWithoutProcess(t *testing.T) {
	b, _, _, _ := setup(t)

	if b.Running() {
		t.Fatal("进程未启动时 Running() 应为 false")
	}
	if got := b.PlayerCount(); got != 0 {
		t.Fatalf("进程未启动时 PlayerCount() = %d, 期望 0", got)
	}
	if b.Generation() != 0 {
		t.Fatalf("状态查询不应拉起进程, Generation = %d", b.Generation())
	}
}

// TestSimulationPlayerCountFromStatus 人数经 room.status 取自 helper
func TestSimulationPlayerCountFromStatus(t *testing.T) {
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpRoomCreate, reply: ok()},
		step{wantOp: OpRoomStart, reply: ok()},
		step{wantOp: OpRoomStatus, reply: &Response{V: ProtocolVersion, OK: true, Running: true, Players: 4}},
	)

	if err := b.Start(matchCfg("r1")); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := b.PlayerCount(); got != 4 {
		t.Fatalf("PlayerCount() = %d, 期望 4", got)
	}
}

// TestSimulationStartFailurePropagates 开局失败: 原因透传, 房间保留, 状态不置位
func TestSimulationStartFailurePropagates(t *testing.T) {
	b, _, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpRoomCreate, reply: ok()},
		step{wantOp: OpRoomStart, reply: &Response{V: ProtocolVersion, OK: false, Error: "map file missing"}},
		step{wantOp: OpRoomStatus, reply: ok()}, // running=false
		// 重试开局: 房间已建好, 只发 room.start
		step{wantOp: OpRoomStart, reply: ok()},
	)

	err := b.Start(matchCfg("r1"))
	if err == nil || !strings.Contains(err.Error(), "map file missing") {
		t.Fatalf("Start = %v, 期望 helper 的失败原因被透传", err)
	}
	if b.Running() {
		t.Fatal("开局失败后 Running() 应为 false")
	}

	if err := b.Start(matchCfg("r1")); err != nil {
		t.Fatalf("重试 Start: %v", err)
	}
}

// TestSimulationStateResetsOnHelperExit helper 死亡后房间作废
//
// 上一代 helper 承载的房间随进程消失; 残留的房间号会让后续操作
// 去查一个不存在的对象.
func TestSimulationStateResetsOnHelperExit(t *testing.T) {
	b, fp, _, _ := setup(t,
		step{wantOp: OpHello, reply: ok()},
		step{wantOp: OpRoomCreate, reply: ok()},
		step{wantOp: OpRoomStart, reply: ok()},
	)

	exited := make(chan struct{})
	b.SetHandlers(Handlers{
		HelperExit: func(uint64) { close(exited) },
	})

	if err := b.Start(matchCfg("r1")); err != nil {
		t.Fatalf("Start: %v", err)
	}

	fp.Kill()
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("helper 退出没有通知上层")
	}

	if b.Running() {
		t.Fatal("helper 退出后 Running() 应为 false")
	}
	if err := b.Stop(); !errors.Is(err, ErrNoRoom) {
		t.Fatalf("helper 退出后 Stop = %v, 期望 ErrNoRoom", err)
	}
}
