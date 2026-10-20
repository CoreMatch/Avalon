// Package relay 对应 net.rwhps.server.net.netconnectprotocol.internal.relay
// (Kotlin 侧以 @file:JvmName("RelayPacket") @file:JvmMultifileClass 聚合)
package relay

import (
	gameio "rwhps/internal/io"
	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
)

// RelayServerInitInfoPacket 对应 relayServerInitInfoInternalPacket()
//
// 让客户端认为当前连接的是 RELAY 服务器
func RelayServerInitInfoPacket() *packet.Packet {
	o := gameio.NewGameOutputStream()
	o.WriteByte(0)
	// RELAY Version
	o.WriteInt(151)
	// ?
	o.WriteInt(1)
	// ?
	o.WriteBoolean(false)
	return o.CreatePacket(packettype.RELAY_VERSION_INFO)
}

// RelayServerTypePacket 对应 relayServerTypeInternal(msg)
//
// 使玩家客户端弹出输入窗口, 输入的数据以明文返回 (对应 117 -> 118)
func RelayServerTypePacket(msg string) *packet.Packet {
	o := gameio.NewGameOutputStream()
	// 理论上的随机数?
	o.WriteByte(1)
	o.WriteInt(5) // 可能和 -AX 一样
	// Msg
	o.WriteString(msg)
	return o.CreatePacket(packettype.RELAY_117) // -> 118
}

// RelayServerTypeReply 对应 relayServerTypeReplyInternalPacket(packet)
//
// 解析 RelayServerTypePacket 的返回包, 得到玩家输入的明文
func RelayServerTypeReply(p *packet.Packet) string {
	inStream := gameio.NewGameInputStreamFromPacket(p, 0)
	// 跳过前面的无用数据
	inStream.Skip(5)
	// 读取数据并去除首尾空白 (原版 trim { it <= ' ' })
	return trimControl(inStream.ReadString())
}

// trimControl 对应 Kotlin String.trim { it <= ' ' }, 仅裁剪 ASCII 控制字符与空格
func trimControl(s string) string {
	start := 0
	end := len(s)
	for start < end && s[start] <= ' ' {
		start++
	}
	for end > start && s[end-1] <= ' ' {
		end--
	}
	return s[start:end]
}

// FromRelayJumpsToAnotherServerPacket 对应 fromRelayJumpsToAnotherServerInternalPacket(ip)
//
// 使用官方协议跳转到另一服务器, 由客户端主动连接
func FromRelayJumpsToAnotherServerPacket(ip string) *packet.Packet {
	o := gameio.NewGameOutputStream()
	// 包中包含的消息
	o.WriteByte(0)
	// 协议版本? (未知)
	o.WriteInt(3)
	// Debug
	o.WriteBoolean(false)
	// For
	o.WriteInt(1)
	o.WriteString(ip)

	return o.CreatePacket(packettype.PACKET_RECONNECT_TO)
}