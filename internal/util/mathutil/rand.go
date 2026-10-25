package mathutil

import (
	"math"
	"sync/atomic"
	"time"
)

// Rand 对应 net.rwhps.server.util.math.Rand
//
// 原注释: "此类实现了 xorshift128+ 算法... 该PRNG的质量比 Random 的质量高得多,
// 并且其循环长度为 2^128 - 1... RandomXS128 的实例不是线程安全的."
//
// 与 libgdx 的 RandomXS128 同源. 纯位运算, 不依赖任何外部状态, 可完整移植.
//
// # 线程安全
//
// 与原版一致: **不是线程安全的**. 只有两个 int64 字段, 没有锁.
// RelayRoom 的伴生 rand 只在 @Synchronized 的 getRelay 里被用到, 所以原版没暴露
// 这个问题. Go 侧保持同样语义 (加锁会改变性能特征, 而原版明确选择不加).
//
// 若将来要在并发路径使用, 请在外层加锁, 不要改这个类型.
type Rand struct {
	seed0 int64
	seed1 int64
}

const (
	normDouble = 1.0 / float64(int64(1)<<53)
	normFloat  = 1.0 / float64(int64(1)<<24)
)

// seedUniquifier 对应 java.util.Random 的静态 seedUniquifier
//
// 原版 Rand() 用 `Random().nextLong()` 播种, 而 java.util.Random() 的无参构造是
// `this(seedUniquifier() ^ System.nanoTime())`. 那个静态乘法链保证"即使两次调用落在
// 同一纳秒, 种子也不同"——macOS 上 time.Now().UnixNano() 的粒度只有微秒, 单独用它
// 会在同一微秒内造出两个同种子的 Rand. 所以这里把整条链照搬过来.
var seedUniquifier int64 = 8682522807148012 // java.util.Random 的初始值

func nextSeedUniquifier() int64 {
	for {
		current := atomic.LoadInt64(&seedUniquifier)
		next := current * 181783497276652981 // 0x285D320AD33FDB5 (L'Ecuyer)
		if atomic.CompareAndSwapInt64(&seedUniquifier, current, next) {
			return next
		}
	}
}

// NewRand 对应 Rand(): 用一个很可能与众不同的值作种子
//
// 原版是 java.util.Random().nextLong(); java.util.Random() 内部是
// seedUniquifier() ^ System.nanoTime(), Go 侧按同样方式组装.
func NewRand() *Rand {
	r := &Rand{}
	r.SetSeed(nextSeedUniquifier() ^ time.Now().UnixNano())
	return r
}

// NewRandSeeded 对应 Rand(seed: Long)
func NewRandSeeded(seed int64) *Rand {
	r := &Rand{}
	r.SetSeed(seed)
	return r
}

// NewRandState 对应 Rand(seed0: Long, seed1: Long): 直接设置内部状态
func NewRandState(seed0, seed1 int64) *Rand {
	return &Rand{seed0: seed0, seed1: seed1}
}

// NextLongNew 对应 nextLongNew(): xorshift128+ 的核心一步
func (r *Rand) NextLongNew() int64 {
	s1 := r.seed0
	s0 := r.seed1
	r.seed0 = s0
	s1 ^= s1 << 23
	ret := s1 ^ s0 ^ int64(uint64(s1)>>17) ^ int64(uint64(s0)>>26)
	r.seed1 = ret
	return ret + s0
}

// Next 对应 next(bits)
//
// 原注释说它在子类里是 final 的, 因为其他方法已经不再用它.
// bits 取 0..63; java.util.Random 只会用 1..32 调用它.
func (r *Rand) Next(bits int) int32 {
	if bits < 0 || bits > 63 {
		bits &= 63
	}
	return int32(r.NextLongNew() & ((int64(1) << bits) - 1))
}

// NextInt 对应 nextInt()
func (r *Rand) NextInt() int32 { return int32(r.NextLongNew()) }

// NextLong 对应继承自 java.util.Random 的 nextLong()
//
// Rand 没有重写它, 所以走的是父类实现: 两次 next(32) 拼成 64 位.
// Go 没有继承, 这里按父类算法显式写出.
func (r *Rand) NextLong() int64 {
	hi := int64(r.Next(32))
	lo := int64(r.Next(32))
	return (hi << 32) + lo
}

