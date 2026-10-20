// Package realize 对应 net.rwhps.server.net.netconnectprotocol.realize
package realize

import (
	gameio "rwhps/internal/io"
	"rwhps/internal/data/global"
	"rwhps/internal/game/enums"
	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
	"rwhps/internal/net/core/server/netpacket"
	"rwhps/internal/net/netconnectprotocol/internal/server"
	"rwhps/internal/struct/list"
	"rwhps/internal/util/compression"
)

// 编译期断言
var _ netpacket.AbstractNetPacket = (*GameVersionPacket)(nil)

// GameVersionPacket 对应 realize.GameVersionPacket
// 提供对服务器最常用包的支持
type GameVersionPacket struct{}

// NewGameVersionPacket 创建实例
func NewGameVersionPacket() *GameVersionPacket { return &GameVersionPacket{} }

// GetSystemMessagePacket 对应 getSystemMessagePacket(msg)
// 使用 chatMessagePacketInternal(msg, "SERVER", 5)
func (g *GameVersionPacket) GetSystemMessagePacket(msg string) (*packet.Packet, error) {
	return server.ChatMessagePacket(msg, "SERVER", 5), nil
}

// GetChatMessagePacket 对应 getChatMessagePacket(msg, sendBy, team)
func (g *GameVersionPacket) GetChatMessagePacket(msg, sendBy string, team int) (*packet.Packet, error) {
	return server.ChatMessagePacket(msg, sendBy, int32(team)), nil
}

// GetPingPacket 对应 getPingPacket(player)
// Kotlin: player.timeTemp = System.currentTimeMillis(); writeLong(0) + writeByte(0) → HEART_BEAT
// 注意: 玩家时间戳由具体 PlayerHess 实现自行更新 (Todo #8)
func (g *GameVersionPacket) GetPingPacket(_ any) (*packet.Packet, error) {
	o := gameio.NewGameOutputStream()
	o.WriteLong(0)
	o.WriteByte(0)
	return o.CreatePacket(packettype.HEART_BEAT), nil
}

// GetTickPacket 对应 getTickPacket(tick)
func (g *GameVersionPacket) GetTickPacket(tick int) (*packet.Packet, error) {
	return server.GameTickPacket(int32(tick)), nil
}

// GetGameTickCommandPacket 对应 getGameTickCommandPacket(tick, cmd)
func (g *GameVersionPacket) GetGameTickCommandPacket(tick int, cmd *packet.GameCommandPacket) (*packet.Packet, error) {
	return server.GameTickCommandPacket(int32(tick), cmd), nil
}

// GetGameTickCommandsPacket 对应 getGameTickCommandsPacket(tick, cmd)
func (g *GameVersionPacket) GetGameTickCommandsPacket(tick int, cmd *list.Seq[*packet.GameCommandPacket]) (*packet.Packet, error) {
	return server.GameTickCommandsPacket(int32(tick), cmd), nil
}

// GetPacketServerInfo 对应 getPacketServerInfo(bytes)
func (g *GameVersionPacket) GetPacketServerInfo(bytes []byte) (*packet.ServerInfoPacket, error) {
	return gameio.NewServerInfoPacket(bytes)
}

// GetDeceiveGameSave 对应 getDeceiveGameSave
// 使用 Cache.packetCache 缓存首次创建的 SYNC 包
func (g *GameVersionPacket) GetDeceiveGameSave() *packet.Packet {
	return global.PacketCacheInstance.GetOrCreate("getDeceiveGameSave", g.buildDeceiveGameSave)
}

