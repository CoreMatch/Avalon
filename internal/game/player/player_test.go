package player

import (
	"errors"
	"testing"

	"rwhps/internal/io/packet"
	"rwhps/internal/util/timeutil"
)

// mockConnect 记录发包行为的假连接
type mockConnect struct {
	systemMsgs []string
	chatMsgs   []string
	kicks      []string
	relayMsgs  []string
	jumps      []string
	packets    []*packet.Packet
}

func (m *mockConnect) SendSystemMessage(text string) error {
	m.systemMsgs = append(m.systemMsgs, text)
	return nil
}

func (m *mockConnect) SendChatMessage(msg, sendBy string, team int) error {
	m.chatMsgs = append(m.chatMsgs, sendBy+":"+msg)
	return nil
}

func (m *mockConnect) SendKick(reason string) error {
	m.kicks = append(m.kicks, reason)
	return nil
}

func (m *mockConnect) SendRelayServerType(msg string, _ func(string)) {
	m.relayMsgs = append(m.relayMsgs, msg)
}

func (m *mockConnect) SendPacket(p *packet.Packet) { m.packets = append(m.packets, p) }

func (m *mockConnect) SendJumpToAnotherServer(ipport string) {
	m.jumps = append(m.jumps, ipport)
}

func (m *mockConnect) IP() string                     { return "127.0.0.1" }
func (m *mockConnect) UseConnectionAgreement() string { return "TCP-NIO" }
func (m *mockConnect) IPCountry() string              { return "LAN" }

func testProfile() *Data {
	d := NewData()
	d.Apply(func(dd *Data) {
		dd.ConnectHexID = "hex-1"
		dd.Name = "alice"
		dd.Index = 2
		dd.Team = 3
	})
	return d
}

// TestFieldDelegation position = index + 1 等委托关系
func TestFieldDelegation(t *testing.T) {
	p := New(nil, nil, testProfile())

	if p.Index() != 2 || p.Position() != 3 {
		t.Fatalf("Index/Position = %d/%d, 期望 2/3", p.Index(), p.Position())
	}
	p.SetPosition(5)
	if p.Index() != 4 {
		t.Fatalf("SetPosition(5) 后 Index = %d, 期望 4", p.Index())
	}
	if p.Name() != "alice" || p.ConnectHexID() != "hex-1" || p.Team() != 3 {
		t.Fatalf("档案字段 = %q/%q/%d", p.Name(), p.ConnectHexID(), p.Team())
	}
	if p.IsAi() {
		t.Fatal("PlayerHess 的 isAi 恒为 false")
	}
}

// TestSendWithoutConnection 未绑定连接时发送必须明确失败
//
// 对应上游 noBindError: ImplementedException.PlayerImplementedException.
func TestSendWithoutConnection(t *testing.T) {
	p := New(nil, nil, testProfile())

	if err := p.SendSystemMessage("hi"); !errors.Is(err, ErrPlayerNotBound) {
		t.Fatalf("SendSystemMessage err = %v, 期望 ErrPlayerNotBound", err)
	}
	if err := p.SendMessage(p, "hi"); !errors.Is(err, ErrPlayerNotBound) {
		t.Fatalf("SendMessage err = %v", err)
	}
	if err := p.SendPopUps("q", func(string) {}); !errors.Is(err, ErrPlayerNotBound) {
		t.Fatalf("SendPopUps err = %v", err)
	}

	// kickPlayer 上游对 nil con 静默返回
	p.KickPlayer("bye", 0)
}

