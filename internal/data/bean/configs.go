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

	// Port 端口
	Port int `json:"port"`

	// ServerName 服务器名称
	ServerName string `json:"serverName"`
	// Subtitle 标题, 留空使用地图名
	Subtitle string `json:"subtitle"`
	// AutoUpList 启动后自动 UPLIST
	AutoUpList bool `json:"autoUpList"`

	// UplistEnable UPLIST 自报总开关 (Go 版新增, 上游没有; DECISIONS D-22).
	// false 时 uplist 控制台命令 / toup,tonp 客户端命令 / AutoUpList 全部停用.
	// 默认 true = 保持自报, 且与 CascadeEnable 无关: 核实 (2026-11-04,
	// /tmp/cascade-ro @ f94f511) Cascade 只实现了向 HRPAuth 的 HA presence
	// 服务注册 (服务发现/转发, 与玩家服务器列表无关), 没有向 RW 官方列表服
	// (gs1/gs4 masterserver) 上报的能力; 在有人以 Cascade 公网地址接管这条
	// 上报链路之前, 关闭自报会让玩家在游戏内服务器列表看不到本服.
	UplistEnable bool `json:"uplistEnable"`

	// MixProtocolEnable 混合协议监听
	MixProtocolEnable bool `json:"mixProtocolEnable"`
	RconMixEnable     bool `json:"rconMixEnable"`
	WebMixEnable      bool `json:"webMixEnable"`

	// WebPort 不为 0 时在对应端口启用一个 HTTP 服务
	WebPort int `json:"webPort"`
	// WebToken HTTP 鉴权
	WebToken string `json:"webToken"`
	// WebHOST 限制
	WebHOST string `json:"webHOST"`

	RunPid int64 `json:"runPid"`

	// 已删除的死配置 (2026-11-04, 作者批准的清理; 对应上游功能未移植,
	// 全仓库零运行时引用): cmdTitle (控制台标题), followBetaVersion
	// (更新系统), ipCheckMultiLanguageSupport, singleUserRelay /
	// singleUserRelayMod (relay 弹窗), rconPort / rconPasswd (RCON),
	// sslEnable / sslMixEnable / sslPasswd (SSL). 旧 Config.json 里的
	// 这些键在载入时被忽略, 下次保存自动消失.

	// ---- 以下三项是 Go 版新增, 原版没有对应字段 ----
	//
	// 原版不需要它们: 它自己就跑在 JVM 里, 直接用 ASM 改写 rusted-warfare.jar
	// 的字节码拿到游戏内核. Go 版做不到这一点, 只能把内核放到一个外部 Java
	// 进程里, 于是需要有人告诉 Go "java 在哪、helper 在哪、带什么参数".
	//
	// 全部留空 = 不启用外部依赖, 服务器行为与不配置时完全一致 (开局返回
	// game.ErrNotImplemented), 见 app.loadSimulation.

	// JavaPath java 可执行文件; 空表示按 PATH 里的 "java" 处理
	JavaPath string `json:"javaPath"`
	// GameHelperPath 外部游戏内核的入口 (jar 路径或主类名), 必填才启用
	GameHelperPath string `json:"gameHelperPath"`
	// GameHelperArgs 追加给 java 的参数, 例如 -Xmx512m
	GameHelperArgs []string `json:"gameHelperArgs"`

	// ---- 以下字段是 Go 版新增的第二批: Cascade 边缘代理后端契约 ----
	//
	// 有意修正（作者已批准，2026-11-04）: Go Server 作为 Cascade
	// (CoreMatch/Cascade, 契约 Cascade-Backend-v1) 的后端, 方案 B 单房间合规体
	// (DECISIONS D-17~D-21). 全部留空/关闭 = 不启用, 行为与现网完全一致.

	// CascadeEnable 启用 Cascade 网关模式: 游戏端口改为 CASC 前导 + 会话门控,
	// 并另起 /v1 控制面. 关闭时保持明文 TCP 直连.
	CascadeEnable bool `json:"cascadeEnable"`
	// CascadeBackendID 后端 ID: JWT 的 aud, health/rooms 应答的 backendId
	CascadeBackendID string `json:"cascadeBackendId"`
	// CascadeRoomCode 本后端对 Cascade 暴露的唯一房间码; 空回落 ConfigServer.ServerID
	CascadeRoomCode string `json:"cascadeRoomCode"`

	// ---- 信任三档 (有意修正, 作者已批准, 2026-11-04; DECISIONS D-24) ----
	// 作者定稿: "默认就是裸奔". 对端身份不再靠 mTLS 强校验与 JWT 验签,
	// 而是三档可选约束:
	//   open   (默认, 留空同义): 接受任何来源, 不校验客户端证书与 JWT 签名,
	//          仅记录来源 IP 并告警; 游戏端口严禁暴露公网.
	//   ip     : 只接受 trustedCascadeIPs 内的来源, 仍不校验 JWT 签名.
	//   pinned : 在 IP 约束之上校验 TLS 客户端证书指纹 (SHA-256 证书 DER),
	//          拒绝不匹配的连接.

	// CascadeTrustMode 信任档位: "" 或 "open" / "ip" / "pinned"
	CascadeTrustMode string `json:"cascadeTrustMode"`
	// TrustedCascadeIPs ip/pinned 档的来源白名单 (精确 IP 或 CIDR);
	// ip 档必填 (空名单 = 启动报错, 不静默锁死也不静默放行)
	TrustedCascadeIPs []string `json:"trustedCascadeIPs"`
	// CascadePinnedFingerprints pinned 档的客户端证书指纹列表
	// (SHA-256(证书 DER) 十六进制); 列表用于轮换重叠期新旧并存.
	// 指纹方案选型评估见 DECISIONS D-24 (选 TLS 客户端证书指纹,
	// Ed25519 公钥指纹列后续项).
	CascadePinnedFingerprints []string `json:"cascadePinnedFingerprints"`

	// CascadeTLSCert / CascadeTLSKey 服务端证书/私钥 (PEM), 游戏口与控制面共用.
	// 配置后传输走 TLS (open/ip 档不索取客户端证书, pinned 档索取并验指纹);
	// 留空即明文 (裸奔). 路径按 "cwd 优先、再可执行文件目录" 回退解析.
	CascadeTLSCert string `json:"cascadeTlsCert"`
	CascadeTLSKey  string `json:"cascadeTlsKey"`
	// CascadeControlAddr /v1 控制面监听地址, 例如 ":5125"
	CascadeControlAddr string `json:"cascadeControlAddr"`
	// CascadePendingTimeoutSec 会话 pending 绑定超时秒数, 0 用默认 5 (契约 :58)
	CascadePendingTimeoutSec int `json:"cascadePendingTimeoutSec"`
}

