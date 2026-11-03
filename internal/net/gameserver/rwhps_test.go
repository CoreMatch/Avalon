package gameserver

import (
	"testing"

	"rwhps/internal/core/service"
	"rwhps/internal/game"
	"rwhps/internal/game/manage"
	"rwhps/internal/net"
	"rwhps/internal/net/core"
	netpacket "rwhps/internal/net/core/server/netpacket"
	"rwhps/internal/net/netconnectprotocol/realize"
)

// registerServerProtocol 复刻启动链的注册关系 (见 app.loadService 与 start 命令):
// provider 按 netType 装配 + AddIRwHps 登记实例 + ProtocolPacket 服务就位.
func registerServerProtocol(t *testing.T) {
	t.Helper()

	// 与 loadService 一致: 先有 ProtocolPacket 服务, 才有 abstractNetPacket 的正常来源
	if err := service.Default.RegisterDefault(service.KindProtocolPacket, core.GlobalProtocol.String(), realize.NewGameVersionPacket()); err != nil {
		t.Fatalf("注册 ProtocolPacket: %v", err)
	}

	manage.SetProvider(func(netType core.NetType) core.IRwHps {
		if netType == core.ServerProtocol {
			return NewRwHps(netType)
		}
		return core.NewNullRwHps(netType)
	})
	manage.AddIRwHps(core.ServerProtocol, "")

	net.NetStaticData.ResetRwHps()
	t.Cleanup(func() {
		manage.Clear()
		net.NetStaticData.ResetRwHps()
		service.Default.Remove(service.KindProtocolPacket, core.GlobalProtocol.String())
	})
}

// TestNetStaticDataRwHpsIsServerProtocol 验收: NetStaticData.RwHps() 不再落到 NullRwHps
//
// 上游对应关系: 引擎启动时 CustomServerSocket.run 调 IRwHpsManage.addIRwHps(ServerProtocol),
// 之后 NetStaticData.RwHps (by lazy) 取到的就是真实协议实现 (firstServer).
func TestNetStaticDataRwHpsIsServerProtocol(t *testing.T) {
	registerServerProtocol(t)

	rw := net.NetStaticData.RwHps()
	if _, ok := rw.(*core.NullRwHps); ok {
		t.Fatal("NetStaticData.RwHps() 仍是 NullRwHps")
	}
	if rw.NetType() != core.ServerProtocol {
		t.Fatalf("NetType = %v", rw.NetType())
	}
	if rw.TypeConnect() == nil {
		t.Fatal("TypeConnect 工厂缺失")
	}
	if rw.AbstractNetPacket() == nil {
		t.Fatal("AbstractNetPacket 缺失")
	}
	if !net.CheckServerStartNet(nil) {
		t.Fatal("CheckServerStartNet 应放行服务端协议")
	}
}

// TestTypeConnectFactoryCreatesPerConnectionHandler 每条连接一个独立处理器
//
// 对应上游 getTypeConnect: 工厂实例本身不变, 每次调用产出新的
// HeadlessTypeConnect(GameVersionServer).
func TestTypeConnectFactoryCreatesPerConnectionHandler(t *testing.T) {
	registerServerProtocol(t)

	factory := net.NetStaticData.RwHps().TypeConnect()

	SetHelper(nil)
	game.SetDefaultRoom(game.NewGameSession("room-factory", nil))
	t.Cleanup(func() { game.SetDefaultRoom(nil) })

	ca1 := core.NewTestConnectionAgreement()
	ca2 := core.NewTestConnectionAgreement()
	tc1 := factory.GetTypeConnect(ca1)
	tc2 := factory.GetTypeConnect(ca2)

	if tc1 == tc2 {
		t.Fatal("两条连接应得到不同的处理器")
	}
	if tc1 == factory || tc2 == factory {
		t.Fatal("处理器不应复用工厂实例")
	}
	if tc1.AbstractNetConnect() == nil || tc2.AbstractNetConnect() == nil {
		t.Fatal("处理器必须绑定连接")
	}
	if tc1.AbstractNetConnect() == tc2.AbstractNetConnect() {
		t.Fatal("两条连接的 AbstractNetConnect 不应相同")
	}

	// 清掉路由表里的残留, 避免影响其它测试
	t.Cleanup(func() {
		connections.remove(tc1.(*HeadlessTypeConnect).con.connID)
		connections.remove(tc2.(*HeadlessTypeConnect).con.connID)
	})
}

// TestAbstractNetPacketFromGlobalService 包构造器来自 ProtocolPacket 服务 (GlobalProtocol 回落)
//
// 对应上游 RwHps.abstractNetPacket 的 ServiceLoader 查找 + 回落.
func TestAbstractNetPacketFromGlobalService(t *testing.T) {
	registerServerProtocol(t)

	rw := net.NetStaticData.RwHps()
	np := rw.AbstractNetPacket()
	if np == nil {
		t.Fatal("AbstractNetPacket 为 nil")
	}
	// 无注册时也有兜底实现, 不应为 nil
	var _ netpacket.AbstractNetPacket = np
}

// TestOtherNetTypesStayNullProvider 非 ServerProtocol 仍回落到 NullRwHps 等价物
//
// Relay 等协议属于后续批次, 不能提前伪造实现.
func TestOtherNetTypesStayNullProvider(t *testing.T) {
	manage.SetProvider(func(netType core.NetType) core.IRwHps {
		if netType == core.ServerProtocol {
			return NewRwHps(netType)
		}
		return core.NewNullRwHps(netType)
	})
	t.Cleanup(func() { manage.Clear() })

	rw := manage.AddIRwHps(core.RelayProtocol, "relay-test")
	if _, ok := rw.(*core.NullRwHps); !ok {
		t.Fatalf("RelayProtocol 应为 NullRwHps 等价物, 得到 %T", rw)
	}
}
