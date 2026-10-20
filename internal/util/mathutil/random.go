// Package mathutil 对应 net.rwhps.server.util.math.RandomUtils
package mathutil

import (
	"math/rand"
	"time"
)

var defaultRanges = []string{"abcdefghijklmnopqrstuvwxyz", "ABCDEFGHIJKLMNOPQRSTUVWXYZ", "0123456789"}
var letterRanges = []string{"abcdefghijklmnopqrstuvwxyz", "ABCDEFGHIJKLMNOPQRSTUVWXYZ"}
var intCharRanges = []string{"0123456789"}

var rnd = rand.New(rand.NewSource(time.Now().UnixNano()))

// GetRandomLetterString 只包含大小写字母的随机串
func GetRandomLetterString(length int) string {
	return getRandomString(length, letterRanges)
}

// GetRandomString 大小写字母+数字的随机串
func GetRandomString(length int) string {
	return getRandomString(length, defaultRanges)
}

// GetRandomIntString 只包含数字的随机串
func GetRandomIntString(length int) string {
	return getRandomString(length, intCharRanges)
}

func getRandomString(length int, ranges []string) string {
	out := make([]byte, length)
	for i := 0; i < length; i++ {
		r := ranges[rnd.Intn(len(ranges))]
		out[i] = r[rnd.Intn(len(r))]
	}
	return string(out)
}

// GetRandomUnsignedInt 随机正整数
func GetRandomUnsignedInt() int {
	v := rnd.Int31()
	if v < 0 {
		v = -v
	}
	return int(v)
}
