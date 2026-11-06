package gateway

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"rwhps/internal/net/core"
	"rwhps/internal/util/log"
)

// Config 是网关装配所需的已解析配置 (由启动链从 bean.CoreConfig 构造).
//
// 有意修正（作者已批准，2026-11-04）: Cascade 后端契约的传输与信任部分
// 改为"信任三档" (DECISIONS D-24): 默认裸奔 (open), 可选 IP 白名单 (ip)
// 或 IP + 客户端证书指纹 (pinned). JWT 三档都不验签 (只做结构化声明校验),
// 契约原 mTLS + Ed25519 验签叙述以本实现为准 (待契约升版, 见 DECISIONS).
type Config struct {
	// BackendID 后端 ID (JWT aud / health rooms 的 backendId)
	BackendID string
	// RoomCode 对 Cascade 暴露的唯一房间码 (已含空值回落)
	RoomCode string

	// TrustMode 信任档位 (open/ip/pinned, DECISIONS D-24)
	TrustMode TrustMode
	// TrustedIPs 来源白名单 (ip 档必填; pinned 档可空 = 不做 IP 约束)
	TrustedIPs *ipAllowList
	// PinnedFingerprints pinned 档的客户端证书指纹集 (SHA-256 证书 DER)
	PinnedFingerprints [][]byte

	// TLSCert / TLSKey 服务端证书/私钥 (PEM 路径); 同时给出时传输走 TLS,
	// 留空即明文. 路径解析与缺失报错见 resolveMaterialPath (C3 回退策略).
	TLSCert string
	TLSKey string

	// ControlAddr /v1 控制面监听地址
	ControlAddr string

	// HelperReady 外部游戏内核 (helper) 就绪探针 (可选).
	// 非 nil 时纳入 /v1/health 的 ready 判据 (DECISIONS D-21 修订):
	// 配置了 helper 就必须等 helper 活着才算就绪.
	HelperReady func() bool

	// PendingTimeout PENDING 绑定限期 (契约 :58, 默认 5 秒)
	PendingTimeout time.Duration
}

// Gateway 是 Cascade 后端契约在 Go 侧的装配体:
// 游戏口传输+前导+会话门控, 控制面 /v1, jti 账本与令牌结构化校验.
type Gateway struct {
	cfg      Config
	registry *Registry
	jti      *JtiStore
	verifier *TokenVerifier
	tlsCfg   *tls.Config // nil = 明文传输 (裸奔)

	ready atomic.Bool

	// controlLn 控制面监听器, Stop 时关闭
	controlMu sync.Mutex
	controlLn net.Listener
}

// New 装配网关: 解析信任档位, 按需载入证书, 建会话注册表与 jti 账本.
// 材料缺失或配置非法都直接报错 —— 启用网关是显式配置, 不得静默降级.
func New(cfg Config, jti *JtiStore) (*Gateway, error) {
	if cfg.BackendID == "" || cfg.RoomCode == "" {
		return nil, errors.New("gateway: backend ID and room code are required")
	}
	if cfg.ControlAddr == "" {
		return nil, errors.New("gateway: control listen address is required")
	}

	// 档位相关的配置校验 (DECISIONS D-24):
	//  - ip 档白名单必填: 空名单要么锁死要么放行, 两者都不得静默发生;
	//  - pinned 档必须有 TLS (指纹来自客户端证书) 与至少一条指纹.
	if cfg.TrustMode == TrustIP && cfg.TrustedIPs.empty() {
		return nil, errors.New("gateway: trust mode \"ip\" requires a non-empty trustedCascadeIPs")
	}
	if cfg.TrustMode == TrustPinned && len(cfg.PinnedFingerprints) == 0 {
		return nil, errors.New("gateway: trust mode \"pinned\" requires at least one cascadePinnedFingerprints entry")
	}

	tlsCfg, err := loadServerTLS(cfg.TLSCert, cfg.TLSKey, cfg.TrustMode)
	if err != nil {
		return nil, err
	}
	if cfg.TrustMode == TrustPinned && tlsCfg == nil {
		return nil, errors.New("gateway: trust mode \"pinned\" requires cascadeTlsCert/cascadeTlsKey (client-cert fingerprint needs TLS)")
	}

	if jti == nil {
		return nil, errors.New("gateway: jti store is required")
	}
	g := &Gateway{
		cfg:      cfg,
		registry: NewRegistry(cfg.PendingTimeout),
		jti:      jti,
		verifier: NewTokenVerifier(cfg.BackendID),
		tlsCfg:   tlsCfg,
	}

	if cfg.TrustMode == TrustOpen {
		// 裸奔告警 (启动一次; 每连接来源 IP 记在 AcceptGame 的 debug 级)
		log.Clog("[Cascade] 裸奔模式已启用 (trust=open): 不校验来源与令牌签名, 游戏端口严禁暴露公网 (生产至少开 ip 档)")
	}
	return g, nil
}

// newTLSListener 把明文监听器包成 TLS 监听器 (控制面用; 游戏面在
// AcceptGame 里逐连接握手, 因为还要读前导)
func newTLSListener(ln net.Listener, cfg *tls.Config) net.Listener {
	return tls.NewListener(ln, cfg)
}

