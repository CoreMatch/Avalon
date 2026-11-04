package gateway

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// 契约 "Identity JWT" (Cascade-Backend-v1.md :61-84):
// 代理以 EdDSA(Ed25519) 签 compact JWS; 后端校验签名、kid、iss、aud、sid、
// sub、iat/exp 与 jti 一次性. JWT 不携带 HA UUID/凭据/授权策略, 后端也不得
// 信任客户端自报身份 (契约 :18-19).
//
// 与 Cascade internal/proxy/token.go 的 Sign 逐字段对齐.

// 令牌错误: 由控制面映射成契约错误码 (如 SESSION_IDENTITY_INVALID)
var (
	// ErrTokenMalformed 结构/编码/签名材料不合法
	ErrTokenMalformed = errors.New("gateway: malformed token")
	// ErrTokenKid 头里 kid 与配置的期望 kid 不符
	ErrTokenKid = errors.New("gateway: unknown key ID")
	// ErrTokenSignature 签名校验失败
	ErrTokenSignature = errors.New("gateway: bad signature")
	// ErrTokenClaims 声明缺失或取值违反契约
	ErrTokenClaims = errors.New("gateway: invalid claims")
	// ErrTokenExpired 已过期
	ErrTokenExpired = errors.New("gateway: token expired")
)

// jwtHeader compact JWS 受保护头 (契约 :64-68)
type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

// TokenClaims 身份令牌声明 (契约 :70-81)
type TokenClaims struct {
	// Iss 代理发行方 (配置项 cascadeProxyIssuer)
	Iss string `json:"iss"`
	// Aud 接收该会话的后端 ID (必须等于本后端 ID)
	Aud string `json:"aud"`
	// Sid TCP 前导里的会话 ID
	Sid string `json:"sid"`
	// Sub RW RegisterPlayer 包里的游戏 UUID
	Sub string `json:"sub"`
	// Iat 签发时间 (Unix 秒)
	Iat int64 `json:"iat"`
	// Exp 过期 (签发后不超过 30 秒)
	Exp int64 `json:"exp"`
	// Jti 唯一的令牌 ID, 一次性消费
	Jti string `json:"jti"`
}

// TokenVerifier 用一把带外分发的 Ed25519 公钥校验代理令牌
//
// 契约允许 kid 轮换与多公钥重叠 (:15-17); 临时只支持单钥单 kid
// (DECISIONS D-17), 期望 kid 不匹配直接拒绝.
type TokenVerifier struct {
	issuer    string
	keyID     string
	backendID string
	publicKey ed25519.PublicKey

	// maxLifetime exp-iat 的契约上限 (30 秒)
	maxLifetime int64
	// clockSkew 跨机时钟漂移宽限 (秒), 只放宽 iat 的未来方向与 exp 的过去方向
	clockSkew int64
}

// NewTokenVerifier 构造校验器; publicKeyPEM 为 PKIX ("PUBLIC KEY") Ed25519 公钥.
// backendID 用于校验 aud (契约 :83-84 后端必须校验 audience).
func NewTokenVerifier(issuer, keyID, backendID string, publicKeyPEM []byte) (*TokenVerifier, error) {
	if issuer == "" || keyID == "" || backendID == "" {
		return nil, errors.New("gateway: token issuer, key ID and backend ID are required")
	}
	block, _ := pem.Decode(publicKeyPEM)
	if block == nil {
		return nil, errors.New("gateway: proxy public key is not PEM encoded")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("gateway: parse proxy public key: %w", err)
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("gateway: proxy public key must be Ed25519")
	}
	return &TokenVerifier{
		issuer:      issuer,
		keyID:       keyID,
		backendID:   backendID,
		publicKey:   key,
		maxLifetime: 30,
		clockSkew:   5,
	}, nil
}

// LoadTokenVerifier 从文件读公钥并构造校验器
func LoadTokenVerifier(issuer, keyID, backendID, path string) (*TokenVerifier, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("gateway: read proxy public key: %w", err)
	}
	return NewTokenVerifier(issuer, keyID, backendID, data)
}

// Verify 校验一条 compact JWS, 返回声明.
//
// sid 的"与本连接会话一致"由调用方在会话上下文里比对
// (同一令牌文本可能合法但属于别的会话).
func (v *TokenVerifier) Verify(token string, now time.Time) (*TokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrTokenMalformed
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrTokenMalformed
	}
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrTokenMalformed
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrTokenMalformed
	}

	var header jwtHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, ErrTokenMalformed
	}
	if header.Alg != "EdDSA" {
		return nil, fmt.Errorf("%w: alg %q", ErrTokenMalformed, header.Alg)
	}
	if header.Kid != v.keyID {
		return nil, fmt.Errorf("%w: %q", ErrTokenKid, header.Kid)
	}

	signingInput := parts[0] + "." + parts[1]
	if !ed25519.Verify(v.publicKey, []byte(signingInput), signature) {
		return nil, ErrTokenSignature
	}

	var claims TokenClaims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return nil, ErrTokenMalformed
	}
	if err := v.checkClaims(&claims, now); err != nil {
		return nil, err
	}
	return &claims, nil
}

// checkClaims 契约 :70-84 的逐条校验
func (v *TokenVerifier) checkClaims(c *TokenClaims, now time.Time) error {
	if c.Iss != v.issuer {
		return fmt.Errorf("%w: iss %q", ErrTokenClaims, c.Iss)
	}
	if c.Aud != v.backendID {
		return fmt.Errorf("%w: aud %q", ErrTokenClaims, c.Aud)
	}
	if c.Sid == "" || c.Sub == "" || c.Jti == "" {
		return fmt.Errorf("%w: empty sid/sub/jti", ErrTokenClaims)
	}
	if c.Exp-c.Iat > v.maxLifetime {
		return fmt.Errorf("%w: lifetime %ds exceeds %ds", ErrTokenClaims, c.Exp-c.Iat, v.maxLifetime)
	}
	nowUnix := now.Unix()
	if c.Iat > nowUnix+v.clockSkew {
		return fmt.Errorf("%w: iat in the future", ErrTokenClaims)
	}
	if c.Exp <= nowUnix-v.clockSkew {
		return ErrTokenExpired
	}
	return nil
}
