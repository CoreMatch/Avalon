package emoji

import "testing"

// TestRangeTablesAreSortedAndDisjoint 保证二分查找的前提成立.
//
// 这两张表是手写的码位区段, 一旦顺序写错, inRanges 的二分查找会**静默**给出
// 错误答案 (不报错, 只是漏判). 因此把"表本身有序且不重叠"钉成测试.
func TestRangeTablesAreSortedAndDisjoint(t *testing.T) {
	for name, table := range map[string][][2]rune{
		"emojiPresentation": emojiPresentation,
		"emojiBase":         emojiBase,
	} {
		for i, r := range table {
			if r[0] > r[1] {
				t.Errorf("%s[%d] = %04X..%04X, 起点大于终点", name, i, r[0], r[1])
			}
			if i == 0 {
				continue
			}
			prev := table[i-1]
			if r[0] <= prev[1] {
				t.Errorf("%s[%d] = %04X..%04X 与前一项 %04X..%04X 重叠或逆序", name, i, r[0], r[1], prev[0], prev[1])
			}
		}
	}
}

// TestContainsEmojiDetectsCommonEmoji 覆盖真实会用到的形态.
func TestContainsEmojiDetectsCommonEmoji(t *testing.T) {
	yes := []string{
		"😀",   // U+1F600 彩色表情
		"a😀b", // 混在中间
		"🎉",   // U+1F389
		"❤️",  // U+2764 + U+FE0F (文本默认 + 变体选择符)
		"🇨🇳",  // 区域指示符组成的国旗
		"👍🏽",  // 肤色修饰符
		"⭐",   // U+2B50
		"✅",   // U+2705
		"⌚",   // U+231A
		"房间😀",
	}
	for _, s := range yes {
		if !ContainsEmoji(s) {
			t.Errorf("ContainsEmoji(%q) = false, want true", s)
		}
	}
}

// TestContainsEmojiRejectsPlainText 确认普通文本不会被误判.
//
// 误判的代价是用户起的房间名被直接拒绝, 因此这一侧必须保守.
func TestContainsEmojiRejectsPlainText(t *testing.T) {
	no := []string{
		"",
		"room",
		"房间",
		"Room-123",
		"ab_cd-EF",
		"❤",   // U+2764 单独出现: 文本默认, 不算 emoji
		"©",   // U+00A9
		"™",   // U+2122
		"a❤b", // 基字符后面没有 U+FE0F
	}
	for _, s := range no {
		if ContainsEmoji(s) {
			t.Errorf("ContainsEmoji(%q) = true, want false", s)
		}
	}
}

// TestContainsEmojiEmptyAndEdge 固定几个边界行为.
func TestContainsEmojiEmptyAndEdge(t *testing.T) {
	if ContainsEmoji("") {
		t.Error("空串不应含 emoji")
	}
	// 变体选择符出现在串首: 没有前驱基字符, 不算
	if ContainsEmoji("️") {
		t.Error("孤立的变体选择符不应算 emoji")
	}
	// 非 BMP 字符按 rune 处理, 不会按字节误判
	if ContainsEmoji("😀") != true {
		t.Error("非 BMP emoji 应被识别")
	}
}
