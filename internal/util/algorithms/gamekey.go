package algorithms

import (
	"math"
	"strconv"
	"strings"
)

// 本文件对应 net.rwhps.server.util.algorithms.Game
//
// 原注释: "验证客户端有效性". 这是 Server-Core 自己复现的客户端密钥推导,
// 用于 PoW 类型 2 的校验 (见 proofofwork.go).
//
// # 两个必须处理的保真点
//
// ## 1. Kotlin 的 Int 是 32 位, 溢出静默回绕
//
// Go 的 int 在 64 位平台是 64 位, 同一个表达式不会溢出. 而这里的运算结果要
// 原样拼进字符串与客户端返回值比对, 位数差一点就对不上. 因此全部用 int32 运算,
// 由 Go 的有符号回绕语义复现 Kotlin 的行为.
//
// ## 2. t1 一段是 Double 运算, 走 Java 的 Double.toString
//
// connectKeyNew_115_Test 与 connectKeyLast 里是 `paramInt * 4000.0 * 11.0`,
// 浮点结果由 Java 的 Double.toString 格式化 —— 它的规则和 Go 的
// strconv.FormatFloat 不一样 (见 javaDoubleToString 的注释).

// ConnectKey114 对应 Game.connectKey_114 (1.14)
//
// 注意最后一段 t1 是 Int 运算, 会回绕.
func ConnectKey114(paramInt int32) string {
	var b strings.Builder
	b.WriteString("c:")
	b.WriteString(strconv.Itoa(int(paramInt)))
	b.WriteString("m:")
	b.WriteString(strconv.Itoa(int(paramInt*87 + 24)))
	b.WriteString("0:")
	b.WriteString(strconv.Itoa(int(paramInt * 44000)))
	b.WriteString("1:")
	b.WriteString(strconv.Itoa(int(paramInt)))
	b.WriteString("2:")
	b.WriteString(strconv.Itoa(int(paramInt * 13000)))
	b.WriteString("3:")
	b.WriteString(strconv.Itoa(int(paramInt + 28000)))
	b.WriteString("4:")
	b.WriteString(strconv.Itoa(int(paramInt * 75000)))
	b.WriteString("5:")
	b.WriteString(strconv.Itoa(int(paramInt + 160000)))
	b.WriteString("6:")
	b.WriteString(strconv.Itoa(int(paramInt * 850000)))
	b.WriteString("t1:")
	b.WriteString(strconv.Itoa(int(paramInt * 44000)))
	b.WriteString("d:")
	b.WriteString(strconv.Itoa(int(paramInt * 5)))
	return b.String()
}

// ConnectKeyNew115Test 对应 Game.connectKeyNew_115_Test (1.15)
//
// 与 114 的差别只有 t1: 改用 Double 运算.
func ConnectKeyNew115Test(paramInt int32) string {
	var b strings.Builder
	b.WriteString("c:")
	b.WriteString(strconv.Itoa(int(paramInt)))
	b.WriteString("m:")
	b.WriteString(strconv.Itoa(int(paramInt*87 + 24)))
	b.WriteString("0:")
	b.WriteString(strconv.Itoa(int(paramInt * 44000)))
	b.WriteString("1:")
	b.WriteString(strconv.Itoa(int(paramInt)))
	b.WriteString("2:")
	b.WriteString(strconv.Itoa(int(paramInt * 13000)))
	b.WriteString("3:")
	b.WriteString(strconv.Itoa(int(paramInt + 28000)))
	b.WriteString("4:")
	b.WriteString(strconv.Itoa(int(paramInt * 75000)))
	b.WriteString("5:")
	b.WriteString(strconv.Itoa(int(paramInt + 160000)))
	b.WriteString("6:")
	b.WriteString(strconv.Itoa(int(paramInt * 850000)))
	b.WriteString("t1:")
	// 原版是 `paramInt * 4000.0 * 11.0`: 两个 double 字面量, 因此整段是 Double 运算.
	// 求值顺序必须保持左结合, 和 Kotlin 一致.
	b.WriteString(javaDoubleToString(float64(paramInt) * 4000.0 * 11.0))
	b.WriteString("d:")
	b.WriteString(strconv.Itoa(int(paramInt * 5)))
	return b.String()
}

