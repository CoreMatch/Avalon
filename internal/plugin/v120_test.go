package plugin

// v1.2.0 测试: 三个延后项 (connection.new 连接级准入 / game.start 开局准入 /
// player.kicked 踢出审计) 的载荷、门控与失败路径.

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/CoreMatch/Avalon/internal/game/event"
	"github.com/CoreMatch/Avalon/internal/game/player"
	"github.com/CoreMatch/Avalon/internal/game/room"
	"github.com/CoreMatch/Avalon/internal/util/jsonutil"
)

// --- connection.new: 连接级准入 (仅否决档, 须授权) ---

// connectionNewPayload 解码连接级准入载荷 (契约 ConnectionNewEvent)
type connectionNewPayload struct {
	Event      string `json:"event"`
	Room       *any   `json:"room"`
	Player     *any   `json:"player"`
	Connection struct {
		Address   string `json:"address"`
		IpLong24  string `json:"ipLong24"`
		Transport string `json:"transport"`
	} `json:"connection"`
}

// TestConnectionNewVetoDenies 授权 + 声明 -> 否决生效, 载荷仅连接层信息
func TestConnectionNewVetoDenies(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(EventConnectionNew), nil)
	observeAckHandlers(fake)
	fake.setHandler(EventConnectionNew, func(_ []byte) (int, []byte) {
		return http.StatusOK, []byte(`{"allowed":false,"reason":"vpn exit node"}`)
	})
	entry := entryFor("gate", fake, hex.EncodeToString(fake.secret))
	entry.VetoEvents = []string{EventConnectionNew}
	hub := loadTestHub(t, configWith(entry))

	d := hub.AdmitConnectionNew("203.0.113.7", "203.0.113", "TCP-NIO")
	if d.Allowed {
		t.Fatal("授权插件否决后不得放行")
	}
	if !strings.Contains(d.Reason, "vpn exit node") || d.Plugin != "gate" {
		t.Fatalf("否决结论 = %+v", d)
	}

	reqs := fake.requestsOf("/v1/events/connection.new")
	if len(reqs) != 1 {
		t.Fatalf("否决咨询应恰好 1 次, got %d", len(reqs))
	}
	var payload connectionNewPayload
	if err := json.Unmarshal(reqs[0].Body, &payload); err != nil {
		t.Fatalf("载荷不是合法 JSON: %v", err)
	}
	if payload.Event != EventConnectionNew {
		t.Errorf("event = %q", payload.Event)
	}
	if payload.Connection.Address != "203.0.113.7" || payload.Connection.IpLong24 != "203.0.113" ||
		payload.Connection.Transport != "TCP-NIO" {
		t.Errorf("connection 段 = %+v", payload.Connection)
	}
	// 首包时刻无玩家身份与房间归属: 契约不设占位字段
	if payload.Room != nil || payload.Player != nil {
		t.Errorf("connection.new 不得携带 room/player 段: room=%v player=%v", payload.Room, payload.Player)
	}
	if st := hub.Status(); st[0].Denied != 1 {
		t.Errorf("denied 计数 = %d", st[0].Denied)
	}
}

// TestConnectionNewAllowPasses 放行 -> Allowed
func TestConnectionNewAllowPasses(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(EventConnectionNew), nil)
	observeAckHandlers(fake)
	entry := entryFor("gate", fake, hex.EncodeToString(fake.secret))
	entry.VetoEvents = []string{EventConnectionNew}
	hub := loadTestHub(t, configWith(entry))

	if d := hub.AdmitConnectionNew("198.51.100.9", "198.51.100", "TCP-NIO"); !d.Allowed {
		t.Fatalf("放行插件不应否决: %+v", d)
	}
	if n := len(fake.requestsOf("/v1/events/connection.new")); n != 1 {
		t.Fatalf("咨询应恰好 1 次, got %d", n)
	}
}

