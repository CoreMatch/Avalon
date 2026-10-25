package algorithms

import (
	"strconv"
	"strings"
	"testing"

	gameio "rwhps/internal/io"
)

// TestPowDigestKnownAnswer SHA-256 已知测试向量.
//
// SHA-256("abc") = ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad
// 经 BigInteger(1,·).toString(16).uppercase() 再 cutting(_, 14) 得 "BA7816BF8F01CF".
func TestPowDigestKnownAnswer(t *testing.T) {
	if got := powDigest("abc"); got != "BA7816BF8F01CF" {
		t.Errorf("powDigest(%q) = %q, want %q", "abc", got, "BA7816BF8F01CF")
	}
	if got := powDigest(""); got != cutting(BigIntegerHexUpper(SHA256(nil)), 14) {
		t.Errorf("powDigest(\"\") 与直接计算不一致: %q", got)
	}
}

// TestCutting 对应 StringFilteringUtil.cutting
func TestCutting(t *testing.T) {
	cases := []struct {
		s      string
		length int
		want   string
	}{
		{"abc", 5, "abc"},
		{"abc", 3, "abc"},
		{"abcde", 3, "abc"},
		{"", 3, ""},
	}
	for _, c := range cases {
		if got := cutting(c.s, c.length); got != c.want {
			t.Errorf("cutting(%q, %d) = %q, want %q", c.s, c.length, got, c.want)
		}
	}
}

// TestNewProofOfWorkInvariants 默认构造函数的结构性不变量
func TestNewProofOfWorkInvariants(t *testing.T) {
	const iterations = 3000

	seen := map[int]int{}
	for i := 0; i < iterations; i++ {
		p := NewNetConnectProofOfWork()
		typ := p.AuthenticateType()

		// 原版: rand.nextInt(0,6) 得 0..5, 其中 2 映射为 5 -> 只会出现 0/1/3/4/5
		switch typ {
		case 0, 1, 3, 4, 5:
		default:
			t.Fatalf("默认构造函数产出了类型 %d, 期望只在 0/1/3/4/5 中", typ)
		}
		seen[typ]++

		// 五个类型都该出现过 (3000 次下概率极高)
		if p.ResultInt() == 0 {
			t.Error("resultInt 不应为 0")
		}

		// 每种类型的参数填充规则
		switch typ {
		case 0:
			if p.InitInt1() == 0 && p.InitInt2() != 0 {
				t.Error("类型 0 应填 initInt1, 不应填 initInt2")
			}
		case 1:
			if p.InitInt2() == 0 && p.InitInt1() != 0 {
				t.Error("类型 1 应填 initInt2, 不应填 initInt1")
			}
		case 3, 4:
			if p.Outcome() == "" {
				t.Error("类型 3/4 的 outcome 不应为空")
			}
			if p.FixedInitial() != "" || p.Off() != 0 || p.MaximumNumberOfCalculations() != 0 {
				t.Error("类型 3/4 不应填充 fixedInitial/off/max")
			}
		case 5:
			if len(p.FixedInitial()) != 4 {
				t.Errorf("类型 5 的 fixedInitial 长度 = %d, 期望 4", len(p.FixedInitial()))
			}
			if p.Off() < 0 || p.Off() > 9 {
				t.Errorf("类型 5 的 off = %d, 期望 0..9", p.Off())
			}
			if p.MaximumNumberOfCalculations() < 0 || p.MaximumNumberOfCalculations() > 9999999 {
				t.Errorf("类型 5 的 max = %d, 期望 0..9999999", p.MaximumNumberOfCalculations())
			}
			if p.InitInt1() != 0 || p.InitInt2() != 0 {
				t.Error("类型 5 不应填充 initInt1/initInt2")
			}
		}

		// outcome 一律是 14 位大写十六进制 (cutting 的结果)
		if typ == 3 || typ == 4 || typ == 5 {
			o := p.Outcome()
			if len(o) > 14 {
				t.Errorf("outcome 长度 %d 超过 14", len(o))
			}
			if o != strings.ToUpper(o) {
				t.Errorf("outcome %q 应为大写", o)
			}
			for _, r := range o {
				if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'F')) {
					t.Errorf("outcome %q 含非十六进制字符 %q", o, r)
				}
			}
		}

		// 每种生成的 PoW 都必须能用自己的正确答案通过校验
		if !p.VerifyPOWResult(p.ResultInt(), typ, p.correctAnswer()) {
			t.Fatalf("类型 %d 的自校验失败", typ)
		}
	}

	for _, typ := range []int{0, 1, 3, 4, 5} {
		if seen[typ] == 0 {
			t.Errorf("3000 次里没出现过类型 %d", typ)
		}
	}
}

