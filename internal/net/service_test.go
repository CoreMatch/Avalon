package net

import (
	"testing"

	"rwhps/internal/data/global"
	"rwhps/internal/game/manage"
	"rwhps/internal/net/core"
	nethttp "rwhps/internal/net/http"
)

// resetServices 清掉 NetStaticData 里登记的服务, 避免用例之间互相污染
func resetServices(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		for _, s := range NetStaticData.Services() {
			NetStaticData.RemoveService(s)
		}
	})
}

// withMixProtocol 临时开关 mixProtocolEnable
func withMixProtocol(t *testing.T, on bool) {
	t.Helper()
	old := global.Config.MixProtocolEnable
	global.Config.MixProtocolEnable = on
	t.Cleanup(func() { global.Config.MixProtocolEnable = old })
}

// TestNewNetServiceRouting 对应构造函数里的 when(rwHps.netType) 分派
func TestNewNetServiceRouting(t *testing.T) {
	resetServices(t)
	withMixProtocol(t, false)

	cases := []struct {
		netType core.NetType
		want    NetServiceKind
	}{
		{core.ServerProtocol, NetServiceHeadless},
		{core.ServerProtocolOld, NetServiceHeadless},
		{core.ServerTestProtocol, NetServiceHeadless},
		{core.RelayProtocol, NetServiceHeadless},
		{core.RelayMulticastProtocol, NetServiceHeadless},
		{core.DedicatedToTheBackend, NetServiceHeadless},
		{core.HttpProtocol, NetServiceHTTP},
		{core.RemoteControlProtocol, NetServiceRemoteControl},
	}
	for _, c := range cases {
		ns := NewNetService("test-"+c.netType.String(), core.NewNullRwHps(c.netType))
		if ns.NetType != c.want {
			t.Fatalf("%s -> %v, want %v", c.netType, ns.NetType, c.want)
		}
		if got := ns.NetType.String(); got == "" {
			t.Fatalf("%s: NetType.String() 为空", c.netType)
		}
	}
}

// TestNewNetServiceStarterTypes 直接断言 start 的具体类型, 防止路由退化回 NewAbstractNet
func TestNewNetServiceStarterTypes(t *testing.T) {
	resetServices(t)
	withMixProtocol(t, false)

	httpSvc := NewNetService("http", core.NewNullRwHps(core.HttpProtocol))
	if _, ok := httpSvc.start.(*nethttp.StartHttp); !ok {
		t.Fatalf("HttpProtocol 应使用 *http.StartHttp, got %T", httpSvc.start)
	}

	rconSvc := NewNetService("rcon", core.NewNullRwHps(core.RemoteControlProtocol))
	if _, ok := rconSvc.start.(*StartRemoteControl); !ok {
		t.Fatalf("RemoteControlProtocol 应使用 *StartRemoteControl, got %T", rconSvc.start)
	}

	gameSvc := NewNetService("game", core.NewNullRwHps(core.ServerProtocol))
	if _, ok := gameSvc.start.(*AbstractNet); !ok {
		t.Fatalf("ServerProtocol 应使用 *AbstractNet, got %T", gameSvc.start)
	}
}

// TestNewNetServiceMix 对应 mixProtocolEnable 分支
func TestNewNetServiceMix(t *testing.T) {
	resetServices(t)
	withMixProtocol(t, true)
	manage.Clear()
	GlobalCache = NewCache()
	t.Cleanup(func() {
		manage.Clear()
		GlobalCache = NewCache()
	})

	ns := NewNetService("mix", core.NewNullRwHps(core.ServerProtocol))
	if ns.NetType != NetServiceMix {
		t.Fatalf("NetType = %v, want %v", ns.NetType, NetServiceMix)
	}
	if _, ok := ns.start.(*StartMixProtocol); !ok {
		t.Fatalf("mixProtocolEnable 应使用 *StartMixProtocol, got %T", ns.start)
	}
}

// TestNewNetServicePanics 覆盖原版 require / TODO() 的两条硬失败路径
func TestNewNetServicePanics(t *testing.T) {
	resetServices(t)
	withMixProtocol(t, false)

	mustPanic := func(name string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Fatalf("%s 应 panic", name)
			}
		}()
		fn()
	}

	mustPanic("nil rwHps", func() { NewNetService("x", nil) })
	mustPanic("NullProtocol", func() { NewNetService("x", core.NewNullRwHps(core.NullProtocol)) })
	mustPanic("GlobalProtocol", func() { NewNetService("x", core.NewNullRwHps(core.GlobalProtocol)) })
}

// TestNetServiceRegistersAndStops 对应 init { netService.add(this) } 与 stop() 的 remove
func TestNetServiceRegistersAndStops(t *testing.T) {
	resetServices(t)
	withMixProtocol(t, false)

	ns := NewNetService("reg", core.NewNullRwHps(core.ServerProtocol))

	found := false
	for _, s := range NetStaticData.Services() {
		if s == ns {
			found = true
		}
	}
	if !found {
		t.Fatal("构造后 NetService 未登记到 NetStaticData")
	}

	ns.Stop()

	for _, s := range NetStaticData.Services() {
		if s == ns {
			t.Fatal("Stop 后 NetService 仍留在 NetStaticData")
		}
	}

	// Stop 幂等
	ns.Stop()
}

// TestNetServiceSetWebData 对应 setWebData: 只有 Web 系启动器接受
func TestNetServiceSetWebData(t *testing.T) {
	resetServices(t)
	withMixProtocol(t, false)

	ns := NewNetService("web", core.NewNullRwHps(core.HttpProtocol))
	if got := ns.SetWebData(nil); got != ns {
		t.Fatal("SetWebData 应返回自身以便链式调用")
	}

	// 非 Web 启动器不应 panic
	game := NewNetService("game", core.NewNullRwHps(core.ServerProtocol))
	game.SetWebData(nil)
}

// TestNetServiceEqualsHashCode 对应 equals / hashCode (String.hashCode 算法)
func TestNetServiceEqualsHashCode(t *testing.T) {
	a := &NetService{ID: "abc"}
	b := &NetService{ID: "abc"}
	c := &NetService{ID: "abd"}

	if !a.Equals(b) {
		t.Fatal("同 ID 应相等")
	}
	if a.Equals(c) || a.Equals(nil) {
		t.Fatal("不同 ID / nil 应不相等")
	}
	if a.HashCode() != b.HashCode() {
		t.Fatal("同 ID 的 HashCode 应相同")
	}
	// Java "abc".hashCode() == 96354
	if got := a.HashCode(); got != 96354 {
		t.Fatalf("HashCode() = %d, want 96354", got)
	}
}

// TestCoreID 对应 NetService.Companion.coreID(): 10 位随机串
func TestCoreID(t *testing.T) {
	id := CoreID()
	if len(id) != 10 {
		t.Fatalf("CoreID() 长度 = %d, want 10", len(id))
	}
	if CoreID() == id {
		t.Fatal("两次 CoreID() 不应相同")
	}
}
