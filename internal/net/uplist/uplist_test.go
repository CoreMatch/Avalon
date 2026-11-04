package uplist

import (
	"strings"
	"sync"
	"testing"
	"time"

	"rwhps/internal/core/thread"
	"rwhps/internal/data/global"
	"rwhps/internal/game"
	"rwhps/internal/game/event"
	"rwhps/internal/game/manage"
	"rwhps/internal/game/player"
	"rwhps/internal/game/room"
	"rwhps/internal/io/packet"
	"rwhps/internal/net"
	"rwhps/internal/net/core"
	netpacket "rwhps/internal/net/core/server/netpacket"
	"rwhps/internal/util/file/load"
	cmd "rwhps/internal/util/game/command"
)

// --- 测试桩 ---

type postRecord struct{ url, param string }

// fakeHTTP 对应测试用的 HttpRequestManage 替身
type fakeHTTP struct {
	mu      sync.Mutex
	respond func(url, param string) string
	posts   []postRecord
}

func (f *fakeHTTP) DoPost(url, param string) string {
	f.mu.Lock()
	f.posts = append(f.posts, postRecord{url, param})
	fn := f.respond
	f.mu.Unlock()
	if fn == nil {
		return ""
	}
	return fn(url, param)
}

func (f *fakeHTTP) recorded() []postRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]postRecord, len(f.posts))
	copy(out, f.posts)
	return out
}

// testBundle 构造带 uplist 相关键的语言包 (不含 uplist.help, 由 addLang 注入)
func testBundle() *load.I18NBundle {
	return load.NewI18NBundleFromBytes([]byte(
		"err.yesList=OK-LIST\nerr.ynList=HALF-LIST\nerr.noList=NO-LIST\n" +
			"err.yesOpen=OK-OPEN\nerr.noOpen=NO-OPEN\nerr.noAdmin=NO-ADMIN\n"))
}

// setup 保存/恢复全局状态, 返回注入假 HTTP 的 UpList 实例
func setup(t *testing.T) (*UpList, *fakeHTTP, *fakeHTTP) {
	t.Helper()

	oldCore, oldCfg, oldCfgS := global.Core, global.Config, global.ConfigServer
	oldURL, oldBundle, oldBundleMap := global.URLData, global.I18NBundle, global.I18NBundleMap

	global.Config.Port = 5123
	global.Config.ServerName = "RW-HPS Test Server Long Name"
	global.Config.Subtitle = ""
	global.Config.AutoUpList = false
	// 测试基线 = 默认配置: 自报开启 (DECISIONS D-22; 零值为 false, 显式置真)
	global.Config.UplistEnable = true
	global.Config.CascadeEnable = false
	global.ConfigServer.Passwd = ""
	global.ConfigServer.MaxPlayer = 10

	global.I18NBundleMap = map[string]*load.I18NBundle{
		"CN": testBundle(), "EN": testBundle(), "HK": testBundle(), "RU": testBundle(),
	}
	global.I18NBundle = global.I18NBundleMap["EN"]

	u := NewUpList()
	coreHTTP := &fakeHTTP{}
	rwHTTP := &fakeHTTP{}
	u.httpClient = coreHTTP
	u.rwHttpClient = rwHTTP

	t.Cleanup(func() {
		thread.CloseTimeTask(thread.CustomUpServerListTask, nil)
		room.SetDefault(nil)
		resetDefault()
		manage.Clear()
		net.NetStaticData.ResetRwHps()
		global.Core, global.Config, global.ConfigServer = oldCore, oldCfg, oldCfgS
		global.URLData, global.I18NBundle, global.I18NBundleMap = oldURL, oldBundle, oldBundleMap
	})
	return u, coreHTTP, rwHTTP
}

// waitFor 在超时前轮询条件
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待 %s 超时", what)
}

// upListJSON 构造 initUpListData 的成功响应 (base64+JSON, 对应上游 API 格式):
// id=sid-1, add/open/update/remove 为各自的模板字符串
func upListJSON() string {
	return `{"id":"c2lkLTE=","add":"YWRkLXRlbXBsYXRl","open":"b3Blbi10ZW1wbGF0ZQ==","update":"dXBkYXRlLXRlbXBsYXRl","remove":"cmVtb3ZlLXRlbXBsYXRl"}`
}

