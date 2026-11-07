package plugin

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CoreMatch/Avalon/internal/data/global"
	"github.com/CoreMatch/Avalon/internal/game/event"
	"github.com/CoreMatch/Avalon/internal/util/jsonutil"
)

// loadTestHub 按给定条目装配 hub (经真实 Load 路径: 配置落盘 -> 校验 -> 协商)
func loadTestHub(t *testing.T, cfgJSON string) *Hub {
	t.Helper()
	writePluginsJSON(t, cfgJSON)
	hub, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(hub.Close)
	return hub
}

// withServerIdentity 临时设定载荷里的服务器身份字段
func withServerIdentity(t *testing.T, serverID, serverName string) {
	t.Helper()
	oldID, oldName := global.ConfigServer.ServerID, global.Config.ServerName
	global.ConfigServer.ServerID = serverID
	global.Config.ServerName = serverName
	t.Cleanup(func() {
		global.ConfigServer.ServerID = oldID
		global.Config.ServerName = oldName
	})
}

var eventIDPattern = regexp.MustCompile(`^evt_[0-9A-HJKMNP-TV-Z]{26}$`)

// --- 零回归与配置校验 ---

// TestLoadWritesDefaultConfigAndIsInert 无 Plugins.json: 写出空默认配置,
// hub 空转 —— 准入立即放行且无任何网络调用 (零回归基线)
func TestLoadWritesDefaultConfigAndIsInert(t *testing.T) {
	isolateBase(t)

	hub, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer hub.Close()

	if !PluginsFile().Exists() {
		t.Fatal("Load 应写出默认 Plugins.json")
	}
	if got := len(hub.Status()); got != 0 {
		t.Fatalf("默认配置不应有插件, got %d", got)
	}

	d := hub.AdmitRoomCreate(RoomCreateRequest{Kind: KindServer, Code: "X"})
	if !d.Allowed {
		t.Fatal("无插件时准入必须立即放行")
	}
	hub.EmitGameOver("X", nil) // 必须是无操作且不 panic
}

// TestLoadRejectsInvalidConfig 显式配置错误必须让启动失败, 不静默降级
func TestLoadRejectsInvalidConfig(t *testing.T) {
	hexSecret, _ := testSecret(t)
	valid := entryFor("p1", &fakePlugin{baseURL: "http://127.0.0.1:1"}, hexSecret)

	cases := []struct {
		name string
		json string
	}{
		{"bad json", `{not json`},
		{"bad version", `{"version": 2}`},
		{"empty name", configWith(Entry{Name: "", BaseURL: "http://127.0.0.1:1", Secrets: []string{hexSecret}})},
		{"duplicate name", configWith(valid, valid)},
		{"bad url", configWith(Entry{Name: "p", BaseURL: "ftp://x", Secrets: []string{hexSecret}})},
		{"no secrets", configWith(Entry{Name: "p", BaseURL: "http://127.0.0.1:1"})},
		{"short secret", configWith(Entry{Name: "p", BaseURL: "http://127.0.0.1:1", Secrets: []string{"abcd"}})},
		{"odd hex secret", configWith(Entry{Name: "p", BaseURL: "http://127.0.0.1:1", Secrets: []string{strings.Repeat("a", 33)}})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateBase(t)
			writePluginsJSON(t, tc.json)
			if _, err := Load(); err == nil {
				t.Fatalf("非法配置应使 Load 失败: %s", tc.json)
			}
		})
	}
}

// --- room.create 准入 ---