// loadServerTLS 构造服务端 TLS 配置.
//
// 传输与信任分离 (DECISIONS D-24): 证书/密钥都给出才启用 TLS, 否则明文;
// open/ip 档不索取客户端证书 (NoClientCert), pinned 档索取但不走 CA 校验
// (RequireAnyClientCert) —— 对端身份由握手中的证书指纹判定.
func loadServerTLS(certPath, keyPath string, mode TrustMode) (*tls.Config, error) {
	if certPath == "" && keyPath == "" {
		return nil, nil // 明文 (裸奔传输)
	}
	if certPath == "" || keyPath == "" {
		return nil, errors.New("gateway: cascadeTlsCert and cascadeTlsKey must be set together")
	}
	resolvedCert, err := resolveMaterialPath(certPath)
	if err != nil {
		return nil, err
	}
	resolvedKey, err := resolveMaterialPath(keyPath)
	if err != nil {
		return nil, err
	}
	cert, err := tls.LoadX509KeyPair(resolvedCert, resolvedKey)
	if err != nil {
		return nil, fmt.Errorf("gateway: load server certificate: %w", err)
	}
	clientAuth := tls.NoClientCert
	if mode == TrustPinned {
		clientAuth = tls.RequireAnyClientCert
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   clientAuth,
	}, nil
}

// Registry 返回会话注册表 (控制面绑定与诊断用)
func (g *Gateway) Registry() *Registry { return g.registry }

// BackendID 后端 ID
func (g *Gateway) BackendID() string { return g.cfg.BackendID }

// RoomCode 唯一房间码
func (g *Gateway) RoomCode() string { return g.cfg.RoomCode }

// SetReady 标记游戏监听器就绪 (health 判据, DECISIONS D-21)
func (g *Gateway) SetReady(v bool) { g.ready.Store(v) }

// Ready 游戏监听是否就绪
func (g *Gateway) Ready() bool { return g.ready.Load() }

// handshakeTimeout TLS 握手与前导读取的限期 (防半开连接占位)
const handshakeTimeout = 10 * time.Second

// AcceptGame 接管一条游戏端口的已接受连接: 信任档位检查 (来源 IP /
// TLS 客户端证书指纹) -> 前导校验 -> 登记 PENDING 会话并挂门控.
// 成功时把 ch.Conn 换成实际承载连接 (TLS 或明文)、ch.Gate 设为会话,
// 返回 true; 失败记日志、断开并返回 false.
//
// 对应契约 "Game TCP preamble" (:23-38): 前导不合法直接断开.
// 信任三档为有意修正 (作者已批准, 2026-11-04; DECISIONS D-24).
func (g *Gateway) AcceptGame(ch *core.Channel) bool {
	remote := ch.Conn.RemoteAddr().String()

	// 1. 来源 IP 约束: ip 档必查; pinned 档仅在配了白名单时查 (可空 = 不做 IP 约束)
	if g.cfg.TrustMode == TrustIP || (g.cfg.TrustMode == TrustPinned && !g.cfg.TrustedIPs.empty()) {
		if !g.cfg.TrustedIPs.contains(remoteIP(remote)) {
			log.Warnf("[Cascade]", "game connection from {0} rejected: source IP not in trustedCascadeIPs (trust={1})", remote, g.cfg.TrustMode)
			_ = ch.Conn.Close()
			return false
		}
	}

	deadline := time.Now().Add(handshakeTimeout)
	_ = ch.Conn.SetDeadline(deadline)

	// 2. 传输层: 配置了服务端证书走 TLS, 否则明文
	var conn net.Conn = ch.Conn
	if g.tlsCfg != nil {
		tlsConn := tls.Server(ch.Conn, g.tlsCfg)
		if err := tlsConn.Handshake(); err != nil {
			log.Warnf("[Cascade]", "game TLS handshake failed from {0}: {1}", remote, err)
			_ = ch.Conn.Close()
			return false
		}
		// 3. pinned 档: 客户端证书指纹必须命中 (轮换重叠期允许多条并存)
		if g.cfg.TrustMode == TrustPinned {
			state := tlsConn.ConnectionState()
			if len(state.PeerCertificates) == 0 ||
				!fingerprintMatches(state.PeerCertificates[0].Raw, g.cfg.PinnedFingerprints) {
				log.Warnf("[Cascade]", "game connection from {0} rejected: client certificate fingerprint not pinned", remote)
				_ = tlsConn.Close()
				return false
			}
		}
		conn = tlsConn
	}

	if g.cfg.TrustMode == TrustOpen {
		log.Debugf("[Cascade]", "game connection from {0} accepted in open mode (裸奔: 未校验对端身份)", remote)
	}

	// 4. 前导
	sessionID, err := ReadPreamble(conn)
	if err != nil {
		log.Warnf("[Cascade]", "game preamble rejected from {0}: {1}", remote, err)
		_ = conn.Close()
		return false
	}

	// 前导之后恢复无限期 (RW 长连接), 会话进入 PENDING 并挂绑定限期
	_ = conn.SetDeadline(time.Time{})
	sess := g.registry.Register(sessionID, func() { _ = ch.Close() })

	ch.Conn = conn
	ch.Gate = sess
	log.Debugf("[Cascade]", "session {0} accepted (PENDING) from {1}", sessionID, remote)
	return true
}

// Stop 关闭控制面监听 (服务器退出时)
func (g *Gateway) Stop() {
	g.controlMu.Lock()
	ln := g.controlLn
	g.controlLn = nil
	g.controlMu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
}
