# RW-HPS Go Server

RW-HPS（Rusted Warfare Headless Project Server）的 Go 复刻版。目标客户端 **1.15 / 协议号 176**。

本目录是 Go 实现；Kotlin/Java 原版位于仓库其它模块，两者功能对照见
`/home/lnb/SamuelPortal-docs/RW-HPS-GO-PARITY.md`。

---

## 这个版本和原版的关键区别

原版自己跑在 JVM 里，用 ASM 改写闭源 `rusted-warfare.jar` 的字节码，把游戏内核留在进程内。
Go 无法执行 Java 字节码，因此**游戏模拟内核被外置为一个独立的 Java 进程（下称 helper）**，
Go 服务端与它之间用一条文本协议（protocol v2，见 `docs/HELPER-PROTOCOL.md`）通信。

由此带来两种运行形态：

| 形态 | 条件 | 能力 |
| --- | --- | --- |
| **无 helper** | 不配置 `gameHelperPath` | 握手、房间/玩家管理、命令、事件、Relay 全部可用；**开局会返回 `game: no simulation backend available`** |
| **有 helper** | 配置 `gameHelperPath` 指向一个实现了 protocol v2 的 Java helper | 上述全部 + 真实开局/游戏模拟 |

> 没有 helper 时服务器依然能完整启动（控制台、HTTP、Relay 都不依赖它），只是无法真正开局。
> helper 本体不在本仓库内，需要按 `docs/HELPER-PROTOCOL.md` 另行实现/获取。

---

## 构建

需要 Go 1.22+。

```bash
cd Go-Server
go build -o rwhps ./cmd/rwhps
```

产物为当前目录下的 `rwhps` 可执行文件。

开发时的完整校验（提交前必须全绿）：

```bash
go build ./...
go vet ./...
go test ./...          # 需要时追加 -race
```

---

## 运行

```bash
./rwhps
```

可选命令行参数：

- `-noPrint`：不打印交互输出（丢弃 stdin/stdout）。
- `-noGui`：无 GUI（Go 版本无 GUI 概念，仅为兼容原版参数）。
- `-Dkey=value`：覆盖配置项（见下文「配置覆盖」）。

首次运行会在**数据根目录**生成 `data/` 及配置文件、语言包探测等。数据根目录默认是
可执行文件所在目录，可用 `-Drwhps.basePath=/path/to/data-root` 或环境变量
`rwhps.basePath` 指定。

启动后进入控制台，直接输入命令（如 `help`）。默认启动命令由配置 `defStartCommand`
决定（缺省 `start`，即自动以 Server 协议开局监听）。

---

## 配置

配置都是 JSON，位于数据根目录的 `data/` 下，首次启动自动生成默认值：

| 文件 | 内容 | `-D` 覆盖前缀 |
| --- | --- | --- |
| `data/Config.json` | 核心配置（端口、日志、Web/RCON、**外部 helper 三项**等） | `rwhps.config.core` |
| `data/ConfigServer.json` | Server 协议房间配置（人数、自动开局、禁言等） | `rwhps.config.server` |
| `data/RelayConfig.json` | Relay 协议配置 | `rwhps.config.relay` |
| `data/Settings.bin` | 身份/封禁等持久化（部分字段 Go 版暂只读内存，见代码注释） | — |

### 配置覆盖

任何配置字段都可以用 `-D<前缀>.<字段名>=<值>` 在命令行覆盖（值若为 Base64 会自动解码）：

- 核心配置前缀 `rwhps.config.core`，例如：
  ```bash
  ./rwhps -Drwhps.config.core.port=5123 -Drwhps.config.core.log=INFO
  ```
- 也可用同名环境变量（命令行 `-D` 优先）。

