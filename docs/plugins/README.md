# 插件开发者文档（avalon-plugin/v1，契约 v1.2.0）

Avalon 的插件是**独立进程**：你自己起一个 HTTP 服务，Avalon 作为客户端
调用你。引擎（JVM 侧）不加载任何第三方代码，插件也永远不需要、不能够
主动连接 Avalon——方向只有单向：**Avalon → 插件**。

- 契约权威：HA-Contract `docs/api/openapi/avalon-plugin-v1.yaml`
  （v1.2.0，三档事件契约）。文档与契约不一致时以契约为准，并请提 issue。
- 设计记录：`knowledge/0006-out-of-process-plugin-extension-points.md`
  （v1 设计）、`knowledge/0010-plugin-v2-implementation-status.md`
  （v2 落地状态与明确未做项）。

## 阅读顺序

| 文档 | 内容 |
|---|---|
| [GETTING-STARTED.md](GETTING-STARTED.md) | 从零写一个 Hello 插件：起进程、握手、订阅、应答否决 |
| [EVENT-CATALOG.md](EVENT-CATALOG.md) | 13 个事件的触发时机、载荷、可否决、否决后果、失败默认 |
| [AUTH-AND-NEGOTIATION.md](AUTH-AND-NEGOTIATION.md) | 双向 HMAC 签名算法、版本协商、密钥配置与轮换 |
| [FAILURE-MODES.md](FAILURE-MODES.md) | 崩溃/超时/不可达/畸形应答的行为表（运维排查用） |
| [NON-GOALS.md](NON-GOALS.md) | 明确不提供的能力，以及为什么 |
| [template/](template/) | 参考实现：可直接改的 Go 模板（标准库，含自测试） |

## 三档事件，一分钟理解

| 档位 | 你能做什么 | 失败时 Avalon 怎么办 |
|---|---|---|
| **observe**（只读观察） | 收事件推送，返回 2xx 确认即可 | 丢弃该事件并计数，不重试 |
| **veto**（同步可否决） | 在限定时间内回 `allowed: false` + 理由，阻止动作 | fail-open：当你的否决不存在，动作照常 |
| **deliver**（异步不可否决） | 收事件推送，必须按 `eventId` 去重 | 重试（退避 1s..60s），Avalon 重启后重放 |

当前各档事件：

- veto：`room.create`（订阅即参与）、`player.join` / `unit.operation` /
  `factory.build` / `connection.new` / `game.start`（须运营显式授权，默认拒绝）；
- deliver：`game.over`；
- observe：其余全部（详见事件目录）。

## 快速自检清单

写完插件后对着过一遍：

- [ ] `GET /v1/manifest` 返回的 `schema` / `protocol` 是
  `avalon-plugin/manifest` / `avalon-plugin/v1`（精确匹配）；
- [ ] 每个请求都验证了 `X-Avalon-Signature`，每个应答都签了名；
- [ ] 否决应答的 `allowed: false` 都带了可读 `reason`；
- [ ] `game.over` 按 `eventId` 去重（至少 24h）；
- [ ] 事件处理有界且快——你的处理时间会被 Avalon 的超时上限截断。
