# 鉴权与版本协商

## 双向 HMAC-SHA256 签名

每个请求和每个应答都携带两个头：

| 头 | 值 |
|---|---|
| `X-Avalon-Timestamp` | 发送时刻的 unix 秒 |
| `X-Avalon-Signature` | `v1=` + `hex(HMAC-SHA256("<时间戳>.<原始正文>", secret))` |

要点：

- 签名覆盖**原始正文字节**（不是格式化后的 JSON）加时间戳，两者都防篡改；
- `GET /v1/manifest` 无正文，按**空串**参与签名；
- 时间戳偏移容忍 **±300 秒**，超出即拒；
- `v1=` 前缀是算法版本号，将来换算法时递增，不是可选装饰；
- 3xx 重定向不被跟随——重定向后的请求不再携带原签名语义，一律按失败处理。

**双向**的含义：

- Avalon → 插件的请求由 Avalon 签名，插件**必须验签**（模板实现：
  `docs/plugins/template/plugin.go::Verify`）；
- 插件 → Avalon 的应答（manifest、裁决、ack）由插件签名，Avalon 验签
  （`internal/plugin/signature.go::verifyResponse`）。应答不签名或签名错
  = 该次调用按失败处理（否决档 fail-open，投递档算失败重试）。

**密钥方向**：Avalon 用 `secrets` 列表的**第一个**密钥签名，用**任一**
密钥验签；插件用自己持有的那把密钥做两件事。两边至少 16 字节。

```
签名示例（伪代码）:
  ts   = 1762588800
  body = {"allowed":true}
  mac  = HMAC_SHA256(key=secret, msg="1762588800." + body)
  头   = "v1=" + hex(mac)
```

## 密钥配置与轮换

密钥配在 Avalon `/data/Plugins.json` 的 `secrets`（hex 字符串列表）：

```json
"secrets": ["<新密钥 hex>", "<旧密钥 hex>"]
```

轮换流程（不断连不重启）：

1. 插件侧先同时接受新旧两把密钥（验签任一通过即可；模板按单密钥写，
   轮换期把它改成列表逐个 `hmac.Equal`）；
2. Avalon 侧把新密钥加到 `secrets` **列表头**（新密钥成为签名用主密钥），
   重启 Avalon 生效；
3. 观察无异常后，移除旧密钥，再重启一次。

重叠期内：Avalon 用新密钥签名（插件两把都试），插件用旧密钥签的应答
Avalon 也认（任一验签通过）。配置校验拒绝奇数长度/非法字符/少于
16 字节的密钥（`internal/plugin/config.go::validate`）。

## 版本协商（manifest）

协商时机：

- Avalon 启动时，对每个启用插件并行拉一次（每插件独立超时，上限 =
  `admissionTimeoutMs`）；
- 任何**连接级**投递失败（拒连/DNS 失败，不含超时）后，插件被打回
  未就绪，进入重试循环：退避 5s 起、翻倍、上限 5min
  （`internal/plugin/hub.go::run`）。

协商内容：插件的 `events` 声明 × Avalon 支持的事件全集
（`internal/plugin/eventv2.go::allEventNames`），逐个事件取版本交集：

| 情况 | 结果 |
|---|---|
| 事件版本有交集 | 该事件按协商版本投递（当前 Avalon 只发版本 1） |
| 插件没声明该事件 | 不投递给你 |
| 声明了但版本无交集 | 不投递给你；已积压的该事件在协商成功时被丢弃计数 |
| `veto: true` 声明 | 记录为"声明了否决能力"；是否真参与否决还看运营授权（默认拒绝） |

**未就绪期间**各档行为：

| 档位 | 行为 |
|---|---|
| veto | 跳过你，按 fail-open 计数（其他就绪插件照常参与） |
| deliver | 事件写入 WAL 积压，协商成功后补投 |
| observe | 内存积压（满则丢最旧），协商成功后补投 |

**协商失败**（无 manifest / 未签名 / 解析失败 / `schema`/`protocol`
不匹配 / id 或 version 为空）= 保持未就绪并退避重试；Avalon 启动**不会**
因为插件不可达而失败——启动只被**非法配置**挡住。

## 插件自报预算与 Avalon 上限

| 配置 | 默认 | 作用 |
|---|---|---|
| manifest `timeoutMs` | 无 = 用 Avalon 上限 | 插件自报单次处理预算，Avalon 取 min |
| `admissionTimeoutMs` | 2500ms | room.create / player.join / connection.new / game.start 准入与 manifest 协商的绝对上限 |
| `gameplayVetoTimeoutMs` | 250ms | unit.operation / factory.build 否决上限（阻塞发起者指令流，预算小） |
| `deliveryTimeoutMs` | 10000ms | 单次投递尝试上限 |
