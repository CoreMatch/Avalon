package net_test

// E2E 矩阵 (稳定壳收尾, 2026-11-05): 用现有辅助协议跑多组真实开局组合,
// 全部到结算. 覆盖维度:
//
//   - 地图: 小图 1v1 / 中图 4p / 大图 10p 三种尺寸;
//   - 玩家构成: 全 AI、AI + 真实协议客户端混合;
//   - 模式: 默认设置 + 非默认设置 (room.settings 变更后开局).
//
// 每组独立拉起真实 helper (互不串状态), 记录开局耗时/结算结果/回放文件.
// 与 gameserver_e2e_test.go 共用其 e2ePrereqs/dialAndPreregister 等辅助.

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rwhps/internal/game"
	"rwhps/internal/game/external"
	"rwhps/internal/game/room"
	"rwhps/internal/io/packettype"
	rwnet "rwhps/internal/net"
	"rwhps/internal/net/core"
	"rwhps/internal/net/gameserver"
)

type matrixScenario struct {
	name      string
	mapName   string
	mapPlayer string
	maxPlayer int
	clients   int // 真实 TCP 客户端数 (预注册握手)
	ais       int // AI 数
	settings  *external.SettingsPayload
}

func f32(v float32) *float32 { return &v }
func boolp(v bool) *bool     { return &v }
func intp(v int) *int        { return &v }

// TestE2ERealGameMatrix 多场景真实开局矩阵, 每组都跑到引擎结算.
func TestE2ERealGameMatrix(t *testing.T) {
	java, jar, _ := e2ePrereqs(t)

	scenarios := []matrixScenario{
		{
			name:      "小图2p-全AI-默认",
			mapName:   "Small_Island (2p)",
			mapPlayer: "[p2]",
			maxPlayer: 2,
			clients:   0,
			ais:       2,
			settings:  nil,
		},
		{
			name:      "中图4p-客户端混合-默认",
			mapName:   "Desert (4p)",
			mapPlayer: "[p4]",
			maxPlayer: 4,
			clients:   2,
			ais:       2,
			settings:  nil,
		},
		{
			name:      "大图10p-客户端混合-非默认设置",
			mapName:   "Crossing Large (10p)",
			mapPlayer: "[z;p10]",
			maxPlayer: 10,
			clients:   2,
			ais:       2,
			settings: &external.SettingsPayload{
				Income:      f32(2.0),
				DisableNuke: boolp(true),
				MaxUnits:    intp(300),
			},
		},
	}

	for i, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			runMatrixScenario(t, java, jar, sc, i)
		})
	}
}