// TestAdmissionDenyWithReasonAndPayload 否决语义 + 载荷字段清单
func TestAdmissionDenyWithReasonAndPayload(t *testing.T) {
	isolateBase(t)
	withServerIdentity(t, "SID42", "TestServer")

	fake := newFakePlugin(t)
	var captured []byte
	fake.setHandler(EventRoomCreate, func(body []byte) (int, []byte) {
		captured = body
		return http.StatusOK, []byte(`{"allowed":false,"reason":"维护中 22:00 后开放"}`)
	})
	hub := loadTestHub(t, configWith(entryFor("gate", fake, hex.EncodeToString(fake.secret))))

	d := hub.AdmitRoomCreate(RoomCreateRequest{
		Kind: KindRelay, Code: "R123", MaxPlayers: 8, IsMod: true,
		CreatorName: "alice", CreatorAddress: "203.0.113.7",
	})
	if d.Allowed {
		t.Fatal("有效否决必须拒绝建房")
	}
	if d.Plugin != "gate" {
		t.Fatalf("否决插件名 = %q", d.Plugin)
	}
	if d.Reason != "gate: 维护中 22:00 后开放" {
		t.Fatalf("理由 = %q", d.Reason)
	}

	// 载荷字段清单 (契约 RoomCreateEvent)
	var payload struct {
		EventID      string `json:"eventId"`
		Event        string `json:"event"`
		EventVersion int    `json:"eventVersion"`
		OccurredAt   string `json:"occurredAt"`
		Server       struct {
			ServerID    string `json:"serverId"`
			ServerName  string `json:"serverName"`
			CoreVersion string `json:"coreVersion"`
		} `json:"server"`
		Room struct {
			Kind       string `json:"kind"`
			Code       string `json:"code"`
			MaxPlayers int    `json:"maxPlayers"`
			IsMod      bool   `json:"isMod"`
		} `json:"room"`
		Creator struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		} `json:"creator"`
	}
	if err := json.Unmarshal(captured, &payload); err != nil {
		t.Fatalf("载荷不是合法 JSON: %v", err)
	}
	if !eventIDPattern.MatchString(payload.EventID) {
		t.Errorf("eventId 形状不符: %q", payload.EventID)
	}
	if payload.Event != EventRoomCreate || payload.EventVersion != 1 {
		t.Errorf("event/eventVersion = %q/%d", payload.Event, payload.EventVersion)
	}
	if payload.OccurredAt == "" {
		t.Error("occurredAt 缺失")
	}
	if payload.Server.ServerID != "SID42" || payload.Server.ServerName != "TestServer" || payload.Server.CoreVersion == "" {
		t.Errorf("server 段 = %+v", payload.Server)
	}
	if payload.Room.Kind != "relay" || payload.Room.Code != "R123" || payload.Room.MaxPlayers != 8 || !payload.Room.IsMod {
		t.Errorf("room 段 = %+v", payload.Room)
	}
	if payload.Creator.Name != "alice" || payload.Creator.Address != "203.0.113.7" {
		t.Errorf("creator 段 = %+v", payload.Creator)
	}
	// 请求签名必须被插件侧验证通过 (假插件验签失败会计数)
	if n := fake.badReqSig; n != 0 {
		t.Errorf("请求签名校验失败 %d 次", n)
	}
}

// TestAdmissionDenyWinsAcrossPlugins 多插件并行: 任一否决即否决, 理由串联
func TestAdmissionDenyWinsAcrossPlugins(t *testing.T) {
	isolateBase(t)
	allow := newFakePlugin(t)
	deny := newFakePlugin(t)
	deny.setHandler(EventRoomCreate, func(_ []byte) (int, []byte) {
		return http.StatusOK, []byte(`{"allowed":false,"reason":"no"}`)
	})
	hub := loadTestHub(t, configWith(
		entryFor("allower", allow, hex.EncodeToString(allow.secret)),
		entryFor("denier", deny, hex.EncodeToString(deny.secret)),
	))

	d := hub.AdmitRoomCreate(RoomCreateRequest{Kind: KindServer, Code: "X"})
	if d.Allowed {
		t.Fatal("任一否决必须拒绝")
	}
	if !strings.Contains(d.Reason, "denier: no") {
		t.Fatalf("理由应含否决插件名与原理由: %q", d.Reason)
	}
	if st := hub.Status(); st[1].Denied != 1 {
		t.Errorf("否决计数 = %d", st[1].Denied)
	}
}

