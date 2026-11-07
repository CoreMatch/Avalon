package manage

import (
	"testing"

	"github.com/CoreMatch/Avalon/internal/net/core"
)

// reset 清空全局注册表
func reset(t *testing.T) {
	t.Helper()
	Clear()
	t.Cleanup(Clear)
}

// TestAddProtocolProviderDefaultID customID 为空时取 netType 名字 (原版默认参数 customId = value.name)
func TestAddProtocolProviderDefaultID(t *testing.T) {
	reset(t)

	got := AddProtocolProvider(core.ServerProtocol, "")
	if got == nil {
		t.Fatal("AddProtocolProvider 返回 nil")
	}
	if got.NetType() != core.ServerProtocol {
		t.Fatalf("NetType() = %v, want ServerProtocol", got.NetType())
	}
	if _, ok := Get("ServerProtocol"); !ok {
		t.Fatal("默认 key 应为 netType 名字")
	}
}

// TestAddProtocolProviderCustomID 对应原版 addProtocolProvider(value, customId)
func TestAddProtocolProviderCustomID(t *testing.T) {
	reset(t)

	AddProtocolProvider(core.RemoteControlProtocol, "MixRemoteControlDef")
	if _, ok := Get("MixRemoteControlDef"); !ok {
		t.Fatal("customID 未生效")
	}
	if _, ok := Get("RemoteControlProtocol"); ok {
		t.Fatal("不应同时用 netType 名字注册")
	}
}

// TestAddProtocolProviderPanicsOnNull 对应原版 require(value != NullProtocol)
func TestAddProtocolProviderPanicsOnNull(t *testing.T) {
	reset(t)

	defer func() {
		if recover() == nil {
			t.Fatal("NullProtocol 应 panic")
		}
	}()
	AddProtocolProvider(core.NullProtocol, "x")
}

// TestFirstServer 对应 ProtocolManage.firstServer: 只挑服务端协议
func TestFirstServer(t *testing.T) {
	reset(t)

	if got := FirstServer(); got != nil {
		t.Fatalf("空注册表 FirstServer() = %v, want nil", got)
	}

	// 先注册非服务端协议, 不应被选中
	AddProtocolProvider(core.HttpProtocol, "h")
	AddProtocolProvider(core.RemoteControlProtocol, "r")
	if got := FirstServer(); got != nil {
		t.Fatalf("只有 HTTP/RCON 时 FirstServer() = %v, want nil", got.NetType())
	}

	AddProtocolProvider(core.ServerProtocol, "s")
	got := FirstServer()
	if got == nil {
		t.Fatal("FirstServer() = nil, want ServerProtocol")
	}
	if got.NetType() != core.ServerProtocol {
		t.Fatalf("FirstServer().NetType() = %v, want ServerProtocol", got.NetType())
	}
}

// TestAllKeepsOrder 保证注册顺序稳定 (原版 ObjectMap 亦保序)
func TestAllKeepsOrder(t *testing.T) {
	reset(t)

	AddProtocolProvider(core.ServerProtocol, "a")
	AddProtocolProvider(core.RelayProtocol, "b")
	AddProtocolProvider(core.HttpProtocol, "c")

	all := All()
	if len(all) != 3 {
		t.Fatalf("All() 长度 = %d, want 3", len(all))
	}
	want := []core.NetType{core.ServerProtocol, core.RelayProtocol, core.HttpProtocol}
	for i, w := range want {
		if all[i].NetType() != w {
			t.Fatalf("All()[%d].NetType() = %v, want %v", i, all[i].NetType(), w)
		}
	}

	// 覆盖同名 key 不改变顺序
	AddProtocolProvider(core.ServerProtocol, "a")
	if got := len(All()); got != 3 {
		t.Fatalf("覆盖后 All() 长度 = %d, want 3", got)
	}
}

// TestSetProvider 验证协议实现工厂可替换 (插件系统缺席时的接缝)
func TestSetProvider(t *testing.T) {
	reset(t)

	called := 0
	SetProvider(func(netType core.NetType) core.IProtocolProvider {
		called++
		return core.NewNullProtocolProvider(netType)
	})
	AddProtocolProvider(core.ServerProtocol, "p")
	if called != 1 {
		t.Fatalf("provider 调用次数 = %d, want 1", called)
	}

	// 传 nil 恢复默认工厂
	SetProvider(nil)
	if got := AddProtocolProvider(core.ServerProtocol, "q"); got == nil {
		t.Fatal("恢复默认工厂后 AddProtocolProvider 返回 nil")
	}
	if called != 1 {
		t.Fatalf("恢复默认后不应再调用自定义 provider, called=%d", called)
	}
}
