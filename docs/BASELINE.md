# BASELINE —— 稳定壳收尾回归基线（2026-11-05）

本文档是"稳定壳"收尾时的实测基线，供后续任何改动做对照。
**改动后若这些数字显著漂移，先解释原因再合并。**

测量环境：Linux x86_64，go1.26.4（go.mod 要求 1.22+），
OpenJDK 17.0.21（`/home/lnb/jdk-17.0.21+9`）。
分支 `chore/stable-shell-final`（`abc1dbd2`，干净克隆验证；
分发包与干净目录实跑在 `f1049de5`）。

---

## 1. E2E 矩阵（真实 helper，干净克隆 verbose 实跑）

`TestE2ERealGameMatrix`（每组独立 helper 进程）+ `TestE2ERealGameToSettlement`：

| 组 | 地图 | 玩家构成 | 设置 | 开局耗时 | 结算 | 回放文件（实体断言） | 单组测试时长 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 小图2p-全AI-默认 | `Small Island (2p)` | 全 AI（2） | 默认 | 3 ms | 获胜 `[AI - Hard]`，时长 8s | `Small Island (2p) [v1.15] ….replay`（3,370 B） | 11.94 s |
| 中图4p-客户端混合-默认 | `Desert (4p)` | 2 真实协议客户端 + 2 AI | 默认 | 2 ms | 获胜 `[AI - Hard]`，时长 8s | `Desert (4p) [v1.15] ….replay`（3,480 B） | 11.16 s |
| 大图10p-客户端混合-非默认设置 | `Crossing Large (10p)` | 真实协议客户端 + AI | `room.settings`：income=2.0 / disableNuke=true / maxUnits=300 | 2 ms | 获胜 `[AI - Hard]`，时长 8s | `Crossing Large (10p) [v1.15] ….replay`（7,231 B） | 11.07 s |

- 全部到结算；回放文件按**真实地图**命名（结算数据里的 `mapName` 字段是引擎
  静态默认图名，headless 下不随 room.start 更新——已知现象，实际地图以回放
  文件名为准，测试注释有记录）。
- `TestE2ERealGameToSettlement`（Crossing Large 10p，2 AI，直连预注册握手）：
  干净克隆 12.73 s；worktree `-count=2` 11.22 s / 11.81 s。
- 已知时序：胜负判定后 `getGameOverData` 即非空，但 `replayName` 由引擎
  game-over 流程 "Recording replay to:" 重定向异步写入（晚于胜负判定）；
  测试轮询等待回放名落定，60 s 上限（本轮定位并修复，见 `abc1dbd2`）。

## 2. 分发包（`dist/build-dist.sh`）

包：`rwhps-f1049de5-linux-amd64.tar.gz`

| 项 | 数值 |
| --- | --- |
| 压缩体积 | 33,196,595 B（≈32 M） |
| 解压体积 | 46 M（du） |
| `rwhps` 二进制（go build -trimpath） | 12,775,204 B |
| `Helper.jar` | 23,307,356 B |
| `data/gameData` | 7,971,187 B（表观）/ ≈12 M（磁盘占用） |

干净目录实跑（`/tmp/clean-run2`，解包 → 改端口/javaPath → `./start.sh`）：

| 环节 | 证据 |
| --- | --- |
| 冷启动 → `Start listening` | **3.52 s**（05:27:21 `External game core attached: ./Helper.jar` → 05:27:24 监听） |
| 无头开局 | 控制台 `addai`×2（`Added 1 AI`）→ `start` |
| 结算 | `gameover` → 回放 `Crossing Large (10p) [v1.15] (5 Nov 2026 05.27.25).replay`（6,198 B） |
| 优雅退出 | `exit` → **退出码 0** |
| 配置回写 | 退出后 `data/Config.json` 仍含 `gameHelperPath`（Java 侧整文件重写竞态已修复） |

`TestDistributionSelfContained`（自动化同上全流程）：干净克隆 23.55 s，
worktree 21.94 s，`-count=2` 稳定。

## 3. JVM 侧（第六轮基线，各 3 次均值，`8f0e16ef`）

测量法见 `REPO-SLIM.md` 阶段三-1（独立进程拉真实 jar，`-Xmx512m` +
`-Xlog:class+load`）：

| 指标 | 数值 |
| --- | --- |
| boot_done（进程启动 → 协议循环就绪） | 1,165 ms |
| engine_socket（→ 引擎网络就绪） | 3,926 ms |
| 常驻 RSS（就绪后 3 s） | ≈455 MB |
| 加载类总数 | 5,344（rwhps 类 479） |

结论维持：启动时间由闭源引擎装载主导（≈3-4 s），jar 瘦身不改变启动速度。

## 4. 仓库与测试面

| 项 | 数值 |
| --- | --- |
| `git archive` | 25,036,800 B |
| JVM 侧行数 / 文件数 | 32,666 行 / 380 文件 |
| Go 测试包 | `go test ./... -count=1` 全绿（干净克隆复验） |
| i18n 键数基线（EN 包） | 205（`TestI18NBundleLoad` 钉死） |
| 全量测试主要耗时 | `internal/net` ≈47-50 s（含矩阵 + 真实 E2E），`internal/app` ≈24 s（分发自足性），`internal/net/gateway` ≈20 s |

## 5. 复测方法

```bash
# 干净克隆 + 引擎壳
git clone -b <branch> <repo> /tmp/check && cd /tmp/check
JAVA_HOME=<jdk17> ./gradlew :Helper:jar

# Go 三件套（真实集成测试自动发现 Helper.jar 与 JDK 17）
cd Go-Server
go build ./... && go vet ./... && go test ./... -count=1

# 分发包 + 干净目录实跑
cd .. && ./dist/build-dist.sh
mkdir /tmp/run && tar xzf dist/out/rwhps-*.tar.gz -C /tmp/run --strip-components=1
cd /tmp/run && ./start.sh     # EULA 确认后: addai / start / gameover / exit
```

对照判据：矩阵三组全过且回放文件按真实地图命名；冷启动 ≤5 s；
退出码 0 且 `gameHelperPath` 未丢；RSS / 类数漂移 >10% 需解释。
