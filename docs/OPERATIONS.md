# OPERATIONS —— Avalon 运维手册

面向部署与维护人员。只写现状，不写设计史；插件开发请看
[plugins/README.md](plugins/README.md)，分发形态看 [DISTRIBUTION.md](DISTRIBUTION.md)，
helper 线协议看 [HELPER-PROTOCOL.md](HELPER-PROTOCOL.md)。

---

## 1. 配置面

所有配置在 `data/` 下，**启动时读取，改动一律重启生效**（无热加载）。
加载与默认值权威：`internal/data/bean/configs.go::NewCoreConfig` /
`NewServerConfig` / `NewRelayConfig`。

### 1.1 Config.json（核心）

| 键 | 默认 | 作用 | 备注 |
|---|---|---|---|
| `port` | 5123 | 游戏端口 | Cascade 模式下改为 CASC 前导 + 会话门控 |
| `serverName` | "Avalon" | 服务器名（列表/日志可见） | |
| `subtitle` | "" | 副标题，留空用地图名 | |
| `log` | "WARN" | 日志级别 | |
| `defStartCommand` | "start" | 默认启动命令 | |
| `autoUpList` | false | 启动后自动上报 RW 官方列表 | 经 `uplist add` 命令进入 |
| `uplistEnable` | true | 自报总开关；false 拦截 uplist/toup/tonp | 与 cascadeEnable 无关 |
| `webPort` | 0 | WEB 面板端口，0 = 不开 | |
| `webToken` | 随机 10 位 | WEB 鉴权令牌 | 首次生成后固定 |
| `webHOST` | "" | WEB 来源限制 | |
| `mixProtocolEnable` / `rconMixEnable` / `webMixEnable` | false | 混合协议监听 | |
| `javaPath` | "" | java 可执行文件，空 = PATH 里的 `java` | |
| `gameHelperPath` | "" | helper jar 路径；**空 = 不装游戏内核** | 文件缺失不挡启动，回落无内核并告警 |
| `gameHelperArgs` | [] | 追加 java 参数，如 `["-Xmx512m"]` | |
| `cascadeEnable` | false | Cascade 网关模式（门控 + /v1 控制面） | |
| `cascadeBackendId` | "avalon-backend" | 后端 ID（JWT aud、health 应答） | |
| `cascadeRoomCode` | "" | 对 Cascade 暴露的房间码，空回落 `serverID` | |
| `cascadeLobby` | false | 零模组大厅模式，见 §2 | |
| `cascadeTrustMode` | "open" | 对端信任档：open / ip / pinned | open = 裸奔，游戏口严禁暴露公网 |
| `trustedCascadeIPs` | [] | ip/pinned 档来源白名单（IP/CIDR） | ip 档必填 |
| `cascadePinnedFingerprints` | [] | pinned 档客户端证书指纹（SHA-256 DER） | 列表语义支持轮换并存 |
| `cascadeTlsCert` / `cascadeTlsKey` | "" | 服务端证书/私钥（PEM），游戏口与控制面共用 | 留空 = 明文 |
| `cascadeControlAddr` | ":5125" | /v1 控制面监听地址 | |
| `cascadePendingTimeoutSec` | 0 | 会话 pending 绑定超时秒，0 = 5 | |

### 1.2 ConfigServer.json（房间）

| 键 | 默认 | 作用 |
|---|---|---|
| `serverID` | 随机 5 位 | 服务器 ID（房间码） |
| `passwd` | "" | 房间密码 |
| `maxPlayer` | 10 | 最大人数 |
| `oneAdmin` | true | 仅一名管理员（自动） |
| `enterAd` / `startAd` | "" | 进入/开局广播文案 |
| `startMinPlayerSize` | -1 | 手动开局最小人数，-1 禁用 |
| `autoStartMinPlayerSize` | 4 | 自动开局倒计时人数，-1 禁用 |
| `maxGameIngTime` | 7200 | 最大对局秒数，-1 禁用（超时提醒，再超 60s 强制结束） |
| `maxOnlyAIGameIngTime` | 3600 | 全 AI 局最大秒数 |
| `enableAI` | false | 允许 AI 玩家 |
| `maxMessageLen` | 40 | 最大发言长度 |
| `maxUnit` | 200 | 最大单位数 |
| `isAfk` | true | AFK 检测 |
| `muteAll` | false | 全员禁言 |
| `turnStoneIntoGold` | false | 点石成金 |
| `saveRePlayFile` | true | 保存回放到 `data/replays/` |

