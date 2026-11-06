package core

// DataPermissionStatus 对应 DataPermissionStatus (安全认证 / Permission Connect Status)
//
// 原版是一个 object, 里面只放三个枚举, 用来标记一条连接当前走到了认证流程的哪一步.
// Go 没有 object, 这里把三个枚举直接提到包级.
//
// 注意: 状态机本身由 TypeConnect 的具体实现推进 (RwHps / TypeRelay),
// 这里只提供状态的命名与顺序, 不实现任何跳转逻辑.

// RelayStatus 对应 DataPermissionStatus.RelayStatus: Relay 连接的认证阶段
type RelayStatus int

const (
	// RelayInitialConnection 链接初始化
	RelayInitialConnection RelayStatus = iota
	// RelayGetPlayerInfo 获取链接 UUID-Hex
	RelayGetPlayerInfo
	// RelayWaitCertified 等待认证(Pow)
	RelayWaitCertified
	// RelayCertifiedEnd 认证(Pow)结束
	RelayCertifiedEnd
	// RelayPlayerPermission 向对应房主注册, 但还未进行游戏注册
	RelayPlayerPermission
	// RelayPlayerJoinPermission 向对应房主完成注册, 且游戏完成注册
	RelayPlayerJoinPermission
	// RelayHostPermission 该链接为房间 HOST
	RelayHostPermission
	// RelayDebug 调试
	RelayDebug
)

// String 返回枚举名 (对应 Kotlin enum.name)
func (s RelayStatus) String() string {
	switch s {
	case RelayInitialConnection:
		return "InitialConnection"
	case RelayGetPlayerInfo:
		return "GetPlayerInfo"
	case RelayWaitCertified:
		return "WaitCertified"
	case RelayCertifiedEnd:
		return "CertifiedEnd"
	case RelayPlayerPermission:
		return "PlayerPermission"
	case RelayPlayerJoinPermission:
		return "PlayerJoinPermission"
	case RelayHostPermission:
		return "HostPermission"
	case RelayDebug:
		return "Debug"
	default:
		return "InitialConnection"
	}
}

// ServerStatus 对应 DataPermissionStatus.ServerStatus: 服务端连接的认证阶段
type ServerStatus int

const (
	// ServerInitialConnection 链接初始化
	ServerInitialConnection ServerStatus = iota
	// ServerCertifiedEnd 认证结束
	ServerCertifiedEnd
)

// String 返回枚举名
func (s ServerStatus) String() string {
	switch s {
	case ServerCertifiedEnd:
		return "CertifiedEnd"
	default:
		return "InitialConnection"
	}
}

// DevConnectStatus 对应 DataPermissionStatus.DevConnectStatus: 专用后端的认证阶段
type DevConnectStatus int

const (
	// DevInitialConnection 链接初始化
	DevInitialConnection DevConnectStatus = iota
	// DevWaitCertified 等待认证(Pow)
	DevWaitCertified
	// DevCertifiedEnd 认证结束
	DevCertifiedEnd
	// DevMaster 主连接
	DevMaster
)

// String 返回枚举名
func (s DevConnectStatus) String() string {
	switch s {
	case DevWaitCertified:
		return "WaitCertified"
	case DevCertifiedEnd:
		return "CertifiedEnd"
	case DevMaster:
		return "Master"
	default:
		return "InitialConnection"
	}
}
