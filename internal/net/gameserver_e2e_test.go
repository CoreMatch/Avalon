package net_test

// 端到端集成测试 (H-4 "一局到底"): Go 服务端 + 真实 helper (JVM 上游引擎),
// 两个客户端真实连入完成预注册握手, 引擎真实开局、AI 推进、产出结算数据.
//
// 有意修正（作者已批准，2026-11-04）: 上游这些能力直接调用进程内引擎,
// Go 版引擎在 helper 内, 经 v2-ext 扩展操作完成 (DECISIONS D-9). 本测试
// 全程不经 Go 侧假实现: 握手包真实进引擎、开局/加 AI/结算全部引擎产出.
//
// 与 gameserver_wire_test.go 的区别: 那里用记录型假内核验证帧路由; 这里
// 拉起真正的 Helper.jar, 验证"真实对局"这条验收线.
//
// 客户端只完成预注册握手 (160 -> 161): 引擎对 REGISTER_PLAYER 另有单位
// checksum + 完整性应答 (g(conn.M)) 的反作弊校验 (见 DECISIONS D-15),
// 真实 RW 客户端才产生得出, 不在本网关职责内. 预注册往返足以证明
// "客户端连入"这条链路真实可用.

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rwhps/internal/game"
	"rwhps/internal/game/external"
	"rwhps/internal/game/room"
	gameio "rwhps/internal/io"
	"rwhps/internal/io/packettype"
	rwnet "rwhps/internal/net"
	"rwhps/internal/net/core"
	"rwhps/internal/net/gameserver"
)

// e2eRepoRoot 从本包目录 (Go-Server/internal/net) 回推到仓库根
//
//	net -> internal -> Go-Server -> 仓库根
func e2eRepoRoot() string {
	return filepath.Join("..", "..", "..")
}

// e2ePrereqs 检查真实 helper 前置条件; 不满足时 t.Skip.
// 与 external/realhelper_test.go 的同名逻辑一致 (跨包测试无法共享, 各自维护).
func e2ePrereqs(t *testing.T) (java, jar, dataDir string) {
	t.Helper()

	root := e2eRepoRoot()
	jar = filepath.Join(root, "Helper", "build", "libs", "Helper.jar")
	if info, err := os.Stat(jar); err != nil || info.IsDir() {
		t.Skipf("Helper.jar 未构建, 跳过端到端测试: %s (构建: ./gradlew :Helper:jar)", jar)
	}

	gameData, err := filepath.Abs(filepath.Join(root, "data", "gameData"))
	if err != nil {
		t.Skipf("解析游戏数据路径失败: %v", err)
	}
	if info, err := os.Stat(gameData); err != nil || !info.IsDir() {
		t.Skipf("仓库缺游戏数据, 跳过端到端测试: %s", gameData)
	}

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
			if info, err := os.Stat(c); err == nil && !info.IsDir() {
				java = c
				break
			}
		}
	}
	if java == "" {
		t.Skip("未找到 JDK 17 的 java, 跳过端到端测试 (设置 RWHPS_TEST_JAVA 可指定)")
	}

	dataDir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "data"), 0o755); err != nil {
		t.Fatalf("准备数据目录: %v", err)
	}
	if err := os.Symlink(gameData, filepath.Join(dataDir, "data", "gameData")); err != nil {
		t.Skipf("软链游戏数据失败, 跳过端到端测试: %v", err)
	}
	return java, jar, dataDir
}

// preregisterFrame 构造客户端握手首包 PREREGISTER_INFO_RECEIVE (160).
//
// 负载字段对应引擎 ad.c() case-160 的读取顺序 (子版本 0 最小集):
//
//	readUTF  -> 包标识 "com.corrodinggames.rts"
//	readInt  -> 子版本 (0: 引擎只读基础字段即回 161)
//	readInt  -> 保留
//
// 引擎随后经 g(conn) 回 PREREGISTER_INFO (161), 无版本/完整性校验.
func preregisterFrame() []byte {
	o := gameio.NewGameOutputStream()
	o.WriteString("com.corrodinggames.rts")
	o.WriteInt(0)
	o.WriteInt(0)
	return rwnet.GamePacketEncoder(o.CreatePacket(packettype.PREREGISTER_INFO_RECEIVE))
}