// TestAdmissionDenyReasonSanitized 理由净化: 控制符剥离 + 200 rune 截断
func TestAdmissionDenyReasonSanitized(t *testing.T) {
	isolateBase(t)
	long := strings.Repeat("长", 250)
	fake := newFakePlugin(t)
	fake.setHandler(EventRoomCreate, func(_ []byte) (int, []byte) {
		return http.StatusOK, []byte(fmt.Sprintf(`{"allowed":false,"reason":"a\u0000b\n%s"}`, long))
	})
	hub := loadTestHub(t, configWith(entryFor("gate", fake, hex.EncodeToString(fake.secret))))

	d := hub.AdmitRoomCreate(RoomCreateRequest{Kind: KindRelay})
	if d.Allowed {
		t.Fatal("应否决")
	}
	if strings.ContainsAny(d.Reason, "\x00\n") {
		t.Error("控制符未被剥离")
	}
	if got := len([]rune(d.Reason)); got > len("gate: ")+200 {
		t.Errorf("理由未按 rune 截断: %d", got)
	}
}

// TestAdmissionTimeoutFailsOpen 超时 = fail-open 放行 + 计数
func TestAdmissionTimeoutFailsOpen(t *testing.T) {
	isolateBase(t)
	fake := newFakePlugin(t)
	release := make(chan struct{})
	fake.setHandler(EventRoomCreate, func(_ []byte) (int, []byte) {
		<-release // 挂住直到测试结束
		return http.StatusOK, []byte(`{"allowed":false,"reason":"late"}`)
	})
	t.Cleanup(func() { close(release) })

	cfg := NewConfig()
	cfg.AdmissionTimeoutMs = 80 // manifest 协商也用它, 本地回环足够快
	cfg.Entries = []Entry{entryFor("slow", fake, hex.EncodeToString(fake.secret))}
	hub := loadTestHub(t, jsonutil.ToJSON(cfg))

	start := time.Now()
	d := hub.AdmitRoomCreate(RoomCreateRequest{Kind: KindRelay})
	if !d.Allowed {
		t.Fatal("超时必须 fail-open 放行")
	}
	if time.Since(start) > 3*time.Second {
		t.Error("准入被超时拖住过久")
	}
	if st := hub.Status(); st[0].FailOpen != 1 {
		t.Errorf("failOpen 计数 = %d", st[0].FailOpen)
	}
}

// TestAdmissionMalformedFailsOpen 畸形应答一律 fail-open
func TestAdmissionMalformedFailsOpen(t *testing.T) {
	cases := []struct {
		name    string
		handler eventHandler
	}{
		{"bad json", func(_ []byte) (int, []byte) { return 200, []byte(`{oops`) }},
		{"missing allowed", func(_ []byte) (int, []byte) { return 200, []byte(`{"reason":"x"}`) }},
		{"deny without reason", func(_ []byte) (int, []byte) { return 200, []byte(`{"allowed":false}`) }},
		{"http 500", func(_ []byte) (int, []byte) { return 500, []byte(`{"allowed":false,"reason":"x"}`) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateBase(t)
			fake := newFakePlugin(t)
			fake.setHandler(EventRoomCreate, tc.handler)
			hub := loadTestHub(t, configWith(entryFor("bad", fake, hex.EncodeToString(fake.secret))))
			if d := hub.AdmitRoomCreate(RoomCreateRequest{Kind: KindRelay}); !d.Allowed {
				t.Fatal("畸形应答必须 fail-open")
			}
			if st := hub.Status(); st[0].FailOpen != 1 {
				t.Errorf("failOpen 计数 = %d", st[0].FailOpen)
			}
		})
	}

	// 应答未签名同样按畸形处理
	t.Run("unsigned response", func(t *testing.T) {
		isolateBase(t)
		fake := newFakePlugin(t)
		fake.signResp = false
		fake.setHandler(EventRoomCreate, func(_ []byte) (int, []byte) {
			return 200, []byte(`{"allowed":false,"reason":"x"}`)
		})
		hub := loadTestHub(t, configWith(entryFor("unsigned", fake, hex.EncodeToString(fake.secret))))
		if d := hub.AdmitRoomCreate(RoomCreateRequest{Kind: KindRelay}); !d.Allowed {
			t.Fatal("未签名应答必须 fail-open")
		}
	})
}

