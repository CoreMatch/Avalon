// Package manage 对应 net.rwhps.server.net.manage
package manage

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"rwhps/internal/util/log"
)

// HttpRequestManage 对应 HttpRequestManage (OkHttp 的 net/http 替代)
type HttpRequestManage struct {
	userAgent string
	client    *http.Client
}

// NewHttpRequestManage 对应 HttpRequestManage(client, userAgent)
func NewHttpRequestManage(userAgent string) *HttpRequestManage {
	return &HttpRequestManage{
		userAgent: userAgent,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

// DoGet 对应 doGet(url), 失败返回空串
func (h *HttpRequestManage) DoGet(rawURL string) string {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		log.Errorf("[HttpRequest]", err)
		return ""
	}
	return h.do(req)
}

// DoPost 对应 doPost(url, "A=B&C=D"), 参数按表单提交
func (h *HttpRequestManage) DoPost(rawURL, param string) string {
	form := url.Values{}
	for _, pair := range strings.Split(param, "&") {
		if pair == "" {
			continue
		}
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		form.Add(k, v)
	}
	req, err := http.NewRequest(http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		log.Errorf("[HttpRequest]", err)
		return ""
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return h.do(req)
}

// DoPostJson 对应 doPostJson(url, param)
func (h *HttpRequestManage) DoPostJson(rawURL, param string) string {
	return h.DoPostJsonWithHead(rawURL, nil, param)
}

// DoPostJsonWithHead 对应 doPostJson(url, head, param)
func (h *HttpRequestManage) DoPostJsonWithHead(rawURL string, head map[string]string, param string) string {
	req, err := http.NewRequest(http.MethodPost, rawURL, strings.NewReader(param))
	if err != nil {
		log.Errorf("[HttpRequest]", err)
		return ""
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	for k, v := range head {
		req.Header.Set(k, v)
	}
	return h.do(req)
}

// do 执行请求并读取响应体, 任何异常都返回空串 (对应原版 catch 后 return "")
func (h *HttpRequestManage) do(req *http.Request) string {
	req.Header.Set("User-Agent", h.userAgent)
	resp, err := h.client.Do(req)
	if err != nil {
		log.Errorf("[HttpRequest]", err)
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Errorf("[HttpRequest]", "Unexpected code {0}", resp.StatusCode)
		return ""
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("[HttpRequest]", err)
		return ""
	}
	return string(body)
}
