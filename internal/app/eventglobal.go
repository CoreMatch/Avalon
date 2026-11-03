// Package app 对应 net.rwhps.server.game.EventGlobal 的全局事件注册.
package app

import (
	"rwhps/internal/data/global"
	"rwhps/internal/game/player"
	"rwhps/internal/net/core"
	"rwhps/internal/util/timeutil"
)

// registerEventGlobal 对应上游在启动期 addGlobalEventManage(EventGlobal()) +
// runRegisterGlobalEvents() 注册的四个全局事件处理器.
//
// 逐条对应与差异:
//   - registerServerLoadEvent      -> ServerLoadEvent 监听: 注册禁言过滤器 (照搬)
//   - registerNetConnectNewEvent   -> core.NetConnectNewEventHook: bannedIP24 拒绝 (照搬)
//   - registerServerHessLoadEvent  -> Go 版无类加载器维度, 其职责 (注册房间默认事件
//     监听与客户端命令) 已分别在 room.NewServerRoom 与启动链的
//     RegisterClientCommands 完成, 见各自注释.
//   - registerServerStartTypeEvent -> Relay 协议且 config.autoUpList 时经
//     ServerCommand 执行 "uplist add" (UpList 功能已内建, 见 net/uplist);
//     控制台标题 (CLITools.setConsoleTitle) Go 版无对应物, 不实现.
func (r *Runtime) registerEventGlobal() {
	// registerServerLoadEvent: 服务器加载完毕时注册禁言聊天过滤器
	r.Events.Register("ServerLoadEvent", func(_ GlobalEvent) {
		if global.Core == nil || global.Core.Admin == nil {
			return
		}
		global.Core.Admin.AddChatFilter(global.ChatFilterFunc(func(p *player.Player, message string) (string, bool) {
			if p.MuteTime > timeutil.ConcurrentMillis() {
				return "", false
			}
			return message, true
		}))
	})

	// registerNetConnectNewEvent: 新连接的 IP /24 段在黑名单内则拒绝
	// (上游经事件 result = true 表达; Go 版钩子返回 true 即拒绝)
	core.NetConnectNewEventHook = func(ca *core.ConnectionAgreement) bool {
		if ca == nil || global.Core == nil || global.Core.Admin == nil {
			return false
		}
		return global.Core.Admin.IsIP24Banned(ca.IPLong24)
	}

	// registerServerStartTypeEvent: Relay 协议且开启 autoUpList 时自动上架
	// (上游 EventGlobal.kt:58-66; 控制台标题部分无 Go 对应物)
	r.Events.Register("ServerStartTypeEvent", func(ev GlobalEvent) {
		e, ok := ev.(ServerStartTypeEvent)
		if !ok {
			return
		}
		switch e.ServerNetType {
		case core.RelayProtocol, core.RelayMulticastProtocol:
			if global.Config.AutoUpList {
				global.ServerCommand.HandleMessageWith("uplist add", global.DefPrint)
			}
		}
	})
}
