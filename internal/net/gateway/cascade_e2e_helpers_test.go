package gateway_test

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rwhps/internal/game"
	"rwhps/internal/game/external"
	"rwhps/internal/game/room"
	"rwhps/internal/net/core"
	"rwhps/internal/net/gateway"
	gameserver "rwhps/internal/net/gameserver"
	rwnet "rwhps/internal/net"
	"rwhps/internal/util/file"
	"rwhps/internal/util/file/plugindata"
)

// findJDK17Java 定位 JDK17 的 java (与 external/realhelper_test 同款逻辑)
func findJDK17Java(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("RWHPS_TEST_JAVA"); p != "" {
		return p
	}
	var candidates []string
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, "jdk-17.0.21+9", "bin", "java"))
	}
	if matches, _ := filepath.Glob("/usr/lib/jvm/*17*/bin/java"); len(matches) > 0 {
		candidates = append(candidates, matches...)
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c
		}
	}
	t.Skip("未找到 JDK17 的 java, 跳过真实 Cascade 联调")
	return ""
}

// startGoBackend 起真实 helper 引擎 + 网关 (mTLS 游戏口 + /v1 控制面).
// 返回后端、网关与停止函数.
func startGoBackend(t *testing.T, java, jar string, km *keyMaterial, gamePort, controlPort int) (*external.Backend, *gateway.Gateway, func()) {
	t.Helper()
	// Go-Server/internal/net/gateway -> 仓库根 (4 级)
	root := filepath.Join("..", "..", "..", "..")

	// helper 数据目录: gameData 软链到仓库资源
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	gameData, err := filepath.Abs(filepath.Join(root, "data", "gameData"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(gameData, filepath.Join(dataDir, "data", "gameData")); err != nil {
		t.Skipf("软链游戏数据失败: %v", err)
	}

	backend, err := external.New(external.Config{
		JavaPath:   java,
		HelperPath: jar,
		ExtraArgs:  []string{"-Drwhps.helper.dataPath=" + dataDir, "-Xmx512m"},
		HelloTimeout: 90 * time.Second,
		OpTimeout:    120 * time.Second,
	})
	if err != nil {
		t.Fatalf("external.New: %v", err)
	}
	if err := backend.EnsureRunning(); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}

	gameserver.AttachBackend(backend)
	room.SetDefault(room.NewServerRoom(game.NewGameSession("GW01", backend)))

	// 先建房再开监听 (生产顺序: start 命令先 StartGame 建房再开端口):
	// helper 的出包路由依赖 room.create 记录的房间号, 连接先于建房到达时
	// 引擎应答会带空 room 字段, 触发协议错误.
	if _, err := backend.Request(&external.Request{Op: external.OpRoomCreate, Room: "GW01"}); err != nil {
		t.Fatalf("room.create: %v", err)
	}

	// 网关: jti 账本 + mTLS 材料 + 代理公钥
	store, err := plugindata.Open(&file.FileUtils{Path: filepath.Join(t.TempDir(), "jti.bin")})
	if err != nil {
		t.Fatal(err)
	}
	gw, err := gateway.New(gateway.Config{
		BackendID:          "rwhps-e2e",
		RoomCode:           "GW01",
		TLSCert:            km.serverCert,
		TLSKey:             km.serverKey,
		ClientCA:           km.caPath,
		ControlAddr:        fmt.Sprintf("127.0.0.1:%d", controlPort),
		ProxyIssuer:        "cascade",
		ProxyKeyID:         "proxy-key-e2e",
		ProxyPublicKeyPath: km.proxyPubPath,
		PendingTimeout:     5 * time.Second,
	}, gateway.NewJtiStore(store))
	if err != nil {
		t.Fatalf("gateway.New: %v", err)
	}

	// 控制面
	go func() { _ = gw.ServeControl() }()

	// 游戏口: 网关模式监听
	an := rwnet.NewAbstractNet(gameserver.NewRwHps(core.ServerProtocol))
	an.SetGateway(gw)
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", gamePort))
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go an.HandleChannel(core.NewChannel(c))
		}
	}()
	gw.SetReady(true)

	stop := func() {
		_ = ln.Close()
		gw.Stop()
		room.SetDefault(nil)
		gameserver.AttachBackend(nil)
		_ = backend.Close()
	}
	return backend, gw, stop
}

