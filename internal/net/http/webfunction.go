// Package http 对应 net.rwhps.server.net.core.web.WebFunction 的工具方法
package http

import "strings"

// StringURLDataResolveToMap 对应 WebFunction.stringUrlDataResolveToJson:
// 把 "a=b&c=d" 形式的 URL 查询串解析为键值表.
//
// 与原版一致: 空串返回空表; 每段按首个 "=" 切分 (原版 split("=") 在多 "=" 时
// 会越界取第二段, 这里用 Cut 取第一段之后的全部, 对常见查询串行为一致).
func StringURLDataResolveToMap(data string) map[string]string {
	out := map[string]string{}
	if data == "" {
		return out
	}
	for _, pair := range strings.Split(data, "&") {
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		out[k] = v
	}
	return out
}

// HeadResolveToCookie 对应 WebFunction.headResolveToCookie:
// 解析请求头里的 Cookie 为键值表 (按 "; " 分段, 每段按 "=" 切分).
func HeadResolveToCookie(accept *AcceptWeb) map[string]string {
	out := map[string]string{}
	if accept == nil {
		return out
	}
	header := accept.GetHeaders("Cookie")
	if header == "" {
		return out
	}
	for _, cookie := range strings.Split(header, ";") {
		cookie = strings.TrimSpace(cookie)
		if cookie == "" {
			continue
		}
		k, v, _ := strings.Cut(cookie, "=")
		out[k] = v
	}
	return out
}
