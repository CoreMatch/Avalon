package gateway

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 身份令牌处理 (有意修正, 作者已批准, 2026-11-04; DECISIONS D-24).
//
// 契约原文 "Identity JWT" (Cascade-Backend-v1.md :61-84) 要求 Ed25519 验签 +
// kid 精确匹配; 作者定稿的信任三档将其改为: **三档都不验签**, 令牌只做
// 结构化声明校验 (形状/aud/sid/sub/jti/iat/exp), 对端身份由连接层信任承担
// (open=无, ip=来源白名单, pinned=来源白名单+客户端证书指纹).
// 契约升版前以本实现为准 (见 DECISIONS D-24 与 README).
//
// jti 一次性消费不受影响: 账本与签名无关, 重叠/裸奔下令牌重放仍被拦.
//
// 声明字段与 Cascade internal/proxy/token.go 的 Sign 逐字段对齐.

// 令牌错误: 由控制面映射成契约错误码 (如 SESSION_IDENTITY_INVALID)
var (
	// ErrTokenMalformed 结构/编码不合法
	ErrTokenMalformed = errors.New("gateway: malformed token")
	// ErrTokenClaims 声明缺失或取值违反契约
	ErrTokenClaims = errors.New("gateway: invalid claims")
	// ErrTokenExpired 已过期
	ErrTokenExpired = errors.New("gateway: token expired")
)

// jwtHeader compact JWS 受保护头 (契约 :64-68). 信任三档下只校验其为合法
// JSON 且 alg=EdDSA (Cascade 恒定签 EdDSA; 不是则视为形状错误), 不再用 kid.
type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

// TokenClaims 身份令牌声明 (契约 :70-81)
type TokenClaims struct {
	// Iss 代理发行方 (信任三档下不再校验, 字段保留用于日志与诊断)
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

// TokenVerifier 对代理令牌做结构化声明校验 (不验签, DECISIONS D-24)
type TokenVerifier struct {
	backendID string

	// maxLifetime exp-iat 的契约上限 (30 秒)
	maxLifetime int64
	// clockSkew 跨机时钟漂移宽限 (秒), 只放宽 iat 的未来方向与 exp 的过去方向
	clockSkew int64
}

// NewTokenVerifier 构造校验器; backendID 用于校验 aud (契约 :83-84
// 后端必须校验 audience).
func NewTokenVerifier(backendID string) *TokenVerifier {
	return &TokenVerifier{
		backendID:   backendID,
		maxLifetime: 30,
		clockSkew:   5,
	}
}

// Verify 解析并结构化校验一条 compact JWS, 返回声明.
// **不校验签名**: 对端身份由连接层信任档位承担 (DECISIONS D-24).
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
	if _, err := base64.RawURLEncoding.DecodeString(parts[2]); err != nil {
		return nil, ErrTokenMalformed
	}

	var header jwtHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, ErrTokenMalformed
	}
	if header.Alg != "EdDSA" {
		return nil, fmt.Errorf("%w: alg %q", ErrTokenMalformed, header.Alg)
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

// checkClaims 契约 :70-84 的逐条校验 (信任三档下去掉签名/iss/kid,
// 其余照搬; DECISIONS D-24)
func (v *TokenVerifier) checkClaims(c *TokenClaims, now time.Time) error {
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
		return fmt.Errorf("%w: iat %d in the future", ErrTokenClaims, c.Iat)
	}
	if c.Exp+v.clockSkew < nowUnix {
		return ErrTokenExpired
	}
	return nil
}