// --- 命令注册 ---

// TestRegisterCommands 对应上游 registerCoreCommands:
// 移除 upserverlist/upserverlistnew 并注册 uplist
func TestRegisterCommands(t *testing.T) {
	_, _, _ = setup(t)

	h := cmd.NewHandler("")
	h.Register("upserverlist", "old", func([]string, any) {})
	h.Register("upserverlistnew", "old", func([]string, any) {})

	RegisterCommands(h)

	if resp := h.HandleMessageWith("upserverlist", nil); resp.Type != cmd.UnknownCommand {
		t.Fatalf("upserverlist 应被移除, got %v", resp.Type)
	}
	if resp := h.HandleMessageWith("uplist help", nil); resp.Type != cmd.Valid {
		t.Fatalf("uplist 未注册, got %v", resp.Type)
	}
}

// TestAddLang 对应上游 AddLang: 四种语言包都注入 uplist.help
func TestAddLang(t *testing.T) {
	_, _, _ = setup(t)

	RegisterCommands(cmd.NewHandler(""))

	for _, lang := range []string{"CN", "EN", "HK", "RU"} {
		if got := global.I18NBundleMap[lang].GetInput("uplist.help"); !strings.Contains(got, "[uplist add]") {
			t.Errorf("%s 语言包缺少 uplist.help: %q", lang, got)
		}
	}
}

// TestUpListCommandDispatch 对应上游 uplist 命令的子命令分发
func TestUpListCommandDispatch(t *testing.T) {
	_, _, _ = setup(t)

	h := cmd.NewHandler("")
	RegisterCommands(h)

	run := func(line string) string {
		var out []string
		h.HandleMessageWith(line, func(s string) { out = append(out, s) })
		return strings.Join(out, "|")
	}

	// help 走 i18n (addLang 注入的英文帮助文本)
	if got := run("uplist help"); !strings.Contains(got, "[uplist add]") {
		t.Fatalf("uplist help = %q", got)
	}
	// 无参与未知子命令给出同款提示 (照搬上游文案)
	for _, line := range []string{"uplist", "uplist foo"} {
		if got := run(line); got != "Check UpList Command ! use 'uplist help'" {
			t.Fatalf("%s = %q", line, got)
		}
	}
	// add/update/remove 在没有服务端协议时被 checkServerStartNet 拦下, 无输出也无 HTTP
	u, coreHTTP, rwHTTP := setupNoopInstance(t)
	_ = u
	if got := run("uplist add"); got != "" {
		t.Fatalf("未启动服务端协议时 uplist add 不应有输出: %q", got)
	}
	if n := len(coreHTTP.recorded()) + len(rwHTTP.recorded()); n != 0 {
		t.Fatalf("未启动服务端协议时不应发起 HTTP, got %d", n)
	}
}

// setupNoopInstance 把 Default 指到一个无响应的假实例 (拦截 add/update/remove)
func setupNoopInstance(t *testing.T) (*UpList, *fakeHTTP, *fakeHTTP) {
	u := NewUpList()
	coreHTTP := &fakeHTTP{}
	rwHTTP := &fakeHTTP{}
	u.httpClient = coreHTTP
	u.rwHttpClient = rwHTTP
	defaultMu.Lock()
	defaultInst = u
	defaultMu.Unlock()
	return u, coreHTTP, rwHTTP
}

// --- initUpListData ---

// TestInitUpListDataSuccess 正常返回时解码五个字段
func TestInitUpListDataSuccess(t *testing.T) {
	u, coreHTTP, _ := setup(t)
	global.URLData = load.NewIniFromBytes([]byte("Get.Api.UpListData.Bak = http://bak.example/uplist"))

	coreHTTP.respond = func(url, param string) string {
		if param != versionParam {
			t.Errorf("POST 参数 = %q, 期望 %q", param, versionParam)
		}
		return upListJSON()
	}

	if !u.initUpListData("") {
		t.Fatal("initUpListData 应成功")
	}
	if u.serverID != "sid-1" || u.addData != "add-template" || u.openData != "open-template" ||
		u.updateData != "update-template" || u.removeData != "remove-template" {
		t.Fatalf("字段解码结果: %+v", u)
	}
}

