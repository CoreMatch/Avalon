package plugin

// 参考实现对接测试: 把 docs/plugins/template 编译成二进制实跑, 用真 hub
// 走一遍 manifest 协商 / 否决准入 / deliver 投递 / observe 投递 —— 保证
// 开发者文档里的模板真的能跑 (示例跑不通比没有更糟).

import (
	"bytes"
	"encoding/hex"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildTemplate 编译参考实现, 返回二进制路径
func buildTemplate(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "template-plugin")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/CoreMatch/Avalon/docs/plugins/template")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("编译模板失败: %v\n%s", err, out)
	}
	return bin
}

// startTemplate 在空闲端口启动模板进程, 等待可连接, 收尾自动杀进程
func startTemplate(t *testing.T, bin, secretHex string, deny bool) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	args := []string{"-addr", addr, "-secret", secretHex}
	if deny {
		args = append(args, "-deny")
	}
	cmd := exec.Command(bin, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动模板进程: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("模板进程输出:\n%s", out.String())
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return "http://" + addr
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("模板进程 5s 内未就绪, 输出:\n%s", out.String())
	return ""
}

// TestTemplatePluginRunsAgainstHub 参考实现接到真 hub: 三档全走通
func TestTemplatePluginRunsAgainstHub(t *testing.T) {
	bin := buildTemplate(t)

	t.Run("allow", func(t *testing.T) {
		isolateBase(t)
		withV2Globals(t)
		hexSecret, _ := testSecret(t)
		baseURL := startTemplate(t, bin, hexSecret, false)

		entry := Entry{Name: "hello", BaseURL: baseURL, Secrets: []string{hexSecret}}
		hub := loadTestHub(t, configWith(entry))

		// 协商: manifest 拉取成功, 插件就绪, 订阅交集含模板声明的事件
		waitFor(t, 10*time.Second, "template ready", func() bool {
			st := hub.Status()
			return len(st) == 1 && st[0].Ready && st[0].ManifestID == "hello-plugin"
		})

		// veto 档: 建房准入放行
		if d := hub.AdmitRoomCreate(RoomCreateRequest{Kind: KindRelay, Code: "R1"}); !d.Allowed {
			t.Fatalf("模板放行模式不应否决: %+v", d)
		}

		// deliver 档: game.over 投递并被确认 (等发送侧计数收敛再断言)
		hub.EmitGameOver("R1", nil)
		waitFor(t, 10*time.Second, "game.over delivered", func() bool {
			return hub.Status()[0].Delivered == 1
		})

		// observe 档: player.kicked 投递并被确认
		hub.EmitPlayerKicked("R1", PlayerInfo{Name: "bob", UUID: "bb01"}, "afk", 0)
		waitFor(t, 10*time.Second, "player.kicked delivered", func() bool {
			return hub.Status()[0].Delivered == 2
		})
	})

	t.Run("deny", func(t *testing.T) {
		isolateBase(t)
		withV2Globals(t)
		hexSecret, _ := testSecret(t)
		baseURL := startTemplate(t, bin, hexSecret, true)

		hub := loadTestHub(t, configWith(Entry{Name: "hello", BaseURL: baseURL, Secrets: []string{hexSecret}}))
		waitFor(t, 10*time.Second, "template ready", func() bool {
			return hub.Status()[0].Ready
		})

		// -deny 模式: 建房被否决, 理由可读且已净化
		d := hub.AdmitRoomCreate(RoomCreateRequest{Kind: KindRelay, Code: "R1"})
		if d.Allowed {
			t.Fatal("模板 -deny 模式应否决建房")
		}
		if !strings.Contains(d.Reason, "hello 插件演示否决") || d.Plugin != "hello" {
			t.Fatalf("否决结论 = %+v", d)
		}
		if st := hub.Status(); st[0].Denied != 1 {
			t.Errorf("denied 计数 = %d", st[0].Denied)
		}
	})
}

// TestTemplatePluginRejectsWrongSecret 密钥不配: 模板拒绝请求 (401),
// 准入按畸形应答 fail-open —— 双向签名任何一侧错配都不得静默通过
func TestTemplatePluginRejectsWrongSecret(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)
	bin := buildTemplate(t)

	_, raw := testSecret(t)
	serverSecret := hex.EncodeToString(raw)
	baseURL := startTemplate(t, bin, serverSecret, false)

	// Avalon 侧配另一把密钥: 请求签名对不上, 模板 401
	otherHex, _ := testSecret(t)
	hub := loadTestHub(t, configWith(Entry{Name: "mismatch", BaseURL: baseURL, Secrets: []string{otherHex}}))

	// manifest 协商同样过不了验签 -> 永不就绪 -> 准入 fail-open
	waitFor(t, 5*time.Second, "fail-open admission", func() bool {
		return hub.AdmitRoomCreate(RoomCreateRequest{Kind: KindRelay}).Allowed
	})
	if st := hub.Status(); st[0].Ready {
		t.Fatal("密钥错配时插件不得就绪")
	}
}
