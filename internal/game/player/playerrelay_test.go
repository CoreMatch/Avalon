package player

import (
	"strings"
	"testing"
)

// TestSetTeamIgnoresValue 锁住原版 team setter 的行为.
//
// 原版是 field = site 而不是 field = value. 这个测试的存在就是为了防止后来者
// "顺手修正"它 —— 一旦改成 field = value, team 会与 site 脱钩, 而
// getPacketTeamData 读回 team 的下游看到的数据就全变了.
func TestSetTeamIgnoresValue(t *testing.T) {
	p := NewPlayerRelay(nil, "uuid-1", "tester")

	p.Site = 7
	p.SetTeam(3)

	// team 不被赋值影响, 始终跟随 site
	if p.Team != 7 {
		t.Errorf("SetTeam(3) 后 Team = %d, 期望 7 (应等于 Site)", p.Team)
	}
	if p.Watch {
		t.Error("SetTeam(3) 不应把 Watch 置 true")
	}

	// -3 是唯一的例外: 它决定 watch, 但依然不改 team
	p.SetTeam(-3)
	if !p.Watch {
		t.Error("SetTeam(-3) 应把 Watch 置 true")
	}
	if p.Team != 7 {
		t.Errorf("SetTeam(-3) 后 Team = %d, 期望仍为 7", p.Team)
	}

	// site 变了, team 跟着变 (下一次 SetTeam 时同步)
	p.Site = 2
	p.SetTeam(0)
	if p.Team != 2 {
		t.Errorf("Site 改为 2 后 Team = %d, 期望 2", p.Team)
	}
}

// TestSetTeamWatchOnlyForMinusThree watch 只对 -3 为真
func TestSetTeamWatchOnlyForMinusThree(t *testing.T) {
	p := NewPlayerRelay(nil, "uuid-1", "tester")

	for _, v := range []int{-4, -2, -1, 0, 1, 100} {
		p.SetTeam(v)
		if p.Watch {
			t.Errorf("SetTeam(%d) 把 Watch 置 true 了", v)
		}
	}
	p.SetTeam(-3)
	if !p.Watch {
		t.Error("SetTeam(-3) 应把 Watch 置 true")
	}
}

// TestNewPlayerRelayCounters 三个计数器按原版的超时/上限构造
//
// 原版: TimeAndNumber(10,3) / TimeAndNumber(10,5) / TimeAndNumber(60,3).
// 计数器本身的语义 (startTime 打戳时机等) 由 totalizer 包的测试负责, 这里只确认
// 三个字段都被正确创建且初始可用.
func TestNewPlayerRelayCounters(t *testing.T) {
	p := NewPlayerRelay(nil, "uuid-1", "tester")

	if p.LastMessageCount == nil || p.LastShortMessageCount == nil || p.PlayerSyncCount == nil {
		t.Fatal("三个计数器不应为 nil")
	}

	// 刚构造时 count = 0 < countMax, CheckStatus 必须放行
	if !p.LastMessageCount.CheckStatus() {
		t.Error("LastMessageCount 初始 CheckStatus 应为 true")
	}
	if !p.LastShortMessageCount.CheckStatus() {
		t.Error("LastShortMessageCount 初始 CheckStatus 应为 true")
	}
	if !p.PlayerSyncCount.CheckStatus() {
		t.Error("PlayerSyncCount 初始 CheckStatus 应为 true")
	}
}

// TestSetLastSentMessageRefreshesTime setter 必须刷新时间戳
func TestSetLastSentMessageRefreshesTime(t *testing.T) {
	p := NewPlayerRelay(nil, "uuid-1", "tester")

	if p.LastMessageTime != 0 {
		t.Fatalf("初始 LastMessageTime = %d, 期望 0", p.LastMessageTime)
	}

	p.SetLastSentMessage("hello")
	if p.LastSentMessage != "hello" {
		t.Errorf("LastSentMessage = %q, want %q", p.LastSentMessage, "hello")
	}
	if p.LastMessageTime == 0 {
		t.Error("SetLastSentMessage 后 LastMessageTime 应被刷新")
	}

	// 直接给字段赋值不该刷新时间戳 —— 这正是要用 setter 的原因
	before := p.LastMessageTime
	p.LastSentMessage = "direct"
	if p.LastMessageTime != before {
		t.Error("直接赋值字段意外刷新了 LastMessageTime")
	}
}

// TestSetDisconnectRefreshesTime
func TestSetDisconnectRefreshesTime(t *testing.T) {
	p := NewPlayerRelay(nil, "uuid-1", "tester")

	p.SetDisconnect(true)
	if !p.Disconnect {
		t.Error("SetDisconnect(true) 后 Disconnect 应为 true")
	}
	if p.DisconnectTime == 0 {
		t.Error("SetDisconnect 后 DisconnectTime 应被刷新")
	}
}

// TestConIsAnyHandle con 用 any 断开导入环, 这里确认它就是个透明把手
func TestConIsAnyHandle(t *testing.T) {
	type fakeRelay struct{ id int }

	p := NewPlayerRelay(fakeRelay{id: 42}, "uuid-1", "tester")

	got, ok := p.Con.(fakeRelay)
	if !ok {
		t.Fatalf("Con 取不回原值, 得到 %T", p.Con)
	}
	if got.id != 42 {
		t.Errorf("Con.id = %d, want 42", got.id)
	}

	// nil 也要能存 (原版 conIn 是非空类型, 但 Go 侧环断开后允许 nil)
	p.Con = nil
	if p.Con != nil {
		t.Error("Con 应可置 nil")
	}
}

// TestStringMatchesKotlinTrimIndent 对照原版多行字符串的每一行
func TestStringMatchesKotlinTrimIndent(t *testing.T) {
	p := NewPlayerRelay(nil, "uuid-abc", "tester")
	p.Site = 4
	p.Team = 9 // 直接设字段, 只影响 String 输出

	want := "Player :\nName : tester\nuuid-Hash : uuid-abc\n\nPosition : 4\nTeam : 9\n"
	if got := p.String(); got != want {
		t.Errorf("String() =\n%q\nwant\n%q", got, want)
	}
	if !strings.HasPrefix(p.String(), "Player :\n") {
		t.Error("String() 应以 Player : 开头")
	}
}