// TestConnectionNewRequiresGrantAndDeclaration 授权与声明缺一不可 -> 不咨询
func TestConnectionNewRequiresGrantAndDeclaration(t *testing.T) {
	// 情形一: manifest 声明 veto, 无运营授权 -> 不咨询 (connection.new 无观察模式)
	isolateBase(t)
	withV2Globals(t)
	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(EventConnectionNew), nil)
	observeAckHandlers(fake)
	hub := loadTestHub(t, configWith(entryFor("g1", fake, hex.EncodeToString(fake.secret))))
	if d := hub.AdmitConnectionNew("198.51.100.9", "198.51.100", "TCP-NIO"); !d.Allowed {
		t.Fatal("未授权不得否决")
	}
	// 只数事件路径: manifest 协商请求不算咨询
	if n := len(fake.requestsOf("/v1/events/connection.new")); n != 0 {
		t.Fatalf("未授权不应咨询 connection.new, got %d", n)
	}
	hub.Close()

	// 情形二: 有授权, manifest 未声明 veto -> 不咨询
	isolateBase(t)
	withV2Globals(t)
	fake2 := newFakePlugin(t)
	fake2.setManifest(v2TestManifest(), nil) // 不声明 veto
	observeAckHandlers(fake2)
	entry := entryFor("g2", fake2, hex.EncodeToString(fake2.secret))
	entry.VetoEvents = []string{EventConnectionNew}
	hub2 := loadTestHub(t, configWith(entry))
	if d := hub2.AdmitConnectionNew("198.51.100.9", "198.51.100", "TCP-NIO"); !d.Allowed {
		t.Fatal("未声明 veto 不得否决")
	}
	if n := len(fake2.requestsOf("/v1/events/connection.new")); n != 0 {
		t.Fatalf("未声明 veto 不应咨询 connection.new, got %d", n)
	}
}

// TestConnectionNewTimeoutFailsOpen 超时 -> 放行 + 计数
func TestConnectionNewTimeoutFailsOpen(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(EventConnectionNew), nil)
	observeAckHandlers(fake)
	release := make(chan struct{})
	fake.setHandler(EventConnectionNew, func(_ []byte) (int, []byte) {
		<-release
		return http.StatusOK, []byte(`{"allowed":false,"reason":"late"}`)
	})
	t.Cleanup(func() { close(release) })

	cfg := NewConfig()
	cfg.AdmissionTimeoutMs = 80
	entry := entryFor("slow", fake, hex.EncodeToString(fake.secret))
	entry.VetoEvents = []string{EventConnectionNew}
	cfg.Entries = []Entry{entry}
	hub := loadTestHub(t, jsonutil.ToJSON(cfg))

	if d := hub.AdmitConnectionNew("198.51.100.9", "198.51.100", "TCP-NIO"); !d.Allowed {
		t.Fatal("超时必须 fail-open 放行")
	}
	if st := hub.Status(); st[0].FailOpen != 1 {
		t.Errorf("failOpen 计数 = %d", st[0].FailOpen)
	}
}

// TestConnectionNewUnreachableFailsOpen 插件不可达 (协商不成) -> 放行 + 计数
func TestConnectionNewUnreachableFailsOpen(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	fake := newFakePlugin(t)
	fake.setManifest(nil, nil) // manifest 端点 500 -> 永不就绪
	entry := entryFor("down", fake, hex.EncodeToString(fake.secret))
	entry.VetoEvents = []string{EventConnectionNew}
	hub := loadTestHub(t, configWith(entry))

	if d := hub.AdmitConnectionNew("198.51.100.9", "198.51.100", "TCP-NIO"); !d.Allowed {
		t.Fatal("插件不可达必须 fail-open 放行")
	}
	if st := hub.Status(); st[0].FailOpen != 1 {
		t.Errorf("failOpen 计数 = %d", st[0].FailOpen)
	}
}

// TestConnectionNewMalformedFailsOpen 畸形应答 (无理由拒绝 / 缺 allowed /
// 应答未签名) -> 一律放行 + 计数
func TestConnectionNewMalformedFailsOpen(t *testing.T) {
	cases := []struct {
		name   string
		status int
		resp   []byte
		sign   bool
	}{
		{"deny without reason", http.StatusOK, []byte(`{"allowed":false}`), true},
		{"missing allowed", http.StatusOK, []byte(`{}`), true},
		{"unsigned response", http.StatusOK, []byte(`{"allowed":false,"reason":"x"}`), false},
		{"status 500", http.StatusInternalServerError, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateBase(t)
			withV2Globals(t)

			fake := newFakePlugin(t)
			fake.setManifest(v2TestManifest(EventConnectionNew), nil)
			observeAckHandlers(fake)
			fake.signResp = tc.sign
			fake.setHandler(EventConnectionNew, func(_ []byte) (int, []byte) {
				return tc.status, tc.resp
			})
			entry := entryFor("bad", fake, hex.EncodeToString(fake.secret))
			entry.VetoEvents = []string{EventConnectionNew}
			hub := loadTestHub(t, configWith(entry))

			if d := hub.AdmitConnectionNew("198.51.100.9", "198.51.100", "TCP-NIO"); !d.Allowed {
				t.Fatalf("%s 必须 fail-open 放行: %+v", tc.name, d)
			}
			if st := hub.Status(); st[0].FailOpen != 1 {
				t.Errorf("failOpen 计数 = %d", st[0].FailOpen)
			}
		})
	}
}

