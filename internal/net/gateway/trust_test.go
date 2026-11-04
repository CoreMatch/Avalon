package gateway

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 信任三档单测 (有意修正, 作者已批准, 2026-11-04; DECISIONS D-24):
// open 放行任意来源 / ip 拒绝非白名单 / pinned 拒绝错指纹.

// newOpenGateway 明文裸奔网关 (无 TLS 材料)
func newOpenGateway(t *testing.T) *Gateway {
	t.Helper()
	gw, err := New(Config{
		BackendID:   "backend-t",
		RoomCode:    "GW01",
		ControlAddr: "127.0.0.1:0",
	}, NewJtiStore(mustJtiStore(t)))
	if err != nil {
		t.Fatalf("gateway.New: %v", err)
	}
	return gw
}

// dialPreamble 明文拨号并写合法前导, 返回连接
func dialPreamble(t *testing.T, addr, sessionID string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePreambleForTest(conn, sessionID); err != nil {
		t.Fatal(err)
	}
	return conn
}

// TestTrustOpenAcceptsAnySource open 档 (默认): 明文传输, 任何来源都放行,
// 前导与会话门控照常.
func TestTrustOpenAcceptsAnySource(t *testing.T) {
	gw := newOpenGateway(t)
	addr, accepted := startGameListener(t, gw)

	sessionID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	conn := dialPreamble(t, addr, sessionID)
	defer conn.Close()

	select {
	case ch := <-accepted:
		if ch == nil {
			t.Fatal("open 档应放行任意来源")
		}
		if ch.Gate == nil {
			t.Fatal("应挂门控")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AcceptGame 未在时限内完成")
	}
	if gw.Registry().Get(sessionID) == nil {
		t.Fatal("会话应登记")
	}
}

// TestTrustOpenControlPlaintext open 档控制面走明文也可服务
func TestTrustOpenControlPlaintext(t *testing.T) {
	gw := newOpenGateway(t)
	gw.SetReady(true)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = gw.ServeControlOn(ln) }()
	t.Cleanup(func() { gw.Stop() })

	resp, err := http.Get("http://" + ln.Addr().String() + "/v1/health")
	if err != nil {
		t.Fatalf("明文控制面: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open 档明文 health = %d", resp.StatusCode)
	}
}

