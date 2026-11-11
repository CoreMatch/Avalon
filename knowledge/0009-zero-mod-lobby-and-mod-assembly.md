# 0009. 零模组大厅分流与模组装配（核查结论 + 设计）

## Status

Design proposal（只读核查产物；证据均给出文件路径 + 符号）。

实现进展（2026-11-08 注记）：§1 三件核查事实是已落地形态的依据——零模组大厅
以 `cascadeLobby` 实现，运维形态见本仓 `docs/OPERATIONS.md` §2；§3/§5 的房间
模组清单与内容端点按 Cascade-Backend 契约 v1.3 落地
（`internal/net/gateway/mods.go`、`internal/game/manage/modmanifest.go`），
实现未走 helper 协议查询操作，而是 Go 侧直接读 `/data/mods` 文件层；§4 的客户端
外挂器即 RW-Mod-Injector 项目（另仓）。其余部分保留原设计文本。

## 摘要

目标形态：Cascade 下挂一个**零模组大厅房间**作为分流点。玩家先进大厅；带模组
外挂器的玩家选择某个模组房间时，由外挂器拉取该房间所需模组、禁用多余的、装配
就绪后再进入。本文先钉死三件核查事实（§1），再给出设计（§2–§7），最后给一句话
结论（§8）。

**最重要的核查发现（会改写任务前提）**：注册时的单位 checksum（`getAllUnitsChecksum`）
**只覆盖基础/核心单位，不计 mod 单位**。真正拦住"没装对应 mod 的客户端"的是另一条
独立机制——服务端在 SERVER_INFO(106) 里下发 customUnits 清单，**客户端侧**逐个比对
（缺 / 不一致即抛异常）。因此背景里"不同子服模组集不同 → 注册 checksum 天然不同 →
连房被 kick"这一因果**不成立**：注册 checksum 与 mod 无关，零模组大厅要解决的门槛是
第二条机制，不是注册 kick。详见 §1.1。

---

## 1. 三件核查事实（钉死）

以下证据全部来自对闭源引擎 `Server-Core/libs/game-lib.jar`（位于
`/home/lnb/Desktop/HA/rw/Avalon-Helper/Server-Core/libs/game-lib.jar`）的字节码反汇编，以及两仓源码。
混淆符号以 `game-lib.jar` 内的类/方法名给出。

### 1.1 Q1 — checksum 到底覆盖什么？mod 是否计入？

**结论：注册 checksum 只覆盖基础单位（编译期固定的单位枚举），不计 mod 单位。**
mod 单位由独立的 customUnits 机制在客户端侧校验。两条机制、两个时机。

**机制① 注册 checksum（kick 发生处，仅基础单位）**

- 分发入口：`com.corrodinggames.rts.gameFramework.j.ad.c(au)`（NetEngine 主包分发），
  `lookupswitch` 中 **case 110 = REGISTER_PLAYER**。
- 注册包解析：依次读 名字 / 3 个 int 版本 / 用户名 / 密码；当协议版本 ≥3 再读一个
  **int（客户端上报的 checksum）**，≥4 读完整性应答串，≥5 读 extra。
- 比对点（字节码偏移 3417–3423）：`iload 12`（客户端 checksum）对
  `invokevirtual com/corrodinggames/rts/gameFramework/l.z()I`，不等即 kick，文案
  **"Your core units are different to the server's core units. Game can not be
  synchronized"** —— 引擎自己说的是 **core units**。
- `l.z()` 实现链：`gameFramework.l.z()`（抽象）→ `com.corrodinggames.rts.game.i.z()`
  仅返回缓存字段 `d`；`d` 在引擎初始化 `Unit.loadAllUnits` → `Loading custom unit
  data` 之后由 **`com.corrodinggames.rts.game.units.am.bM()`** 计算并缓存。
- `am.bM()` 算法：**只迭代 `EnumSet.allOf(com.corrodinggames.rts.game.units.ar.class)`**，
  对每个基础单位取 `bw()`（= `31*(int)bN() + (int)cv`）累乘 31 求和。
  `ar` 是 `extends java.lang.Enum` 的编译期固定枚举；mod/自定义单位
  （`com.corrodinggames.rts.game.units.custom.l`）**不是它的常量，运行时无法加入**。
  门控 `gameFramework.l.as()` 恒返回 `true`（`iconst_1; ireturn`，`game.i` 未重写）。

