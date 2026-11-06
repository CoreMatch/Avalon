package global

import (
	"testing"

	"rwhps/internal/game/player"
	"rwhps/internal/util/timeutil"
)

// TestDefaultChatFilterRepeat 上游 Administration.init 的默认过滤器:
// 非管理员 30 秒内不能发送两条相同消息
func TestDefaultChatFilterRepeat(t *testing.T) {
	a := NewAdministration()
	p := player.New(nil, nil, nil)

	if _, ok := a.FilterMessage(p, "hello"); !ok {
		t.Fatal("第一条消息应放行")
	}
	if _, ok := a.FilterMessage(p, "hello"); ok {
		t.Fatal("30 秒内重复消息应被丢弃")
	}
	if _, ok := a.FilterMessage(p, "world"); !ok {
		t.Fatal("不同消息应放行")
	}
}

// TestDefaultChatFilterAdminExempt 管理员不受重复消息限制
func TestDefaultChatFilterAdminExempt(t *testing.T) {
	a := NewAdministration()
	p := player.New(nil, nil, nil)
	p.IsAdmin = true

	if _, ok := a.FilterMessage(p, "same"); !ok {
		t.Fatal("管理员第一条应放行")
	}
	if _, ok := a.FilterMessage(p, "same"); !ok {
		t.Fatal("管理员重复消息也应放行")
	}
}

// TestDefaultChatFilterRepeatExpire 30 秒之后允许重复 (通过回拨 lastMessageTime 模拟)
func TestDefaultChatFilterRepeatExpire(t *testing.T) {
	a := NewAdministration()
	p := player.New(nil, nil, nil)

	if _, ok := a.FilterMessage(p, "hello"); !ok {
		t.Fatal("第一条消息应放行")
	}
	p.LastMessageTime = timeutil.ConcurrentMillis() - 31*1000
	if _, ok := a.FilterMessage(p, "hello"); !ok {
		t.Fatal("超过 30 秒后重复消息应放行")
	}
}

// TestAddChatFilterMute 对应 EventGlobal.registerServerLoadEvent 注册的禁言过滤器
func TestAddChatFilterMute(t *testing.T) {
	a := NewAdministration()
	a.AddChatFilter(ChatFilterFunc(func(p *player.Player, message string) (string, bool) {
		if p.MuteTime > timeutil.ConcurrentMillis() {
			return "", false
		}
		return message, true
	}))

	muted := player.New(nil, nil, nil)
	muted.MuteTime = timeutil.GetTimeFutureMillis(60 * 1000)
	if _, ok := a.FilterMessage(muted, "hi"); ok {
		t.Fatal("禁言玩家消息应被丢弃")
	}

	normal := player.New(nil, nil, nil)
	if msg, ok := a.FilterMessage(normal, "hi"); !ok || msg != "hi" {
		t.Fatalf("正常玩家消息应放行: %q %v", msg, ok)
	}
}

// TestFilterMessagePassesOriginal 上游把原始消息 (而非上一个过滤器的输出) 传给每个过滤器
func TestFilterMessagePassesOriginal(t *testing.T) {
	a := NewAdministration()
	a.AddChatFilter(ChatFilterFunc(func(_ *player.Player, message string) (string, bool) {
		return message + "-first", true
	}))

	var seen string
	a.AddChatFilter(ChatFilterFunc(func(_ *player.Player, message string) (string, bool) {
		seen = message
		return message, true
	}))

	p := player.New(nil, nil, nil)
	if _, ok := a.FilterMessage(p, "abc"); !ok {
		t.Fatal("消息应放行")
	}
	// 第二个过滤器看到的应是原始 "abc" 而不是 "abc-first" (上游行为)
	if seen != "abc" {
		t.Fatalf("过滤器应收到原始消息, got %q", seen)
	}
}

// TestPlayerDataCache 玩家离线档案的读写清
func TestPlayerDataCache(t *testing.T) {
	a := NewAdministration()

	info := NewPlayerInfo("hex-1", timeutil.GetTimeFutureMillis(60*1000), 0)
	a.PlayerDataCache["hex-1"] = info

	got, ok := a.PlayerDataCache["hex-1"]
	if !ok || got.UUID != "hex-1" || got.TimesKicked != info.TimesKicked {
		t.Fatalf("读取离线档案失败: %v", got)
	}

	a.PlayerDataCache = map[string]*PlayerInfo{}
	if _, ok := a.PlayerDataCache["hex-1"]; ok {
		t.Fatal("清空后不应再命中")
	}
}
