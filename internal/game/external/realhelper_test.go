package external

// 真实 helper 集成测试: 拉起真正的 Helper.jar (JVM + 上游游戏引擎),
// 走完整协议链路验证 v2 基础操作与 v2-ext 四个扩展操作都工作在
// 真实路径上 (非降级路径).
//
// 有意修正（作者已批准，2026-11-04）: 上游这些能力直接调用进程内引擎,
// Go 版经 helper 进程完成 (DECISIONS D-9). 本测试即"真实可用"的验收:
// 引擎真实开局、真实生成/移除 AI、真实返回结算数据.
//
// 前置条件不满足时跳过 (不失败):
//   - Helper.jar 未构建 (./gradlew :Helper:jar)
//   - 找不到 JDK 17 的 java (环境变量 RWHPS_TEST_JAVA 可显式指定)
//   - 仓库缺 data/gameData 游戏数据
//
// 单测耗时约 1-2 分钟 (JVM 冷启动 + 引擎载入 + 真实对局推进).

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"rwhps/internal/game"
)

// repoRoot 从本包目录 (Go-Server/internal/game/external) 回推到仓库根
func repoRoot() string {
	return filepath.Join("..", "..", "..", "..")
}

// realHelperPrereqs 检查真实 helper 的前置条件; 不满足时 t.Skip
//
// 返回 java 可执行文件路径、Helper.jar 路径与准备好的数据目录
// (data/gameData 软链到仓库资源, 其余配置由 helper 首次启动生成).
func realHelperPrereqs(t *testing.T) (java, jar, dataDir string) {
	t.Helper()

	root := repoRoot()
	jar = filepath.Join(root, "Helper", "build", "libs", "Helper.jar")
	if !fileExists(jar) {
		t.Skipf("Helper.jar 未构建, 跳过真实集成测试: %s (构建: ./gradlew :Helper:jar)", jar)
	}

	gameData, err := filepath.Abs(filepath.Join(root, "data", "gameData"))
	if err != nil {
		t.Skipf("解析游戏数据路径失败: %v", err)
	}
	if info, err := os.Stat(gameData); err != nil || !info.IsDir() {
		t.Skipf("仓库缺游戏数据, 跳过真实集成测试: %s", gameData)
	}

	// JDK: 系统默认的 java 版本可能过新 (引擎与 Kotlin 运行时按 JDK 17 验证),
	// 优先取环境变量, 其次找本机常见的 17 安装位置, 找不到就跳过.
	java = os.Getenv("RWHPS_TEST_JAVA")
	if java == "" {
		var candidates []string
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, "jdk-17.0.21+9", "bin", "java"))
		}
		if matches, err := filepath.Glob("/usr/lib/jvm/*17*/bin/java"); err == nil {
			candidates = append(candidates, matches...)
		}
		for _, c := range candidates {
			if fileExists(c) {
				java = c
				break
			}
		}
	}
	if java == "" || !fileExists(java) {
		t.Skip("未找到 JDK 17 的 java, 跳过真实集成测试 (设置 RWHPS_TEST_JAVA 可指定)")
	}

	// 数据目录: 每次测试独立, 避免与手工调试目录互相污染
	dataDir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "data"), 0o755); err != nil {
		t.Fatalf("准备数据目录: %v", err)
	}
	if err := os.Symlink(gameData, filepath.Join(dataDir, "data", "gameData")); err != nil {
		t.Skipf("软链游戏数据失败, 跳过真实集成测试: %v", err)
	}
	return java, jar, dataDir
}

