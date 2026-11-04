#!/bin/bash
# build-dist.sh —— 构建可独立运行的分发包 (Linux 优先)
#
# 产物: dist/out/rwhps-<git短hash>-linux-<arch>.tar.gz
# 内容: rwhps 二进制 + Helper.jar + data/gameData + 默认配置 + 启动脚本
#
# 前置: ./gradlew :Helper:jar (JDK17) 已构建出 Helper/build/libs/Helper.jar
set -e

REPO=$(cd "$(dirname "$0")/.." && pwd)
OUT=${1:-$REPO/dist/out}
VERSION=$(git -C "$REPO" rev-parse --short HEAD 2>/dev/null || echo dev)
ARCH=$(uname -m)
case $ARCH in
    x86_64) ARCH=amd64 ;;
    aarch64) ARCH=arm64 ;;
esac
NAME=rwhps-$VERSION-linux-$ARCH
STAGE=$OUT/$NAME

rm -rf "$STAGE"
mkdir -p "$STAGE/data"

# 1. Go 二进制 (运行时主体)
(cd "$REPO/Go-Server" && go build -trimpath -o "$STAGE/rwhps" ./cmd/rwhps)

# 2. Helper.jar (Java 引擎壳, 需预先 ./gradlew :Helper:jar)
JAR=$REPO/Helper/build/libs/Helper.jar
if [ ! -f "$JAR" ]; then
    echo "缺少 $JAR —— 先在仓库根执行: JAVA_HOME=<jdk17> ./gradlew :Helper:jar" >&2
    exit 1
fi
cp "$JAR" "$STAGE/Helper.jar"

# 3. 游戏数据 (引擎必需)
cp -r "$REPO/data/gameData" "$STAGE/data/gameData"

# 4. 默认配置 / 启动脚本 / 文档
#    端口与 JVM 参数按需改 data/Config.json; 详见 docs/DISTRIBUTION.md
cp "$REPO/dist/Config.json" "$STAGE/data/Config.json"
cp "$REPO/dist/ConfigServer.json" "$STAGE/data/ConfigServer.json"
cp "$REPO/dist/start.sh" "$STAGE/start.sh"
chmod +x "$STAGE/start.sh" "$STAGE/rwhps"
cp "$REPO/docs/DISTRIBUTION.md" "$STAGE/DISTRIBUTION.md"

# 5. 打包
mkdir -p "$OUT"
tar czf "$OUT/$NAME.tar.gz" -C "$OUT" "$NAME"

SIZE_TGZ=$(du -h "$OUT/$NAME.tar.gz" | cut -f1)
SIZE_DIR=$(du -sh "$STAGE" | cut -f1)
echo "分发包: $OUT/$NAME.tar.gz (压缩 $SIZE_TGZ / 解压 $SIZE_DIR)"
