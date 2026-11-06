// Package uplist 对应上游 plugin/beta/UpListMain.kt (368 行的 beta 插件).
//
// 上游是插件体系 (JVM 字节码类加载) 加载的插件, 功能为把服务器上报到
// Rusted Warfare 官方服务器列表 (gs1/gs4 corrodinggames masterserver).
// 插件加载框架本身 (Plugin/PluginManage/类加载) 属 JVM 能力, Go 版不移植;
// 本包把它承载的具体功能作为普通 Go 代码直接内建 (里程碑 5).
//
// 与上游的逐条对应:
//
//	initUpListData / add / uplist / update / remove -> 同名方法
//	registerCoreCommands ("uplist" 控制台命令)       -> RegisterCommands
//	registerServerClientCommands ("toup"/"tonp")     -> RegisterClientCommands
//	registerEvents (ServerGameOverEvent 监听)        -> RegisterRoomEvents
//	init() 里的 AddLang (注入 uplist.help)           -> addLang
//	uplist() 末尾的 CustomUpServerListTask 50s 定时  -> uplist 内的 NewTimedTask
//
// 与上游的差异:
//   - 上游插件实例字段无并发保护 (Kotlin 插件默认单线程使用); Go 版命令回调
//     与定时任务并发访问同一实例, 全部可变状态由 mu 保护.
//   - 上游硬编码两个列表服务器地址; Go 版做成包级变量, 便于测试指向假服务器,
//     默认值与上游逐字一致.
package uplist

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"rwhps/internal/core/thread"
	"rwhps/internal/data/global"
	"rwhps/internal/game/event"
	"rwhps/internal/game/player"
	"rwhps/internal/game/room"
	"rwhps/internal/net"
	"rwhps/internal/net/core"
	"rwhps/internal/net/manage"
	str "rwhps/internal/util/str"
	"rwhps/internal/util/algorithms"
	"rwhps/internal/util/ip"
	"rwhps/internal/util/isutils"
	"rwhps/internal/util/jsonutil"
	"rwhps/internal/util/log"

	cmd "rwhps/internal/util/game/command"
)

// versionParam 对应上游插件字段 version = "Version=HPS#1" (initUpListData 的 POST 参数)
const versionParam = "Version=HPS#1"

// gs1URL / gs4URL 对应上游 uplist()/update()/remove() 里硬编码的两个列表服务器地址.
// 做成变量仅为测试可替换 (指向 httptest 假服务器), 默认值与上游一致.
var (
	gs1URL = "http://gs1.corrodinggames.com/masterserver/1.4/interface"
	gs4URL = "http://gs4.corrodinggames.net/masterserver/1.4/interface"
)

// doPoster 是 UpList 用到的 HTTP 客户端最小界面 (manage.HttpRequestManage 实现),
// 对应上游的 Data.core.http / Data.core.rwHttp 两个实例.
type doPoster interface {
	DoPost(url, param string) string
}

// UpList 对应 UpListMain 插件实例
type UpList struct {
	mu sync.Mutex

	// port 对应上游 port 字段, 初始为 Data.config.port
	port string

	// versionBeta/versionGame/versionGameInt 对应上游同名字段 (initUpListData 里刷新)
	versionBeta    bool
	versionGame    string
	versionGameInt int

	// upServerList 对应上游 upServerList: 是否已在列表上
	upServerList bool

	// 五个数据缓存, 对应上游 lateinit 的 serverID/addData/openData/updateData/removeData
	serverID   string
	addData    string
	openData   string
	updateData string
	removeData string

	// uplistFlag 对应上游 uplistFlag: 0=未上架, 1=已上架
	uplistFlag int
	// over 对应上游 over: toup 成功后置真, 对局结束时用于自动下架
	over bool

	// httpClient 对应 Data.core.http (initUpListData 用)
	httpClient doPoster
	// rwHttpClient 对应 Data.core.rwHttp (uplist/update/remove 用)
	rwHttpClient doPoster
}

var (
	defaultMu   sync.Mutex
	defaultInst *UpList
)

