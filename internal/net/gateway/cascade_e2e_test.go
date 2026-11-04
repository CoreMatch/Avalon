package gateway_test

// 真实 Cascade 联调 (G-7): 用 /tmp/cascade-ro 构建出代理进程, 连到本仓库
// 开了网关模式的 Go 后端 (真实 helper 引擎), 验证
//   客户端连 Cascade -> 房间码路由 -> 身份绑定 (PUT /v1/sessions/{sid}/identity)
//   -> 转发进引擎 -> 真实开局到结算.
//
// 前置不满足时跳过 (不失败): 缺 Helper.jar / JDK17 / Cascade 源码 / go 工具链.
// Cascade 源码默认 /tmp/cascade-ro, 可用 RWHPS_CASCADE_SRC 覆盖.

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// --- 密钥材料生成 ---

// keyMaterial 一套联调所需的密钥: 代理 Ed25519 对 + mTLS (CA/服务端/客户端)
type keyMaterial struct {
	dir string

	proxyKeyPath string // Ed25519 PKCS#8 (Cascade 用, 0600)
	proxyPubPath string // Ed25519 PKIX (Go 校验用)

	caPath       string // CA (双方信任根)
	serverCert   string // Go 服务端证书
	serverKey    string
	clientCert   string // Cascade 客户端证书
	clientKey    string // 0600
}

func genKeyMaterial(t *testing.T) *keyMaterial {
	t.Helper()
	dir := t.TempDir()
	m := &keyMaterial{dir: dir}

	writeFile := func(name string, data []byte, perm os.FileMode) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, perm); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// Ed25519 对
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	m.proxyKeyPath = writeFile("proxy-ed25519-key.pem",
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}), 0o600)
	m.proxyPubPath = writeFile("proxy-ed25519-pub.pem",
		pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}), 0o600)

	// CA
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "e2e-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ := x509.ParseCertificate(caDER)
	m.caPath = writeFile("ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o600)

	sign := func(cn string, serial int64, extKeyUsage x509.ExtKeyUsage) (string, string) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(serial),
			Subject:      pkix.Name{CommonName: cn},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(24 * time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{extKeyUsage},
			IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
			DNSNames:     []string{"localhost"},
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		certPath := writeFile(cn+".crt", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
		keyPath := writeFile(cn+".key", pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600)
		return certPath, keyPath
	}
	m.serverCert, m.serverKey = sign("backend", 2, x509.ExtKeyUsageServerAuth)
	m.clientCert, m.clientKey = sign("cascade-proxy", 3, x509.ExtKeyUsageClientAuth)
	return m
}

// --- Cascade 构建 ---

// cascadeSrc 返回 Cascade 源码目录; 不存在返回 ""
func cascadeSrc() string {
	if p := os.Getenv("RWHPS_CASCADE_SRC"); p != "" {
		return p
	}
	return "/tmp/cascade-ro"
}

// buildCascade 构建 Cascade 二进制到 dir; 失败返回错误 (调用方跳过)
func buildCascade(t *testing.T, dir string) string {
	t.Helper()
	src := cascadeSrc()
	if _, err := os.Stat(filepath.Join(src, "go.mod")); err != nil {
		t.Skipf("Cascade 源码不可用 (%s), 跳过真实联调: %v", src, err)
	}
	bin := filepath.Join(dir, "cascade")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/cascade")
	cmd.Dir = src
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("构建 Cascade 失败, 跳过真实联调: %v\n%s", err, out)
	}
	return bin
}

// freePort 取一个可用端口
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// --- RW 客户端帧 ---

// rwFrame 构造 [4B 长度][4B 类型][负载] 帧
func rwFrame(typeInt int, body []byte) []byte {
	frame := make([]byte, 8+len(body))
	binary.BigEndian.PutUint32(frame[0:4], uint32(len(body)))
	binary.BigEndian.PutUint32(frame[4:8], uint32(typeInt))
	copy(frame[8:], body)
	return frame
}

