// Package http 对应 net.rwhps.server.net.http
package http

import "errors"

// WebGet 对应 core.web.WebGet (抽象类)
// 由于 Go 无类继承, 这里定义为接口
type WebGet interface {
	Get(accept *AcceptWeb, send *SendWeb)
}

// WebPost 对应 core.web.WebPost (抽象类)
type WebPost interface {
	Post(accept *AcceptWeb, send *SendWeb)
	// StringPostDataResolveToJson 原版 protected 工具方法, 暴露以便子类调用
	StringPostDataResolveToJson(accept *AcceptWeb) (map[string]string, error)
}

// WebSocketHandler 对应 core.web.WebSocket
type WebSocketHandler interface {
	OnMessage(ws *StartWebSocket, conn Connection, msg string)
	OnClose(ws *StartWebSocket, conn Connection)
}

// Connection 对应 Netty Channel 在 WebSocket 上下文中的最小投影
type Connection interface {
	WriteString(msg string) error
	Close() error
}

// WSUri 对应 WebData.Companion.WS_URI
const WSUri = "/WebSocket"

// Error 实例
var (
	ErrRepeatAdd = errors.New("[AddWebInstance] Repeat Add")
	ErrTabooAdd  = errors.New("[AddWebInstance] TabooA Add, Can not be used WebSocket URL")
)

// WebData 对应 net.rwhps.server.net.http.WebData
// 路由注册表: 精确匹配 + 单层通配 (`/xxx/*`) + 全通配 (`/prefix**`)。
type WebData struct {
	getData             map[string]WebGet
	postData            map[string]WebPost
	getWildcardAll      map[string]WebGet // key 已去掉 ** 结尾
	postWildcardAll     map[string]WebPost
	webSocketData       map[string]*StartWebSocket
	getOrder            []string // 保持有序, 用于稳定遍历 (虽然精确 + 全通配场景不需要顺序, 留作未来扩展)
	postOrder           []string
	getWildcardOrder    []string
	postWildcardOrder   []string
}

// NewWebData 创建空 WebData
func NewWebData() *WebData {
	return &WebData{
		getData:           make(map[string]WebGet),
		postData:          make(map[string]WebPost),
		getWildcardAll:    make(map[string]WebGet),
		postWildcardAll:   make(map[string]WebPost),
		webSocketData:     make(map[string]*StartWebSocket),
		getOrder:          []string{},
		postOrder:         []string{},
		getWildcardOrder:  []string{},
		postWildcardOrder: []string{},
	}
}

// AddWebGetInstance 对应 addWebGetInstance
func (w *WebData) AddWebGetInstance(url string, webGet WebGet) error {
	if _, ok := w.getData[url]; ok {
		return ErrRepeatAdd
	}
	if startsWith(url, WSUri) {
		return ErrTabooAdd
	}
	if endsWith(url, "**") {
		key := url[:len(url)-2]
		w.getWildcardAll[key] = webGet
		w.getWildcardOrder = append(w.getWildcardOrder, key)
	} else {
		w.getData[url] = webGet
		w.getOrder = append(w.getOrder, url)
	}
	return nil
}

// AddWebPostInstance 对应 addWebPostInstance
func (w *WebData) AddWebPostInstance(url string, webPost WebPost) error {
	if _, ok := w.postData[url]; ok {
		return ErrRepeatAdd
	}
	if startsWith(url, WSUri) {
		return ErrTabooAdd
	}
	if endsWith(url, "**") {
		key := url[:len(url)-2]
		w.postWildcardAll[key] = webPost
		w.postWildcardOrder = append(w.postWildcardOrder, key)
	} else {
		w.postData[url] = webPost
		w.postOrder = append(w.postOrder, url)
	}
	return nil
}

// AddWebSocketInstance 对应 addWebSocketInstance
func (w *WebData) AddWebSocketInstance(url string, ws WebSocketHandler) error {
	if _, ok := w.webSocketData[url]; ok {
		return ErrRepeatAdd
	}
	w.webSocketData[url] = NewStartWebSocket(ws)
	return nil
}

