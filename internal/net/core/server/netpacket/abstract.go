// Package netpacket 对应 net.rwhps.server.net.core.server.packet
package netpacket

import (
	"rwhps/internal/io/packet"
	"rwhps/internal/struct/list"
)

// AbstractNetPacket 对应 AbstractNetPacket
// 获取包 转换包 理论上全版本通用 但是部分版本需要特殊覆盖实现
type AbstractNetPacket interface {
	// GetSystemMessagePacket 获取系统命名的消息包
	GetSystemMessagePacket(msg string) (*packet.Packet, error)

	// GetChatMessagePacket 发送用户名命名的消息
	GetChatMessagePacket(msg string, sendBy string, team int) (*packet.Packet, error)

	// GetPingPacket Ping
	GetPingPacket(player any) (*packet.Packet, error)

	// GetTickPacket 获取时刻包
	GetTickPacket(tick int) (*packet.Packet, error)

	// GetGameTickCommandPacket 获取时刻包 (单条位移)
	GetGameTickCommandPacket(tick int, cmd *packet.GameCommandPacket) (*packet.Packet, error)

	// GetGameTickCommandsPacket 获取时刻包 (多条位移)
	GetGameTickCommandsPacket(tick int, cmd *list.Seq[*packet.GameCommandPacket]) (*packet.Packet, error)

	// GetPacketServerInfo 获取包中的服务器数据
	GetPacketServerInfo(bytes []byte) (*packet.ServerInfoPacket, error)

	// GetDeceiveGameSave 欺骗客户端获取同步包
	GetDeceiveGameSave() *packet.Packet

	// GameSummonPacket 获取单位生成包
	GameSummonPacket(index int, unit string, x, y float32, size int) *packet.GameCommandPacket

	// GetExitPacket 退出
	GetExitPacket() (*packet.Packet, error)
}
