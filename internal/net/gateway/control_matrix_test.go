package gateway

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"net"
	"net/http"
	"testing"
	"time"
)

// 端点 × 信任档矩阵 (契约 v1.2.1 "Backend trust strategies": 信任检查包住
// 整个 /v1 面, 任何控制端点都可能返回 403 FORBIDDEN) 与错误路径补齐.
//
// 每个 /v1 端点各取一条请求:
//   - open 档放行 (得到端点自身状态码, 绝不是 403);
//   - ip 档非白名单来源一律 403 FORBIDDEN;
//   - pinned 档错指纹一律 403 FORBIDDEN, 正确指纹放行到端点自身状态码.

// matrixEndpoint 一个端点的代表请求与其"通过信任检查后"的期望状态.
type matrixEndpoint struct {
	name       string
	method     string
	path       string
	body       any
	wantStatus int
}

func matrixEndpoints() []matrixEndpoint {
	return []matrixEndpoint{
		{"health", http.MethodGet, "/v1/health", nil, http.StatusOK},
		{"rooms", http.MethodGet, "/v1/rooms", nil, http.StatusOK},
		// 空 token 在信任检查之后才被拒 (400 INVALID_REQUEST), 证明请求穿过了信任层.
		{"identity", http.MethodPut, "/v1/sessions/matrix-session/identity", map[string]string{"token": ""}, http.StatusBadRequest},
		{"create", http.MethodPost, "/v1/rooms/create", map[string]any{"code": "GW01"}, http.StatusCreated},
		{"get room", http.MethodGet, "/v1/rooms/GW01", nil, http.StatusOK},
		{"delete room", http.MethodDelete, "/v1/rooms/GW01", nil, http.StatusBadRequest},
		{"settings", http.MethodPut, "/v1/rooms/GW01/settings", map[string]any{"mapName": "Matrix Map"}, http.StatusOK},
		{"kick", http.MethodPost, "/v1/rooms/GW01/kick", map[string]string{"playerId": "nope"}, http.StatusNotFound},
		{"ban", http.MethodPost, "/v1/rooms/GW01/ban", map[string]string{"playerId": "nope"}, http.StatusNotFound},
		{"players", http.MethodGet, "/v1/rooms/GW01/players", nil, http.StatusOK},
	}
}

// startMatrixControl 在 :0 监听器上起控制面, 返回基地址.
func startMatrixControl(t *testing.T, gw *Gateway) string {
	t.Helper()
	gw.SetReady(true)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = gw.ServeControlOn(ln) }()
	t.Cleanup(func() { gw.Stop() })
	scheme := "http"
	if gw.tlsCfg != nil {
		scheme = "https"
	}
	return scheme + "://" + ln.Addr().String()
}

func doMatrix(t *testing.T, client *http.Client, base string, ep matrixEndpoint) (int, string) {
	t.Helper()
	resp, body := doJSON(t, client, ep.method, base+ep.path, ep.body)
	code, _ := body["code"].(string)
	return resp.StatusCode, code
}

// TestTrustMatrixOpenPassesEveryEndpoint open 档: 每个端点都穿过信任层,
// 得到端点自身的状态码 (放行证明).
func TestTrustMatrixOpenPassesEveryEndpoint(t *testing.T) {
	gw := newOpenGateway(t)
	withTestRoom(t)
	base := startMatrixControl(t, gw)
	client := &http.Client{Timeout: 10 * time.Second}

	for _, ep := range matrixEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			status, code := doMatrix(t, client, base, ep)
			if code == CodeForbidden {
				t.Fatalf("open 档 %s %s 不应被信任层拦截 (FORBIDDEN)", ep.method, ep.path)
			}
			if status != ep.wantStatus {
				t.Fatalf("open 档 %s %s = %d (%s), 期望 %d", ep.method, ep.path, status, code, ep.wantStatus)
			}
		})
	}
}

// TestTrustMatrixIPRejectsEveryEndpoint ip 档: 非白名单来源对每个端点都是
// 403 FORBIDDEN (拒绝证明).
func TestTrustMatrixIPRejectsEveryEndpoint(t *testing.T) {
	list, err := ParseIPAllowList([]string{"10.9.9.9/32"}) // 127.0.0.1 不在名单内
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
	withTestRoom(t)
	base := startMatrixControl(t, gw)
	client := &http.Client{Timeout: 10 * time.Second}

	for _, ep := range matrixEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			status, code := doMatrix(t, client, base, ep)
			if status != http.StatusForbidden || code != CodeForbidden {
				t.Fatalf("ip 档 %s %s = %d (%s), 期望 403 %s", ep.method, ep.path, status, code, CodeForbidden)
			}
		})
	}
}

// matrixPinnedClient 带客户端证书的 https 客户端
func matrixPinnedClient(clientCert tls.Certificate) *http.Client {
	return &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			Certificates:       []tls.Certificate{clientCert},
			InsecureSkipVerify: true,
		}},
		Timeout: 10 * time.Second,
	}
}

// TestTrustMatrixPinnedRejectsWrongFingerprintEveryEndpoint pinned 档:
// 指纹不匹配时对每个端点都是 403 FORBIDDEN.
func TestTrustMatrixPinnedRejectsWrongFingerprintEveryEndpoint(t *testing.T) {
	wrong := hex.EncodeToString(sha256.New().Sum(nil))
	gw, _, clientCert := pinnedGateway(t, []string{wrong})
	withTestRoom(t)
	base := startMatrixControl(t, gw)
	client := matrixPinnedClient(clientCert)

	for _, ep := range matrixEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			status, code := doMatrix(t, client, base, ep)
			if status != http.StatusForbidden || code != CodeForbidden {
				t.Fatalf("pinned 档错指纹 %s %s = %d (%s), 期望 403 %s", ep.method, ep.path, status, code, CodeForbidden)
			}
		})
	}
}

