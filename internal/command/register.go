// Package command 对应 Main.kt 里的命令注册入口
package command

import (
	cmd "rwhps/internal/util/game/command"
)

// Register 一次性注册全部服务端命令
//
// 对应原版的四处调用:
//
//	CoreCommands(Data.SERVER_COMMAND)   -> registerCore / registerCorex / registerInfo / registerStart
//	CommandsEx(Data.PING_COMMAND)       -> registerPingCommands
//	HessMain.registerCoreCommands       -> registerStartGame
//
// server 一般为 global.ServerCommand, ping 一般为 global.PingCommand.
//
// 未移植: CoreCommands.init 末尾那个 register("log", "[a...]") —— 它用 HotLoadClass
// 把 a.class 直接塞进 JVM, Go 没有等价能力, 也不该有.
func Register(server, ping *cmd.Handler, d *Deps) {
	if d == nil {
		d = NewDeps()
	}

	registerCore(server, d)
	registerCorex(server, d)
	registerInfo(server, d)
	registerStart(server, d)
	registerStartGame(server, d)

	// ServerCommands (Hess 控制台命令) 依赖房间实体; 房间在启动链建房后才可用,
	// 命令内部对 d.Session 做了 nil 守卫, 故可与核心命令一同注册.
	registerServerCommands(server, d)

	if ping != nil {
		registerPingCommands(ping, d)
	}
}