自动纠偏：`maxPlayer < startMinPlayerSize` 时 `startMinPlayerSize` 重置为 0
并告警（`internal/data/bean/configs.go::checkValue`），**不**挡启动。

### 1.3 其余文件

- `RelayConfig.json`：`mainID`（默认 "R"）、`upList`（默认 true）。
- `Plugins.json`：插件配置，见 §4；**非法配置启动直接失败**。
- `Settings.bin`：状态文件（语言 `serverCountry`、首启标记 `initStart`、
  两个 UUID）。非运维手改项；`setlanguage` 命令会写它。

### 1.4 配错就起不来的项

| 项 | 失败行为 |
|---|---|
| `Plugins.json` 非法（坏 JSON / version≠1 / 重名 / 非法 URL / 缺密钥 / 密钥短于 16 字节 / 奇数长度 hex / `vetoEvents` 含 room.create 或未知事件） | 启动显式失败，错误信息指明条目与字段（`internal/plugin/config.go::validate`） |
| `cascadeTrustMode: "ip"` 且 `trustedCascadeIPs` 为空 | 启动报错（不静默锁死也不静默放行） |
| `cascadeTrustMode: "pinned"` 缺服务端证书或指纹 | 启动报错 |
| `cascadeEnable` 所需材料缺失 | 启动失败（显式配置不降级，`internal/app/bootstrap.go::loadCascadeGateway`） |

**不**挡启动的：`gameHelperPath` 文件缺失（回落无内核 + 告警）、
`ConfigServer` 数值矛盾（自动纠偏 + 告警）。

---

## 2. 大厅与路由（Cascade 拓扑）

### 2.1 形态

每个子服 = 一个 Avalon 单房间后端（`cascadeEnable: true`）+ 自己的 helper，
挂在 Cascade 代理后面。另指定一个后端为**零模组大厅**：

```json
{
  "cascadeEnable": true,
  "cascadeLobby": true,
  "cascadeBackendId": "lobby",
  "cascadeControlAddr": ":5125"
}
```

`cascadeLobby: true` 的语义（`internal/data/bean/configs.go::CoreConfig`）：
起引擎、建房、开放游戏端口、允许玩家连入停留，但**永不真正开局**
（只 room.create 不 room.start，`internal/game/seam.go::GameSession.StartGame`
的大厅分支），并拒绝客户端 `/start`（提示 `err.lobbyNoStart`，
`internal/command/clientcommands.go::RegisterClientCommands`）。
大厅的 `data/mods` 为空：任何客户端（无论装没装 mod）都能安全进入停留
（原因——注册 checksum 只覆盖基础单位、mod 门槛是客户端侧 customUnits
检查：`knowledge/0009-zero-mod-lobby-and-mod-assembly.md` §1）。
在 Cascade 侧把它配成 `default_backend_id`。

### 2.2 玩家从大厅到子服

1. 客户端连 **Cascade 的公网游戏口**，被路由到大厅后端（default backend）；
2. 玩家在大厅聊天框发 `/goto <房间码>`——这是 **Cascade 代理**的命令，
   不是 Avalon 的命令；
3. Cascade 把房间码解析到所属后端，向客户端回 `PACKET_RECONNECT_TO`，
   目标是 `PublicGameHost:<该后端的 jump_port>`（Cascade
   `internal/proxy/session.go` 的 goto 处理）；
4. 客户端重连到跳转端口；Cascade 的跳转端口监听器把这条连接**直通**
   到对应后端，不看预注册房间码（`internal/proxy/server.go` 的 jump
   listener）。

### 2.3 jump port 约束：子服为什么不对外暴露

- 跳转目标永远是 **Cascade 代理自己的** `PublicGameHost + jump_port`——
  后端真实地址从不出现在任何包里。需要被跳入的后端在 Cascade 配置里
  声明 `jump_port`（>0），代理为该后端**多开一个公网监听端口**；
  子服游戏口只需对 Cascade 可达，不需要公网暴露。
