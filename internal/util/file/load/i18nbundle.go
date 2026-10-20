package load

import (
	"rwhps/internal/struct/orderedmap"
	"rwhps/internal/util/log"
	messageformat "rwhps/internal/util/str"
)

// I18NBundle 对应 I18NBundle
type I18NBundle struct {
	languageData *orderedmap.Map[string]
}

// NewI18NBundleFromBytes 从字节加载
func NewI18NBundleFromBytes(b []byte) *I18NBundle {
	bundle := &I18NBundle{languageData: orderedmap.New[string]()}
	bundle.load(b)
	return bundle
}

// NewI18NBundleFromIni 从 Ini 构建
func NewI18NBundleFromIni(ini *Ini) *I18NBundle {
	bundle := &I18NBundle{languageData: orderedmap.New[string]()}
	ini.Data().EachAll(func(k, v string) { bundle.languageData.Put(k, v) })
	return bundle
}

// AddLanguageDataBytes 追加语言数据
func (b *I18NBundle) AddLanguageDataBytes(data []byte) {
	b.load(data)
}

func (b *I18NBundle) load(data []byte) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("[Load Language File] Error")
		}
	}()
	p := parseProperties(string(data))
	p.EachAll(func(k, v string) { b.languageData.Put(k, v) })
}

// AddLangIni 加入自定义 (cover=强制覆盖)
func (b *I18NBundle) AddLangIni(ini *Ini, cover bool) bool {
	flag := cover
	ini.Data().EachAll(func(k, v string) {
		if cover {
			b.languageData.Put(k, v)
		} else {
			flag = !b.languageData.ContainsKey(k)
			if flag {
				b.languageData.Put(k, v)
			}
		}
	})
	return flag
}

// AddLang 加入单条自定义
func (b *I18NBundle) AddLang(k, v string, cover bool) bool {
	if cover {
		b.languageData.Put(k, v)
		return true
	}
	flag := !b.languageData.ContainsKey(k)
	if flag {
		b.languageData.Put(k, v)
	}
	return flag
}

func (b *I18NBundle) language(input string, params []any) string {
	text, ok := b.languageData.Get(input)
	if ok && text != "" {
		return messageformat.Format(text, params)
	}
	log.Warn("Translation missing, please check " + input)
	return input + " : Key is invalid."
}

// GetInput 对应 getinput
func (b *I18NBundle) GetInput(input string, params ...any) string {
	return b.language(input, params)
}

// GetInputt 对应 getinputt
func (b *I18NBundle) GetInputt(input string, params []any) string {
	return b.language(input, params)
}

// Contains 是否包含 key
func (b *I18NBundle) Contains(k string) bool { return b.languageData.ContainsKey(k) }

// Data 返回底层数据 (只读用途)
func (b *I18NBundle) Data() *orderedmap.Map[string] { return b.languageData }
