package load

import (
	"strings"
	"testing"

	"rwhps/internal/resource"
)

// TestI18NBundleLoad 用真实语言包验证加载与取值
func TestI18NBundleLoad(t *testing.T) {
	b, err := resource.Bundle("EN")
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}
	bundle := NewI18NBundleFromBytes(b)

	// HPS_en_US.properties 去掉注释与续行后正好 204 个 key; 数量变化说明解析器漂移了
	if bundle.Size() != 204 {
		t.Fatalf("bundle size = %d, want 204", bundle.Size())
	}
	if !bundle.Contains("server.login") {
		t.Fatal("missing server.login")
	}
	// Go 版新增: 外部游戏内核的接入提示 (原版没有这个 key, 它不需要外部进程)
	if !bundle.Contains("server.load.gameCore") {
		t.Fatal("missing server.load.gameCore")
	}
	login := bundle.GetInput("server.login")
	if !strings.Contains(login, "RW-HPS") {
		t.Errorf("server.login = %q", login)
	}
	// 续行与 \n 转义应被还原成换行
	if !strings.Contains(login, "\n") {
		t.Errorf("server.login should contain a newline: %q", login)
	}
}

// TestI18NBundleMissingKey 缺 key 时的回退文案, 对应 language()
func TestI18NBundleMissingKey(t *testing.T) {
	bundle := NewI18NBundleFromBytes([]byte("only.key=value\n"))
	if got := bundle.GetInput("nope"); got != "nope : Key is invalid." {
		t.Errorf("missing key = %q", got)
	}
	if got := bundle.GetInput("only.key"); got != "value" {
		t.Errorf("existing key = %q", got)
	}
}

// TestI18NBundleParams 参数替换走 MessageFormat
func TestI18NBundleParams(t *testing.T) {
	bundle := NewI18NBundleFromBytes([]byte("msg=hello {0}, {1} ms\nnum={0}\n"))
	if got := bundle.GetInput("msg", "world", 1234); got != "hello world, 1,234 ms" {
		t.Errorf("params = %q", got)
	}
	// 无参数时不报错
	if got := bundle.GetInput("num"); got != "{0}" {
		t.Errorf("no params = %q, want %q", got, "{0}")
	}
}

// TestIniKeys URL.ini 的 key 列表按插入顺序
func TestIniKeys(t *testing.T) {
	b, err := resource.URLIni()
	if err != nil {
		t.Fatalf("URLIni: %v", err)
	}
	ini := NewIniFromBytes(b)
	keys := ini.Keys()
	if len(keys) == 0 {
		t.Fatal("no keys")
	}
	if keys[0] != "Get.Core.Update.AllVersion" {
		t.Errorf("first key = %q", keys[0])
	}
	if got := ini.ReadString("Get.Core.Update.AllVersion", ""); !strings.Contains(got, "api.github.com") {
		t.Errorf("ReadString = %q", got)
	}
}
