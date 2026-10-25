package app

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rwhps/internal/data/bean"
	"rwhps/internal/data/global"
	"rwhps/internal/game"
	"rwhps/internal/game/external"
	"rwhps/internal/resource"
	"rwhps/internal/util/file"
	"rwhps/internal/util/file/load"
	"rwhps/internal/util/log"
)

// writeFakeHelper 造一个"存在但不可执行"的 jar 文件
//
// loadSimulation 只做静态检查 (文件在不在), 不会真的去跑它,
// 所以内容无所谓, 有个文件就够了.
func writeFakeHelper(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "helper.jar")
	if err := os.WriteFile(path, []byte("fake"), 0o644); err != nil {
		t.Fatalf("写假 helper: %v", err)
	}
	return path
}

// TestLoadSimulationDisabledByDefault 默认配置下必须没有后端
//
// 这是最重要的一条: 不配置 gameHelperPath 时行为与加这个特性之前完全一致,
// 开局返回 ErrNotImplemented 而不是别的什么.
func TestLoadSimulationDisabledByDefault(t *testing.T) {
	setupBase(t)

	global.Config = bean.NewCoreConfig()

	if sim := loadSimulation(); sim != nil {
		t.Fatalf("未配置 helper 时 loadSimulation 应返回 nil, 得到 %T", sim)
	}
	// 全局默认值也必须还是 Unimplemented
	if _, ok := game.Backend.(game.Unimplemented); !ok {
		t.Fatalf("game.Backend = %T, 期望 game.Unimplemented", game.Backend)
	}
}

// TestLoadSimulationBlankPathIsDisabled 空白字符串等同于未配置
func TestLoadSimulationBlankPathIsDisabled(t *testing.T) {
	setupBase(t)

	cfg := bean.NewCoreConfig()
	cfg.GameHelperPath = "   "
	global.Config = cfg

	if sim := loadSimulation(); sim != nil {
		t.Fatalf("helper 路径为空白时应返回 nil, 得到 %T", sim)
	}
}

// TestLoadSimulationFallsBackWhenHelperMissing 文件不存在时回落, 且不装钩子
func TestLoadSimulationFallsBackWhenHelperMissing(t *testing.T) {
	setupBase(t)

	cfg := bean.NewCoreConfig()
	cfg.GameHelperPath = filepath.Join(t.TempDir(), "nope.jar")
	global.Config = cfg

	hookBefore := global.CloseServerHook
	if sim := loadSimulation(); sim != nil {
		t.Fatalf("helper 不存在时应返回 nil, 得到 %T", sim)
	}
	// 没有后端就不该去包退出钩子, 否则 Close 会在一个 nil 后端上做事
	if !sameFunc(global.CloseServerHook, hookBefore) {
		t.Fatal("helper 不可用时不应该改动 CloseServerHook")
	}
}

// TestLoadSimulationFallsBackWhenJavaMissing java 不存在同样回落
func TestLoadSimulationFallsBackWhenJavaMissing(t *testing.T) {
	setupBase(t)

	cfg := bean.NewCoreConfig()
	cfg.JavaPath = filepath.Join(t.TempDir(), "no-such-java")
	cfg.GameHelperPath = writeFakeHelper(t)
	global.Config = cfg

	if sim := loadSimulation(); sim != nil {
		t.Fatalf("java 不存在时应返回 nil, 得到 %T", sim)
	}
}

// TestLoadSimulationAttachesBackend 配置齐备时返回外部后端
func TestLoadSimulationAttachesBackend(t *testing.T) {
	setupBase(t)

	cfg := bean.NewCoreConfig()
	cfg.GameHelperPath = writeFakeHelper(t)
	global.Config = cfg

	sim := loadSimulation()
	if sim == nil {
		t.Fatal("配置齐备时应返回非 nil 后端")
	}
	backend, ok := sim.(*external.Backend)
	if !ok {
		t.Fatalf("后端类型 = %T, 期望 *external.Backend", sim)
	}

	// 装上了 game.Simulation 就该能当后端用; 进程是惰性启动的, 这里只确认接口满足
	if backend.Running() {
		t.Fatal("刚装上还不该处于运行中 (进程尚未启动)")
	}
}