// correctAnswer 返回该 PoW 类型下客户端应回传的答案
func (p *NetConnectProofOfWork) correctAnswer() string {
	switch p.AuthenticateType() {
	case 0:
		return strconv.Itoa(int(p.initInt1))
	case 1:
		return strconv.Itoa(int(p.initInt2))
	case 3, 4:
		return p.outcome
	case 5, 6:
		return strconv.Itoa(int(p.off))
	}
	return ""
}

// TestVerifyPOWResultRejects 各种应当被拒绝的情况
func TestVerifyPOWResultRejects(t *testing.T) {
	base := func() *NetConnectProofOfWork {
		return &NetConnectProofOfWork{
			resultInt:        12345,
			authenticateType: 0,
			initInt1:         777,
		}
	}

	p := base()
	if p.VerifyPOWResult(999, 0, "777") {
		t.Error("resultInt 不匹配应拒绝")
	}
	if p.VerifyPOWResult(12345, 1, "777") {
		t.Error("authenticateType 不匹配应拒绝")
	}
	if p.VerifyPOWResult(12345, 0, "778") {
		t.Error("答案错误应拒绝")
	}
	if p.VerifyPOWResult(12345, 0, "") {
		t.Error("空答案应拒绝")
	}
	// 正确的那次要能过
	if !p.VerifyPOWResult(12345, 0, "777") {
		t.Error("正确答案应通过")
	}
}

// TestVerifyPOWResultType2UsesConnectKeyLast 类型 2 走 Game.connectKeyLast
func TestVerifyPOWResultType2UsesConnectKeyLast(t *testing.T) {
	p := &NetConnectProofOfWork{
		resultInt:        42,
		authenticateType: 2,
		initInt1:         100,
	}

	want := ConnectKeyLast(100)
	if !p.VerifyPOWResult(42, 2, want) {
		t.Errorf("类型 2 用 connectKeyLast 的正确答案 %q 应通过", want)
	}
	if p.VerifyPOWResult(42, 2, "100") {
		t.Error("类型 2 不应接受 initInt1 的十进制形式")
	}
}

// TestVerifyPOWResultNonNumericOff 5/6 号分支的 offIn 非数字时返回 false 而不是 panic
//
// 原版这里 offIn.toInt() 抛 NumberFormatException, 由调用方的 try/catch 吞掉.
func TestVerifyPOWResultNonNumericOff(t *testing.T) {
	p := &NetConnectProofOfWork{
		resultInt:        7,
		authenticateType: 5,
		off:              3,
	}

	if !p.VerifyPOWResult(7, 5, "3") {
		t.Error("正确的 off 应通过")
	}
	for _, bad := range []string{"", "abc", "3.0", " 3", "3 ", "+3x", "99999999999999999999", "-"} {
		if p.VerifyPOWResult(7, 5, bad) {
			t.Errorf("offIn = %q 应被拒绝", bad)
		}
	}
	// 带符号的数字是合法的 (Kotlin toInt 与 strconv.ParseInt 都接受)
	if !p.VerifyPOWResult(7, 5, "+3") {
		t.Error("offIn = \"+3\" 应被接受")
	}
}

