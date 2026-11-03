package player

// AiPlayer 对应 AiPlayer: 电脑玩家
//
// 上游继承 PlayerHess 并覆盖 isAi / kickPlayer. Go 无虚方法, 覆盖语义这样表达:
//   - isAi      -> IsAi() 方法 (嵌入的 Player 返回 false, AiPlayer 返回 true;
//     管理层需要区分时用类型断言, 见 PlayerManage.AddAI 的注释)
//   - kickPlayer -> Player.RemoveOnKick 钩子: 上游覆盖里"removePlayer + 从房间
//     移除自己"两步在构造时注入, 存储为 *Player 时行为依然正确
//
// 上游 kickPlayer 顺序: playerPrivateData.removePlayer() ->
// con.room.playerManage.playerGroup/playerAll.remove(this) -> super.kickPlayer.
// Go 版 player 包不依赖房间/管理层 (避免环), 移除动作由管理层注入 RemoveHook.
type AiPlayer struct {
	*Player
}

// NewAiPlayer 对应 AiPlayer(con, playerPrivateData)
//
// removeHook 为"从房间 playerGroup/playerAll 移除自己"的动作, 由 PlayerManage
// 注入; 传 nil 时只保留 removePlayer 语义.
func NewAiPlayer(con PlayerConnect, i18n func(key string, params ...any) string, data *Data, removeHook func()) *AiPlayer {
	p := New(con, i18n, data)
	p.RemoveOnKick = func() {
		// 对应 playerPrivateData.removePlayer(): 引擎侧移除 (当前协议未支持, 空操作)
		p.Data().RemovePlayer()
		if removeHook != nil {
			removeHook()
		}
	}
	return &AiPlayer{Player: p}
}

// IsAi 对应 override val isAi = true
func (a *AiPlayer) IsAi() bool { return true }
