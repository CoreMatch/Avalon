package app_test

// 分发自足性验证 (稳定壳收尾, 2026-11-05): 在干净目录组装分发形态
// (rwhps 二进制 + Helper.jar + data/gameData + 默认配置), 以独立进程实跑
// "启动 -> 客户端直连握手 -> 控制台加 AI/开局/结算 -> 优雅退出", 证明
// 分发包不依赖仓库环境. 同时验证退出后 Config.json 的 Go 独有键
// (gameHelperPath) 仍在 —— 否则下次启动装不上游戏内核.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	gameio "rwhps/internal/io"
	"rwhps/internal/io/packettype"
	rwnet "rwhps/internal/net"
)

// distRepoRoot: internal/app -> Go-Server -> 仓库根
func distRepoRoot() string {
	return filepath.Join("..", "..", "..")
}

func distPrereqs(t *testing.T) (java, jar, gameData string) {
	t.Helper()

	root := distRepoRoot()
	jar = filepath.Join(root, "Helper", "build", "libs", "Helper.jar")
	if info, err := os.Stat(jar); err != nil || info.IsDir() {
		t.Skipf("Helper.jar 未构建, 跳过分发验证: %s", jar)
	}
	gameData, err := filepath.Abs(filepath.Join(root, "data", "gameData"))
	if err != nil {
		t.Fatalf("解析游戏数据路径: %v", err)
	}
	if info, err := os.Stat(gameData); err != nil || !info.IsDir() {
		t.Skipf("仓库缺游戏数据: %s", gameData)
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
		t.Skip("未找到 JDK 17 的 java (设置 RWHPS_TEST_JAVA 可指定)")
	}
	return
}