// TestSendWithConnection 绑定连接后各发包路径
func TestSendWithConnection(t *testing.T) {
	c := &mockConnect{}
	p := New(c, nil, testProfile())

	if err := p.SendSystemMessage("sys"); err != nil {
		t.Fatalf("SendSystemMessage: %v", err)
	}
	from := New(nil, nil, func() *Data {
		d := NewData()
		d.Apply(func(dd *Data) { dd.Name = "bob"; dd.Team = 7 })
		return d
	}())
	if err := p.SendMessage(from, "hello"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if err := p.SendPopUps("choose", func(string) {}); err != nil {
		t.Fatalf("SendPopUps: %v", err)
	}
	p.KickPlayer("bad", 5)

	if len(c.systemMsgs) != 1 || c.systemMsgs[0] != "sys" {
		t.Fatalf("系统消息 = %v", c.systemMsgs)
	}
	if len(c.chatMsgs) != 1 || c.chatMsgs[0] != "bob:hello" {
		t.Fatalf("聊天 = %v", c.chatMsgs)
	}
	if len(c.relayMsgs) != 1 || c.relayMsgs[0] != "choose" {
		t.Fatalf("弹窗 = %v", c.relayMsgs)
	}
	if len(c.kicks) != 1 || c.kicks[0] != "bad" {
		t.Fatalf("kick = %v", c.kicks)
	}
	// kickPlayer(time) 写入未来的到期时间
	if p.KickTime <= timeutil.ConcurrentMillis() {
		t.Fatalf("KickTime = %d, 期望未来时间", p.KickTime)
	}
}

// TestCustomData addData / getData / removeData
func TestCustomData(t *testing.T) {
	p := New(nil, nil, testProfile())

	if _, ok := p.GetData("missing"); ok {
		t.Fatal("不存在的键不应命中")
	}
	if got := p.GetDataDefault("missing", 42); got != 42 {
		t.Fatalf("默认值 = %v", got)
	}

	p.AddData("Ping", 99)
	v, ok := p.GetData("Ping")
	if !ok || v != 99 {
		t.Fatalf("GetData(Ping) = %v, %v", v, ok)
	}

	p.RemoveData("Ping")
	if _, ok := p.GetData("Ping"); ok {
		t.Fatal("removeData 后不应命中")
	}
}

// TestEquals 以 connectHexID 判定
func TestEquals(t *testing.T) {
	a := New(nil, nil, testProfile())
	b := New(nil, nil, testProfile()) // 同 hex-1
	c := New(nil, nil, func() *Data {
		d := NewData()
		d.Apply(func(dd *Data) { dd.ConnectHexID = "hex-2" })
		return d
	}())

	if !a.Equals(b) {
		t.Fatal("同 connectHexID 应相等")
	}
	if a.Equals(c) {
		t.Fatal("不同 connectHexID 不应相等")
	}
	if !a.EqualsHexID("hex-1") {
		t.Fatal("EqualsHexID 应命中")
	}
	if a.Equals(nil) {
		t.Fatal("与 nil 不应相等")
	}
}

// TestPlayerJumpsToAnotherServer 非法输入路径
//
// 注意: IsDomainName 会真实触发 DNS 解析 (与上游 InetAddress.getByName 一致),
// 因此非法用例用空串走快速失败, 避免测试依赖网络.
func TestPlayerJumpsToAnotherServer(t *testing.T) {
	p := New(nil, nil, testProfile())

	// 空域名直接判非法, 不走 DNS
	if err := p.PlayerJumpsToAnotherServer("", 5123); !errors.Is(err, ErrNetDomain) {
		t.Fatalf("err = %v, 期望 ErrNetDomain", err)
	}

	// 合法地址但没有连接
	if err := p.PlayerJumpsToAnotherServer("127.0.0.1", 5123); !errors.Is(err, ErrNetConnectClosed) {
		t.Fatalf("err = %v, 期望 ErrNetConnectClosed", err)
	}

	// 有连接: 发包委托给连接层
	c := &mockConnect{}
	p.SetCon(c)
	if err := p.PlayerJumpsToAnotherServer("127.0.0.1", 6666); err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(c.jumps) != 1 || c.jumps[0] != "127.0.0.1:6666" {
		t.Fatalf("跳转目标 = %v", c.jumps)
	}
}

// TestClear 解绑连接
func TestClear(t *testing.T) {
	c := &mockConnect{}
	p := New(c, nil, testProfile())
	p.Clear()
	if p.Con() != nil {
		t.Fatal("Clear 后连接应为 nil")
	}
}

// TestAiPlayer isAi 与踢出自移除
func TestAiPlayer(t *testing.T) {
	c := &mockConnect{}
	removed := false
	ai := NewAiPlayer(c, nil, testProfile(), func() { removed = true })

	if !ai.IsAi() {
		t.Fatal("AiPlayer.IsAi 应为 true")
	}

	ai.KickPlayer("ai out", 0)
	if !removed {
		t.Fatal("kick 应触发自移除钩子")
	}
	if len(c.kicks) != 1 {
		t.Fatalf("kick 包 = %v", c.kicks)
	}
}

// TestStatusData 六个战绩字段的键值表
func TestStatusData(t *testing.T) {
	d := testProfile()
	d.Apply(func(dd *Data) { dd.UnitsKilled = 3; dd.BuildingsLost = 5 })
	p := New(nil, nil, d)

	st := p.StatusData()
	if st["unitsKilled"] != 3 || st["buildingsLost"] != 5 || st["unitsLost"] != 0 {
		t.Fatalf("StatusData = %v", st)
	}
	if len(st) != 6 {
		t.Fatalf("StatusData 键数 = %d, 期望 6", len(st))
	}
}