// Default 对应上游 PluginManage 持有的那个 UpListMain 单例.
// 惰性创建: 首次调用时 global.Core / global.Config 已由启动链装配完毕.
func Default() *UpList {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultInst == nil {
		defaultInst = NewUpList()
	}
	return defaultInst
}

// resetDefault 清空单例 (测试用, 上游无对应)
func resetDefault() {
	defaultMu.Lock()
	defaultInst = nil
	defaultMu.Unlock()
}

// NewUpList 对应 UpListMain 的构造: port 取 Data.config.port, 版本字段取默认值
// (versionBeta=false / versionGame="1.15" / versionGameInt=176, 见上游字段声明).
func NewUpList() *UpList {
	u := &UpList{
		port:           strconv.Itoa(global.Config.Port),
		versionBeta:    false,
		versionGame:    "1.15",
		versionGameInt: 176,
	}
	if global.Core != nil {
		u.httpClient = global.Core.HTTP
		u.rwHttpClient = global.Core.RWHTTP
	}
	if u.httpClient == nil {
		u.httpClient = manage.NewHttpRequestManage(global.UserAgent)
	}
	if u.rwHttpClient == nil {
		u.rwHttpClient = manage.NewHttpRequestManage("RW-HPS " + global.ServerCoreVersion)
	}
	return u
}

// --- 注册入口 ---

// 有意修正（作者已批准，2026-11-04, DECISIONS D-22）: 自报总开关.
//
// 背景: Cascade 边缘代理模式下曾提议"启用 Cascade 即自动关停自报, presence
// 由 Cascade 统一向 HRPAuth 注册". 核实 (/tmp/cascade-ro @ f94f511) 后作者
// 改判: (1) Cascade 的 HA presence 注册的是服务发现/转发信息, 与玩家在
// 游戏内看到的服务器列表无关; (2) Cascade 没有向 RW 官方列表服
// (gs1/gs4 masterserver) 上报的能力. 因此本开关与 cascadeEnable 解耦,
// 默认保持自报; 关闭后端自报的前提是有人以 Cascade 公网地址向 RW 官方
// 列表接管上报, 届时再由作者决定改默认值.

// disabledMsg 自报被关闭时各入口 (控制台命令/客户端命令) 的统一提示
const disabledMsg = "UPLIST 自报已按配置关闭 (uplistEnable=false); RW 官方列表上报由 Cascade 侧以公网地址接管后才会改默认值 (DECISIONS D-22)"

// enabled 报告 UPLIST 自报是否启用 (config uplistEnable, 默认 true).
// 运行时读取, 与 cascadeEnable 无关.
func enabled() bool { return global.Config.UplistEnable }

// RegisterCommands 对应上游 registerCoreCommands(handler):
// 移除旧命令 upserverlist/upserverlistnew 后注册 uplist 命令.
//
// 上游注册点: Main.kt 的 server.load.plugin.command 阶段
// (PluginManage.runRegisterCoreCommands(Data.SERVER_COMMAND)).
func RegisterCommands(h *cmd.Handler) {
	addLang()

	h.RemoveCommand("upserverlist")
	h.RemoveCommand("upserverlistnew")

	h.RegisterWith("uplist", "[command...]", "serverCommands.upserverlist", func(args []string, param any) {
		// 上游回调签名 { args, log: StrCons }; Go 版控制台经 HandleMessageWith(line, global.DefPrint) 传入
		print, _ := param.(func(string))
		if print == nil {
			print = global.DefPrint
		}

		// 自报总开关 (DECISIONS D-22): 关闭时拦截 add/update/remove;
		// help 与未知子命令提示不受影响. AutoUpList 的自动上架经
		// "uplist add" 命令进入, 同样在此被拦下.
		if len(args) > 0 && args[0] != "help" && !enabled() {
			print(disabledMsg)
			return
		}

		if len(args) > 0 {
			switch args[0] {
			case "add":
				net.CheckServerStartNet(func() {
					if len(args) > 1 {
						Default().add(print, args[1])
					} else {
						Default().add(print, "")
					}
				})
			case "update":
				net.CheckServerStartNet(func() { Default().update() })
			case "remove":
				net.CheckServerStartNet(func() { Default().remove(print) })
			case "help":
				print(tr("uplist.help"))
			default:
				print("Check UpList Command ! use 'uplist help'")
			}
		} else {
			print("Check UpList Command ! use 'uplist help'")
		}
	})
}

