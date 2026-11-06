package app

import (
	"strings"
	"time"

	"rwhps/internal/data/global"
	"rwhps/internal/game"
	"rwhps/internal/game/external"
	"rwhps/internal/net/gateway"
	"rwhps/internal/util/file"
	"rwhps/internal/util/file/plugindata"
	"rwhps/internal/util/log"
)

// loadCascadeGateway 在启用 Cascade 后端契约时装配网关并启动 /v1 控制面.
//
// 有意修正（作者已批准，2026-11-04）: Go Server 作为 Cascade 的后端,
// 方案 B 单房间合规体 (DECISIONS D-17~D-21); 对端信任为三档
// (open/ip/pinned, DECISIONS D-24): 默认裸奔, 可选 IP 白名单或
// IP + 客户端证书指纹. 未启用 (cascadeEnable=false) 时为无操作,
// 行为与现网完全一致.
//
// 启用但配置缺失/非法时返回错误使启动失败 —— 网关是显式配置,
// 不得静默降级 (那会让代理与后端的信任假设错位).
func loadCascadeGateway() error {
	cfg := global.Config
	if !cfg.CascadeEnable {
		return nil
	}

	roomCode := cfg.CascadeRoomCode
	if roomCode == "" {
		roomCode = global.ConfigServer.ServerID
	}

	// 信任三档解析 (DECISIONS D-24): 档位文本 / IP 白名单 / 指纹集,
	// 任一非法都让启动显式失败.
	trustMode, err := gateway.ParseTrustMode(cfg.CascadeTrustMode)
	if err != nil {
		return err
	}
	trustedIPs, err := gateway.ParseIPAllowList(cfg.TrustedCascadeIPs)
	if err != nil {
		return err
	}
	fingerprints, err := gateway.ParseFingerprints(cfg.CascadePinnedFingerprints)
	if err != nil {
		return err
	}

	// jti 一次性账本: 复用 plugindata Bin 存储, 独立文件 (DECISIONS D-19)
	jtiStore, err := plugindata.Open(file.GetFolder("/data").ToFile("CascadeJti.bin"))
	if err != nil {
		log.Warnf("[Cascade]", "jti store decode failed, starting fresh: {0}", err)
	}

	gw, err := gateway.New(gateway.Config{
		BackendID:          cfg.CascadeBackendID,
		RoomCode:           roomCode,
		TrustMode:          trustMode,
		TrustedIPs:         trustedIPs,
		PinnedFingerprints: fingerprints,
		TLSCert:            cfg.CascadeTLSCert,
		TLSKey:             cfg.CascadeTLSKey,
		ControlAddr:        cfg.CascadeControlAddr,
		HelperReady:        helperReadyProbe(),
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

	log.Clog("[Cascade] gateway enabled: backend={0} room={1} control={2} trust={3}",
		cfg.CascadeBackendID, roomCode, cfg.CascadeControlAddr, trustMode)
	return nil
}

// helperReadyProbe 返回 /v1/health 的 helper 就绪探针 (DECISIONS D-21 修订):
//   - 未配置 helper: 返回 nil, ready 判据维持现状;
//   - 配置了 helper 且已装上: 返回 external.Backend.Ready (非阻塞);
//   - 配置了 helper 但装载失败 (回落无后端): 返回恒 false 探针 ——
//     配置了就必须等 helper 就绪, 装不上则永不就绪 (不静默降级).
func helperReadyProbe() func() bool {
	if strings.TrimSpace(global.Config.GameHelperPath) == "" {
		return nil
	}
	if eb, ok := game.Backend.(*external.Backend); ok {
		return eb.Ready
	}
	return func() bool { return false }
}