// --- game.start: 开局准入 (双模, 授权后否决) ---

// TestGameStartAdmissionVetoBlocks 授权 + 声明 -> 否决开局, 载荷为开局前快照
// (startTime=0), 且否决后不发观察通知
func TestGameStartAdmissionVetoBlocks(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(EventGameStart), nil)
	observeAckHandlers(fake)
	fake.setHandler(EventGameStart, func(_ []byte) (int, []byte) {
		return http.StatusOK, []byte(`{"allowed":false,"reason":"tournament lock"}`)
	})
	entry := entryFor("gate", fake, hex.EncodeToString(fake.secret))
	entry.VetoEvents = []string{EventGameStart}
	hub := loadTestHub(t, configWith(entry))

	d := hub.AdmitGameStart("WIRE1", MatchInfo{
		MapName: "Crossing Large (10p)", MapPlayer: "[z;p10]",
		MaxPlayers: 10, StartTime: 0,
		Players: []PlayerInfo{{Name: "alice", UUID: "aa01", Team: 1}},
	})
	if d.Allowed {
		t.Fatal("授权插件否决后不得开局")
	}
	if !strings.Contains(d.Reason, "tournament lock") {
		t.Fatalf("否决理由 = %+v", d)
	}

	reqs := fake.requestsOf("/v1/events/game.start")
	if len(reqs) != 1 {
		t.Fatalf("否决咨询应恰好 1 次 (否决后无观察通知), got %d", len(reqs))
	}
	var payload struct {
		Event string `json:"event"`
		Match struct {
			MapName    string `json:"mapName"`
			StartTime  int64  `json:"startTime"`
			MaxPlayers int    `json:"maxPlayers"`
			Players    []struct {
				Name string `json:"name"`
			} `json:"players"`
		} `json:"match"`
	}
	if err := json.Unmarshal(reqs[0].Body, &payload); err != nil {
		t.Fatalf("载荷不是合法 JSON: %v", err)
	}
	if payload.Event != EventGameStart || payload.Match.MapName != "Crossing Large (10p)" ||
		payload.Match.MaxPlayers != 10 || len(payload.Match.Players) != 1 ||
		payload.Match.Players[0].Name != "alice" {
		t.Errorf("game.start 准入载荷不符: %+v", payload)
	}
	// 开局前快照: startTime 必须为 0 (契约 v1.2.0)
	if payload.Match.StartTime != 0 {
		t.Errorf("开局前准入的 startTime 应为 0, got %d", payload.Match.StartTime)
	}
	if st := hub.Status(); st[0].Denied != 1 {
		t.Errorf("denied 计数 = %d", st[0].Denied)
	}
}

// TestGameStartAdmissionRequiresGrant 未授权 -> 不咨询, 立即放行
func TestGameStartAdmissionRequiresGrant(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(EventGameStart), nil) // 声明了, 但无运营授权
	observeAckHandlers(fake)
	hub := loadTestHub(t, configWith(entryFor("g", fake, hex.EncodeToString(fake.secret))))

	if d := hub.AdmitGameStart("WIRE1", MatchInfo{}); !d.Allowed {
		t.Fatal("未授权不得否决开局")
	}
	if n := len(fake.requestsOf("/v1/events/game.start")); n != 0 {
		t.Fatalf("未授权不应咨询, got %d", n)
	}
}

// TestGameStartAdmissionTimeoutFailsOpen 超时 -> 照常开局 + 计数
func TestGameStartAdmissionTimeoutFailsOpen(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(EventGameStart), nil)
	observeAckHandlers(fake)
	release := make(chan struct{})
	fake.setHandler(EventGameStart, func(_ []byte) (int, []byte) {
		<-release
		return http.StatusOK, []byte(`{"allowed":false,"reason":"late"}`)
	})
	t.Cleanup(func() { close(release) })

	cfg := NewConfig()
	cfg.AdmissionTimeoutMs = 80
	entry := entryFor("slow", fake, hex.EncodeToString(fake.secret))
	entry.VetoEvents = []string{EventGameStart}
	cfg.Entries = []Entry{entry}
	hub := loadTestHub(t, jsonutil.ToJSON(cfg))

	if d := hub.AdmitGameStart("WIRE1", MatchInfo{}); !d.Allowed {
		t.Fatal("超时必须 fail-open, 照常开局")
	}
	if st := hub.Status(); st[0].FailOpen != 1 {
		t.Errorf("failOpen 计数 = %d", st[0].FailOpen)
	}
}