**机制② mod 单位一致性（真正的 mod 门槛，客户端侧）**

- 服务端 `com.corrodinggames.rts.gameFramework.j.ad.c(c)`（字符串 `sendServerInfo`）
  构造 **SERVER_INFO(106)** 包，其中经 `custom/l.a(j/as)` 写入 **customUnits 清单**
  （单位名 + 服务端校验和），`bipush 106` 发出。
- 客户端在 `ad.c(au)` **case 106** 收到 SERVER_INFO 时经 `custom/l.a(j/k)` 读出清单，
  逐个调 `custom/l.a(ab, HashMap)` 校验：
  - 客户端没有该单位 → 抛 `bd`：**"The server requires the unit: X that was not found"**；
  - 有但校验和不等 → 抛 `bd`：**"Found unit: X but it does not match the server's copy"**
    （附 `checksum c:<客户端> s:<服务端>`）。
- `bd extends bo extends java.lang.Exception`（受检异常）；`custom/l.a(j/k)` 的异常表
  只有 `any`（finally），**不捕获 `bd`**，异常向上传播进客户端收包循环。

**对任务前提的修正**：只要游戏版本一致、mod 未覆写基础单位数据，不同子服的
`getAllUnitsChecksum` 相同，mod 客户端**不会因机制①被 kick**。真正挡 mod 的是机制②，
且发生在客户端收 SERVER_INFO 时（注册刚过、进大厅之前）。
（未决反例：若某 mod 覆写基础单位数据，会改变 `bw()` 从而改变机制①——见 §7 风险。）

### 1.2 Q2 — Avalon 是否看得到注册包里的 checksum 字段？

**结论：看不到。生产 Cascade 走 gameserver（ServerProtocol）模式，REGISTER_PLAYER
被当作不透明字节原样转发，Avalon 只读包类型号，从不解析负载。**

转发路径（全部在 `/home/lnb/Desktop/HA/rw/Avalon`）：

1. 门控只看类型：`internal/net/decoder.go::NewServerHandler`（`ch.Gate.AllowPacket
   (msg.Type.TypeInt())`），门控实现 `internal/net/gateway/session.go::Session.AllowPacket`
   —— PENDING 只放行 160，BOUND 全放行；**只判类型，不碰负载**。
2. 分发：`internal/net/gameserver/typeconnect.go::HeadlessTypeConnect.ProcessConnect`
   只拦截 `CHAT_RECEIVE / GAMECOMMAND_RECEIVE / RELAY_118_117_RETURN / DISCONNECT`，
   REGISTER_PLAYER(110) 落 default → `ReceivePacket`。
3. 转发：`internal/net/gameserver/gameserver.go::GameVersionServer.ReceivePacket` →
   `internal/game/external/conn.go::Backend.ConnPacket`，负载
   `Data: encodeBase64(p.Bytes)` **原样 Base64 透传**，无解析。
4. helper 侧：[xref] `Helper/.../go/GoBridge.kt::feedPacket` →
   [xref] `Server-Core/.../j/NetEnginePackaging.kt::addPacket` → 引擎 `ad.c(au)` case 110
   才真正解析出 checksum。

模式选择证据：`internal/app/initialization.go::loadService` 中 `core.ServerProtocol →
gameserver.NewProtocolProvider`；`core.RelayProtocol/RelayMulticastProtocol →
netconnectprotocol.NewRelayProtocolProvider`。Cascade 子服是单房间后端（`internal/net/
gateway/rooms.go`，DECISIONS D-20"房间即服务器"），跑 ServerProtocol。

唯一解析 REGISTER_PLAYER 内容的是 **Relay 风味**（P2P 房主转发）：
`internal/net/netconnectprotocol/realize/gameversionrelay.go::GameVersionRelay.
RelayRegisterConnection` 只解析 **名字 + UUID-Hex**（`ReadString / Skip(12) /
ReadString / ReadIsString / ReadString / ReadString`），**同样不解析 checksum**，
且该路径不是生产 Cascade 子服路径。

### 1.3 Q3 — 引擎是否有稳定的"已连接未 spawn"状态？期间是否引用 mod 单位？