// writeJavaString 写 RW 的 readUTF 字符串 (2 字节长度 + UTF-8)
func writeJavaString(buf *[]byte, s string) {
	b := []byte(s)
	var l [2]byte
	binary.BigEndian.PutUint16(l[:], uint16(len(b)))
	*buf = append(*buf, l[:]...)
	*buf = append(*buf, b...)
}

func writeInt(buf *[]byte, v int32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(v))
	*buf = append(*buf, b[:]...)
}

// preregisterBody 构造带房间码的 Preregister(160) 负载.
// 布局 (与引擎 ad.c() case-160 / Cascade DecodePreregister 对齐):
//
//	string 包标识 / int 子版本(>=2 才有 query) / int 客户端版本 /
//	子版本>=1: int / 子版本>=2: bool+string query
func preregisterBody(roomCode string) []byte {
	var buf []byte
	writeJavaString(&buf, "com.corrodinggames.rts")
	writeInt(&buf, 2)   // 子版本: 带 query
	writeInt(&buf, 176) // 客户端版本
	writeInt(&buf, 0)   // 子版本>=1 的附加 int
	// query (bool true + string)
	buf = append(buf, 1)
	writeJavaString(&buf, roomCode)
	return buf
}

// registerBody 构造 RegisterPlayer(110) 负载.
// 布局 (与引擎 case-110 / Cascade DecodeRegister 对齐):
//
//	string 包标识 / int 子版本 / int 客户端版本 / int / string 名字 /
//	bool+string (可空) / string (子版本>=1) / string 游戏UUID (子版本>=2)
//
// 注意: 引擎注册还有单位 checksum + 完整性应答两道反作弊 (DECISIONS D-16),
// 假客户端过不了, 会被引擎踢回——这恰好证明包已到达引擎注册逻辑.
func registerBody(name, uuidHex string) []byte {
	var buf []byte
	writeJavaString(&buf, "com.corrodinggames.rts")
	writeInt(&buf, 2)   // 子版本
	writeInt(&buf, 176) // 客户端版本
	writeInt(&buf, 0)
	writeJavaString(&buf, name)
	buf = append(buf, 0) // 可空 string = false
	writeJavaString(&buf, "e2e")   // 子版本>=1
	writeJavaString(&buf, uuidHex) // 子版本>=2: 游戏 UUID
	return buf
}

