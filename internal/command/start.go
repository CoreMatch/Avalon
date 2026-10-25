// Package command 对应 CoreCommands.registerStartServer 与 HessMain.registerCoreCommands
package command

import (
	"strings"

	"rwhps/internal/data/global"
	"rwhps/internal/net"
	"rwhps/internal/net/core"
	cmd "rwhps/internal/util/game/command"
	"rwhps/internal/util/log"
)

// registerStart 对应 CoreCommands.registerStartServer
func registerStart(h *cmd.Handler, d *Deps) {
	// 原版: 注册 Relay 客户端命令 -> 重置日志等级 -> 以 RelayProtocol 启动服务.
	// Relay 属于后续批次, 这里只报告未实现, 不去启动一个没有协议实现的监听.
	h.Register("startrelay", "serverCommands.start", func(_ []string, _ any) {
		if len(net.NetStaticData.Services()) > 0 {
			d.print("The server is not closed, please close")
			return
		}
		d.print("Relay is not implemented in the Go port yet")
	})

	h.Register("startrelaytest", "serverCommands.start", func(_ []string, _ any) {
		if len(net.NetStaticData.Services()) > 0 {
			d.print("The server is not closed, please close")
			return
		}
		d.print("Relay is not implemented in the Go port yet")
	})

	// HIDE: 由 startrelay / 启动链内部调用, 参数为 IRwHps 实例
	h.RegisterWith("startnetservice", "<id> <isPort> [sPort] [ePort]", "HIDE", func(args []string, param any) {
		proto, _ := param.(core.IRwHps)
		if proto == nil {
			log.Clog("[Start Service] No parameter")
			return
		}

		// 原版 arg[1].toBoolean(): 只有 "true" (忽略大小写) 为真
		if strings.EqualFold(args[1], "true") {
			ns := net.NewNetService(args[0], proto)

			// 原版对 Relay 协议设置 workThreadCount = 3000; Go 版按连接起 goroutine,
			// 没有等价的工作线程数配置, 故不设置.

			// 对应 Threads.newThreadCoreNet { ... openPort(...) }
			go func() {
				if len(args) > 3 {
					sPort := atoiOr(args[2], 0)
					ePort := atoiOr(args[3], 0)
					ns.OpenPortRange(global.Config.Port, sPort, ePort)
					return
				}
				ns.OpenPort(global.Config.Port)
			}()
		}

		d.ServerStartType(proto.NetType())
	})
}

// registerStartGame 对应 HessMain.registerCoreCommands 里的 start
//
// 原版在此加载 GameModularReusableLoadClass 并执行 GameStartInit.init/start ——
// 也就是把闭源的 rusted-warfare.jar 拉进 JVM. Go 版走 game.Simulation 接缝,
// 没有后端时 StartGame 返回 game.ErrNotImplemented.
func registerStartGame(h *cmd.Handler, d *Deps) {
	h.Register("start", "serverCommands.start", func(_ []string, _ any) {
		if global.StartServer {
			d.print("The server is not closed, please close")
			return
		}
		if d.Session == nil {
			d.print("No room available")
			return
		}

		global.StartServer = true

		// 与原版的差异: 原版启动不会失败, 因此 StartServer 一旦置 true 就保持.
		// Go 版 StartGame 可能返回 ErrNotImplemented, 失败时把标志复位,
		// 否则服务器会永久卡在"已启动"状态, 连重试的机会都没有.
		if err := d.Session.StartGame(); err != nil {
			global.StartServer = false
			// 用 LogError 而不是 Error: 后者把 error 当作 tag, 正文为空, 消息会丢
			log.LogError("start", err)
			return
		}

		log.Clog(tr("server.load.headless"))
		log.Set(strings.ToUpper(global.Config.Log))
	})
}

// atoiOr 解析失败时返回默认值 (对应 Kotlin 的 toIntOrNull 场景)
func atoiOr(s string, def int) int {
	n := 0
	if s == "" {
		return def
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return def
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}
