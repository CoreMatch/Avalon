# 事件目录（契约 v1.2.0）

13 个事件，三档语义。所有事件共享信封字段：

<!--
机器可读事件清单（一致性检查读取此区块，勿改格式/语言标签）：
- 代码 <-> 本区块：internal/plugin/eventcatalog_test.go::TestEventCatalogDocInSync
- 三方（代码/openapi/本区块）：HA-Contract scripts/check-avalon-plugin-event-drift.sh
增删事件时必须同步更新本区块、下方表格与 HA-Contract 契约。
-->

```plugin-events
room.create
game.over
player.join
player.leave
player.banned
player.unbanned
player.ipbanned
game.start
player.chat
unit.operation
factory.build
connection.new
player.kicked
```

| 字段 | 说明 |
|---|---|
| `eventId` | `evt_` + 26 字符 ULID；发出时生成一次，重试/重放复用，插件按它去重 |
| `event` | 事件名 |
| `eventVersion` | 当前恒为 1 |
| `occurredAt` | RFC3339 UTC 发出时刻 |
| `server` | `{serverId, serverName, coreVersion}` |

载荷的权威定义在契约（HA-Contract
`docs/api/openapi/avalon-plugin-v1.yaml`）；Avalon 侧构造代码在
`internal/plugin/event.go::buildRoomCreateEvent` 与
`internal/plugin/eventv2.go::buildPlayerJoinEvent` 等（事件全集定义在
`internal/plugin/eventv2.go::allEventNames`）。

## 总表

| 事件 | 档位 | 触发时机 | 门控 | 超时预算 | 超时/错误默认 |
|---|---|---|---|---|---|
| `room.create` | veto | 建房前（启动单例房 / Cascade 控制面 / relay 建房三条路径） | 订阅即参与 | `admissionTimeoutMs`（默认 2500ms） | fail-open 放行 |
| `game.over` | deliver | 房间对局结束事件触发后 | 订阅 | `deliveryTimeoutMs`（默认 10s）/次 | 重试，退避 1s..60s |
| `player.join` | observe + veto | 玩家注册进房后 | 否决须授权 | 否决：`admissionTimeoutMs` | fail-open（玩家留下） |
| `player.leave` | observe | 玩家断连/被踢 | 订阅 | `deliveryTimeoutMs` | 丢弃计数 |
| `player.banned` | observe | 封禁动作触发（控制台/客户端管理/Cascade） | 订阅 | 同上 | 丢弃计数 |
| `player.unbanned` | observe | 解封动作触发 | 订阅 | 同上 | 丢弃计数 |
| `player.ipbanned` | observe | IP 封禁动作触发 | 订阅 | 同上 | 丢弃计数 |
| `game.start` | observe + veto | 否决模式：开局前；观察模式：开局成功后 | 否决须授权 | 否决：`admissionTimeoutMs` | fail-open（照常开局） |
| `player.chat` | observe | 聊天通过长度/全员禁言/过滤器检查后 | 订阅 | `deliveryTimeoutMs` | 丢弃计数 |
| `unit.operation` | observe + veto | 解析到客户端单位指令 | 否决须授权 | 否决：`gameplayVetoTimeoutMs`（默认 250ms） | fail-open（指令放行） |
| `factory.build` | observe + veto | 解析到客户端工厂建造指令 | 否决须授权 | 同上 | 同上 |
| `connection.new` | 仅 veto | 新连接首包、协议状态建立前 | 须授权 | `admissionTimeoutMs` | fail-open（连接放行） |
| `player.kicked` | observe（审计） | 任一踢出发生时（单一咽喉点 `internal/game/player/player.go::KickPlayer`） | 订阅 | `deliveryTimeoutMs` | 丢弃计数，不反作用于踢出 |

