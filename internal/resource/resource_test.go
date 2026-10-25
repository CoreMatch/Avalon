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

// TestEULA 中英文许可文本都可读取, 对应 eula() 的分支
func TestEULA(t *testing.T) {
	cn, err := EULA("CN")
	if err != nil || len(cn) == 0 {
		t.Fatalf("EULA(CN) = %d bytes, %v", len(cn), err)
	}
	en, err := EULA("EN")
	if err != nil || len(en) == 0 {
		t.Fatalf("EULA(EN) = %d bytes, %v", len(en), err)
	}
	if string(cn) == string(en) {
		t.Error("EULA(CN) and EULA(EN) should differ")
	}
}

// TestURLIni 对应 Data.urlData 的读取来源
func TestURLIni(t *testing.T) {
	b, err := URLIni()
	if err != nil {
		t.Fatalf("URLIni: %v", err)
	}
	if !strings.Contains(string(b), "Get.Core.Update.AllVersion") {
		t.Error("URLIni missing Get.Core.Update.AllVersion")
	}
}