// TestGameStartPackageEntryFastPaths 包级入口: 无 hub / 无授权插件时立即放行
// (零网络 I/O, 零回归)
func TestGameStartPackageEntryFastPaths(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	if d := AdmitGameStart(nil); !d.Allowed {
		t.Fatal("nil 房间应直接放行")
	}

	old := Default()
	SetDefault(nil)
	t.Cleanup(func() { SetDefault(old) })
	if d := AdmitGameStart(&room.ServerRoom{}); !d.Allowed {
		t.Fatal("无 hub 应直接放行")
	}
}

// --- player.kicked: 踢出审计 (observe 档) ---

// installKickAudit 装踢出审计钩子并负责恢复 (钩子是 player 包级全局)
func installKickAudit(t *testing.T) {
	t.Helper()
	old := player.OnKicked
	RegisterPlayerKickAudit()
	t.Cleanup(func() { player.OnKicked = old })
}

// TestPlayerKickedAuditEmitted 踢出 -> 审计事件投递, 载荷含理由/时长/房间码
func TestPlayerKickedAuditEmitted(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(), nil)
	observeAckHandlers(fake)
	hub := loadTestHub(t, configWith(entryFor("audit", fake, hex.EncodeToString(fake.secret))))
	r := wireRoom(t, hub)
	room.SetDefault(r)
	t.Cleanup(func() { room.SetDefault(nil) })
	installKickAudit(t)

	con := &wireConn{ip: "198.51.100.23"}
	p := newWirePlayer(con, "bob", "bb02")
	p.KickPlayer("afk too long", 60)

	waitFor(t, 5*time.Second, "player.kicked delivered", func() bool {
		return len(fake.requestsOf("/v1/events/player.kicked")) == 1
	})
	var payload struct {
		Event       string `json:"event"`
		Reason      string `json:"reason"`
		DurationSec int    `json:"durationSec"`
		Room        struct {
			Code string `json:"code"`
		} `json:"room"`
		Player struct {
			Name    string `json:"name"`
			Address string `json:"address"`
			Bot     bool   `json:"bot"`
		} `json:"player"`
	}
	body := fake.requestsOf("/v1/events/player.kicked")[0].Body
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("载荷不是合法 JSON: %v", err)
	}
	if payload.Event != EventPlayerKicked || payload.Reason != "afk too long" ||
		payload.DurationSec != 60 || payload.Room.Code != "WIRE1" ||
		payload.Player.Name != "bob" || payload.Player.Address != "198.51.100.23" ||
		payload.Player.Bot {
		t.Errorf("player.kicked 载荷不符: %s", body)
	}
	// 审计不改变踢出本身
	if len(con.kickReasons()) != 1 || con.kickReasons()[0] != "afk too long" {
		t.Errorf("踢出动作应照常: %v", con.kickReasons())
	}
}

// TestPlayerKickedAuditBotAndSanitize AI 玩家带 bot 标志; 理由净化 (去控制符、
// 截断 200 rune)
func TestPlayerKickedAuditBotAndSanitize(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(), nil)
	observeAckHandlers(fake)
	hub := loadTestHub(t, configWith(entryFor("audit", fake, hex.EncodeToString(fake.secret))))
	r := wireRoom(t, hub)
	room.SetDefault(r)
	t.Cleanup(func() { room.SetDefault(nil) })
	installKickAudit(t)

	con := &wireConn{}
	data := player.NewData()
	data.Apply(func(d *player.Data) {
		d.Name = "ai-1"
		d.ConnectHexID = "a1"
		d.Bot = true
	})
	p := player.New(con, nil, data)

	longReason := strings.Repeat("长", 250) + "\n\t"
	p.KickPlayer(longReason, 0)

	waitFor(t, 5*time.Second, "player.kicked delivered", func() bool {
		return len(fake.requestsOf("/v1/events/player.kicked")) == 1
	})
	var payload struct {
		Reason      string `json:"reason"`
		DurationSec int    `json:"durationSec"`
		Player      struct {
			Bot bool `json:"bot"`
		} `json:"player"`
	}
	if err := json.Unmarshal(fake.requestsOf("/v1/events/player.kicked")[0].Body, &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Player.Bot {
		t.Error("AI 玩家应带 bot 标志")
	}
	if payload.DurationSec != 0 {
		t.Errorf("durationSec = %d, 期望 0 (只踢不锁)", payload.DurationSec)
	}
	if strings.ContainsAny(payload.Reason, "\n\t") || len([]rune(payload.Reason)) != 200 {
		t.Errorf("理由应净化并截断 200 rune: len=%d", len([]rune(payload.Reason)))
	}
}