// copyCascadeMigrations 把 Cascade 的数据库迁移文件拷进运行目录.
//
// Cascade 按 cwd 相对路径找 internal/database/migrations (migrate.go:83),
// 找不到就静默跳过建表 —— 独立部署必须带着迁移目录跑, 这里在测试目录内
// 复刻同样的布局.
func copyCascadeMigrations(t *testing.T, dir string) {
	t.Helper()
	src := filepath.Join(cascadeSrc(), "internal", "database", "migrations")
	dst := filepath.Join(dir, "internal", "database", "migrations")
	if err := copyTree(src, dst); err != nil {
		t.Fatalf("拷贝 Cascade 迁移文件: %v", err)
	}
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// writeCascadeConfig 写 Cascade 的 config.yaml
func writeCascadeConfig(t *testing.T, dir string, km *keyMaterial, controlURL, gameAddr string, cascadeGamePort, cascadeHTTPPort int) {
	t.Helper()
	cfg := fmt.Sprintf(`version: 2
game_listen_addr: "127.0.0.1:%d"
http_listen_addr: "127.0.0.1:%d"
default_backend_id: "rwhps-e2e"
backends:
  - id: "rwhps-e2e"
    control_url: "%s"
    game_address: "%s"
    prefixes: ["GW"]
proxy_issuer: "cascade"
proxy_key_id: "proxy-key-e2e"
proxy_private_key_path: "%s"
backend_client_cert: "%s"
backend_client_key: "%s"
backend_ca_pool: "%s"
health_interval_sec: 1
room_snapshot_interval_sec: 1
pending_session_timeout_sec: 5
`, cascadeGamePort, cascadeHTTPPort, controlURL, gameAddr,
		km.proxyKeyPath, km.clientCert, km.clientKey, km.caPath)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
}

// waitCascadeReady 轮询 Cascade 的 /readyz 直到默认后端就绪
func waitCascadeReady(t *testing.T, httpPort int, timeout time.Duration) {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/readyz", httpPort)
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			status := resp.StatusCode
			_ = resp.Body.Close()
			if status == http.StatusOK {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("Cascade 未在 %s 内就绪 (后端未健康)", timeout)
}

// runRealGameToSettlement 在同一后端上真实开局并拿到结算
func runRealGameToSettlement(t *testing.T, backend *external.Backend) {
	t.Helper()
	if err := backend.Start(game.MatchConfig{
		RoomID:    "GW01",
		MapName:   game.DefaultMapName,
		MapPlayer: game.DefaultMapPlayer,
		MaxPlayer: 10,
	}); err != nil {
		t.Fatalf("真实开局: %v", err)
	}
	t.Log("[E2E] 同一后端真实开局 (Crossing Large 10p)")

	var positions []int
	for i := 0; i < 2; i++ {
		ai, err := backend.AddAI()
		if err != nil {
			t.Fatalf("AddAI #%d: %v", i+1, err)
		}
		if ai == nil {
			t.Fatalf("AddAI #%d 无档案", i+1)
		}
		positions = append(positions, ai.Position)
	}
	time.Sleep(6 * time.Second)

	if err := backend.RemovePlayer(positions[0]); err != nil {
		t.Fatalf("RemovePlayer: %v", err)
	}

	deadline := time.Now().Add(60 * time.Second)
	for {
		result, ok, err := backend.GameResult()
		if err != nil {
			t.Fatalf("GameResult: %v", err)
		}
		if ok {
			if len(result.WinPlayerList) == 0 {
				t.Fatalf("结算缺获胜方: %+v", result)
			}
			t.Logf("[E2E] 真实结算: 获胜=%v 地图=%q 时长=%ds", result.WinPlayerList, result.MapName, result.GameTime)
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("60 秒内未拿到结算")
		}
		time.Sleep(2 * time.Second)
	}
}