// buildDeceiveGameSave 首次创建欺骗 GameSave 包 (用于 SYNC).
//
// 内容: writeByte(0) + writeInt(0) + writeInt(0) + writeFloat(0) + writeFloat(0) +
//
//	writeBoolean(true) + writeBoolean(false) + gzip("gameSave", "This is RW-HPS [Deceive Get GameSave]!") → SYNC
func (g *GameVersionPacket) buildDeceiveGameSave() *packet.Packet {
	o := gameio.NewGameOutputStream()
	o.WriteByte(0)
	o.WriteInt(0)
	o.WriteInt(0)
	o.WriteFloat(0)
	o.WriteFloat(0)
	o.WriteBoolean(true)
	o.WriteBoolean(false)

	// 模拟 CompressOutputStream.getGzipOutputStream("gameSave", isGzip=false) + writeString
	//   getByteArray() 把字符串以 Modified UTF-8 写入, 然后 gzip 压缩.
	strBytes := []byte("This is RW-HPS [Deceive Get GameSave]!")
	mutfStr := string(strBytes) // Kotlin CompressOutputStream 默认编码 UTF-8
	// 直接 gzip 字符串字节
	gz := compression.GzipEncode([]byte(mutfStr))
	o.FlushEncodeDataFromCompress("gameSave", gz)

	return o.CreatePacket(packettype.SYNC)
}

// GameSummonPacket 对应 gameSummonPacket(index, unit, x, y, size)
func (g *GameVersionPacket) GameSummonPacket(index int, unit string, x, y float32, size int) *packet.GameCommandPacket {
	o := gameio.NewGameOutputStream()
	o.WriteByte(byte(index))
	o.WriteBoolean(true)
	// 建造
	o.WriteInt(int32(enums.ActionBuild.Ordinal()))

	var unitID int32 = -2
	if !isNumeric(unit) {
		for _, u := range enums.UnitAll() {
			if asciiEqIgnoreCase(u.Name(), unit) {
				unitID = int32(u.Ordinal())
				goto doneLookup
			}
		}
	} else {
		v := atoi(unit)
		unitID = int32(v)
	}
doneLookup:

	o.WriteInt(unitID)

	if unitID == -2 {
		o.WriteString(unit)
	}

	// X
	o.WriteFloat(x)
	// Y
	o.WriteFloat(y)
	// Tager
	o.WriteLong(-1)
	// 如果生成圆, 那么这就是半径
	o.WriteByte(byte(size))
	o.WriteFloat(1)
	o.WriteFloat(1)
	o.WriteBoolean(false)
	o.WriteBoolean(false)
	o.WriteBoolean(false)
	o.WriteBoolean(false)
	o.WriteBoolean(false)
	o.WriteBoolean(false)

	o.WriteInt(-1)
	o.WriteInt(-1)

	o.WriteBoolean(false)
	o.WriteBoolean(false)

	o.WriteInt(0)

	o.WriteBoolean(false)
	o.WriteBoolean(false)

	o.WriteLong(-1)
	o.WriteString("-1")
	o.WriteBoolean(false)
	o.WriteShort(0)
	// System action
	o.WriteBoolean(true)
	o.WriteByte(0)
	o.WriteFloat(0)
	o.WriteFloat(0)
	// action type
	o.WriteInt(5)
	o.WriteInt(0)
	o.WriteBoolean(false)
	return packet.NewGameCommandPacket(index, o.GetByteArray(), false)
}

// GetExitPacket 对应 getExitPacket (缓存的 playerExitInternalPacket)
func (g *GameVersionPacket) GetExitPacket() (*packet.Packet, error) {
	return global.PacketCacheInstance.GetOrCreate("getExitPacket", func() *packet.Packet {
		return server.PlayerExitPacket("exited")
	}), nil
}

// asciiEqIgnoreCase 对应 Kotlin String.equals(other, ignoreCase=true)
func asciiEqIgnoreCase(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// isNumeric 对应 Kotlin IsUtils.notIsNumeric —— 返回字符串是否是纯数字
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// atoi 简单 int 解析, 错误返回 0
func atoi(s string) int {
	if s == "" {
		return 0
	}
	v := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0
		}
		v = v*10 + int(c-'0')
	}
	return v
}