package netconnectprotocol

import (
	"rwhps/internal/core/service"
	"rwhps/internal/net/core"
	netpacket "rwhps/internal/net/core/server/netpacket"
	"rwhps/internal/net/netconnectprotocol/realize"
	"rwhps/internal/util/log"
)

// RelayRwHps 对应上游 RwHps 在 RelayProtocol / RelayMulticastProtocol 下的实例
//
// 上游 RwHps 是通用类, 经 ServiceLoader 按 netType 反射查找 ProtocolType /
// ProtocolPacket; 上游给 Relay 注册的实现是:
//
//	ProtocolType   : TypeRelay (Initialization.loadService)
//	Protocol       : GameVersionRelay
//	ProtocolPacket : 未注册 -> 回落 GlobalProtocol -> GameVersionPacket
//
// Go 版把两级反射查找收敛为构造时的一次装配, 来源不变.
// (ServerProtocol 的同款装配在 net/gameserver.NewRwHps.)
type RelayRwHps struct {
	netType           core.NetType
	typeConnect       core.TypeConnect
	abstractNetPacket netpacket.AbstractNetPacket
}

// 编译期确认: RelayRwHps 实现 core.IRwHps
var _ core.IRwHps = (*RelayRwHps)(nil)

// NewRelayRwHps 构造 Relay 风味的协议实现
//
// RelayMulticastProtocol 上游对应 TypeRelayRebroadcast / GameVersionRelayRebroadcast
// (多播转发), 尚未移植; 传入该类型时同样装配 TypeRelay, 行为等价于普通 Relay
// (multicastAnalysis 在 GameVersionRelay 里本就是空实现).
func NewRelayRwHps(netType core.NetType) *RelayRwHps {
	return &RelayRwHps{
		netType:           netType,
		typeConnect:       NewTypeRelayFactory(),
		abstractNetPacket: lookupRelayNetPacket(netType),
	}
}

// lookupRelayNetPacket 对应 RwHps.abstractNetPacket 的惰性解析
//
// 先按 netType 名查 ProtocolPacket 服务, 查不到回落 GlobalProtocol ——
// 与上游 try/catch 回落完全一致 (上游 Relay 两型都没有注册 ProtocolPacket,
// 实际总是落到 GlobalProtocol 的 GameVersionPacket).
func lookupRelayNetPacket(netType core.NetType) netpacket.AbstractNetPacket {
	for _, name := range []string{netType.String(), core.GlobalProtocol.String()} {
		if v, ok := service.Default.Lookup(service.KindProtocolPacket, name); ok {
			if np, ok := v.(netpacket.AbstractNetPacket); ok {
				return np
			}
		}
	}
	log.Warnf("[ServiceLoader]", "ProtocolPacket not registered, falling back to GameVersionPacket")
	return realize.NewGameVersionPacket()
}

// NetType 实现 core.IRwHps
func (r *RelayRwHps) NetType() core.NetType { return r.netType }

// TypeConnect 实现 core.IRwHps: 返回连接解析器工厂
func (r *RelayRwHps) TypeConnect() core.TypeConnect { return r.typeConnect }

// AbstractNetPacket 实现 core.IRwHps: 返回包构造器
func (r *RelayRwHps) AbstractNetPacket() netpacket.AbstractNetPacket { return r.abstractNetPacket }
