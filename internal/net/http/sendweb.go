// Package http 对应 net.rwhps.server.net.http
package http

import (
	"strconv"
	"strings"
	"sync"

	"rwhps/internal/data/global"
)

// 状态码常量
const (
	StatusOK         = 200
	StatusBadRequest = 400
	StatusNotFound   = 404
)

// SendWeb 对应 net.rwhps.server.net.http.SendWeb
// 使用底层 http.ResponseWriter 风格的 WriteFunc 写出响应, 替代 Netty 的 Channel.writeAndFlush.
type SendWeb struct {
	mu        sync.Mutex
	status    int
	replaceHd map[string]string
	appendHd  map[string][]string
	cacheData []byte

	// Writer 是由 StartHttp 在请求生命周期内注入的写出函数
	Writer func(status int, headers map[string][]string, body []byte) error
	// KeepAlive 由 StartHttp 注入
	KeepAlive bool
}

// NewSendWeb 对应 SendWeb(channel, request)
func NewSendWeb(writer func(status int, headers map[string][]string, body []byte) error, keepAlive bool) *SendWeb {
	return &SendWeb{
		status:    StatusOK,
		replaceHd: defaultHeaders(),
		appendHd:  map[string][]string{},
		Writer:    writer,
		KeepAlive: keepAlive,
	}
}

// defaultHeaders 对应 SendWeb 构造时的默认 replaceHeaders
func defaultHeaders() map[string]string {
	return map[string]string{
		"Server":         "RW-HPS/" + global.ServerCoreVersion + " (WebData)",
		"Content-Type":   "text/html;charset=utf-8",
		"Accept-Charset": "UTF-8",
	}
}

// SetHead 对应 setHead(key, value), 覆盖默认 header
func (s *SendWeb) SetHead(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replaceHd[key] = value
}

// CustomAppendHead 对应 customAppendHead, 可重复的 header
func (s *SendWeb) CustomAppendHead(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appendHd[key] = append(s.appendHd[key], value)
}

// SetConnectType 对应 setConnectType(String), 覆盖 Content-Type
func (s *SendWeb) SetConnectType(t string) {
	s.SetHead("Content-Type", t+";charset=UTF-8")
}

// SetCookie 对应 setCookie, 附加 Set-Cookie 头
func (s *SendWeb) SetCookie(cKey, cValue string, maxAge int, path string) {
	s.CustomAppendHead("Set-Cookie",
		cKey+"="+cValue+"; Max-Age="+strconv.Itoa(maxAge)+"; Path="+path)
}

// SetDataBytes 对应 setData(ByteArray)
func (s *SendWeb) SetDataBytes(b []byte) {
	s.SetConnectType("multipart/form-data")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cacheData = b
}

// SetDataString 对应 setData(String)
func (s *SendWeb) SetDataString(str string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cacheData = []byte(str)
}

// Send 对应 send(); 写出响应
func (s *SendWeb) Send() error {
	if s.Writer == nil {
		return nil
	}
	if s.cacheData == nil {
		return nil // 不主动 panic, 与原版语义接近 (这里改返回错误)
	}
	headers := make(map[string][]string, len(s.replaceHd)+len(s.appendHd)+1)
	for k, v := range s.replaceHd {
		headers[k] = []string{v}
	}
	for k, vs := range s.appendHd {
		headers[k] = append(headers[k], vs...)
	}
	headers["Content-Length"] = []string{strconv.Itoa(len(s.cacheData))}
	return s.Writer(s.status, headers, s.cacheData)
}

// writeHeaders 内部合并 headers (供 404/BadRequest 等调用)
func (s *SendWeb) writeHeaders() map[string][]string {
	h := make(map[string][]string, len(s.replaceHd)+len(s.appendHd))
	for k, v := range s.replaceHd {
		h[k] = []string{v}
	}
	for k, vs := range s.appendHd {
		h[k] = append(h[k], vs...)
	}
	return h
}

// Send404 对应 send404()
func (s *SendWeb) Send404() {
	s.status = StatusNotFound
	s.SetConnectType("text/html")
	body := `<p>[404] File not found
    <br>The project is based on
        <strong>RW-HPS</strong>
        <br>
            <a href="https://github.com/deng-rui/RW-HPS">RW-HPS Github</a>
        </p>`
	s.cacheData = []byte(strings.TrimSpace(body))
	_ = s.Send()
}

// SendBedRequest 对应 sendBedRequest()
func (s *SendWeb) SendBedRequest() {
	s.status = StatusBadRequest
	s.SetConnectType("text/html")
	body := `<p>[400] Can't find a match HOST
    <br>The project is based on
        <strong>RW-HPS</strong>
        <br>
            <a href="https://github.com/deng-rui/RW-HPS">RW-HPS Github</a>
        </p>`
	s.cacheData = []byte(strings.TrimSpace(body))
	_ = s.Send()
}