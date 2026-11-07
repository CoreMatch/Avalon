#!/bin/bash
# build-dist.sh —— 构建可独立运行的分发包 (Linux 优先)
#
# 产物: dist/out/avalon-<git短hash>-linux-<arch>.tar.gz
# 内容: avalon 二进制 + avalon-helper.jar + data/gameData + 默认配置 + 启动脚本
#
# 前置:
#   - 本仓 (Avalon): Go 二进制
#   - Avalon-Helper 检出 (环境变量 AVALON_HELPER_REPO 指向): avalon-helper.jar 与 data/gameData,
#     且已构建 avalon-helper.jar: cd $AVALON_HELPER_REPO && JAVA_HOME=<jdk17> ./gradlew :Helper:jar
set -e

REPO=$(cd "$(dirname "$0")/.." && pwd)
OUT=${1:-$REPO/dist/out}
VERSION=$(git -C "$REPO" rev-parse --short HEAD 2>/dev/null || echo dev)
ARCH=$(uname -m)
case $ARCH in
    x86_64) ARCH=amd64 ;;
    aarch64) ARCH=arm64 ;;
esac
NAME=avalon-$VERSION-linux-$ARCH
STAGE=$OUT/$NAME

rm -rf "$STAGE"
mkdir -p "$STAGE/data"

# 1. Go 二进制 (运行时主体)
(cd "$REPO" && go build -trimpath -o "$STAGE/avalon" ./cmd/avalon)

# 2. avalon-helper.jar (Java 引擎壳, 来自 Avalon-Helper 仓, 需预先 ./gradlew :Helper:jar)
if [ -z "$AVALON_HELPER_REPO" ]; then
    echo "缺少环境变量 AVALON_HELPER_REPO —— 请指向 Avalon-Helper 检出 (含 Helper/ 与 data/gameData)" >&2
    exit 1
fi
JAR=$AVALON_HELPER_REPO/Helper/build/libs/avalon-helper.jar
if [ ! -f "$JAR" ]; then
    echo "缺少 $JAR —— 先在该检出根执行: JAVA_HOME=<jdk17> ./gradlew :Helper:jar" >&2
    exit 1
fi
cp "$JAR" "$STAGE/avalon-helper.jar"

# 3. 游戏数据 (引擎必需, 来自 Avalon-Helper 仓)
cp -r "$AVALON_HELPER_REPO/data/gameData" "$STAGE/data/gameData"

# 4. 默认配置 / 启动脚本 / 文档
#    端口与 JVM 参数按需改 data/Config.json; 详见 docs/DISTRIBUTION.md
cp "$REPO/dist/Config.json" "$STAGE/data/Config.json"
cp "$REPO/dist/ConfigServer.json" "$STAGE/data/ConfigServer.json"
cp "$REPO/dist/start.sh" "$STAGE/start.sh"
chmod +x "$STAGE/start.sh" "$STAGE/avalon"
cp "$REPO/docs/DISTRIBUTION.md" "$STAGE/DISTRIBUTION.md"

# 5. 打包
mkdir -p "$OUT"
tar czf "$OUT/$NAME.tar.gz" -C "$OUT" "$NAME"

SIZE_TGZ=$(du -h "$OUT/$NAME.tar.gz" | cut -f1)
SIZE_DIR=$(du -sh "$STAGE" | cut -f1)
echo "分发包: $OUT/$NAME.tar.gz (压缩 $SIZE_TGZ / 解压 $SIZE_DIR)"