// ConnectKeyLast 对应 Game.connectKeyLast (1.15.P10)
//
// 比 115 多出 7: 与 8: 两段.
func ConnectKeyLast(paramInt int32) string {
	var b strings.Builder
	b.WriteString("c:")
	b.WriteString(strconv.Itoa(int(paramInt)))
	b.WriteString("m:")
	b.WriteString(strconv.Itoa(int(paramInt*87 + 24)))
	b.WriteString("0:")
	b.WriteString(strconv.Itoa(int(paramInt * 44000)))
	b.WriteString("1:")
	b.WriteString(strconv.Itoa(int(paramInt)))
	b.WriteString("2:")
	b.WriteString(strconv.Itoa(int(paramInt * 13000)))
	b.WriteString("3:")
	b.WriteString(strconv.Itoa(int(paramInt + 28000)))
	b.WriteString("4:")
	b.WriteString(strconv.Itoa(int(paramInt * 75000)))
	b.WriteString("5:")
	b.WriteString(strconv.Itoa(int(paramInt + 160000)))
	b.WriteString("6:")
	b.WriteString(strconv.Itoa(int(paramInt * 850000)))
	b.WriteString("7:")
	b.WriteString(strconv.Itoa(int(paramInt * 1800000)))
	b.WriteString("8:")
	b.WriteString(strconv.Itoa(int(paramInt * 3800000)))
	b.WriteString("t1:")
	b.WriteString(javaDoubleToString(float64(paramInt) * 4000.0 * 11.0))
	b.WriteString("d:")
	b.WriteString(strconv.Itoa(int(paramInt * 5)))
	return b.String()
}

// javaDoubleToString 复现 Java Double.toString 的格式化规则.
//
// 不能直接用 strconv.FormatFloat(v, 'g', -1, 64): 两者的十进制位数选择接近
// (都要最短且能 round-trip), 但**输出格式**不同. 以 4.4e7 为例:
//
//	Java:  "4.4E7"     Go 'g': "4.4e+07"
//	Java:  "4400000.0" Go 'g': "4.4e+06"
//
// Java 的规则 (摘自 Double.toString 的 javadoc):
//
//   - 10^-3 <= |d| < 10^7 用小数形式, 且小数点后至少一位 (100.0 / 0.001)
//   - 其余用"计算机科学计数法", 大写 E, 指数不带正号也不带前导零 (1.0E10 / 1.0E-4)
//   - 小数点后同样至少一位
//   - NaN / Infinity / -Infinity / 0.0 / -0.0 有专门字样
//
// 实现: 先用 'e' -1 拿到最短 round-trip 的有效数字与十进制指数, 再按 Java 的
// 规则重新排版.
func javaDoubleToString(v float64) string {
	if math.IsNaN(v) {
		return "NaN"
	}
	if math.IsInf(v, 1) {
		return "Infinity"
	}
	if math.IsInf(v, -1) {
		return "-Infinity"
	}

	sign := ""
	if math.Signbit(v) {
		sign = "-"
		v = -v
	}
	if v == 0 {
		return sign + "0.0"
	}

	// 'e' -1 给出最短 round-trip 表示, 形如 "4.4e+07"
	s := strconv.FormatFloat(v, 'e', -1, 64)
	ei := strings.IndexByte(s, 'e')
	mantissa := s[:ei] // "4.4"
	exp, err := strconv.Atoi(s[ei+1:])
	if err != nil {
		// FormatFloat 的输出必然带合法指数, 到这里说明出了问题
		return sign + s
	}

	// digits 是去掉小数点后的有效数字, 小数点原本在第一位之后
	digits := strings.Replace(mantissa, ".", "", 1)

	// Java 的小数形式区间: 10^-3 <= v < 10^7, 即首位的十进制指数在 [-3, 7)
	if exp >= -3 && exp < 7 {
		var b strings.Builder
		b.WriteString(sign)
		switch {
		case exp >= 0:
			// 整数部分占 exp+1 位
			if len(digits) <= exp+1 {
				b.WriteString(digits)
				b.WriteString(strings.Repeat("0", exp+1-len(digits)))
				b.WriteString(".0")
			} else {
				b.WriteString(digits[:exp+1])
				b.WriteByte('.')
				b.WriteString(digits[exp+1:])
			}
		default:
			// 0.00...digits, 小数点后先补 -exp-1 个零
			b.WriteString("0.")
			b.WriteString(strings.Repeat("0", -exp-1))
			b.WriteString(digits)
		}
		return b.String()
	}

	// 科学计数法: d.dddEexp, 指数不带正号与前导零
	var b strings.Builder
	b.WriteString(sign)
	b.WriteByte(digits[0])
	b.WriteByte('.')
	if len(digits) > 1 {
		b.WriteString(digits[1:])
	} else {
		b.WriteByte('0')
	}
	b.WriteByte('E')
	b.WriteString(strconv.Itoa(exp))
	return b.String()
}