// TestAdmissionUnreachableFailsOpen 插件不可达: 启动协商失败 -> 准入跳过并放行
func TestAdmissionUnreachableFailsOpen(t *testing.T) {
	isolateBase(t)
	// 占用一个端口再释放, 得到一个保证无人监听的地址
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	hexSecret, _ := testSecret(t)
	hub := loadTestHub(t, configWith(Entry{Name: "ghost", BaseURL: "http://" + addr, Secrets: []string{hexSecret}}))

	if st := hub.Status(); st[0].Ready {
		t.Fatal("不可达插件不应 ready")
	}
	if d := hub.AdmitRoomCreate(RoomCreateRequest{Kind: KindRelay}); !d.Allowed {
		t.Fatal("不可达插件必须 fail-open")
	}
	if st := hub.Status(); st[0].FailOpen != 1 {
		t.Errorf("failOpen 计数 = %d", st[0].FailOpen)
	}
}

// --- game.over 投递 ---

// TestGameOverDeliveredAndAcked 正常投递 + 载荷字段 + WAL 清空 + 计数
func TestGameOverDeliveredAndAcked(t *testing.T) {
	isolateBase(t)
	withServerIdentity(t, "SID42", "TestServer")

	fake := newFakePlugin(t)
	hub := loadTestHub(t, configWith(entryFor("stats", fake, hex.EncodeToString(fake.secret))))

	hub.EmitGameOver("SID42", &event.GameOverData{
		GameTime:      1234,
		AllPlayerList: []string{"a", "b"},
		WinPlayerList: []string{"a"},
		MapName:       "Crossing Large (10p)",
		PlayerData:    map[string]map[string]int{"a": {"kills": 3}},
		ReplayName:    "replay-1",
	})

	waitFor(t, 5*time.Second, "game.over delivered", func() bool {
		return len(fake.requestsOf("/v1/events/game.over")) == 1
	})
	// worker 的顺序是 deliver -> pop -> delivered.Add: 假插件收到请求甚至
	// WAL 清空之后, delivered 计数仍可能差最后一步. 等计数收敛再断言,
	// 否则高负载/-race 下会读到在途窗口.
	waitFor(t, 5*time.Second, "wal cleared & delivered counted", func() bool {
		return hub.plugins[0].outbox.size() == 0 && hub.Status()[0].Delivered == 1
	})

	reqs := fake.requestsOf("/v1/events/game.over")
	var payload struct {
		EventID      string `json:"eventId"`
		Event        string `json:"event"`
		EventVersion int    `json:"eventVersion"`
		Room         struct {
			Kind string `json:"kind"`
			Code string `json:"code"`
		} `json:"room"`
		Result *struct {
			MapName       string                    `json:"mapName"`
			GameTime      int                       `json:"gameTime"`
			AllPlayerList []string                  `json:"allPlayerList"`
			WinPlayerList []string                  `json:"winPlayerList"`
			PlayerData    map[string]map[string]int `json:"playerData"`
			ReplayName    string                    `json:"replayName"`
		} `json:"result"`
	}
	if err := json.Unmarshal(reqs[0].Body, &payload); err != nil {
		t.Fatalf("载荷不是合法 JSON: %v", err)
	}
	if !eventIDPattern.MatchString(payload.EventID) || payload.Event != EventGameOver || payload.EventVersion != 1 {
		t.Errorf("信封字段不符: %+v", payload)
	}
	if payload.Room.Kind != KindServer || payload.Room.Code != "SID42" {
		t.Errorf("room 段 = %+v", payload.Room)
	}
	if payload.Result == nil || payload.Result.GameTime != 1234 ||
		payload.Result.MapName != "Crossing Large (10p)" ||
		len(payload.Result.AllPlayerList) != 2 || payload.Result.AllPlayerList[0] != "a" ||
		payload.Result.WinPlayerList[0] != "a" ||
		payload.Result.PlayerData["a"]["kills"] != 3 ||
		payload.Result.ReplayName != "replay-1" {
		t.Errorf("result 段 = %+v", payload.Result)
	}
	if st := hub.Status(); st[0].Delivered != 1 {
		t.Errorf("delivered 计数 = %d", st[0].Delivered)
	}
}

