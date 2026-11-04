package httpapi

// ConsoleWebSocket 对应上游 plugin/beta/http/ConsoleWebSocket.kt 与
// Authentication 里的 "/WebSocket/HttpApi/api/Console" 路由注册.
//
// 有意修正（作者已批准，2026-11-04）: 上游控制台 WebSocket 依赖 Netty 的
// WS 管线; Go 版在 internal/net/http 补齐升级握手后 (DECISIONS D-14),
// 本文件把端点从 not-portable 名单移回正式移植范围:
//   - 控制台输出转发: 对应上游 registerGlobalEvents 的 ServerConsolePrintEvent
//     监听 (consoleCache 缓存 + agentConsoleLog 实时推送), Go 版经
//     log.ConsoleHook 接入同一条日志出口
//   - 命令下发: runCommand 走 global.ServerCommand + DefPrint (上游同款)
//
// 消息协议与上游逐条一致 (JSON 文本帧):
//
//	{"cookie":"<AuthCookie 签发值>","type":"register"}  首帧鉴权, 应答 {"code":200,"data":"Register OK"}
//	{"type":"ping"}                                     应答 {"code":200,"data":"pong"}
//	{"type":"getConsole"}                               注册为控制台代理: 先回缓存 (200),
//	                                                    之后每条控制台输出推 {"code":1000,...};
//	                                                    重复注册回 {"code":403,"data":"Repeat"}
//	{"type":"runCommand","runCommand":"..."}            执行服务端命令
//	非法 JSON 回 {"code":400,...}; cookie 过期/缺失回 {"code":401,...}

import (
	"strings"
	"sync"

	"rwhps/internal/data/global"
	nethttp "rwhps/internal/net/http"
	"rwhps/internal/util/jsonutil"
	"rwhps/internal/util/log"
	"rwhps/internal/util/timeutil"
)

// consoleCache 对应 GetData.consoleCache (SeqSave(20)): 最近 20 条控制台输出
type consoleCache struct {
	mu   sync.Mutex
	data []string
	max  int
}

func newConsoleCache(max int) *consoleCache {
	return &consoleCache{max: max}
}

// add 对应 SeqSave.addSeq
func (c *consoleCache) add(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.data) >= c.max {
		c.data = c.data[1:]
	}
	c.data = append(c.data, s)
}

// snapshot 取全部缓存副本
func (c *consoleCache) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.data))
	copy(out, c.data)
	return out
}

// DefaultConsoleCache 对应 GetData.consoleCache 单例 (容量 20)
var DefaultConsoleCache = newConsoleCache(20)

// consoleAgents 对应 GetData.agentConsole + agentConsoleLog:
// 已订阅实时控制台输出的 WS 连接
type consoleAgents struct {
	mu     sync.Mutex
	agents map[string]func(line string)
}

var defaultConsoleAgents = &consoleAgents{agents: make(map[string]func(string))}

// add 登记一个代理; 重复返回 false (对应上游 agentConsole.contains 检查)
func (a *consoleAgents) add(key string, forward func(string)) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.agents[key]; ok {
		return false
	}
	a.agents[key] = forward
	return true
}

// remove 对应 closeWs 里的注销
func (a *consoleAgents) remove(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.agents, key)
}

// forward 把一条控制台输出推给全部代理
func (a *consoleAgents) forward(line string) {
	a.mu.Lock()
	targets := make([]func(string), 0, len(a.agents))
	for _, f := range a.agents {
		targets = append(targets, f)
	}
	a.mu.Unlock()
	for _, f := range targets {
		f(line)
	}
}

// installConsoleHook 对应上游 registerGlobalEvents 的 ServerConsolePrintEvent
// 监听: 每条控制台输出既进缓存又推给已订阅的 WS 代理.
//
// Go 版日志统一经 log.logs -> ConsoleHook 出口, 在 Register 时挂上.
func installConsoleHook() {
	log.ConsoleHook = func(line string) {
		DefaultConsoleCache.add(line)
		defaultConsoleAgents.forward(line)
	}
}

// wsStatus 对应 ConsoleWebSocket 私有 WebStatus(code, data)
//
// 注意与 httpapi.WebStatus (status/data 字符串) 不同: WS 控制台用的是
// 数字 code (200/400/401/403/1000), 照搬上游.
type wsStatus struct {
	Code int    `json:"code"`
	Data string `json:"data"`
}

func wsStatusJSON(code int, data string) string {
	return jsonutil.ToJSON(wsStatus{Code: code, Data: data})
}

