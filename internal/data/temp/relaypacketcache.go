// Package temp 对应 net.rwhps.server.data.temp
package temp

import "rwhps/internal/io/packet"

// RelayPacketCache 对应 RelayPacketCache
//
// 原注释只有一行 @PrivateMark, 类里只有一个可空的 startGamePacket.
// 它的唯一消费者是 GameVersionRelay (Relay 开局包的缓存), RelayRoom 只是持有它.
//
// Kotlin 的 Packet? 映射为 *packet.Packet, nil 即 null.
type RelayPacketCache struct {
	// StartGamePacket 对应 startGamePacket
	StartGamePacket *packet.Packet
}

// NewRelayPacketCache 对应 RelayPacketCache(): 初始时 startGamePacket 为 null
func NewRelayPacketCache() *RelayPacketCache {
	return &RelayPacketCache{}
}