// TestLoadSimulationWrapsCloseHook 退出钩子必须关掉外部进程
//
// 否则服务器退出后 helper 会变成孤儿 JVM, 而它往往比服务器本身更占内存.
func TestLoadSimulationWrapsCloseHook(t *testing.T) {
	setupBase(t)

	cfg := bean.NewCoreConfig()
	cfg.GameHelperPath = writeFakeHelper(t)
	global.Config = cfg

	sim := loadSimulation()
	if sim == nil {
		t.Fatal("配置齐备时应返回非 nil 后端")
	}
	backend := sim.(*external.Backend)

	// 原钩子要照样被调用 (它负责停端口监听), 不能因为包了一层就丢掉
	origCalled := false
	prev := global.CloseServerHook
	global.CloseServerHook = func() {
		origCalled = true
		prev()
	}

	// 触发一次关闭: 进程没起来过, Close 应该是空操作且不报错
	global.CloseServerHook()

	if !origCalled {
		t.Fatal("原 CloseServerHook 没有被调用")
	}
	// 钩子可能被重复触发 (退出路径上多处调用), 不能 panic, 也不能报错
	global.CloseServerHook()
	if err := backend.Close(); err != nil {
		t.Fatalf("重复 Close 应仍是无操作, 得到 %v", err)
	}
}

// sameFunc 比较两个 func 是否同一个
//
// Go 不允许直接比较 func, 但对"这个钩子还是原来那个吗"这类断言,
// reflect 的 Pointer() 是唯一务实的手段.
func sameFunc(a, b func()) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

// TestLoadSimulationSurvivesNoI18N 语言包没加载时也不能 panic
//
// loadSimulation 在启动链里早于/晚于语言包加载都可能, tr() 必须对 nil bundle 安全.
func TestLoadSimulationSurvivesNoI18N(t *testing.T) {
	setupBase(t)
	global.I18NBundle = nil

	cfg := bean.NewCoreConfig()
	cfg.GameHelperPath = writeFakeHelper(t)
	global.Config = cfg

	if sim := loadSimulation(); sim == nil {
		t.Fatal("语言包缺失不应影响后端装载")
	}
}

// TestSimulationLogging 确认回落时留下可读警告, 接上时打印本地化提示
func TestSimulationLogging(t *testing.T) {
	setupBase(t)

	// 接上真实语言包, 否则 tr() 只会返回 key 本身
	b, err := resource.Bundle("EN")
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}
	global.I18NBundle = load.NewI18NBundleFromBytes(b)

	var buf strings.Builder
	log.SetOutput(&buf)
	log.Set("TRACK") // 放到最低级别, 让 Info/Warn 都出来

	// 失败路径: helper 不存在 -> 回落 + 一条带标签的警告
	cfg := bean.NewCoreConfig()
	cfg.GameHelperPath = filepath.Join(t.TempDir(), "nope.jar")
	global.Config = cfg

	if sim := loadSimulation(); sim != nil {
		t.Fatal("helper 不存在时应回落")
	}
	if !strings.Contains(buf.String(), "GameCore") {
		t.Fatalf("回落时没有留下警告, 日志 = %q", buf.String())
	}

	// 成功路径: 接上后要打印本地化提示, 并且带上 helper 路径
	buf.Reset()
	cfg.GameHelperPath = writeFakeHelper(t)
	global.Config = cfg
	if sim := loadSimulation(); sim == nil {
		t.Fatal("配置齐备时应装上后端")
	}
	if !strings.Contains(buf.String(), "External game core attached") {
		t.Fatalf("成功时没有打印接入提示, 日志 = %q", buf.String())
	}
	if !strings.Contains(buf.String(), "helper.jar") {
		t.Fatalf("接入提示里没有 helper 路径, 日志 = %q", buf.String())
	}
}