// TestTrustIPRejectsNonWhitelisted ip 档: 白名单外来源一律拒绝 (游戏口与控制面)
func TestTrustIPRejectsNonWhitelisted(t *testing.T) {
	list, err := ParseIPAllowList([]string{"10.9.9.9/32"})
	if err != nil {
		t.Fatal(err)
	}
	gw, err := New(Config{
		BackendID:   "backend-t",
		RoomCode:    "GW01",
		ControlAddr: "127.0.0.1:0",
		TrustMode:   TrustIP,
		TrustedIPs:  list,
	}, NewJtiStore(mustJtiStore(t)))
	if err != nil {
		t.Fatal(err)
	}

	// 游戏口: 127.0.0.1 不在 10.9.9.9/32 内 -> 拒绝
	addr, accepted := startGameListener(t, gw)
	conn := dialPreamble(t, addr, "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee")
	defer conn.Close()
	select {
	case ch := <-accepted:
		if ch != nil {
			t.Fatal("ip 档应拒绝非白名单来源")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AcceptGame 未在时限内返回")
	}
	if gw.Registry().Size() != 0 {
		t.Fatal("被拒连接不应登记会话")
	}

	// 控制面: 同款拒绝 (403 FORBIDDEN)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = gw.ServeControlOn(ln) }()
	t.Cleanup(func() { gw.Stop() })
	gw.SetReady(true)

	resp, err := http.Get("http://" + ln.Addr().String() + "/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("ip 档控制面应 403, got %d", resp.StatusCode)
	}
}

// TestTrustIPAcceptsWhitelistedCIDR ip 档: CIDR 命中的来源放行
func TestTrustIPAcceptsWhitelistedCIDR(t *testing.T) {
	list, err := ParseIPAllowList([]string{"127.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	gw, err := New(Config{
		BackendID:   "backend-t",
		RoomCode:    "GW01",
		ControlAddr: "127.0.0.1:0",
		TrustMode:   TrustIP,
		TrustedIPs:  list,
	}, NewJtiStore(mustJtiStore(t)))
	if err != nil {
		t.Fatal(err)
	}
	addr, accepted := startGameListener(t, gw)

	conn := dialPreamble(t, addr, "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee")
	defer conn.Close()
	select {
	case ch := <-accepted:
		if ch == nil {
			t.Fatal("CIDR 命中应放行")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AcceptGame 未在时限内完成")
	}
}

// pinnedGateway TLS + 客户端证书指纹固定的网关; fingerprints 为十六进制列表
func pinnedGateway(t *testing.T, fingerprints []string) (*Gateway, string, tls.Certificate) {
	t.Helper()
	certPath, keyPath, _, clientCert, _ := tlsFixture(t)
	fps, err := ParseFingerprints(fingerprints)
	if err != nil {
		t.Fatal(err)
	}
	gw, err := New(Config{
		BackendID:          "backend-t",
		RoomCode:           "GW01",
		ControlAddr:        "127.0.0.1:0",
		TrustMode:          TrustPinned,
		PinnedFingerprints: fps,
		TLSCert:            certPath,
		TLSKey:             keyPath,
	}, NewJtiStore(mustJtiStore(t)))
	if err != nil {
		t.Fatal(err)
	}
	return gw, certPath, clientCert
}

// TestTrustPinnedRejectsWrongFingerprint pinned 档: 指纹不匹配的客户端证书被拒
func TestTrustPinnedRejectsWrongFingerprint(t *testing.T) {
	wrong := hex.EncodeToString(sha256.New().Sum(nil)) // 任意 32 字节
	gw, _, clientCert := pinnedGateway(t, []string{wrong})
	addr, accepted := startGameListener(t, gw)

	conn, err := tls.Dial("tcp", addr, &tls.Config{
		Certificates:       []tls.Certificate{clientCert},
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("握手本身应完成 (拒绝发生在指纹比对): %v", err)
	}
	defer conn.Close()
	if err := writePreambleForTest(conn, "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"); err != nil {
		t.Fatal(err)
	}

	select {
	case ch := <-accepted:
		if ch != nil {
			t.Fatal("pinned 档应拒绝错指纹")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AcceptGame 未在时限内返回")
	}
	if gw.Registry().Size() != 0 {
		t.Fatal("错指纹不应登记会话")
	}
}

// TestTrustPinnedAcceptsMatchingFingerprint pinned 档: 指纹命中即放行;
// 重叠窗口 (新旧指纹并存) 同样放行 —— 轮换不停服的配置基础.
func TestTrustPinnedAcceptsMatchingFingerprint(t *testing.T) {
	_, _, _, clientCert, _ := tlsFixture(t)
	good := CertFingerprintHex(clientCert.Certificate[0])
	stale := hex.EncodeToString(sha256.New().Sum(nil))

	// 列表同时含旧 (不匹配) 与新 (匹配) 指纹: 轮换重叠期语义
	gw, _, _ := pinnedGateway(t, []string{stale, good})
	addr, accepted := startGameListener(t, gw)

	conn, err := tls.Dial("tcp", addr, &tls.Config{
		Certificates:       []tls.Certificate{clientCert},
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := writePreambleForTest(conn, "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"); err != nil {
		t.Fatal(err)
	}

	select {
	case ch := <-accepted:
		if ch == nil {
			t.Fatal("指纹命中应放行 (含重叠窗口)")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AcceptGame 未在时限内完成")
	}
}

// TestTrustPinnedControlPlane pinned 档控制面: 错指纹 403, 正确指纹放行
func TestTrustPinnedControlPlane(t *testing.T) {
	_, _, _, clientCert, _ := tlsFixture(t)
	good := CertFingerprintHex(clientCert.Certificate[0])
	wrong := hex.EncodeToString(sha256.New().Sum(nil))

	for _, c := range []struct {
		name string
		fps  []string
		want int
	}{
		{"错指纹", []string{wrong}, http.StatusForbidden},
		{"正确指纹", []string{good}, http.StatusOK},
	} {
		t.Run(c.name, func(t *testing.T) {
			gw, _, _ := pinnedGateway(t, c.fps)
			gw.SetReady(true)
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			go func() { _ = gw.ServeControlOn(ln) }()
			t.Cleanup(func() { gw.Stop() })

			client := &http.Client{
				Transport: &http.Transport{TLSClientConfig: &tls.Config{
					Certificates:       []tls.Certificate{clientCert},
					InsecureSkipVerify: true,
				}},
				Timeout: 10 * time.Second,
			}
			resp, err := client.Get("https://" + ln.Addr().String() + "/v1/health")
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != c.want {
				t.Fatalf("%s: health = %d, 期望 %d", c.name, resp.StatusCode, c.want)
			}
		})
	}
}

// TestTrustModeValidation 档位与材料的组合校验 (显式失败, 不静默)
func TestTrustModeValidation(t *testing.T) {
	jti := func() *JtiStore { return NewJtiStore(mustJtiStore(t)) }
	certPath, keyPath, _, clientCert, _ := tlsFixture(t)
	goodFP, err := hex.DecodeString(CertFingerprintHex(clientCert.Certificate[0]))
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		cfg  Config
	}{
		{"ip 档空白名单", Config{BackendID: "b", RoomCode: "r", ControlAddr: ":0", TrustMode: TrustIP, TrustedIPs: &ipAllowList{}}},
		{"pinned 档无指纹", Config{BackendID: "b", RoomCode: "r", ControlAddr: ":0", TrustMode: TrustPinned, TLSCert: certPath, TLSKey: keyPath}},
		{"pinned 档无 TLS", Config{BackendID: "b", RoomCode: "r", ControlAddr: ":0", TrustMode: TrustPinned, PinnedFingerprints: [][]byte{goodFP}}},
		{"只配证书不配密钥", Config{BackendID: "b", RoomCode: "r", ControlAddr: ":0", TLSCert: certPath}},
	}
	for _, c := range cases {
		if _, err := New(c.cfg, jti()); err == nil {
			t.Fatalf("%s: 应报错", c.name)
		}
	}

	// 解析层的显式失败
	if _, err := ParseTrustMode("naked"); err == nil {
		t.Fatal("未知档位应报错")
	}
	if _, err := ParseIPAllowList([]string{"not-an-ip"}); err == nil {
		t.Fatal("非法 IP 条目应报错")
	}
	if _, err := ParseIPAllowList([]string{"10.0.0.0/77"}); err == nil {
		t.Fatal("非法 CIDR 应报错")
	}
	if _, err := ParseFingerprints([]string{"zz"}); err == nil {
		t.Fatal("非法指纹应报错")
	}
	if _, err := ParseFingerprints([]string{hex.EncodeToString([]byte("short"))}); err == nil {
		t.Fatal("长度不足 32 字节的指纹应报错")
	}
}

// TestParseTrustModeAndAllowList 解析语义: 空值默认 open; CIDR/精确 IP 命中
func TestParseTrustModeAndAllowList(t *testing.T) {
	for _, s := range []string{"", "open", "OPEN"} {
		if m, err := ParseTrustMode(s); err != nil || m != TrustOpen {
			t.Fatalf("ParseTrustMode(%q) = %v, %v", s, m, err)
		}
	}
	if m, _ := ParseTrustMode("ip"); m != TrustIP {
		t.Fatal("ip")
	}
	if m, _ := ParseTrustMode("pinned"); m != TrustPinned {
		t.Fatal("pinned")
	}

	list, err := ParseIPAllowList([]string{"127.0.0.1", "10.0.0.0/8", " "})
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range []string{"127.0.0.1", "10.5.5.5"} {
		if !list.contains(net.ParseIP(hit)) {
			t.Fatalf("%s 应命中", hit)
		}
	}
	if list.contains(net.ParseIP("192.168.0.1")) {
		t.Fatal("192.168.0.1 不应命中")
	}
	if list.empty() {
		t.Fatal("非空名单")
	}
	var nilList *ipAllowList
	if !nilList.empty() || nilList.contains(net.ParseIP("127.0.0.1")) {
		t.Fatal("nil 名单应为空且不命中")
	}
}

// TestControlHealthWaitsHelper helper 就绪探针纳入 ready 判据 (DECISIONS D-21 修订)
func TestControlHealthWaitsHelper(t *testing.T) {
	gw := newOpenGateway(t)
	helperUp := false
	gw.cfg.HelperReady = func() bool { return helperUp }
	gw.SetReady(true)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = gw.ServeControlOn(ln) }()
	t.Cleanup(func() { gw.Stop() })

	get := func() int {
		resp, err := http.Get("http://" + ln.Addr().String() + "/v1/health")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if code := get(); code != http.StatusServiceUnavailable {
		t.Fatalf("helper 未就绪应 503, got %d", code)
	}
	helperUp = true
	if code := get(); code != http.StatusOK {
		t.Fatalf("helper 就绪应 200, got %d", code)
	}
}

// TestResolveMaterialPath cwd 优先、再可执行文件目录的回退解析
func TestResolveMaterialPath(t *testing.T) {
	dir := t.TempDir()
	probe := filepath.Join(dir, "probe.crt")
	if err := os.WriteFile(probe, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 存在的路径 (绝对) 原样即用
	if p, err := resolveMaterialPath(probe); err != nil || p != probe {
		t.Fatalf("存在路径应原样即用: %v %v", p, err)
	}

	// cwd 找不到时回落可执行文件目录
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("取不到可执行文件路径: %v", err)
	}
	const rel = "rwhps-material-probe.crt"
	nextExe := filepath.Join(filepath.Dir(exe), rel)
	if err := os.WriteFile(nextExe, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(nextExe) })
	if p, err := resolveMaterialPath(rel); err != nil || p != nextExe {
		t.Fatalf("应回落到可执行文件目录: %v %v", p, err)
	}

	// 缺失显式报错
	if _, err := resolveMaterialPath("definitely-missing-12345.crt"); err == nil {
		t.Fatal("缺失材料应报错")
	}
	if _, err := resolveMaterialPath(""); err == nil {
		t.Fatal("空路径应报错")
	}
}