// dialAndPreregister 连入网关并完成预注册握手, 返回引擎回的 161 包类型与负载.
//
// 验收"客户端连入": 客户端首包经 Go 网关进真实引擎, 引擎的 PREREGISTER_INFO
// 应答经同一条 TCP 连接回到客户端.
func dialAndPreregister(t *testing.T, port int) (int, []byte) {
	t.Helper()
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if _, err := conn.Write(preregisterFrame()); err != nil {
		t.Fatalf("write preregister: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	typeInt, body := readFrame(t, conn)
	return typeInt, body
}

// TestE2ERealGameToSettlement 一局到底:
// 两客户端真实连入 (预注册握手) -> 引擎真实开局 -> AI 推进 -> 结算数据产出.
func TestE2ERealGameToSettlement(t *testing.T) {
	java, jar, dataDir := e2ePrereqs(t)

	// --- 1. 真实后端 (拉起 JVM + 上游引擎) ---
	backend, err := external.New(external.Config{
		JavaPath:   java,
		HelperPath: jar,
		ExtraArgs: []string{
			"-Drwhps.helper.dataPath=" + dataDir,
			"-Xmx512m",
		},
		HelloTimeout: 90 * time.Second,
		OpTimeout:    120 * time.Second,
	})
	if err != nil {
		t.Fatalf("external.New: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close() })

	if err := backend.EnsureRunning(); err != nil {
		t.Fatalf("EnsureRunning (hello 握手): %v", err)
	}

	// --- 2. 把真实后端接进 Go 网关, 并建默认房间 ---
	const roomID = "room-e2e"
	gameserver.AttachBackend(backend)
	t.Cleanup(func() { gameserver.AttachBackend(nil) })

	room.SetDefault(room.NewServerRoom(game.NewGameSession(roomID, backend)))
	t.Cleanup(func() { room.SetDefault(nil) })

	// --- 3. TCP 监听 (与生产同款 AbstractNet + ServerProtocol) ---
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	an := rwnet.NewAbstractNet(gameserver.NewRwHps(core.ServerProtocol))
	go func() {
		for {
			serverConn, err := listener.Accept()
			if err != nil {
				return
			}
			go an.HandleChannel(core.NewChannel(serverConn))
		}
	}()

	// --- 4. 先建房 (生产顺序: 房间先于客户端连接存在; helper 的出包路由
	// 依赖 room.create 记录的房间号) ---
	if _, err := backend.Request(&external.Request{Op: external.OpRoomCreate, Room: roomID}); err != nil {
		t.Fatalf("room.create: %v", err)
	}

	// --- 5. 两个客户端真实连入, 各完成一次预注册握手 ---
	for _, name := range []string{"clientA", "clientB"} {
		typeInt, body := dialAndPreregister(t, port)
		if typeInt != int(packettype.PREREGISTER_INFO) {
			t.Fatalf("%s: 收到包类型 %d, 期望 PREREGISTER_INFO(161)", name, typeInt)
		}
		if len(body) == 0 {
			t.Fatalf("%s: PREREGISTER_INFO 负载为空", name)
		}
		t.Logf("[E2E] %s 连入并完成预注册握手, 引擎应答 %d 字节", name, len(body))
	}

	// --- 6. 大厅加两个 AI (真实引擎) ---
	lobby := make([]int, 0, 2)
	for i := 0; i < 2; i++ {
		resp, err := backend.Request(&external.Request{Op: external.OpRoomAddAI, Room: roomID})
		if err != nil {
			t.Fatalf("room.addai #%d: %v", i+1, err)
		}
		if resp.AI == nil || resp.AI.Position == nil {
			t.Fatalf("room.addai #%d 应带回 AI 档案", i+1)
		}
		lobby = append(lobby, *resp.AI.Position)
		t.Logf("[E2E] 大厅加入 AI #%d: name=%q position=%d", i+1, resp.AI.Name, *resp.AI.Position)
	}

	// --- 7. 真实开局 ---
	if err := backend.Start(game.MatchConfig{
		RoomID:    roomID,
		MapName:   game.DefaultMapName,
		MapPlayer: game.DefaultMapPlayer,
		MaxPlayer: 10,
	}); err != nil {
		t.Fatalf("Start (room.start): %v", err)
	}
	t.Log("[E2E] 引擎真实开局 (Crossing Large 10p)")

	// --- 8. 对局推进: 让两个 AI 真实建造/交战一会儿 (结算时长由此而来) ---
	time.Sleep(8 * time.Second)

	// --- 9. 移除一个大厅 AI, 使一方清空, 引擎判定剩余方获胜 ---
	if err := backend.RemovePlayer(lobby[0]); err != nil {
		t.Fatalf("RemovePlayer(%d): %v", lobby[0], err)
	}
	t.Logf("[E2E] 移除 AI position=%d, 等待引擎结算", lobby[0])

	// --- 10. 轮询真实结算数据 ---
	// 注意时序: 胜负判定后 getGameOverData 即返回非空, 但 ReplayName 来自
	// room.replayFileName, 由引擎 game-over 流程中 "Recording replay to:"
	// 重定向 (GameMainRedirections) 异步写入, 晚于胜负判定 —— 首个非空结果
	// 可能尚未带回放名, 需继续等待 (干净克隆复跑实证, 2026-11-05).
	deadline := time.Now().Add(60 * time.Second)
	for {
		result, ok, err := backend.GameResult()
		if err != nil {
			t.Fatalf("GameResult: %v", err)
		}
		if ok {
			if len(result.WinPlayerList) == 0 {
				t.Fatalf("结算数据缺获胜方: %+v", result)
			}
			if result.MapName == "" {
				t.Fatalf("结算数据缺地图字段: %+v", result)
			}
			if result.ReplayName != "" {
				// gameTime 是"对局时长秒"(concurrentSecond - startTime), 不是时间戳;
				// 本局只跑了几秒到几十秒, 超过 1 小时即说明 startTime 未被设置.
				if result.GameTime < 0 || result.GameTime > 3600 {
					t.Fatalf("gameTime = %d, 不像对局时长 (startTime 未设置?)", result.GameTime)
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
				t.Logf("[E2E] 结算: 获胜=%v 地图=%q 回放=%q 时长=%ds",
					result.WinPlayerList, result.MapName, result.ReplayName, result.GameTime)
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("60 秒内未拿到含回放名的引擎结算数据")
		}
		time.Sleep(2 * time.Second)
	}
}
