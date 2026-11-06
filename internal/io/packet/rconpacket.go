// Package packet - RCON 协议包 (对应 io.packet.RconPacket)
package packet

// RconPacket 对应 RconPacket(Source RCON 客户端请求/响应封装)
type RconPacket struct {
	// ID 请求/响应 ID (密码错误时服务端置 -1)
	ID int
	// Type 请求类型 (见 RconConstant)
	Type int
	// Body 请求/响应体
	Body string
}

// NewRconPacket 构造 RconPacket
func NewRconPacket(id, t int, body string) *RconPacket {
	return &RconPacket{ID: id, Type: t, Body: body}
}

// RconConstant 对应 RconPacket.RconConstant
// 常量数值与原版 Source RCON 协议完全一致.
type RconConstant int

const (
	// RconServerdataResponseValue 普通命令响应
	RconServerdataResponseValue RconConstant = 0
	// RconServerdataAuthResponse 认证响应
	RconServerdataAuthResponse RconConstant = 2
	// RconServerdataExecCommand 执行命令 (与 RconServerdataAuthResponse 同值, 历史遗留)
	RconServerdataExecCommand RconConstant = 2
	// RconServerdataAuth 认证请求
	RconServerdataAuth RconConstant = 3
)
