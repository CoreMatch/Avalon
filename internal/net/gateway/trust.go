package gateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// 信任三档 (有意修正, 作者已批准, 2026-11-04; DECISIONS D-24).
//
// 作者定稿: "默认就是裸奔". 对端 (Cascade 代理) 身份不再靠 mTLS 强校验与
// JWT 验签, 而是三档可选约束:
//
//	TrustOpen   接受任何来源; 不校验客户端证书, 不校验 JWT 签名;
//	            仅记录来源 IP (启动告警一次 + 每连接 debug).
//	TrustIP     只接受白名单内的来源; 仍不校验 JWT 签名.
//	TrustPinned 在 IP 约束之上校验 TLS 客户端证书指纹
//	            (SHA-256(证书 DER)), 拒绝不匹配的连接.
//
// 传输与信任分离: 配置了服务端证书/密钥时传输走 TLS (open/ip 不索取客户端
// 证书, pinned 索取), 未配置即明文. JWT 三档都不验签, 只做结构化声明校验
// (aud/sid/sub/jti/exp), 见 jwt.go.
type TrustMode int

const (
	TrustOpen TrustMode = iota
	TrustIP
	TrustPinned
)

// String 返回档位名 (日志与错误信息用)
func (m TrustMode) String() string {
	switch m {
	case TrustIP:
		return "ip"
	case TrustPinned:
		return "pinned"
	default:
		return "open"
	}
}

// ParseTrustMode 解析配置里的档位文本; "" 与 "open" 同义 (默认裸奔).
func ParseTrustMode(s string) (TrustMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "open":
		return TrustOpen, nil
	case "ip":
		return TrustIP, nil
	case "pinned":
		return TrustPinned, nil
	default:
		return TrustOpen, fmt.Errorf("gateway: unknown cascadeTrustMode %q (want open/ip/pinned)", s)
	}
}

// ipAllowList 来源 IP 白名单 (精确 IP 与 CIDR 混合)
type ipAllowList struct {
	exact []net.IP
	cidrs []*net.IPNet
}

// ParseIPAllowList 解析白名单条目: 每项是精确 IP 或 CIDR;
// 任一非法即报错 (配置错误必须显式, 不得静默放行或静默锁死).
func ParseIPAllowList(entries []string) (*ipAllowList, error) {
	l := &ipAllowList{}
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if strings.Contains(e, "/") {
			_, netw, err := net.ParseCIDR(e)
			if err != nil {
				return nil, fmt.Errorf("gateway: bad trustedCascadeIPs CIDR %q: %w", e, err)
			}
			l.cidrs = append(l.cidrs, netw)
			continue
		}
		ip := net.ParseIP(e)
		if ip == nil {
			return nil, fmt.Errorf("gateway: bad trustedCascadeIPs entry %q (not an IP or CIDR)", e)
		}
		l.exact = append(l.exact, ip)
	}
	return l, nil
}

// contains 报告 ip 是否命中白名单
func (l *ipAllowList) contains(ip net.IP) bool {
	if l == nil || ip == nil {
		return false
	}
	for _, e := range l.exact {
		if e.Equal(ip) {
			return true
		}
	}
	for _, c := range l.cidrs {
		if c.Contains(ip) {
			return true
		}
	}
	return false
}

// empty 白名单是否没有任何条目
func (l *ipAllowList) empty() bool {
	return l == nil || (len(l.exact) == 0 && len(l.cidrs) == 0)
}

// ParseFingerprints 解析指纹列表: SHA-256(证书 DER) 十六进制 (大小写不敏感,
// 允许冒号分隔的常见写法). 空条目跳过; 非法即报错.
func ParseFingerprints(entries []string) ([][]byte, error) {
	var out [][]byte
	for _, e := range entries {
		trimmed := strings.TrimSpace(strings.ReplaceAll(e, ":", ""))
		if trimmed == "" {
			continue
		}
		raw, err := hex.DecodeString(trimmed)
		if err != nil || len(raw) != sha256.Size {
			return nil, fmt.Errorf("gateway: bad cascadePinnedFingerprints entry %q (want 64 hex chars of SHA-256)", e)
		}
		out = append(out, raw)
	}
	return out, nil
}

// fingerprintMatches 报告证书 DER 的 SHA-256 是否命中指纹集
func fingerprintMatches(certDER []byte, set [][]byte) bool {
	sum := sha256.Sum256(certDER)
	for _, fp := range set {
		if bytes.Equal(sum[:], fp) {
			return true
		}
	}
	return false
}

// CertFingerprintHex 计算证书 DER 的 SHA-256 指纹 (小写十六进制, 无分隔).
// 供测试与文档示例使用.
func CertFingerprintHex(certDER []byte) string {
	sum := sha256.Sum256(certDER)
	return hex.EncodeToString(sum[:])
}

// resolveMaterialPath 按 "cwd 优先、再可执行文件目录" 解析证书/密钥路径
// (与 Cascade 侧 2026-11-04 修复的查找策略一致):
//  1. 按原样 (绝对路径或相对 cwd) 存在即用;
//  2. 否则回落到可执行文件所在目录下的同名相对路径;
//  3. 都找不到返回显式错误 (材料缺失不得静默).
func resolveMaterialPath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("gateway: material path is empty")
	}
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	if exe, err := os.Executable(); err == nil {
		alt := filepath.Join(filepath.Dir(exe), p)
		if _, err := os.Stat(alt); err == nil {
			return alt, nil
		}
	}
	return "", fmt.Errorf("gateway: material %q not found (tried cwd, then executable directory)", p)
}

// remoteIP 从地址串 (host:port 或纯 IP) 提取 IP
func remoteIP(addr string) net.IP {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	return net.ParseIP(host)
}