// TestPlayerKickedDeliveryFailureDropsButKickStands 投递失败 -> 丢弃计数,
// 踢出动作本身不受影响 (observe 审计档的明确语义: 不重试不重放)
func TestPlayerKickedDeliveryFailureDropsButKickStands(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(), nil)
	observeAckHandlers(fake)
	fake.setHandler(EventPlayerKicked, func(_ []byte) (int, []byte) {
		return http.StatusInternalServerError, nil
	})
	hub := loadTestHub(t, configWith(entryFor("audit", fake, hex.EncodeToString(fake.secret))))
	r := wireRoom(t, hub)
	room.SetDefault(r)
	t.Cleanup(func() { room.SetDefault(nil) })
	installKickAudit(t)

	con := &wireConn{}
	p := newWirePlayer(con, "carl", "cc03")
	p.KickPlayer("bye", 0)

	// 投递失败 -> 丢弃并计数 (等计数收敛, 见 hub_test 同款窗口说明)
	waitFor(t, 5*time.Second, "kicked event dropped", func() bool {
		st := hub.Status()
		return st[0].Dropped == 1 && st[0].Delivered == 0
	})
	// 踢出照常生效: 踢出包已发, 重进锁定已置
	if len(con.kickReasons()) != 1 {
		t.Fatalf("投递失败不得影响踢出动作: %v", con.kickReasons())
	}
	if p.KickTime <= 0 {
		t.Error("KickTime 应已设置")
	}
}

// TestPlayerKickedJoinVetoProducesAudit 进入准入否决触发的踢出同样产生审计事件
// (咽喉点覆盖, 正确行为而非重复计数)
func TestPlayerKickedJoinVetoProducesAudit(t *testing.T) {
	isolateBase(t)
	withV2Globals(t)

	fake := newFakePlugin(t)
	fake.setManifest(v2TestManifest(EventPlayerJoin), nil)
	observeAckHandlers(fake)
	fake.setHandler(EventPlayerJoin, func(_ []byte) (int, []byte) {
		return http.StatusOK, []byte(`{"allowed":false,"reason":"ban-evasion"}`)
	})
	entry := entryFor("gate", fake, hex.EncodeToString(fake.secret))
	entry.VetoEvents = []string{EventPlayerJoin}
	hub := loadTestHub(t, configWith(entry))
	r := wireRoom(t, hub)
	room.SetDefault(r)
	t.Cleanup(func() { room.SetDefault(nil) })
	installKickAudit(t)

	con := &wireConn{}
	r.EventManage.Fire(&event.PlayerJoinEvent{Player: newWirePlayer(con, "mallory", "beef01")})

	waitFor(t, 5*time.Second, "kick audit after join veto", func() bool {
		return len(fake.requestsOf("/v1/events/player.kicked")) == 1
	})
	body := string(fake.requestsOf("/v1/events/player.kicked")[0].Body)
	if !strings.Contains(body, "ban-evasion") {
		t.Errorf("审计理由应含否决理由: %s", body)
	}
}

// TestPlayerKickedNoHubIsNoop 无 hub 时钩子为空操作, 踢出照常
func TestPlayerKickedNoHubIsNoop(t *testing.T) {
	withV2Globals(t)

	old := Default()
	SetDefault(nil)
	t.Cleanup(func() { SetDefault(old) })
	installKickAudit(t)

	con := &wireConn{}
	p := newWirePlayer(con, "dave", "dd04")
	p.KickPlayer("maintenance", 10) // 不得 panic

	if len(con.kickReasons()) != 1 || con.kickReasons()[0] != "maintenance" {
		t.Fatalf("无 hub 时踢出应照常: %v", con.kickReasons())
	}
}
