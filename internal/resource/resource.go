// Package resource 对应原版通过 Main::class.java.getResourceAsStream 读取的内置资源.
//
// 原版把 bundles/eula/URL.ini 打在 jar 里; Go 用 go:embed 内嵌同一批文件.
// 文件内容与 Server-Core/src/main/resources 下的原始文件逐字节一致,
// 原版更新这些资源时需同步拷贝 (见 assets/README.md).
package resource

import (
	"embed"
	"fmt"
)

//go:embed assets/bundles/*.properties assets/eula/*.md assets/URL.ini
var assets embed.FS

// bundleFiles 对应 Initialization.loadLang 里的 4 个语言包
var bundleFiles = map[string]string{
	"CN": "assets/bundles/HPS_zh_CN.properties",
	"HK": "assets/bundles/HPS_zh_HK.properties",
	"RU": "assets/bundles/HPS_ru_RU.properties",
	"EN": "assets/bundles/HPS_en_US.properties",
}

// Languages 按 loadLang 的注册顺序返回语言代码
func Languages() []string { return []string{"CN", "HK", "RU", "EN"} }

// Bundle 返回指定语言的语言包字节, 对应 getResourceAsStream("/bundles/HPS_xx.properties")
func Bundle(lang string) ([]byte, error) {
	name, ok := bundleFiles[lang]
	if !ok {
		return nil, fmt.Errorf("resource: unknown language %q", lang)
	}
	return assets.ReadFile(name)
}

// EULA 返回许可文本, 对应 eula() 里按 serverCountry 选择 Main-China / Main-English
func EULA(country string) ([]byte, error) {
	name := "assets/eula/Main-English.md"
	if country == "CN" {
		name = "assets/eula/Main-China.md"
	}
	return assets.ReadFile(name)
}

// URLIni 返回 URL.ini, 对应 Data.urlData (LoadIni)
func URLIni() ([]byte, error) {
	return assets.ReadFile("assets/URL.ini")
}
