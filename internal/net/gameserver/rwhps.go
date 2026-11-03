// Package gameserver 对应上游 plugin/internal/headless/inject/net 里的服务端协议实现
// (HeadlessTypeConnect / GameVersionServer).
//
// # 与上游的架构差异
//
// 上游这些类活在 headless 插件里, 游戏引擎 (rusted-warfare.jar) 被 ASM 注入到同一个
// JVM 内: 客户端的握手 (PREREGISTER_INFO / REGISTER_PLAYER / SERVER_INFO / TEAM_LIST)
// 由引擎自己的网络循环完成, RW-HPS 只在引擎边界改写少数包 (见 PlayerConnectX).
//
// Go 进程跑不了 Java 字节码, 引擎整体外置为 helper 进程 (见 game/external).
// 因此本包是 "RW 客户端" 与 "外部内核" 之间的协议网关:
//
//   - 连接建立时向 helper 登记 (conn.open), 对应上游 netEngine.aM.add(playerConnect)
//   - 客户端包按上游 HeadlessTypeConnect.processConnect 的同款分支分发:
//     聊天/游戏指令/断开被 Go 侧拦截处理, 其余包经 conn.packet 转给引擎
//   - helper 发来的包 (引擎 -> 客户端) 按连接 ID 路由回对应客户端
//
// 版本判定 (协议号 176 / 游戏版本 1.15) 与上游一致: 拒绝错误版本客户端的逻辑
// 在引擎的 PREREGISTER 握手内完成, Go 侧只转发 —— 上游同样如此, 见 DECISIONS D-4.
package gameserver

import (
	"rwhps/internal/core/service"
	"rwhps/internal/data/global"
	"rwhps/internal/net/core"
	netpacket "rwhps/internal/net/core/server/netpacket"
	"rwhps/internal/net/netconnectprotocol/realize"
	"rwhps/internal/util/log"
)

// RwHps 对应 netconnectprotocol.RwHps: 核心协议实现
//
// 上游实例化路径: IRwHpsManage.addIRwHps -> ServiceLoader(ServiceType.IRwHps)
// -> RwHps(classLoader, netType), 其 typeConnect / abstractNetPacket 再分别经
// ServiceLoader 的 ProtocolType / ProtocolPacket 解析. Go 版把两级反射查找
// 收敛为构造时的一次装配, 来源不变:
//
//	typeConnect       <- HeadlessTypeConnect (上游注册点 GameEngine.kt:118)
//	abstractNetPacket <- ServiceLoader(ProtocolPacket, netType) 回落 GlobalProtocol
//	                   (上游 RwHps.kt:54-62 的同款回落)
type RwHps struct {
	netType           core.NetType
	typeConnect       core.TypeConnect
	abstractNetPacket netpacket.AbstractNetPacket
}

// NewRwHps 构造 ServerProtocol 的协议实现
func NewRwHps(netType core.NetType) *RwHps {
	return &RwHps{
		netType:           netType,
		typeConnect:       NewHeadlessTypeConnect(),
		abstractNetPacket: lookupNetPacket(netType),
	}
}

// lookupNetPacket 对应 RwHps.abstractNetPacket 的惰性解析
//
// 先按 netType 名查 ProtocolPacket 服务, 查不到回落 GlobalProtocol ——
// 与上游 try/catch 回落完全一致. 两处都没有时退回 GameVersionPacket 直装,
// 保证服务端协议永远有包构造器可用 (上游等价路径是注册缺失即抛
// ImplementedException, Go 侧以可用性优先, 记一条警告).
func lookupNetPacket(netType core.NetType) netpacket.AbstractNetPacket {
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
func (r *RwHps) NetType() core.NetType { return r.netType }

// TypeConnect 实现 core.IRwHps: 返回连接解析器工厂
func (r *RwHps) TypeConnect() core.TypeConnect { return r.typeConnect }

// AbstractNetPacket 实现 core.IRwHps: 返回包构造器
func (r *RwHps) AbstractNetPacket() netpacket.AbstractNetPacket { return r.abstractNetPacket }

// Version 对应 Data.SERVER_CORE_VERSION 拼接的解析器版本说明 (诊断用)
func Version() string { return global.ServerCoreVersion + ": 1.0.0" }
