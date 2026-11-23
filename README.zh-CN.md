<div align="center">
  <h1>Avalon</h1>
  <p><strong>运行真正的铁锈战争服务端，掌控每一间房间。</strong></p>
  <p><a href="./README.md">English</a></p>
</div>

Avalon 是面向 Rusted Warfare 1.15 / 协议 176 的专用服务端运行时。它以 Go 提供网络、房间、管理和控制台能力，并由真正的 JVM 游戏模拟提供支撑。

## 为什么选择 Avalon

- **房间运行尽在掌握。** 在统一的运维运行时中管理玩家、审核与实时房间。
- **让服务器被发现。** 接入服务器列表与 Relay 流程，同时保留管理控制权。
- **运行真正的游戏。** 协议 v4 驱动 JVM 模拟、AI 与比赛结果。
- **解耦扩展能力。** 进程外插件无需将第三方代码载入引擎即可增加行为。
- **分发已验证内容。** 自定义地图与模组在分发给客户端之前完成校验。
- **隔离故障，拓展边缘。** Helper 崩溃隔离保护宿主进程，Cascade 提供共享公共入口。

## 进一步了解

- [运维文档](docs/OPERATIONS.md) — 房间、路由、内容与插件运维
- [分发文档](docs/DISTRIBUTION.md) — 受支持的运行时与分发形态
- [Avalon-Helper](https://github.com/CoreMatch/Avalon-Helper) — JVM 模拟伴随组件
- [HA 契约](https://github.com/CoreMatch/HA-Contract) — 权威集成契约
- [HA Wiki](https://wiki.mcnb.dev/) — 生态部署与开发指南

## 使用边界

Avalon 并非纯 Go 实现、官方 Rusted Warfare 服务端，也不承诺与上游功能完全对等；真正的对局需要 Avalon-Helper。

## 许可证与来源

[AGPL-3.0](LICENSE) · 衍生自 [RW-HPS](https://github.com/RW-HPS/RW-HPS)，保留上游版权声明。
