// rwhps 主程序入口 (对应 net.rwhps.server.Main)
//
// 当前阶段 (Todo #5): 仅完成网络核心层, 提供最简启动验证.
// 主流程:
//   1. 初始化 i18n / config
//   2. 创建 NetService 并 OpenPort
//   3. 等待 SIGINT / SIGTERM 优雅关闭
//
// 完整游戏逻辑/启动流程/统计上报等在 #8/#9 实现.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"rwhps/internal/data/global"
	"rwhps/internal/net"
	"rwhps/internal/net/core"
	"rwhps/internal/util/log"
)

func main() {
	fmt.Println("RW-HPS Go Server starting...")

	// 1. 初始化全局数据 (配置/i18n)
	if err := bootstrap(); err != nil {
		log.Errorf("bootstrap failed: %v", err)
		os.Exit(1)
	}

	// 2. 创建 NetService (用 NullRwHps 占位)
	ns := net.NewNetService(net.CoreID(), core.NewNullRwHps(core.ServerProtocol))

	// 3. 启动监听
	go func() {
		ns.OpenPort(global.Config.Port)
	}()

	// 4. 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	fmt.Printf("\nReceived signal %v, shutting down...\n", sig)
	ns.Stop()
	fmt.Println("Bye.")
}

// bootstrap 初始化配置 (本次仅占位)
func bootstrap() error {
	// TODO: i18n bundle / config load (Data.i18NBundle / Data.config)
	global.Config.Port = 5123
	return nil
}