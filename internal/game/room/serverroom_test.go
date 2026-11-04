package room

import (
	"errors"
	"testing"
	"time"

	"rwhps/internal/core/thread"
	"rwhps/internal/data/global"
	"rwhps/internal/game"
	"rwhps/internal/game/event"
	"rwhps/internal/game/manage"
	"rwhps/internal/game/player"
)

// fakeSim 记录调用并按需返回错误
type fakeSim struct {
	startErr error
	stopErr  error
	started  bool
	stopped  bool
	running  bool
}

func (f *fakeSim) Start(game.MatchConfig) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.started, f.running = true, true
	return nil
}

func (f *fakeSim) Stop() error {
	if f.stopErr != nil {
		return f.stopErr
	}
	f.stopped, f.running = true, false
	return nil
}

func (f *fakeSim) Running() bool    { return f.running }
func (f *fakeSim) PlayerCount() int { return 0 }

// newTestRoom 构造带假内核的房间, 并负责收尾定时器
func newTestRoom(t *testing.T, sim game.Simulation) *ServerRoom {
	t.Helper()
	r := NewServerRoom(game.NewGameSession("room-sm", sim))
	t.Cleanup(func() {
		thread.CloseTimeTask(thread.AutoCheckTask, nil)
		thread.CloseTimeTask(thread.GameOverTask, nil)
		thread.CloseTimeTask(thread.CallTeamTask, nil)
		// 事件默认监听会挂自动开局/AFK 倒计时 (上游同样常驻), 测试结束前收掉
		thread.CloseTimeTask(thread.AutoStartTask, nil)
		thread.CloseTimeTask(thread.PlayerAfkTask, nil)
		SetDefault(nil)
	})
	return r
}

// TestRoomAssembly 实体链装配: 上游 ServerRoom 的成员一个不少
func TestRoomAssembly(t *testing.T) {
	r := newTestRoom(t, nil)

	if r.PlayerManage == nil || r.Call == nil || r.TeamManage == nil {
		t.Fatal("PlayerManage / Call / TeamManage 缺失")
	}
	if r.ClientHandler == nil {
		t.Fatal("ClientHandler 缺失")
	}
	if r.BanUnit == nil || r.FlagData == nil {
		t.Fatal("BanUnit / FlagData 缺失")
	}
	// 上游默认值
	if !r.FlagData.SendGameStatusFlag() || !r.FlagData.ForcedCloseSendMsg() || !r.FlagData.OneSay() || !r.FlagData.AiWarn() {
		t.Fatal("FlagData 默认值应为 true/true/true/_/true")
	}
	if r.FlagData.Ai() {
		t.Fatal("FlagData.Ai 默认应为 false")
	}
	if r.IsMixPlayer {
		t.Fatal("IsMixPlayer 默认应为 false")
	}
}

// TestRoomStartGame 开局状态机: 内核成功后才写状态并挂检查线程
func TestRoomStartGame(t *testing.T) {
	old := global.ConfigServer.MaxGameIngTime
	global.ConfigServer.MaxGameIngTime = 3600
	t.Cleanup(func() { global.ConfigServer.MaxGameIngTime = old })

	fs := &fakeSim{}
	r := newTestRoom(t, fs)

	if err := r.StartGame(); err != nil {
		t.Fatalf("StartGame: %v", err)
	}
	if !fs.started || !r.IsStartGame {
		t.Fatal("开局后内核应启动且 IsStartGame 为 true")
	}
	if r.GetEndTime()-r.GetStartTime() != 3600 {
		t.Fatalf("计时 = %d, 期望 3600", r.GetEndTime()-r.GetStartTime())
	}
	// 上游 roomStartGame 挂上自动检查线程
	if !thread.ContainsTimeTask(thread.AutoCheckTask) {
		t.Fatal("开局后应有 AutoCheckTask")
	}
}

// TestRoomStartGameBackendError 内核失败时房间保持未开局
func TestRoomStartGameBackendError(t *testing.T) {
	boom := errors.New("boom")
	r := newTestRoom(t, &fakeSim{startErr: boom})

	if err := r.StartGame(); !errors.Is(err, boom) {
		t.Fatalf("StartGame err = %v, 期望 boom", err)
	}
	if r.IsStartGame {
		t.Fatal("内核失败时不应置开局状态")
	}
	if thread.ContainsTimeTask(thread.AutoCheckTask) {
		t.Fatal("内核失败时不应挂检查线程")
	}
}

