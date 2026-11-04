package app

import (
	"time"

	"rwhps/internal/data/global"
	"rwhps/internal/net/gateway"
	"rwhps/internal/util/file"
	"rwhps/internal/util/file/plugindata"
	"rwhps/internal/util/log"
)

// loadCascadeGateway 在启用 Cascade 后端契约时装配网关并启动 /v1 控制面.
//
// 有意修正（作者已批准，2026-11-04）: Go Server 作为 Cascade 的后端,
// 方案 B 单房间合规体 (DECISIONS D-17~D-21). 未启用 (cascadeEnable=false)
// 时为无操作, 行为与现网完全一致.
//
// 启用但材料缺失/非法时返回错误使启动失败 —— 网关是显式配置,
// 不得静默降级回明文 (那会让代理与后端的信任假设错位).
func loadCascadeGateway() error {
	cfg := global.Config
	if !cfg.CascadeEnable {
		return nil
	}

	roomCode := cfg.CascadeRoomCode
	if roomCode == "" {
		roomCode = global.ConfigServer.ServerID
	}

	// jti 一次性账本: 复用 plugindata Bin 存储, 独立文件 (DECISIONS D-19)
	jtiStore, err := plugindata.Open(file.GetFolder("/data").ToFile("CascadeJti.bin"))
	if err != nil {
		log.Warnf("[Cascade]", "jti store decode failed, starting fresh: {0}", err)
	}

	gw, err := gateway.New(gateway.Config{
		BackendID:          cfg.CascadeBackendID,
		RoomCode:           roomCode,
		TLSCert:            cfg.CascadeTLSCert,
		TLSKey:             cfg.CascadeTLSKey,
		ClientCA:           cfg.CascadeClientCA,
		ControlAddr:        cfg.CascadeControlAddr,
		ProxyIssuer:        cfg.CascadeProxyIssuer,
		ProxyKeyID:         cfg.CascadeProxyKeyID,
		ProxyPublicKeyPath: cfg.CascadeProxyPublicKeyPath,
		// 0 -> gateway 默认 5 秒 (契约 :58)
		PendingTimeout: time.Duration(cfg.CascadePendingTimeoutSec) * time.Second,
	}, gateway.NewJtiStore(jtiStore))
	if err != nil {
		return err
	}

	global.CascadeGateway = gw

	// 控制面独立协程常驻; 游戏口的网关挂接在 startnetservice 命令里完成
	// (那里才创建游戏协议的 NetService).
	go func() {
		if err := gw.ServeControl(); err != nil {
			log.Errorf("[Cascade]", "control API exited: {0}", err)
		}
	}()

	log.Clog("[Cascade] gateway enabled: backend={0} room={1} control={2}",
		cfg.CascadeBackendID, roomCode, cfg.CascadeControlAddr)
	return nil
}
