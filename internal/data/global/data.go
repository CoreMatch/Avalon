// Package global 对应 net.rwhps.server.data.global.Data / NetStaticData
package global

import (
	"rwhps/internal/data/bean"
	"rwhps/internal/util/file/load"
	"rwhps/internal/util/game/command"
	"rwhps/internal/util/log"
	"rwhps/internal/util/systemprops"
)

// 服务器路径常量 (与原版一致)
const (
	ServerDataPath            = "/data"
	ServerSavePath            = "/data/saves"
	ServerCachePath           = "/data/cache"
	ServerLibPath             = "/data/libs"
	ServerLogPath             = "/data/log"
	ServerPlayerPath          = "/data/player"
	ServerMapsPath            = "/data/maps"
	ServerPluginsPath         = "/data/plugins"
	PluginGameCoreDataPath    = "/data/gameData"
	PluginModsPath            = "/data/mods"
	PluginRePlaysPath         = "/data/replays"
)

// 插件默认变量
const (
	// ServerID 自定义包名
	ServerID = "net.rwhps.server"
	// ServerIDRelay 自定义 RELAY ID
	ServerIDRelay = "net.rwhps.server.relayCustomMode.Dr"
	// ServerIDRelayGet RELAY 诱骗 UUID-Hex 时的 UUID
	ServerIDRelayGet = "net.rwhps.server.relayGetUUIDHex.Dr"
	// ServerCoreVersion 服务器主版本
	ServerCoreVersion = "3.0.0"
	// ToptKey 服务器 TOPT 密码
	ToptKey = "net.rwhps.server.topt # Dr (dr@der.kim)"
	// ServerRelayUUID RELAY 使用的 UUID
	ServerRelayUUID = "Dr (dr@der.kim) & Tiexiu.xyz Core Team"
	// ServerEulaVersion EULA 版本
	ServerEulaVersion = "1.1.2"

	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36 Edg/114.0.1823.51"

	// DefaultEncoding 服务器默认编码
	DefaultEncoding = "UTF-8"

	HeadlessName = "RW-HPS Core Headless"
)

const LineSeparator = "\n"

// 命令处理器
var (
	// ServerCommand 服务端 服务端命令
	ServerCommand = command.NewHandler("")
	// LogCommand 服务端 Log 命令
	LogCommand = command.NewHandler("!")
	// RelayCommand 服务端 Relay 命令
	RelayCommand = command.NewHandler(".")
	// PingCommand Ping 命令
	PingCommand = command.NewHandler("")
)

// 配置与全局状态
var (
	Config       bean.CoreConfig
	ConfigServer bean.ServerConfig
	ConfigRelay  bean.RelayConfig

	I18NBundle    *load.I18NBundle
	I18NBundleMap = map[string]*load.I18NBundle{}

	// DefPrint 对应 Data.defPrint
	DefPrint = func(obj string) { log.Clog(obj) }

	Vote         any
	RunPid       int64
	StartServer  bool
	ServerCountry = "EN"

	NeverEnd = false
)

// MainParameters 对应 BeanMainParameters
type MainParameters struct {
	NoPrint bool
	NoGui   bool
}

// CreateMainParameters 对应 BeanMainParameters.create
func CreateMainParameters(args []string) MainParameters {
	systemprops.Init(args)
	p := MainParameters{}
	for _, arg := range args {
		switch arg {
		case "-noPrint":
			p.NoPrint = true
		case "-noGui":
			p.NoGui = true
		}
	}
	return p
}

// 全局参数 (由 Main 初始化)
var MainParametersValue MainParameters