// consoleWsHandler 实现 nethttp.WebSocketHandler, 对应
// Authentication 注册的 WebSocket 路由体 + ConsoleWebSocket.
type consoleWsHandler struct {
	auth *authentication

	mu       sync.Mutex
	sessions map[nethttp.Connection]*consoleSession
}

// consoleSession 对应上游挂在 channel attr 上的 ConsoleWebSocket 实例状态
type consoleSession struct {
	authed bool
	agent  bool
}

func newConsoleWsHandler(auth *authentication) *consoleWsHandler {
	return &consoleWsHandler{auth: auth, sessions: make(map[nethttp.Connection]*consoleSession)}
}

// OnMessage 对应 ws(channel, json)
func (h *consoleWsHandler) OnMessage(_ *nethttp.StartWebSocket, conn nethttp.Connection, msg string) {
	var m map[string]any
	if err := jsonutil.FromJSON(msg, &m); err != nil {
		_ = conn.WriteString(wsStatusJSON(400, "[Bad Request] 请传入正确 Json !"))
		return
	}

	sess := h.sessionOf(conn)

	// 上游首帧鉴权: cookie 字段必须在 cookiesData 里且未过期
	if !sess.authed {
		cookie, _ := m["cookie"].(string)
		if !h.auth.cookieValid(cookie) {
			_ = conn.WriteString(wsStatusJSON(401, "[Unauthorized] The cookies expires"))
			return
		}
		sess.authed = true
	}

	switch m["type"] {
	case "register":
		_ = conn.WriteString(wsStatusJSON(200, "Register OK"))
	case "ping":
		_ = conn.WriteString(wsStatusJSON(200, "pong"))
	case "getConsole":
		key := agentKey(conn)
		if !defaultConsoleAgents.add(key, func(line string) {
			// 对应上游 agentConsoleLog 回调: code 1000 实时推送
			_ = conn.WriteString(wsStatusJSON(1000, line))
		}) {
			_ = conn.WriteString(wsStatusJSON(403, "Repeat"))
			return
		}
		sess.agent = true
		// 对应上游: 先把 consoleCache 用 LINE_SEPARATOR 连接后整体下发
		_ = conn.WriteString(wsStatusJSON(200, strings.Join(DefaultConsoleCache.snapshot(), global.LineSeparator)))
	case "runCommand":
		// 对应上游: Data.SERVER_COMMAND.handleMessage(json.getString("runCommand"), Data.defPrint)
		command, _ := m["runCommand"].(string)
		global.ServerCommand.HandleMessageWith(command, global.DefPrint)
	}
}

// OnClose 对应 closeWs(channel): 注销代理与会话
func (h *consoleWsHandler) OnClose(_ *nethttp.StartWebSocket, conn nethttp.Connection) {
	h.mu.Lock()
	sess := h.sessions[conn]
	delete(h.sessions, conn)
	h.mu.Unlock()

	if sess != nil && sess.agent {
		defaultConsoleAgents.remove(agentKey(conn))
	}
}

// sessionOf 取/建连接级会话状态
func (h *consoleWsHandler) sessionOf(conn nethttp.Connection) *consoleSession {
	h.mu.Lock()
	defer h.mu.Unlock()
	sess, ok := h.sessions[conn]
	if !ok {
		sess = &consoleSession{}
		h.sessions[conn] = sess
	}
	return sess
}

// agentKey 对应上游 channel.id(): 以连接地址作唯一键
func agentKey(conn nethttp.Connection) string {
	type addrIface interface{ RemoteAddr() string }
	if a, ok := conn.(addrIface); ok {
		return a.RemoteAddr()
	}
	return ""
}

// cookieValid 对应 Authentication 里对 cookiesData 的过期判断:
// 存在且过期时间不早于当前秒才算有效.
func (a *authentication) cookieValid(cookie string) bool {
	if cookie == "" {
		return false
	}
	a.mu.Lock()
	expiry := a.cookiesData[cookie]
	a.mu.Unlock()
	return expiry >= int64(timeutil.ConcurrentSecond())
}

// registerConsoleWebSocket 对应 Authentication.registerAuthenticationCenter 里
// addWebSocketInstance("/WebSocket/HttpApi/api/Console", ...) 的一段.
func (a *authentication) registerConsoleWebSocket(webData *nethttp.WebData) {
	if err := webData.AddWebSocketInstance("/WebSocket/"+name+"/api/Console", newConsoleWsHandler(a)); err != nil {
		log.Errorf("[HttpApi]", "{0}", err)
	}
}
