package resource

import (
	"strings"
	"testing"
)

// TestBundles 4 个语言包都能内嵌读取且含关键 key
func TestBundles(t *testing.T) {
	for _, lang := range Languages() {
		b, err := Bundle(lang)
		if err != nil {
			t.Fatalf("Bundle(%s): %v", lang, err)
		}
		if len(b) == 0 {
			t.Fatalf("Bundle(%s) is empty", lang)
		}
	}
	// 原版 HK/RU 语言包确实没有 server.login, 启动时会打印 "Key is invalid."
	// (见 Initialization.loadLang 与 getinput 的回退), 这里按原样记录该差异.
	for _, lang := range []string{"CN", "EN"} {
		b, _ := Bundle(lang)
		if !strings.Contains(string(b), "server.login") {
			t.Errorf("Bundle(%s) missing key server.login", lang)
		}
	}
	for _, lang := range []string{"HK", "RU"} {
		b, _ := Bundle(lang)
		if strings.Contains(string(b), "server.login") {
			t.Errorf("Bundle(%s) unexpectedly contains server.login; original parity changed", lang)
		}
	}
	if _, err := Bundle("XX"); err == nil {
		t.Error("Bundle(XX) should fail")
	}
}

// TestURLIni 对应 Data.urlData 的读取来源
func TestURLIni(t *testing.T) {
	b, err := URLIni()
	if err != nil {
		t.Fatalf("URLIni: %v", err)
	}
	if !strings.Contains(string(b), "Get.Api.ServerLanguage") {
		t.Error("URLIni missing Get.Api.ServerLanguage")
	}
}
