package external

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"rwhps/internal/game"
)

func TestRequestRoundTrip(t *testing.T) {
	req := &Request{
		V:         ProtocolVersion,
		ID:        7,
		Op:        OpRoomStart,
		Gen:       3,
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
	line, err := encodeRequest(&Request{
		V: ProtocolVersion, ID: 1, Op: OpRoomStart, Gen: 1,
		Room: "r1", Map: "a<b>&c",
	})
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

// TestValidateRequest 字段校验: 缺房间/连接/世代号一律拒绝
func TestValidateRequest(t *testing.T) {
	cases := []struct {
		name string
		req  *Request
		want string
	}{
		{"nil", nil, "nil request"},
		{"版本不符", &Request{V: 1, ID: 1, Op: OpHello, Gen: 1}, "protocol 1"},
		{"id 为 0", &Request{V: ProtocolVersion, ID: 0, Op: OpHello, Gen: 1}, "id must be positive"},
		{"缺少世代号", &Request{V: ProtocolVersion, ID: 1, Op: OpHello}, "without generation"},
		{"hello 带房间", &Request{V: ProtocolVersion, ID: 1, Op: OpHello, Gen: 1, Room: "r1"}, "must not carry room"},
		{"room 缺房间", &Request{V: ProtocolVersion, ID: 1, Op: OpRoomStart, Gen: 1}, "without room"},
		{"conn 缺连接", &Request{V: ProtocolVersion, ID: 1, Op: OpConnOpen, Gen: 1, Room: "r1"}, "without conn"},
		{"包缺类型", &Request{V: ProtocolVersion, ID: 1, Op: OpConnPacket, Gen: 1, Room: "r1", Conn: "c1"}, "without packet type"},
		{"未知 op", &Request{V: ProtocolVersion, ID: 1, Op: "room.teleport", Gen: 1}, "unknown op"},
	}
	for _, c := range cases {
		err := validateRequest(c.req)
		if err == nil {
			t.Errorf("%s: validateRequest 应报错", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: 错误 = %v, 期望包含 %q", c.name, err, c.want)
		}
	}
}

func TestDecodeResponse(t *testing.T) {
	resp, err := decodeResponse(`{"v":2,"type":"response","id":3,"gen":1,"ok":true,"running":true,"players":4}`)
	if err != nil {
		t.Fatalf("decodeResponse: %v", err)
	}
	if resp.ID != 3 || !resp.OK || !resp.Running || resp.Players != 4 {
		t.Fatalf("解析结果 = %+v", resp)
	}
}

func TestDecodeResponseErrors(t *testing.T) {
	cases := map[string]string{
		"":                                      "empty line",
		`{"v":2,"type":"response","id":1,"ok":`: "decode message",
		`{"v":99,"type":"response","id":1,"gen":1,"ok":true}`: "helper speaks protocol 99",
		`{"v":2,"type":"response","id":1,"gen":0,"ok":true}`:  "without generation",
		`{"v":2,"type":"response","id":0,"gen":1,"ok":true}`:  "without request id",
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

// TestDecodeUnknownMessageType 未知 type 是致命协议错误
func TestDecodeUnknownMessageType(t *testing.T) {
	_, err := decodeMessage(`{"v":2,"type":"whatever","gen":1}`)
	if err == nil || !strings.Contains(err.Error(), "unknown message type") {
		t.Fatalf("decodeMessage = %v, 期望未知类型错误", err)
	}
}

// TestDecodePacketMessage 游戏包必须带房间/连接/类型
func TestDecodePacketMessage(t *testing.T) {
	pkt, err := decodeMessage(`{"v":2,"type":"packet","gen":1,"room":"r1","conn":"c1","ptype":115,"data":"AQID"}`)
	if err != nil {
		t.Fatalf("decodeMessage: %v", err)
	}
	p, ok := pkt.(*PacketMessage)
	if !ok {
		t.Fatalf("类型 = %T, 期望 *PacketMessage", pkt)
	}
	if p.Room != "r1" || p.Conn != "c1" || p.PType != 115 {
		t.Fatalf("解析结果 = %+v", p)
	}

	for _, bad := range []string{
		`{"v":2,"type":"packet","gen":1,"conn":"c1","ptype":1}`,
		`{"v":2,"type":"packet","gen":1,"room":"r1","ptype":1}`,
		`{"v":2,"type":"packet","gen":1,"room":"r1","conn":"c1"}`,
	} {
		if _, err := decodeMessage(bad); err == nil {
			t.Errorf("decodeMessage(%q) 应报错", bad)
		}
	}
}

// TestDecodeEventMessage 玩家级事件必须能定位到连接
func TestDecodeEventMessage(t *testing.T) {
	if _, err := decodeMessage(`{"v":2,"type":"event","gen":1,"event":"player.join","room":"r1"}`); err == nil {
		t.Fatal("player.join 缺少 conn 时应报错")
	}
	ev, err := decodeMessage(`{"v":2,"type":"event","gen":1,"event":"chat","room":"r1","conn":"c1","data":{"name":"n","text":"t"}}`)
	if err != nil {
		t.Fatalf("decodeMessage: %v", err)
	}
	e := ev.(*EventMessage)
	chat, err := decodeChat(e.Data)
	if err != nil {
		t.Fatalf("decodeChat: %v", err)
	}
	if chat.Name != "n" || chat.Text != "t" {
		t.Fatalf("聊天负载 = %+v", chat)
	}
}

// TestDecodePlayerProfile 玩家档案的字段要完整
func TestDecodePlayerProfile(t *testing.T) {
	raw := json.RawMessage(`{"id":"p1","name":"Alice","team":2,"ready":true,"host":true,"bot":false,"spectator":false}`)
	p, err := decodePlayerProfile(raw)
	if err != nil {
		t.Fatalf("decodePlayerProfile: %v", err)
	}
	if p.ID != "p1" || p.Name != "Alice" || p.Team != 2 || !p.Ready || !p.Host {
		t.Fatalf("玩家档案 = %+v", p)
	}
	if _, err := decodePlayerProfile(nil); err == nil {
		t.Fatal("空负载应报错")
	}
}

// TestBase64RoundTrip Base64 要与 java.util.Base64 一致
func TestBase64RoundTrip(t *testing.T) {
	data := []byte{0x00, 0x01, 0xFE, 0xFF, 0x41}
	enc := encodeBase64(data)
	if enc != "AAH+/0E=" {
		t.Fatalf("encodeBase64 = %q, 期望 %q", enc, "AAH+/0E=")
	}
	back, err := decodeBase64(enc)
	if err != nil {
		t.Fatalf("decodeBase64: %v", err)
	}
	if string(back) != string(data) {
		t.Fatalf("往返 = %v, 期望 %v", back, data)
	}
	if out, err := decodeBase64(""); err != nil || out != nil {
		t.Fatalf("空串应解出 nil, 得到 %v / %v", out, err)
	}
	if _, err := decodeBase64("!!!"); err == nil {
		t.Fatal("非法 Base64 应报错")
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

// --- v2-ext (DECISIONS D-9, 有意修正, 作者已批准, 2026-11-04) ---

// TestValidateRequestExt 扩展操作的字段校验
func TestValidateRequestExt(t *testing.T) {
	pos0 := 0
	pos3 := 3
	neg := -1

	cases := []struct {
		name    string
		req     *Request
		wantErr string // 空表示应通过
	}{
		{"addai 带房间", &Request{V: ProtocolVersion, ID: 1, Op: OpRoomAddAI, Gen: 1, Room: "r1"}, ""},
		{"addai 缺房间", &Request{V: ProtocolVersion, ID: 1, Op: OpRoomAddAI, Gen: 1}, "without room"},
		{"sync 带房间", &Request{V: ProtocolVersion, ID: 1, Op: OpRoomSync, Gen: 1, Room: "r1"}, ""},
		{"sync 缺房间", &Request{V: ProtocolVersion, ID: 1, Op: OpRoomSync, Gen: 1}, "without room"},
		{"result 带房间", &Request{V: ProtocolVersion, ID: 1, Op: OpGameResult, Gen: 1, Room: "r1"}, ""},
		{"result 缺房间", &Request{V: ProtocolVersion, ID: 1, Op: OpGameResult, Gen: 1}, "without room"},
		{"removeplayer 完整", &Request{V: ProtocolVersion, ID: 1, Op: OpRoomRemovePlayer, Gen: 1, Room: "r1", Position: &pos3}, ""},
		{"removeplayer 位置0合法", &Request{V: ProtocolVersion, ID: 1, Op: OpRoomRemovePlayer, Gen: 1, Room: "r1", Position: &pos0}, ""},
		{"removeplayer 缺房间", &Request{V: ProtocolVersion, ID: 1, Op: OpRoomRemovePlayer, Gen: 1, Position: &pos3}, "without room"},
		{"removeplayer 缺位置", &Request{V: ProtocolVersion, ID: 1, Op: OpRoomRemovePlayer, Gen: 1, Room: "r1"}, "without valid position"},
		{"removeplayer 负位置", &Request{V: ProtocolVersion, ID: 1, Op: OpRoomRemovePlayer, Gen: 1, Room: "r1", Position: &neg}, "without valid position"},
	}
	for _, c := range cases {
		err := validateRequest(c.req)
		if c.wantErr == "" {
			if err != nil {
				t.Errorf("%s: 应通过校验, 得到 %v", c.name, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: 应报错", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s: 错误 = %v, 期望包含 %q", c.name, err, c.wantErr)
		}
	}
}

// TestEncodeRequestExtPosition position 字段编码: 0 不能被 omitempty 吞掉
func TestEncodeRequestExtPosition(t *testing.T) {
	pos0 := 0
	line, err := encodeRequest(&Request{
		V: ProtocolVersion, ID: 1, Op: OpRoomRemovePlayer, Gen: 1,
		Room: "r1", Position: &pos0,
	})
	if err != nil {
		t.Fatalf("encodeRequest: %v", err)
	}
	if !strings.Contains(line, `"position":0`) {
		t.Fatalf("position=0 应原样编码: %s", line)
	}

	// 未提供 position 时不出现该字段
	line, err = encodeRequest(&Request{
		V: ProtocolVersion, ID: 1, Op: OpRoomAddAI, Gen: 1, Room: "r1",
	})
	if err != nil {
		t.Fatalf("encodeRequest: %v", err)
	}
	if strings.Contains(line, "position") {
		t.Fatalf("未提供 position 时不应出现该字段: %s", line)
	}
}

// TestDecodeResponseGameResult game.result 应答的 result 负载解码
func TestDecodeResponseGameResult(t *testing.T) {
	line := `{"v":2,"type":"response","id":9,"gen":1,"ok":true,"result":{"gameTime":600,"allPlayerList":["a","b"],"winPlayerList":["a"],"mapName":"m1","playerData":{"a":{"unitsKilled":1,"buildingsKilled":2,"experimentalsKilled":3,"unitsLost":4,"buildingsLost":5,"experimentalsLost":6}},"replayName":"rep1"}}`
	resp, err := decodeResponse(line)
	if err != nil {
		t.Fatalf("decodeResponse: %v", err)
	}
	if resp.Result == nil {
		t.Fatal("result 应被解码")
	}
	r := resp.Result
	if r.GameTime != 600 || r.MapName != "m1" || r.ReplayName != "rep1" {
		t.Fatalf("标量字段 = %+v", r)
	}
	if len(r.AllPlayerList) != 2 || r.AllPlayerList[0] != "a" {
		t.Fatalf("allPlayerList = %v", r.AllPlayerList)
	}
	if len(r.WinPlayerList) != 1 || r.WinPlayerList[0] != "a" {
		t.Fatalf("winPlayerList = %v", r.WinPlayerList)
	}
	pd := r.PlayerData["a"]
	for k, want := range map[string]int{
		"unitsKilled": 1, "buildingsKilled": 2, "experimentalsKilled": 3,
		"unitsLost": 4, "buildingsLost": 5, "experimentalsLost": 6,
	} {
		if pd[k] != want {
			t.Fatalf("playerData[a][%s] = %d, want %d", k, pd[k], want)
		}
	}

	// result 缺失 (尚未分出胜负) 时 Result 为 nil
	resp, err = decodeResponse(`{"v":2,"type":"response","id":9,"gen":1,"ok":true}`)
	if err != nil {
		t.Fatalf("decodeResponse: %v", err)
	}
	if resp.Result != nil {
		t.Fatalf("无 result 时应为 nil, 得到 %+v", resp.Result)
	}
}

// TestErrorfUnsupported v2-only helper 对扩展操作回 "unsupported",
// 归一成 game.ErrUnsupported 供调用方降级.
func TestErrorfUnsupported(t *testing.T) {
	err := errorf(&Response{OK: false, Error: "unsupported"})
	if !errors.Is(err, game.ErrUnsupported) {
		t.Fatalf("unsupported 应归一为 game.ErrUnsupported, 得到 %v", err)
	}
	// 大小写不敏感
	if err := errorf(&Response{OK: false, Error: "Unsupported"}); !errors.Is(err, game.ErrUnsupported) {
		t.Fatalf("大小写变体也应归一, 得到 %v", err)
	}
}
