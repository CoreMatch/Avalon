// Package relay 对应 net.rwhps.server.command.relay
//
// 上游这个包只有两个类: RelayCommands (注册入口, 函数体几乎为空) 与
// RelayClientCommands (Relay 客户端命令实体). 调用点在 CoreCommands.startrelay:
// 每次启动 Relay 服务前 RelayCommands(Data.SERVER_COMMAND), 其中
// RelayClientCommands 挂到 Data.RELAY_COMMAND 上.
package relay

import (
	"rwhps/internal/data/global"
	cmd "rwhps/internal/util/game/command"
)

// RegisterRelayCommands 对应 RelayCommands(handler)
//
// 上游 init 块三步:
//  1. registerRelayCommand(handler) —— 函数体为空 (上游就是空方法)
//  2. PluginManage.runRegisterRelayCommands(handler) —— 插件系统未移植, 跳过
//  3. RelayClientCommands(Data.RELAY_COMMAND) —— 客户端命令挂到 RELAY_COMMAND
//
// 参数 handler 对应上游传入的 SERVER_COMMAND handler, 只被第 1/2 步消费;
// 两步都为空后这里不再使用它, 保留签名以对齐上游调用点.
func RegisterRelayCommands(handler *cmd.Handler) {
	_ = handler
	RegisterRelayClientCommands(global.RelayCommand)
}
