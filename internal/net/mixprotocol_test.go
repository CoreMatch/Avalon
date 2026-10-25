package net

import (
	"encoding/binary"
	"testing"

	"rwhps/internal/data/global"
	"rwhps/internal/game/manage"
	"rwhps/internal/io/packettype"
	"rwhps/internal/net/core"
)

// withMixConfig 临时修改混合协议开关, 测试结束恢复
func withMixConfig(t *testing.T, rconMix, webMix bool) {
	t.Helper()
	oldRcon, oldWeb := global.Config.RconMixEnable, global.Config.WebMixEnable
	global.Config.RconMixEnable = rconMix
	global.Config.WebMixEnable = webMix
	t.Cleanup(func() {
		global.Config.RconMixEnable = oldRcon
		global.Config.WebMixEnable = oldWeb
	})
}

// gameHead 构造 Game 协议首包头部: [length BE][type BE]
func gameHead(length, typ int32) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint32(b[0:4], uint32(length))
	binary.BigEndian.PutUint32(b[4:8], uint32(typ))
	return b
}

// rconHead 构造 RCON 首包头部: [length LE][id LE][type LE]
func rconHead(length, id, typ int32) []byte {
	b := make([]byte, 12)
	binary.LittleEndian.PutUint32(b[0:4], uint32(length))
	binary.LittleEndian.PutUint32(b[4:8], uint32(id))
	binary.LittleEndian.PutUint32(b[8:12], uint32(typ))
	return b
}

// TestSniffGame 对应 StartMixProtocol 第 1 分支
func TestSniffGame(t *testing.T) {
	withMixConfig(t, true, true)

	// 已知协议号且 > 1000
	if got := Sniff(gameHead(4, 2001)); got != MixGame {
		t.Fatalf("type=2001(SERVER_DEBUG) -> %v, want MixGame", got)
	}
	// PREREGISTER_INFO_RECEIVE (160) 即使 < 1000 也算 Game
	if got := Sniff(gameHead(4, int32(packettype.PREREGISTER_INFO_RECEIVE))); got != MixGame {
		t.Fatalf("type=160 -> %v, want MixGame", got)
	}
	// 已知但 <= 1000 且不是 160 -> 不是 Game (原版 from().typeInt 比较的结果)
	if got := Sniff(gameHead(4, int32(packettype.HEART_BEAT))); got != MixHttp {
		t.Fatalf("type=108(HEART_BEAT) -> %v, want MixHttp", got)
	}
	// 未知协议号 -> from() 返回 NOT_RESOLVED(-1), 不算 Game
	if got := Sniff(gameHead(4, 16777216)); got != MixHttp {
		t.Fatalf("未知 type=16777216 -> %v, want MixHttp", got)
	}
	// length <= 0 不算
	if got := Sniff(gameHead(0, 2001)); got != MixHttp {
		t.Fatalf("length=0 -> %v, want MixHttp (回落到 HTTP)", got)
	}
	// 不足 8 字节不算 Game
	if got := Sniff(gameHead(4, 2001)[:7]); got != MixHttp {
		t.Fatalf("7 字节 -> %v, want MixHttp (回落到 HTTP)", got)
	}
}

// TestSniffRcon 对应 StartMixProtocol 第 2 分支 (需 rconMixEnable)
func TestSniffRcon(t *testing.T) {
	withMixConfig(t, true, true)

	// length 在 4..4096, type 在 0..3 且 != 1
	for _, typ := range []int32{0, 2, 3} {
		if got := Sniff(rconHead(10, 7, typ)); got != MixRcon {
			t.Fatalf("type=%d -> %v, want MixRcon", typ, got)
		}
	}
	// type == 1 排除
	if got := Sniff(rconHead(10, 7, 1)); got != MixHttp {
		t.Fatalf("type=1 -> %v, want MixHttp", got)
	}
	// length 越界排除
	if got := Sniff(rconHead(3, 7, 0)); got != MixHttp {
		t.Fatalf("length=3 -> %v, want MixHttp", got)
	}
	if got := Sniff(rconHead(4097, 7, 0)); got != MixHttp {
		t.Fatalf("length=4097 -> %v, want MixHttp", got)
	}

	// 开关关闭时不识别 RCON
	withMixConfig(t, false, true)
	if got := Sniff(rconHead(10, 7, 0)); got != MixHttp {
		t.Fatalf("rconMixEnable=false -> %v, want MixHttp", got)
	}
}