// NewCoreConfig 创建带默认值的核心配置
func NewCoreConfig() CoreConfig {
	return CoreConfig{
		NoteEnglish: "Different protocols use different configuration files, please go to the corresponding file ConfigServer/ConfigRelay",
		NoteChina:   "不同协议使用的配置文件不同, 请自行前往对应文件 ConfigServer/ConfigRelay",

		DefStartCommand: "start",
		Log:             "WARN",

		Port:       5123,
		ServerName: "RW-HPS",
		Subtitle:   "",
		AutoUpList: false,
		UplistEnable: true,

		MixProtocolEnable: false,
		RconMixEnable:     false,
		WebMixEnable:      false,

		WebPort:  0,
		WebToken: mathutil.GetRandomLetterString(10),
		WebHOST:  "",

		RunPid: 0,

		JavaPath:       "",
		GameHelperPath: "",
		GameHelperArgs: nil,

		CascadeEnable:             false,
		CascadeBackendID:          "rwhps-backend",
		CascadeRoomCode:           "",
		CascadeTrustMode:          "open",
		TrustedCascadeIPs:         nil,
		CascadePinnedFingerprints: nil,
		CascadeTLSCert:            "",
		CascadeTLSKey:             "",
		CascadeControlAddr:        ":5125",
		CascadePendingTimeoutSec:  0,
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

// SaveServerConfig 保存 ConfigServer.json
//
// 与 SaveCoreConfig 配套: helper (JVM) 退出时会按 Java 侧 BeanServerConfig
// 的字段集整文件重写 ConfigServer.json, Go 必须在 helper 退出后再写一次,
// 保证两侧字段集的并集不丢 (见 bootstrap 的 CloseServerHook 包装).
func SaveServerConfig(c ServerConfig) {
	Save(c, ServerConfigFile())
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

	EnterAd string `json:"enterAd"`
	StartAd string `json:"startAd"`

	// 已删除的死配置 (2026-11-04): maxPlayerJoinAd / startPlayerJoinAd
	// (广告位无运行时引用), defIncome (收入命令不读配置),
	// modsLoadErrorPrint (mod 加载错误打印未移植).

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

	IsAfk   bool `json:"isAfk"`
	MuteAll bool `json:"muteAll"`

	// TurnStoneIntoGold 点石成金
	TurnStoneIntoGold bool `json:"turnStoneIntoGold"`

	// SaveRePlayFile 是否保存 RePlay
	SaveRePlayFile bool `json:"saveRePlayFile"`
}

// NewServerConfig 创建带默认值的 Server 配置
func NewServerConfig() ServerConfig {
	return ServerConfig{
		ServerID: mathutil.GetRandomLetterString(5),

		EnterAd: "",
		StartAd: "",

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

		IsAfk:   true,
		MuteAll: false,

		TurnStoneIntoGold: false,

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
//
// 已删除的死配置 (2026-11-04): mainServerIP / mainServerPort
// (relay 主服地址无运行时引用).
type RelayConfig struct {
	MainID     string `json:"mainID"`
	MainServer bool   `json:"mainServer"`
	UpList     bool   `json:"upList"`
}

// NewRelayConfig 创建带默认值的 Relay 配置
func NewRelayConfig() RelayConfig {
	return RelayConfig{
		MainID:     "R",
		MainServer: true,
		UpList:     true,
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
