// Package http 对应 net.rwhps.server.net.http
package http

import (
	"bufio"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"

	"rwhps/internal/data/global"
	"rwhps/internal/net/core"
	"rwhps/internal/util/log"
)

// ConnIO 是 HTTP 层所需的最小连接投影 (便于测试用 in-memory conn 驱动)
type ConnIO interface {
	io.Reader
	io.Writer
	io.Closer
}

// addrConn 可选投影: 提供对端地址
type addrConn interface {
	RemoteAddr() net.Addr
}

// StartHttp 对应 net.rwhps.server.net.handler.tcp.StartHttp
// 嗅探首字节: "GET "/POST "/"DELETE "/"HEAD "/"PUT " 即视为 HTTP, 接管该连接.
// WebSocket (以 "GET /WebSocket" 开头) 升级为 ws:// 协议, 本实现维持最小占位 (走 gorilla websocket 与本项目之外的依赖, 当前 TODO).
type StartHttp struct {
	rwHps    core.IRwHps
	webData  *WebData
}

// NewStartHttp 对应 StartHttp(rwHps).init()
func NewStartHttp(rwHps core.IRwHps) *StartHttp {
	return &StartHttp{rwHps: rwHps}
}

// SetWebData 对应 setWebData
func (s *StartHttp) SetWebData(d *WebData) { s.webData = d }

// IsHttpReq 对应 isHttpReq(head)
func (s *StartHttp) IsHttpReq(head string) bool {
	return startsWith(head, "GET ") || startsWith(head, "POST ") ||
		startsWith(head, "DELETE ") || startsWith(head, "HEAD ") ||
		startsWith(head, "PUT ")
}

// HandleChannel 在嗅探到 HTTP 流量时调用, 负责完整 HTTP/1.1 请求循环.
func (s *StartHttp) HandleChannel(ch *core.Channel) {
	defer func() {
		_ = ch.Close()
	}()
	s.HandleConn(ch.Conn)
}

// HandleConn 与 HandleChannel 等价, 但直接接受底层 conn-like 接口, 便于测试驱动.
func (s *StartHttp) HandleConn(conn ConnIO) {
	defer func() {
		_ = conn.Close()
	}()
	reader := bufio.NewReader(conn)
	for {
		req, err := readRequest(reader, conn)
		if err != nil {
			if !errors.Is(err, errEOF) {
				log.Errorf("[HTTP] read req err: %v", err)
			}
			return
		}
		if req == nil {
			return
		}
		s.dispatch(conn, req)
		if !req.KeepAlive() {
			return
		}
	}
}

// dispatch 派发请求到 WebData; WebSocket 占位 (升级握手暂不实现).
func (s *StartHttp) dispatch(conn io.WriteCloser, req *HttpRequestLite) {
	if s.webData == nil {
		// 与原版一致: webData 未注入时静默返回
		return
	}
	// WebSocket 升级路径 (占位): "GET /WebSocket" 开头, 真正握手交给未来扩展
	if req.Method == "GET" && startsWith(req.URL, WSUri) {
		// 此处应完成 101 Switching Protocols 升级. 当前实现回 404 以保持兼容.
		writeNotFound(conn, req, keepAlive(req))
		return
	}

	// 校验 WebHOST
	if wh := strings.TrimSpace(global.Config.WebHOST); wh != "" {
		if host := req.GetHeader("Host"); host != wh {
			sendBadRequest(conn, req, keepAlive(req))
			return
		}
	}

	send := NewSendWeb(func(status int, headers map[string][]string, body []byte) error {
		return writeResponse(conn, req, status, headers, body)
	}, keepAlive(req))

	switch req.Method {
	case "GET":
		s.webData.RunWebGetInstance(req.URL, req, send)
	case "POST":
		// *** POST 修复: 先聚合完整请求再取 body ***
		s.webData.RunWebPostInstance(req.URL, req.PostBody, req, send)
	case "DELETE":
		fallthrough
	case "HEAD":
		fallthrough
	case "PUT":
		// 原版同样由 startHttp 接管, 但 Kotlin 实际只处理 GET/POST.
		// 这里与 Kotlin 行为一致: 其他方法回 404.
		send.Send404()
	default:
		send.Send404()
	}
}

// keepAlive 对应 HttpUtil.isKeepAlive: 没显式 Connection: close 则视为 true (HTTP/1.1 默认持久连接)
func keepAlive(req *HttpRequestLite) bool {
	if req.Proto == "HTTP/1.0" {
		// HTTP/1.0 默认 close, 但若有显式 keep-alive 则持久 (这里简化, 默认 false)
		if v := strings.ToLower(req.GetHeader("Connection")); strings.Contains(v, "keep-alive") {
			return true
		}
		return false
	}
	if strings.ToLower(req.GetHeader("Connection")) == "close" {
		return false
	}
	return true
}