// TestSerializeRoundTrip WriteTo / ReadNetConnectProofOfWork 往返
func TestSerializeRoundTrip(t *testing.T) {
	cases := []*NetConnectProofOfWork{
		{authenticateType: 0, initInt1: 111},
		{authenticateType: 1, initInt2: 222},
		{authenticateType: 3, initInt1: 1, initInt2: 2, outcome: "ABCDEF0123456789"},
		{authenticateType: 4, initInt1: 3, initInt2: 4, outcome: "0123456789ABCDEF"},
		{authenticateType: 5, fixedInitial: "aBcD", off: 7, maximumNumberOfCalculations: 12345, outcome: "FEEDFACECAFEBEE"},
		// 类型 6 注意: 原版的 write 分支不写 initInt_1, read 的私有构造函数也把它
		// 置 0, 所以往返后 initInt1 必然丢失 (信息只残留在 fixedInitial 的尾部).
		// 这是原版行为, 见 TestSerializeType6LosesInitInt1.
		{authenticateType: 6, fixedInitial: "wXyZ9", off: 0, maximumNumberOfCalculations: 1, outcome: "DEADBEEFDEADBEE"},
	}

	for _, want := range cases {
		o := gameio.NewGameOutputStream()
		want.WriteTo(o)

		got := ReadNetConnectProofOfWork(gameio.NewGameInputStreamFromBytes(o.GetByteArray(), 0))

		if got.authenticateType != want.authenticateType {
			t.Errorf("类型 %d: authenticateType = %d, want %d", want.authenticateType, got.authenticateType, want.authenticateType)
		}
		if want.authenticateType != 6 && (got.initInt1 != want.initInt1 || got.initInt2 != want.initInt2) {
			t.Errorf("类型 %d: initInt = (%d, %d), want (%d, %d)",
				want.authenticateType, got.initInt1, got.initInt2, want.initInt1, want.initInt2)
		}
		if got.outcome != want.outcome {
			t.Errorf("类型 %d: outcome = %q, want %q", want.authenticateType, got.outcome, want.outcome)
		}
		if got.fixedInitial != want.fixedInitial || got.off != want.off || got.maximumNumberOfCalculations != want.maximumNumberOfCalculations {
			t.Errorf("类型 %d: fixed/off/max = (%q, %d, %d), want (%q, %d, %d)",
				want.authenticateType, got.fixedInitial, got.off, got.maximumNumberOfCalculations,
				want.fixedInitial, want.off, want.maximumNumberOfCalculations)
		}
	}
}

// TestSerializeType6LosesInitInt1 类型 6 的序列化不携带 initInt_1.
//
// 原版 write 的 5/6 分支只写 fixedInitial/off/max/outcome 四项, read 的私有
// 构造函数也把 initInt_1 置 0. 于是往返后 initInt1 归零 —— 信息只残留在
// fixedInitial 的尾部 (类型 6 生成时把 initInt_1 拼在了后面).
func TestSerializeType6LosesInitInt1(t *testing.T) {
	want := &NetConnectProofOfWork{authenticateType: 6, initInt1: 9, fixedInitial: "wXyZ9", off: 0, outcome: "X"}

	o := gameio.NewGameOutputStream()
	want.WriteTo(o)
	got := ReadNetConnectProofOfWork(gameio.NewGameInputStreamFromBytes(o.GetByteArray(), 0))

	if got.initInt1 != 0 {
		t.Errorf("往返后 initInt1 = %d, 原版行为应为 0", got.initInt1)
	}
	if got.fixedInitial != "wXyZ9" {
		t.Errorf("fixedInitial = %q, want %q", got.fixedInitial, "wXyZ9")
	}
}

// TestSerializeUnknownTypeFallsBackToRandom 类型字节无法识别时原版返回一个新的随机实例
func TestSerializeUnknownTypeFallsBackToRandom(t *testing.T) {
	o := gameio.NewGameOutputStream()
	o.WriteByte(99) // 无法识别的类型

	got := ReadNetConnectProofOfWork(gameio.NewGameInputStreamFromBytes(o.GetByteArray(), 0))
	if got == nil {
		t.Fatal("反序列化返回了 nil")
	}
	switch got.AuthenticateType() {
	case 0, 1, 3, 4, 5:
	default:
		t.Errorf("回落实例的类型 = %d, 期望是默认构造函数可能产出的 0/1/3/4/5", got.AuthenticateType())
	}
}

// TestAuthenticateTypeIsSigned 类型字节按有符号解释.
//
// Kotlin 的 Byte 是有符号的, 客户端可以传来 0xFF (= -1). 若用 uint8 存就会变成
// 255 —— 虽然同样落进 when 的 else 分支, 但 AuthenticateType() 的返回值不同,
// 调用方拿到的类型号对不上.
//
// 注意不能通过 ReadNetConnectProofOfWork 观察: 无法识别的类型会走 default 分支
// 返回一个新的随机实例, 把负号吞掉. 所以直接测存取两端.
func TestAuthenticateTypeIsSigned(t *testing.T) {
	p := newPowFromInts(-1, 5, 0, "")
	if p.AuthenticateType() >= 0 {
		t.Errorf("AuthenticateType = %d, 期望为负", p.AuthenticateType())
	}

	// 写出去必须还是那一个字节 0xFF.
	// 注意只写 1 字节: -1 不匹配 when 的任何分支, 与原版 else -> {} 一致.
	o := gameio.NewGameOutputStream()
	p.WriteTo(o)
	b := o.GetByteArray()
	if len(b) != 1 || b[0] != 0xFF {
		t.Errorf("WriteTo 输出 % x, 期望仅首字节 0xFF", b)
	}
}
