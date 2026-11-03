package room

import (
	"sync"

	"rwhps/internal/core/thread"
	"rwhps/internal/data/global"
	"rwhps/internal/data/temp"
	"rwhps/internal/game"
	"rwhps/internal/game/event"
	"rwhps/internal/game/manage"
	"rwhps/internal/game/player"
	"rwhps/internal/struct/list"
	cmd "rwhps/internal/util/game/command"
	"rwhps/internal/util/log"
)

// ServerRoom 对应 ServerRoom: 服务端模式的房间实体
//
// 上游字段逐一保留; gameModule (内核模块) 的位置由嵌入的 game.GameSession
// (Simulation 接缝) 承担. 状态壳 (开局计时/地图/模拟内核) 在 GameSession 里,
// 实体链 (玩家/广播/命令/标志位) 在本类型里, 两层合起来才是完整的上游 ServerRoom.
type ServerRoom struct {
	*game.GameSession

	// ClientHandler 对应 clientHandler: 客户端命令系统 ("/" 前缀, 为多端准备)
	// 命令注册属里程碑 3, 这里先建好容器.
	ClientHandler *cmd.Handler

	// PlayerManage 对应 playerManage
	PlayerManage *manage.PlayerManage

	// TeamManage 对应 teamManage (上游为空类, 照搬占位)
	TeamManage *manage.TeamManage

	// Call 对应 call
	Call *manage.CallManage

	// EventManage 对应 eventManage: 房间的游戏事件总线
	EventManage *event.Manage

	// BanUnit 对应 banUnit: 被封禁的单位名
	BanUnit *list.Seq[string]

	// FlagData 对应 flagData: 一次性状态标志
	FlagData *temp.ServerCacheFlag

	// PlayerData 对应 playerData: 开局时快照的非 AI 玩家档案 (RePlay 素材)
	// 上游为可空 Seq, 未开局 (或未启用 saveRePlayFile) 时为 nil
	PlayerData []map[string]any

	// IsMixPlayer 对应 isMixPlayer
	IsMixPlayer bool

	mu sync.Mutex
	// forcedReturn 对应 forcedReturn: gr() 的重入守卫
	forcedReturn bool
}

// NewServerRoom 对应 ServerRoom(gameModule)
//
// 构造时即注册上游 Event() 的全部默认监听 (对应上游
// EventGlobal.registerServerHessLoadEvent 在模块加载时的 registerListener(Event())).
func NewServerRoom(session *game.GameSession) *ServerRoom {
	r := &ServerRoom{
		GameSession:   session,
		ClientHandler: cmd.NewHandler("/"),
		EventManage:   event.NewManage(),
		BanUnit:       list.NewSeq[string](),
		FlagData:      temp.NewServerCacheFlag(),
	}
	r.PlayerManage = manage.NewPlayerManage()
	r.TeamManage = manage.NewTeamManage()
	r.Call = manage.NewCallManage(r.PlayerManage, r)
	r.registerDefaultEvents()
	return r
}

// IsAfk 对应 isAfk: 直接透传配置
func (r *ServerRoom) IsAfk() bool { return global.ConfigServer.IsAfk }

// --- RoomHooks (manage.CallManage 定义的接缝) ---

// GetRoomID 对应 roomID 读
func (r *ServerRoom) GetRoomID() string { return r.GameSession.RoomID }

// GetEndTime 对应 endTime 读 (线程安全)
func (r *ServerRoom) GetEndTime() int64 { return r.GameSession.GetEndTime() }

// GetFlagData 对应 flagData 读
func (r *ServerRoom) GetFlagData() *temp.ServerCacheFlag { return r.FlagData }

// --- 状态机 ---

// StartGame 对应"开局"全链: 模拟内核开局 (GameSession.StartGame) +
// 上游 roomStartGame 的房间侧动作.
//
// 上游时序: 引擎起来后, 客户端/引擎触发 START_GAME -> isStartGame = true ->
// roomStartGame(). Go 版由 start 命令触发: 内核开局成功即等价于引擎就绪,
// 随后逐条执行 roomStartGame 的动作:
//
//	startTime/endTime 写入 (GameSession.StartGame 内)
//	Threads.pause(CallTeamTask)
//	playerAll.eachAll { updateDate() }
//	call.startCheckThread()
func (r *ServerRoom) StartGame() error {
	if err := r.GameSession.StartGame(); err != nil {
		return err
	}

	thread.Pause(thread.CallTeamTask)
	r.PlayerManage.RunPlayerArrayDataRunnable(func(p *player.Player) {
		p.UpdateDate()
	})
	r.Call.StartCheckThread()
	return nil
}

