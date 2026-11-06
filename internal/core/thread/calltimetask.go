// Package thread 对应 net.rwhps.server.core.thread
//
// 包含 CallTimeTask (任务标识枚举) 与 Threads (线程池 + 计时器的统一入口).
package thread

// CallGroupData 对应 CallGroupData
//
// 原版是 internal object 里的两个常量, 只有 CallTimeTask 用得到, 因此合成一个文件.
const (
	// GroupCoreServer 对应 coreServer
	GroupCoreServer = "[Core]Server"
	// GroupCoreExpandServer 对应 coreExpandServer
	GroupCoreExpandServer = "[CoreX]Server"
)

// CallTimeTask 对应 CallTimeTask
//
// 原注释: 每个枚举值携带 (group, description) 两个属性, 作为 Quartz JobKey 的
// (name, group) 和触发器描述. 名称与分组必须与原版逐一对应, 否则已排程任务的
// 键会变, 跨版本的行为不一致.
type CallTimeTask int

const (
	CallTeamTask CallTimeTask = iota
	PlayerAfkTask
	GameOverTask
	VoteTask
	AutoStartTask
	AutoUpdateMapsTask
	AutoCheckTask

	CustomUpServerListTask
	UpServerListTask
	UpServerListNewTask

	BlackListCheckTask

	ServerUploadDataTask
	ServerHostKeepAlive
	ServerUploadDataCheckTimeTask
	RelayRoomCheckTimeTask
	ServerStatusUpdate

	ServerUpStatistics
)

// callTimeTaskData 对应枚举构造器里的 (group, description) 参数表
var callTimeTaskData = [...]struct {
	group       string
	description string
}{
	CallTeamTask:                  {GroupCoreServer, "Update player team list"},
	PlayerAfkTask:                 {GroupCoreServer, "Transfer of authority"},
	GameOverTask:                  {GroupCoreServer, "Check gameover"},
	VoteTask:                      {GroupCoreServer, "Vote"},
	AutoStartTask:                 {GroupCoreExpandServer, "Start automatically"},
	AutoUpdateMapsTask:            {GroupCoreExpandServer, "Auto update maps"},
	AutoCheckTask:                 {GroupCoreExpandServer, "Auto Check player survives"},
	CustomUpServerListTask:        {"UpList", "[Plugin UpList] Update Data"},
	UpServerListTask:              {"UpList", "Core"},
	UpServerListNewTask:           {"UpList", "Core"},
	BlackListCheckTask:            {"[BlackList]", "超时去除"},
	ServerUploadDataTask:          {"[RCN]", "列表更新"},
	ServerHostKeepAlive:           {"[RCN]", "保活"},
	ServerUploadDataCheckTimeTask: {"[RCN]", "超时"},
	RelayRoomCheckTimeTask:        {"[RCN]", "房间超时机制"},
	ServerStatusUpdate:            {"[RCN]", "状态更新"},
	ServerUpStatistics:            {"[RW-HPS]", "统计数据更新"},
}

// Group 对应 CallTimeTask.group
func (t CallTimeTask) Group() string { return callTimeTaskData[t].group }

// Description 对应 CallTimeTask.description
func (t CallTimeTask) Description() string { return callTimeTaskData[t].description }

// String 对应 Kotlin data class / enum 默认的 toString: 枚举名
func (t CallTimeTask) String() string { return callTimeTaskNames[t] }

// callTimeTaskNames 对应 Enum.name
var callTimeTaskNames = [...]string{
	CallTeamTask:                  "CallTeamTask",
	PlayerAfkTask:                 "PlayerAfkTask",
	GameOverTask:                  "GameOverTask",
	VoteTask:                      "VoteTask",
	AutoStartTask:                 "AutoStartTask",
	AutoUpdateMapsTask:            "AutoUpdateMapsTask",
	AutoCheckTask:                 "AutoCheckTask",
	CustomUpServerListTask:        "CustomUpServerListTask",
	UpServerListTask:              "UpServerListTask",
	UpServerListNewTask:           "UpServerListNewTask",
	BlackListCheckTask:            "BlackListCheckTask",
	ServerUploadDataTask:          "ServerUploadDataTask",
	ServerHostKeepAlive:           "ServerHostKeepAlive",
	ServerUploadDataCheckTimeTask: "ServerUploadData_CheckTimeTask",
	RelayRoomCheckTimeTask:        "RelayRoom_CheckTimeTask",
	ServerStatusUpdate:            "ServerStatusUpdate",
	ServerUpStatistics:            "ServerUpStatistics",
}