### 关键字段（Config.json）

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `port` | `5123` | 游戏端口 |
| `defStartCommand` | `start` | 启动后自动执行的控制台命令 |
| `log` | `WARN` | 日志等级 |
| `serverName` / `subtitle` | `RW-HPS` / 空 | 服务器名 / 副标题（空则用地图名） |
| `autoUpList` | `false` | 启动后自动上报服务器列表（Relay 协议） |
| `webPort` / `webToken` | `0` / 随机 | 不为 0 时启用 HTTP 面板 |
| `javaPath` | 空 | helper 用的 java 可执行文件；空=用 `PATH` 里的 `java` |
| `gameHelperPath` | 空 | **外部游戏内核入口（jar 路径或主类名）；留空=不启用 helper** |
| `gameHelperArgs` | 空 | 追加给 java 的参数，如 `["-Xmx512m"]` |

---

## 安装并接入外部游戏内核（helper）

只有需要**真实开局**时才需要这一步。

1. **准备运行环境**：安装 Java（helper 是 Java 进程）。确认 `java -version` 可用，
   或在 `javaPath` 里给出绝对路径。
2. **获取/构建 helper**：helper 是一个实现 protocol v2 的 Java 程序（内含闭源游戏内核），
   本仓库不提供。协议合同见 [`docs/HELPER-PROTOCOL.md`](docs/HELPER-PROTOCOL.md)。
3. **配置**：在 `data/Config.json` 填入（或用 `-D` 覆盖）：
   ```json
   {
     "javaPath": "",
     "gameHelperPath": "/path/to/rwhps-helper.jar",
     "gameHelperArgs": ["-Xmx512m"]
   }
   ```
   - `gameHelperPath` 相对路径会锚定到数据根目录；裸文件名按 `PATH`/当前目录解释。
4. **启动并确认**：日志出现 `server.load.gameCore`（外部内核已接入）即成功。
   若配置有误，启动日志会给出 `external game core unavailable, falling back to no backend`，
   服务器仍以无内核形态运行（开局报错）。

helper 子进程的生命周期由 Go 侧托管：服务器退出时会先 `stop`、关 stdin、再回收。

---

## 常用控制台命令

| 命令 | 说明 |
| --- | --- |
| `help` | 列出命令 |
| `start` | 以 Server 协议开局并监听游戏端口 |
| `startrelay` | 以 Relay 协议启动（端口区间 5201-5500） |
| `startrelaytest` | Relay 多播测试（端口区间 7000-8000） |
| `stop` | 停止全部网络服务 |
| `version` | 版本与当前房间信息 |
| `players` / `admins` | 在线玩家 / 管理员 |
| `say <text>` | 向全体广播 |
| `kick <位置> [秒]` / `ban <位置>` / `mute <位置>` | 踢出 / 封禁 / 禁言 |
| `gameover` | 强制结束本局 |
| `uplist add\|update\|remove\|help` | 服务器列表上报（需 helper/Relay 场景） |
| `exit` | 退出 |

游戏内聊天命令（玩家侧）以 `.`、`-`、`_` 开头，或 `/` 前缀；Relay 场景用 `.` 前缀。
具体清单见 `internal/command/`（ServerCommands/ClientCommands）与
`internal/command/relay/`、`internal/net/uplist/`。

---

## 目录结构（Go-Server）

```
cmd/rwhps/            程序入口
internal/
  app/                启动链（对应原版 Main/Initialization/Application）+ 全局事件
  command/            控制台与客户端命令（含 ex/Vote、relay/）
  core/               线程池、定时任务标识、服务装载
  data/               全局数据、配置 bean、管理数据（封禁/管理员）
  game/               游戏层：事件总线、房间、玩家、管理、外部内核接缝(external)
  io/                 字节流与包（含 GameCommandOnePacket 解析）
  net/                网络层：协议实现（gameserver/Relay）、HTTP、服务、解码
  resource/           内嵌语言包与静态资源
  struct/             容器（Seq/Map）
  timetask/           定时任务（Quartz 语义的 Go 重写）
  util/               工具（日志、加解密、文件、压缩、i18n 等）
```

---

## 许可证

与上游 RW-HPS 一致：GNU AGPLv3。
