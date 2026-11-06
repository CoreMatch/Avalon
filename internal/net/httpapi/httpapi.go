package httpapi

import (
	"strings"
	"sync"

	"rwhps/internal/data/global"
	"rwhps/internal/game/event"
	"rwhps/internal/game/room"
	nethttp "rwhps/internal/net/http"
	"rwhps/internal/util/algorithms"
	"rwhps/internal/util/jsonutil"
	"rwhps/internal/util/log"
	"rwhps/internal/util/mathutil"
	"rwhps/internal/util/timeutil"
)

// name / cookieName 对应 RwHpsWebApiMain.name / cookieName 常量
const (
	name       = "HttpApi"
	cookieName = "HttpApi-Authentication"
)

// Register 对应 RwHpsWebApiMain.onEnable -> Authentication.registerAuthenticationCenter:
// 在 WebData 上注册 AuthCookie (GET/POST) 与 get/post 通配路由.
//
// 上游注册点: Main.kt 插件阶段的 runOnEnable. Go 版由启动链在插件接线块调用.
//
// 有意修正（作者已批准，2026-11-04）: 控制台 WebSocket 端点
// (/WebSocket/HttpApi/api/Console) 随 internal/net/http 补齐升级握手后移回
// 正式移植范围 (DECISIONS D-14); 控制台输出转发经 log.ConsoleHook 接入,
// 对应上游 registerGlobalEvents 的 ServerConsolePrintEvent 监听.
func Register(webData *nethttp.WebData) {
	if webData == nil {
		return
	}
	auth := newAuthentication()
	for _, r := range []struct {
		url string
		get nethttp.WebGet
	}{
		{"/" + name + "/api/AuthCookie", auth},
		{"/" + name + "/api/get/**", auth},
	} {
		if err := webData.AddWebGetInstance(r.url, r.get); err != nil {
			log.Errorf("[HttpApi]", "{0}", err)
		}
	}
	for _, r := range []struct {
		url  string
		post nethttp.WebPost
	}{
		{"/" + name + "/api/AuthCookie", auth},
		{"/" + name + "/api/post/**", auth},
	} {
		if err := webData.AddWebPostInstance(r.url, r.post); err != nil {
			log.Errorf("[HttpApi]", "{0}", err)
		}
	}

	// 控制台 WebSocket (D-14)
	auth.registerConsoleWebSocket(webData)
	installConsoleHook()
}

// RegisterRoomEvents 对应 RwHpsWebApiMain.registerEvents -> HttpApiEvent:
// 监听房间 ServerGameOverEvent, 把非空 GameOverData 序列化进缓存.
//
// 上游经插件 runRegisterEvents 挂到房间 eventManage; Go 版由启动链接线.
// 当前 helper 协议尚无结算数据 (DECISIONS D-9), GameOverData 恒为 nil 时
// 缓存保持为空 —— 与上游 `gameOverData?.run {...}` 的 nil 跳过语义一致.
func RegisterRoomEvents(r *room.ServerRoom) {
	if r == nil || r.EventManage == nil {
		return
	}
	event.Register(r.EventManage, func(ev *event.ServerGameOverEvent) {
		if ev == nil || ev.GameOverData == nil {
			return
		}
		DefaultGameOverCache.add(jsonutil.ToJSON(ev.GameOverData))
	})
}

// authentication 对应 Authentication: Cookie 签发与鉴权, 同时作为
// WebGet/WebPost 实现体把请求转发给 forwardingCenter.
type authentication struct {
	mu          sync.Mutex
	cookiesData map[string]int64 // cookie 值 -> 过期秒数 (对应 ObjectIntMap)

	forwarding *forwardingCenter
}

func newAuthentication() *authentication {
	return &authentication{
		cookiesData: make(map[string]int64),
		forwarding:  &forwardingCenter{},
	}
}

// Get 实现 WebGet: /AuthCookie 签发 Cookie, /api/get/** 先鉴权再分发
func (a *authentication) Get(accept *nethttp.AcceptWeb, send *nethttp.SendWeb) {
	if accept.GetURL == "/"+name+"/api/AuthCookie" {
		a.registerCookie(nethttp.StringURLDataResolveToMap(accept.URLData), accept, send)
		return
	}
	if a.authenticate(nethttp.HeadResolveToCookie(accept), send) {
		a.forwarding.getCategorize(accept, send)
	}
}

// Post 实现 WebPost: /AuthCookie 签发 Cookie, /api/post/** 先鉴权再分发
func (a *authentication) Post(accept *nethttp.AcceptWeb, send *nethttp.SendWeb) {
	if accept.GetURL == "/"+name+"/api/AuthCookie" {
		a.registerCookie(postBodyToMap(accept), accept, send)
		return
	}
	if a.authenticate(nethttp.HeadResolveToCookie(accept), send) {
		a.forwarding.postCategorize(accept, send)
	}
}

// StringPostDataResolveToJson 实现 WebPost 接口 (本包内直接走 postBodyToMap)
func (a *authentication) StringPostDataResolveToJson(accept *nethttp.AcceptWeb) (map[string]string, error) {
	return postBodyToMap(accept), nil
}

