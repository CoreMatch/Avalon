# RW-HPS Helper Protocol v2

Go 服务端与外部游戏内核（helper，一个 Java 进程）之间的通信协议。

- **权威实现**：`internal/game/external/protocol.go`（Go 侧编解码与校验）。本文档与之
  一一对应；若两者不一致，以 `protocol.go` 为准。
- **helper 侧参考实现**：仓库根目录 `Helper/` 模块（`HelperMain.kt` 协议主循环 +
  `GoBridge.kt` 引擎桥接，v2 与 v2-ext 全部操作）。构建与部署见
  `Go-Server/README.md`「安装并接入外部游戏内核」。
- **协议版本**：`ProtocolVersion = 2`。握手时版本不一致直接失败，不做尽力兼容。
  升级协议时递增该值。

---

## 传输与线格式

- 通道：helper 子进程的 **stdin / stdout**。Go 侧经 `java [args] <gameHelperPath>` 拉起。
- 编码：**每行一条 JSON，LF 结尾，UTF-8**。
- 游戏包负载：**标准 Base64**（RFC 4648，带填充），与 `java.util.Base64.getEncoder()` 一致。
- JSON 字符串转义：Go 侧序列化关闭了 HTML 转义（地图名可能含 `< > &`），helper 侧
  解析/生成时也应避免额外的 HTML 转义。

### 世代号（gen）

- Go 侧每拉起一个 helper 进程，世代号从 1 递增。
- helper 在握手（`hello`）时学到自己的世代号，此后**每条消息都回带**它。
- Go 侧收到世代号不等于当前世代的消息一律丢弃——用于丢弃上一代 helper 在管道里迟到的
  包，防止串到新一局。

---

## 消息信封

### Go -> helper（请求）

```json
{"v":2,"id":1,"gen":1,"op":"room.start","room":"r1","map":"...","mapPlayer":"...","maxPlayer":10}
```

| 字段 | 说明 |
| --- | --- |
| `v` | 协议版本，恒为 2 |
| `id` | 请求序号，Go 侧单调递增，与应答的 `id` 对齐（必须为正） |
| `gen` | 世代号（必须非 0） |
| `op` | 操作名，见下表 |
| `room` | 房间 ID（`room.*`/`conn.*` 必填） |
| `conn` | 连接 ID，房间内唯一（`conn.*` 必填） |
| `map` / `mapPlayer` / `maxPlayer` | `room.start` 的开局参数 |
| `ptype` / `data` | `conn.packet` 的游戏包类型号与 Base64 负载 |

### helper -> Go（应答 / 主动消息）

所有消息共有判别头 `{"v":2,"type":"...","gen":N}`，按 `type` 分发：

```json
{"v":2,"type":"response","gen":1,"id":1,"ok":true,"running":true,"players":3,"rooms":1}
{"v":2,"type":"packet","gen":1,"room":"r1","conn":"c1","ptype":115,"data":"<base64>"}
{"v":2,"type":"event","gen":1,"event":"player.join","room":"r1","conn":"c1","data":{...}}
{"v":2,"type":"fatal","gen":1,"error":"...","room":"r1"}
```

| type | 说明 |
| --- | --- |
| `response` | 对某次请求的应答，`id` 对齐；`ok` 为 false 时 `error` 给原因 |
| `packet` | 引擎产生、要转发给某客户端连接的游戏包（`room`/`conn`/`ptype` 必填） |
| `event` | 玩家/房间事件（`event`/`room` 必填，玩家级事件还需 `conn`） |
| `fatal` | helper 或其子进程的致命错误（`error` 必填） |

`response` 的扩展字段按操作有效：`running`/`players` 仅 `room.status`，`rooms` 仅 `hello`。

---

## 操作（Go -> helper）

| op | 必填字段 | 语义 |
| --- | --- | --- |
| `hello` | （无 room/conn） | 握手：校验版本，返回状态（`rooms`=当前房间数） |
| `room.create` | room | 创建房间。**幂等**：已存在则返回 OK 并保持原房间 |
| `room.destroy` | room | 销毁房间 |
| `room.start` | room（+map/mapPlayer/maxPlayer） | 开局（对应 ServerRoom.roomStartGame） |
| `room.stop` | room | 结束当前局（对应 ServerRoom.gr） |
| `room.status` | room | 查询运行状态（`running`）与人数（`players`） |
| `conn.open` | room, conn | 登记一条新客户端连接 |
| `conn.close` | room, conn | 关闭一条客户端连接 |
| `conn.packet` | room, conn, ptype（+data） | 把客户端游戏包转给引擎 |

校验规则（Go 侧在发送前拦截，见 `validateRequest`）：
`hello` 不得携带 room/conn；`room.*` 必须带 room；`conn.*` 必须带 room+conn；
`conn.packet` 另需 `ptype>0`。

---

## 事件（helper -> Go，`type:"event"`）

