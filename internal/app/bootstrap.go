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
	nethttp "rwhps/internal/net/http"
	"rwhps/internal/net/httpapi"
	"rwhps/internal/net/uplist"
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

	// Cascade 后端契约装配 (方案 B, DECISIONS D-17~D-21): 启用时建网关并
	// 起 /v1 控制面; 未启用时无操作. 材料缺失会使启动失败 (显式配置不降级).
	if err := loadCascadeGateway(); err != nil {
		return err
	}

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
		// 对应上游引擎模块加载时的 ClientCommands(room.clientHandler):
		// 客户端聊天 "/" 命令挂到房间自己的 Handler 上.
		command.RegisterClientCommands(r.Session.ClientHandler, r.Session)
	})

	// 插件系统 (server.load.plugin / runOnEnable / server.load.plugin.command / runInit)
	// 插件加载框架本身 (JVM 类加载) 不移植; 原内置插件承载的具体功能以普通 Go
	// 代码内建, 在此按上游插件阶段的时序接线 (里程碑 5):
	//   - UpList: runRegisterCoreCommands -> uplist.RegisterCommands
	//             runRegisterServerClientCommands / runRegisterEvents 对应
	//             上游在 ServerHessLoadEvent 时的接线, Go 版收敛到建房后一次性完成
	//   - HttpApi: runOnEnable -> httpapi.Register (WebData 路由),
	//              registerEvents -> httpapi.RegisterRoomEvents (结算缓存)
	uplist.RegisterCommands(global.ServerCommand)
	uplist.RegisterClientCommands(r.Session.ClientHandler)
	uplist.RegisterRoomEvents(r.Session)
	httpapi.Register(nethttp.DefaultWebData)
	httpapi.RegisterRoomEvents(r.Session)

	// Event 加载
	log.LoadStatusLog("server.load.events", func() {
		// 对应原版 addGlobalEventManage(EventGlobal()) + runRegisterGlobalEvents().
		r.registerEventGlobal()
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