// postBodyToMap 对应 WebPost.stringPostDataResolveToJson:
// application/json 按 JSON 解析 (只取字符串字段, 对应上游 Json.getString 用法),
// 其余按表单解析; 空体返回空表.
//
// 与上游差异: 上游遇到未知 Content-Type 抛 ImplementedException; Go 版回落到
// 表单解析, 不中断请求.
func postBodyToMap(accept *nethttp.AcceptWeb) map[string]string {
	if accept == nil || accept.Data == "" {
		return map[string]string{}
	}
	if strings.Contains(strings.ToLower(accept.GetHeaders("Content-Type")), "application/json") {
		var m map[string]any
		if err := jsonutil.FromJSON(accept.Data, &m); err != nil {
			return map[string]string{}
		}
		out := make(map[string]string, len(m))
		for k, v := range m {
			if s, ok := v.(string); ok {
				out[k] = s
			}
		}
		return out
	}
	return nethttp.StringURLDataResolveToMap(accept.Data)
}

// registerCookie 对应 Authentication.registerCookie:
// passwd == Data.config.webToken 时签发 24h Cookie, 否则 403.
func (a *authentication) registerCookie(json map[string]string, accept *nethttp.AcceptWeb, send *nethttp.SendWeb) {
	if origin := accept.GetHeaders("Origin"); origin != "" {
		// 上游: 允许跨域
		send.SetHead("Access-Control-Allow-Origin", origin)
		send.SetHead("Access-Control-Allow-Credentials", "true")
	}

	if json["passwd"] == global.Config.WebToken {
		cValue := algorithms.SHA256Hex([]byte(mathutil.GetRandomString(32)))
		send.SetCookie(cookieName, cValue, 86400, name)
		a.mu.Lock()
		a.cookiesData[cValue] = int64(timeutil.ConcurrentSecond()) + 86400
		a.mu.Unlock()
		send.SetDataString("Hi RW-HPS !")
		_ = send.Send()
		return
	}
	send.SetStatus(nethttp.StatusForbidden)
	send.SetDataString("[Forbidden] Input error")
	_ = send.Send()
}

// authenticate 对应 Authentication.authentication:
// 无 Cookie 401; Cookie 不存在或过期 401.
func (a *authentication) authenticate(cookies map[string]string, send *nethttp.SendWeb) bool {
	if len(cookies) == 0 {
		send.SetStatus(nethttp.StatusUnauthorized)
		send.SetDataString("[Unauthorized] Please certify")
		_ = send.Send()
		return false
	}

	a.mu.Lock()
	expiry := a.cookiesData[cookies[cookieName]]
	a.mu.Unlock()
	if expiry < int64(timeutil.ConcurrentSecond()) {
		send.SetStatus(nethttp.StatusUnauthorized)
		send.SetDataString("[Unauthorized] The cookies expires")
		_ = send.Send()
		return false
	}
	return true
}

// forwardingCenter 对应 MessageForwardingCenter: get/post 子路由分发
type forwardingCenter struct{}

// getCategorize 对应 getCategorize: 只读状态查询
//
// 上游四条路由, Go 版移植三条:
//   - event/GameOver / info/SystemInfo / info/GameInfo 照搬
//   - info/ModsInfo 依赖 ModManage (Go 无 Mod 系统), 走 else 分支回 404
func (f *forwardingCenter) getCategorize(accept *nethttp.AcceptWeb, send *nethttp.SendWeb) {
	send.SetConnectType("application/json")
	switch strings.TrimPrefix(accept.GetURL, "/"+name+"/api/get/") {
	case "event/GameOver":
		// 上游: 返回缓存内容后清空
		send.SetDataString(ToWebStatusJSON(jsonutil.ToJSON(DefaultGameOverCache.snapshotAndClear()), ""))
	case "info/SystemInfo":
		send.SetDataString(ToWebStatusJSON(jsonutil.ToJSON(SystemInfoSnapshot()), ""))
	case "info/GameInfo":
		send.SetDataString(ToWebStatusJSON(jsonutil.ToJSON(GameInfoSnapshot()), ""))
	default:
		// 上游 send404(false) + 末尾 send(): Go 版 Send404 即发即止, 语义等价
		send.Send404()
		return
	}
	_ = send.Send()
}

// postCategorize 对应 postCategorize:
// 上游两条路由 run/ServerCommand 与 run/ClientCommand; ClientCommand 依赖
// 上游 Cherry 假玩家 (player/unofficial/Cherry), Go 无对应物, 走 else 回 404.
func (f *forwardingCenter) postCategorize(accept *nethttp.AcceptWeb, send *nethttp.SendWeb) {
	send.SetConnectType("application/json")
	switch strings.TrimPrefix(accept.GetURL, "/"+name+"/api/post/") {
	case "run/ServerCommand":
		runServerCommand(accept, send)
	default:
		send.Send404()
		return
	}
	_ = send.Send()
}

// runServerCommand 对应 post/run/ServerCommand.post:
// 取 runCommand 字段执行 Data.SERVER_COMMAND, 输出捕获后包装返回.
func runServerCommand(accept *nethttp.AcceptWeb, send *nethttp.SendWeb) {
	command := postBodyToMap(accept)["runCommand"]
	if strings.TrimSpace(command) == "" {
		// 对应 AbstractPost.checkStringPost 的失败分支
		send.SetDataString(ToWebStatusJSON("", "Unknown parameter"))
		return
	}
	var result strings.Builder
	global.ServerCommand.HandleMessageWith(command, func(s string) { result.WriteString(s) })
	send.SetDataString(ToWebStatusJSON(result.String(), ""))
}
