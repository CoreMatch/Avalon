package core

import (
	"errors"

	netpacket "rwhps/internal/net/core/server/netpacket"
)

// IRwHps 对应 IRwHps 接口
type IRwHps interface {
	// NetType 当前 IRW-HPS 使用的协议
	NetType() NetType
	// TypeConnect 服务器的协议, 主要提供包的解析功能
	TypeConnect() TypeConnect
	// AbstractNetPacket 服务器 Packet 协议
	AbstractNetPacket() netpacket.AbstractNetPacket
}

// ErrNotImplemented 对应 ImplementedException("Should not be used")
var ErrNotImplemented = errors.New("should not be used")

// NullRwHps 对应 FakeRwHps(classLoader, NullProtocol)
type NullRwHps struct {
	netType NetType
}

// NewNullRwHps 对应 FakeRwHps(classLoader, netType)
func NewNullRwHps(netType NetType) *NullRwHps {
	return &NullRwHps{netType: netType}
}

// NetType 实现 IRwHps
func (n *NullRwHps) NetType() NetType { return n.netType }

// TypeConnect 实现 IRwHps
func (n *NullRwHps) TypeConnect() TypeConnect { return nil }

// AbstractNetPacket 实现 IRwHps
func (n *NullRwHps) AbstractNetPacket() netpacket.AbstractNetPacket { return nil }