// TestInitUpListDataErrorBranches 照搬上游 [-1]/[-2]/[-4]/[-0]/[-5] 错误分支
func TestInitUpListDataErrorBranches(t *testing.T) {
	cases := []struct {
		name string
		resp string
		want bool
	}{
		{"blank", "", false},
		{"-1", "[-1] Please Check API", false},
		{"-2", "[-2] IP prohibited", false},
		{"-4 without new url", "[-4] -> Error", false},
		{"-4 malformed short", "[-4]", false},
		{"-0 info then bad json", "[-0] some info", false},
		{"-5 msg then bad json", "[-5] some error", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u, coreHTTP, _ := setup(t)
			global.URLData = load.NewIniFromBytes([]byte("Get.Api.UpListData.Bak = http://bak.example/uplist"))
			coreHTTP.respond = func(string, string) string { return c.resp }

			if got := u.initUpListData(""); got != c.want {
				t.Fatalf("initUpListData(%q) = %v, 期望 %v", c.resp, got, c.want)
			}
		})
	}
}

// TestInitUpListDataFallbackMainURL Bak 地址失败时回落主地址 (上游同款两级)
func TestInitUpListDataFallbackMainURL(t *testing.T) {
	u, coreHTTP, _ := setup(t)
	global.URLData = load.NewIniFromBytes([]byte(
		"Get.Api.UpListData = http://main.example/uplist\nGet.Api.UpListData.Bak = http://bak.example/uplist"))

	coreHTTP.respond = func(url, _ string) string {
		if url == "http://main.example/uplist" {
			return upListJSON()
		}
		return ""
	}

	if !u.initUpListData("") {
		t.Fatal("主地址应成功")
	}
	posts := coreHTTP.recorded()
	if len(posts) != 2 || posts[0].url != "http://bak.example/uplist" || posts[1].url != "http://main.example/uplist" {
		t.Fatalf("请求顺序异常: %+v", posts)
	}
}

// TestInitUpListDataVersionRedirect [-4] 携带新 URL 时递归重试 (上游 initUpListData(newUrl))
func TestInitUpListDataVersionRedirect(t *testing.T) {
	u, coreHTTP, _ := setup(t)
	global.URLData = load.NewIniFromBytes([]byte("Get.Api.UpListData.Bak = http://bak.example/uplist"))

	coreHTTP.respond = func(url, _ string) string {
		if url == "http://bak.example/uplist" {
			return "[-4] -> http://new.example/uplist"
		}
		if url == "http://new.example/uplist" {
			return upListJSON()
		}
		return ""
	}

	if !u.initUpListData("") {
		t.Fatal("重定向后的新地址应成功")
	}
	if u.serverID != "sid-1" {
		t.Fatalf("serverID = %q", u.serverID)
	}
}

// TestInitUpListDataExplicitURL urlIn 非空时只用该地址, 失败也不回落
func TestInitUpListDataExplicitURL(t *testing.T) {
	u, coreHTTP, _ := setup(t)
	global.URLData = load.NewIniFromBytes([]byte("Get.Api.UpListData.Bak = http://bak.example/uplist"))

	coreHTTP.respond = func(string, string) string { return "" }
	if u.initUpListData("http://custom.example/uplist") {
		t.Fatal("空响应应失败")
	}
	posts := coreHTTP.recorded()
	if len(posts) != 1 || posts[0].url != "http://custom.example/uplist" {
		t.Fatalf("指定 URL 时不应回落主地址: %+v", posts)
	}
}

// --- resolveVersion ---

// fakeTypeConnect 实现 core.TypeConnect 与 versionProvider
type fakeTypeConnect struct {
	beta    bool
	game    string
	gameInt int
}

func (f *fakeTypeConnect) GetTypeConnect(*core.ConnectionAgreement) core.TypeConnect { return f }
func (f *fakeTypeConnect) SetData(any)                                                {}
func (f *fakeTypeConnect) ProcessConnect(*packet.Packet) error                        { return nil }
func (f *fakeTypeConnect) AbstractNetConnect() *core.AbstractNetConnect               { return nil }
func (f *fakeTypeConnect) Version() string                                            { return "test" }
func (f *fakeTypeConnect) SupportedVersionBeta() bool                                 { return f.beta }
func (f *fakeTypeConnect) SupportedVersionGame() string                               { return f.game }
func (f *fakeTypeConnect) SupportedVersionInt() int                                   { return f.gameInt }

