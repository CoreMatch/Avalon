// Package server 对应 net.rwhps.server.net.netconnectprotocol.internal.server
// (Kotlin 侧以 @file:JvmName("ServerPacket") @file:JvmMultifileClass 聚合, 此处按函数语义拆分文件)
package server

import (
	gameio "rwhps/internal/io"
	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
	"rwhps/internal/struct/list"
	"rwhps/internal/util/compression"
)

// GameTickPacket 对应 gameTickPacketInternal(tick)
//
// 仅更新 Tick, 不携带按键包
func GameTickPacket(tick int32) *packet.Packet {
	o := gameio.NewGameOutputStream()
	// Game TICK
	o.WriteInt(tick)
	// Readable key pack length
	// 因为没有按键包, 此包仅让客户端更新 Tick
	o.WriteInt(0)
	return o.CreatePacket(packettype.TICK)
}

// GameTickCommandPacket 对应 gameTickCommandPacketInternal(tick, cmd)
//
// 指定 Tick 下向玩家发送单个按键包 (单位行动包)
func GameTickCommandPacket(tick int32, cmd *packet.GameCommandPacket) *packet.Packet {
	o := gameio.NewGameOutputStream()
	// Game TICK
	o.WriteInt(tick)
	// Readable key pack length
	o.WriteInt(1)

	writeCommand(o, cmd)
	return o.CreatePacket(packettype.TICK)
}

// GameTickCommandsPacket 对应 gameTickCommandsPacketInternal(tick, cmd)
//
// 指定 Tick 下向玩家发送多个按键包 (单位行动包)
func GameTickCommandsPacket(tick int32, cmd *list.Seq[*packet.GameCommandPacket]) *packet.Packet {
	o := gameio.NewGameOutputStream()
	// Game TICK
	o.WriteInt(tick)
	// Readable key pack length
	o.WriteInt(int32(cmd.Size()))
	cmd.EachAll(func(c *packet.GameCommandPacket) {
		writeCommand(o, c)
	})
	return o.CreatePacket(packettype.TICK)
}

// writeCommand 处理单条命令: gzip 时按 flushEncodeData(CompressOutputStream(head="c", isGzip=true)) 编码
func writeCommand(o *gameio.GameOutputStream, c *packet.GameCommandPacket) {
	if c.Gzip {
		o.FlushEncodeDataFromCompress("c", compression.GzipEncode(c.Bytes))
	} else {
		o.WriteBytes(c.Bytes)
	}
}

// ChatMessagePacket 对应 chatMessagePacketInternal(msg, sendBy, team)
//
// 向玩家发送聊天消息
func ChatMessagePacket(msg string, sendBy string, team int32) *packet.Packet {
	o := gameio.NewGameOutputStream()
	// 包中包含的消息
	o.WriteString(msg)
	// 协议版本 (旧版本此处依次为 0 1 2 3)
	o.WriteByte(3)
	// 发送者名称是否为空并写入数据
	o.WriteIsString(sendBy)
	// Team
	o.WriteInt(team)
	// Team
	o.WriteInt(team)
	return o.CreatePacket(packettype.CHAT)
}

// ChatUserMessagePacket 对应 chatUserMessagePacketInternal(msg)
//
// 模拟玩家发送消息
func ChatUserMessagePacket(msg string) *packet.Packet {
	o := gameio.NewGameOutputStream()
	// 包中包含的消息
	o.WriteString(msg)
	o.WriteByte(0)
	return o.CreatePacket(packettype.CHAT_RECEIVE)
}

// PlayerExitPacket 对应 playerExitInternalPacket(cause)
//
// 玩家退出服务器
func PlayerExitPacket(cause string) *packet.Packet {
	o := gameio.NewGameOutputStream()
	o.WriteString(cause)
	return o.CreatePacket(packettype.DISCONNECT)
}

// ReceivingStatusPacket 对应 receivingStatusInternalPacket(receiveSize, allSize)
//
// 数据包接收进度
func ReceivingStatusPacket(receiveSize int32, allSize int32) *packet.Packet {
	o := gameio.NewGameOutputStream()
	o.WriteByte(0)
	o.WriteInt(receiveSize)
	o.WriteInt(allSize)
	return o.CreatePacket(packettype.PACKET_DOWNLOAD_PENDING)
}