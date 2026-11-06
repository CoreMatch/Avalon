// Package packettype 对应 net.rwhps.server.io.packet.type
package packettype

// AbstractPacketType 对应 AbstractPacketType 接口
type AbstractPacketType interface {
	TypeInt() int
	TypeIntBytes() []byte
	NameEnum() string
	// From 通过 Int 获取新头部
	From(t int) AbstractPacketType
}

// PacketType 对应 PacketType 枚举 (游戏协议号)
type PacketType int

// 协议号定义, 数值与原版完全一致
const (
	// DEBUG
	SERVER_DEBUG_RECEIVE PacketType = 2000
	SERVER_DEBUG         PacketType = 2001

	// Ex
	GET_SERVER_INFO_RECEIVE PacketType = 3000
	GET_SERVER_INFO         PacketType = 3001
	UPDATA_CLASS_RECEIVE    PacketType = 3010
	STATUS_RESULT           PacketType = 3999

	// Preregister
	PREREGISTER_INFO_RECEIVE PacketType = 160
	PREREGISTER_INFO         PacketType = 161
	PASSWD_ERROR             PacketType = 113
	REGISTER_PLAYER          PacketType = 110

	// Server Info
	SERVER_INFO PacketType = 106
	TEAM_LIST   PacketType = 115

	// Heart
	HEART_BEAT          PacketType = 108
	HEART_BEAT_RESPONSE PacketType = 109

	// Chat
	CHAT_RECEIVE PacketType = 140
	CHAT         PacketType = 141

	// Net Status
	PACKET_DOWNLOAD_PENDING PacketType = 4
	KICK                    PacketType = 150
	DISCONNECT              PacketType = 111

	// StartGame
	START_GAME             PacketType = 120
	ACCEPT_START_GAME      PacketType = 112
	RETURN_TO_BATTLEROOM   PacketType = 122

	// GameStart Commands
	TICK                PacketType = 10
	GAMECOMMAND_RECEIVE PacketType = 20
	SYNCCHECKSUM_STATUS PacketType = 31
	SYNC_CHECK          PacketType = 30
	SYNC                PacketType = 35

	// Relay
	RELAY_117                 PacketType = 117
	RELAY_118_117_RETURN      PacketType = 118
	RELAY_POW                 PacketType = 151
	RELAY_POW_RECEIVE         PacketType = 152
	RELAY_VERSION_INFO        PacketType = 163
	RELAY_BECOME_SERVER       PacketType = 170
	FORWARD_CLIENT_ADD        PacketType = 172
	FORWARD_CLIENT_REMOVE     PacketType = 173
	PACKET_FORWARD_CLIENT_FROM PacketType = 174
	PACKET_FORWARD_CLIENT_TO   PacketType = 175
	PACKET_FORWARD_CLIENT_TO_REPEATED PacketType = 176
	PACKET_RECONNECT_TO       PacketType = 178

	EMPTYP_ACKAGE PacketType = 0
	NOT_RESOLVED  PacketType = -1

	// INSTANCE 提供的实例化 本身实际上无用
	INSTANCE PacketType = -2147483648
)

// nameByType 记录枚举名, 用于 NameEnum
var nameByType = map[PacketType]string{
	SERVER_DEBUG_RECEIVE: "SERVER_DEBUG_RECEIVE", SERVER_DEBUG: "SERVER_DEBUG",
	GET_SERVER_INFO_RECEIVE: "GET_SERVER_INFO_RECEIVE", GET_SERVER_INFO: "GET_SERVER_INFO",
	UPDATA_CLASS_RECEIVE: "UPDATA_CLASS_RECEIVE", STATUS_RESULT: "STATUS_RESULT",
	PREREGISTER_INFO_RECEIVE: "PREREGISTER_INFO_RECEIVE", PREREGISTER_INFO: "PREREGISTER_INFO",
	PASSWD_ERROR: "PASSWD_ERROR", REGISTER_PLAYER: "REGISTER_PLAYER",
	SERVER_INFO: "SERVER_INFO", TEAM_LIST: "TEAM_LIST",
	HEART_BEAT: "HEART_BEAT", HEART_BEAT_RESPONSE: "HEART_BEAT_RESPONSE",
	CHAT_RECEIVE: "CHAT_RECEIVE", CHAT: "CHAT",
	PACKET_DOWNLOAD_PENDING: "PACKET_DOWNLOAD_PENDING", KICK: "KICK", DISCONNECT: "DISCONNECT",
	START_GAME: "START_GAME", ACCEPT_START_GAME: "ACCEPT_START_GAME", RETURN_TO_BATTLEROOM: "RETURN_TO_BATTLEROOM",
	TICK: "TICK", GAMECOMMAND_RECEIVE: "GAMECOMMAND_RECEIVE", SYNCCHECKSUM_STATUS: "SYNCCHECKSUM_STATUS",
	SYNC_CHECK: "SYNC_CHECK", SYNC: "SYNC",
	RELAY_117: "RELAY_117", RELAY_118_117_RETURN: "RELAY_118_117_RETURN",
	RELAY_POW: "RELAY_POW", RELAY_POW_RECEIVE: "RELAY_POW_RECEIVE",
	RELAY_VERSION_INFO: "RELAY_VERSION_INFO", RELAY_BECOME_SERVER: "RELAY_BECOME_SERVER",
	FORWARD_CLIENT_ADD: "FORWARD_CLIENT_ADD", FORWARD_CLIENT_REMOVE: "FORWARD_CLIENT_REMOVE",
	PACKET_FORWARD_CLIENT_FROM: "PACKET_FORWARD_CLIENT_FROM", PACKET_FORWARD_CLIENT_TO: "PACKET_FORWARD_CLIENT_TO",
	PACKET_FORWARD_CLIENT_TO_REPEATED: "PACKET_FORWARD_CLIENT_TO_REPEATED", PACKET_RECONNECT_TO: "PACKET_RECONNECT_TO",
	EMPTYP_ACKAGE: "EMPTYP_ACKAGE", NOT_RESOLVED: "NOT_RESOLVED", INSTANCE: "INSTANCE",
}

func (p PacketType) TypeInt() int { return int(p) }

func (p PacketType) TypeIntBytes() []byte {
	return IntToBytes(int(p))
}

func (p PacketType) NameEnum() string {
	if n, ok := nameByType[p]; ok {
		return n
	}
	return "UNKNOWN"
}

// From 通过 Int 获取新头部, 未知返回 NOT_RESOLVED
func (p PacketType) From(t int) AbstractPacketType {
	if _, ok := nameByType[PacketType(t)]; ok {
		return PacketType(t)
	}
	return NOT_RESOLVED
}

// Resolver 对应 IRwHps.packetType, 由 ServiceLoader 注入.
// 默认使用 PacketType 自身.
var Resolver AbstractPacketType = INSTANCE

// IntToBytes 对应 GameOutputStream.intToBytes (大端)
func IntToBytes(value int) []byte {
	return []byte{
		byte(value >> 24 & 0xFF),
		byte(value >> 16 & 0xFF),
		byte(value >> 8 & 0xFF),
		byte(value & 0xFF),
	}
}
