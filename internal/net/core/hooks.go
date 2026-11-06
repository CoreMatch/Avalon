package core

// 事件系统钩子.
// 原版通过 PluginManage.runGlobalEventManage(...).await() 运行全局事件,
// 事件系统尚未实现, 因此以钩子形式预留注入点, 默认 nil 表示无事件处理.

// NetConnectCloseEventHook 对应 PluginManage.runGlobalEventManage(NetConnectCloseEvent(this))
var NetConnectCloseEventHook func(*ConnectionAgreement)

// NetConnectNewEventHook 对应 PluginManage.runGlobalEventManage(NetConnectNewEvent)
// 返回 true 表示事件结果 result 为真 (拒绝该连接)
var NetConnectNewEventHook func(*ConnectionAgreement) bool