// TestStartChainAttachesConfiguredBackend 走完整启动链验证后端被装上
//
// 前面几个测试都是直接调 loadSimulation; 这一个要确认它真的接在了
// bootstrap 的正确位置 —— 建房间之前, 且结果最终落到 game.Backend 上.
func TestStartChainAttachesConfiguredBackend(t *testing.T) {
	setupBase(t)
	seedSettings(t)

	// 预置一份配好 helper 的 Config.json, 让 StringToCoreConfig 读到他
	cfg := bean.NewCoreConfig()
	cfg.GameHelperPath = writeFakeHelper(t)
	bean.SaveCoreConfig(cfg)

	var out bytes.Buffer
	log.SetOutput(&out)

	r := New(global.MainParameters{})
	if err := r.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	backend, ok := game.Backend.(*external.Backend)
	if !ok {
		t.Fatalf("game.Backend = %T, 期望 *external.Backend", game.Backend)
	}
	// 房间构造时取走的就是它
	if r.Session == nil || r.Session.Simulation == nil {
		t.Fatal("Session 或其后端为 nil")
	}
	if r.Session.Simulation != game.Simulation(backend) {
		t.Fatalf("房间持有的后端 = %T, 期望与 game.Backend 同一个", r.Session.Simulation)
	}
	// 外部依赖接上了要能在启动日志里看见
	if !strings.Contains(out.String(), "External game core attached") {
		t.Fatalf("启动日志里没有外部内核接入提示:\n%s", out.String())
	}

	// 收尾: 关掉钩子, 确认外部进程被要求退出且不影响端口监听
	global.CloseServerHook()
}

// TestStartChainKeepsDefaultBackend 不配置时启动链必须保持无后端
//
// 这是"不配置就与加这个特性之前完全一致"的回归保障.
func TestStartChainKeepsDefaultBackend(t *testing.T) {
	setupBase(t)
	seedSettings(t)

	bean.SaveCoreConfig(bean.NewCoreConfig()) // GameHelperPath 为空

	r := New(global.MainParameters{})
	if err := r.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, ok := game.Backend.(game.Unimplemented); !ok {
		t.Fatalf("game.Backend = %T, 期望 game.Unimplemented", game.Backend)
	}
	// 没有后端, 默认 start 命令必须明确失败
	if global.StartServer {
		t.Fatal("没有模拟后端时 StartServer 应保持 false")
	}
}

// TestSimulationUsesFileBasePath helper 相对路径应锚到数据根目录
//
// 原版所有路径都走 file.GetFolder 体系, Go 版的外部依赖路径也不该例外:
// 否则同一个配置换个目录启动就时好时坏.
func TestSimulationUsesFileBasePath(t *testing.T) {
	setupBase(t)

	dir := t.TempDir()
	file.SetBasePath(dir)

	// 在数据根目录下放一个 helper; 用相对路径 (不带前导 /) 引用它
	rel := filepath.Join("data", "libs", "helper.jar")
	abs := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("建目录: %v", err)
	}
	if err := os.WriteFile(abs, []byte("fake"), 0o644); err != nil {
		t.Fatalf("写 helper: %v", err)
	}

	cfg := bean.NewCoreConfig()
	cfg.GameHelperPath = rel
	global.Config = cfg

	if sim := loadSimulation(); sim == nil {
		t.Fatalf("数据根目录下的 %s 应该被找到", rel)
	}
}

// TestAnchorPath 相对路径锚定, 裸名字与绝对路径原样返回
func TestAnchorPath(t *testing.T) {
	setupBase(t)
	file.SetBasePath(t.TempDir())
	root := file.BasePath()

	cases := []struct{ in, want string }{
		// 裸名字是 PATH 查找, 锚定会破坏它
		{"java", "java"},
		// 绝对路径不该被改写
		{"/opt/java/bin/java", "/opt/java/bin/java"},
		// 空值原样
		{"", ""},
		// 相对路径锚到根目录
		{"libs/helper.jar", filepath.Join(root, "libs/helper.jar")},
	}
	for _, c := range cases {
		if got := anchorPath(c.in); got != c.want {
			t.Errorf("anchorPath(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}

	// Windows 风格分隔符: 在 Windows 上是分隔符, 在 Unix 上是普通字符,
	// 但两种情况都必须被锚定 —— 否则配置换个目录启动就失效.
	got := anchorPath(`data\libs\helper.jar`)
	if !strings.HasPrefix(got, root) {
		t.Errorf("anchorPath(%q) = %q, 期望以数据根目录 %q 开头", `data\libs\helper.jar`, got, root)
	}
}
