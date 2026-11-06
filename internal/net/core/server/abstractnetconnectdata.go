package server

// AbstractNetConnectData 对应 AbstractNetConnectData
//
// 原注释: "Only provide interface but not implement. As the interface of game CoreNet,
// it provides various data support for GameServer and GameRelay"
//
// 四个只读属性, 零个方法 —— 原版少见的"纯数据接口", 因此它也是这两个接口里
// 唯一可以完全机械移植的一个: 不涉及任何游戏语义, 也没有可空之外的坑.
//
// 实现者 (均不在本包):
//   - GameVersionServer: plugin/internal/headless/inject/net, Java 耦合, 不可移植
//   - GameVersionRelay:  netconnectprotocol/realize, 属 Relay 批次
type AbstractNetConnectData interface {
	// Name 对应 name: Player Name
	Name() string

	// RegisterPlayerId 对应 registerPlayerId: Player UUID?
	//
	// Kotlin 声明为 String? (可空). Go 侧统一用空串表示"无", 与 io 层
	// readIsString 把 null 读成 "" 的既有约定保持一致, 不引入 *string.
	RegisterPlayerId() string

	// BetaGameVersion 对应 betaGameVersion: Player Is Beta Version
	BetaGameVersion() bool

	// ClientVersion 对应 clientVersion: Player Version
	//
	// 注意 GameVersionServer 里它是 `get() = supportedVersionInt`,
	// 而 GameVersionRelay 里默认 151 并在 relayDirectInspection 时被真实客户端版本覆盖.
	// 同一个成员在两种 flavour 下语义不同, 实现时不要混淆.
	ClientVersion() int
}