`event` 字段取值与 `data` 负载形状：

| event | 需 conn | data 负载 |
| --- | --- | --- |
| `player.join` | 是 | PlayerProfile |
| `player.leave` | 是 | PlayerProfile |
| `player.update` | 是 | PlayerProfile |
| `chat` | 是 | ChatPayload |
| `command` | 是 | CommandPayload |
| `room.started` | 否 | （无固定负载） |
| `room.stopped` | 否 | （无固定负载） |

### PlayerProfile

```json
{"id":"<玩家唯一标识>","name":"<名字>","team":1,"ready":true,"host":false,"bot":false,"spectator":false}
```

v2-ext 扩展：`room.addai` 应答里的 AI 档案额外携带 **`position`** 字段（0 基整数，
引擎内槽位，指针语义——`0` 必须原样上线）。常规 `player.*` 事件不携带该字段。

### ChatPayload / CommandPayload

```json
{"name":"<发言者/发起者>","text":"<内容>"}
```

---

## 错误约定

- 协议层错误（非法 JSON / 版本不符 / id 不对应 / 空行 / 必填字段缺失）：Go 侧归为
  `external: protocol error`。**未知消息类型视为致命**（静默忽略会让两侧版本悄悄错位）。
- helper 进程退出（管道 EOF 或非 0 退出码）：`external: helper process is not running`。
- 操作失败：应答 `ok:false` + `error` 原因，原样透传到上层日志。

---

## 实现者注意事项

1. `room.create` 必须幂等（Go 侧按世代跟踪房间，但 helper 崩溃重启等时序下仍可能收到
   重复创建，拒绝会导致整条开局链路失败）。
2. 每条消息都要带回正确的 `gen`；握手时记下 Go 给的世代号。
3. 玩家级事件（`player.*`/`chat`/`command`）必须带 `conn`，否则 Go 侧无法定位连接。
4. 游戏包负载一律 Base64；`ptype` 用 Rusted Warfare 协议号（与 `internal/io/packettype` 一致）。
5. 不要在 JSON 里做 HTML 转义。

---

## 扩展操作（protocol v2-ext）

> **有意修正（作者已批准，2026-11-04）**：上游这些能力直接调用进程内的游戏引擎对象；
> Go 版引擎在 helper 内，经本节扩展操作完成（DECISIONS D-9）。
> 权威实现：`internal/game/external/protocol.go`（操作常量与校验）、
> `internal/game/external/ext.go`（Backend 方法）。

### 版本与向后兼容

- **扩展版本记为 v2-ext**：它不是新的线协议版本——`ProtocolVersion` 仍为 `2`，信封、
  世代号、传输格式与 v2 完全一致，只是**新增**了五个操作名、两个请求字段（`position`、
  `settings`）与两个应答字段（`result`、`ai`）。v2 现有操作不受任何影响。
  （前四项为 D-9 扩展；`room.settings` 与 `ai` 应答字段为 Cascade 对接期补充，
  DECISIONS D-15/D-20。）
- **只实现了 v2 的 helper**（未声明支持 v2-ext）收到这五个操作时，应应答：

  ```json
  {"v":2,"type":"response","gen":1,"id":1,"ok":false,"error":"unsupported"}
  ```

  Go 侧把 `error:"unsupported"` 归一为 `game.ErrUnsupported` 并**降级为空操作、仅记日志**：
  不 panic、不断开连接、不影响 v2 功能。
- 未配置 helper 时，Go 侧根本不会发起这些操作（走既有降级路径：`ErrNotPortable` /
  空操作 + 日志），行为与扩展引入前一致。

### 扩展操作一览（Go -> helper）

| op | 必填字段 | 语义 |
| --- | --- | --- |
| `room.addai` | room | 让引擎生成一个 AI（对应 `gameScriptMultiPlayer.addAi`）。创建的 AI 档案随应答的 `ai` 字段推回 Go 侧（见下） |
| `room.sync` | room | 引擎全量同步（对应 `gameLinkFunction.allPlayerSync`） |
| `room.removeplayer` | room, position | 按位置移除引擎内的玩家（AI 移除，对应 `playerPrivateData.removePlayer`） |
| `game.result` | room | 查询结算数据（对应 `gameHessData.getGameOverData`） |
| `room.settings` | room, settings | 把房间设置写入引擎 `gameLinkServerData`（Cascade 后端契约的房间门面，DECISIONS D-20） |

校验规则（Go 侧发送前拦截）：五个操作都必须带 `room`；`room.removeplayer` 另需
`position`（**0 基**，0 合法；上游位置即 0..maxPlayer-1）；`room.settings` 的
`settings` 负载至少含一个字段（全空时 Go 侧不发请求）。

### 请求字段扩展

| 字段 | 说明 |
| --- | --- |
| `position` | 仅 `room.removeplayer`：玩家在引擎内的位置（0 基整数）。以指针语义编码——`0` 必须原样上线，不得被当作缺省吞掉 |