// RegisterClientCommands 对应上游 registerServerClientCommands(handler): toup / tonp.
//
// 上游注册点: 引擎模块加载 (ServerHessLoadEvent) 时经
// PluginManage.runRegisterServerClientCommands(room.clientHandler);
// Go 版在启动链建房后与 ClientCommands 同处接线 (见 app/bootstrap.go).
func RegisterClientCommands(h *cmd.Handler) {
	h.Register("toup", "#up", func(_ []string, param any) {
		p := asPlayer(param)
		if !isAdmin(p) {
			return
		}
		if !enabled() { // DECISIONS D-22 自报总开关
			_ = p.SendSystemMessage(disabledMsg)
			return
		}
		u := Default()
		switch u.getUplistFlag() {
		case 0:
			// 上游 kotlin.concurrent.thread { ... }: 独立线程, 不占命令回调
			go func() {
				_ = p.SendSystemMessage("耐心等待, 错误信息不打印, 请自行在控制台提前实验")
				if u.initUpListData("") {
					u.setOver(true)
					// 上游 this.port = port.ifBlank { Data.config.port.toString() }
					u.setPortIfBlank()
					thread.NewThreadCore(func() {
						u.markListed()
						u.uplist(func(s string) { _ = p.SendSystemMessage(s) })
					})
				}
			}()
		default:
			_ = p.SendSystemMessage("Already on the list")
		}
	})

	h.Register("tonp", "#up", func(_ []string, param any) {
		p := asPlayer(param)
		if !isAdmin(p) {
			return
		}
		if !enabled() { // DECISIONS D-22 自报总开关
			_ = p.SendSystemMessage(disabledMsg)
			return
		}
		u := Default()
		switch u.getUplistFlag() {
		case 1:
			go func() {
				_ = p.SendSystemMessage("耐心等待")
				u.remove(func(s string) { _ = p.SendSystemMessage(s) })
			}()
		default:
			_ = p.SendSystemMessage("no uplist")
		}
	})
}

// RegisterRoomEvents 对应上游 registerEvents(eventManage): 监听 ServerGameOverEvent.
//
// 上游插件事件经 EventGlobal.registerServerHessLoadEvent ->
// PluginManage.runRegisterEvents 注册到房间 eventManage; Go 版由启动链在
// 建房后直接接线.
func RegisterRoomEvents(r *room.ServerRoom) {
	if r == nil || r.EventManage == nil {
		return
	}
	event.Register(r.EventManage, func(_ *event.ServerGameOverEvent) {
		Default().gameOver()
	})
}

// isAdmin 对应上游 registerServerClientCommands 里的 isAdmin 闭包
func isAdmin(p *player.Player) bool {
	if p == nil {
		return false
	}
	if p.IsAdmin {
		return true
	}
	_ = p.SendSystemMessage(p.GetInput("err.noAdmin"))
	return false
}

// asPlayer 把命令回调的 param 还原为玩家
func asPlayer(param any) *player.Player {
	p, _ := param.(*player.Player)
	return p
}

// tr 对应 Data.i18NBundle.getinput
func tr(key string, params ...any) string {
	if global.I18NBundle == nil {
		return key
	}
	return global.I18NBundle.GetInput(key, params...)
}