// BattleRoom 对应 battleRoom: 从对局回到可继续开局的房间状态
func (r *ServerRoom) BattleRoom() {
	thread.UnPause(thread.CallTeamTask)
	r.cleanData()
}

// Gr 对应 gr(): 强制结束本局
//
// 上游顺序: forcedReturn 重入守卫 -> cleanData -> closeTimeTask(CallTeamTask)
// -> Log.clog("[$roomID] Gameover") -> reBootServer { cleanPlayerAllData }.
//
// reBootServer 是"重启引擎网络层回到大厅"; Go 版引擎在 helper 内, 对局结束
// 后回大厅由引擎自行完成, 这里直接执行 reBootServer 回调里最终做的事.
func (r *ServerRoom) Gr() {
	r.mu.Lock()
	if r.forcedReturn {
		r.mu.Unlock()
		return
	}
	r.forcedReturn = true
	r.mu.Unlock()

	r.cleanData()
	thread.CloseTimeTask(thread.CallTeamTask, nil)

	log.Clog("[" + r.GameSession.RoomID + "] Gameover")

	r.PlayerManage.CleanPlayerAllData()
}

// GameOver 对应 gameover 命令的入口: 先停模拟内核, 再走 gr 的清理.
//
// 与上游的差异说明: 上游 gameover 命令经引擎结束对局, 引擎再回调 gr;
// Go 版命令直接驱动 —— 先 StopSimulation (等价引擎侧结束), 再 Gr.
// 遮蔽嵌入的 GameSession.GameOver: 持有 *ServerRoom 的调用方走本方法.
func (r *ServerRoom) GameOver() error {
	if err := r.GameSession.StopSimulation(); err != nil {
		return err
	}
	r.Gr()
	return nil
}

// voteStopper 是房间对投票对象的最小依赖面.
//
// 上游 cleanThread 直接调 Data.vote!!.stopVote(); Go 版 Vote 位于命令层
// (internal/command/ex), room 不能反向导入, 经 global.Vote 槽位 + 窄接口解耦.
type voteStopper interface{ StopVote() }

// cleanThread 对应 cleanThread
//
// 上游: 停止投票 + 关 GameOverTask/AutoCheckTask.
func (r *ServerRoom) cleanThread() {
	if v, ok := global.Vote.(voteStopper); ok && v != nil {
		v.StopVote()
	}
	thread.CloseTimeTask(thread.GameOverTask, nil)
	thread.CloseTimeTask(thread.AutoCheckTask, nil)
}

// cleanData 对应 cleanData
//
// 逐条对应:
//
//	isStartGame = false / startTime = 0 / endTime = 0  -> GameSession.ResetMatchState
//	cleanThread()
//	flagData = ServerCacheFlag()                        -> FlagData.Reset (原地重置, 见 Reset 注释)
//	banUnit.clear()
//	forcedReturn = false
//	eventManage.fire(ServerGameOverEvent(gameOverData)).await(); gameOverData = null
//	MapManage.maps.mapData?.clean()                     -> manage.Maps
func (r *ServerRoom) cleanData() {
	r.GameSession.ResetMatchState()
	r.cleanThread()

	r.FlagData.Reset()

	r.BanUnit.Clear()

	r.mu.Lock()
	r.forcedReturn = false
	r.mu.Unlock()

	// 上游 gameOverData 由引擎结算产出; Go 版引擎在 helper 内, 结算数据尚无
	// 协议通道 (DECISIONS D-9), 与上游字段为 null 的声明一致, 以 nil 触发.
	r.EventManage.Fire(&event.ServerGameOverEvent{GameOverData: nil})

	if manage.Maps.MapData != nil {
		manage.Maps.MapData.Clean()
	}
}

// --- 单例槽位 ---
//
// 对应上游 HeadlessModuleManage.hessLoaderMap[classLoader].room 的取用路径:
// 上游按 classLoader 区分多个游戏模块实例; Go 版无类加载器维度, 服务端模式
// 恒为单房间, 收敛为包级槽位 (DECISIONS D-5).

var (
	defaultRoomMu sync.RWMutex
	defaultRoom   *ServerRoom
)

// SetDefault 由启动链在创建房间后调用; 传 nil 清除
func SetDefault(r *ServerRoom) {
	defaultRoomMu.Lock()
	defaultRoom = r
	defaultRoomMu.Unlock()
}

// Default 取当前单例房间; 未设置时为 nil
func Default() *ServerRoom {
	defaultRoomMu.RLock()
	defer defaultRoomMu.RUnlock()
	return defaultRoom
}