// fakeRwHps 实现 core.IRwHps
type fakeRwHps struct {
	nt core.NetType
	tc core.TypeConnect
}

func (f *fakeRwHps) NetType() core.NetType                       { return f.nt }
func (f *fakeRwHps) TypeConnect() core.TypeConnect               { return f.tc }
func (f *fakeRwHps) AbstractNetPacket() netpacket.AbstractNetPacket { return nil }

// registerFakeRwHps 把一个假协议实现注册成 firstServer
func registerFakeRwHps(t *testing.T, nt core.NetType, tc core.TypeConnect) {
	t.Helper()
	manage.SetProvider(func(core.NetType) core.IRwHps { return &fakeRwHps{nt: nt, tc: tc} })
	manage.AddIRwHps(nt, "uplist-test")
	net.NetStaticData.ResetRwHps()
}

// TestResolveVersion Server 协议族取协议实现体的版本, 其余用 1.15-Other 默认值
func TestResolveVersion(t *testing.T) {
	u, _, _ := setup(t)

	registerFakeRwHps(t, core.ServerProtocol, &fakeTypeConnect{beta: true, game: "1.16", gameInt: 180})
	u.resolveVersion()
	if !u.versionBeta || u.versionGame != "1.16" || u.versionGameInt != 180 {
		t.Fatalf("Server 协议族应取协议实现版本: %v %v %v", u.versionBeta, u.versionGame, u.versionGameInt)
	}

	registerFakeRwHps(t, core.RelayProtocol, nil)
	u.resolveVersion()
	if u.versionBeta || u.versionGame != "1.15-Other" || u.versionGameInt != 176 {
		t.Fatalf("非 Server 协议族应用默认值: %v %v %v", u.versionBeta, u.versionGame, u.versionGameInt)
	}
}

// --- uplist / update / remove ---

// newRoom 建一间带默认地图的房间并设为单例
func newRoom(t *testing.T) *room.ServerRoom {
	t.Helper()
	r := room.NewServerRoom(game.NewGameSession("uplist-room", nil))
	room.SetDefault(r)
	return r
}

// TestUplistFlow 对应上游 uplist(print): 占位符替换 + 两列表服务器 + 定时任务
func TestUplistFlow(t *testing.T) {
	u, _, rwHTTP := setup(t)
	newRoom(t)

	u.mu.Lock()
	u.serverID = "sid-1"
	u.addData = "name={RW-HPS.S.NAME}&ver={RW-HPS.RW.VERSION}&verInt={RW-HPS.RW.VERSION.INT}" +
		"&beta={RW-HPS.RW.IS.VERSION}&passwd={RW-HPS.RW.IS.PASSWD}&ip={RW-HPS.S.PRIVATE.IP}" +
		"&port={RW-HPS.S.PORT}&map={RW-HPS.RW.MAP.NAME}&size={RW-HPS.PLAYER.SIZE}&max={RW-HPS.PLAYER.SIZE.MAX}"
	u.openData = "check&port={RW-HPS.S.PORT}"
	u.versionBeta = false
	u.versionGame = "1.15"
	u.versionGameInt = 176
	u.port = "5123"
	u.mu.Unlock()

	// 两个列表服务器都返回 serverID, 端口检查都返回 true
	rwHTTP.respond = func(_, param string) string {
		if strings.Contains(param, "check") {
			return "true"
		}
		return "ok sid-1"
	}

	var printed []string
	u.uplist(func(s string) { printed = append(printed, s) })

	if strings.Join(printed, "|") != "OK-LIST|OK-OPEN" {
		t.Fatalf("输出 = %v", printed)
	}

	posts := rwHTTP.recorded()
	if len(posts) != 4 {
		t.Fatalf("应有 4 次 POST (add x2 + open x2), got %d", len(posts))
	}
	if posts[0].url != gs1URL || posts[1].url != gs4URL {
		t.Fatalf("add 请求顺序异常: %+v", posts[:2])
	}
	addParam := posts[0].param
	for _, want := range []string{
		"name=RW-HPS Test Ser", // cutting(serverName, 15)
		"ver=1.15", "verInt=176", "beta=false", "passwd=false",
		"port=5123", "map=Crossing Large (10p)", "size=0", "max=10",
	} {
		if !strings.Contains(addParam, want) {
			t.Errorf("addData 缺少 %q: %q", want, addParam)
		}
	}
	if strings.Contains(addParam, "{RW-HPS") {
		t.Errorf("addData 仍有未替换占位符: %q", addParam)
	}
	if !strings.Contains(posts[2].param, "port=5123") {
		t.Errorf("openData 未替换端口: %q", posts[2].param)
	}

	// 50s 周期的 CustomUpServerListTask 已排程
	if !thread.ContainsTimeTask(thread.CustomUpServerListTask) {
		t.Fatal("CustomUpServerListTask 未排程")
	}
}