- `PublicGameHost` 必须公网可解析可达；留空 = 禁用大厅跳转
  （`/goto` 回 `msg.jump_disabled`）。
- 一个后端一个 jump_port；端口冲突代理启动即报错。
- Avalon 侧另有一个上游遗留的跳转原语
  `internal/game/player/player.go::PlayerJumpsToAnotherServer`（域名校验后
  发跳转包），属 relay 时代路径；Cascade 大厅分流不走它。

---

## 3. 地图分发（协议 v4）

自定义地图由**引擎内置通道**随开局包流送给客户端，无额外端口；内容清单
（哈希）保证同名不同内容不被当作已有。权威记录：
`knowledge/0011-map-distribution.md`。

### 3.1 登记：清单怎么产生

1. 把地图文件（`.tmx` 或 zip 包）放进 `data/maps/`；
2. 重新登记：控制台 `map` 命令（`internal/command/servercommands.go` 调
   `internal/game/manage/mapmanage.go::ReadMapAndSave`）或重启；
3. 登记时对每张图计算 **SHA-256 + 大小** 存入 `MapData`；zip 包的哈希覆盖
   的是将被物化流送的那个条目（引擎实际流送的字节）。清单视图：
   `internal/game/manage/mapmanifest.go::BuildMapManifest`。

清单缺失（登记时未能计算哈希）的条目不带哈希——开局校验按内容不符
**明确失败**，不静默放行。

### 3.2 校验：两道复核

| 时机 | 位置 | 失败 |
|---|---|---|
| 开局前（Go） | `internal/game/seam.go::GameSession.StartGame` 物化后调 `internal/game/gamemaps.go::MapData.VerifyOnDisk` | 文件缺失 → `ErrMapSourceUnavailable`；大小/哈希不符或无清单 → `ErrMapContentMismatch`；房间保持未开局 |
| 流送前（helper） | 协议 v4 的 `room.start` 对非默认地图强制携带 `mapSha256`/`mapSize`（`internal/game/external/protocol.go::ProtocolVersion` 的校验），helper 侧 `GoBridge.roomStart` 复核盘上字节 | 类型化错误：`map file missing: X` / `map content mismatch: X (...)`，拒绝开局 |

版本闸门同 v3：握手版本不一致直接失败，不静默降级。

### 3.3 开局后：传输失败的分类

| 现象 | 判定 | 信号 |
|---|---|---|
| 传输中断 | `internal/net/gameserver/registry.go::routeStartGame` 写失败 | 日志 `map delivery: transfer interrupted ...` |
| 客户端拒绝/加载失败 | 已送达但断连前无接受/游戏内包（启发式，官方协议无显式拒绝包） | 日志 `map delivery: client refused or failed to load map ...`（`internal/net/gameserver/gameserver.go::mapDeliveryOutcome`） |

限制：zip 图只流送物化后的 tmx，非内置 tileset 贴图仍需客户端预装
（与官方主机行为一致）。

---

## 4. 插件三档（运维视角）

开发视角见 [plugins/](plugins/README.md)；机制权威在
`knowledge/0010-plugin-v2-implementation-status.md`。

### 4.1 怎么开

1. 插件方给你一个 HTTP 服务（自起进程）与一个 hex 密钥；
2. 写 `data/Plugins.json`（可直接抄）：

```json
{
  "version": 1,
  "admissionTimeoutMs": 2500,
  "deliveryTimeoutMs": 10000,
  "outboxCapacity": 128,
  "maxEventAgeHours": 24,
  "gameplayVetoTimeoutMs": 250,
  "entries": [
    {
      "name": "stats",
      "baseUrl": "http://127.0.0.1:9800",
      "secrets": ["<hex 密钥>"],
      "enabled": true,
      "vetoEvents": []
    }
  ]
}
```

3. 重启 Avalon。日志出现 `[Plugin] <name> ready: ...` 即握手成功；
   控制台 `plugin list` 看状态与计数。

不配置（无文件或空 entries）= 所有路径立即放行、零网络 I/O，与未启用
完全一致（零回归）。

