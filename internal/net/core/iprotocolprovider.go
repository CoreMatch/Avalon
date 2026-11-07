package core

import (
	"errors"

	netpacket "github.com/CoreMatch/Avalon/internal/net/core/server/netpacket"
)

// IProtocolProvider 对应 IProtocolProvider 接口
type IProtocolProvider interface {
	// NetType 当前 IRW-HPS 使用的协议
	NetType() NetType
	// TypeConnect 服务器的协议, 主要提供包的解析功能
	TypeConnect() TypeConnect
	// AbstractNetPacket 服务器 Packet 协议
	AbstractNetPacket() netpacket.AbstractNetPacket
}

// ErrNotImplemented 对应 ImplementedException("Should not be used")
var ErrNotImplemented = errors.New("should not be used")

// NullProtocolProvider 对应 FakeProtocolProvider(classLoader, NullProtocol)
type NullProtocolProvider struct {
	netType NetType
}

// NewNullProtocolProvider 对应 FakeProtocolProvider(classLoader, netType)
func NewNullProtocolProvider(netType NetType) *NullProtocolProvider {
	return &NullProtocolProvider{netType: netType}
}

// NetType 实现 IProtocolProvider
func (n *NullProtocolProvider) NetType() NetType { return n.netType }

// TypeConnect 实现 IProtocolProvider
func (n *NullProtocolProvider) TypeConnect() TypeConnect { return nil }

// AbstractNetPacket 实现 IProtocolProvider
func (n *NullProtocolProvider) AbstractNetPacket() netpacket.AbstractNetPacket { return nil }