**结论：有——注册通过到开局之前的大厅态。期间引擎不模拟/不 spawn 任何单位
（含 mod 单位），不会因 mod 单位引用而崩溃或错位。但机制②的 customUnits 校验
恰发生在进入大厅态的第一步（收 SERVER_INFO），因此"缺 mod 的客户端"只能在
零模组房间的大厅里安全停留，在模组房间会在收 SERVER_INFO 时即失败。**

- 大厅态：`ad.c(au)` case 110 注册通过后，服务端发 SERVER_INFO(106) + TEAM_LIST(115)，
  客户端进入队伍/大厅界面；开局由 START_GAME(120) →
  [xref] `Helper/.../go/GoBridge.kt::roomStart` → `gameScriptMultiPlayer.multiplayerStart()`
  触发。**单位模拟只在开局后发生**，大厅态无单位 spawn。
- 大厅态引擎只把 mod 单位**数据**装载在内存（启动期 `loadAllUnits` + 自定义单位装载），
  不进入模拟循环；SERVER_INFO 里写的也只是 customUnits 的"名字 + 校验和"元数据，
  不是模拟引用。故大厅态不会因 mod 单位崩溃/错位。
- 风险窗口在**开局后**：缺 mod 的客户端若混进模组房间并开局，模拟 mod 单位时会
  失配/脱同步。机制②在收 SERVER_INFO 时就把这种客户端挡在大厅之前，正是利用这一点。

---

## 2. 目标形态与分流原理

- 每个子服 = 一个 Avalon 单房间后端 + 自己的 helper（`data/mods` 决定其模组集）。
- 指定其中一个子服为**零模组大厅**（`data/mods` 为空）。它的 SERVER_INFO 里
  customUnits 清单为空，机制②对任何客户端都平凡通过；只要基础单位/版本一致，
  机制①也通过。**因此任意客户端（装没装 mod、装了什么 mod）都能安全进入并停留。**
- 玩家在大厅里浏览房间目录、选一个模组房间；外挂器按该房间的模组清单装配
  （拉缺的、禁多的、校哈希），装配就绪后客户端**重连**到目标模组房间。此时客户端
  已持有全部所需 mod，收 SERVER_INFO 时机制②通过，正常进入。
- 关键点：机制②在客户端收 SERVER_INFO 时（注册刚过）就执行，**装配必须发生在
  客户端连上模组房间之前**。外挂器必须先从房间目录（带外）拿到目标房间的模组清单，
  不能等连上再反应。

---

## 3. 房间目录与需求清单

**现状**：控制面 `GET /v1/rooms` / `GET /v1/rooms/{code}` 返回 `RoomSummary`
（`internal/net/gateway/rooms.go::RoomSummary`），字段为 `code / title / mapName /
maxPlayers / players / inGame / ownerId / ownerName / adminCount / createdAt /
members`。**没有任何模组信息。**

**需要新增（每个子服对外暴露）**：

| 字段 | 说明 | 产生者 | 暴露方式 |
| --- | --- | --- | --- |
| `roomName` / `code` | 房间名/码 | 已有（`RoomSummary.Title/Code`） | 现有 `/v1/rooms` |
| `mods[].name` | 所需模组名 | helper（见下） | 新增字段/端点 |
| `mods[].hash` | 模组文件哈希（SHA-256，供下载校验） | helper/内容通道 | 新增 |
| `mods[].size` | 模组文件字节数 | helper/内容通道 | 新增 |
| `mods[].unitChecksum` | 引擎侧单位校验和（可选，用于高级核对） | 引擎 | 新增 |
| `ready` | 该子服模组是否装载就绪 | helper 探针 | 新增（可并入 `/v1/health`） |
| `players` / `maxPlayers` / `inGame` | 人数/上限/是否开局 | 已有 | 现有 |

- **谁产生**：模组清单的权威来源是 helper 内的引擎。现有
  [xref] `Server-Core/.../game/manage/ModManage.kt::getModsList` 只返回**模组名**，
  **没有哈希/大小**；`load()` 从 `gameUnitData.getUnitData(coreName)` 取数。
  需要在 helper 侧扩展：枚举 `data/mods`（`Data.Plugin_Mods_Path`，
  `FileLoaderRedirections` 把 `mods/units` 前缀重定向到此）下的模组包，计算文件
  SHA-256 与大小，连同单位校验和一起上报。