### 4.2 开了之后哪些事件能拦住动作

| 事件 | 能拦什么 | 门槛 |
|---|---|---|
| `room.create` | 建房（启动期单例房 = 启动失败；Cascade 403；relay 错误信道） | 订阅即参与 |
| `game.start` | **开局**（取消本次开局） | 须 `vetoEvents` 授权 + manifest 声明 |
| `player.join` | 玩家进入（进入后立即踢出并附理由） | 同上 |
| `connection.new` | 裸连接（首包即断连，理由只进日志） | 同上 |
| `unit.operation` / `factory.build` | 对局内单条指令（250ms 紧超时） | 同上 |

授权是双闸门（配置授权 + manifest 声明），默认拒绝；`vetoEvents` 只能填
这五个事件名，填错启动失败。所有否决**超时/故障一律放行**（fail-open），
插件挂了不会卡死服务器。

### 4.3 出问题怎么快速关掉

没有热加载——关插件必须重启，但路径简单且可预期：

1. 把该 entry 的 `"enabled"` 改 `false`（保留配置）或整条删除；紧急时
   直接清空 `entries` 数组；
2. 重启 Avalon。

恢复原状 = 零回归：无启用插件时准入立即放行、无任何网络调用。
排查期先看 `plugin list` 的 `failOpen`（插件故障放行计数）与
`dropped`（丢弃事件数），语义见 [plugins/FAILURE-MODES.md](plugins/FAILURE-MODES.md)。

---

## 5. 故障对照

只列实际见过的。

| 现象 | 先看哪里 | 常见成因 |
|---|---|---|
| 游戏端口迟迟不就绪，日志循环刷 `helper speaks protocol N, Go side is M` | helper jar 与 Avalon 的协议版本 | 两侧版本不同步（jar 新于/旧于 Go 侧）。对齐版本：线协议版本以 `docs/HELPER-PROTOCOL.md` 的 `ProtocolVersion` 为准，helper 与 Avalon 必须同版（实测：v4 jar 配 v3 Go，helper 无限重启，端口永不就绪） |
| 服务器起来了，`/start` 回 "This command needs the game engine..." | 启动日志找 `[GameCore]` 告警 | `gameHelperPath`/`javaPath` 未配或文件缺失，回落无内核（不挡启动）。补齐配置重启 |
| 启动直接失败，错误以 `plugin:` 开头 | 错误信息指明的条目/字段 | `Plugins.json` 非法（见 §1.4）。按字段修正，不存在静默降级 |
| 启动直接失败，提示信任/证书相关 | `cascadeTrustMode` 与配套项 | ip 档白名单为空；pinned 档缺证书或指纹（见 §1.4） |
| `plugin list` 里 `failOpen` 持续上涨 | 对应插件进程与网络 | 插件不可达/太慢/应答畸形。准入已自动放行，主流程不受影响；修复插件后计数停止增长 |
| 启动时 ERROR `...der.kim... no such host` | 无需处理 | 上游国家探测域名已死，自动回落 EN 语言，非致命（每次启动都会出现） |
| 开局失败，日志有 `map content mismatch` / `ErrMapContentMismatch` | `data/maps/` 里该图文件 | 盘上文件与登记时不一致（被改过）。重新登记（`map` 命令重扫）或恢复文件 |
| 开局失败，`map file missing` / `ErrMapSourceUnavailable` | 地图物化目录 | 登记后文件被删/未落盘。重新登记 |
| helper 重启后玩家掉线、房间状态重置 | 日志 `helper generation N exited` | helper 世代更替：旧世代承载的连接作废，房间在新一代重建（只建房不开局）。客户端需重连；频繁出现则查 helper 崩溃原因（内存/引擎异常） |
| 首启卡在提问上（无人值守场景） | 已修复，无需处理 | 首启 WEB 面板询问现在对非终端输入直接跳过（默认不初始化），终端输入有界等待超时走默认（`internal/app/initialization.go::firstRunPrompt`） |

日志位置：`data/logs/`；回放：`data/replays/`；插件投递队列（deliver 档
WAL）：`data/plugins/outbox/`。
