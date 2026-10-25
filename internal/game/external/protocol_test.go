package external

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRequestRoundTrip(t *testing.T) {
	req := &Request{
		V:         ProtocolVersion,
		ID:        7,
		Op:        OpStart,
		Room:      "room-1",
		Map:       "Crossing Large (10p)",
		MapPlayer: "[z;p10]",
		MaxPlayer: 10,
	}

	line, err := encodeRequest(req)
	if err != nil {
		t.Fatalf("encodeRequest: %v", err)
	}

	var back Request
	if err := json.Unmarshal([]byte(line), &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back != *req {
		t.Fatalf("round trip = %+v, 期望 %+v", back, *req)
	}
}

// TestEncodeRequestNoHTMLEscaping 地图名里可能出现 < > &, 不能被 json 转义成 \u003c
//
// 用 json.Encoder 会默认开启 SetEscapeHTML, 让 helper 收到与 Go 侧不同的字符串.
func TestEncodeRequestNoHTMLEscaping(t *testing.T) {
	line, err := encodeRequest(&Request{V: 1, ID: 1, Op: OpStart, Map: "a<b>&c"})
	if err != nil {
		t.Fatalf("encodeRequest: %v", err)
	}
	if strings.Contains(line, `\u003c`) || strings.Contains(line, `\u0026`) {
		t.Fatalf("HTML 字符被转义了: %s", line)
	}
	if !strings.Contains(line, "a<b>&c") {
		t.Fatalf("地图名未原样传递: %s", line)
	}
}

func TestDecodeResponse(t *testing.T) {
	resp, err := decodeResponse(`{"v":1,"id":3,"ok":true,"running":true,"players":4}`)
	if err != nil {
		t.Fatalf("decodeResponse: %v", err)
	}
	if resp.ID != 3 || !resp.OK || !resp.Running || resp.Players != 4 {
		t.Fatalf("解析结果 = %+v", resp)
	}
}

func TestDecodeResponseErrors(t *testing.T) {
	cases := map[string]string{
		"":                          "empty line",
		`{"v":1,"id":1,"ok":`:       "decode response",
		`{"v":99,"id":1,"ok":true}`: "helper speaks protocol 99",
	}
	for line, want := range cases {
		_, err := decodeResponse(line)
		if err == nil {
			t.Fatalf("decodeResponse(%q) 应报错", line)
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("decodeResponse(%q) 错误 = %v, 期望包含 %q", line, err, want)
		}
	}
}

func TestErrorf(t *testing.T) {
	if err := errorf(&Response{OK: true}); err != nil {
		t.Errorf("OK 响应应返回 nil, 得到 %v", err)
	}
	if err := errorf(&Response{OK: false}); err == nil || !strings.Contains(err.Error(), "without a reason") {
		t.Errorf("无原因的失败 = %v", err)
	}
	err := errorf(&Response{OK: false, Error: "map file missing"})
	if err == nil || !strings.Contains(err.Error(), "map file missing") {
		t.Errorf("失败原因未原样透传: %v", err)
	}
}
