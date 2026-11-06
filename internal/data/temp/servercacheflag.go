package temp

import "sync"

// ServerCacheFlag 对应 ServerCacheFlag: 房间内的一次性状态标志
//
// 这些标志控制"同一局里只发生一次"的行为 (只提醒一次快结束 / 只播报一次
// 剩余玩家等), 每局开始时整体重置 (见 ServerRoom.cleanData).
//
// 与上游的差异: 字段经互斥锁保护. 上游这些布尔值被检查定时器线程与
// 主线程同时读写, JVM 里是无人察觉的竞态; Go 的内存模型与 -race 下
// 必须显式同步.
type ServerCacheFlag struct {
	mu sync.Mutex

	// sendGameStatusFlag: 是否还未播报过结算数据
	sendGameStatusFlag bool
	// forcedCloseSendMsg: 超时强关前是否还没提醒过
	forcedCloseSendMsg bool
	// oneSay: 只剩一人时的倒计时提醒是否还没说过
	oneSay bool

	// ai: 房间里是否只剩 AI (由 TEAM_LIST 重写路径维护)
	ai bool
	// aiWarn: 全 AI 局的提醒是否还没说过
	aiWarn bool
}

// NewServerCacheFlag 对应构造函数默认值
//
// 上游: sendGameStatusFlag/forcedCloseSendMsg/oneSay/aiWarn 默认 true, ai 默认 false.
func NewServerCacheFlag() *ServerCacheFlag {
	f := &ServerCacheFlag{}
	f.Reset()
	return f
}

// Reset 恢复为初始默认值
//
// 对应上游 cleanData 里的 `flagData = ServerCacheFlag()`. 上游是整体换对象,
// Go 版选择原地重置: 自动检查定时器持有旧对象的引用, 换对象会让它继续
// 读写已被丢弃的实例.
func (f *ServerCacheFlag) Reset() {
	f.mu.Lock()
	f.sendGameStatusFlag = true
	f.forcedCloseSendMsg = true
	f.oneSay = true
	f.ai = false
	f.aiWarn = true
	f.mu.Unlock()
}

// SendGameStatusFlag 读 sendGameStatusFlag
func (f *ServerCacheFlag) SendGameStatusFlag() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sendGameStatusFlag
}

// SetSendGameStatusFlag 写 sendGameStatusFlag
func (f *ServerCacheFlag) SetSendGameStatusFlag(v bool) {
	f.mu.Lock()
	f.sendGameStatusFlag = v
	f.mu.Unlock()
}

// ForcedCloseSendMsg 读 forcedCloseSendMsg
func (f *ServerCacheFlag) ForcedCloseSendMsg() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.forcedCloseSendMsg
}

// SetForcedCloseSendMsg 写 forcedCloseSendMsg
func (f *ServerCacheFlag) SetForcedCloseSendMsg(v bool) {
	f.mu.Lock()
	f.forcedCloseSendMsg = v
	f.mu.Unlock()
}

// OneSay 读 oneSay
func (f *ServerCacheFlag) OneSay() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.oneSay
}

// SetOneSay 写 oneSay
func (f *ServerCacheFlag) SetOneSay(v bool) {
	f.mu.Lock()
	f.oneSay = v
	f.mu.Unlock()
}

// Ai 读 ai
func (f *ServerCacheFlag) Ai() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ai
}

// SetAi 写 ai
func (f *ServerCacheFlag) SetAi(v bool) {
	f.mu.Lock()
	f.ai = v
	f.mu.Unlock()
}

// AiWarn 读 aiWarn
func (f *ServerCacheFlag) AiWarn() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.aiWarn
}

// SetAiWarn 写 aiWarn
func (f *ServerCacheFlag) SetAiWarn(v bool) {
	f.mu.Lock()
	f.aiWarn = v
	f.mu.Unlock()
}
