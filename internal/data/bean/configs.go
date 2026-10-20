package bean

import (
	"os"

	"rwhps/internal/util/file"
	"rwhps/internal/util/log"
	"rwhps/internal/util/mathutil"
)

// CoreConfig 对应 BeanCoreConfig
type CoreConfig struct {
	NoteEnglish string `json:"noteEnglish"`
	NoteChina   string `json:"noteChina"`

	// DefStartCommand 默认启动命令
	DefStartCommand string `json:"defStartCommand"`
	Log             string `json:"log"`
	CmdTitle        string `json:"cmdTitle"`

	// FollowBetaVersion 更新是否使用测试版本
	FollowBetaVersion bool `json:"followBetaVersion"`

	// Port 端口
	Port int `json:"port"`

	// ServerName 服务器名称
	ServerName string `json:"serverName"`
	// Subtitle 标题, 留空使用地图名
	Subtitle string `json:"subtitle"`
	// AutoUpList 启动后自动 UPLIST
	AutoUpList bool `json:"autoUpList"`

	// IpCheckMultiLanguageSupport ip多语言支持
	IpCheckMultiLanguageSupport bool `json:"ipCheckMultiLanguageSupport"`

	// SingleUserRelay 单用户 relay 禁用弹窗选择
	SingleUserRelay bool `json:"singleUserRelay"`
	// SingleUserRelayMod 单用户 relay 默认 mods 配置
	SingleUserRelayMod bool `json:"singleUserRelayMod"`

	// MixProtocolEnable 混合协议监听
	MixProtocolEnable bool `json:"mixProtocolEnable"`
	RconMixEnable     bool `json:"rconMixEnable"`
	WebMixEnable      bool `json:"webMixEnable"`

	RconPort   int    `json:"rconPort"`
	RconPasswd string `json:"rconPasswd"`

	// WebPort 不为 0 时在对应端口启用一个 HTTP 服务
	WebPort int `json:"webPort"`
	// WebToken HTTP 鉴权
	WebToken string `json:"webToken"`
	// WebHOST 限制
	WebHOST string `json:"webHOST"`
	// SslEnable 启用 SSL
	SslEnable bool `json:"sslEnable"`
	// SslMixEnable 混合协议下 SSL 同端口
	SslMixEnable bool `json:"sslMixEnable"`
	// SslPasswd SSL 密码
	SslPasswd string `json:"sslPasswd"`

	RunPid int64 `json:"runPid"`
}

// NewCoreConfig 创建带默认值的核心配置
func NewCoreConfig() CoreConfig {
	return CoreConfig{
		NoteEnglish: "Different protocols use different configuration files, please go to the corresponding file ConfigServer/ConfigRelay",
		NoteChina:   "不同协议使用的配置文件不同, 请自行前往对应文件 ConfigServer/ConfigRelay",

		DefStartCommand: "start",
		Log:             "WARN",
		CmdTitle:        "",
		FollowBetaVersion: false,
		Port:            5123,
		ServerName:      "RW-HPS",
		Subtitle:        "",
		AutoUpList:      false,

		IpCheckMultiLanguageSupport: true,

		SingleUserRelay:    false,
		SingleUserRelayMod: false,

		MixProtocolEnable: false,
		RconMixEnable:     false,
		WebMixEnable:      false,

		RconPort:   0,
		RconPasswd: mathutil.GetRandomLetterString(10),

		WebPort:  0,
		WebToken: mathutil.GetRandomLetterString(10),
		WebHOST:  "",
		SslEnable:    false,
		SslMixEnable: false,
		SslPasswd:    "RW-HPS",

		RunPid: 0,
	}
}

// CoreConfigFile 对应 BeanCoreConfig.fileUtils
func CoreConfigFile() *file.FileUtils {
	return file.GetFolder("/data").ToFile("Config.json")
}

// SaveCoreConfig 保存 (写入当前 pid)
func SaveCoreConfig(c CoreConfig) {
	c.RunPid = int64(os.Getpid())
	Save(c, CoreConfigFile())
}

// StringToCoreConfig 对应 BeanCoreConfig.stringToClass()
func StringToCoreConfig() CoreConfig {
	c := LoadOrInit(CoreConfigFile(), NewCoreConfig())
	ReadProperty(&c, "rwhps.config.core", CoreConfigFile())
	return c
}