// TestUplistPartialAndFail 部分成功 (ynList) 与全部失败 (noList/noOpen)
func TestUplistPartialAndFail(t *testing.T) {
	u, _, rwHTTP := setup(t)
	newRoom(t)

	u.mu.Lock()
	u.serverID = "sid-1"
	u.addData = "data"
	u.openData = "open"
	u.mu.Unlock()

	// 只有 gs1 返回 serverID; 端口检查不返回 true
	rwHTTP.respond = func(url, param string) string {
		if url == gs1URL && !strings.Contains(param, "open") {
			return "ok sid-1"
		}
		return "nothing"
	}

	var printed []string
	u.uplist(func(s string) { printed = append(printed, s) })
	if strings.Join(printed, "|") != "HALF-LIST|NO-OPEN" {
		t.Fatalf("输出 = %v", printed)
	}
}

// TestUpdate 对应上游 update(): 状态字段按 isRelay/isStartGame 切换
func TestUpdate(t *testing.T) {
	u, _, rwHTTP := setup(t)
	r := newRoom(t)

	u.mu.Lock()
	u.updateData = "status={RW-HPS.S.STATUS}&name={RW-HPS.S.NAME}&max={RW-HPS.PLAYER.SIZE.MAX}"
	u.port = "5123"
	u.mu.Unlock()

	u.update()
	posts := rwHTTP.recorded()
	if len(posts) != 2 {
		t.Fatalf("update 应 POST 两个列表服务器, got %d", len(posts))
	}
	if !strings.Contains(posts[0].param, "status=battleroom") {
		t.Fatalf("未开局应为 battleroom: %q", posts[0].param)
	}

	rwHTTP.posts = nil
	r.IsStartGame = true
	u.update()
	if posts := rwHTTP.recorded(); !strings.Contains(posts[0].param, "status=ingame") {
		t.Fatalf("开局后应为 ingame: %q", posts[0].param)
	}
}

// TestRemove 对应上游 remove(log)
func TestRemove(t *testing.T) {
	u, _, rwHTTP := setup(t)

	// 未上架: 不请求, 提示无需删除
	var printed []string
	u.remove(func(s string) { printed = append(printed, s) })
	if strings.Join(printed, "|") != "Not uploaded No deletion is required" {
		t.Fatalf("输出 = %v", printed)
	}
	if n := len(rwHTTP.recorded()); n != 0 {
		t.Fatalf("未上架不应发请求, got %d", n)
	}

	// 已上架: 两列表服务器各删一次, 标志复位
	u.mu.Lock()
	u.upServerList = true
	u.uplistFlag = 1
	u.removeData = "remove-payload"
	u.mu.Unlock()

	printed = nil
	u.remove(func(s string) { printed = append(printed, s) })
	if strings.Join(printed, "|") != "Deleted UPLIST" {
		t.Fatalf("输出 = %v", printed)
	}
	posts := rwHTTP.recorded()
	if len(posts) != 2 || posts[0].param != "remove-payload" || posts[1].param != "remove-payload" {
		t.Fatalf("删除请求异常: %+v", posts)
	}
	if u.isUpServerList() || u.getUplistFlag() != 0 {
		t.Fatal("删除后标志应复位")
	}
}