// TestTrustMatrixPinnedAcceptsMatchingFingerprintEveryEndpoint pinned 档:
// 指纹命中时每个端点穿过信任层, 得到端点自身状态码.
func TestTrustMatrixPinnedAcceptsMatchingFingerprintEveryEndpoint(t *testing.T) {
	_, _, _, clientCert, _ := tlsFixture(t)
	good := CertFingerprintHex(clientCert.Certificate[0])
	gw, _, _ := pinnedGateway(t, []string{good})
	withTestRoom(t)
	base := startMatrixControl(t, gw)
	client := matrixPinnedClient(clientCert)

	for _, ep := range matrixEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			status, code := doMatrix(t, client, base, ep)
			if code == CodeForbidden {
				t.Fatalf("pinned 档正确指纹 %s %s 不应被拦截 (FORBIDDEN)", ep.method, ep.path)
			}
			if status != ep.wantStatus {
				t.Fatalf("pinned 档正确指纹 %s %s = %d (%s), 期望 %d", ep.method, ep.path, status, code, ep.wantStatus)
			}
		})
	}
}

// TestTrustMatrixPinnedIPAllowlistStillEnforced pinned 档同时配了来源白名单
// 时, 白名单外的来源先被拒 (指纹正确也不放行).
func TestTrustMatrixPinnedIPAllowlistStillEnforced(t *testing.T) {
	_, _, _, clientCert, _ := tlsFixture(t)
	good := CertFingerprintHex(clientCert.Certificate[0])
	list, err := ParseIPAllowList([]string{"10.9.9.9/32"})
	if err != nil {
		t.Fatal(err)
	}
	fps, err := ParseFingerprints([]string{good})
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath, _, _, _ := tlsFixture(t)
	gw, err := New(Config{
		BackendID:          "backend-t",
		RoomCode:           "GW01",
		ControlAddr:        "127.0.0.1:0",
		TrustMode:          TrustPinned,
		TrustedIPs:         list,
		PinnedFingerprints: fps,
		TLSCert:            certPath,
		TLSKey:             keyPath,
	}, NewJtiStore(mustJtiStore(t)))
	if err != nil {
		t.Fatal(err)
	}
	withTestRoom(t)
	base := startMatrixControl(t, gw)
	client := matrixPinnedClient(clientCert)

	status, code := doMatrix(t, client, base, matrixEndpoints()[0])
	if status != http.StatusForbidden || code != CodeForbidden {
		t.Fatalf("pinned 档白名单外来源 = %d (%s), 期望 403 %s (指纹正确也不放行)", status, code, CodeForbidden)
	}
}

// TestControlRoomFacadeUnknownRoomErrors 补齐门面错误路径: 未知房间码在
// 每个房间端点上都是 404 ROOM_NOT_FOUND.
func TestControlRoomFacadeUnknownRoomErrors(t *testing.T) {
	_, base, client, _ := controlFixture(t)
	withTestRoom(t)

	// controlFixture 的 base 已含 /v1 前缀.
	for _, ep := range []matrixEndpoint{
		{"get unknown", http.MethodGet, "/rooms/ZZ99", nil, http.StatusNotFound},
		{"delete unknown", http.MethodDelete, "/rooms/ZZ99", nil, http.StatusNotFound},
		{"settings unknown", http.MethodPut, "/rooms/ZZ99/settings", map[string]any{"mapName": "x"}, http.StatusNotFound},
		{"kick unknown room", http.MethodPost, "/rooms/ZZ99/kick", map[string]string{"playerId": "p"}, http.StatusNotFound},
		{"ban unknown room", http.MethodPost, "/rooms/ZZ99/ban", map[string]string{"playerId": "p"}, http.StatusNotFound},
		{"players unknown", http.MethodGet, "/rooms/ZZ99/players", nil, http.StatusNotFound},
	} {
		t.Run(ep.name, func(t *testing.T) {
			status, code := doMatrix(t, client, base, ep)
			if status != ep.wantStatus || code != CodeRoomNotFound {
				t.Fatalf("%s %s = %d (%s), 期望 404 %s", ep.method, ep.path, status, code, CodeRoomNotFound)
			}
		})
	}
}

// TestControlIdentityEmptyTokenAndClosedSession 补齐身份端点错误路径:
// 空 token -> 400 INVALID_REQUEST; 已关闭会话 -> 410 SESSION_EXPIRED.
func TestControlIdentityEmptyTokenAndClosedSession(t *testing.T) {
	gw, base, client, signer := controlFixture(t)
	withTestRoom(t)

	resp, body := doJSON(t, client, http.MethodPut, base+"/sessions/whatever/identity", map[string]string{"token": ""})
	if resp.StatusCode != http.StatusBadRequest || body["code"] != CodeInvalidRequest {
		t.Fatalf("空 token 应 400 %s, got %d %v", CodeInvalidRequest, resp.StatusCode, body)
	}

	// 直接关闭一个仍在注册表中的会话, 命中 handleIdentity 的 StateClosed 分支.
	sess := gw.Registry().Register("66666666-6666-4666-8666-666666666666", nil)
	sess.close()
	now := time.Now()
	token := signer.sign(t, "backend-t", sess.ID, "p", "jti-closed", now.Unix(), now.Add(30*time.Second).Unix())
	resp, body = doJSON(t, client, http.MethodPut, base+"/sessions/"+sess.ID+"/identity", map[string]string{"token": token})
	if resp.StatusCode != http.StatusGone || body["code"] != CodeSessionExpired {
		t.Fatalf("已关闭会话应 410 %s, got %d %v", CodeSessionExpired, resp.StatusCode, body)
	}
}