// TestGameOverNilResult 无结算数据时 result=null, 对局结束事实仍投递
func TestGameOverNilResult(t *testing.T) {
	isolateBase(t)
	fake := newFakePlugin(t)
	hub := loadTestHub(t, configWith(entryFor("stats", fake, hex.EncodeToString(fake.secret))))

	hub.EmitGameOver("R1", nil)
	waitFor(t, 5*time.Second, "game.over delivered", func() bool {
		return len(fake.requestsOf("/v1/events/game.over")) == 1
	})

	body := fake.requestsOf("/v1/events/game.over")[0].Body
	if !strings.Contains(string(body), `"result":null`) {
		t.Fatalf("result 应为 null: %s", body)
	}
}

// TestGameOverRetriesOnFailureThenDelivers 500 后按退避重试, 最终送达
func TestGameOverRetriesOnFailureThenDelivers(t *testing.T) {
	isolateBase(t)
	fake := newFakePlugin(t)
	var mu sync.Mutex
	calls := 0
	fake.setHandler(EventGameOver, func(_ []byte) (int, []byte) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n <= 2 {
			return http.StatusInternalServerError, nil
		}
		return http.StatusOK, []byte(`{}`)
	})
	hub := loadTestHub(t, configWith(entryFor("flaky", fake, hex.EncodeToString(fake.secret))))

	hub.EmitGameOver("R1", nil)
	// 两次失败 + 退避 (1s+2s) 后成功
	waitFor(t, 15*time.Second, "delivered after retries", func() bool {
		return len(fake.requestsOf("/v1/events/game.over")) >= 3
	})
	// 同 TestGameOverDeliveredAndAcked: 等计数收敛 (pop 在 delivered.Add 之前)
	waitFor(t, 5*time.Second, "wal cleared & counted", func() bool {
		st := hub.Status()
		return st[0].Delivered == 1 && st[0].Retries >= 2
	})
	st := hub.Status()
	if st[0].Delivered != 1 || st[0].Retries < 2 {
		t.Errorf("计数 = delivered %d retries %d", st[0].Delivered, st[0].Retries)
	}
}