// addLang 对应上游 init() 里的 AddLang: 向四种语言包注入 uplist.help.
// 上游经 plugin.loadLang 写入 Data.i18NBundleMap; Go 版直接写全局语言包.
func addLang() {
	langs := map[string]string{
		"CN": "[uplist add] 服务器上传到列表 显示配置文件端口\n[uplist add (port)] 服务器上传到列表 服务器运行配置文件端口 显示自定义端口\n[uplist update] 立刻更新列表服务器信息\n[uplist remove] 取消服务器上传列表\n[uplist help] 获取帮助",
		"EN": "[uplist add] Server upload to list Show profile port\n[uplist add (port)] Server upload to list Server running profile port Display custom port\n[uplist update] Update list server information immediately\n[uplist remove] Cancel server upload list\n[uplist help] Get Help",
		"HK": "[uplist add] 服务器上传到列表 显示配置文件端口\n[uplist add (port)] 服务器上传到列表 服务器运行配置文件端口 显示自定义端口\n[uplist update] 立刻更新列表服务器信息\n[uplist remove] 取消服务器上传列表\n[uplist help] 获取帮助",
		"RU": "[uplist add] Загрузка сервера в список Показать порт профиля\n[uplist add (port)] Загрузка сервера в список Порт запущенного профиля сервера Показать пользовательские порты\n[uplist update] Немедленное обновление информации сервера списка\n[uplist remove] Отмена загрузки сервера в список\n[uplist help] Получить помощь",
	}
	for lang, text := range langs {
		if bundle, ok := global.I18NBundleMap[lang]; ok && bundle != nil {
			bundle.AddLang("uplist.help", text, false)
		}
	}
}

// --- 核心流程 (照搬 UpListMain.kt) ---

// versionProvider 对应上游 initUpListData 里
// `(typeConnect.abstractNetConnect as AbstractNetConnectServer)` 所需的最小界面;
// gameserver.HeadlessTypeConnect 实现了这三个方法 (见其注释).
type versionProvider interface {
	SupportedVersionBeta() bool
	SupportedVersionGame() string
	SupportedVersionInt() int
}

// resolveVersion 对应 initUpListData 开头的版本信息刷新 (UpListMain.kt:145-156):
// netType 在 [ServerProtocol, ServerTestProtocol] 区间内时取协议实现体的
// supportedversion*, 否则用 false / "1.15-Other" / 176.
func (u *UpList) resolveVersion() {
	rwHps := net.NetStaticData.RwHps()
	nt := rwHps.NetType()
	if nt >= core.ServerProtocol && nt <= core.ServerTestProtocol {
		if vc, ok := rwHps.TypeConnect().(versionProvider); ok {
			u.setVersion(vc.SupportedVersionBeta(), vc.SupportedVersionGame(), vc.SupportedVersionInt())
			return
		}
		// 上游此处强转对 Server 协议族恒成功; Go 版实现体若缺版本方法,
		// 退回与上游 else 分支相同的默认值.
	}
	u.setVersion(false, "1.15-Other", 176)
}

func (u *UpList) setVersion(beta bool, game string, gameInt int) {
	u.mu.Lock()
	u.versionBeta = beta
	u.versionGame = game
	u.versionGameInt = gameInt
	u.mu.Unlock()
}

