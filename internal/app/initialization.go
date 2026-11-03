// Package app 对应 net.rwhps.server.core.Initialization.
package app

import (
	"strings"

	"rwhps/internal/core/service"
	"rwhps/internal/data/global"
	"rwhps/internal/game/manage"
	"rwhps/internal/io/packettype"
	"rwhps/internal/net/core"
	"rwhps/internal/net/gameserver"
	"rwhps/internal/net/netconnectprotocol"
	"rwhps/internal/net/netconnectprotocol/realize"
	"rwhps/internal/resource"
	"rwhps/internal/util/file/load"
	"rwhps/internal/util/file/plugindata"
	"rwhps/internal/util/log"
)

// loadLang 对应 Initialization.loadLang
//
// 原版从 jar 内读 4 个语言包并按 CN/HK/RU/EN 顺序注册, 默认用 EN.
// Go 版用 go:embed 内嵌同一批文件 (internal/resource).
func loadLang() error {
	if err := loadURLData(); err != nil {
		return err
	}

	for _, lang := range resource.Languages() {
		b, err := resource.Bundle(lang)
		if err != nil {
			return err
		}
		global.I18NBundleMap[lang] = load.NewI18NBundleFromBytes(b)
	}
	global.I18NBundle = global.I18NBundleMap["EN"]

	// log 包通过函数注入取词, 避免 log -> global 的循环依赖.
	// 闭包每次读 global.I18NBundle, 所以 setlanguage 换语言后立刻生效.
	log.Bundle = func(key string, params ...any) string {
		if global.I18NBundle == nil {
			return key
		}
		return global.I18NBundle.GetInput(key, params...)
	}
	return nil
}

// loadURLData 对应 Data.urlData 的静态初始化
//
// 原版写在 Data 的伴生对象里 (LoadIni("/URL.ini")), 在 Data 第一次被引用时就完成;
// Go 没有伴生对象, 因此把它并到 loadLang 的开头, 保证 initServerLanguage 取 URL 时它已就绪.
func loadURLData() error {
	b, err := resource.URLIni()
	if err != nil {
		return err
	}
	global.URLData = load.NewIniFromBytes(b)
	return nil
}

// startInit 对应 Initialization.startInit
func startInit(settings *plugindata.Settings) {
	initServerLanguage(settings, "")
	eula(settings)
}

// initServerLanguage 对应 Initialization.initServerLanguage
//
// country 为空时按首次启动探测到的出口 IP 国家决定语言, 并持久化;
// country 非空 (setlanguage 命令) 时只接受 HK/CN/RU, 其它一律回落到 EN.
//
// 与原版的差异: 原版 `Data.i18NBundleMap[serverCountry]!!` 在语言包缺失时会 NPE,
// 这里回落到 EN 并记一条警告 —— 语言包缺失是打包问题, 不该让服务器起不来.
func initServerLanguage(settings *plugindata.Settings, country string) {
	if strings.TrimSpace(country) == "" {
		global.ServerCountry = settings.GetString("serverCountry", detectCountry)
	} else {
		lang := "EN"
		for _, c := range []string{"HK", "CN", "RU"} {
			if strings.Contains(country, c) {
				lang = c
				break
			}
		}
		global.ServerCountry = lang
		settings.SetString("serverCountry", lang)
	}

	if bundle, ok := global.I18NBundleMap[global.ServerCountry]; ok && bundle != nil {
		global.I18NBundle = bundle
	} else {
		log.Warnf("[Language]", "bundle {0} not found, falling back to EN", global.ServerCountry)
		global.ServerCountry = "EN"
		global.I18NBundle = global.I18NBundleMap["EN"]
	}

	log.Clog(tr("server.language"))
}

// detectCountry 对应 initServerLanguage 里那个"按出口 IP 判断国家"的默认值闭包
//
// 原版请求 http://http.api.data.der.kim/IP/getCountry 并按返回文本里的中文国家名判断;
// 请求失败时 DoGet 返回空串, 于是落到 EN.
func detectCountry() string {
	if global.Core == nil || global.Core.HTTP == nil || global.URLData == nil {
		return "EN"
	}
	body := global.Core.HTTP.DoGet(global.URLData.ReadString("Get.Api.ServerLanguage.Bak", ""))
	switch {
	case strings.Contains(body, "香港"):
		return "HK"
	case strings.Contains(body, "中国"):
		return "CN"
	case strings.Contains(body, "俄罗斯"):
		return "RU"
	default:
		return "EN"
	}
}

