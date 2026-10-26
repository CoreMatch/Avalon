// Package game 对应 net.rwhps.server.util.game
//
// 原版这里只有 GameOtherUtils 一个 object. 按"只移植有生产调用方的成员"的
// 约定, 目前只落 getBetaVersion —— 它的唯一生产调用方是 GameVersionRelay.
// mapProcessing 与 getPoint 在原版只有测试用到, 不移植.
package game

// GetBetaVersion 对应 GameOtherUtils.getBetaVersion
//
// 原版: `return version in 152 .. 175`
//
// 落在 152..175 的客户端版本被当作 beta 版, 之后的行为 (例如房间是否标 mod)
// 与正式版不同.
func GetBetaVersion(version int) bool {
	return version >= 152 && version <= 175
}
