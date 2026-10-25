package app

import (
	"path/filepath"
	"strings"

	"rwhps/internal/data/global"
	"rwhps/internal/game"
	"rwhps/internal/game/external"
	"rwhps/internal/util/file"
	"rwhps/internal/util/log"
)

// anchorPath 把"看起来像路径"的相对路径锚到数据根目录
//
// 原版所有资源路径都挂在数据根目录下 (file.GetFolder 体系), 配置里写相对路径
// 是常见写法. 这里统一锚定, 否则 helper 能不能找到取决于进程的 CWD, 而子进程
// 还会继承这个 CWD —— 同一个配置换个目录启动就时好时坏, 很难查.
//
// 只有含分隔符的相对路径才锚定: 裸名字 (java) 是 PATH 查找, 锚定反而会破坏它.
func anchorPath(p string) string {
	if p == "" || filepath.IsAbs(p) || !strings.ContainsAny(p, `/\`) {
		return p
	}
	return filepath.Join(file.BasePath(), p)
}

// loadSimulation 把游戏内核装到全局 game.Backend 上
//
// 对应原版的哪一行: 没有. 原版自己就跑在 JVM 里, HeadlessProxyClass() 用 ASM
// 改写 rusted-warfare.jar 的字节码之后, 游戏内核直接就在进程内了, 不存在
// "装载"这个动作. Go 版多出这一步, 恰恰是因为内核只能跑在外面.
//
// 返回值直接交给 game.SetBackend:
//   - nil 表示没有外部依赖, 退回 game.Unimplemented, 开局返回可读的错误
//   - 非 nil 表示外部进程后端已就位 (进程尚未启动, 是惰性的)
//
// 这里只做"参数是否齐备 + 文件是否存在"的静态检查, 不启动子进程: 配置错了
// 应该在启动日志里看见一句话, 而不是等到第一次开局才以一个奇怪的 EOF 炸出来.
func loadSimulation() game.Simulation {
	cfg := global.Config

	if strings.TrimSpace(cfg.GameHelperPath) == "" {
		// 没配就是没有. 不打日志 —— 这是默认状态, 每次启动都喊一声只是噪音.
		return nil
	}

	backend, err := external.New(external.Config{
		JavaPath:   anchorPath(cfg.JavaPath),
		HelperPath: anchorPath(cfg.GameHelperPath),
		ExtraArgs:  cfg.GameHelperArgs,
	})
	if err != nil {
		// 与 loadLang 的策略一致: 外部依赖不可用不该让服务器起不来,
		// 回落到无后端状态, 开局时给出 ErrNotImplemented.
		log.Warnf("[GameCore]", "external game core unavailable, falling back to no backend: {0}", err)
		return nil
	}

	// 服务器退出时外部进程要跟着走: 先 stop 再关 stdin, 最后 Kill, 全程由
	// Backend.Close 收尾. 挂到 CloseServerHook 上是为了和停止端口监听走同一条
	// 退出路径 —— 原版 closeServer 也会连带停掉游戏模块.
	prevHook := global.CloseServerHook
	global.CloseServerHook = func() {
		prevHook()
		if err := backend.Close(); err != nil {
			log.Errorf("[GameCore]", "close helper: {0}", err)
		}
	}

	// 用 Clog 而不是 Infof: 这一行是启动状态, 该和 server.hi 它们一样始终可见
	// (Clog 走 LevelConsole, 不被 logGrade 过滤), 而且要走语言包 ——
	// 外部内核接上是用户最需要在启动时确认的一件事.
	log.Clog(tr("server.load.gameCore", cfg.GameHelperPath))
	return backend
}