// TestGameOverUnreachableThenRecovery 插件宕机期间事件入 outbox,
// 恢复后补投 (至少一次语义跨越插件不可用窗口)
func TestGameOverUnreachableThenRecovery(t *testing.T) {
	isolateBase(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	hexSecret, raw := testSecret(t)
	hub := loadTestHub(t, configWith(Entry{Name: "down", BaseURL: "http://" + addr, Secrets: []string{hexSecret}}))

	// 宕机期间发出事件: 入 outbox 等待
	hub.EmitGameOver("R1", &event.GameOverData{MapName: "m-down"})
	waitFor(t, 5*time.Second, "event buffered", func() bool {
		return hub.plugins[0].outbox.size() == 1
	})

	// 在同一地址恢复插件
	fake := newFakePluginOnAddr(t, addr, raw)
	// 假插件收到请求后 worker 还要 pop + 计数, 等计数收敛再断言
	waitFor(t, 30*time.Second, "delivered after recovery", func() bool {
		if len(fake.requestsOf("/v1/events/game.over")) != 1 {
			return false
		}
		st := hub.Status()
		return st[0].Delivered == 1 && st[0].Ready
	})
	if st := hub.Status(); st[0].Delivered != 1 || !st[0].Ready {
		t.Errorf("恢复后状态 = %+v", st[0])
	}
}

// TestReplayAfterRestartIsIdempotent WAL 重放: 同一 eventId 跨重启重复投递,
// 插件按 eventId 去重即幂等 (模拟 ack 之后 / WAL 移除之前崩溃的窗口)
func TestReplayAfterRestartIsIdempotent(t *testing.T) {
	isolateBase(t)
	fake := newFakePlugin(t)
	entry := entryFor("stats", fake, hex.EncodeToString(fake.secret))
	writePluginsJSON(t, configWith(entry))

	// 预置一条 WAL 记录 (等价于入队后进程崩溃, 未及投递)
	ev := buildGameOverEvent("R1", &event.GameOverData{MapName: "m-replay"}, time.Now())
	body := jsonutil.ToJSON(ev)
	rec := outboxRecord{EventID: ev.EventID, Event: EventGameOver, EnqueuedAtMs: time.Now().UnixMilli(), Body: body}
	outboxFile("stats").WriteFileBytes([]byte(jsonutil.ToJSON(rec) + "\n"))

	// 第一代: 启动即重放投递
	hub1, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "first replay delivery", func() bool {
		return len(fake.requestsOf("/v1/events/game.over")) == 1
	})
	hub1.Close()

	// 模拟 ack 之后、WAL 移除之前的崩溃: 原样写回同一条记录
	outboxFile("stats").WriteFileBytes([]byte(jsonutil.ToJSON(rec) + "\n"))

	// 第二代: 再次重放 —— eventId 不变, 插件去重窗口内即幂等
	hub2, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub2.Close)
	waitFor(t, 10*time.Second, "second replay delivery", func() bool {
		return len(fake.requestsOf("/v1/events/game.over")) == 2
	})

	reqs := fake.requestsOf("/v1/events/game.over")
	var id1, id2 struct {
		EventID string `json:"eventId"`
	}
	_ = json.Unmarshal(reqs[0].Body, &id1)
	_ = json.Unmarshal(reqs[1].Body, &id2)
	if id1.EventID == "" || id1.EventID != id2.EventID {
		t.Fatalf("重放必须复用同一 eventId: %q vs %q", id1.EventID, id2.EventID)
	}
	if id1.EventID != ev.EventID {
		t.Fatalf("eventId 与入队时不一致: %q vs %q", id1.EventID, ev.EventID)
	}
}

