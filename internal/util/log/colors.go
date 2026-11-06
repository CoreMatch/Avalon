package log

import (
	"strings"
	"sync"
)

// 与 ColorCodes.kt 一致的颜色码表
const (
	cReset   = "\u001B[0m"
	cBold    = "\u001B[1m"
	cItalic  = "\u001B[3m"
	cUnder   = "\u001B[4m"
	cBlack   = "\u001B[30m"
	cRed     = "\u001B[31m"
	cGreen   = "\u001B[32m"
	cYellow  = "\u001B[33m"
	cBlue    = "\u001B[34m"
	cPurple  = "\u001B[35m"
	cCyan    = "\u001B[36m"
	cWhite   = "\u001B[37m"
	cBackRed = "\u001B[41m"
	cBackGrn = "\u001B[42m"
	cBackYel = "\u001B[43m"
	cBackBlu = "\u001B[44m"
	cBackDef = "\u001B[49m"
	cLRed    = "\u001B[91m"
	cLGreen  = "\u001B[92m"
	cLYellow = "\u001B[93m"
	cLBlue   = "\u001B[94m"
	cLMagen  = "\u001B[95m"
	cLCyan   = "\u001B[96m"
)

type colorPair struct{ code, value string }

var colorPairs = []colorPair{
	{"ff", "\u001b[H\u001b[2J"},
	{"fr", cReset},
	{"fb", cBold},
	{"fi", cItalic},
	{"fu", cUnder},
	{"bk", cBlack},
	{"r", cRed},
	{"g", cGreen},
	{"y", cYellow},
	{"b", cBlue},
	{"p", cPurple},
	{"c", cCyan},
	{"lr", cLRed},
	{"lg", cLGreen},
	{"ly", cLYellow},
	{"lm", cLMagen},
	{"lb", cLBlue},
	{"lc", cLCyan},
	{"w", cWhite},
	{"bd", cBackDef},
	{"br", cBackRed},
	{"bg", cBackGrn},
	{"by", cBackYel},
	{"bb", cBackBlu},
}

var (
	colorEnabled   = true
	colorEnabledMu sync.RWMutex
)

// SetColorEnabled 对应 System property rwhps.log.color
func SetColorEnabled(enabled bool) {
	colorEnabledMu.Lock()
	defer colorEnabledMu.Unlock()
	colorEnabled = enabled
}

// FormatColors 将 &xx 颜色标记替换为 ANSI 序列; empty=true 时全部清空
func FormatColors(text string, empty bool) string {
	colorEnabledMu.RLock()
	enabled := colorEnabled
	colorEnabledMu.RUnlock()

	out := text
	for i := range colorPairs {
		repl := colorPairs[i].value
		if empty || !enabled {
			repl = ""
		}
		out = strings.ReplaceAll(out, "&"+colorPairs[i].code, repl)
	}
	return out
}

// StripColors 移除 ANSI 转义序列 (用于写文件)
func StripColors(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	i := 0
	for i < len(text) {
		if text[i] == 0x1b && i+1 < len(text) && text[i+1] == '[' {
			j := i + 2
			for j < len(text) && !((text[j] >= 'a' && text[j] <= 'z') || (text[j] >= 'A' && text[j] <= 'Z')) {
				j++
			}
			if j < len(text) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(text[i])
		i++
	}
	return b.String()
}
