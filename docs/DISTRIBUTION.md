# DISTRIBUTION —— RW-HPS 稳定壳分发指南

分发形态：**Go 二进制（运行时主体）+ Helper.jar（Java 引擎壳）+ 游戏数据**，
一个目录即可独立运行，不依赖仓库环境。

## 包内容

```
rwhps-<version>-linux-<arch>/
├── rwhps                  # Go 服务端二进制 (运行时主体: 网关/房间/协议/控制台)
├── Helper.jar             # Java 引擎壳 (fat jar, 由 Go 侧按需自动拉起)
├── start.sh               # 启动脚本 (cd 到脚本目录并 exec ./rwhps)
├── DISTRIBUTION.md        # 本文档
└── data/
    ├── Config.json        # 核心配置 (端口/日志/helper 路径, 见下)
    ├── ConfigServer.json  # 房间配置 (enableAI/回放开关等)
    └── gameData/          # 游戏数据 (引擎必需, 勿删)
```

运行时还会在 `data/` 下生成：`Settings.bin`（EULA/语言等状态）、
`replays/`（回放文件）、`logs/`。

## 运行要求

- Linux x86_64/aarch64（二进制为静态链接的 Go 产物）；
- **JDK/JRE 17** 的 `java` 在 `PATH` 中，或在 `data/Config.json` 的
  `javaPath` 里显式指定（引擎与 Kotlin 运行时按 JDK 17 验证）；
- 内存建议 ≥1 GB（helper 默认 `-Xmx512m`，引擎常驻约 450 MB）。

## 启动步骤

```sh
tar xzf rwhps-<version>-linux-<arch>.tar.gz
cd rwhps-<version>-linux-<arch>
./start.sh            # 或直接 ./rwhps
```

首次启动会显示 EULA 确认（输入 `Yes` 同意）。随后自动执行默认启动命令
（`defStartCommand`，默认 `start`）：加载游戏内核（拉起 Helper.jar）→
监听 `data/Config.json` 里的 `port`（默认 5123）。

看到 `Start listening ...` 即就绪。控制台可用命令：`help` / `status` /
`addai`（需 `enableAI=true`）/ `start` / `stop` / `gameover` / `exit` 等。

## 关键配置（data/Config.json）

| 键 | 说明 |
| --- | --- |
| `port` | 游戏监听端口（默认 5123） |
| `gameHelperPath` | Helper.jar 路径（相对包根目录，默认 `./Helper.jar`）。**勿删此键**，否则装不上游戏内核 |
| `gameHelperArgs` | 追加给 JVM 的参数（默认 `["-Xmx512m"]`） |
| `javaPath` | java 可执行文件路径（空 = 用 `PATH` 里的 `java`） |
| `log` | 日志级别（默认 `WARN`） |

配置缺失的键会在首次启动时以默认值补齐并落盘。

## 升级方式（不丢配置）

配置与游戏产物都在 `data/` 目录，升级只替换可执行物：

1. 停服（控制台 `exit`，或 SIGTERM——**不要 SIGKILL**，退出路径含配置保存）；
2. 用新包覆盖 `rwhps` 与 `Helper.jar` 两个文件；
3. **保留整个 `data/` 目录**（Config.json / ConfigServer.json / Settings.bin /
   replays 都在里面）；
4. 重新启动。

只升 Go 二进制或只升 Helper.jar 均可（两者经 stdin/stdout 线协议解耦，
协议版本 `v2`，见 `Go-Server/docs/HELPER-PROTOCOL.md`）。

> 注：退出时 Go 与 helper 会先后写配置，Go 侧在 helper 退出后最后写回，
> 保证 `gameHelperPath` 等 Go 独有键不丢（2026-11-05 修复）。

## 构建分发包（开发者）

```sh
# 仓库根目录
JAVA_HOME=<jdk17> ./gradlew :Helper:jar   # 先构建引擎壳
./dist/build-dist.sh                       # 产出 dist/out/rwhps-*.tar.gz
```

## 验收记录

分发自足性由 `Go-Server/internal/app/dist_e2e_test.go`
（`TestDistributionSelfContained`）持续验证：干净目录组装 → 启动 →
真实客户端 TCP 直连预注册握手（160→161）→ 控制台 `addai`×2 + `start` →
引擎真实对局 → `gameover` 结算 → 回放文件落盘 → `exit` 优雅退出 →
退出后 `gameHelperPath` 仍在。