// eula 对应 Initialization.eula
//
// 两步交互: 许可版本不匹配时展示 EULA 并要求 Yes, 首次启动时询问是否需要 WEB 面板初始化.
// 两步都通过 log.WaitLicense 读取 stdin, 与原版一致.
func eula(settings *plugindata.Settings) {
	if readString(settings, "eulaVersion") != global.ServerEulaVersion {
		log.WaitLicense(func() {
			text, err := resource.EULA(global.ServerCountry)
			if err != nil {
				log.Errorf("[EULA]", "{0}", err)
				return
			}
			log.Clog(string(text))
		}, func() {
			settings.SetString("eulaVersion", global.ServerEulaVersion)
		}, func() {
			// 原版: { Data.core.exit() } —— 拒绝许可直接退出进程
			if global.Core != nil {
				global.Core.Exit(nil)
			}
		})
	}

	if settings.GetBool("initStart", true) {
		log.WaitLicense(func() {
			if global.ServerCountry == "CN" {
				log.Clog("您看起来是第一次运行服务器, 需要WEB面板帮助您初始化吗")
				return
			}
			log.Clog("It looks like you are running the server for the first time. Do you need the WEB panel to help you initialize?")
		}, func() {
			settings.SetBool("initStart", false)
		}, func() {})
	}

	if err := settings.Save(); err != nil {
		log.Errorf("[Settings]", "save failed: {0}", err)
	}
}

// readString 读取一个字符串设置项, 不存在时返回空串且不写入
//
// 对应 Kotlin 的 pluginData["key", ""]: 只有取值形式, 没有 putIfAbsent 的副作用.
func readString(settings *plugindata.Settings, key string) string {
	if v, ok := settings.Get(key); ok && v != nil && v.Kind == plugindata.KindString {
		return v.Str
	}
	return ""
}

// loadLib 对应 Initialization.loadLib
//
// 原版按 /maven/*/compileOnly.txt 与 implementation.txt 列出的坐标, 从 Maven 下载 jar
// 并注入自己的 ClassLoader (ASM-Framework / Server-Core / TimeTaskQuartz).
// Go 版是静态链接的单一二进制, 没有运行期类加载器, 也没有需要按需下载的依赖,
// 因此这里是空实现 —— 保留函数是为了让启动链的顺序与原版一一对应.
func loadLib() {}

// checkEnvironment 对应 Initialization.checkEnvironment
//
// 原版调用 NativeLibrary.getProcess() 探测 JNA 本地库是否与当前架构兼容.
// Go 版没有 JNA, 这一步天然不适用.
func checkEnvironment() {}

// loadService 对应 Initialization.loadService
//
// 原版注册 5 类服务; Relay 协议族 (TypeRelay / GameVersionRelay) 在里程碑 4
// 移植后由 manage.SetProvider 按 NetType 装配. RelayMulticastProtocol 上游对应
// TypeRelayRebroadcast / GameVersionRelayRebroadcast (多播转发), 尚未移植,
// 暂以普通 Relay 实现承接 (见 netconnectprotocol.NewRelayRwHps 注释).
//
// IRwHps 一族: 原版 addIRwHps 通过 ServiceLoader.newInstance 创建实例,
// 实例来源按 netType 查表 —— ServerProtocol 的实现体 (HeadlessTypeConnect 一族)
// 已移植为 net/gameserver, Relay 两型为 netconnectprotocol.RelayRwHps.
//
// 上游对应的注册点:
//   - GameEngine.kt:118 ServiceLoader.addService(ProtocolType, ServerProtocol, HeadlessTypeConnect)
//   - Initialization.kt:264-270 Relay 两型 (ProtocolType: TypeRelay / Protocol: GameVersionRelay)
func loadService() {
	if err := service.Default.RegisterDefault(service.KindProtocolPacket, core.GlobalProtocol.String(), realize.NewGameVersionPacket()); err != nil {
		log.Errorf("[ServiceLoader]", "{0}", err)
	}
	if err := service.Default.RegisterDefault(service.KindPacketType, core.GlobalProtocol.String(), packettype.INSTANCE); err != nil {
		log.Errorf("[ServiceLoader]", "{0}", err)
	}

	// realize 不导入父包 netconnectprotocol (避免循环), getPacketTeamData 在此接线
	realize.SetPacketTeamDataParser(netconnectprotocol.GetPacketTeamData)

	manage.SetProvider(func(netType core.NetType) core.IRwHps {
		switch netType {
		case core.ServerProtocol:
			return gameserver.NewRwHps(netType)
		case core.RelayProtocol, core.RelayMulticastProtocol:
			return netconnectprotocol.NewRelayRwHps(netType)
		}
		return core.NewNullRwHps(netType)
	})
}

// tr 对应 Data.i18NBundle.getinput
func tr(key string, params ...any) string {
	if global.I18NBundle == nil {
		return key
	}
	return global.I18NBundle.GetInput(key, params...)
}
