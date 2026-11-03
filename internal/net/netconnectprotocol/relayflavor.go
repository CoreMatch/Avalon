package netconnectprotocol

import (
	"rwhps/internal/io/packet"
	"rwhps/internal/net/netconnectprotocol/internal/relay"
)

// 本文件是 internal/relay 的对外桥.
//
// Go 的 internal 包规则只允许 netconnectprotocol 子树内的代码直接导入
// internal/relay; 而服务端协议实现体 (net/gameserver) 的 Relay flavour 方法
// (对应上游 GameVersionServer.sendRelayServerType / sendRelayServerTypeReply)
// 需要这两个构造器. 在父包开一层薄转发, 避免重复实现已有的包布局.

// RelayServerTypePacket 对应 relayServerTypeInternal(msg): 117 包
func RelayServerTypePacket(msg string) *packet.Packet { return relay.RelayServerTypePacket(msg) }

// RelayServerTypeReply 对应 relayServerTypeReplyInternalPacket(packet): 解析 118 包
func RelayServerTypeReply(p *packet.Packet) string { return relay.RelayServerTypeReply(p) }