// RemoveWebGetInstance 对应 removeWebGetInstance
func (w *WebData) RemoveWebGetInstance(url string) {
	delete(w.getData, url)
}

// RemoveWebPostInstance 对应 removeWebPostInstance
func (w *WebData) RemoveWebPostInstance(url string) {
	delete(w.postData, url)
}

// RemoveWebSocketInstance 对应 removeWebSocketInstance
func (w *WebData) RemoveWebSocketInstance(url string) {
	delete(w.webSocketData, url)
}

// splitURL 还原 URL 与 query string; 与原版语义一致
func splitURL(url string) (path, query string) {
	if i := lastIndex(url, "?"); i >= 0 {
		path = url[:i]
		if i+1 < len(url) {
			query = url[i+1:]
		}
		return path, query
	}
	return url, ""
}

// wildcardOf 对应 "${url.substring(0, url.lastIndexOf("/") + 1)}*"
func wildcardOf(url string) string {
	if i := lastIndex(url, "/"); i >= 0 {
		return url[:i+1] + "*"
	}
	return "*"
}

// runWildcardAll 与原版 eachControl 遍历一致, 返回 true 表示已处理
func runWildcardAll(table map[string]WebGet, order []string, url string, accept *AcceptWeb, send *SendWeb) bool {
	for _, k := range order {
		if startsWith(url, k) {
			if h, ok := table[k]; ok {
				h.Get(accept, send)
				return true
			}
		}
	}
	return false
}

// runPostWildcardAll 同上但用于 POST
func runPostWildcardAll(table map[string]WebPost, order []string, url string, accept *AcceptWeb, send *SendWeb) bool {
	for _, k := range order {
		if startsWith(url, k) {
			if h, ok := table[k]; ok {
				h.Post(accept, send)
				return true
			}
		}
	}
	return false
}

// RunWebGetInstance 对应 runWebGetInstance(url, request, sendWeb)
func (w *WebData) RunWebGetInstance(url string, request *HttpRequestLite, sendWeb *SendWeb) {
	getUrl, urlData := splitURL(url)
	wildcard := wildcardOf(url)
	accept := NewAcceptWeb(getUrl, urlData, "", request)

	if h, ok := w.getData[getUrl]; ok {
		h.Get(accept, sendWeb)
		return
	}
	if h, ok := w.getData[wildcard]; ok {
		h.Get(accept, sendWeb)
		return
	}
	if runWildcardAll(w.getWildcardAll, w.getWildcardOrder, url, accept, sendWeb) {
		return
	}
	sendWeb.Send404()
}

// RunWebPostInstance 对应 runWebPostInstance(url, data, request, sendWeb)
func (w *WebData) RunWebPostInstance(url, data string, request *HttpRequestLite, sendWeb *SendWeb) {
	getUrl, urlData := splitURL(url)
	wildcard := wildcardOf(url)
	accept := NewAcceptWeb(getUrl, urlData, data, request)

	if h, ok := w.postData[getUrl]; ok {
		h.Post(accept, sendWeb)
		return
	}
	if h, ok := w.postData[wildcard]; ok {
		h.Post(accept, sendWeb)
		return
	}
	if runPostWildcardAll(w.postWildcardAll, w.postWildcardOrder, url, accept, sendWeb) {
		return
	}
	sendWeb.Send404()
}

// RunWebSocketInstance 对应 runWebSocketInstance
func (w *WebData) RunWebSocketInstance(url string) *StartWebSocket {
	if h, ok := w.webSocketData[url]; ok {
		return h
	}
	return nil
}

// --- 字符串辅助: 避免引入 strings 包以减少依赖 ---

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// lastIndex 对应 String.lastIndexOf, 用于查找字节位置 (URL 仅含 ASCII 安全)
func lastIndex(s, sub string) int {
	if len(sub) == 0 {
		return len(s)
	}
	for i := len(s) - len(sub); i >= 0; i-- {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}