- **怎么暴露**：helper 协议（`docs/HELPER-PROTOCOL.md`）新增一个 `room.mods` /
  `mods.list` 查询操作（v2-ext 风格，v2-only helper 回 `unsupported` 降级），Go 侧
  拿到后并入控制面。对外二选一：
  - 在 `RoomSummary` 增 `mods[]`（`GET /v1/rooms/{code}` 详情携带）；或
  - 新增 `GET /v1/rooms/{code}/mods` 专门端点（推荐，避免列表接口膨胀）。
- **契约**：按项目硬约束，新增端点/字段须先在 HA-Contract 登记
  （`cascade-backend-v1.yaml` / Cascade-Backend-v1.md）并升版本、跑 drift，再动代码。

---

## 4. 外挂器的装配流程

外挂器是**客户端侧**组件，与游戏客户端同机运行。流程：

1. **取清单**：从房间目录（§3 端点，带外 HTTP）读目标房间的 `mods[]`（名/哈希/大小）。
2. **对账**：与客户端本地已装模组比对，得出"缺的 / 多的 / 哈希不一致的"三个集合。
3. **拉缺的**：经内容通道（§5）下载缺失模组，逐个校验 SHA-256；校验失败即重试/报错。
4. **禁多的**：把房间不要求的模组移出启用集。**原因**：注册 checksum 只算基础单位，
   多装 mod 不会挡注册；但客户端若持有服务端没有的单位，开局后一旦被使用会脱同步，
   故按"精确匹配房间模组集"装配。（若某 mod 会覆写基础单位数据，还会改变机制①，
   更须禁用，见 §7。）
5. **校哈希**：下载物与清单哈希逐一核对通过才允许进入下一步。
6. **生效**：RW 引擎在**启动期**装载 mod（`loadAllUnits` + 自定义单位装载），
   运行中无标准热重载。服务端侧 `ModManage.reLoadMods` 尚且要 `reBootServer`
   （[xref] `Server-Core/.../ModManage.kt::reLoadMods`），**客户端同理——装配后需要重启
   客户端（或等价的引擎重载）才能让新模组集生效**。这是本方案最大的不确定点（§8）。
7. **进入**：装配并生效后，客户端重连目标模组房间（§6 交接），收 SERVER_INFO 时
   机制②通过。
8. **失败回退**：任一步失败（下载失败/哈希不符/装载失败/进入被踢）→ 外挂器把客户端
   带回**零模组大厅**并给出可读原因；绝不把半装配状态的客户端留在模组房间。

---

## 5. 内容通道（模组文件分发）

传输模组文件给客户端的通道。要求**有哈希、有签名、可分块断点、有大小上限**；
**不能照抄 RWPP 的"无哈希无签名、按文件名匹配"**——那样无法防篡改、无法断点、
无法保证"装上的就是服务端要的"。

- **传输路**：复用现有"控制面/内容"HTTP 通道（与 §3 房间目录同一信任域），或独立
  内容服务器。模组文件属**系统边界外输入**，必须校验后才落客户端启用目录。
- **分块与断点**：大模组按固定块传输，支持 `Range`/断点续传；每块带块哈希，
  整体带文件 SHA-256。
- **哈希与签名**：清单里的 `hash` 为文件 SHA-256；建议再附发布者签名（防清单本身被
  篡改）。客户端只信任"哈希命中 + 签名有效"的模组。
- **大小上限**：单模组与单次装配总量设上限（参照控制面 `decodeBody` 的 1MB 请求体
  上限思路，文件通道另设更大量级但必须有限），超限拒绝并回退大厅。
- **与机制②的衔接**：清单哈希命中 ⇒ 文件一致 ⇒ 引擎解析出的单位校验和应与服务端
  `unitChecksum` 一致。若出现"文件哈希对但机制②仍失败"，说明引擎版本/解析差异，
  应回退并报诊断（这是 §7 风险之一）。

---

## 6. 大厅 → 房间的切换与交接

**能否复用现有 interop（`matchcore://join` 深链 + loopback handoff）？**
`/home/lnb/Desktop/HA/matchcore/INTEROP-SPEC.md` 描述的是 **Minecraft** 交接
（MCA→SamuelClient→启动游戏进程），与 RW 不同游戏、不同动作（它是"拉起进程"，
这里是"网络重连"）。**其具体实现不可直接复用**，但"深链仅作唤起提示 + 真参数走
loopback + 凭据不过界"的**模式**可借鉴到外挂器与客户端的本地协同上。

