# 插件模板（参考实现）

avalon-plugin/v1 契约（v1.2.0）的最小完整插件：manifest 协商、双向
HMAC 签名、事件分发、否决应答，只用 Go 标准库。

- `plugin.go` —— 契约机制（签名、manifest、路由），一般不用改；
- `main.go` —— Hello 插件的装配：订阅了什么、怎么应答，改这里；
- `plugin_test.go` —— 纯标准库自测试，拷到哪里都能跑。

## 作为独立插件使用

1. 把本目录整体拷到你的仓库，`go mod init your-plugin && go build .`；
2. 生成共享密钥（至少 16 字节）：`openssl rand -hex 32`；
3. 启动：`./your-plugin -addr 127.0.0.1:9800 -secret <hex>`；
4. 在 Avalon 的 `/data/Plugins.json` 登记（字段语义见
   `docs/plugins/GETTING-STARTED.md`）：

```json
{
  "version": 1,
  "entries": [
    {
      "name": "hello",
      "baseUrl": "http://127.0.0.1:9800",
      "secrets": ["<同一个 hex 密钥>"],
      "vetoEvents": []
    }
  ]
}
```

5. 重启 Avalon，看到日志 `[Plugin] hello ready: id=hello-plugin ...` 即
   握手成功。

`-deny` 标志可以体验否决语义：所有建房准入会被否决并给出理由。

## 在 Avalon 仓内

本目录属于 Avalon 模块，`go build ./...` 与 `go vet ./...` 会编译它，
保证模板不会悄悄坏掉；`internal/plugin/template_e2e_test.go` 会把模板
二进制接到真实 hub 上走一遍协商/否决/投递。
