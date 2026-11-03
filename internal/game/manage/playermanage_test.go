package manage

import (
	"testing"

	"rwhps/internal/data/global"
	"rwhps/internal/game/player"
	"rwhps/internal/io/packet"
)

// pmMockConnect 管理层测试用的假连接
type pmMockConnect struct{ name string }

func (m *pmMockConnect) SendSystemMessage(string) error            { return nil }
func (m *pmMockConnect) SendChatMessage(string, string, int) error { return nil }
func (m *pmMockConnect) SendKick(string) error                     { return nil }
func (m *pmMockConnect) SendRelayServerType(string, func(string))  {}
func (m *pmMockConnect) SendPacket(*packet.Packet)                 {}
func (m *pmMockConnect) SendJumpToAnotherServer(string)            {}
func (m *pmMockConnect) IP() string                                { return "127.0.0.1" }
func (m *pmMockConnect) UseConnectionAgreement() string            { return "TCP" }
func (m *pmMockConnect) IPCountry() string                         { return "LAN" }

func profileData(hexID, name string, index, team int) *player.Data {
	d := player.NewData()
	d.Apply(func(dd *player.Data) {
		dd.ConnectHexID = hexID
		dd.Name = name
		dd.Index = index
		dd.Team = team
	})
	return d
}

// oneAdminSetup 把配置拨到上游默认 (oneAdmin = true), 结束还原
func oneAdminSetup(t *testing.T) {
	t.Helper()
	old := global.ConfigServer.OneAdmin
	global.ConfigServer.OneAdmin = true
	t.Cleanup(func() { global.ConfigServer.OneAdmin = old })
}

// TestAddNewPlayer 新玩家: 进两个列表, oneAdmin 下第一个成为自动管理员
func TestAddNewPlayer(t *testing.T) {
	oneAdminSetup(t)
	m := NewPlayerManage()

	p := m.AddAbstractPlayer(&pmMockConnect{}, profileData("h1", "alice", 0, 1), nil)

	if m.PlayerGroup.Size() != 1 || m.PlayerAll.Size() != 1 {
		t.Fatalf("列表大小 = %d/%d, 期望 1/1", m.PlayerGroup.Size(), m.PlayerAll.Size())
	}
	if !p.IsAdmin || !p.AutoAdmin {
		t.Fatal("oneAdmin 下第一个玩家应为自动管理员")
	}
}

// TestOneAdminOnlyOnce oneAdmin 只分配一次
func TestOneAdminOnlyOnce(t *testing.T) {
	oneAdminSetup(t)
	m := NewPlayerManage()

	first := m.AddAbstractPlayer(&pmMockConnect{}, profileData("h1", "alice", 0, 1), nil)
	second := m.AddAbstractPlayer(&pmMockConnect{}, profileData("h2", "bob", 1, 2), nil)

	if !first.IsAdmin || !first.AutoAdmin {
		t.Fatal("第一个玩家应为管理员")
	}
	if second.IsAdmin || second.AutoAdmin {
		t.Fatal("第二个玩家不应再自动成为管理员")
	}
}

// TestReconnectByHexID 同 connectHexID 重连: 复用实体, 换绑连接, 不重复进 playerAll
//
// 对应上游 addAbstractPlayer 的 eachAllFind 分支.
func TestReconnectByHexID(t *testing.T) {
	oneAdminSetup(t)
	m := NewPlayerManage()

	con1 := &pmMockConnect{name: "c1"}
	p1 := m.AddAbstractPlayer(con1, profileData("h1", "alice", 0, 1), nil)

	// 对局中玩家断线: 上游只移出 playerGroup, 保留 playerAll
	m.RemoveFromGroup(p1)
	if m.PlayerGroup.Size() != 0 || m.PlayerAll.Size() != 1 {
		t.Fatalf("断线后 = %d/%d, 期望 0/1", m.PlayerGroup.Size(), m.PlayerAll.Size())
	}

	// 重连: 同 hexID
	con2 := &pmMockConnect{name: "c2"}
	p2 := m.AddAbstractPlayer(con2, profileData("h1", "alice", 0, 1), nil)

	if p2 != p1 {
		t.Fatal("重连应复用同一玩家实体")
	}
	if p2.Con() != player.PlayerConnect(con2) {
		t.Fatal("重连后连接应换绑为新的")
	}
	if m.PlayerAll.Size() != 1 {
		t.Fatalf("playerAll 不应重复, 大小 = %d", m.PlayerAll.Size())
	}
	if m.PlayerGroup.Size() != 1 {
		t.Fatalf("重连后应回到 playerGroup, 大小 = %d", m.PlayerGroup.Size())
	}
}