// initUpListData 对应上游 initUpListData(urlIn = ""): POST 拿
// serverID/addData/openData/updateData/removeData (base64+JSON).
func (u *UpList) initUpListData(urlIn string) bool {
	u.resolveVersion()

	url := urlIn
	if isutils.IsBlank(url) {
		url = urlStringData("Get.Api.UpListData.Bak")
	}

	resultUpList := u.httpClient.DoPost(url, versionParam)

	// 上游: 备用地址失败且未指定 URL 时, 回落到主地址
	if isutils.IsBlank(resultUpList) && isutils.IsBlank(urlIn) {
		resultUpList = u.httpClient.DoPost(urlStringData("Get.Api.UpListData"), versionParam)
	}

	if isutils.IsBlank(resultUpList) {
		log.Error("[Get UPLIST Data Error] Unexpected error Failed to initialize")
		return false
	}

	if strings.HasPrefix(resultUpList, "[-1]") {
		log.Error("[Get UPLIST Data Error] Please Check API")
		return false
	} else if strings.HasPrefix(resultUpList, "[-2]") {
		log.Error("[Get UPLIST Data Error] IP prohibited")
		return false
	} else if strings.HasPrefix(resultUpList, "[-4]") {
		log.Error("[Get UPLIST Data Error] Version Error")
		// 上游 resultUpList.substring(8): 响应格式为 "[-4] -> <新地址或 Error>".
		// 上游对长度不足 8 的响应会抛 StringIndexOutOfBoundsException; Go 版不抛异常,
		// 按"没有新地址"处理 (与 newUrl == "Error" 分支的可观察结果一致).
		if len(resultUpList) <= 8 {
			log.Error("[Get UPLIST Data Error] Version Error & New Error")
			return false
		}
		newURL := resultUpList[8:]
		if newURL == "Error" {
			log.Error("[Get UPLIST Data Error] Version Error & New Error")
			return false
		}
		return u.initUpListData(newURL)
	} else if strings.HasPrefix(resultUpList, "[-0]") {
		log.Error("[UPLIST Info] " + strings.TrimPrefix(resultUpList, "[-0]"))
	} else if strings.HasPrefix(resultUpList, "[-5]") {
		log.Error(resultUpList)
	}

	// 上游 Json(resultUpList) 解析失败会抛 JSONException; Go 版不抛异常,
	// 解析失败按初始化失败处理 (错误分支的可观察结果一致: 不上架).
	var json map[string]any
	if err := jsonutil.FromJSON(resultUpList, &json); err != nil {
		log.Errorf("[Get UPLIST Data Error]", "{0}", err)
		return false
	}

	u.mu.Lock()
	u.serverID = base64Field(json, "id")
	u.addData = base64Field(json, "add")
	u.openData = base64Field(json, "open")
	u.updateData = base64Field(json, "update")
	u.removeData = base64Field(json, "remove")
	u.mu.Unlock()
	return true
}

// urlStringData 对应 Data.urlData.readString(key) (Go 版 ReadString 需默认值, 传空串)
func urlStringData(key string) string {
	if global.URLData == nil {
		return ""
	}
	return global.URLData.ReadString(key, "")
}

// base64Field 对应 Base64.decodeString(json.getString(key)):
// 缺失字段按空串解码; 解码失败返回空串 (上游 Base64 工具对非法输入同样宽容).
func base64Field(json map[string]any, key string) string {
	v, _ := json[key].(string)
	if v == "" {
		return ""
	}
	out, err := algorithms.Base64DecodeString(v)
	if err != nil {
		return ""
	}
	return out
}

// add 对应上游 add(log, port = "")
func (u *UpList) add(print func(string), port string) {
	if u.isUpServerList() {
		print("Already on the list")
		return
	}
	if u.initUpListData("") {
		u.setPort(port)
		// 上游 Threads.newThreadCore { upServerList = true; uplistFlag = 1; uplist(Log::clog) }
		thread.NewThreadCore(func() {
			u.markListed()
			u.uplist(func(s string) { log.Clog(s) })
		})
	}
}

// uplist 对应上游 uplist(print): 替换 addData/openData 占位符并推送两个列表服务器,
// 成功后挂 50s 周期的 CustomUpServerListTask.
func (u *UpList) uplist(print func(string)) {
	addData0, serverID := u.buildAddData()

	log.Debug(addData0)

	addGs1 := strings.Contains(u.rwHttpClient.DoPost(gs1URL, addData0), serverID)
	addGs4 := strings.Contains(u.rwHttpClient.DoPost(gs4URL, addData0), serverID)
	if addGs1 || addGs4 {
		if addGs1 && addGs4 {
			print(tr("err.yesList"))
		} else {
			print(tr("err.ynList"))
		}
	} else {
		print(tr("err.noList"))
	}

	openData0 := strings.ReplaceAll(u.snapshotOpenData(), "{RW-HPS.S.PORT}", u.snapshotPort())

	checkPortGs1 := strings.Contains(u.rwHttpClient.DoPost(gs1URL, openData0), "true")
	checkPortGs4 := strings.Contains(u.rwHttpClient.DoPost(gs4URL, openData0), "true")
	if checkPortGs1 || checkPortGs4 {
		print(tr("err.yesOpen"))
	} else {
		print(tr("err.noOpen"))
	}

	// 上游 Threads.newTimedTask(CustomUpServerListTask, 50, 50, SECONDS) { update() }
	thread.NewTimedTask(thread.CustomUpServerListTask, 50*time.Second, 50*time.Second, func() {
		u.update()
	})
}

