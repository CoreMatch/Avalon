// Package mathutil 对应 net.rwhps.server.util.math.RandomUtils
package mathutil

import (
	"math/rand"
)

var defaultRanges = []string{"abcdefghijklmnopqrstuvwxyz", "ABCDEFGHIJKLMNOPQRSTUVWXYZ", "0123456789"}
var letterRanges = []string{"abcdefghijklmnopqrstuvwxyz", "ABCDEFGHIJKLMNOPQRSTUVWXYZ"}
var intCharRanges = []string{"0123456789"}

// 这里刻意用 math/rand 的包级函数而不是自持一个 *rand.Rand.
//
// 包级函数走的是 globalRand, 其源是 lockedSource, 并发安全. 自持 *rand.Rand 则
// 不是: rand.Rand 的方法没有锁, 多个 goroutine 同时调用会数据竞争.
//
// 此前的实现是 `var rnd = rand.New(rand.NewSource(time.Now().UnixNano()))`,
// 当时唯一的调用方是 configs.go 的启动期初始化 (单线程), 所以没暴露.
// NetConnectProofOfWork 每建立一条连接就构造一次, 是第一个并发调用方,
// 因此必须改.

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
		r := ranges[rand.Intn(len(ranges))]
		out[i] = r[rand.Intn(len(r))]
	}
	return string(out)
}

// GetRandomUnsignedInt 随机正整数
//
// 对应 kotlin.random.Random.nextInt().absoluteValue.
// 注意: rand.Int31() 的范围是 [0, 2^31), 取绝对值后与 Kotlin 的
// nextInt().absoluteValue (范围 [0, 2^31), 但 Int.MIN_VALUE 的绝对值是它本身)
// 在极端值上有差异, 见测试.
func GetRandomUnsignedInt() int {
	v := rand.Int31()
	if v < 0 {
		v = -v
	}
	return int(v)
}
