package log

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// capture 把输出重定向到缓冲区, 并关闭颜色以便断言
func capture(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	SetOutput(buf)
	SetColorEnabled(false)
	t.Cleanup(func() {
		SetOutput(nil)
		SetColorEnabled(true)
	})
	return buf
}

// TestSetOutput 验证输出重定向与复位
func TestSetOutput(t *testing.T) {
	buf := &bytes.Buffer{}
	SetOutput(buf)
	if Output() != buf {
		t.Fatal("SetOutput 未生效")
	}

	SetOutput(nil)
	if Output() != os.Stdout {
		t.Fatal("SetOutput(nil) 应恢复 os.Stdout")
	}
}

// TestSetInput 验证输入重定向与复位
func TestSetInput(t *testing.T) {
	r := strings.NewReader("x")
	SetInput(r)
	if Input() != r {
		t.Fatal("SetInput 未生效")
	}

	SetInput(nil)
	if Input() != os.Stdin {
		t.Fatal("SetInput(nil) 应恢复 os.Stdin")
	}
}

// TestTimestampHasNoMillis 对应 Time.getMilliFormat(1) = "yyyy-MM-dd HH:mm:ss"
//
// 原版名字里带 Milli 但下标 1 的格式串不含毫秒, 这里锁住这个易错点.
func TestTimestampHasNoMillis(t *testing.T) {
	re := regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`)
	if got := timestamp(); !re.MatchString(got) {
		t.Fatalf("timestamp() = %q, 不符合 yyyy-MM-dd HH:mm:ss", got)
	}
}

// TestClogParseLog 对应 Log.clog 的 {} 顺序替换
func TestClogParseLog(t *testing.T) {
	buf := capture(t)

	Clog("hello {} world", "go")
	if !strings.Contains(buf.String(), "hello go world") {
		t.Fatalf("输出 = %q", buf.String())
	}

	// 参数不足时保留表达式内文, 空占位符即空串 (与 Parser.parseLog 一致)
	buf.Reset()
	Clog("{} {}", 1)
	if !strings.Contains(buf.String(), ": 1 \n") {
		t.Fatalf("输出 = %q, 期望 \": 1 \"", buf.String())
	}
}

// TestClogParseLogConsumesBracePlaceholders 锁定一个易被"优化"掉的怪癖
//
// 原版 Parser.parseLog 把 {任意内容} 都当作占位符 (而不是只认 {}),
// 所以 "{0}" 在 parseLog 阶段就被吃掉, 后面的 MessageFormat 基本无事可做:
// 第二个 {0} 因为参数用尽而回退成内文 "0", 数字分组也不会发生.
// 数字分组只在 I18NBundle.getInput 那条路径上生效.
func TestClogParseLogConsumesBracePlaceholders(t *testing.T) {
	buf := capture(t)

	Clog("{0} and {0}", "x")
	if !strings.Contains(buf.String(), "x and 0") {
		t.Fatalf("输出 = %q, 期望 \"x and 0\"", buf.String())
	}

	// 没有参数时 parseLog 直接返回原文, MessageFormat 也没有参数可用
	buf.Reset()
	Clog("{0}")
	if !strings.Contains(buf.String(), "{0}") {
		t.Fatalf("输出 = %q, 期望保留 \"{0}\"", buf.String())
	}
}

// TestClogNumberNotGrouped 数字分组不会经由 clog 生效 (原因见上一个用例)
func TestClogNumberNotGrouped(t *testing.T) {
	buf := capture(t)

	Clog("{0}", 1234)
	if strings.Contains(buf.String(), "1,234") {
		t.Fatalf("clog 路径不应发生数字分组: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "1234") {
		t.Fatalf("输出 = %q", buf.String())
	}
}

// TestClogNonString 非字符串参数原样输出
func TestClogNonString(t *testing.T) {
	buf := capture(t)

	Clog(42)
	if !strings.Contains(buf.String(), "42") {
		t.Fatalf("输出 = %q", buf.String())
	}
}

// withBundle 临时注入 i18n 取词函数
func withBundle(t *testing.T, tr Translator) {
	t.Helper()
	old := Bundle
	Bundle = tr
	t.Cleanup(func() { Bundle = old })
}

// TestLoadStatusLogOK 对应 loadStatusLog 的 Start -> OK
func TestLoadStatusLogOK(t *testing.T) {
	buf := capture(t)
	withBundle(t, func(key string, params ...any) string {
		return key + fmt.Sprint(params...)
	})

	ran := false
	LoadStatusLog("load.map", func() { ran = true })

	if !ran {
		t.Fatal("run 未执行")
	}
	out := buf.String()
	if !strings.Contains(out, "load.mapStart") {
		t.Fatalf("缺少 Start 输出: %q", out)
	}
	if !strings.Contains(out, "load.mapOK") {
		t.Fatalf("缺少 OK 输出: %q", out)
	}
	if strings.Contains(out, "load.mapERROR") {
		t.Fatalf("不应出现 ERROR: %q", out)
	}
	// Start 不换行, OK 用 \r 覆盖同一行
	if !strings.Contains(out, "\r") {
		t.Fatalf("OK 行应以 \\r 开头覆盖: %q", out)
	}
}

// TestLoadStatusLogPanic panic 时打印 ERROR 且不再打印 OK
func TestLoadStatusLogPanic(t *testing.T) {
	buf := capture(t)
	withBundle(t, func(key string, params ...any) string {
		return key + fmt.Sprint(params...)
	})

	LoadStatusLog("load.map", func() { panic("boom") })

	out := buf.String()
	if !strings.Contains(out, "load.mapERROR") {
		t.Fatalf("缺少 ERROR 输出: %q", out)
	}
	if strings.Contains(out, "load.mapOK") {
		t.Fatalf("panic 后不应出现 OK: %q", out)
	}
}

// TestLoadStatusLogWithoutBundle 未注入 Bundle 时退回 key
func TestLoadStatusLogWithoutBundle(t *testing.T) {
	buf := capture(t)
	withBundle(t, nil)

	LoadStatusLog("load.map", func() {})

	if !strings.Contains(buf.String(), "load.map") {
		t.Fatalf("输出 = %q", buf.String())
	}
}

// TestWaitLicenseYes Yes 走 ok 分支
func TestWaitLicenseYes(t *testing.T) {
	buf := capture(t)
	SetInput(strings.NewReader("YeS\n"))
	t.Cleanup(func() { SetInput(nil) })

	shown, okCalled, noCalled := false, false, false
	WaitLicense(
		func() { shown = true },
		func() { okCalled = true },
		func() { noCalled = true },
	)

	if !shown || !okCalled || noCalled {
		t.Fatalf("shown=%v ok=%v no=%v", shown, okCalled, noCalled)
	}
	out := buf.String()
	if !strings.Contains(out, "Please Enter (Yes/No) > ") {
		t.Fatalf("缺少输入提示: %q", out)
	}
	if !strings.Contains(out, "Thanks !") {
		t.Fatalf("缺少 Thanks 输出: %q", out)
	}
}

// TestWaitLicenseNo No 走 no 分支
func TestWaitLicenseNo(t *testing.T) {
	capture(t)
	SetInput(strings.NewReader("no\n"))
	t.Cleanup(func() { SetInput(nil) })

	okCalled, noCalled := false, false
	WaitLicense(nil, func() { okCalled = true }, func() { noCalled = true })

	if okCalled || !noCalled {
		t.Fatalf("ok=%v no=%v", okCalled, noCalled)
	}
}

// TestWaitLicenseRetry 非法输入后重新提示, 直到合法
func TestWaitLicenseRetry(t *testing.T) {
	buf := capture(t)
	SetInput(strings.NewReader("maybe\nYes\n"))
	t.Cleanup(func() { SetInput(nil) })

	okCalled := false
	WaitLicense(nil, func() { okCalled = true }, nil)

	if !okCalled {
		t.Fatal("重试后 Yes 应触发 ok")
	}
	out := buf.String()
	if !strings.Contains(out, "Re Enter") {
		t.Fatalf("缺少 Re Enter: %q", out)
	}
	if got := strings.Count(out, "Please Enter (Yes/No) > "); got != 2 {
		t.Fatalf("提示出现 %d 次, want 2", got)
	}
}

// TestWaitLicenseEOF 输入结束时既不 ok 也不 no (原版此处会抛异常)
func TestWaitLicenseEOF(t *testing.T) {
	buf := capture(t)
	SetInput(strings.NewReader(""))
	t.Cleanup(func() { SetInput(nil) })

	okCalled, noCalled := false, false
	WaitLicense(nil, func() { okCalled = true }, func() { noCalled = true })

	if okCalled || noCalled {
		t.Fatalf("EOF 时不应执行回调: ok=%v no=%v", okCalled, noCalled)
	}
	if !strings.Contains(buf.String(), "license not accepted") {
		t.Fatalf("缺少 EOF 提示: %q", buf.String())
	}
}

// TestErrorfTagAndTemplate *f 系列对应 Kotlin 的 error(tag, e, vararg params):
// 首参是 tag, 次参是消息模板, 占位符是 {0}/{1} 而不是 Go 的 %v.
func TestErrorfTagAndTemplate(t *testing.T) {
	buf := capture(t)
	Set(LevelAll.Name)

	Errorf("[HttpRequest]", errBoom)
	Warnf("[net]", "bind {0} fail: {1}", 5123, errBoom)
	Debugf("Config", "Set OK {0} = {1}", "port", 5123)
	Trackf("Config Property", "{0} no Property", "x")

	out := buf.String()
	for _, want := range []string{
		"[HttpRequest]",
		"[net]: bind 5123 fail: boom",
		"Config: Set OK port = 5123",
		"Config Property: x no Property",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("日志缺少 %q\n实际输出:\n%s", want, out)
		}
	}
	// 回归: 以前用 fmt.Sprint 会把格式串原样打进日志
	for _, bad := range []string{"%v", "%d", "%s"} {
		if strings.Contains(out, bad) {
			t.Fatalf("日志里出现了未展开的 %q\n实际输出:\n%s", bad, out)
		}
	}
}

// TestErrorfWithError 消息位直接放 error 时应打印堆栈 (与 Kotlin 的 tag/e 双参一致)
func TestErrorfWithError(t *testing.T) {
	buf := capture(t)
	Set(LevelError.Name)

	Errorf("[Settings]", errBoom)

	out := buf.String()
	if !strings.Contains(out, "[Settings]") {
		t.Fatalf("缺少 tag: %q", out)
	}
	if !strings.Contains(out, "boom") {
		t.Fatalf("缺少错误正文: %q", out)
	}
}

// errBoom 是测试用的固定错误
var errBoom = fmt.Errorf("boom")
