// Package http 对应 net.rwhps.server.net.http
package http

// HttpRequestLite 是解析后的轻量 HTTP 请求结构 (服务端协作用于传递元数据到 WebGet/WebPost/WebSocket)
type HttpRequestLite struct {
	Method     string
	URL        string
	Proto      string // 例如 "HTTP/1.1"
	Headers    map[string][]string
	Query      string // "?key=value&..."
	RemoteAddr string
	// PostBody 聚合后的请求体 (原版因 pipeline 顺序错误始终为空, 此处按正确语义填充)
	PostBody string
}

// GetHeader 获取 header 的首值 (大小写不敏感)
func (r *HttpRequestLite) GetHeader(key string) string {
	if r == nil {
		return ""
	}
	for k, v := range r.Headers {
		if asciiEqualIgnoreCase(k, key) && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

// AcceptWeb 对应 net.rwhps.server.net.http.AcceptWeb
type AcceptWeb struct {
	GetURL  string
	URLData string
	Data    string
	Request *HttpRequestLite
}

// NewAcceptWeb 对应 AcceptWeb(getUrl, urlData, data, request)
func NewAcceptWeb(getURL, urlData, data string, request *HttpRequestLite) *AcceptWeb {
	return &AcceptWeb{GetURL: getURL, URLData: urlData, Data: data, Request: request}
}

// GetHeaders 对应 getHeaders(key); 大小写不敏感
func (a *AcceptWeb) GetHeaders(key string) string {
	if a == nil || a.Request == nil {
		return ""
	}
	return a.Request.GetHeader(key)
}

// asciiEqualIgnoreCase 对比 ASCII 字符串忽略大小写
func asciiEqualIgnoreCase(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca := a[i]
		cb := b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}