RW 原生的切换机制已存在，应优先复用：

- **跳转包**：`internal/game/player/player.go::Player.PlayerJumpsToAnotherServer` →
  `internal/net/gameserver/gameserver.go::SendJumpToAnotherServer` →
  `internal/net/netconnectprotocol/internal/relay/relaypacket.go::
  FromRelayJumpsToAnotherServerPacket`，发 **`PACKET_RECONNECT_TO`(178)**，
  让客户端重连到指定 `ip:port`。这是"服务端指挥客户端换服"的现成通道。
- **两种切换编排**：
  1. **外挂器驱动**（推荐）：装配就绪后，外挂器直接让客户端断开大厅、重连目标子服
     （客户端本就掌握自己的连接）。简单、时序清晰，装配完成才发起连接。
  2. **服务端驱动**：大厅服在确认外挂器就绪后对玩家发 `PACKET_RECONNECT_TO(178)`
     指向目标子服。需要"外挂器就绪 → 大厅服"的一次带外通知，且必须保证**跳转发生
     在装配完成之后**（机制②在目标子服收 SERVER_INFO 时即校验）。
- 无论哪种，**红线**是：客户端连上目标模组房间的那一刻，模组集必须已装配并生效。

---

## 7. 红线清单（未就绪客户端的禁止事项）

未就绪 = 模组集尚未按目标房间装配并生效。禁止：

1. **禁止进入任何模组房间**：未就绪客户端不得对模组子服发起注册（会在收
   SERVER_INFO 时机制②失败，白白暴露失败体验）。
2. **禁止在模组房间开局**：缺 mod 客户端若混入并开局，模拟 mod 单位时脱同步/崩溃。
3. **禁止跳过哈希/签名校验**：不得装载未校验或校验失败的模组。
4. **禁止保留"多余的"覆写型 mod**：会覆写基础单位数据、改变机制①注册 checksum 的
   mod 必须禁用，否则连注册都会被 kick。
5. **禁止把半装配状态带进对局**：装配任一步失败必须整体回退大厅，不得"部分装载"进入。
6. **禁止篡改/伪造房间目录与清单**：清单须签名可信，客户端不得信任未签名来源。

---

## 8. "不需要修改引擎 kick 逻辑"是否成立？

**成立（在"装配先于连接"的前提下）。** 理由：

- 机制①（注册 checksum）只算基础单位：客户端只要基础单位/版本与服务端一致即通过，
  与装没装 mod 无关——**无需为 mod 放宽或改动这条 kick**。
- 机制②（customUnits）是客户端侧自检：只要外挂器在客户端连上模组房间**之前**把模组
  装配齐，收 SERVER_INFO 时自然通过——**同样无需改引擎**。
- 因此整套方案是"绕开既有 kick 逻辑、在连接发生前把条件凑齐"，不触碰引擎的
  kick/校验代码。

**不成立的情形（边界）**：若无法保证"装配先于连接"（例如客户端无法在连接前完成模组
生效、或存在覆写基础单位的 mod 改变了机制①），则要么改装配时序、要么就得动引擎
kick 逻辑。故该结论**依赖 §4 的装配时序与 §7 的覆写型 mod 禁令**成立。

---

## 9. 一句话结论

**这套形态可行**——零模组大厅能作为分流点（任意客户端可安全停留），装配齐模组后重连
模组房间即可通过现有两道校验，**无需修改引擎 kick 逻辑**；**最大的技术风险是客户端
侧"模组装配后如何生效"**：RW 引擎在启动期装载 mod、运行中无标准热重载，装配大概率
要重启客户端（或等价引擎重载）才能生效，这决定了整个"装配→进入"体验是否顺滑；
**相对工作量排序（从大到小）**：外挂器装配流程（含客户端生效机制）＞ 内容通道
（哈希/签名/分块断点）＞ 房间目录与需求清单（helper 上报 + 控制面端点 + 契约登记）＞
大厅→房间切换交接（复用 `PACKET_RECONNECT_TO`/直连）＞ 红线清单（规范约束）。