// readRequest 读取完整 HTTP/1.1 请求 (请求行 + 头 + body by Content-Length)
func readRequest(r *bufio.Reader, conn ConnIO) (*HttpRequestLite, error) {
	reqLine, err := readLine(r)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(reqLine, " ")
	if len(parts) < 3 {
		return nil, errBadRequest
	}
	method, url, proto := parts[0], parts[1], parts[2]

	req := &HttpRequestLite{
		Method:     method,
		URL:        url,
		Proto:      proto,
		Headers:    make(map[string][]string),
		RemoteAddr: remoteAddrOf(conn),
	}

	// 头
	for {
		line, err := readLine(r)
		if err != nil {
			return nil, err
		}
		if line == "" {
			break
		}
		idx := strings.IndexByte(line, ':')
		if idx < 0 {
			continue
		}
		k := strings.TrimSpace(line[:idx])
		v := strings.TrimSpace(line[idx+1:])
		req.Headers[k] = append(req.Headers[k], v)
		if strings.EqualFold(k, "Host") {
			// Host 只有一个值, 但 Headers 是 map, 这里覆盖式存放
			req.Headers[k] = []string{v}
		}
	}

	// query string
	if i := strings.IndexByte(req.URL, '?'); i >= 0 {
		req.Query = req.URL[i+1:]
	}

	// body (仅对有 Content-Length 的请求生效)
	cl := req.GetHeader("Content-Length")
	if cl != "" {
		n, err := strconv.Atoi(cl)
		if err != nil || n < 0 {
			return nil, errBadRequest
		}
		if n > 0 {
			buf := make([]byte, n)
			if _, err := readFull(r, buf); err != nil {
				return nil, err
			}
			req.PostBody = string(buf)
		}
	}
	return req, nil
}

// readLine 读取一行 (去掉结尾 \r\n)
func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	return line, nil
}

// readFull 跨 bufio.Reader 与原始 conn 安全读取 (bufio 缓冲不足时回到 conn)
func readFull(r *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			if total == len(buf) {
				return total, nil
			}
			return total, err
		}
		if n == 0 {
			break
		}
	}
	return total, nil
}

// writeResponse 直接在 conn 上写出 HTTP/1.1 响应
func writeResponse(conn io.Writer, req *HttpRequestLite, status int, headers map[string][]string, body []byte) error {
	if status == 0 {
		status = StatusOK
	}
	statusLine := req.Proto + " " + strconv.Itoa(status) + " " + statusText(status) + "\r\n"
	var b strings.Builder
	b.WriteString(statusLine)
	for k, vs := range headers {
		for _, v := range vs {
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(v)
			b.WriteString("\r\n")
		}
	}
	b.WriteString("\r\n")
	if _, err := conn.Write([]byte(b.String())); err != nil {
		return err
	}
	if len(body) > 0 {
		if _, err := conn.Write(body); err != nil {
			return err
		}
	}
	return nil
}

// writeNotFound 简化的内部 404 (用于 StartHttp 占位)
func writeNotFound(conn io.Writer, req *HttpRequestLite, keepAlive bool) {
	body := []byte("[404] File not found\n")
	headers := map[string][]string{
		"Server":         {"RW-HPS/" + global.ServerCoreVersion + " (WebData)"},
		"Content-Type":   {"text/html;charset=utf-8"},
		"Accept-Charset": {"UTF-8"},
		"Content-Length": {strconv.Itoa(len(body))},
		"Connection":     connectionHeader(keepAlive),
	}
	_ = writeResponse(conn, req, StatusNotFound, headers, body)
}

// sendBadRequest 简化的内部 400
func sendBadRequest(conn io.Writer, req *HttpRequestLite, keepAlive bool) {
	body := []byte("[400] Bad request\n")
	headers := map[string][]string{
		"Server":         {"RW-HPS/" + global.ServerCoreVersion + " (WebData)"},
		"Content-Type":   {"text/html;charset=utf-8"},
		"Accept-Charset": {"UTF-8"},
		"Content-Length": {strconv.Itoa(len(body))},
		"Connection":     connectionHeader(keepAlive),
	}
	_ = writeResponse(conn, req, StatusBadRequest, headers, body)
}

func connectionHeader(keepAlive bool) []string {
	if keepAlive {
		return []string{"keep-alive"}
	}
	return []string{"close"}
}

// remoteAddrOf 尽力取出对端地址, 不可用时返回空串
func remoteAddrOf(conn ConnIO) string {
	if a, ok := conn.(addrConn); ok {
		if addr := a.RemoteAddr(); addr != nil {
			return addr.String()
		}
	}
	return ""
}

// statusText 返回 HTTP 状态码文本
func statusText(code int) string {
	switch code {
	case StatusOK:
		return "OK"
	case StatusBadRequest:
		return "Bad Request"
	case StatusNotFound:
		return "Not Found"
	}
	return "OK"
}

var (
	errBadRequest = errors.New("http: bad request")
	errEOF        = errors.New("http: eof")
)

// KeepAlive 对应 HttpRequestLite 的 KeepAlive 判定.
func (r *HttpRequestLite) KeepAlive() bool { return keepAlive(r) }

// IsWebSocketUpgrade 对应 startHttp 中 headS.startsWith("GET ${WS_URI}") 判断
func IsWebSocketUpgrade(head string) bool {
	return startsWith(head, "GET "+WSUri)
}