package algorithms

import (
	"math/rand"
	"strconv"
	"strings"

	gameio "rwhps/internal/io"
	"rwhps/internal/util/mathutil"
	"rwhps/internal/util/timeutil"
)

// NetConnectProofOfWork 对应 NetConnectProofOfWork
//
// 原注释: "Proof-of-Work Verify if it is a client (Only the most versatile part)"
//
// 这是 Relay 协议握手的 PoW 质询: 服务端随机选一种校验类型, 把参数发给客户端,
// 客户端算完回传, 服务端比对. 全文件 194 行, 不依赖任何游戏状态, 可完整移植.
//
// # 类型分布
//
// 默认构造函数只会产出 0/1/3/4/5 五种 (rand.nextInt(0,6) 得 0..5, 其中 2 被映射成 5).
// 类型 2 与 6 只能来自反序列化 —— 即客户端/插件伪造的包. 因此 verifyPOWResult 的
// 2 号分支 (走 Game.connectKeyLast) 在原版的正常流程里跑不到, 但必须实现,
// 否则伪造包会被漏过.
type NetConnectProofOfWork struct {
	// resultInt 对应 resultInt: 本质是个 nonce, 生成时的 Unix 秒
	resultInt int32

	// authenticateType 对应 authenticateType: Byte
	//
	// Kotlin 是有符号 Byte, 反序列化时客户端可以传来任意字节 (含负数),
	// 因此这里用 int8 而不是 uint8, 否则 -1 会变成 255 而漏过类型判断.
	authenticateType int8

	initInt1 int32
	initInt2 int32

	outcome                     string
	fixedInitial                string
	off                         int32
	maximumNumberOfCalculations int32
}

// NewNetConnectProofOfWork 对应无参构造函数 NetConnectProofOfWork()
func NewNetConnectProofOfWork() *NetConnectProofOfWork {
	// rand.nextInt(0, 6) -> 0..5; 其中 2 被改成 5 ("No use 4/6")
	authenticateType := rand.Intn(6)
	if authenticateType == 2 {
		authenticateType = 5
	}

	p := &NetConnectProofOfWork{
		resultInt:        int32(timeutil.ConcurrentSecond()),
		authenticateType: int8(authenticateType),
	}

	// initInt_1: 类型 0, 或 2..4, 或 6
	if authenticateType == 0 || (authenticateType >= 2 && authenticateType <= 4) || authenticateType == 6 {
		p.initInt1 = int32(rand.Uint32())
	}
	// initInt_2: 类型 1, 或 2..4
	if authenticateType == 1 || (authenticateType >= 2 && authenticateType <= 4) {
		p.initInt2 = int32(rand.Uint32())
	}

	switch {
	case authenticateType >= 3 && authenticateType <= 4:
		p.outcome = powDigest(strconv.Itoa(int(p.initInt1)) + "|" + strconv.Itoa(int(p.initInt2)))
	case authenticateType >= 5 && authenticateType <= 6:
		p.fixedInitial = mathutil.GetRandomLetterString(4)
		if authenticateType == 6 {
			p.fixedInitial += strconv.Itoa(int(p.initInt1))
		}
		p.off = int32(rand.Intn(10))
		p.maximumNumberOfCalculations = int32(rand.Intn(10000000))
		p.outcome = powDigest(p.fixedInitial + "" + strconv.Itoa(int(p.off)))
	}

	return p
}

// powDigest 对应 BigInteger(1, DigestUtils.sha256(s)).toString(16).uppercase()
// 再经 StringFilteringUtil.cutting(_, 14)
//
// cutting 按字符数截断; 这里的结果全是 ASCII 十六进制, 按字节截断等价.
func powDigest(s string) string {
	return cutting(BigIntegerHexUpper(SHA256([]byte(s))), 14)
}

// BigIntegerHexUpper 对应 BigInteger(1, bytes).toString(16).uppercase()
func BigIntegerHexUpper(data []byte) string {
	return strings.ToUpper(BigIntegerHex(data))
}

// cutting 对应 StringFilteringUtil.cutting(str, length)
func cutting(s string, length int) string {
	if len(s) < length {
		return s
	}
	return s[:length]
}

// ResultInt 对应 resultInt (get)
func (p *NetConnectProofOfWork) ResultInt() int32 { return p.resultInt }

// AuthenticateType 对应 authenticateType.toInt()
//
// 返回 int 而不是 int8: 原版所有使用点都立刻 .toInt() 或与 IntRange 比较.
func (p *NetConnectProofOfWork) AuthenticateType() int { return int(p.authenticateType) }

// InitInt1 对应 initInt_1
func (p *NetConnectProofOfWork) InitInt1() int32 { return p.initInt1 }

// InitInt2 对应 initInt_2
func (p *NetConnectProofOfWork) InitInt2() int32 { return p.initInt2 }

// Outcome 对应 outcome
func (p *NetConnectProofOfWork) Outcome() string { return p.outcome }

