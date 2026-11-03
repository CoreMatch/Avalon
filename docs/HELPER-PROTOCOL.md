# RW-HPS Helper Protocol v2

Go 服务端与外部游戏内核（helper，一个 Java 进程）之间的通信协议。

- **权威实现**：`internal/game/external/protocol.go`（Go 侧编解码与校验）。本文档与之
  一一对应；若两者不一致，以 `protocol.go` 为准。
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
