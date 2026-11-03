// Package command 对应 net.rwhps.server.command
//
// 包含 CoreCommands (help/stop/version/setlanguage/exit/plugins/plugin/tryupdate)、
// CommandsEx (hi) 与 HessMain 里的 start, 以及 CoreCommands 的 startrelay 系列.
//
// 与原版差异 (逐条在各自命令处说明):
//   - 插件系统与 Relay 尚未移植, plugin / plugins / startrelay / startrelaytest
//     只给出明确的"未实现"输出, 不伪造成功
//   - tryupdate 通过下载 GitHub Release 差分补丁来改写核心 jar, Go 版没有 jar,
//     因此只报告不支持
//   - start 走 game.Simulation 接缝, 没有后端时返回 ErrNotImplemented
package command

import (
	"rwhps/internal/game/room"
	"rwhps/internal/net/core"
)

// Deps 收集命令层需要、但无法直接 import 的外部能力
//
// 这些点都落在 app 启动链上, 而 app 会 import 本包, 直接反向依赖会形成循环,
// 因此和 log 包的 Bundle 一样用注入的方式解耦. 未注入的字段一律降级为无操作.
type Deps struct {
	// Print 对应 StrCons: 命令输出回调
	Print func(string)

	// Exit 对应 Data.core.exit(run): 关闭服务器, 执行 run 后退出进程
	Exit func(run func())

	// SetLanguage 对应 Initialization.initServerLanguage
	SetLanguage func(lang string)

	// Plugins 对应 PluginManage.run: 遍历已加载插件
	Plugins func(each func(name, description, author, version string))

	// PluginCommand 对应 PluginCenter.pluginCenter.command
	PluginCommand func(text string, out func(string))

	// ServerStartType 对应 PluginManage.runGlobalEventManage(ServerStartTypeEvent)
	ServerStartType func(t core.NetType)

	// Session 对应 HeadlessModuleManage.hps.room (单房间模式下的那一间)
	// nil 表示还没有房间
	Session *room.ServerRoom
}

// NewDeps 返回一套全部降级为无操作的依赖, 便于测试与早期启动阶段使用
//
// 注意: 默认的 Exit 只是执行传入的收尾函数, 不会真的结束进程 ——
// 真正的退出流程由 app 启动链注入.
func NewDeps() *Deps {
	return &Deps{
		Print: func(string) {},
		Exit: func(run func()) {
			if run != nil {
				run()
			}
		},
		SetLanguage:     func(string) {},
		Plugins:         func(func(string, string, string, string)) {},
		PluginCommand:   func(string, func(string)) {},
		ServerStartType: func(core.NetType) {},
	}
}

// print 安全地调用输出回调
func (d *Deps) print(text string) {
	if d.Print != nil {
		d.Print(text)
	}
}
