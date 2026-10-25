// Package command 对应 net.rwhps.server.command.CoreCommands
package command

import (
	"runtime"
	"strings"

	"rwhps/internal/data/global"
	"rwhps/internal/net"
	"rwhps/internal/net/core"
	cmd "rwhps/internal/util/game/command"
	"rwhps/internal/util/log"
)

// tr 复用 log 包注入的取词函数, 避免出现第二套 i18n 注入机制
func tr(key string, params ...any) string {
	if log.Bundle == nil {
		return key
	}
	return log.Bundle(key, params...)
}

// usage 拼出 "text <param>" 形式的用法串 (对应 help 里的拼接逻辑)
func usage(c *cmd.Command) string {
	if c.ParamText == "" {
		return c.Text
	}
	return c.Text + " " + c.ParamText
}

// registerCore 对应 CoreCommands.registerCore
func registerCore(h *cmd.Handler, d *Deps) {
	h.Register("help", "serverCommands.help", func(_ []string, _ any) {
		d.print("Commands:")
		for _, c := range h.CommandList() {
			switch {
			case strings.HasPrefix(c.Description, "#"):
				// 以 # 开头的描述直接输出原文 (跳过 i18n)
				d.print("   " + usage(c) + " - " + c.Description[1:])
			case c.Description == "HIDE":
				continue
			default:
				d.print("   " + usage(c) + " - " + tr(c.Description))
			}
		}
	})

	// HIDE: 不在 help 里出现
	h.Register("stop", "HIDE", func(_ []string, _ any) {
		services := net.NetStaticData.Services()
		if len(services) == 0 {
			d.print("Server does not start")
			return
		}
		// 对应 NetServer.closeServer(): 逐个 stop
		for _, s := range services {
			s.Stop()
		}
	})

	h.Register("version", "serverCommands.version", func(_ []string, _ any) {
		rw := net.NetStaticData.RwHps()
		d.print(tr("status.versionS", heapMB(), global.ServerCoreVersion, rw.NetType().String()))

		switch {
		case core.CheckProtocolIsServer(rw.NetType()):
			// 对应 HeadlessModuleManage.hessLoaderMap.values.forEach
			if d.Session == nil {
				return
			}
			st := d.Session.Snapshot()
			d.print(tr("status.versionS.server",
				st.MapName,
				d.Session.PlayerCount(),
				rw.NetType().String(),
				classLoaderName(),
				st.RoomID,
			))
		case rw.NetType() == core.RelayProtocol || rw.NetType() == core.RelayMulticastProtocol:
			total := 0
			for _, s := range net.NetStaticData.Services() {
				total += s.GetConnectSize()
			}
			d.print(tr("status.versionS.relay", total))
		}
	})

	h.RegisterWith("setlanguage", "<Language>", "serverCommands.setlanguage", func(args []string, _ any) {
		d.SetLanguage(args[0])
	})

	h.Register("exit", "serverCommands.exit", func(_ []string, _ any) {
		d.Exit(nil)
	})
}

// registerInfo 对应 CoreCommands.registerInfo
func registerInfo(h *cmd.Handler, d *Deps) {
	h.Register("plugins", "serverCommands.plugins", func(_ []string, _ any) {
		d.Plugins(func(name, description, author, version string) {
			d.print(tr("plugin.info", name, description, author, version))
		})
	})
}

// registerCorex 对应 CoreCommands.registerCorex
func registerCorex(h *cmd.Handler, d *Deps) {
	h.RegisterWith("plugin", "<TEXT...>", "serverCommands.plugin", func(args []string, _ any) {
		d.PluginCommand(args[0], d.print)
	})

	h.Register("tryupdate", "serverCommands.tryUpdate", func(_ []string, _ any) {
		// 原版: 拉取 GitHub Release 列表, 下载 *.patch, 用 DiffUpdate 把补丁叠加到
		// 自己的核心 jar 上, 再在退出时覆盖 jar 文件.
		// Go 版没有可打补丁的 jar, 也没有等价的分发形态, 这里只如实报告不支持.
		d.print("Self-update is not supported in the Go port (there is no core jar to patch)")
	})
}

// heapMB 对应原版传给 status.versionS 的 SystemUtils.javaHeap / 1024 / 1024
//
// Go 版没有 JVM 堆, 用 runtime 向操作系统申请到的总字节数 (MemStats.Sys) 作为等价指标.
func heapMB() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Sys / 1024 / 1024
}

// classLoaderName 对应原版打印的 useClassLoader
//
// Go 版没有类加载器概念 (也没有插件隔离), 返回空串.
func classLoaderName() string { return "" }