// TestSniffHttpAndFallback 对应第 3/4 分支
//
// 原版 webMixEnable 打开时该分支无条件 return true, 所以任何非 Game/RCON 的流量都归 HTTP;
// 关闭时返回 MixUnknown, 但 pick 仍把它交给 HTTP (原版 super.channelRead -> StartHttp).
func TestSniffHttpAndFallback(t *testing.T) {
	withMixConfig(t, false, true)
	if got := Sniff([]byte("GET / HTTP/1.1\r\n")); got != MixHttp {
		t.Fatalf("webMixEnable=true -> %v, want MixHttp", got)
	}

	withMixConfig(t, false, false)
	if got := Sniff([]byte("GET / HTTP/1.1\r\n")); got != MixUnknown {
		t.Fatalf("webMixEnable=false -> %v, want MixUnknown", got)
	}
	if got := Sniff(nil); got != MixUnknown {
		t.Fatalf("空首包 -> %v, want MixUnknown", got)
	}
}

// TestMixKindString 保证日志可读
func TestMixKindString(t *testing.T) {
	cases := map[MixKind]string{
		MixUnknown: "Unknown",
		MixGame:    "Game",
		MixRcon:    "RCON",
		MixHttp:    "HTTP",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Fatalf("%d.String() = %q, want %q", int(k), got, want)
		}
	}
}

// TestMixPickRouting 验证嗅探结果到启动器的映射
func TestMixPickRouting(t *testing.T) {
	m := NewStartMixProtocol(core.NewNullRwHps(core.ServerProtocol))

	if got := m.pick(MixGame); got != ChannelStarter(m.game) {
		t.Fatalf("MixGame 应路由到 game, got %T", got)
	}
	if got := m.pick(MixRcon); got != ChannelStarter(m.remote) {
		t.Fatalf("MixRcon 应路由到 remote, got %T", got)
	}
	// MixUnknown 必须落到 HTTP —— 对应原版 super.channelRead (父类 StartHttp)
	for _, k := range []MixKind{MixHttp, MixUnknown} {
		if got := m.pick(k); got != ChannelStarter(m.http) {
			t.Fatalf("%v 应路由到 http, got %T", k, got)
		}
	}
}

// TestNewStartMixProtocolUsesCache 验证两个辅助协议走 GlobalCache + manage 注册
func TestNewStartMixProtocolUsesCache(t *testing.T) {
	manage.Clear()
	GlobalCache = NewCache()
	t.Cleanup(func() {
		manage.Clear()
		GlobalCache = NewCache()
	})

	_ = NewStartMixProtocol(core.NewNullRwHps(core.ServerProtocol))

	if _, ok := GlobalCache.iRwHpsCache["MixRemoteControl"]; !ok {
		t.Fatal("MixRemoteControl 未写入 GlobalCache")
	}
	if _, ok := GlobalCache.iRwHpsCache["MixHttp"]; !ok {
		t.Fatal("MixHttp 未写入 GlobalCache")
	}
	// 对应原版 addIRwHps(..., "MixRemoteControlDef") / "MixHttpDef"
	if _, ok := manage.Get("MixRemoteControlDef"); !ok {
		t.Fatal("MixRemoteControlDef 未注册到 IRwHpsManage")
	}
	if _, ok := manage.Get("MixHttpDef"); !ok {
		t.Fatal("MixHttpDef 未注册到 IRwHpsManage")
	}

	// 二次构造应复用缓存, 不重复注册新实例
	before := len(manage.All())
	_ = NewStartMixProtocol(core.NewNullRwHps(core.ServerProtocol))
	if after := len(manage.All()); after != before {
		t.Fatalf("重复构造新增了 %d 个 IRwHps", after-before)
	}
}
