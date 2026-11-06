package gateway

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rwhps/internal/game"
	"rwhps/internal/game/room"
	"rwhps/internal/util/file"
	"rwhps/internal/util/file/plugindata"
)

// --- TLS/密钥夹具 ---

// tlsFixture 现生成 CA + 服务端证书 (SAN 127.0.0.1) + 客户端证书,
// 返回服务端材料路径与客户端可用的 tls.Certificate/CA 池.
func tlsFixture(t *testing.T) (certPath, keyPath, caPath string, clientCert tls.Certificate, clientPool *x509.CertPool) {
	t.Helper()
	dir := t.TempDir()

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
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
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}

	writePEM := func(name, typ string, der []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// 服务端证书
	srvKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	srvTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "backend"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	srvDER, err := x509.CreateCertificate(rand.Reader, srvTmpl, caCert, &srvKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	certPath = writePEM("server.crt", "CERTIFICATE", srvDER)
	keyPath = writePEM("server.key", "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(srvKey))
	caPath = writePEM("ca.pem", "CERTIFICATE", caDER)

	// 客户端证书 (模拟 Cascade 代理)
	cliKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	cliTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "cascade-proxy"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	cliDER, err := x509.CreateCertificate(rand.Reader, cliTmpl, caCert, &cliKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	clientCert = tls.Certificate{Certificate: [][]byte{cliDER}, PrivateKey: cliKey}
	clientPool = x509.NewCertPool()
	clientPool.AddCert(caCert)
	return certPath, keyPath, caPath, clientCert, clientPool
}

// ed25519Fixture 生成签名密钥对: 私钥留测试用, 公钥写成 PKIX PEM 给网关
func ed25519Fixture(t *testing.T) (pubPath string, priv ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	pubPath = filepath.Join(t.TempDir(), "proxy.pub")
	if err := os.WriteFile(pubPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return pubPath, priv
}

// controlFixture 装配完整网关 + 控制面监听 + 带客户端证书的 http 客户端.
// 信任三档下默认 open (DECISIONS D-24): TLS 传输 (配了证书), 不索客户端证书,
// 令牌不验签 —— 客户端证书仍带着, 服务端不索取即不发送, 与现网行为一致.
func controlFixture(t *testing.T) (*Gateway, string, *http.Client, *testSigner) {
	t.Helper()
	certPath, keyPath, _, _, clientPool := tlsFixture(t)
	_, priv := ed25519Fixture(t)

	store, err := plugindata.Open(&file.FileUtils{Path: filepath.Join(t.TempDir(), "jti.bin")})
	if err != nil {
		t.Fatal(err)
	}
	gw, err := New(Config{
		BackendID:      "backend-t",
		RoomCode:       "GW01",
		TLSCert:        certPath,
		TLSKey:         keyPath,
		ControlAddr:    "127.0.0.1:0",
		PendingTimeout: 5 * time.Second,
	}, NewJtiStore(store))
	if err != nil {
		t.Fatalf("gateway.New: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = gw.ServeControlOn(ln) }()
	t.Cleanup(func() { gw.Stop() })

	base := "https://" + ln.Addr().String() + "/v1"
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			RootCAs:    clientPool,
			ServerName: "127.0.0.1",
		}},
		Timeout: 10 * time.Second,
	}
	signer := &testSigner{issuer: "cascade", kid: "kid-t", key: priv}
	return gw, base, client, signer
}

// withTestRoom 挂一个单例房间供快照/门面测试
func withTestRoom(t *testing.T) *room.ServerRoom {
	t.Helper()
	r := room.NewServerRoom(game.NewGameSession("room-gw", nil))
	room.SetDefault(r)
	t.Cleanup(func() { room.SetDefault(nil) })
	return r
}

func doJSON(t *testing.T, client *http.Client, method, url string, body any) (*http.Response, map[string]any) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

// --- health / rooms ---

func TestControlHealthReadiness(t *testing.T) {
	gw, base, client, _ := controlFixture(t)
	withTestRoom(t)

	resp, body := doJSON(t, client, http.MethodGet, base+"/health", nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("未就绪应 503, got %d", resp.StatusCode)
	}
	if body["code"] != CodeBackendNotReady {
		t.Fatalf("code = %v", body["code"])
	}

	gw.SetReady(true)
	resp, body = doJSON(t, client, http.MethodGet, base+"/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("就绪应 200, got %d", resp.StatusCode)
	}
	if body["backendId"] != "backend-t" || body["status"] != "ready" {
		t.Fatalf("health body = %v", body)
	}
}

func TestControlRoomsSnapshot(t *testing.T) {
	_, base, client, _ := controlFixture(t)
	withTestRoom(t)

	resp, body := doJSON(t, client, http.MethodGet, base+"/rooms", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d", resp.StatusCode)
	}
	if body["backendId"] != "backend-t" {
		t.Fatalf("backendId = %v", body["backendId"])
	}
	rooms, ok := body["rooms"].([]any)
	if !ok || len(rooms) != 1 {
		t.Fatalf("rooms = %v (期望恰一个房间)", body["rooms"])
	}
	one := rooms[0].(map[string]any)
	if one["code"] != "GW01" {
		t.Fatalf("code = %v", one["code"])
	}
	for _, key := range []string{"title", "mapName", "maxPlayers", "players", "inGame", "ownerId", "ownerName", "adminCount", "createdAt"} {
		if _, has := one[key]; !has {
			t.Fatalf("快照缺字段 %s: %v", key, one)
		}
	}
}

// --- 身份绑定 ---

func TestControlIdentityBind(t *testing.T) {
	gw, base, client, signer := controlFixture(t)
	withTestRoom(t)

	sess := gw.Registry().Register("11111111-1111-4111-8111-111111111111", nil)
	now := time.Now()
	token := signer.sign(t, "backend-t", sess.ID, "player-uuid", "jti-a", now.Unix(), now.Add(30*time.Second).Unix())

	resp, _ := doJSON(t, client, http.MethodPut, base+"/sessions/"+sess.ID+"/identity", map[string]string{"token": token})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("绑定应 204, got %d", resp.StatusCode)
	}
	if sess.State() != StateBound || sess.PlayerID() != "player-uuid" {
		t.Fatalf("会话应已绑定: state=%v player=%q", sess.State(), sess.PlayerID())
	}

	// 幂等重试 (同身份同 jti): 204
	resp, _ = doJSON(t, client, http.MethodPut, base+"/sessions/"+sess.ID+"/identity", map[string]string{"token": token})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("幂等重试应 204, got %d", resp.StatusCode)
	}

	// 换 jti 重试: 409 冲突
	token2 := signer.sign(t, "backend-t", sess.ID, "player-uuid", "jti-b", now.Unix(), now.Add(30*time.Second).Unix())
	resp, body := doJSON(t, client, http.MethodPut, base+"/sessions/"+sess.ID+"/identity", map[string]string{"token": token2})
	if resp.StatusCode != http.StatusConflict || body["code"] != CodeSessionIdentityConflict {
		t.Fatalf("换 jti 应 409 %s, got %d %v", CodeSessionIdentityConflict, resp.StatusCode, body)
	}
}

func TestControlIdentityRejects(t *testing.T) {
	gw, base, client, signer := controlFixture(t)
	withTestRoom(t)
	now := time.Now()

	// 未知会话: 404
	token := signer.sign(t, "backend-t", "22222222-2222-4222-8222-222222222222", "p", "jti-u", now.Unix(), now.Add(30*time.Second).Unix())
	resp, body := doJSON(t, client, http.MethodPut, base+"/sessions/22222222-2222-4222-8222-222222222222/identity", map[string]string{"token": token})
	if resp.StatusCode != http.StatusNotFound || body["code"] != CodeSessionNotFound {
		t.Fatalf("未知会话应 404 %s, got %d %v", CodeSessionNotFound, resp.StatusCode, body)
	}

	// 签名篡改仍接受 (信任三档不验签, DECISIONS D-24): 声明合法即绑定
	sessTampered := gw.Registry().Register("55555555-5555-4555-8555-555555555555", nil)
	bad := signer.sign(t, "backend-t", sessTampered.ID, "p", "jti-v", now.Unix(), now.Add(30*time.Second).Unix())
	bad = bad[:len(bad)-4] + "AAAA"
	resp, body = doJSON(t, client, http.MethodPut, base+"/sessions/"+sessTampered.ID+"/identity", map[string]string{"token": bad})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("不验签: 签名篡改但声明合法应 204, got %d %v", resp.StatusCode, body)
	}

	// 结构非法 (声明无法解析): 400 SESSION_IDENTITY_INVALID
	resp, body = doJSON(t, client, http.MethodPut, base+"/sessions/"+sessTampered.ID+"/identity", map[string]string{"token": "not-a-jwt"})
	if resp.StatusCode != http.StatusBadRequest || body["code"] != CodeSessionIdentityInvalid {
		t.Fatalf("结构非法应 400 %s, got %d %v", CodeSessionIdentityInvalid, resp.StatusCode, body)
	}

	// sid 与路径不符: 400
	sess := gw.Registry().Register("33333333-3333-4333-8333-333333333333", nil)
	tokenOther := signer.sign(t, "backend-t", "44444444-4444-4444-8444-444444444444", "p", "jti-w", now.Unix(), now.Add(30*time.Second).Unix())
	resp, body = doJSON(t, client, http.MethodPut, base+"/sessions/"+sess.ID+"/identity", map[string]string{"token": tokenOther})
	if resp.StatusCode != http.StatusBadRequest || body["code"] != CodeInvalidRequest {
		t.Fatalf("sid 不符应 400 %s, got %d %v", CodeInvalidRequest, resp.StatusCode, body)
	}

	// jti 重放 (另一会话消费过): 400 —— 不验签不影响一次性账本
	gw.Registry().Register("44444444-4444-4444-8444-444444444444", nil)
	if err := gw.jti.Consume("jti-replay", now.Add(30*time.Second).Unix()); err != nil {
		t.Fatal(err)
	}
	tokenReplay := signer.sign(t, "backend-t", sess.ID, "p", "jti-replay", now.Unix(), now.Add(30*time.Second).Unix())
	resp, body = doJSON(t, client, http.MethodPut, base+"/sessions/"+sess.ID+"/identity", map[string]string{"token": tokenReplay})
	if resp.StatusCode != http.StatusBadRequest || body["code"] != CodeSessionIdentityReplayed {
		t.Fatalf("jti 重放应 400 %s, got %d %v", CodeSessionIdentityReplayed, resp.StatusCode, body)
	}
}

