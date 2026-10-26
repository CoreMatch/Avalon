// Package emoji 提供 emoji 判定 (对应 com.vdurmont.emoji.EmojiManager.containsEmoji)
//
// # 与原版的差异
//
// 原版用第三方库 com.vdurmont:emoji-java, 它内部是一份完整的 emoji 数据表
// (按 unicode 码位 + 变体选择符 + ZWJ 序列匹配). Go 侧不引入外部依赖
// (go.mod 目前零依赖), 因此按 Unicode UTS#51 的规则自行实现:
//
//   - Emoji_Presentation=Yes 的码位直接算 emoji (绝大多数彩色 emoji);
//   - "文本默认"字符 (©, ™, ❤, ▶ ...) 本身不算, 但后面跟 U+FE0F
//     (变体选择符-16) 时算 —— 这正是 ❤️ 与 ❤ 的区别;
//   - 肤色修饰符 (U+1F3FB..U+1F3FF) 算 emoji.
//
// # 与 emoji-java 的已知差异
//
// emoji-java 的数据表是某个 Unicode 版本的快照, 且包含少量按 Unicode 规则
// 属于"文本默认"的字符 (例如某些 dingbat), 而本实现严格按 UTS#51 的
// Emoji_Presentation 划分. 极端情况下, 某个房间 ID 的判定结果可能与原版不同.
// 判定偏保守 (宁可漏判不误判) —— 误判会让用户精心起的房间名直接被拒.
package emoji

// emojiPresentation 是 Emoji_Presentation=Yes 的码位区段 (按码位升序, 互不重叠)
var emojiPresentation = [][2]rune{
	{0x231A, 0x231B},   // ⌚ ⌛
	{0x23E9, 0x23EC},   // ⏩ ⏪ ⏫ ⏬
	{0x23F0, 0x23F0},   // ⏰
	{0x23F3, 0x23F3},   // ⏳
	{0x25FD, 0x25FE},   // ◽ ◾
	{0x2614, 0x2615},   // ☔ ☕
	{0x2648, 0x2653},   // ♈..♓
	{0x267F, 0x267F},   // ♿
	{0x2693, 0x2693},   // ⚓
	{0x26A1, 0x26A1},   // ⚡
	{0x26AA, 0x26AB},   // ⚪ ⚫
	{0x26BD, 0x26BE},   // ⚽ ⚾
	{0x26C4, 0x26C5},   // ⛄ ⛅
	{0x26CE, 0x26CE},   // ⛎
	{0x26D4, 0x26D4},   // ⛔
	{0x26EA, 0x26EA},   // ⛪
	{0x26F2, 0x26F3},   // ⛲ ⛳
	{0x26F5, 0x26F5},   // ⛵
	{0x26FA, 0x26FA},   // ⛺
	{0x26FD, 0x26FD},   // ⛽
	{0x2705, 0x2705},   // ✅
	{0x270A, 0x270B},   // ✊ ✋
	{0x2728, 0x2728},   // ✨
	{0x274C, 0x274C},   // ❌
	{0x274E, 0x274E},   // ❎
	{0x2753, 0x2755},   // ❓ ❔ ❕
	{0x2757, 0x2757},   // ❗
	{0x2795, 0x2797},   // ➕ ➖ ➗
	{0x27B0, 0x27B0},   // ➰
	{0x27BF, 0x27BF},   // ➿
	{0x2B1B, 0x2B1C},   // ⬛ ⬜
	{0x2B50, 0x2B50},   // ⭐
	{0x2B55, 0x2B55},   // ⭕
	{0x1F004, 0x1F004}, // 🀄
	{0x1F0CF, 0x1F0CF}, // 🃏
	{0x1F18E, 0x1F18E}, // 🆎
	{0x1F191, 0x1F19A}, // 🆑..🆚
	{0x1F1E6, 0x1F1FF}, // 区域指示符 (国旗的组成部分)
	{0x1F201, 0x1F201}, // 🈁
	{0x1F21A, 0x1F21A}, // 🈚
	{0x1F22F, 0x1F22F}, // 🈯
	{0x1F232, 0x1F23A}, // 🈲..🈺
	{0x1F250, 0x1F251}, // 🉐 🉑
	{0x1F300, 0x1F321}, // 🌀..🏡
	{0x1F324, 0x1F393}, // 🏤..🏳
	{0x1F396, 0x1F397}, // 🏖 🏗
	{0x1F399, 0x1F9CB}, // 🏙..🧋 (含肤色修饰符 U+1F3FB..U+1F3FF)
	{0x1F9CD, 0x1F9FF}, // 🧍..🧿
	{0x1FA70, 0x1FA74}, // 🩰..🩴
	{0x1FA78, 0x1FA7C}, // 🩸..🩼
	{0x1FA80, 0x1FA86}, // 🪀..🪆
	{0x1FA90, 0x1FAAC}, // 🪐..🪬
	{0x1FAB0, 0x1FABA}, // 🪰..🪺
	{0x1FAC0, 0x1FAC5}, // 🫀..🫅
	{0x1FAD0, 0x1FAD9}, // 🫐..🫙
	{0x1FAE0, 0x1FAE7}, // 🫠..🫧
	{0x1FAF0, 0x1FAF6}, // 🫰..🫶
}

// emojiBase 是"文本默认"的 emoji 基字符
//
// 这些字符单独出现时不按 emoji 呈现, 但加上 U+FE0F 之后就是 (例如 ❤ + FE0F = ❤️).
var emojiBase = [][2]rune{
	{0x00A9, 0x00A9}, // ©
	{0x00AE, 0x00AE}, // ®
	{0x203C, 0x203C}, // ‼
	{0x2049, 0x2049}, // ⁉
	{0x2122, 0x2122}, // ™
	{0x2139, 0x2139}, // ℹ
	{0x2190, 0x21FF}, // 箭头
	{0x2300, 0x23FF}, // 技术符号
	{0x25A0, 0x25FF}, // 几何图形
	{0x2600, 0x27BF}, // 杂项符号与装饰
	{0x2934, 0x2935}, // ⤴ ⤵
	{0x2B00, 0x2BFF}, // 杂项符号与箭头
	{0x3030, 0x3030}, // 〰
	{0x303D, 0x303D}, // 〽
	{0x3297, 0x3297}, // ㊗
	{0x3299, 0x3299}, // ㊙
}

const (
	// variationSelector16 是变体选择符-16 (U+FE0F)
	variationSelector16 = 0xFE0F
)

func inRanges(r rune, ranges [][2]rune) bool {
	lo, hi := 0, len(ranges)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch {
		case r < ranges[mid][0]:
			hi = mid - 1
		case r > ranges[mid][1]:
			lo = mid + 1
		default:
			return true
		}
	}
	return false
}

// IsEmojiPresentation 判断该码位是否按 emoji 呈现
func IsEmojiPresentation(r rune) bool {
	return inRanges(r, emojiPresentation)
}

// IsEmojiBase 判断该码位是否是"文本默认"的 emoji 基字符
func IsEmojiBase(r rune) bool {
	return inRanges(r, emojiBase)
}

// ContainsEmoji 对应 EmojiManager.containsEmoji
//
// 只要串里出现一个 emoji 就返回 true.
func ContainsEmoji(s string) bool {
	runes := []rune(s)
	for i, r := range runes {
		if IsEmojiPresentation(r) {
			return true
		}
		// U+FE0F 把前一个基字符变成 emoji 呈现
		if r == variationSelector16 && i > 0 && IsEmojiBase(runes[i-1]) {
			return true
		}
	}
	return false
}