// TestDistributionSelfContained 干净目录实跑分发包全流程.
func TestDistributionSelfContained(t *testing.T) {
	java, jar, gameData := distPrereqs(t)
	stage := t.TempDir()

	// --- 1. 组装分发目录 (与 dist/build-dist.sh 同布局) ---
	goDir, err := filepath.Abs(filepath.Join("..", "..")) // Go-Server 模块根
	if err != nil {
		t.Fatalf("解析 Go-Server 目录: %v", err)
	}
	build := exec.Command("go", "build", "-trimpath", "-o", filepath.Join(stage, "rwhps"), "./cmd/rwhps")
	build.Dir = goDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build 二进制失败: %v\n%s", err, out)
	}
	binInfo, _ := os.Stat(filepath.Join(stage, "rwhps"))
	jarInfo, _ := os.Stat(jar)
	if err := copyFile(jar, filepath.Join(stage, "Helper.jar")); err != nil {
		t.Fatalf("复制 Helper.jar: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(stage, "data"), 0o755); err != nil {
		t.Fatalf("建 data 目录: %v", err)
	}
	if err := os.Symlink(gameData, filepath.Join(stage, "data", "gameData")); err != nil {
		t.Skipf("软链游戏数据失败: %v", err)
	}

	port := freePort(t)
	configJSON := fmt.Sprintf(`{
  "port": %d,
  "log": "WARN",
  "javaPath": %q,
  "gameHelperPath": "./Helper.jar",
  "gameHelperArgs": ["-Xmx512m"]
}`, port, java)
	if err := os.WriteFile(filepath.Join(stage, "data", "Config.json"), []byte(configJSON), 0o644); err != nil {
		t.Fatalf("写 Config.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stage, "data", "ConfigServer.json"),
		[]byte(`{"enableAI": true, "saveRePlayFile": true}`), 0o644); err != nil {
		t.Fatalf("写 ConfigServer.json: %v", err)
	}

	// --- 2. 启动分发二进制 (干净目录, 无仓库依赖) ---
	cmd := exec.Command(filepath.Join(stage, "rwhps"))
	cmd.Dir = stage
	// 独立进程组: 超时清理时整组杀 (dist 二进制 + 它拉起的 helper JVM),
	// 避免孙进程握着管道让 cmd.Wait 挂死.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	var outBuf limitedWriter
	outBuf.max = 4 << 20
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf // 合流, 便于断言失败时查看
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动分发二进制: %v", err)
	}
	procDone := make(chan error, 1)
	go func() { procDone <- cmd.Wait() }()
	defer func() {
		select {
		case <-procDone:
		default:
			// 杀整个进程组 (含 helper)
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			select {
			case <-procDone:
			case <-time.After(10 * time.Second):
			}
		}
	}()

	startAt := time.Now()

	// 首次启动有两道交互确认 (EULA + WEB 面板询问): 预置应答,
	// 否则控制台在空管道上阻塞等待输入. 应答行到达时若提示已过,
	// 只会被当作未知命令忽略, 无副作用.
	if _, err := io.WriteString(stdin, "Yes\nNo\n"); err != nil {
		t.Fatalf("预置 EULA 应答: %v", err)
	}

	// --- 3. 等游戏端口就绪 (轮询拨号) ---
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(120 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("120 秒内游戏端口 %d 未就绪, 输出:\n%s", port, outBuf.String())
		}
		time.Sleep(500 * time.Millisecond)
	}
	bootDur := time.Since(startAt)
	t.Logf("[分发] 冷启动到端口就绪: %v (二进制 %dB / jar %dB)",
		bootDur.Round(time.Millisecond), binInfo.Size(), jarInfo.Size())

	// --- 4. 真实客户端直连: 预注册握手 (160 -> 161) ---
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("客户端拨号: %v", err)
	}
	o := gameio.NewGameOutputStream()
	o.WriteString("com.corrodinggames.rts")
	o.WriteInt(0)
	o.WriteInt(0)
	if _, err := conn.Write(rwnet.GamePacketEncoder(o.CreatePacket(packettype.PREREGISTER_INFO_RECEIVE))); err != nil {
		t.Fatalf("写预注册帧: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	typeInt, _ := readDistFrame(t, conn)
	_ = conn.Close()
	if typeInt != int(packettype.PREREGISTER_INFO) {
		t.Fatalf("直连握手收到包类型 %d, 期望 161 (PREREGISTER_INFO)", typeInt)
	}
	t.Logf("[分发] 客户端直连预注册握手成功 (160->161)")

	// --- 5. 控制台驱动: 加两个 AI -> 开局 -> 推进 -> 强制结算 ---
	console := func(line string) {
		if _, err := io.WriteString(stdin, line+"\n"); err != nil {
			t.Fatalf("控制台写 %q: %v", line, err)
		}
	}
	console("addai")
	console("addai")
	console("start")
	time.Sleep(8 * time.Second)
	console("gameover")

	// --- 6. 等回放文件落盘 (引擎结算产物) ---
	replayDir := filepath.Join(stage, "data", "replays")
	deadline = time.Now().Add(60 * time.Second)
	var replays []string
	for {
		replays = listFiles(t, replayDir)
		if len(replays) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("60 秒内未见回放文件 (%s), 输出:\n%s", replayDir, outBuf.String())
		}
		time.Sleep(2 * time.Second)
	}
	t.Logf("[分发] 结算完成, 回放文件: %v", replays)

	// --- 7. 优雅退出 (控制台 exit), 验证退出码与配置保持 ---
	console("exit")
	select {
	case err := <-procDone:
		if err != nil {
			t.Errorf("分发进程退出码异常: %v, 输出:\n%s", err, outBuf.String())
		}
	case <-time.After(60 * time.Second):
		t.Fatalf("exit 命令后 60 秒未退出, 输出:\n%s", outBuf.String())
	}

	cfgAfter, err := os.ReadFile(filepath.Join(stage, "data", "Config.json"))
	if err != nil {
		t.Fatalf("读退出后的 Config.json: %v", err)
	}
	if !strings.Contains(string(cfgAfter), "gameHelperPath") {
		t.Errorf("退出后 Config.json 丢失 gameHelperPath (Java 侧重写覆盖了 Go 键):\n%s", cfgAfter)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("取空闲端口: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func copyFile(src, dst string) error {
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, in, 0o644)
}

func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			info, _ := e.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			out = append(out, fmt.Sprintf("%s(%dB)", e.Name(), size))
		}
	}
	return out
}

// readDistFrame 读一个游戏帧 (与网关同款: 8 字节头 = 4 长度 + 4 类型,
// 长度仅计帧体), 返回包类型号与帧体.
func readDistFrame(t *testing.T, conn net.Conn) (int, []byte) {
	t.Helper()
	var head [8]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		t.Fatalf("读帧头: %v", err)
	}
	length := binary.BigEndian.Uint32(head[0:4])
	typeInt := binary.BigEndian.Uint32(head[4:8])
	body := make([]byte, length)
	if _, err := io.ReadFull(conn, body); err != nil {
		t.Fatalf("读帧体: %v", err)
	}
	return int(typeInt), body
}

// limitedWriter 防止引擎日志撑爆测试内存 (stdout/stderr 合流, 需并发安全)
type limitedWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buf.Len() < w.max {
		w.buf.Write(p)
	}
	return len(p), nil
}

func (w *limitedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}
