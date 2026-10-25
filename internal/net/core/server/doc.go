// Package server 对应 net.rwhps.server.net.core.server
//
// 原版这个包下有两个文件: AbstractNetConnectServer.kt 与 AbstractNetConnectData.kt,
// 外加 packet/ 子包 (已移植为本包下的 netpacket).
//
// # 为什么只搬接口, 不搬实现
//
// 这两个接口在原版里的实现者分布是高度不对称的, 这是决定本包形态的唯一依据:
//
//	AbstractNetConnectData    -> GameVersionServer  (Java 耦合, 不可移植)
//	                          -> GameVersionRelay   (Relay flavour, 属 Relay 批次)
//	AbstractNetConnectServer  -> 只有 GameVersionServer 一个实现者
//
// 而 GameVersionServer 位于 plugin/internal/headless/inject/net/, 它的 import 列表里
// 直接出现 com.corrodinggames.rts.game.units.y 这类混淆后的游戏类 —— 属于必须跑在
// JVM 上、且依赖具体 rusted-warfare.jar 的那一层. 按既定边界规则, 这类能力走
// internal/game/external 外部进程依赖, 不在本包复刻, 更不允许在 Go 里重写它的算法.
//
// 因此本包只声明契约, 不提供实现. 任何 Go 结构体将来要实现这些接口时:
// 纯协议部分可以自己写, 游戏语义部分应当转交外部 helper.
package server