// buildAddData 对应上游 uplist() 里 addData0 的全部占位符替换
func (u *UpList) buildAddData() (addData0 string, serverID string) {
	u.mu.Lock()
	addData := u.addData
	serverID = u.serverID
	versionGame := u.versionGame
	versionGameInt := u.versionGameInt
	versionBeta := u.versionBeta
	port := u.port
	u.mu.Unlock()

	addData0 = strings.ReplaceAll(addData, "{RW-HPS.RW.VERSION}", versionGame)
	addData0 = strings.ReplaceAll(addData0, "{RW-HPS.RW.VERSION.INT}", strconv.Itoa(versionGameInt))
	addData0 = strings.ReplaceAll(addData0, "{RW-HPS.RW.IS.VERSION}", strconv.FormatBool(versionBeta))
	addData0 = strings.ReplaceAll(addData0, "{RW-HPS.RW.IS.PASSWD}", strconv.FormatBool(!isutils.IsBlank(global.ConfigServer.Passwd)))
	addData0 = strings.ReplaceAll(addData0, "{RW-HPS.S.NAME}", str.Cutting(global.Config.ServerName, 15))
	addData0 = strings.ReplaceAll(addData0, "{RW-HPS.S.PRIVATE.IP}", privateIp())
	addData0 = strings.ReplaceAll(addData0, "{RW-HPS.S.PORT}", port)

	addData0 = strings.ReplaceAll(addData0, "{RW-HPS.RW.MAP.NAME}", getMapName())
	addData0 = strings.ReplaceAll(addData0, "{RW-HPS.PLAYER.SIZE}", strconv.Itoa(serverPlayerSize()))

	addData0 = strings.ReplaceAll(addData0, "{RW-HPS.PLAYER.SIZE.MAX}", strconv.Itoa(global.ConfigServer.MaxPlayer))

	return addData0, serverID
}

// update 对应上游 update(): 替换 updateData 占位符并推送两个列表服务器
func (u *UpList) update() {
	u.mu.Lock()
	updateData := u.updateData
	port := u.port
	u.mu.Unlock()

	updateData0 := strings.ReplaceAll(updateData, "{RW-HPS.RW.IS.PASSWD}", strconv.FormatBool(!isutils.IsBlank(global.ConfigServer.Passwd)))
	updateData0 = strings.ReplaceAll(updateData0, "{RW-HPS.S.NAME}", str.Cutting(global.Config.ServerName, 15))
	updateData0 = strings.ReplaceAll(updateData0, "{RW-HPS.S.PRIVATE.IP}", privateIp())
	updateData0 = strings.ReplaceAll(updateData0, "{RW-HPS.S.PORT}", port)

	updateData0 = strings.ReplaceAll(updateData0, "{RW-HPS.RW.MAP.NAME}", getMapName())
	// 上游: if (isRelay || HeadlessModuleManage.hps.room.isStartGame) "ingame" else "battleroom"
	status := "battleroom"
	if isRelay() || roomIsStartGame() {
		status = "ingame"
	}
	updateData0 = strings.ReplaceAll(updateData0, "{RW-HPS.S.STATUS}", status)
	updateData0 = strings.ReplaceAll(updateData0, "{RW-HPS.PLAYER.SIZE}", strconv.Itoa(serverPlayerSize()))

	updateData0 = strings.ReplaceAll(updateData0, "{RW-HPS.PLAYER.SIZE.MAX}", strconv.Itoa(global.ConfigServer.MaxPlayer))

	u.rwHttpClient.DoPost(gs1URL, updateData0)
	u.rwHttpClient.DoPost(gs4URL, updateData0)
}