// FixedInitial 对应 fixedInitial
func (p *NetConnectProofOfWork) FixedInitial() string { return p.fixedInitial }

// Off 对应 off
func (p *NetConnectProofOfWork) Off() int32 { return p.off }

// MaximumNumberOfCalculations 对应 maximumNumberOfCalculations
func (p *NetConnectProofOfWork) MaximumNumberOfCalculations() int32 {
	return p.maximumNumberOfCalculations
}

// VerifyPOWResult 对应 verifyPOWResult(resultInt, authenticateType, offIn)
//
// 与原版的一处差异: 5/6 号分支原版是 `this.off == offIn.toInt()`, 而
// String.toInt() 在非数字入参上抛 NumberFormatException. 原版的调用方
// (GameVersionRelay.receiveVerifyClientValidity) 用 try/catch 把它吞掉返回 false,
// 所以这里直接把解析失败当作 false —— 可观察行为一致.
func (p *NetConnectProofOfWork) VerifyPOWResult(resultInt int32, authenticateType int, offIn string) bool {
	if p.resultInt != resultInt || int(p.authenticateType) != authenticateType {
		return false
	}

	switch authenticateType {
	case 0:
		return strconv.Itoa(int(p.initInt1)) == offIn
	case 1:
		return strconv.Itoa(int(p.initInt2)) == offIn
	case 2:
		return ConnectKeyLast(p.initInt1) == offIn
	case 3, 4:
		return p.outcome == offIn
	case 5, 6:
		v, err := strconv.ParseInt(offIn, 10, 32)
		if err != nil {
			return false
		}
		return p.off == int32(v)
	}
	return true
}

// WriteTo 对应 companion object 里 serializer 的 write
//
// 注: 原版的 SerializerTypeAll.TypeSerializer 在 Server-Core 主源码里没有任何
// 引用点 (grep 过), 推测是给插件 API 用的. 照搬, 但记为未使用.
func (p *NetConnectProofOfWork) WriteTo(o *gameio.GameOutputStream) {
	o.WriteByte(byte(p.authenticateType))
	switch int(p.authenticateType) {
	case 0:
		o.WriteInt(p.initInt1)
	case 1:
		o.WriteInt(p.initInt2)
	case 3, 4:
		o.WriteInt(p.initInt1)
		o.WriteInt(p.initInt2)
		o.WriteString(p.outcome)
	case 5, 6:
		o.WriteString(p.fixedInitial)
		o.WriteInt(p.off)
		o.WriteInt(p.maximumNumberOfCalculations)
		o.WriteString(p.outcome)
	}
}

// ReadNetConnectProofOfWork 对应 companion object 里 serializer 的 read
//
// 两处与原版一致但反直觉的地方, 刻意保留:
//
//   - 类型字节无法识别时, 原版返回一个新的随机 NetConnectProofOfWork(),
//     而不是报错. 客户端可以利用这点让服务端白算一次 SHA-256, 但这是原版行为.
//   - Kotlin 的属性初始化器在每个构造函数里都会跑, 所以反序列化出来的实例
//     resultInt 是"当前秒"而不是流里的值. 于是它几乎不可能通过校验
//     (除非同一秒内生成). 原版如此, 照搬.
func ReadNetConnectProofOfWork(in *gameio.GameInputStream) *NetConnectProofOfWork {
	authenticateType := int8(in.ReadByte())

	switch int(authenticateType) {
	case 0:
		return newPowFromInts(authenticateType, in.ReadInt(), 0, "")
	case 1:
		return newPowFromInts(authenticateType, 0, in.ReadInt(), "")
	case 3, 4:
		return newPowFromInts(authenticateType, in.ReadInt(), in.ReadInt(), in.ReadString())
	case 5, 6:
		return newPowFromFixed(authenticateType, in.ReadString(), in.ReadInt(), in.ReadInt(), in.ReadString())
	default:
		return NewNetConnectProofOfWork()
	}
}

// newPowFromInts 对应 Kotlin 的 Type 0/1 与 Type 3/4 私有构造函数
func newPowFromInts(authenticateType int8, initInt1, initInt2 int32, outcome string) *NetConnectProofOfWork {
	return &NetConnectProofOfWork{
		resultInt:        int32(timeutil.ConcurrentSecond()),
		authenticateType: authenticateType,
		initInt1:         initInt1,
		initInt2:         initInt2,
		outcome:          outcome,
	}
}

// newPowFromFixed 对应 Kotlin 的 Type 5/6 私有构造函数
func newPowFromFixed(authenticateType int8, fixedInitial string, off, maximumNumberOfCalculations int32, outcome string) *NetConnectProofOfWork {
	return &NetConnectProofOfWork{
		resultInt:                   int32(timeutil.ConcurrentSecond()),
		authenticateType:            authenticateType,
		outcome:                     outcome,
		fixedInitial:                fixedInitial,
		off:                         off,
		maximumNumberOfCalculations: maximumNumberOfCalculations,
	}
}