// TestGr 强制结束: 状态清零, 检查线程收掉
func TestGr(t *testing.T) {
	fs := &fakeSim{}
	r := newTestRoom(t, fs)
	if err := r.StartGame(); err != nil {
		t.Fatalf("StartGame: %v", err)
	}
	r.BanUnit.Add("tank")

	r.Gr()

	if r.IsStartGame || r.GetStartTime() != 0 || r.GetEndTime() != 0 {
		t.Fatal("Gr 后开局状态应清零")
	}
	if r.BanUnit.Size() != 0 {
		t.Fatal("Gr 后 banUnit 应清空")
	}
	if !r.FlagData.OneSay() || !r.FlagData.SendGameStatusFlag() {
		t.Fatal("Gr 后 FlagData 应重置")
	}
	if thread.ContainsTimeTask(thread.AutoCheckTask) {
		t.Fatal("Gr 后 AutoCheckTask 应收掉")
	}
	// gr 不停内核 (上游由引擎自行回到大厅)
	if fs.stopped {
		t.Fatal("Gr 不应调用 Simulation.Stop")
	}
}

// TestGameOverCommand gameover 入口: 先停内核再走 gr 清理
func TestGameOverCommand(t *testing.T) {
	fs := &fakeSim{}
	r := newTestRoom(t, fs)
	if err := r.StartGame(); err != nil {
		t.Fatalf("StartGame: %v", err)
	}

	if err := r.GameOver(); err != nil {
		t.Fatalf("GameOver: %v", err)
	}
	if !fs.stopped {
		t.Fatal("GameOver 应停内核")
	}
	if r.IsStartGame {
		t.Fatal("GameOver 后应清零开局状态")
	}
}

// TestGameOverBackendError 内核拒绝停止时不清空状态
func TestGameOverBackendError(t *testing.T) {
	boom := errors.New("boom")
	fs := &fakeSim{stopErr: boom}
	r := newTestRoom(t, fs)
	if err := r.StartGame(); err != nil {
		t.Fatalf("StartGame: %v", err)
	}

	if err := r.GameOver(); !errors.Is(err, boom) {
		t.Fatalf("GameOver err = %v, 期望 boom", err)
	}
	if !r.IsStartGame {
		t.Fatal("内核失败时不应清空开局状态")
	}
}

// TestBattleRoom 对应 battleRoom: 恢复 CallTeamTask + cleanData
func TestBattleRoom(t *testing.T) {
	r := newTestRoom(t, &fakeSim{})
	if err := r.StartGame(); err != nil {
		t.Fatalf("StartGame: %v", err)
	}
	r.BanUnit.Add("tank")

	r.BattleRoom()

	if r.IsStartGame {
		t.Fatal("BattleRoom 后应清零开局状态")
	}
	if r.BanUnit.Size() != 0 {
		t.Fatal("BattleRoom 后 banUnit 应清空")
	}
}