```json
{"v":2,"id":4,"gen":1,"op":"room.removeplayer","room":"r1","position":3}
```

| 字段 | 说明 |
| --- | --- |
| `settings` | 仅 `room.settings`：对象负载，字段全为可选，只写提供的项 |

`settings` 负载字段与引擎 `gameLinkServerData` 的对应（helper 侧映射照搬上游
`ClientCommands` 的 credits/nukes/fog/sharedcontrol/startingunits/income 命令）：

| 字段 | 类型 | 引擎字段 | 映射 |
| --- | --- | --- | --- |
| `credits` | int | `credits` | 金额档照上游映射表转引擎档位（0→1、1000→2、2000→3、5000→4、10000→5、50000→6、100000→7、200000→8、4000→0、其余→0） |
| `fog` | int | `fog` | 0=off 1=basic 2=full，原样写入 |
| `income` | float | `income` | 原样写入 |
| `disableNuke` | bool | `nukes` | 否定语义：`nukes = !disableNuke` |
| `sharedControl` | bool | `sharedcontrol` | 原样写入 |
| `startingUnits` | int | `startingunits` | 原样写入 |
| `maxUnits` | int | `maxUnit` | 原样写入 |

```json
{"v":2,"id":5,"gen":1,"op":"room.settings","room":"r1","settings":{"credits":5000,"fog":1,"disableNuke":true}}
```

### 应答字段扩展：`ai`

仅 `room.addai` 有效。**有意修正（作者已批准，2026-11-04）**：上游 `addAI` 同步把
引擎生成的 AI 包装进 `room.playerManage`；Go 版引擎在 helper 内，档案经本字段回传，
Go 侧据此登记入房（DECISIONS D-9）。

```json
{"v":2,"type":"response","gen":1,"id":3,"ok":true,"ai":{
  "id":"AI-zIp74I0J","name":"AI-Hard","team":0,"ready":true,
  "host":false,"bot":true,"spectator":false,"position":0}}
```

- 负载形状为 PlayerProfile + `position`（0 基槽位，指针语义）。
- **引擎没有创建 AI**（无空位，对应上游扫描的静默返回）：应答 `ok:true` 且
  **不带** `ai` 字段，Go 侧不登记。
- 不走 `player.join` 事件：引擎只为真实连接触发 `PlayerJoinEvent`（上游
  `PlayerConnectX`），AI 没有对应事件可搭。

### 应答字段扩展：`result`

仅 `game.result` 有效。**字段与上游 `getGameOverData()` 返回的 `GameOverData` 一一对应**：

```json
{"v":2,"type":"response","gen":1,"id":5,"ok":true,"result":{
  "gameTime":600,
  "allPlayerList":["alice","bob"],
  "winPlayerList":["alice"],
  "mapName":"Crossing Large (10p)",
  "playerData":{
    "alice":{"unitsKilled":12,"buildingsKilled":3,"experimentalsKilled":1,
             "unitsLost":4,"buildingsLost":2,"experimentalsLost":0},
    "bob":{"unitsKilled":4,"buildingsKilled":2,"experimentalsKilled":0,
           "unitsLost":12,"buildingsLost":3,"experimentalsLost":1}
  },
  "replayName":"2026-11-04-1"
}}
```

| 字段 | 说明 |
| --- | --- |
| `gameTime` | 对局时长（秒），对应 `concurrentSecond - startTime` |
| `allPlayerList` | 全部玩家名 |
| `winPlayerList` | 获胜方玩家名 |
| `mapName` | 地图名 |
| `playerData` | 玩家名 -> 战绩键值表，**固定六项**：`unitsKilled` / `buildingsKilled` / `experimentalsKilled` / `unitsLost` / `buildingsLost` / `experimentalsLost` |
| `replayName` | 回放文件名 |

**尚未分出胜负时**（上游 `getGameOverData()` 返回 `null`）：应答 `ok:true` 且**不带**
`result` 字段。Go 侧把它当作"无结算数据"，跳过播报（与上游 `?: return` 一致）。

### Go 侧消费点

| 操作 | 消费方 |
| --- | --- |
| `room.addai` | 控制台/客户端 `addai` 命令 → `PlayerManage.AddAI` → `ServerRoom.AddAI` |
| `room.sync` | `CallManage.SyncAllPlayer`（`addmoney` 等命令收尾的全量同步） |
| `room.removeplayer` | `ServerRoom.RemovePlayer`（AI 移除：引擎侧成功后清房间列表） |
| `game.result` | `CallManage` 检查线程结算播报（`survive.player`），数据暂存房间后随 `ServerGameOverEvent` 发出 |
| `room.settings` | Cascade 后端契约房间门面 `PUT /v1/rooms/{code}/settings` → `ServerRoom.ApplySettings`（`net/gateway`） |
