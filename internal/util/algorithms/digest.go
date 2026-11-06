// Package algorithms 对应 net.rwhps.server.util.algorithms
package algorithms

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// SHA256 对应 DigestUtils.sha256
func SHA256(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

// SHA256Hex 对应 DigestUtils.sha256Hex: SHA-256 摘要的小写十六进制 (定长 64)
func SHA256Hex(data []byte) string {
	return hex.EncodeToString(SHA256(data))
}

// BigIntegerHex 对应 BigInteger(1, bytes).toString(16).
//
// 语义: 按无符号大整数解释, 输出小写十六进制, 去掉前导 0; 全 0 时输出 "0".
func BigIntegerHex(data []byte) string {
	i := 0
	for i < len(data) && data[i] == 0 {
		i++
	}
	if i == len(data) {
		return "0"
	}
	return strings.TrimLeft(hex.EncodeToString(data[i:]), "0")
}