// ServerConfig 对应 BeanServerConfig
type ServerConfig struct {
	// ServerID 服务器ID
	ServerID string `json:"serverID"`

	EnterAd          string `json:"enterAd"`
	StartAd          string `json:"startAd"`
	MaxPlayerJoinAd  string `json:"maxPlayerJoinAd"`
	StartPlayerJoinAd string `json:"startPlayerJoinAd"`

	// Passwd 密码
	Passwd string `json:"passwd"`

	// MaxPlayer 服务器最大人数
	MaxPlayer int `json:"maxPlayer"`
	// OneAdmin only Admin (Auto)
	OneAdmin bool `json:"oneAdmin"`

	// StartMinPlayerSize 最小 Start 人数 (-1 禁用)
	StartMinPlayerSize int `json:"startMinPlayerSize"`
	// AutoStartMinPlayerSize 最小自动开始人数 (-1 禁用)
	AutoStartMinPlayerSize int `json:"autoStartMinPlayerSize"`
	// MaxGameIngTime 最大游戏时间 s (-1 禁用)
	MaxGameIngTime int `json:"maxGameIngTime"`
	// MaxOnlyAIGameIngTime 最大仅AI游戏时间 s (-1 禁用)
	MaxOnlyAIGameIngTime int `json:"maxOnlyAIGameIngTime"`

	EnableAI bool `json:"enableAI"`

	// MaxMessageLen 最大发言长度
	MaxMessageLen int `json:"maxMessageLen"`
	// MaxUnit 最大单位数
	MaxUnit int `json:"maxUnit"`
	// DefIncome 默认倍率
	DefIncome float32 `json:"defIncome"`

	IsAfk   bool `json:"isAfk"`
	MuteAll bool `json:"muteAll"`

	// TurnStoneIntoGold 点石成金
	TurnStoneIntoGold bool `json:"turnStoneIntoGold"`

	// ModsLoadErrorPrint Mod 加载错误信息
	ModsLoadErrorPrint bool `json:"modsLoadErrorPrint"`

	// SaveRePlayFile 是否保存 RePlay
	SaveRePlayFile bool `json:"saveRePlayFile"`
}

// NewServerConfig 创建带默认值的 Server 配置
func NewServerConfig() ServerConfig {
	return ServerConfig{
		ServerID: mathutil.GetRandomLetterString(5),

		EnterAd:           "",
		StartAd:           "",
		MaxPlayerJoinAd:   "",
		StartPlayerJoinAd: "",

		Passwd: "",

		MaxPlayer: 10,
		OneAdmin:  true,

		StartMinPlayerSize:     -1,
		AutoStartMinPlayerSize: 4,
		MaxGameIngTime:         7200,
		MaxOnlyAIGameIngTime:   3600,

		EnableAI: false,

		MaxMessageLen: 40,
		MaxUnit:       200,
		DefIncome:     1.0,

		IsAfk:   true,
		MuteAll: false,

		TurnStoneIntoGold: false,

		ModsLoadErrorPrint: false,

		SaveRePlayFile: true,
	}
}

// ServerConfigFile 对应 BeanServerConfig.fileUtils
func ServerConfigFile() *file.FileUtils {
	return file.GetFolder("/data").ToFile("ConfigServer.json")
}

// StringToServerConfig 对应 BeanServerConfig.stringToClass()
func StringToServerConfig() ServerConfig {
	c := LoadOrInit(ServerConfigFile(), NewServerConfig())
	ReadProperty(&c, "rwhps.config.server", ServerConfigFile())
	c.checkValue()
	return c
}

func (c *ServerConfig) checkValue() {
	if c.MaxPlayer < c.StartMinPlayerSize {
		log.Warn("MaxPlayer < StartMinPlayerSize , Reset !")
		CoverField(c, "startMinPlayerSize", 0)
	}
}

// RelayConfig 对应 BeanRelayConfig
type RelayConfig struct {
	MainID        string `json:"mainID"`
	MainServer    bool   `json:"mainServer"`
	UpList        bool   `json:"upList"`
	MainServerIP  string `json:"mainServerIP"`
	MainServerPort int   `json:"mainServerPort"`
}

// NewRelayConfig 创建带默认值的 Relay 配置
func NewRelayConfig() RelayConfig {
	return RelayConfig{
		MainID:         "R",
		MainServer:     true,
		UpList:         true,
		MainServerIP:   "relay.der.kim",
		MainServerPort: 4993,
	}
}

// RelayConfigFile 对应 BeanRelayConfig.fileUtils
func RelayConfigFile() *file.FileUtils {
	return file.GetFolder("/data").ToFile("RelayConfig.json")
}

// StringToRelayConfig 对应 BeanRelayConfig.stringToClass()
func StringToRelayConfig() RelayConfig {
	c := LoadOrInit(RelayConfigFile(), NewRelayConfig())
	ReadProperty(&c, "rwhps.config.relay", RelayConfigFile())
	return c
}