// TestGameOver 对应上游 ServerGameOverEvent 监听: over 时自动下架
func TestGameOver(t *testing.T) {
	u, _, rwHTTP := setup(t)

	u.mu.Lock()
	u.upServerList = true
	u.uplistFlag = 1
	u.removeData = "remove-payload"
	u.over = true
	u.mu.Unlock()

	u.gameOver()

	if u.isUpServerList() {
		t.Fatal("over 时对局结束应下架")
	}
	if u.snapshotOver() {
		t.Fatal("over 应复位")
	}
	if n := len(rwHTTP.recorded()); n != 2 {
		t.Fatalf("下架应请求两个列表服务器, got %d", n)
	}

	// over=false 时不下架
	u.mu.Lock()
	u.upServerList = true
	u.mu.Unlock()
	rwHTTP.posts = nil
	u.gameOver()
	if !u.isUpServerList() {
		t.Fatal("非 over 时不应下架")
	}
	if n := len(rwHTTP.recorded()); n != 0 {
		t.Fatalf("非 over 时不应请求, got %d", n)
	}
}

// TestRegisterRoomEvents 监听挂到房间 EventManage, 对局结束事件触发下架
func TestRegisterRoomEvents(t *testing.T) {
	u, _, rwHTTP := setup(t)
	r := newRoom(t)

	base := r.EventManage.ListenerCount(&event.ServerGameOverEvent{})
	RegisterRoomEvents(r)
	if n := r.EventManage.ListenerCount(&event.ServerGameOverEvent{}); n != base+1 {
		t.Fatalf("监听器数量 = %d, 期望 %d (默认监听 + uplist)", n, base+1)
	}

	defaultMu.Lock()
	defaultInst = u
	defaultMu.Unlock()

	u.mu.Lock()
	u.upServerList = true
	u.uplistFlag = 1
	u.removeData = "remove-payload"
	u.over = true
	u.mu.Unlock()

	r.EventManage.Fire(&event.ServerGameOverEvent{})

	waitFor(t, "对局结束自动下架", func() bool { return !u.isUpServerList() })
	if u.snapshotOver() {
		t.Fatal("over 应复位")
	}
	if n := len(rwHTTP.recorded()); n != 2 {
		t.Fatalf("下架应请求两个列表服务器, got %d", n)
	}
}

// --- 客户端命令 toup / tonp ---

// uplistConn 记录发给玩家的系统消息
type uplistConn struct {
	mu  sync.Mutex
	msg []string
}

func (c *uplistConn) SendSystemMessage(text string) error {
	c.mu.Lock()
	c.msg = append(c.msg, text)
	c.mu.Unlock()
	return nil
}
func (c *uplistConn) SendChatMessage(_, _ string, _ int) error { return nil }
func (c *uplistConn) SendKick(string) error                    { return nil }
func (c *uplistConn) SendRelayServerType(string, func(string)) {}
func (c *uplistConn) SendPacket(*packet.Packet)                {}
func (c *uplistConn) SendJumpToAnotherServer(string)           {}
func (c *uplistConn) IP() string                               { return "127.0.0.1" }
func (c *uplistConn) UseConnectionAgreement() string           { return "TCP" }
func (c *uplistConn) IPCountry() string                        { return "0" }

func (c *uplistConn) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.msg, "\n")
}

