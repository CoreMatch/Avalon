// Package app 对应 上游 game.EventGlobal 的全局事件注册.
package app

import (
	"github.com/CoreMatch/Avalon/internal/data/global"
	"github.com/CoreMatch/Avalon/internal/game/player"
	"github.com/CoreMatch/Avalon/internal/net/core"
	"github.com/CoreMatch/Avalon/internal/plugin"
	"github.com/CoreMatch/Avalon/internal/util/log"
	"github.com/CoreMatch/Avalon/internal/util/timeutil"
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
	// (上游经事件 result = true 表达; Go 版钩子返回 true 即拒绝),
	// 随后是插件连接级准入 (契约 v1.2.0 connection.new, 仅授权否决插件参与):
	//   - 内建 IP/24 ban 先行, 命中即断连, 不再咨询插件;
	//   - 插件否决 = 裸断连, 理由只进服务端日志 (首包时刻对端尚未进入
	//     协议, 无呈现通道, 契约已如实声明);
	//   - 插件超时/不可达/畸形应答 = fail-open 放行 (与其他否决档一致);
	//   - 无授权插件时零网络 I/O, 行为与未启用一致.
	core.NetConnectNewEventHook = func(ca *core.ConnectionAgreement) bool {
		if ca == nil {
			return false
		}
		if global.Core != nil && global.Core.Admin != nil && global.Core.Admin.IsIP24Banned(ca.IPLong24) {
			return true
		}
		if d := plugin.AdmitConnectionNew(ca.IP, ca.IPLong24, ca.UseAgreementStr); !d.Allowed {
			log.Warnf("[Plugin]", "connection.new vetoed by {0}: {1} (address={2})", d.Plugin, d.Reason, ca.IP)
			return true
		}
		return false
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
