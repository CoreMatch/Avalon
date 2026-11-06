// Package messageformat 对应 net.rwhps.server.util.str.StringFilteringUtil
//
// 本文件只移植目前有调用方的成员 (cutting), 其余成员等出现消费者时再按需追加.
package messageformat

// Cutting 对应 StringFilteringUtil.cutting(str, length): 截断到 length 长.
//
// 与原版一致: 长度不足时原样返回, 否则取前 length 个字符.
// 原版 substring 按 UTF-16 码元计数; Go 版按 rune 计数, 对 BMP 内字符
// (服务器名常见的中英文/西里尔文) 两者结果一致.
func Cutting(str string, length int) string {
	runes := []rune(str)
	if len(runes) < length {
		return str
	}
	return string(runes[:length])
}