// remove 对应上游 remove(log)
func (u *UpList) remove(print func(string)) {
	u.mu.Lock()
	if !u.upServerList {
		u.mu.Unlock()
		print("Not uploaded No deletion is required")
		return
	}
	removeData := u.removeData
	u.mu.Unlock()

	u.rwHttpClient.DoPost(gs1URL, removeData)
	u.rwHttpClient.DoPost(gs4URL, removeData)

	u.mu.Lock()
	u.upServerList = false
	u.uplistFlag = 0
	u.mu.Unlock()
	print("Deleted UPLIST")
}

// gameOver 对应上游 ServerGameOverEvent 监听:
// if (over) remove({}); over = false
func (u *UpList) gameOver() {
	if u.snapshotOver() {
		u.remove(func(string) {})
	}
	u.setOver(false)
}

// --- 状态访问器与上游私有属性对应 ---

// isRelay 对应上游 isRelay 属性
func isRelay() bool {
	nt := net.NetStaticData.RwHps().NetType()
	return nt == core.RelayProtocol || nt == core.RelayMulticastProtocol
}

// getMapName 对应上游 getMapName 属性:
// Data.config.subtitle.ifEmptyResult({ if (isRelay) "RELAY" else room.maps.mapName }) { cutting(it, 15) }
func getMapName() string {
	if global.Config.Subtitle != "" {
		return str.Cutting(global.Config.Subtitle, 15)
	}
	if isRelay() {
		return "RELAY"
	}
	if r := room.Default(); r != nil && r.Maps != nil {
		return r.Maps.MapName
	}
	return ""
}

// roomIsStartGame 对应上游 HeadlessModuleManage.hps.room.isStartGame
func roomIsStartGame() bool {
	if r := room.Default(); r != nil {
		return r.IsStartGame
	}
	return false
}

// serverPlayerSize 对应上游 serverPlayerSize 属性:
// relay 模式累加全部 HeadlessNet 服务的连接数, 否则取房间 playerGroup 大小.
func serverPlayerSize() int {
	if isRelay() {
		total := 0
		for _, s := range net.NetStaticData.Services() {
			if s.NetType == net.NetServiceHeadless {
				total += s.GetConnectSize()
			}
		}
		return total
	}
	if r := room.Default(); r != nil && r.PlayerManage != nil {
		return r.PlayerManage.PlayerGroup.Size()
	}
	return 0
}

// privateIp 对应上游 privateIp 属性: 取网卡私网 IP, 空时回落 10.0.0.1
func privateIp() string {
	p := ip.GetPrivateIp()
	if isutils.IsBlank(p) {
		p = "10.0.0.1"
	}
	return p
}

// --- 加锁的小工具 ---

func (u *UpList) isUpServerList() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.upServerList
}

func (u *UpList) getUplistFlag() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.uplistFlag
}

// markListed 对应上游 `upServerList = true; uplistFlag = 1`
func (u *UpList) markListed() {
	u.mu.Lock()
	u.upServerList = true
	u.uplistFlag = 1
	u.mu.Unlock()
}

func (u *UpList) setOver(v bool) {
	u.mu.Lock()
	u.over = v
	u.mu.Unlock()
}

func (u *UpList) snapshotOver() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.over
}

// setPort 对应上游 this.port = port.ifBlank { Data.config.port.toString() }
func (u *UpList) setPort(port string) {
	if isutils.IsBlank(port) {
		port = strconv.Itoa(global.Config.Port)
	}
	u.mu.Lock()
	u.port = port
	u.mu.Unlock()
}

// setPortIfBlank 对应上游 toup 里的 this.port = port.ifBlank { Data.config.port.toString() }
func (u *UpList) setPortIfBlank() {
	u.mu.Lock()
	if isutils.IsBlank(u.port) {
		u.port = strconv.Itoa(global.Config.Port)
	}
	u.mu.Unlock()
}

func (u *UpList) snapshotPort() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.port
}

func (u *UpList) snapshotOpenData() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.openData
}
