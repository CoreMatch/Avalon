# 从零写一个 Hello 插件

目标：10 分钟内跑起来一个真实插件——Avalon 建房时咨询它、对局结束时
推送结算、有人被踢时收到审计通知。

参考实现就在本目录 [template/](template/)（Go，标准库），本文所有代码
都从那里来，且经过真 hub 集成测试验证（`internal/plugin/template_e2e_test.go::TestTemplatePluginRunsAgainstHub`）。

## 0. 先搞清楚方向

插件是 HTTP **服务端**，Avalon 是客户端：

```
Avalon ──GET /v1/manifest──────────▶ 你的插件   (握手/协商)
Avalon ──POST /v1/events/<name>────▶ 你的插件   (事件推送/准入咨询)
```

你不需要知道 Avalon 的地址，不需要主动连它，也没有任何插件→Avalon 的
调用通道（见 [NON-GOALS.md](NON-GOALS.md)）。

## 1. 起进程

任何语言都行，只要提供两个端点。Go 模板的入口
（`docs/plugins/template/main.go::main`）：

```bash
go build -o hello-plugin .
./hello-plugin -addr 127.0.0.1:9800 -secret <hex 密钥>
```

`-secret` 是与 Avalon 共享的密钥（至少 16 字节），用
`openssl rand -hex 32` 生成，两边必须一致（见
[AUTH-AND-NEGOTIATION.md](AUTH-AND-NEGOTIATION.md)）。

## 2. 握手：manifest

Avalon 启动时（以及每次与你断联后）会来拉
`GET /v1/manifest`。你返回自我描述，声明**你订阅哪些事件**：

```json
{
  "schema": "avalon-plugin/manifest",
  "protocol": "avalon-plugin/v1",
  "id": "hello-plugin",
  "version": "0.1.0",
  "events": {
    "room.create":   { "versions": [1], "veto": true },
    "game.over":     { "versions": [1] },
    "player.kicked": { "versions": [1] }
  },
  "timeoutMs": 2000,
  "maxConcurrency": 1
}
```

- `schema` / `protocol` 必须**精确匹配**这两个值，否则协商失败；
- `events` 里没有的事件不会推给你；`versions` 与 Avalon 无交集的事件
  同样不会推给你（当前 Avalon 只发版本 1）；
- `veto: true` 是"我想参与否决"的**声明**，对需要授权的事件还须运营
  在 Avalon 配置里授权才生效（默认拒绝，见
  [EVENT-CATALOG.md](EVENT-CATALOG.md) 的门控列）；
- `timeoutMs` 是自报处理预算，Avalon 会用它和自己的上限取 min；
- 应答必须签名（见鉴权文档），否则 Avalon 视为协商失败。

模板里的实现在 `docs/plugins/template/plugin.go::serveManifest`。

## 3. 收事件与应答

所有事件都是 `POST /v1/events/<事件名>`，正文 JSON，带签名头。
模板的分发在 `docs/plugins/template/plugin.go::serveEvent`。

**观察档**（如 `player.kicked`）：记日志/入库，返回 2xx 即可，正文忽略：

```go
p.Handle("player.kicked", false, func(body []byte) (int, []byte) {
    log.Printf("[player.kicked] %s", body)
    return http.StatusOK, []byte(`{}`)
})
```

**投递档**（`game.over`）：同样返回 2xx 确认，但它是至少一次投递——
**必须按 `eventId` 去重**（保留去重状态 ≥24h），重试与重启重放都复用
同一 `eventId`。

**否决档**（如 `room.create`）：应答必须是裁决 JSON：

```go
p.Handle("room.create", true, func(body []byte) (int, []byte) {
    if 不允许 {
        return http.StatusOK, VerdictJSON(Verdict{
            Allowed: false,
            Reason:  "维护中 22:00 后开放", // 必填且可读!
        })
    }
    return http.StatusOK, VerdictJSON(Verdict{Allowed: true})
})
```

两条铁律：

1. `allowed: false` **必须带 `reason`**。无理由的拒绝被 Avalon 当作畸形
   应答，直接 fail-open——你的否决等于没发；
2. 快。否决咨询有硬超时（默认上限 2500ms，对局内事件 250ms），超时
   同样 fail-open。理由会净化后呈现给对端（去控制符、截断 200 rune），
   净化实现在 `internal/plugin/hub.go::sanitizeReason`。

## 4. 在 Avalon 登记

编辑 Avalon 的 `/data/Plugins.json`（不存在时首次启动会生成默认值）：

```json
{
  "version": 1,
  "entries": [
    {
      "name": "hello",
      "baseUrl": "http://127.0.0.1:9800",
      "secrets": ["<hex 密钥，与 -secret 一致>"],
      "enabled": true,
      "vetoEvents": []
    }
  ]
}
```

- `name` 唯一，用于日志与状态；`baseUrl` 必须是绝对 http(s) 地址；
- `secrets` 是 hex 列表，第一个为主密钥（Avalon 用它签名），全部用于
  验签——轮换时新旧并存即可；
- `vetoEvents` 是授权清单：`player.join` / `unit.operation` /
  `factory.build` / `connection.new` / `game.start` 只有写进这里（且
  manifest 声明了 `veto`）才参与否决；`room.create` 订阅即参与，写进
  去反而会让启动报错（配置校验见 `internal/plugin/config.go::validate`）。

**配置纪律**：非法配置（坏 JSON、重复名字、非法 URL、短密钥、未知
否决事件）会让 Avalon **启动失败**，不会静默降级。

重启 Avalon，日志出现：

```
[Plugin] hello ready: id=hello-plugin version=0.1.0 events=[room.create, game.over, player.kicked]
```

握手成功。控制台 `plugin list` 可随时查看各插件状态与计数。

## 5. 验证你的插件

- 模板自带纯标准库自测试（`docs/plugins/template/plugin_test.go`），
  拷到任何仓库都能 `go test .`；
- 在 Avalon 仓内，`internal/plugin/template_e2e_test.go::TestTemplatePluginRunsAgainstHub`
  会把模板编译成二进制、起真进程、接真 hub 走一遍
  协商/放行/否决/投递/密钥错配——改模板后跑它；
- 运维视角看 `plugin list` 的 `delivered / retries / dropped / denied /
  failOpen` 五个计数，含义见 [FAILURE-MODES.md](FAILURE-MODES.md)。