**门控**（"否决须授权"）= 双闸门，缺一不可：manifest 对该事件声明
`veto: true`，且运营把你写进该事件的 `vetoEvents` 授权清单（默认拒绝）。
只满足其一：双模事件退回观察模式；`connection.new`（无观察模式）干脆
不咨询。授权校验在 `internal/plugin/hub.go::vetoConsulted`。

## 载荷字段（信封之外的部分）

| 事件 | 字段 | 说明 |
|---|---|---|
| `room.create` | `room{kind,code,maxPlayers,isMod}` + `creator{name,address}` | kind: server/cascade/relay；creator: console/cascade/建房玩家 |
| `game.over` | `room{kind,code}` + `result` | `result` 可为 `null`（引擎无结算数据时对局结束事实仍投递） |
| `player.join` | `room` + `player` | 见 PlayerInfo |
| `player.leave` | `room` + `player` + `banRemainingMs` + `inGame` | 断开原因（自愿/被踢/断网）取不到，不携带；`banRemainingMs>0` = 带着踢/封窗口离开 |
| `player.banned` / `player.unbanned` / `player.ipbanned` | `room` + `player` | 同形 |
| `game.start` | `room` + `match{mapName,mapPlayer,maxPlayers,startTime,players[]}` | **开局前准入时 `startTime=0`**；开局后观察通知里是实际开局时间 |
| `player.chat` | `room` + `player` + `message` | 内置过滤后的文本；禁言消息不会到达 |
| `unit.operation` | `room` + `player` + `action` + `unitName` + `x` + `y` | action 为指令种类名 |
| `factory.build` | `room` + `player` + `actionId` | 不以 `_` 开头的建造动作 |
| `connection.new` | `connection{address,ipLong24,transport}` | **只有连接层信息**：此时无玩家身份、无房间归属，契约不设占位字段 |
| `player.kicked` | `room` + `player` + `reason` + `durationSec` | reason 已净化；`durationSec=0` = 只踢不锁重进；AI 也走这里（`player.bot` 可辨） |

**PlayerInfo**（`player` 段）：`name, uuid, team, slot, host, bot,
spectator, admin, address`。对局内数据（击杀/经济/位置/存活）属引擎内部，
不在契约内，没有占位字段。

## 否决之后发生什么

| 事件 | 否决的可观察后果 | 理由去哪 |
|---|---|---|
| `room.create` | 建房不发生 | 启动单例房：启动失败并带理由；Cascade：403 `FORBIDDEN` 消息；relay：`RELAY_117` 错误信道回理由（三个调用点：`internal/app/bootstrap.go::Start`、`internal/net/gateway/rooms.go::createRoom`、`internal/net/netconnectprotocol/realize/gameversionrelay.go::newRelayId`） |
| `player.join` | 玩家"进入后被立即踢出"（异步踢，语义等价同步拒绝）；被否决的进入**不发**观察通知 | 随踢出理由送达玩家 |
| `game.start` | 开局取消（不 fire 开局事件，观察档也不通知） | 发起管理员收到系统消息；自动开局（无发起人）只进日志 |
| `unit.operation` / `factory.build` | 指令包在进引擎前被丢弃 | 只进日志（对局内无玩家侧通道） |
| `connection.new` | 连接被立即断开 | 只进日志（首包时刻对端尚未进入协议，无呈现通道） |

## 观察档的投递机制（所有 observe 事件通用）

- 每插件单 worker、FIFO，与 deliver 档共享队列；观察档**不落盘**；
- 队列满（`outboxCapacity`，默认 128）丢最旧；
- 插件不可达期间事件在内存积压，协商恢复后补投；超过
  `maxEventAgeHours`（默认 24h）或协商后版本无交集的积压被丢弃计数；
- Avalon 重启 = 观察档事件全部消失（不持久、不重放）；
- 投递失败（超时/5xx/坏签名）立即丢弃计数，不重试；连接级失败会把插件
  打回未就绪并走 manifest 重试（见 [FAILURE-MODES.md](FAILURE-MODES.md)）。
