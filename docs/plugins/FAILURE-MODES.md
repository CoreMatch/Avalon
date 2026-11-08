# 失败模式手册

运维排查用。总原则一句话：**插件是辅助治理，内置机制才是主边界——
死插件绝不锁死服务器**。所有否决档失败一律 fail-open；观察档失败丢弃；
投递档失败重试。

## 插件侧故障 → Avalon 侧行为

| 故障 | veto 档（准入） | observe 档 | deliver 档 |
|---|---|---|---|
| 进程崩溃/拒连 | 跳过该插件，放行并计 `failOpen` | 事件在内存积压（满丢最旧），恢复后补投 | 事件留在 WAL，恢复后补投 |
| 响应超时 | 放行并计 `failOpen`（超时阈值见下表） | 该次投递丢弃计 `dropped` | 计 `retries`，退避重试 |
| 返回 5xx | 同上（按档位） | 丢弃计 `dropped` | 重试 |
| 应答未签名/签名错 | 放行计 `failOpen` | 丢弃计 `dropped` | 重试 |
| 应答超 64KB | 放行计 `failOpen` | 丢弃计 `dropped` | 重试 |
| 裁决缺 `allowed` 字段 | 放行计 `failOpen` | —（非否决事件无裁决） | — |
| `allowed:false` 无 `reason` | **放行**计 `failOpen`（无理由否决视为畸形） | — | — |
| 连接级失败（拒连/DNS） | — | 插件打回未就绪，manifest 重试退避 5s..5min | 同左（积压不丢） |

## 超时阈值速查

| 场景 | 阈值 | 来源 |
|---|---|---|
| room.create / player.join / connection.new / game.start 准入 | 默认 2500ms | `admissionTimeoutMs` |
| unit.operation / factory.build 否决 | 默认 **250ms** | `gameplayVetoTimeoutMs` |
| manifest 自报 `timeoutMs` | 与上述取 **min** | 插件 manifest |
| 启动期 manifest 协商 | 每插件 ≤ `admissionTimeoutMs` | 并行，互不阻塞 |
| 单次投递尝试 | 默认 10s | `deliveryTimeoutMs` |
| 投递重试退避 | 1s 起，翻倍，上限 60s | 硬编码（`internal/plugin/hub.go::run`） |
| manifest 重试退避 | 5s 起，翻倍，上限 5min | 同上 |
| 事件最大年龄 | 默认 24h，超龄丢弃 | `maxEventAgeHours` |
| 时间戳偏移容忍 | ±300s | `internal/plugin/signature.go::verifyResponse` |

## Avalon 侧故障 → 插件侧影响

| 故障 | 影响 |
|---|---|
| Avalon 重启 | deliver 档从 WAL 重放（插件必须按 `eventId` 去重 ≥24h）；observe 档**全部丢失**（不持久不重放，设计如此）；veto 无状态无影响 |
| 队列积压（插件慢） | 每插件队列上限 `outboxCapacity`（默认 128），满则丢最旧并计 `dropped`；单 worker FIFO 保序 |

## 幂等与去重窗口

| 项 | 值 | 谁负责 |
|---|---|---|
| `eventId` 稳定性 | 发出时生成一次（ULID），重试/重放复用 | Avalon |
| 去重窗口 | **≥24 小时**（覆盖最大事件年龄 + 重放） | 插件 |
| 至多一次的事件 | 全部 veto 档：绝不重试 | — |
| 至少一次的事件 | `game.over`（deliver） | 插件去重 |
| 尽力一次的事件 | 全部 observe 档：失败即弃 | 无需去重（但按 `eventId` 防重也无害） |

## 排查入口

控制台 `plugin list` 每插件一行，计数含义：

| 计数 | 含义 | 非零说明 |
|---|---|---|
| `delivered` | 成功投递/裁决次数 | 正常 |
| `retries` | deliver 档重试次数 | 插件慢或短暂不可用 |
| `dropped` | 丢弃事件数（队列满/超龄/观察档投递失败/版本不匹配积压） | 插件消费不过来或长期不可达 |
| `denied` | 该插件发起的有效否决次数 | 正常（若意外，检查插件逻辑） |
| `failOpen` | 该插件故障导致的放行次数 | **插件有问题**：超时/不可达/畸形应答 |

状态字段 `state=ready` 表示协商成功；`not ready` 时看 Avalon 日志的
`[Plugin] <name>: manifest retry failed` 定位（拒连 = 进程没起，
401 = 密钥错配，超时 = 插件太慢）。

## 故障注入对照

每种失败模式都有自动化测试覆盖，改插件机制前先跑：

- 超时/畸形/无理由/未签名/500：`internal/plugin/hub_test.go::TestAdmissionTimeoutFailsOpen` 等 + `internal/plugin/v120_test.go::TestConnectionNewMalformedFailsOpen`；
- 不可达与恢复：`internal/plugin/hub_test.go::TestGameOverUnreachableThenRecovery`；
- 崩溃重启重放与幂等：`internal/plugin/hub_test.go::TestReplayAfterRestartIsIdempotent`；
- 观察档不重放：`internal/plugin/v2_test.go::TestV2ObserveNotReplayedAcrossRestart`。