// TestAdminRestoration 登记过的管理员重连后恢复权限
func TestAdminRestoration(t *testing.T) {
	old := global.ConfigServer.OneAdmin
	global.ConfigServer.OneAdmin = false // 关掉自动分配, 只看登记数据
	t.Cleanup(func() { global.ConfigServer.OneAdmin = old })

	oldCore := global.Core
	// 只装 Admin 字段: NewApplication 会碰 Settings.bin, 单测里不想要文件系统副作用
	global.Core = &global.Application{Admin: global.NewAdministration()}
	t.Cleanup(func() { global.Core = oldCore })

	m := NewPlayerManage()

	// 无登记: 不是管理员
	p := m.AddAbstractPlayer(&pmMockConnect{}, profileData("h9", "carol", 0, 1), nil)
	if p.IsAdmin {
		t.Fatal("未登记的玩家不应是管理员")
	}

	// 登记为管理员 (对应 giveadmin 之后的数据): 新连接进来即恢复权限
	global.Core.Admin.AddAdmin("h10", "dave", true)
	p2 := m.AddAbstractPlayer(&pmMockConnect{}, profileData("h10", "dave", 1, 2), nil)
	if !p2.IsAdmin || !p2.SuperAdmin {
		t.Fatalf("登记过的玩家应恢复权限: admin=%v super=%v", p2.IsAdmin, p2.SuperAdmin)
	}
}

// TestFindPlayer 按名字 / 位置 / 歧义 / 找不到
func TestFindPlayer(t *testing.T) {
	m := NewPlayerManage()
	m.AddAbstractPlayer(&pmMockConnect{}, profileData("h1", "alice", 0, 1), nil)
	m.AddAbstractPlayer(&pmMockConnect{}, profileData("h2", "bob", 1, 2), nil)
	m.AddAbstractPlayer(&pmMockConnect{}, profileData("h3", "alicia", 2, 1), nil)

	var lastMsg string
	out := func(s string) { lastMsg = s }

	// 完整名字
	if p := m.FindPlayer(out, "bob"); p == nil || p.Name() != "bob" {
		t.Fatalf("按名字找 bob 失败: %v (%q)", p, lastMsg)
	}
	// 位置 (上游 1 起): "1" -> index 0 -> alice
	if p := m.FindPlayer(out, "1"); p == nil || p.Name() != "alice" {
		t.Fatalf("按位置 1 找失败: %v", p)
	}
	// 歧义: "ali" 命中 alice 与 alicia
	lastMsg = ""
	if p := m.FindPlayer(out, "ali"); p != nil {
		t.Fatalf("歧义名字不应返回玩家, 得到 %v", p)
	}
	if lastMsg == "" {
		t.Fatal("歧义时应有提示")
	}
	// 找不到
	lastMsg = ""
	if p := m.FindPlayer(out, "nobody"); p != nil {
		t.Fatalf("不存在的名字不应返回玩家: %v", p)
	}
	if lastMsg == "" {
		t.Fatal("找不到时应有提示")
	}
	// 越界位置
	if p := m.FindPlayer(out, "99"); p != nil {
		t.Fatalf("越界位置不应返回玩家: %v", p)
	}
}

// TestGetPlayerAndTeamQuery 按 index 查与同队名字列表
func TestGetPlayerAndTeamQuery(t *testing.T) {
	m := NewPlayerManage()
	m.AddAbstractPlayer(&pmMockConnect{}, profileData("h1", "alice", 0, 1), nil)
	m.AddAbstractPlayer(&pmMockConnect{}, profileData("h2", "bob", 1, 2), nil)
	m.AddAbstractPlayer(&pmMockConnect{}, profileData("h3", "carol", 2, 1), nil)

	if p := m.GetPlayer(1); p == nil || p.Name() != "bob" {
		t.Fatalf("GetPlayer(1) = %v", p)
	}
	if p := m.GetPlayer(9); p != nil {
		t.Fatalf("GetPlayer(9) 应为 nil, 得到 %v", p)
	}

	names := m.GetPlayersNameOnTheSameTeam(1)
	if len(names) != 2 {
		t.Fatalf("队伍 1 的名字 = %v", names)
	}
}

// TestCleanPlayerAllData 清空两个列表
func TestCleanPlayerAllData(t *testing.T) {
	m := NewPlayerManage()
	m.AddAbstractPlayer(&pmMockConnect{}, profileData("h1", "alice", 0, 1), nil)
	m.AddAbstractPlayer(&pmMockConnect{}, profileData("h2", "bob", 1, 2), nil)

	m.CleanPlayerAllData()

	if m.PlayerGroup.Size() != 0 || m.PlayerAll.Size() != 0 {
		t.Fatalf("清空后 = %d/%d, 期望 0/0", m.PlayerGroup.Size(), m.PlayerAll.Size())
	}
}

// TestAddAINotPortable addAI 需要协议扩展, 当前明确报错而不是假装成功
func TestAddAINotPortable(t *testing.T) {
	m := NewPlayerManage()
	if err := m.AddAI(); err != ErrNotPortable {
		t.Fatalf("AddAI err = %v, 期望 ErrNotPortable", err)
	}
}