// TestAutoCheckEndsEmptyRoom 自动检查: 无 AI 且 0 在线玩家时收掉房间
//
// 上游每秒检查一次; 这里等两个周期确保触发. 这是"关键迁移"之一:
// 开局 -> 玩家走光 -> gr() 自动发生.
func TestAutoCheckEndsEmptyRoom(t *testing.T) {
	r := newTestRoom(t, &fakeSim{})
	if err := r.StartGame(); err != nil {
		t.Fatalf("StartGame: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	// 经 Snapshot 读 (带锁): 检查线程会在另一 goroutine 里写 IsStartGame
	for r.GameSession.Snapshot().IsStartGame && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if r.GameSession.Snapshot().IsStartGame {
		t.Fatal("0 玩家时自动检查应触发 gr() 结束对局")
	}
}

// TestDefaultSlot 单例槽位
func TestDefaultSlot(t *testing.T) {
	r := newTestRoom(t, nil)
	SetDefault(r)
	if Default() != r {
		t.Fatal("Default 应返回已设置的房间")
	}
}

// --- v2-ext 扩展操作 (DECISIONS D-9, 有意修正, 作者已批准, 2026-11-04) ---

// fakeRoomControl 在 fakeSim 基础上实现 game.RoomControl
type fakeRoomControl struct {
	fakeSim

	addAIProfile *game.AIProfile
	addAIErr     error
	addAICnt     int
	syncErr      error
	syncCnt      int
	removeErr    error
	removed      []int

	result    *event.GameOverData
	resultOK  bool
	resultErr error
}

func (f *fakeRoomControl) AddAI() (*game.AIProfile, error) {
	f.addAICnt++
	return f.addAIProfile, f.addAIErr
}

func (f *fakeRoomControl) SyncAllPlayer() error {
	f.syncCnt++
	return f.syncErr
}

func (f *fakeRoomControl) RemovePlayer(position int) error {
	f.removed = append(f.removed, position)
	return f.removeErr
}

func (f *fakeRoomControl) GameResult() (*event.GameOverData, bool, error) {
	return f.result, f.resultOK, f.resultErr
}

// TestRoomExtAddAI AddAI 经 RoomControl 转发; 降级路径返回 ErrNotPortable
func TestRoomExtAddAI(t *testing.T) {
	rc := &fakeRoomControl{}
	r := newTestRoom(t, rc)

	if err := r.AddAI(); err != nil {
		t.Fatalf("AddAI: %v", err)
	}
	if rc.addAICnt != 1 {
		t.Fatalf("AddAI 应转发到内核, cnt = %d", rc.addAICnt)
	}

	// helper 只支持 v2: 降级为 ErrNotPortable (与既有行为一致)
	rc.addAIErr = game.ErrUnsupported
	if err := r.AddAI(); err != manage.ErrNotPortable {
		t.Fatalf("unsupported 应降级为 ErrNotPortable, 得到 %v", err)
	}

	// 无扩展后端 (Unimplemented): 同样降级
	r2 := newTestRoom(t, nil)
	if err := r2.AddAI(); err != manage.ErrNotPortable {
		t.Fatalf("无后端应降级为 ErrNotPortable, 得到 %v", err)
	}
}

// TestRoomExtAddAIRegister 内核回报档案时, AI 按上游语义包装入房
// (对应 PlayerManage.addAI 的 playerGroup.add + playerAll.add)
func TestRoomExtAddAIRegister(t *testing.T) {
	rc := &fakeRoomControl{addAIProfile: &game.AIProfile{
		ID: "aihex", Name: "AI - Hard", Team: 2, Position: 5,
	}}
	r := newTestRoom(t, rc)

	if err := r.AddAI(); err != nil {
		t.Fatalf("AddAI: %v", err)
	}
	if r.PlayerManage.PlayerGroup.Size() != 1 || r.PlayerManage.PlayerAll.Size() != 1 {
		t.Fatalf("AI 应进房: group=%d all=%d",
			r.PlayerManage.PlayerGroup.Size(), r.PlayerManage.PlayerAll.Size())
	}

	p := r.PlayerManage.GetPlayer(5)
	if p == nil {
		t.Fatal("GetPlayer(5) 应按 index 找到 AI")
	}
	if p.Name() != "AI - Hard" || p.Team() != 2 || !p.Data().Bot {
		t.Fatalf("AI 档案字段 = name=%s team=%d bot=%v", p.Name(), p.Team(), p.Data().Bot)
	}

	// 引擎未创建 (无空位): 档案为 nil, 房间不变化
	rc.addAIProfile = nil
	if err := r.AddAI(); err != nil {
		t.Fatalf("AddAI (无档案): %v", err)
	}
	if r.PlayerManage.PlayerGroup.Size() != 1 {
		t.Fatal("无档案时不应登记")
	}

	// AI 的 kick 自移除: 上游 AiPlayer.kickPlayer 的房间列表清理语义
	p.KickPlayer("remove", 0)
	if r.PlayerManage.PlayerGroup.Size() != 0 || r.PlayerManage.PlayerAll.Size() != 0 {
		t.Fatal("KickPlayer 后 AI 应移出房间列表")
	}
}

// TestRoomExtSyncAllPlayer SyncAllPlayer 转发; 降级为空操作
func TestRoomExtSyncAllPlayer(t *testing.T) {
	rc := &fakeRoomControl{}
	r := newTestRoom(t, rc)

	if err := r.SyncAllPlayer(); err != nil {
		t.Fatalf("SyncAllPlayer: %v", err)
	}
	if rc.syncCnt != 1 {
		t.Fatalf("SyncAllPlayer 应转发到内核, cnt = %d", rc.syncCnt)
	}

	rc.syncErr = game.ErrUnsupported
	if err := r.SyncAllPlayer(); err != nil {
		t.Fatalf("unsupported 应降级为空操作, 得到 %v", err)
	}

	r2 := newTestRoom(t, nil)
	if err := r2.SyncAllPlayer(); err != nil {
		t.Fatalf("无后端应降级为空操作, 得到 %v", err)
	}
}

// TestRoomExtRemovePlayer RemovePlayer: 引擎移除成功后清房间列表
//
// 位置语义与上游一致: 0 基 (getPlayer(index)), SetPosition 是 1 基入口.
func TestRoomExtRemovePlayer(t *testing.T) {
	rc := &fakeRoomControl{}
	r := newTestRoom(t, rc)

	data := player.NewData()
	data.Apply(func(d *player.Data) {
		d.Name = "ai1"
		d.ConnectHexID = "aihex1"
	})
	p := r.PlayerManage.AddAbstractPlayer(nil, data, nil)
	p.SetPosition(4) // Index() == 3

	if err := r.RemovePlayer(3); err != nil {
		t.Fatalf("RemovePlayer: %v", err)
	}
	if len(rc.removed) != 1 || rc.removed[0] != 3 {
		t.Fatalf("应按位置转发到内核, removed = %v", rc.removed)
	}
	if r.PlayerManage.GetPlayer(3) != nil {
		t.Fatal("引擎移除成功后应清掉房间列表里的玩家")
	}

	// 引擎侧失败时不动房间列表
	rc.removeErr = errors.New("boom")
	data2 := player.NewData()
	data2.Apply(func(d *player.Data) {
		d.Name = "ai2"
		d.ConnectHexID = "aihex2"
	})
	p2 := r.PlayerManage.AddAbstractPlayer(nil, data2, nil)
	p2.SetPosition(5) // Index() == 4
	if err := r.RemovePlayer(4); err == nil {
		t.Fatal("引擎移除失败应报错")
	}
	if r.PlayerManage.GetPlayer(4) == nil {
		t.Fatal("引擎移除失败时不应动房间列表")
	}

	// 降级路径
	rc.removeErr = game.ErrUnsupported
	if err := r.RemovePlayer(4); err != manage.ErrNotPortable {
		t.Fatalf("unsupported 应降级为 ErrNotPortable, 得到 %v", err)
	}
}

// TestRoomExtGameResult GameResult 转发; 降级为"无数据"
func TestRoomExtGameResult(t *testing.T) {
	want := &event.GameOverData{GameTime: 42, WinPlayerList: []string{"a"}}
	rc := &fakeRoomControl{result: want, resultOK: true}
	r := newTestRoom(t, rc)

	data, ok, err := r.GameResult()
	if err != nil || !ok || data != want {
		t.Fatalf("GameResult = %v/%v/%v", data, ok, err)
	}

	rc.resultErr = game.ErrUnsupported
	if data, ok, err := r.GameResult(); err != nil || ok || data != nil {
		t.Fatalf("unsupported 应降级为无数据, 得到 %v/%v/%v", data, ok, err)
	}

	r2 := newTestRoom(t, nil)
	if data, ok, err := r2.GameResult(); err != nil || ok || data != nil {
		t.Fatalf("无后端应降级为无数据, 得到 %v/%v/%v", data, ok, err)
	}
}

// TestGrFiresGameOverWithData cleanData 把暂存的结算数据随事件发出并清空
func TestGrFiresGameOverWithData(t *testing.T) {
	r := newTestRoom(t, &fakeSim{})
	want := &event.GameOverData{GameTime: 42}

	var got *event.GameOverData
	event.Register(r.EventManage, func(ev *event.ServerGameOverEvent) { got = ev.GameOverData })

	r.SetGameOverData(want)
	r.Gr()

	if got != want {
		t.Fatalf("ServerGameOverEvent 应携带结算数据, 得到 %v", got)
	}
	// 上游 fire 之后 gameOverData = null: 再次 gr 不应带旧数据
	r.mu.Lock()
	r.forcedReturn = false
	r.mu.Unlock()
	got = nil
	r.Gr()
	if got != nil {
		t.Fatalf("结算数据只应发出一次, 得到 %v", got)
	}
}
