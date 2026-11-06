package app

import (
	"testing"

	"rwhps/internal/data/global"
	"rwhps/internal/game/player"
	"rwhps/internal/net/core"
	"rwhps/internal/util/timeutil"
)

// TestEventGlobalMuteFilter ServerLoadEvent 注册禁言过滤器
// (对应 EventGlobal.registerServerLoadEvent)
func TestEventGlobalMuteFilter(t *testing.T) {
	setupBase(t)
	r := New(global.MainParameters{})

	r.registerEventGlobal()
	r.Events.ServerLoad()

	muted := player.New(nil, nil, nil)
	muted.MuteTime = timeutil.GetTimeFutureMillis(60 * 1000)
	if _, ok := global.Core.Admin.FilterMessage(muted, "hi"); ok {
		t.Fatal("禁言玩家的消息应被过滤")
	}

	normal := player.New(nil, nil, nil)
	if msg, ok := global.Core.Admin.FilterMessage(normal, "hi"); !ok || msg != "hi" {
		t.Fatalf("正常玩家消息应放行: %q %v", msg, ok)
	}
}

// TestEventGlobalBannedIP24 NetConnectNewEvent 拒绝 /24 黑名单 IP
// (对应 EventGlobal.registerNetConnectNewEvent)
func TestEventGlobalBannedIP24(t *testing.T) {
	setupBase(t)
	r := New(global.MainParameters{})

	oldHook := core.NetConnectNewEventHook
	t.Cleanup(func() { core.NetConnectNewEventHook = oldHook })

	r.registerEventGlobal()
	if core.NetConnectNewEventHook == nil {
		t.Fatal("应安装 NetConnectNewEventHook")
	}

	global.Core.Admin.BanIP24("1.2.3")

	banned := &core.ConnectionAgreement{IPLong24: "1.2.3"}
	if !core.NetConnectNewEventHook(banned) {
		t.Fatal("/24 黑名单内的连接应被拒绝")
	}

	clean := &core.ConnectionAgreement{IPLong24: "9.9.9"}
	if core.NetConnectNewEventHook(clean) {
		t.Fatal("非黑名单连接不应被拒绝")
	}
}