// --- 房间门面 ---

func TestControlRoomFacade(t *testing.T) {
	_, base, client, _ := controlFixture(t)
	withTestRoom(t)

	// create: 本服码幂等 201
	resp, body := doJSON(t, client, http.MethodPost, base+"/rooms/create", map[string]any{"code": "GW01", "title": "x", "maxPlayers": 8})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create 本服码应 201, got %d %v", resp.StatusCode, body)
	}
	// create: 其它码 409
	resp, body = doJSON(t, client, http.MethodPost, base+"/rooms/create", map[string]any{"code": "ZZ99"})
	if resp.StatusCode != http.StatusConflict || body["code"] != CodeRoomConflict {
		t.Fatalf("create 异码应 409, got %d %v", resp.StatusCode, body)
	}

	// get: 本服码 200 带 members
	resp, body = doJSON(t, client, http.MethodGet, base+"/rooms/GW01", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get 应 200, got %d", resp.StatusCode)
	}
	if _, has := body["members"]; !has {
		t.Fatalf("详情应带 members: %v", body)
	}
	// get: 未知码 404
	resp, body = doJSON(t, client, http.MethodGet, base+"/rooms/ZZ99", nil)
	if resp.StatusCode != http.StatusNotFound || body["code"] != CodeRoomNotFound {
		t.Fatalf("get 未知码应 404, got %d %v", resp.StatusCode, body)
	}

	// delete: 单房间后端不支持 400
	resp, body = doJSON(t, client, http.MethodDelete, base+"/rooms/GW01", nil)
	if resp.StatusCode != http.StatusBadRequest || body["code"] != CodeInvalidRequest {
		t.Fatalf("delete 应 400 %s, got %d %v", CodeInvalidRequest, resp.StatusCode, body)
	}

	// players: 200 + 数组
	resp, body = doJSON(t, client, http.MethodGet, base+"/rooms/GW01/players", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("players 应 200, got %d", resp.StatusCode)
	}
	if _, has := body["players"]; !has {
		t.Fatalf("players 缺 players 字段: %v", body)
	}

	// kick/ban: 未知玩家 404
	resp, body = doJSON(t, client, http.MethodPost, base+"/rooms/GW01/kick", map[string]string{"playerId": "nope"})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("kick 未知玩家应 404, got %d %v", resp.StatusCode, body)
	}
	resp, body = doJSON(t, client, http.MethodPost, base+"/rooms/GW01/ban", map[string]string{"playerId": "nope"})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("ban 未知玩家应 404, got %d %v", resp.StatusCode, body)
	}

	// settings: mapName 作用到房间
	resp, body = doJSON(t, client, http.MethodPut, base+"/rooms/GW01/settings", map[string]any{"mapName": "New Map"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings 应 200, got %d %v", resp.StatusCode, body)
	}
	if body["mapName"] != "New Map" {
		t.Fatalf("settings 未生效: %v", body["mapName"])
	}
}
