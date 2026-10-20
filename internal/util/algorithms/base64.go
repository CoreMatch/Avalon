// Package algorithms 对应 net.rwhps.server.util.algorithms.Base64
package algorithms

import "encoding/base64"

// Base64Decode 解码为字节
func Base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// Base64DecodeString 解码为字符串 (UTF-8)
func Base64DecodeString(s string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Base64Encode 编码字节
func Base64Encode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// Base64EncodeString 编码字符串
func Base64EncodeString(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// Base64IsBase64 对应 Base64.isBase64
func Base64IsBase64(v string) bool {
	key, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return false
	}
	result := base64.StdEncoding.EncodeToString(key)
	return equalIgnoreCase(result, v)
}

func equalIgnoreCase(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