// TestBackpressureDropsOldestAndPreservesOrder 队列满丢最旧, 存活事件保序
func TestBackpressureDropsOldestAndPreservesOrder(t *testing.T) {
	isolateBase(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	hexSecret, raw := testSecret(t)
	cfg := NewConfig()
	cfg.OutboxCapacity = 3
	cfg.Entries = []Entry{{Name: "bp", BaseURL: "http://" + addr, Secrets: []string{hexSecret}}}
	hub := loadTestHub(t, jsonutil.ToJSON(cfg))

	// 插件不可达期间连发 5 条: 最旧 2 条被背压丢弃
	for i := 1; i <= 5; i++ {
		hub.EmitGameOver("R1", &event.GameOverData{MapName: fmt.Sprintf("m%d", i)})
	}
	waitFor(t, 5*time.Second, "outbox bounded", func() bool {
		return hub.plugins[0].outbox.size() == 3
	})
	if st := hub.Status(); st[0].Dropped != 2 {
		t.Fatalf("背压丢弃计数 = %d, 期望 2", st[0].Dropped)
	}

	// 恢复后按序收到存活的 m3 m4 m5
	fake := newFakePluginOnAddr(t, addr, raw)
	waitFor(t, 30*time.Second, "3 deliveries", func() bool {
		return len(fake.requestsOf("/v1/events/game.over")) == 3
	})
	var order []string
	for _, r := range fake.requestsOf("/v1/events/game.over") {
		var p struct {
			Result struct {
				MapName string `json:"mapName"`
			} `json:"result"`
		}
		_ = json.Unmarshal(r.Body, &p)
		order = append(order, p.Result.MapName)
	}
	if strings.Join(order, ",") != "m3,m4,m5" {
		t.Fatalf("到达顺序 = %v, 期望 [m3 m4 m5]", order)
	}
}

// --- 版本协商 ---

// TestNegotiationControlsDelivery manifest 未声明的事件不投递;
// 未就绪期间积压的事件在协商后按声明清理
func TestNegotiationControlsDelivery(t *testing.T) {
	isolateBase(t)

	// A 只支持 game.over: 准入不应打到它
	onlyOver := defaultTestManifest()
	delete(onlyOver.Events, EventRoomCreate)
	fakeA := newFakePlugin(t)
	fakeA.setManifest(onlyOver, nil)
	hubA := loadTestHub(t, configWith(entryFor("only-over", fakeA, hex.EncodeToString(fakeA.secret))))
	if d := hubA.AdmitRoomCreate(RoomCreateRequest{Kind: KindRelay}); !d.Allowed {
		t.Fatal("未订阅 room.create 的插件不应影响准入")
	}
	if n := len(fakeA.requestsOf("/v1/events/room.create")); n != 0 {
		t.Fatalf("room.create 不应投递给未订阅插件, got %d", n)
	}
	hubA.Close()

	// B 只支持 room.create: game.over 就绪即跳过入队
	onlyCreate := defaultTestManifest()
	delete(onlyCreate.Events, EventGameOver)
	fakeB := newFakePlugin(t)
	fakeB.setManifest(onlyCreate, nil)
	hubB := loadTestHub(t, configWith(entryFor("only-create", fakeB, hex.EncodeToString(fakeB.secret))))
	hubB.EmitGameOver("R1", nil)
	time.Sleep(100 * time.Millisecond) // 给入队路径一点时间
	if n := hubB.plugins[0].outbox.size(); n != 0 {
		t.Fatalf("未订阅 game.over 的就绪插件不应入队, got %d", n)
	}
	hubB.Close()

	// C 启动时 manifest 不可用 (500): 事件先积压, 协商出"不含 game.over"
	// 的 manifest 后积压被清理并计数
	fakeC := newFakePlugin(t)
	fakeC.setManifest(nil, nil) // manifest 端点 500 -> unready
	hubC := loadTestHub(t, configWith(entryFor("flaky-manifest", fakeC, hex.EncodeToString(fakeC.secret))))
	hubC.EmitGameOver("R1", nil)
	waitFor(t, 5*time.Second, "buffered while unready", func() bool {
		return hubC.plugins[0].outbox.size() == 1
	})
	fakeC.setManifest(onlyCreate, nil) // 协商恢复, 但不含 game.over
	waitFor(t, 15*time.Second, "buffered dropped after negotiation", func() bool {
		st := hubC.Status()
		return st[0].Ready && st[0].Dropped == 1 && hubC.plugins[0].outbox.size() == 0
	})
}

// --- 鉴权 ---

// TestSecretRotationOverlap 轮换重叠期: 新密钥签名请求, 旧密钥签名的应答仍被接受
func TestSecretRotationOverlap(t *testing.T) {
	isolateBase(t)
	newHex, newRaw := testSecret(t)
	_, oldRaw := testSecret(t)

	fake := newFakePlugin(t)
	fake.secret = newRaw     // 插件用新密钥验证请求
	fake.respSecret = oldRaw // 但应答仍用旧密钥签名 (轮换重叠期)
	hub := loadTestHub(t, configWith(Entry{
		Name:    "rotating",
		BaseURL: fake.baseURL,
		Secrets: []string{newHex, hex.EncodeToString(oldRaw)},
	}))

	hub.EmitGameOver("R1", nil)
	// 假插件收到请求后 worker 还要 pop + 计数, 等计数收敛再断言
	waitFor(t, 5*time.Second, "delivered with rotated keys", func() bool {
		return len(fake.requestsOf("/v1/events/game.over")) == 1 &&
			hub.Status()[0].Delivered == 1
	})
	if st := hub.Status(); st[0].Delivered != 1 {
		t.Errorf("轮换重叠期应正常投递: %+v", st[0])
	}
}

// --- 停用与并发 ---

// TestDisabledPluginSkipped enabled=false: 不协商、不投递、状态可见
func TestDisabledPluginSkipped(t *testing.T) {
	isolateBase(t)
	fake := newFakePlugin(t)
	disabled := false
	hub := loadTestHub(t, configWith(Entry{
		Name:    "off",
		BaseURL: fake.baseURL,
		Secrets: []string{hex.EncodeToString(fake.secret)},
		Enabled: &disabled,
	}))

	if n := fake.requestCount(); n != 0 {
		t.Fatalf("停用插件不应收到任何请求, got %d", n)
	}
	if d := hub.AdmitRoomCreate(RoomCreateRequest{Kind: KindRelay}); !d.Allowed {
		t.Fatal("停用插件不影响准入")
	}
	hub.EmitGameOver("R1", nil)
	time.Sleep(50 * time.Millisecond)
	if n := fake.requestCount(); n != 0 {
		t.Fatalf("停用插件不应收到事件, got %d", n)
	}
	if st := hub.Status(); st[0].Enabled {
		t.Error("状态应显示 disabled")
	}
}

// TestConcurrentAdmissionAndEmit 并发准入 + 并发发射, -race 下无数据竞态,
// 全部事件按序送达
func TestConcurrentAdmissionAndEmit(t *testing.T) {
	isolateBase(t)
	fake := newFakePlugin(t)
	hub := loadTestHub(t, configWith(entryFor("hammer", fake, hex.EncodeToString(fake.secret))))

	const n = 25
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < n; j++ {
				if d := hub.AdmitRoomCreate(RoomCreateRequest{Kind: KindRelay}); !d.Allowed {
					t.Error("健康插件应放行")
					return
				}
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for j := 0; j < n; j++ {
				hub.EmitGameOver("R1", &event.GameOverData{MapName: fmt.Sprintf("c%d-%d", seed, j)})
			}
		}(i)
	}
	wg.Wait()

	total := 4 * n
	// 假插件收齐后 worker 的 pop/计数仍可能差最后一步, 等计数收敛再断言
	var st []PluginStatus
	waitFor(t, 30*time.Second, "all events delivered & counted", func() bool {
		if len(fake.requestsOf("/v1/events/game.over")) != total {
			return false
		}
		st = hub.Status()
		return st[0].Delivered == int64(total)
	})
	if st[0].Delivered != int64(total) {
		t.Errorf("delivered = %d, 期望 %d", st[0].Delivered, total)
	}
	if fake.badReqSig != 0 {
		t.Errorf("并发下请求签名不应失败: %d", fake.badReqSig)
	}
}

// --- 辅助 ---

// newFakePluginOnAddr 在指定地址上启动假插件 (恢复场景用)
func newFakePluginOnAddr(t *testing.T, addr string, secret []byte) *fakePlugin {
	t.Helper()
	f := &fakePlugin{
		t:         t,
		secret:    secret,
		manifest:  defaultTestManifest(),
		handlers:  map[string]eventHandler{},
		signResp:  true,
		verifyReq: true,
	}
	f.handlers[EventRoomCreate] = func(_ []byte) (int, []byte) {
		return http.StatusOK, []byte(`{"allowed":true}`)
	}
	f.handlers[EventGameOver] = func(_ []byte) (int, []byte) {
		return http.StatusOK, []byte(`{}`)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listen %s: %v", addr, err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(f.handle))
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	f.baseURL = srv.URL
	return f
}