// NextIntBounded 对应 nextInt(n)
func (r *Rand) NextIntBounded(n int) int { return int(r.NextLongBounded(int64(n))) }

// NextLongBounded 对应 nextLongNew(n: Long)
//
// 拒绝条件 `bits - value + (n - 1) >= 0` 看着恒真, 其实不是: bits 最大接近 2^63,
// 加上 (n-1) 会**溢出**成负数, 那一轮就被拒绝重来. Go 的 int64 溢出同样是二进制
// 回绕, 行为一致.
func (r *Rand) NextLongBounded(n int64) int64 {
	if n <= 0 {
		panic("n must be positive")
	}
	for {
		bits := int64(uint64(r.NextLongNew()) >> 1)
		value := bits % n
		if bits-value+(n-1) >= 0 {
			return value
		}
	}
}

// NextDouble 对应 nextDouble()
func (r *Rand) NextDouble() float64 {
	return float64(uint64(r.NextLongNew())>>11) * normDouble
}

// NextFloat 对应 nextFloat()
func (r *Rand) NextFloat() float32 {
	return float32(float64(uint64(r.NextLongNew())>>40) * normFloat)
}

// NextBoolean 对应 nextBoolean()
func (r *Rand) NextBoolean() bool { return r.NextLongNew()&1 != 0 }

// NextBytes 对应 nextBytes(bytes)
//
// 每 8 字节一组长整型, 从数组尾部向前填 (与原版的 --i 一致).
func (r *Rand) NextBytes(bytes []byte) {
	i := len(bytes)
	for i != 0 {
		n := i
		if n > 8 {
			n = 8
		}
		bits := r.NextLongNew()
		for n != 0 {
			n--
			i--
			bytes[i] = byte(bits)
			bits >>= 8
		}
	}
}

// SetSeed 对应 setSeed(seed)
//
// 种子为 0 时原版改用 Long.MIN_VALUE, 避免全零态 (全零态下 xorshift 会永远产出 0).
func (r *Rand) SetSeed(seed int64) {
	s := seed
	if s == 0 {
		s = math.MinInt64
	}
	seed0 := murmurHash3(s)
	r.seed0 = seed0
	r.seed1 = murmurHash3(seed0)
}

// SetState 对应 setState(seed0, seed1)
func (r *Rand) SetState(seed0, seed1 int64) {
	r.seed0 = seed0
	r.seed1 = seed1
}

// GetState 对应 getState(seed): seed 为 0 或 1
func (r *Rand) GetState(seed int) int64 {
	if seed == 0 {
		return r.seed0
	}
	return r.seed1
}

// Chance 对应 chance(chance)
func (r *Rand) Chance(chance float64) bool { return r.NextDouble() < chance }

// RangeFloat 对应 range(amount: Float)
func (r *Rand) RangeFloat(amount float32) float32 {
	return r.NextFloat()*amount*2 - amount
}

// RandomFloat 对应 random(max: Float)
func (r *Rand) RandomFloat(max float32) float32 { return r.NextFloat() * max }

// RandomFloatRange 对应 random(min: Float, max: Float)
func (r *Rand) RandomFloatRange(min, max float32) float32 {
	return min + (max-min)*r.NextFloat()
}

// RangeInt 对应 range(amount: Int)
func (r *Rand) RangeInt(amount int) int { return r.NextIntBounded(amount*2+1) - amount }

// RandomInt 对应 random(min: Int, max: Int)
//
// RelayRoom.getRelay 里生成随机房间号用的就是这个: rand.random(1000, 100000).
func (r *Rand) RandomInt(min, max int) int {
	return min + r.NextIntBounded(max-min+1)
}

// murmurHash3 对应伴生对象里的 murmurHash3(xIn)
//
// 注意两个乘数都是负数常量, 直接照抄十六进制字面量:
//
//	-0xae502812aa7333     (14 位, 约 -3.02e17)
//	-0x3b314601e57a13ad   (16 位且最高位为 0, 约 -4.29e18)
//
// 两者都在 int64 范围内. 写成 int64 常量让 Go 的回绕语义与 Kotlin 的 Long 一致.
func murmurHash3(xIn int64) int64 {
	x := xIn
	x ^= int64(uint64(x) >> 33)
	x *= -0xae502812aa7333
	x ^= int64(uint64(x) >> 33)
	x *= -0x3b314601e57a13ad
	x ^= int64(uint64(x) >> 33)
	return x
}