// TestClientCommandsToupTonp 对应上游 registerServerClientCommands 的 toup/tonp
func TestClientCommandsToupTonp(t *testing.T) {
	u, coreHTTP, rwHTTP := setup(t)
	r := newRoom(t)

	global.URLData = load.NewIniFromBytes([]byte("Get.Api.UpListData.Bak = http://bak.example/uplist"))
	coreHTTP.respond = func(string, string) string { return upListJSON() }
	rwHTTP.respond = func(_, param string) string {
		if strings.Contains(param, "open") {
			return "true"
		}
		return "ok sid-1"
	}

	defaultMu.Lock()
	defaultInst = u
	defaultMu.Unlock()

	h := cmd.NewHandler("/")
	RegisterClientCommands(h)

	// 非管理员被拒
	con0 := &uplistConn{}
	data0 := player.NewData()
	data0.Apply(func(d *player.Data) { d.Name = "p0"; d.ConnectHexID = "hx0" })
	p0 := r.PlayerManage.AddAbstractPlayer(con0, data0, nil)
	p0.IsAdmin = false
	h.HandleMessageWith("/toup", p0)
	// 玩家未绑定语言包时 GetInput 原样返回键名 (与 clientcommands 测试同款断言)
	if !strings.Contains(con0.text(), "err.noAdmin") {
		t.Fatalf("非管理员应收到 err.noAdmin: %q", con0.text())
	}

	// 管理员 toup: 异步完成 init + 上架, 消息推给玩家
	con1 := &uplistConn{}
	data1 := player.NewData()
	data1.Apply(func(d *player.Data) { d.Name = "p1"; d.ConnectHexID = "hx1" })
	p1 := r.PlayerManage.AddAbstractPlayer(con1, data1, nil)
	p1.IsAdmin = true

	h.HandleMessageWith("/toup", p1)
	waitFor(t, "toup 上架", func() bool { return u.getUplistFlag() == 1 })
	waitFor(t, "toup 玩家消息", func() bool {
		return strings.Contains(con1.text(), "OK-LIST") && strings.Contains(con1.text(), "OK-OPEN")
	})
	if !u.snapshotOver() {
		t.Fatal("toup 成功后 over 应为真")
	}

	// 已上架再 toup: Already on the list
	h.HandleMessageWith("/toup", p1)
	waitFor(t, "重复 toup 提示", func() bool { return strings.Contains(con1.text(), "Already on the list") })

	// tonp: 下架并复位
	h.HandleMessageWith("/tonp", p1)
	waitFor(t, "tonp 下架", func() bool { return u.getUplistFlag() == 0 })
	waitFor(t, "tonp 玩家消息", func() bool { return strings.Contains(con1.text(), "Deleted UPLIST") })

	// 未上架再 tonp: no uplist
	h.HandleMessageWith("/tonp", p1)
	waitFor(t, "重复 tonp 提示", func() bool { return strings.Contains(con1.text(), "no uplist") })
}

// --- uplistEnable 总开关 (有意修正, 作者已批准, 2026-11-04, DECISIONS D-22) ---

// TestUplistDisabledGate uplistEnable=false: 控制台命令 add/update/remove 与
// 客户端命令 toup/tonp 全部被拦, 不发起任何 HTTP; help 与未知子命令提示不受影响.
func TestUplistDisabledGate(t *testing.T) {
	u, coreHTTP, rwHTTP := setup(t)
	r := newRoom(t)
	global.Config.UplistEnable = false

	// 即使列表服可达, 被门控拦下后也不应触碰
	global.URLData = load.NewIniFromBytes([]byte("Get.Api.UpListData.Bak = http://bak.example/uplist"))
	coreHTTP.respond = func(string, string) string { return upListJSON() }
	rwHTTP.respond = func(_, param string) string {
		if strings.Contains(param, "open") {
			return "true"
		}
		return "ok sid-1"
	}

	defaultMu.Lock()
	defaultInst = u
	defaultMu.Unlock()

	h := cmd.NewHandler("")
	RegisterCommands(h)

	run := func(line string) string {
		var out []string
		h.HandleMessageWith(line, func(s string) { out = append(out, s) })
		return strings.Join(out, "|")
	}

	for _, line := range []string{"uplist add", "uplist add 5124", "uplist update", "uplist remove"} {
		if got := run(line); !strings.Contains(got, "uplistEnable=false") {
			t.Fatalf("%s 应给出关闭提示: %q", line, got)
		}
	}
	if got := run("uplist help"); !strings.Contains(got, "[uplist add]") {
		t.Fatalf("关闭自报不应影响 help: %q", got)
	}
	if n := len(coreHTTP.recorded()) + len(rwHTTP.recorded()); n != 0 {
		t.Fatalf("关闭自报时不应发起 HTTP, got %d", n)
	}
	if u.getUplistFlag() != 0 {
		t.Fatal("关闭自报时上架标志应保持 0")
	}

	// 客户端命令同样被拦 (管理员身份先行通过)
	hc := cmd.NewHandler("/")
	RegisterClientCommands(hc)
	con := &uplistConn{}
	data := player.NewData()
	data.Apply(func(d *player.Data) { d.Name = "adm"; d.ConnectHexID = "hx-adm" })
	p := r.PlayerManage.AddAbstractPlayer(con, data, nil)
	p.IsAdmin = true

	hc.HandleMessageWith("/toup", p)
	if !strings.Contains(con.text(), "uplistEnable=false") {
		t.Fatalf("toup 应收到关闭提示: %q", con.text())
	}
	hc.HandleMessageWith("/tonp", p)
	if strings.Count(con.text(), "uplistEnable=false") != 2 {
		t.Fatalf("tonp 应收到关闭提示: %q", con.text())
	}
	if u.getUplistFlag() != 0 || u.isUpServerList() {
		t.Fatal("关闭自报时客户端命令不得改变上架状态")
	}
	if n := len(coreHTTP.recorded()) + len(rwHTTP.recorded()); n != 0 {
		t.Fatalf("客户端命令被拦后不应发起 HTTP, got %d", n)
	}
}

