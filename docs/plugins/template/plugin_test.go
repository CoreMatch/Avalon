package main

// 模板自测试: 只用标准库, 随模板目录拷到任何仓库都能跑 (go test .).
// 另有一个真 hub 对接测试在 Avalon 仓 internal/plugin/template_e2e_test.go.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

var testSecretBytes = bytes.Repeat([]byte{0x5a}, 32)

func newTestPlugin() *Plugin {
	p := NewPlugin("tpl-test", "0.0.1", testSecretBytes)
	p.Handle("room.create", true, func(_ []byte) (int, []byte) {
		return http.StatusOK, VerdictJSON(Verdict{Allowed: false, Reason: "test deny"})
	})
	p.Handle("game.over", false, func(_ []byte) (int, []byte) {
		return http.StatusOK, []byte(`{}`)
	})
	return p
}

// signedRequest 按契约给请求打签名 (模拟 Avalon 侧行为)
func signedRequest(t *testing.T, method, url string, body []byte) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now().Unix()
	req.Header.Set(headerTimestamp, strconv.FormatInt(ts, 10))
	req.Header.Set(headerSignature, Sign(testSecretBytes, ts, body))
	return req
}

func TestSignVerifyRoundtrip(t *testing.T) {
	body := []byte(`{"event":"room.create"}`)
	ts := time.Now().Unix()
	sig := Sign(testSecretBytes, ts, body)
	if err := Verify(testSecretBytes, strconv.FormatInt(ts, 10), sig, body); err != nil {
		t.Fatalf("自签自验应通过: %v", err)
	}
	// 正文被篡改 -> 验签失败
	if err := Verify(testSecretBytes, strconv.FormatInt(ts, 10), sig, []byte(`tampered`)); err == nil {
		t.Fatal("篡改正文后验签应失败")
	}
	// 时间戳过期 -> 拒绝
	old := time.Now().Add(-10 * time.Minute).Unix()
	if err := Verify(testSecretBytes, strconv.FormatInt(old, 10), Sign(testSecretBytes, old, body), body); err == nil {
		t.Fatal("超出 ±300s 的时间戳应拒绝")
	}
}

func TestManifestShapeAndSignature(t *testing.T) {
	p := newTestPlugin()
	srv := httptest.NewServer(p)
	defer srv.Close()

	resp, err := srv.Client().Do(signedRequest(t, http.MethodGet, srv.URL+"/v1/manifest", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("manifest 状态码 = %d", resp.StatusCode)
	}

	var m struct {
		Schema   string                     `json:"schema"`
		Protocol string                     `json:"protocol"`
		ID       string                     `json:"id"`
		Version  string                     `json:"version"`
		Events   map[string]EventCapability `json:"events"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("manifest 不是合法 JSON: %v", err)
	}
	if m.Schema != ManifestSchemaValue || m.Protocol != ProtocolID || m.ID != "tpl-test" {
		t.Fatalf("manifest 字段不符: %+v", m)
	}
	if cap, ok := m.Events["room.create"]; !ok || len(cap.Versions) != 1 || cap.Versions[0] != 1 || !cap.Veto {
		t.Fatalf("room.create 能力声明不符: %+v", m.Events)
	}
	if cap, ok := m.Events["game.over"]; !ok || cap.Veto {
		t.Fatalf("game.over 不应声明 veto: %+v", m.Events)
	}

	// 应答必须带签名且可验证
	ts := resp.Header.Get(headerTimestamp)
	sig := resp.Header.Get(headerSignature)
	if err := Verify(testSecretBytes, ts, sig, body); err != nil {
		t.Fatalf("manifest 应答签名应可验证: %v", err)
	}
}

func TestManifestRejectsUnsignedRequest(t *testing.T) {
	p := newTestPlugin()
	srv := httptest.NewServer(p)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/manifest", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("未签名请求应 401, got %d", resp.StatusCode)
	}
}

func TestVetoEventDenyVerdict(t *testing.T) {
	p := newTestPlugin()
	srv := httptest.NewServer(p)
	defer srv.Close()

	body := []byte(`{"event":"room.create","room":{"kind":"relay"}}`)
	resp, err := srv.Client().Do(signedRequest(t, http.MethodPost, srv.URL+"/v1/events/room.create", body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d", resp.StatusCode)
	}
	var v Verdict
	if err := json.Unmarshal(respBody, &v); err != nil {
		t.Fatalf("应答不是合法 Verdict: %v", err)
	}
	if v.Allowed || v.Reason != "test deny" {
		t.Fatalf("Verdict = %+v", v)
	}
	// 否决应答同样必须签名
	if err := Verify(testSecretBytes, resp.Header.Get(headerTimestamp), resp.Header.Get(headerSignature), respBody); err != nil {
		t.Fatalf("应答签名应可验证: %v", err)
	}
}

func TestObserveEventAckAndUnknownEvent(t *testing.T) {
	p := newTestPlugin()
	srv := httptest.NewServer(p)
	defer srv.Close()

	// 已订阅的观察档事件: 200 ack
	resp, err := srv.Client().Do(signedRequest(t, http.MethodPost, srv.URL+"/v1/events/game.over", []byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("game.over ack 状态码 = %d", resp.StatusCode)
	}

	// 未订阅的事件: 404 (明确拒绝, 便于排查配置漂移)
	resp2, err := srv.Client().Do(signedRequest(t, http.MethodPost, srv.URL+"/v1/events/player.chat", []byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("未订阅事件应 404, got %d", resp2.StatusCode)
	}
}
