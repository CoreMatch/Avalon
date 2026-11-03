// Package httpapi 对应上游 plugin/beta/http/ (RwHpsWebApiMain 插件, 约 556 行).
//
// 上游是插件体系加载的 Web 控制 API 插件 (HttpApi): Cookie 鉴权 + 只读状态查询
// (SystemInfo/GameInfo/GameOver 结算) + 命令执行 (ServerCommand/ClientCommand)
// + 控制台 WebSocket. Go 版无插件框架, 把其中可移植的部分作为普通包内建
// (里程碑 5); 各成员的取舍逐条记录在 DECISIONS D-14:
//
//	已移植:  WebStatus / Authentication (AuthCookie + get/post 鉴权分发) /
//	         MessageForwardingCenter 的只读 GET 路由与 run/ServerCommand /
//	         GetData 的 GameOver 缓存 + SystemInfo + GameInfo (Go 侧已有字段)
//	未移植:  ConsoleWebSocket (Go 的 WS 升级握手未实现, StartHttp 对
//	         "GET /WebSocket" 回 404) / run/ClientCommand (依赖上游 Cherry
//	         假玩家) / info/ModsInfo (Go 无 Mod 系统) /
//	         GameInfo 的 income/noNukes/credits/sharedControl (引擎侧数据,
//	         见 DECISIONS D-9) / ServerConsolePrintEvent 控制台转发
package httpapi

import (
	"rwhps/internal/util/jsonutil"
)

// WebStatus 对应 WebStatus.kt: API 统一响应包装 (status + data)
type WebStatus struct {
	Status string `json:"status"`
	Data   string `json:"data"`
}

// ToWebStatusJSON 对应 String.toWebStatusJson(status = "OK"):
// 包装为 WebStatus 并序列化; status 为空时用 "OK".
func ToWebStatusJSON(data, status string) string {
	if status == "" {
		status = "OK"
	}
	return jsonutil.ToJSON(WebStatus{Status: status, Data: data})
}
