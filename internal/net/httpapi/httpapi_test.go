package httpapi

import (
	"strings"
	"testing"

	"rwhps/internal/data/global"
	"rwhps/internal/game"
	"rwhps/internal/game/event"
	"rwhps/internal/game/room"
	nethttp "rwhps/internal/net/http"
)

// captured 记录 SendWeb 写出的响应
type captured struct {
	status  int
	headers map[string][]string
	body    string
}

func newCaptureSend() (*nethttp.SendWeb, *captured) {
	c := &captured{}
	send := nethttp.NewSendWeb(func(status int, headers map[string][]string, body []byte) error {
		c.status = status
		c.headers = headers
		c.body = string(body)
		return nil
	}, false)
	return send, c
}

func getRequest(url, query string, headers map[string][]string) *nethttp.HttpRequestLite {
	if headers == nil {
		headers = map[string][]string{}
	}
	return &nethttp.HttpRequestLite{Method: "GET", URL: url, Proto: "HTTP/1.1", Headers: headers, Query: query}
}

func setupWebToken(t *testing.T) {
	t.Helper()
	old := global.Config.WebToken
	global.Config.WebToken = "test-token"
	t.Cleanup(func() { global.Config.WebToken = old })
}

// TestToWebStatusJSON 对应 WebStatus.toWebStatusJson
func TestToWebStatusJSON(t *testing.T) {
	if got := ToWebStatusJSON("hello", ""); !strings.Contains(got, `"status":"OK"`) || !strings.Contains(got, `"data":"hello"`) {
		t.Fatalf("ToWebStatusJSON = %q", got)
	}
	if got := ToWebStatusJSON("x", "ERR"); !strings.Contains(got, `"status":"ERR"`) {
		t.Fatalf("ToWebStatusJSON = %q", got)
	}
}

// TestGameOverCache 对应 SeqSave(10) 语义: 满时移除最旧, 取走后清空
func TestGameOverCache(t *testing.T) {
	c := newGameOverCache(3)
	c.add("a")
	c.add("b")
	c.add("c")
	c.add("d") // 挤出 a
	got := c.snapshotAndClear()
	if strings.Join(got, ",") != "b,c,d" {
		t.Fatalf("缓存内容 = %v", got)
	}
	if c.size() != 0 {
		t.Fatal("取走后应清空")
	}
}

// TestRegisterRoomEvents 对应 HttpApiEvent: 非空 GameOverData 写入缓存, nil 跳过
func TestRegisterRoomEvents(t *testing.T) {
	oldCache := DefaultGameOverCache
	DefaultGameOverCache = newGameOverCache(10)
	t.Cleanup(func() { DefaultGameOverCache = oldCache })

	r := room.NewServerRoom(game.NewGameSession("httpapi-room", nil))
	RegisterRoomEvents(r)

	// nil GameOverData: 与上游 `gameOverData?.run` 一致, 不写入
	r.EventManage.Fire(&event.ServerGameOverEvent{GameOverData: nil})
	if DefaultGameOverCache.size() != 0 {
		t.Fatal("nil 结算数据不应写入缓存")
	}

	r.EventManage.Fire(&event.ServerGameOverEvent{GameOverData: &event.GameOverData{GameTime: 42}})
	if DefaultGameOverCache.size() != 1 {
		t.Fatalf("缓存条数 = %d, 期望 1", DefaultGameOverCache.size())
	}
}