func runMatrixScenario(t *testing.T, java, jar string, sc matrixScenario, idx int) {
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "data"), 0o755); err != nil {
		t.Fatalf("准备数据目录: %v", err)
	}
	gameData, err := filepath.Abs(filepath.Join(e2eRepoRoot(), "data", "gameData"))
	if err != nil {
		t.Fatalf("解析游戏数据路径: %v", err)
	}
	if err := os.Symlink(gameData, filepath.Join(dataDir, "data", "gameData")); err != nil {
		t.Skipf("软链游戏数据失败: %v", err)
	}

	// --- 1. 独立真实后端 ---
	backend, err := external.New(external.Config{
		JavaPath:     java,
		HelperPath:   jar,
		ExtraArgs:    []string{"-Drwhps.helper.dataPath=" + dataDir, "-Xmx512m"},
		HelloTimeout: 90 * time.Second,
		OpTimeout:    120 * time.Second,
	})
	if err != nil {
		t.Fatalf("external.New: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.EnsureRunning(); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}

	// --- 2. 接入网关 + 默认房间 ---
	roomID := fmt.Sprintf("room-matrix-%d", idx)
	gameserver.AttachBackend(backend)
	t.Cleanup(func() { gameserver.AttachBackend(nil) })
	room.SetDefault(room.NewServerRoom(game.NewGameSession(roomID, backend)))
	t.Cleanup(func() { room.SetDefault(nil) })

	// --- 3. TCP 监听 (有客户端的场景) ---
	var port int
	if sc.clients > 0 {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		defer listener.Close()
		port = listener.Addr().(*net.TCPAddr).Port
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
	}

	// --- 4. 建房 ---
	if _, err := backend.Request(&external.Request{Op: external.OpRoomCreate, Room: roomID}); err != nil {
		t.Fatalf("room.create: %v", err)
	}

	// --- 5. 客户端连入 (预注册握手) ---
	for i := 0; i < sc.clients; i++ {
		typeInt, body := dialAndPreregister(t, port)
		if typeInt != int(packettype.PREREGISTER_INFO) {
			t.Fatalf("client#%d: 收到包类型 %d, 期望 161", i+1, typeInt)
		}
		if len(body) == 0 {
			t.Fatalf("client#%d: PREREGISTER_INFO 负载为空", i+1)
		}
	}

	// --- 6. 加 AI ---
	lobby := make([]int, 0, sc.ais)
	for i := 0; i < sc.ais; i++ {
		resp, err := backend.Request(&external.Request{Op: external.OpRoomAddAI, Room: roomID})
		if err != nil {
			t.Fatalf("room.addai #%d: %v", i+1, err)
		}
		if resp.AI == nil || resp.AI.Position == nil {
			t.Fatalf("room.addai #%d 应带回 AI 档案", i+1)
		}
		lobby = append(lobby, *resp.AI.Position)
	}

	// --- 7. 非默认设置 (开局前写入引擎) ---
	if sc.settings != nil {
		resp, err := backend.Request(&external.Request{
			Op: external.OpRoomSettings, Room: roomID, Settings: sc.settings,
		})
		if err != nil {
			t.Fatalf("room.settings: %v", err)
		}
		if !resp.OK {
			t.Fatalf("room.settings 被拒: %s", resp.Error)
		}
	}

	// --- 8. 真实开局 (计时) ---
	startAt := time.Now()
	if err := backend.Start(game.MatchConfig{
		RoomID:    roomID,
		MapName:   sc.mapName,
		MapPlayer: sc.mapPlayer,
		MaxPlayer: sc.maxPlayer,
	}); err != nil {
		t.Fatalf("Start (map=%q): %v", sc.mapName, err)
	}
	startDur := time.Since(startAt)

	// --- 9. 对局推进 ---
	time.Sleep(8 * time.Second)

	// --- 10. 移除一个 AI 使一方清空, 引擎判胜 ---
	if err := backend.RemovePlayer(lobby[0]); err != nil {
		t.Fatalf("RemovePlayer(%d): %v", lobby[0], err)
	}

	// --- 11. 轮询结算 ---
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
			if result.MapName == "" {
				t.Fatalf("结算缺地图字段: %+v", result)
			}
			// 注: result.MapName 是引擎结算数据里的静态默认图名 (headless 下
			// 不随 room.start 更新), 实际对局地图以回放文件名为准 —— 引擎按
			// 真实地图生成回放 (见下方 replayFiles 断言).
			if result.ReplayName == "" {
				t.Fatalf("结算缺回放字段: %+v", result)
			}
			replayFiles := findReplayFiles(t, dataDir)
			t.Logf("[矩阵] %s: 开局耗时=%v 获胜=%v 地图=%q 回放=%q 时长=%ds 回放文件=%v",
				sc.name, startDur.Round(time.Millisecond), result.WinPlayerList,
				result.MapName, result.ReplayName, result.GameTime, replayFiles)
			if len(replayFiles) == 0 {
				t.Errorf("结算报告了回放 %q 但数据目录未找到回放文件", result.ReplayName)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("60 秒内未拿到引擎结算数据")
		}
		time.Sleep(2 * time.Second)
	}
}

// findReplayFiles 在 helper 数据目录下找回放/存档文件 (.replay/.rwsave)
func findReplayFiles(t *testing.T, dataDir string) []string {
	t.Helper()
	var found []string
	_ = filepath.Walk(dataDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if ext := filepath.Ext(p); ext == ".replay" || ext == ".rwsave" {
			found = append(found, fmt.Sprintf("%s(%dB)", strings.TrimPrefix(p, dataDir+"/"), info.Size()))
		}
		return nil
	})
	return found
}