// TestUplistEnabledKeepsSelfReport uplistEnable=true 时, cascadeEnable 任一取值
// 下自报照常工作 (开关与 Cascade 模式解耦, 默认保持自报; DECISIONS D-22).
// 走 toup 客户端命令的完整链路: init -> 上架 -> 定时任务排程.
func TestUplistEnabledKeepsSelfReport(t *testing.T) {
	for _, cascade := range []bool{false, true} {
		name := "cascadeEnable=false"
		if cascade {
			name = "cascadeEnable=true"
		}
		t.Run(name, func(t *testing.T) {
			u, coreHTTP, rwHTTP := setup(t)
			r := newRoom(t)
			global.Config.UplistEnable = true
			global.Config.CascadeEnable = cascade

			global.URLData = load.NewIniFromBytes([]byte("Get.Api.UpListData.Bak = http://bak.example/uplist"))
			coreHTTP.respond = func(string, string) string { return upListJSON() }
			rwHTTP.respond = func(_, param string) string {
				if strings.Contains(param, "open") {
					return "true"
				}
				return "ok sid-1"
			}

			defaultMu.Lock()
			defaultInst = u
			defaultMu.Unlock()

			hc := cmd.NewHandler("/")
			RegisterClientCommands(hc)
			con := &uplistConn{}
			data := player.NewData()
			data.Apply(func(d *player.Data) { d.Name = "adm"; d.ConnectHexID = "hx-adm" })
			p := r.PlayerManage.AddAbstractPlayer(con, data, nil)
			p.IsAdmin = true

			hc.HandleMessageWith("/toup", p)
			waitFor(t, "自报上架", func() bool { return u.getUplistFlag() == 1 })
			waitFor(t, "上架玩家消息", func() bool {
				return strings.Contains(con.text(), "OK-LIST") && strings.Contains(con.text(), "OK-OPEN")
			})
		})
	}
}

// TestUplistConsoleAddEnabled 控制台 "uplist add" 在开关开启且服务端协议已就绪时
// 照常上架 (AutoUpList 的自动上架同样经此命令进入, 等价覆盖).
func TestUplistConsoleAddEnabled(t *testing.T) {
	u, coreHTTP, rwHTTP := setup(t)
	newRoom(t)
	global.Config.UplistEnable = true

	registerFakeRwHps(t, core.ServerProtocol, &fakeTypeConnect{beta: false, game: "1.15", gameInt: 176})

	global.URLData = load.NewIniFromBytes([]byte("Get.Api.UpListData.Bak = http://bak.example/uplist"))
	coreHTTP.respond = func(string, string) string { return upListJSON() }
	rwHTTP.respond = func(_, param string) string {
		if strings.Contains(param, "open") {
			return "true"
		}
		return "ok sid-1"
	}

	defaultMu.Lock()
	defaultInst = u
	defaultMu.Unlock()

	h := cmd.NewHandler("")
	RegisterCommands(h)
	h.HandleMessageWith("uplist add", func(string) {})

	waitFor(t, "控制台 add 上架", func() bool { return u.getUplistFlag() == 1 })
}