// TestAuthCookieFlow 对应 Authentication.registerCookie + authentication
func TestAuthCookieFlow(t *testing.T) {
	setupWebToken(t)
	wd := nethttp.NewWebData()
	Register(wd)

	// 密码错误 -> 403 (查询串并入 URL, RunWebGetInstance 按 url 参数切分)
	send, c := newCaptureSend()
	wd.RunWebGetInstance("/"+name+"/api/AuthCookie?passwd=wrong", getRequest("/"+name+"/api/AuthCookie", "passwd=wrong", nil), send)
	if c.status != nethttp.StatusForbidden {
		t.Fatalf("错误密码应 403, got %d", c.status)
	}

	// 密码正确 -> 200 + Set-Cookie
	send, c = newCaptureSend()
	wd.RunWebGetInstance("/"+name+"/api/AuthCookie?passwd=test-token", getRequest("/"+name+"/api/AuthCookie", "passwd=test-token", nil), send)
	if c.status != nethttp.StatusOK {
		t.Fatalf("正确密码应 200, got %d", c.status)
	}
	if c.body != "Hi RW-HPS !" {
		t.Fatalf("响应体 = %q", c.body)
	}
	cookies := c.headers["Set-Cookie"]
	if len(cookies) == 0 || !strings.Contains(cookies[0], cookieName+"=") {
		t.Fatalf("缺少 Set-Cookie: %v", c.headers)
	}
	cookieVal := strings.TrimPrefix(strings.SplitN(cookies[0], ";", 2)[0], cookieName+"=")

	// 无 Cookie 访问 get/** -> 401
	send, c = newCaptureSend()
	wd.RunWebGetInstance("/"+name+"/api/get/info/SystemInfo", getRequest("/"+name+"/api/get/info/SystemInfo", "", nil), send)
	if c.status != nethttp.StatusUnauthorized {
		t.Fatalf("无 Cookie 应 401, got %d", c.status)
	}

	// 带 Cookie 访问 info/SystemInfo -> 200 + JSON
	send, c = newCaptureSend()
	req := getRequest("/"+name+"/api/get/info/SystemInfo", "", map[string][]string{
		"Cookie": {cookieName + "=" + cookieVal},
	})
	wd.RunWebGetInstance("/"+name+"/api/get/info/SystemInfo", req, send)
	if c.status != nethttp.StatusOK {
		t.Fatalf("带 Cookie 应 200, got %d", c.status)
	}
	// data 字段是二次序列化的 JSON 字符串 (引号被转义, 与上游 toJson 行为一致)
	if !strings.Contains(c.body, `"status":"OK"`) || !strings.Contains(c.body, `system`) {
		t.Fatalf("SystemInfo 响应异常: %q", c.body)
	}

	// 未知 get 子路由 -> 404
	send, c = newCaptureSend()
	wd.RunWebGetInstance("/"+name+"/api/get/info/Nope", getRequest("/"+name+"/api/get/info/Nope", "", map[string][]string{
		"Cookie": {cookieName + "=" + cookieVal},
	}), send)
	if c.status != nethttp.StatusNotFound {
		t.Fatalf("未知路由应 404, got %d", c.status)
	}
}

// TestRunServerCommand 对应 post/run/ServerCommand
func TestRunServerCommand(t *testing.T) {
	setupWebToken(t)
	wd := nethttp.NewWebData()
	Register(wd)

	// 先拿 Cookie
	send, c := newCaptureSend()
	wd.RunWebGetInstance("/"+name+"/api/AuthCookie?passwd=test-token", getRequest("/"+name+"/api/AuthCookie", "passwd=test-token", nil), send)
	if c.status != nethttp.StatusOK || len(c.headers["Set-Cookie"]) == 0 {
		t.Fatalf("AuthCookie 应 200 且带 Set-Cookie, got %d %v", c.status, c.headers)
	}
	cookieVal := strings.TrimPrefix(strings.SplitN(c.headers["Set-Cookie"][0], ";", 2)[0], cookieName+"=")

	// 注册一条可观察的命令
	global.ServerCommand.Register("httpapiprobe", "test", func(args []string, param any) {
		if fn, ok := param.(func(string)); ok {
			fn("probe-ok")
		}
	})
	t.Cleanup(func() { global.ServerCommand.RemoveCommand("httpapiprobe") })

	// 带 Cookie 执行命令
	send, c = newCaptureSend()
	req := &nethttp.HttpRequestLite{
		Method: "POST", URL: "/" + name + "/api/post/run/ServerCommand", Proto: "HTTP/1.1",
		Headers: map[string][]string{
			"Cookie":       {cookieName + "=" + cookieVal},
			"Content-Type": {"application/json"},
		},
		PostBody: `{"runCommand":"httpapiprobe"}`,
	}
	wd.RunWebPostInstance("/"+name+"/api/post/run/ServerCommand", req.PostBody, req, send)
	if c.status != nethttp.StatusOK {
		t.Fatalf("run/ServerCommand 应 200, got %d", c.status)
	}
	if !strings.Contains(c.body, "probe-ok") {
		t.Fatalf("响应应包含命令输出: %q", c.body)
	}

	// runCommand 为空 -> Unknown parameter
	send, c = newCaptureSend()
	req.PostBody = `{"runCommand":""}`
	wd.RunWebPostInstance("/"+name+"/api/post/run/ServerCommand", req.PostBody, req, send)
	if !strings.Contains(c.body, "Unknown parameter") {
		t.Fatalf("空命令应返回 Unknown parameter: %q", c.body)
	}
}
