// Package app 对应 net.rwhps.server.Main.main.
package app

import (
	"rwhps/internal/command"
	"rwhps/internal/data/bean"
	"rwhps/internal/data/global"
	"rwhps/internal/game"
	"rwhps/internal/game/manage"
	"rwhps/internal/game/room"
	"rwhps/internal/net"
	"rwhps/internal/net/core"
	cmd "rwhps/internal/util/game/command"
	"rwhps/internal/util/log"
)

// Start 对应 Main.main 从 SystemSetProperty 到 newThreadCore(inputMonitor) 之间的全部流程
//
// 顺序与原版逐行对应, 每一步的"为什么"写在各段注释里. 没有移植的步骤不是省略,
// 而是明确标注了原因 —— 尤其是插件系统 (batch-1 决定先不做) 与 ASM 字节码改写 (Go 做不到).
func (r *Runtime) Start() error {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return nil
	}
	r.started = true
	r.mu.Unlock()

	// SystemSetProperty.setJlineIdea / setOnlyIpv4 / setAwtHeadless:
	// 三个都是给 JLine 与 AWT 用的 JVM 参数, Go 版没有对应物.

	// Statisticians.addTime("Core.Main")
	global.AddTime("Core.Main")

	// set("TRACK"); Log.set("TRACK")
	log.Set("TRACK")
	// 原版 Log 对象静态初始化时就绑定了日志文件; Go 版需要显式指定目录.
	log.LogPath(global.ServerLogPath)

	// Logger.getLogger("io.netty").level = Level.OFF: 没有 netty, 也没有 JUL 日志器.

	// inputMonitorInit()
	r.initConsole()

	// Initialization() 的构造阶段: 环境检查 + 语言包 + 统计上报定时器.
	// 统计上报 (initGetServerData) 每 60s 连一次 relay.der.kim:6001 上报服务器信息,
	// 是一条对外遥测通道, 不属于服务器自身逻辑, 这里不移植.
	checkEnvironment()
	if err := loadLang(); err != nil {
		return err
	}

	log.Clog(tr("server.login"))
	log.Clog("Load ing...")

	global.Config = bean.StringToCoreConfig()
	global.ConfigServer = bean.StringToServerConfig()
	global.ConfigRelay = bean.StringToRelayConfig()

	// 原版房间由游戏模块加载时创建 (HeadlessModuleManage.hps.room);
	// Go 版没有游戏模块, 这里建一个绑定 game.Backend 的空房间,
	// 让 start / hi / version 有确定的对象可读. 没有后端时 start 会返回 ErrNotImplemented.
	//
	// 后端必须在建房间之前装好: 房间在构造时就把 game.Backend 取走了.
	// 对应原版的哪一行: 没有 —— 原版的内核在进程内, 见 loadSimulation 的说明.
	game.SetBackend(loadSimulation())
	// ServerRoom 在 GameSession 状态壳之上装配玩家/广播/命令实体链 (里程碑 2).
	r.Session = room.NewServerRoom(game.NewGameSession(global.ConfigServer.ServerID, game.Backend))
	// 连接层经 room.Default() 取单例房间, 对应上游
	// HeadlessModuleManage.hessLoaderMap[classLoader].room 的取用路径.
	room.SetDefault(r.Session)

	// startInit() + loadLib()
	startInit(r.Core.Settings)
	loadLib()

	log.Clog(tr("server.hi"))
	log.Clog(tr("server.project.url"))
	log.Clog(tr("server.thanks"))

	// HeadlessProxyClass(): 在 JVM 内用 ASM 改写 rusted-warfare.jar 的字节码.
	// Go 进程无法执行 Java 字节码, 也没有等价的字节码改写手段 —— 见 game.Simulation 的说明.

	// 命令加载
	log.LoadStatusLog("server.load.command", func() {
		command.Register(global.ServerCommand, global.PingCommand, r.deps())
	})

	// 插件系统 (server.load.plugin / runOnEnable / server.load.plugin.command / runInit)
	// 按 batch-1 的决定暂不移植. 原版在这里会打印 "Loading plugins...OK",
	// 而 Go 版确实什么都没加载, 因此不打印这一步 —— 不伪造加载进度.

	// Event 加载
	log.LoadStatusLog("server.load.events", func() {
		// 原版: addGlobalEventManage(EventGlobal()) + runRegisterGlobalEvents().
		// EventGlobal 的四个处理器都依赖尚未移植的子系统, 所以当前没有监听者;
		// 事件总线本身是通的, 后续批次直接 AddGlobalEventManager 即可.
	})

	log.Set(global.Config.Log)

	log.LoadStatusLog("server.load.maps", func() {
		manage.ReadMapAndSave()
	})

	log.LoadStatusLog("server.load.service", func() {
		loadService()
	})

	// 加载完毕
	log.Clog(tr("server.load.end", global.ComputeTime("Core.Main")))
	r.Events.ServerLoad()

	// 默认直接启动服务器
	resp := global.ServerCommand.HandleMessageWith(global.Config.DefStartCommand, global.DefPrint)
	if resp != nil && resp.Type != cmd.NoCommand && resp.Type != cmd.Valid {
		log.Clog(tr("server.start.defCommand"))
	}

	// 对应 Threads.newThreadCoreNet { if (Data.config.webPort != 0) { ... } }
	if global.Config.WebPort != 0 {
		go func() {
			web := net.NewNetService(net.CoreID(), manage.AddIRwHps(core.HttpProtocol, "CoreWebService"))
			web.OpenPort(global.Config.WebPort)
		}()
	}

	return nil
}
