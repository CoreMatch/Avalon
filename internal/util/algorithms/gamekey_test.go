package algorithms

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// TestJavaDoubleToString 对照 Java Double.toString 的输出.
//
// 这些期望值全部来自 Java 的既定行为 (Double.toString 的 javadoc 示例与规则),
// 不是从 Go 的输出反推的 —— 否则测试就失去意义了.
func TestJavaDoubleToString(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		// 特殊值
		{0.0, "0.0"},
		{math.Copysign(0, -1), "-0.0"},
		{math.NaN(), "NaN"},
		{math.Inf(1), "Infinity"},
		{math.Inf(-1), "-Infinity"},

		// 小数形式: 10^-3 <= |d| < 10^7, 小数点后至少一位
		{1.0, "1.0"},
		{100.0, "100.0"},
		{0.5, "0.5"},
		{0.1, "0.1"},
		{0.001, "0.001"},
		{123456.7, "123456.7"},
		{1234567.0, "1234567.0"},
		{4400000.0, "4400000.0"},
		{9999999.0, "9999999.0"},

		// 科学计数法: 大写 E, 指数不带正号不带前导零
		{10000000.0, "1.0E7"},
		{44000000.0, "4.4E7"},
		{12345678.0, "1.2345678E7"},
		{1e10, "1.0E10"},
		{1e100, "1.0E100"},
		{0.0001, "1.0E-4"},
		{1e-10, "1.0E-10"},

		// 负数
		{-1.0, "-1.0"},
		{-100.0, "-100.0"},
		{-0.001, "-0.001"},
		{-1e10, "-1.0E10"},
		{-1e-4, "-1.0E-4"},
	}

	for _, c := range cases {
		if got := javaDoubleToString(c.in); got != c.want {
			t.Errorf("javaDoubleToString(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestJavaDoubleToStringRoundTrips 确认输出仍能精确 round-trip 回原值.
//
// Java 与 Go 都追求"最短且能 round-trip"的有效数字. 只要 ParseFloat 回来等于
// 原值, 就说明既没丢精度也没引入多余位数 —— 比逐位比对数字序列更稳.
func TestJavaDoubleToStringRoundTrips(t *testing.T) {
	values := []float64{
		1, 100, 0.5, 0.1, 0.001, 123456.7, 1234567, 4400000, 9999999,
		1e7, 4.4e7, 1.2345678e7, 1e10, 1e100, 1e-4, 1e-10,
		3.0 / 7.0, 2.0 / 3.0, math.Pi, math.E,
	}

	for _, v := range values {
		got := javaDoubleToString(v)
		back, err := strconv.ParseFloat(got, 64)
		if err != nil {
			t.Errorf("javaDoubleToString(%v) = %q 无法解析: %v", v, got, err)
			continue
		}
		if back != v {
			t.Errorf("javaDoubleToString(%v) = %q 解析回来是 %v, 不相等", v, got, back)
		}
	}
}

// TestJavaDoubleToStringIsShortest 确认没有引入比必要更多的位数.
//
// 做法: 把 Java 形式的输出还原成完整数字串, 去掉作为占位的首尾零, 与 strconv
// 的最短表示比有效数字序列. Java 的小数形式会补零 (4400000.0), 科学计数法也补
// .0, 这些零是排版用的, 不算有效数字.
func TestJavaDoubleToStringIsShortest(t *testing.T) {
	values := []float64{
		1, 100, 0.5, 0.1, 0.001, 123456.7, 1234567, 4400000, 9999999,
		1e7, 4.4e7, 1.2345678e7, 1e10, 1e100, 1e-4, 1e-10,
		3.0 / 7.0, 2.0 / 3.0, math.Pi, math.E, 1230.0, 100.5, 1.1e-5,
	}

	for _, v := range values {
		ref := strconv.FormatFloat(v, 'e', -1, 64)
		ei := strings.IndexByte(ref, 'e')
		wantDigits := strings.Trim(strings.Replace(ref[:ei], ".", "", 1), "0")

		got := javaDoubleToString(v)

		// 只取尾数部分 (E 之前), 拼成连续数字串
		mantissa := got
		if i := strings.IndexByte(got, 'E'); i >= 0 {
			mantissa = got[:i]
		}
		var digits strings.Builder
		for _, r := range mantissa {
			if r >= '0' && r <= '9' {
				digits.WriteRune(r)
			}
		}
		gotDigits := strings.Trim(digits.String(), "0")

		if gotDigits != wantDigits {
			t.Errorf("javaDoubleToString(%v) = %q, 有效数字 %q, 最短表示应为 %q",
				v, got, gotDigits, wantDigits)
		}
	}
}

// TestConnectKeyLastKnownValues 手工核算的期望值
func TestConnectKeyLastKnownValues(t *testing.T) {
	cases := []struct {
		in   int32
		want string
	}{
		// 全零: 只有 +28000 / +160000 两段非零, t1 是 0.0
		{
			in:   0,
			want: "c:0m:240:01:02:03:280004:05:1600006:07:08:0t1:0.0d:0",
		},
		{
			in:   1,
			want: "c:1m:1110:440001:12:130003:280014:750005:1600016:8500007:18000008:3800000t1:44000.0d:5",
		},
		// t1 = 100 * 4000.0 * 11.0 = 4400000.0, 小于 10^7 走小数形式
		{
			in:   100,
			want: "c:100m:87240:44000001:1002:13000003:281004:75000005:1601006:850000007:1800000008:380000000t1:4400000.0d:500",
		},
		// t1 = 44000000.0, 达到 10^7 走科学计数法
		// 注意 8: 段 1000 * 3800000 = 3.8e9 超出 int32 上限, 回绕成 -494967296
		{
			in:   1000,
			want: "c:1000m:870240:440000001:10002:130000003:290004:750000005:1610006:8500000007:18000000008:-494967296t1:4.4E7d:5000",
		},
	}

	for _, c := range cases {
		if got := ConnectKeyLast(c.in); got != c.want {
			t.Errorf("ConnectKeyLast(%d) =\n %q\nwant\n %q", c.in, got, c.want)
		}
	}
}

// TestConnectKey114And115UseIntForT1 114 的 t1 是 Int 运算 (会回绕),
// 115 与 Last 的 t1 是 Double 运算 (不回绕).
func TestConnectKey114And115UseIntForT1(t *testing.T) {
	// paramInt * 44000 在 int32 下: 100000 * 44000 = 4.4e9, 超出 int32 上限
	var v int32 = 100000

	got114 := ConnectKey114(v)
	// 用另一条算式独立算一遍: int64 相乘后截断, 与 int32 相乘回绕应当一致
	want114T1 := strconv.Itoa(int(int32(int64(v) * 44000)))
	if !strings.Contains(got114, "t1:"+want114T1+"d:") {
		t.Errorf("ConnectKey114(%d) 的 t1 段不是 int32 回绕值 %q\n完整: %q", v, want114T1, got114)
	}

	// 115 的同一段必须是 Double: 4.4e9 >= 10^7, 科学计数法
	got115 := ConnectKeyNew115Test(v)
	if !strings.Contains(got115, "t1:4.4E9d:") {
		t.Errorf("ConnectKeyNew115Test(%d) 的 t1 段应走 Double 科学计数法\n完整: %q", v, got115)
	}
}

// TestConnectKeyWrapsAt32Bit 证明用的是 32 位回绕而不是 64 位.
//
// 1000000 * 850000 = 8.5e11, 远超 int32 上限; 若误用 Go 的 int (64 位) 就会
// 原样输出 850000000000. 这里独立用 int64 相乘再截断来算期望值.
func TestConnectKeyWrapsAt32Bit(t *testing.T) {
	var v int32 = 1000000

	want := strconv.Itoa(int(int32(int64(v) * 850000)))
	if want == "850000000000" {
		t.Fatal("测试自身失效: int64 相乘居然没超出 int32")
	}

	got := ConnectKeyLast(v)
	if !strings.Contains(got, "6:"+want+"7:") {
		t.Errorf("ConnectKeyLast(%d) 的 6: 段 = %q, 期望 int32 回绕值 %q", v, want, want)
	}
	if strings.Contains(got, "6:8500000000007:") {
		t.Error("出现了 64 位结果, 说明回绕语义丢了")
	}
}

// TestConnectKey115AndLastDiffer 115 与 Last 只差 7:/8: 两段
func TestConnectKey115AndLastDiffer(t *testing.T) {
	var v int32 = 7

	a := ConnectKeyNew115Test(v)
	b := ConnectKeyLast(v)

	if !strings.Contains(b, "7:"+strconv.Itoa(int(v*1800000))+"8:"+strconv.Itoa(int(v*3800000))) {
		t.Errorf("ConnectKeyLast 缺少 7:/8: 段: %q", b)
	}
	if strings.Contains(a, "7:") || strings.Contains(a, "8:") {
		t.Errorf("ConnectKeyNew115Test 不应有 7:/8: 段: %q", a)
	}
}
