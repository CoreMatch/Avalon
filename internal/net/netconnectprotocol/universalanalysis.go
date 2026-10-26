// Package netconnectprotocol 对应 net.rwhps.server.net.netconnectprotocol 的顶层部分
// (UniversalAnalysisOfGamePackages; TypeRelay / RwHps / FakeRwHps 在后续批次)
package netconnectprotocol

import (
	"errors"
	"strings"

	"rwhps/internal/game/player"
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

// GetPacketTeamData 对应 UniversalAnalysisOfGamePackages.getPacketTeamData
//
// 从 TEAM_LIST 包里找出"自己"那一行, 把 site / team / name 填回 playerRelay.
//
// # 读取顺序严格照搬原版
//
//	Int(site) -> Boolean -> Int(playerSize) -> 解码头 "c" 的内层流
//	  每个玩家: Boolean(是否在房间里)
//	    Int -> Byte(site)
//	      site 相同: Int -> Int(team) -> IsString(name) -> Boolean
//	      site 不同: skip 8 -> IsString -> skip 1
//	    之后按 parseVersion 逐段跳过:
//	      >=55 skip 5, >=91 skip 5, >=97 skip 2, >=125 skip 6,
//	      >=149 IsString + skip 4, >=156 四个 IsInt + skip 4
//
// # 两处必须留意的语义
//
//   - playerRelay.site 是直接赋值 (原版 site 是普通 var), 但 playerRelay.team
//     走的是 setter —— 原版的 setter 是 `field = site`, 因此 team 最终等于
//     **site**, 而不是包里读到的那个 Int. 这里必须调 SetTeam 而不是直接写
//     字段, 否则 team 会真的存下读到的值, 与原版脱钩.
//   - site 的比较用 Java byte 的**有符号**语义 (0xFF 是 -1), Go 侧
//     GameInputStream.ReadByte 已经是这个语义, 直接用即可.
//
// 原版在 parseVersion == 0 时抛 ParseException; Go 侧返回 error.
func GetPacketTeamData(parse *gameio.GameInputStream, playerRelay *player.PlayerRelay) error {
	if parse == nil || playerRelay == nil {
		return errors.New("GetPacketTeamData: nil parse stream or player relay")
	}
	if parse.ParseVersion() == 0 {
		return errors.New("need To Parse The Version")
	}

	playerRelay.Site = int(parse.ReadInt())
	if parse.ReadBoolean() {
		return nil
	}
	playerSize := parse.ReadInt()

	unit := parse.GetDecodeStream(true)
	for count := int32(0); count < playerSize; count++ {
		if !unit.ReadBoolean() {
			continue
		}

		unit.ReadInt()
		site := unit.ReadByte()
		if site == playerRelay.Site {
			unit.ReadInt()
			// 原版是 `playerRelay.team = it.readInt()`, 走的是 setter (field = site)
			playerRelay.SetTeam(int(unit.ReadInt()))
			playerRelay.Name = unit.ReadIsString()
			unit.ReadBoolean()
		} else {
			unit.Skip(8) // Int+Int
			unit.ReadIsString()
			unit.Skip(1)
		}

		unit.Skip(12) // Int+Long
		if parse.ParseVersion() >= 55 {
			unit.Skip(5) // Boolean+Int
		}
		if parse.ParseVersion() >= 91 {
			unit.Skip(5) // Int+Byte
		}
		if parse.ParseVersion() >= 97 {
			unit.Skip(2) // Boolean *2
		}
		if parse.ParseVersion() >= 125 {
			unit.Skip(6) // Boolean+Boolean+Int
		}
		if parse.ParseVersion() >= 149 {
			unit.ReadIsString()
			unit.Skip(4) // Int
		}
		if parse.ParseVersion() >= 156 {
			unit.ReadIsInt()
			unit.ReadIsInt()
			unit.ReadIsInt()
			unit.ReadIsInt()
			unit.Skip(4) // Int
		}
	}

	return nil
}
