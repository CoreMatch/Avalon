#!/bin/sh
# RW-HPS 稳定壳启动器
#
# 布局约定 (见 DISTRIBUTION.md):
#   ./rwhps        Go 服务端 (运行时主体, 监听 data/Config.json 的 port)
#   ./Helper.jar   Java 引擎壳 (由 Go 侧按 gameHelperPath 自动拉起)
#   ./data/        配置与游戏数据 (Config.json / ConfigServer.json / gameData)
#
# 需要 JDK/JRE 17 的 java 在 PATH 中, 或在 data/Config.json 里配置 javaPath.
cd "$(dirname "$0")" || exit 1

if ! command -v java >/dev/null 2>&1; then
    echo "未找到 java: 请安装 JDK/JRE 17, 或在 data/Config.json 设置 javaPath" >&2
    exit 1
fi

exec ./rwhps "$@"
