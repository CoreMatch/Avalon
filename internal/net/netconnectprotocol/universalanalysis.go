// Package netconnectprotocol 对应 net.rwhps.server.net.netconnectprotocol 的顶层部分
// (UniversalAnalysisOfGamePackages; TypeRelay / RwHps / FakeRwHps 在后续批次)
package netconnectprotocol

import (
	"strings"

	gameio "rwhps/internal/io"
)

// GetPacketUnitName 对应 UniversalAnalysisOfGamePackages.getPacketUnitName
//
// 从一个"游戏内单位数据包"里取出全部不重复的单位名, 供 mod 校验 / 单位限制使用.
// 读取顺序严格照搬原版:
//
//	String(空) -> skip 8 -> String -> skip 34 -> Boolean
//	  └ true: 解码头 "c" 的内层流, skip 4, 循环 readInt 次:
//	      String / Int / Boolean / IsString(名字) / skip 16
//
// 与原版的差异: 原版 readString/skip 抛 IOException 由调用方处理;
// Go 版把错误随第一个返回值返回, 读到的部分单位名同样返回 (调用方可自行决定是否使用).
func GetPacketUnitName(bytes []byte) ([]string, error) {
	result := make([]string, 0, 8)

	stream := gameio.NewGameInputStreamFromBytes(bytes, 0)
	stream.ReadString()
	stream.Skip(8)
	stream.ReadString()
	stream.Skip(34)

	if !stream.ReadBoolean() {
		return result, nil
	}

	unit := stream.GetDecodeStream(false)
	unit.Skip(4)
	// 注意: Kotlin 的 `for (i in 0 until unit.readInt())` 只读一次上界,
	// 这里必须把 count 提到循环外, 否则每轮都会从流里再吃掉 4 字节.
	count := unit.ReadInt()
	for i := int32(0); i < count; i++ {
		unit.ReadString()
		unit.ReadInt()
		unit.ReadBoolean()

		cacheA := unit.ReadIsString()
		cache := cacheA
		if strings.TrimSpace(cacheA) == "" {
			cache = "Default"
		}

		found := false
		for _, name := range result {
			if name == cache {
				found = true
				break
			}
		}
		if !found {
			result = append(result, cache)
		}

		unit.Skip(16)
	}

	return result, nil
}
