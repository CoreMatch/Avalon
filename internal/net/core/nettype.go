// Package core 对应 net.rwhps.server.net.core
package core

// NetType 对应 IRwHps.NetType
type NetType int

const (
	// ServerProtocol (默认) Server协议 原汁原味服务器实现
	ServerProtocol NetType = iota
	// ServerProtocolOld Server旧协议 提供过去版本的服务器实现
	ServerProtocolOld
	// ServerTestProtocol Server协议 Server服务器实现 加入测试功能
	ServerTestProtocol
	// RelayProtocol Relay协议 普通RELAY实现
	RelayProtocol
	// RelayMulticastProtocol Relay协议 多播RELAY实现
	RelayMulticastProtocol
	// DedicatedToTheBackend 专用后端
	DedicatedToTheBackend
	// HttpProtocol HTTP Service
	HttpProtocol
	// RemoteControlProtocol RemoteControl (RCON) Service
	RemoteControlProtocol
	// GlobalProtocol 全局使用
	GlobalProtocol
	// GameProtocol 非公开使用
	GameProtocol
	// NullProtocol 无实现 找不到对应实现
	NullProtocol
)

var netTypeNames = [...]string{
	"ServerProtocol",
	"ServerProtocolOld",
	"ServerTestProtocol",
	"RelayProtocol",
	"RelayMulticastProtocol",
	"DedicatedToTheBackend",
	"HttpProtocol",
	"RemoteControlProtocol",
	"GlobalProtocol",
	"GameProtocol",
	"NullProtocol",
}

// String 对应枚举名
func (n NetType) String() string {
	if n >= 0 && int(n) < len(netTypeNames) {
		return netTypeNames[n]
	}
	return "NullProtocol"
}

// NetTypeFrom 对应 IRwHps.NetType.from(type), 找不到返回 NullProtocol
func NetTypeFrom(typeName string) NetType {
	for i, name := range netTypeNames {
		if name == typeName {
			return NetType(i)
		}
	}
	return NullProtocol
}