// readFrame 从连接读一个完整帧, 返回类型与负载
func readFrame(t *testing.T, conn net.Conn) (int, []byte) {
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

// waitForPort 等端口可连
func waitForPort(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("端口 %s 未在 %s 内就绪", addr, timeout)
}

// TestCascadeRealProxyE2E 真实 Cascade 联调主流程.
//
// 本测试编排: 真实 helper 引擎 + Go 网关 (mTLS/前导/门控/控制面) +
// 真实 Cascade 代理进程 + 一个假 RW 客户端. 断言:
//  1. 客户端经 Cascade 收到引擎的 Preregister 应答 (161) —— 房间码路由 + 转发通了
//  2. 客户端发 RegisterPlayer 后, Go 侧会话被绑定 (身份链路通了)
//  3. 引擎对注册包有响应 (转发到引擎了)
//  4. 同一后端上真实开局并产出结算 (引擎对局真实可用)
func TestCascadeRealProxyE2E(t *testing.T) {
	// 前置: helper jar / java 由 external 包同款逻辑检查. 这里复用最小集.
	// 本包在 Go-Server/internal/net/gateway, 回推 4 级到仓库根.
	root := filepath.Join("..", "..", "..", "..")
	jar := filepath.Join(root, "Helper", "build", "libs", "Helper.jar")
	if info, err := os.Stat(jar); err != nil || info.IsDir() {
		t.Skipf("Helper.jar 未构建, 跳过真实 Cascade 联调: %s", jar)
	}
	java := findJDK17Java(t)

	km := genKeyMaterial(t)
	cascadeBin := buildCascade(t, t.TempDir())

	gamePort := freePort(t)
	controlPort := freePort(t)
	cascadeGamePort := freePort(t)
	cascadeHTTPPort := freePort(t)

	// --- 1. 起 Go 后端 (helper 引擎 + 网关) ---
	backend, gw, stopBackend := startGoBackend(t, java, jar, km, gamePort, controlPort)
	defer stopBackend()

	gameAddr := fmt.Sprintf("127.0.0.1:%d", gamePort)
	controlURL := fmt.Sprintf("https://127.0.0.1:%d", controlPort)
	waitForPort(t, gameAddr, 10*time.Second)

	// --- 2. 写 Cascade 配置并启动 ---
	cascadeDir := t.TempDir()
	copyCascadeMigrations(t, cascadeDir)
	writeCascadeConfig(t, cascadeDir, km, controlURL, gameAddr, cascadeGamePort, cascadeHTTPPort)
	cascadeCmd := exec.Command(cascadeBin)
	cascadeCmd.Dir = cascadeDir
	cascadeLog := filepath.Join(cascadeDir, "cascade.log")
	lf, err := os.Create(cascadeLog)
	if err != nil {
		t.Fatal(err)
	}
	cascadeCmd.Stdout = lf
	cascadeCmd.Stderr = lf
	if err := cascadeCmd.Start(); err != nil {
		t.Fatalf("启动 Cascade: %v", err)
	}
	defer func() {
		_ = cascadeCmd.Process.Kill()
		_, _ = cascadeCmd.Process.Wait()
		_ = lf.Close()
		if data, err := os.ReadFile(cascadeLog); err == nil {
			t.Logf("[Cascade 日志]\n%s", tail(data, 2000))
		}
	}()

	cascadeGameAddr := fmt.Sprintf("127.0.0.1:%d", cascadeGamePort)
	waitForPort(t, cascadeGameAddr, 15*time.Second)
	// 等 Cascade 把后端探测为健康
	waitCascadeReady(t, cascadeHTTPPort, 30*time.Second)

	// --- 3. 假客户端连 Cascade, 走完整链路 ---
	const roomCode = "GW01"
	conn, err := net.Dial("tcp", cascadeGameAddr)
	if err != nil {
		t.Fatalf("连 Cascade: %v", err)
	}
	defer conn.Close()

	// 3a. Preregister(160) 带房间码 -> 期望引擎回 161
	if _, err := conn.Write(rwFrame(160, preregisterBody(roomCode))); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	typeInt, body := readFrame(t, conn)
	if typeInt != 161 {
		t.Fatalf("期望 Preregister 应答 (161), 得到 %d (%d 字节)", typeInt, len(body))
	}
	t.Logf("[E2E] 客户端经 Cascade 收到引擎 Preregister 应答 (161), %d 字节 —— 房间码路由+转发生效", len(body))

	// 3b. RegisterPlayer(110) -> Cascade 绑定身份后转发引擎
	const playerUUID = "0011223344556677"
	if _, err := conn.Write(rwFrame(110, registerBody("E2EPlayer", playerUUID))); err != nil {
		t.Fatal(err)
	}

	// 立即读一帧诊断: 绑定失败时 Cascade 会回 KICK(150) 带原因
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	diagType, diagBody := readFrame(t, conn)
	t.Logf("[E2E] RegisterPlayer 后首帧: 类型=%d %d 字节 %q", diagType, len(diagBody), string(diagBody))

	// 等 Go 侧会话被绑定 (身份链路)
	deadline := time.Now().Add(20 * time.Second)
	bound := false
	for time.Now().Before(deadline) && !bound {
		for _, id := range gw.Registry().IDs() {
			if s := gw.Registry().Get(id); s != nil && s.PlayerID() == playerUUID {
				bound = true
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !bound {
		t.Fatal("身份未在限期内绑定 (会话未出现该玩家 UUID)")
	}
	t.Logf("[E2E] 身份绑定成功: 会话已关联玩家 UUID %s", playerUUID)

	// --- 4. 同一后端真实开局到结算 ---
	runRealGameToSettlement(t, backend)

	_ = backend
}

// tail 取末尾 n 字节
func tail(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[len(b)-n:]
}
