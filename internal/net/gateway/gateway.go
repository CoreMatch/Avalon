package gateway

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"rwhps/internal/net/core"
	"rwhps/internal/util/log"
)

// Config 是网关装配所需的已解析配置 (由启动链从 bean.CoreConfig 构造).
//
// 有意修正（作者已批准，2026-11-04）: Cascade 后端契约的传输与信任部分
// (契约 "Transport and trust" :8-21): 控制面与游戏面都是 mTLS, 身份走
// Ed25519 JWT. 临时只支持单套证书与单把代理公钥 (DECISIONS D-17).
type Config struct {
	// BackendID 后端 ID (JWT aud / health rooms 的 backendId)
	BackendID string
	// RoomCode 对 Cascade 暴露的唯一房间码 (已含空值回落)
	RoomCode string

	// TLSCert / TLSKey 服务端证书/私钥 (PEM), 游戏口与控制面共用
	TLSCert string
	TLSKey string
	// ClientCA 校验 Cascade 代理客户端证书的 CA (PEM)
	ClientCA string

	// ControlAddr /v1 控制面监听地址
	ControlAddr string

	// ProxyIssuer / ProxyKeyID / ProxyPublicKeyPath JWT 校验材料
	ProxyIssuer         string
	ProxyKeyID          string
	ProxyPublicKeyPath string

	// PendingTimeout PENDING 绑定限期 (契约 :58, 默认 5 秒)
	PendingTimeout time.Duration
}

// Gateway 是 Cascade 后端契约在 Go 侧的装配体:
// 游戏口 mTLS+前导+会话门控, 控制面 /v1, jti 账本与令牌校验.
type Gateway struct {
	cfg      Config
	registry *Registry
	jti      *JtiStore
	verifier *TokenVerifier
	tlsCfg   *tls.Config

	ready atomic.Bool

	// controlLn 控制面监听器, Stop 时关闭
	controlMu sync.Mutex
	controlLn net.Listener
}

// New 装配网关: 载入证书/公钥, 建会话注册表与 jti 账本.
// 任一材料缺失或非法都直接报错 —— 启用网关是显式配置, 不得静默降级.
func New(cfg Config, jti *JtiStore) (*Gateway, error) {
	if cfg.BackendID == "" || cfg.RoomCode == "" {
		return nil, errors.New("gateway: backend ID and room code are required")
	}
	if cfg.ControlAddr == "" {
		return nil, errors.New("gateway: control listen address is required")
	}

	tlsCfg, err := loadServerTLS(cfg.TLSCert, cfg.TLSKey, cfg.ClientCA)
	if err != nil {
		return nil, err
	}
	verifier, err := LoadTokenVerifier(cfg.ProxyIssuer, cfg.ProxyKeyID, cfg.BackendID, cfg.ProxyPublicKeyPath)
	if err != nil {
		return nil, err
	}
	if jti == nil {
		return nil, errors.New("gateway: jti store is required")
	}
	return &Gateway{
		cfg:      cfg,
		registry: NewRegistry(cfg.PendingTimeout),
		jti:      jti,
		verifier: verifier,
		tlsCfg:   tlsCfg,
	}, nil
}

// newTLSListener 把明文监听器包成 mTLS 监听器 (控制面用; 游戏面在
// AcceptGame 里逐连接握手, 因为还要读前导)
func newTLSListener(ln net.Listener, cfg *tls.Config) net.Listener {
	return tls.NewListener(ln, cfg)
}

// loadServerTLS 构造 mTLS 配置: 出示服务端证书, 并要求并校验代理客户端证书
// (契约 :10-14 双向 mTLS).
func loadServerTLS(certPath, keyPath, caPath string) (*tls.Config, error) {
	if certPath == "" || keyPath == "" || caPath == "" {
		return nil, errors.New("gateway: TLS cert, key and client CA are required")
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("gateway: load server certificate: %w", err)
	}
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("gateway: read client CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("gateway: client CA file contains no valid certificates")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
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

// AcceptGame 接管一条游戏端口的已接受连接: mTLS 握手 -> 前导校验 ->
// 登记 PENDING 会话并挂门控. 成功时把 ch.Conn 换成 TLS 连接、ch.Gate
// 设为会话, 返回 true; 失败记日志、断开并返回 false.
//
// 对应契约 "Game TCP preamble" (:23-38): 前导不合法直接断开.
func (g *Gateway) AcceptGame(ch *core.Channel) bool {
	deadline := time.Now().Add(handshakeTimeout)
	_ = ch.Conn.SetDeadline(deadline)

	tlsConn := tls.Server(ch.Conn, g.tlsCfg)
	if err := tlsConn.Handshake(); err != nil {
		log.Warnf("[Cascade]", "game TLS handshake failed: {0}", err)
		_ = ch.Conn.Close()
		return false
	}

	sessionID, err := ReadPreamble(tlsConn)
	if err != nil {
		log.Warnf("[Cascade]", "game preamble rejected: {0}", err)
		_ = tlsConn.Close()
		return false
	}

	// 前导之后恢复无限期 (RW 长连接), 会话进入 PENDING 并挂绑定限期
	_ = tlsConn.SetDeadline(time.Time{})
	sess := g.registry.Register(sessionID, func() { _ = ch.Close() })

	ch.Conn = tlsConn
	ch.Gate = sess
	log.Debugf("[Cascade]", "session {0} accepted (PENDING)", sessionID)
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