// newRealBackend 按前置条件构造真实后端
func newRealBackend(t *testing.T) *Backend {
	t.Helper()
	java, jar, dataDir := realHelperPrereqs(t)

	b, err := New(Config{
		JavaPath:   java,
		HelperPath: jar,
		ExtraArgs: []string{
			"-Drwhps.helper.dataPath=" + dataDir,
			"-Xmx512m",
		},
		// JVM 冷启动 + 引擎初始化留足余量
		HelloTimeout: 90 * time.Second,
		OpTimeout:    120 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

// TestRealHelperV2Ext 真实 helper 上走通 v2 基础操作与 v2-ext 四项:
//
//	room.create/addai(大厅) -> room.start -> room.status ->
//	addai(局内) -> room.sync -> room.removeplayer x2 -> game.result
//
// 结算数据由引擎真实产出: 移除 AI 使一方清空, 引擎判定剩余方获胜.
func TestRealHelperV2Ext(t *testing.T) {
	b := newRealBackend(t)

	// --- v2: 握手 + 房间创建 ---
	if err := b.EnsureRunning(); err != nil {
		t.Fatalf("EnsureRunning (hello 握手): %v", err)
	}
	if gen := b.Generation(); gen != 1 {
		t.Fatalf("Generation = %d, 期望 1", gen)
	}

	const roomID = "r1"
	if _, err := b.Request(&Request{Op: OpRoomCreate, Room: roomID}); err != nil {
		t.Fatalf("room.create: %v", err)
	}

	// --- v2-ext: 大厅里加两个 AI (原始请求, 验证应答的 ai 档案字段) ---
	lobby := make([]int, 0, 2)
	for i := 0; i < 2; i++ {
		resp, err := b.Request(&Request{Op: OpRoomAddAI, Room: roomID})
		if err != nil {
			t.Fatalf("room.addai #%d: %v", i+1, err)
		}
		if resp.AI == nil {
			t.Fatalf("room.addai #%d 应带回 AI 档案", i+1)
		}
		if resp.AI.Position == nil {
			t.Fatalf("room.addai #%d 档案缺 position", i+1)
		}
		if !resp.AI.Bot || resp.AI.Name == "" {
			t.Fatalf("room.addai #%d 档案异常: %+v", i+1, resp.AI)
		}
		lobby = append(lobby, *resp.AI.Position)
	}

	// --- v2: 真实开局 (room.create 幂等重发 + room.start) ---
	err := b.Start(game.MatchConfig{
		RoomID:    roomID,
		MapName:   game.DefaultMapName,
		MapPlayer: game.DefaultMapPlayer,
		MaxPlayer: 10,
	})
	if err != nil {
		t.Fatalf("Start (room.start): %v", err)
	}
	// 地图异步载入, 留引擎几秒再动局内操作
	time.Sleep(5 * time.Second)

	// --- v2: room.status 走真实查询, 人数应含两个 AI ---
	if n := b.PlayerCount(); n < 2 {
		t.Fatalf("PlayerCount = %d, 期望 >= 2 (两个 AI)", n)
	}

	// --- v2-ext (类型化方法, 真实路径): 局内再加一个 AI ---
	ai3, err := b.AddAI()
	if err != nil {
		t.Fatalf("AddAI: %v", err)
	}
	if ai3 == nil {
		t.Fatal("AddAI 应带回 AI 档案")
	}
	if ai3.Position < 0 || ai3.Position > 9 {
		t.Fatalf("AddAI 档案 position 越界: %+v", ai3)
	}

	// --- v2-ext: 全量同步 ---
	if err := b.SyncAllPlayer(); err != nil {
		t.Fatalf("SyncAllPlayer: %v", err)
	}

	// --- v2-ext: 移除局内加的 AI, 再移除一个大厅 AI 使一方清空 ---
	if err := b.RemovePlayer(ai3.Position); err != nil {
		t.Fatalf("RemovePlayer(%d): %v", ai3.Position, err)
	}
	if err := b.RemovePlayer(lobby[0]); err != nil {
		t.Fatalf("RemovePlayer(%d): %v", lobby[0], err)
	}

	// --- v2-ext: 结算数据 (引擎判定剩余 AI 获胜) ---
	deadline := time.Now().Add(60 * time.Second)
	for {
		result, ok, err := b.GameResult()
		if err != nil {
			t.Fatalf("GameResult: %v", err)
		}
		if ok {
			if len(result.WinPlayerList) == 0 {
				t.Fatalf("结算数据缺获胜方: %+v", result)
			}
			if result.MapName == "" || result.ReplayName == "" {
				t.Fatalf("结算数据缺地图/回放字段: %+v", result)
			}
			for name, stats := range result.PlayerData {
				for _, key := range []string{
					"unitsKilled", "buildingsKilled", "experimentalsKilled",
					"unitsLost", "buildingsLost", "experimentalsLost",
				} {
					if _, has := stats[key]; !has {
						t.Fatalf("玩家 %s 战绩缺 %s: %v", name, key, stats)
					}
				}
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("60 秒内未拿到引擎结算数据 (移除 AI 后应判定剩余方获胜)")
		}
		time.Sleep(2 * time.Second)
	}
}
