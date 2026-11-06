package netconnectprotocol

import (
	"testing"

	"rwhps/internal/game/player"
	gameio "rwhps/internal/io"
)

// buildTeamPacket 按原版读取顺序拼出 TEAM_LIST 包:
//
//	Int(site) -> Boolean(在房间里) -> Int(玩家数) -> String(内层流头) + 长度前缀的内层流
//
// Boolean 为 true 时原版读完就返回, 后面没有内容.
//
// 注意 playerSize 是在**外层**流里读的 (在 getDecodeStream 之前), 内层流里只有
// 每个玩家的记录 —— 位置放错会让整条解析错位.
func buildTeamPacket(site int32, inRoom bool, playerSize int32, inner *gameio.GameOutputStream) []byte {
	o := gameio.NewGameOutputStream()
	o.WriteInt(site)
	o.WriteBoolean(inRoom)
	if inRoom {
		return o.GetByteArray()
	}
	o.WriteInt(playerSize)
	o.WriteString("c") // GetDecodeStream 读掉的内层流头
	o.WriteBytesAndLength(inner.GetByteArray())
	return o.GetByteArray()
}

// buildTeamInner 按原版读取顺序拼出内层流:
//
//	[ Boolean(在房间里) -> Int -> Byte(site) -> (自己的行: Int -> Int(team) -> IsString(name) -> Boolean)
//	                    (别人的行: skip 8 -> IsString -> skip 1)
//	  -> skip 12 -> (按 parseVersion 逐段跳过) ] * n
//
// teamRow 里的 team 值就是包里那个 Int; 原版的 setter 会把它丢掉 (field = site),
// 因此这里可以放心填一个与 site 不同的值来验证这个行为.
func buildTeamInner(parseVersion int, rows []teamRow) *gameio.GameOutputStream {
	inner := gameio.NewGameOutputStream()
	for _, r := range rows {
		inner.WriteBoolean(r.present)
		if !r.present {
			continue
		}
		inner.WriteInt(0) // Int
		inner.WriteByte(byte(r.site))
		if r.site == r.selfSite {
			inner.WriteInt(0)      // Int
			inner.WriteInt(r.team) // Int (team)
			inner.WriteIsString(r.name)
			inner.WriteBoolean(true)
		} else {
			inner.WriteBytes(make([]byte, 8)) // skip 8
			inner.WriteIsString(r.name)
			inner.WriteBytes(make([]byte, 1)) // skip 1
		}
		inner.WriteBytes(make([]byte, 12)) // skip 12
		if parseVersion >= 55 {
			inner.WriteBytes(make([]byte, 5))
		}
		if parseVersion >= 91 {
			inner.WriteBytes(make([]byte, 5))
		}
		if parseVersion >= 97 {
			inner.WriteBytes(make([]byte, 2))
		}
		if parseVersion >= 125 {
			inner.WriteBytes(make([]byte, 6))
		}
		if parseVersion >= 149 {
			inner.WriteIsString("tail")
			inner.WriteBytes(make([]byte, 4))
		}
		if parseVersion >= 156 {
			inner.WriteIsInt(1)
			inner.WriteIsInt(2)
			inner.WriteIsInt(3)
			inner.WriteIsInt(4)
			inner.WriteBytes(make([]byte, 4))
		}
	}
	return inner
}

type teamRow struct {
	present  bool
	site     int
	selfSite int
	team     int32
	name     string
}

// TestGetPacketTeamDataFillsOwnRow 固定"找到自己那一行"的路径.
//
// 注意 team 的最终值: 原版 team 的 setter 是 `field = site`, 所以包里读到的
// team 值会被丢掉, team 最终等于 site. 这是原版实打实的行为, 不是笔误.
func TestGetPacketTeamDataFillsOwnRow(t *testing.T) {
	const selfSite = 7
	rows := []teamRow{
		{present: true, site: 3, selfSite: selfSite, team: 1, name: "other"},
		{present: true, site: selfSite, selfSite: selfSite, team: 2, name: "me"},
		{present: true, site: 9, selfSite: selfSite, team: 3, name: "another"},
	}

	parse := gameio.NewGameInputStreamFromBytes(buildTeamPacket(selfSite, false, int32(len(rows)), buildTeamInner(151, rows)), 151)
	pr := player.NewPlayerRelay(nil, "uuid", "init")

	if err := GetPacketTeamData(parse, pr); err != nil {
		t.Fatalf("GetPacketTeamData: %v", err)
	}

	if pr.Site != selfSite {
		t.Errorf("Site = %d, want %d", pr.Site, selfSite)
	}
	if pr.Name != "me" {
		t.Errorf("Name = %q, want %q", pr.Name, "me")
	}
	// 原版 setter: field = site, 因此 team 等于 site 而不是包里读到的 2
	if pr.Team != selfSite {
		t.Errorf("Team = %d, want %d (原版 setter 是 field = site)", pr.Team, selfSite)
	}
}

// TestGetPacketTeamDataWatchFlag 固定 team setter 的另一半副作用: watch = (value == -3).
func TestGetPacketTeamDataWatchFlag(t *testing.T) {
	const selfSite = 4
	rows := []teamRow{
		{present: true, site: selfSite, selfSite: selfSite, team: -3, name: "watcher"},
	}

	parse := gameio.NewGameInputStreamFromBytes(buildTeamPacket(selfSite, false, int32(len(rows)), buildTeamInner(151, rows)), 151)
	pr := player.NewPlayerRelay(nil, "uuid", "init")

	if err := GetPacketTeamData(parse, pr); err != nil {
		t.Fatalf("GetPacketTeamData: %v", err)
	}
	if !pr.Watch {
		t.Error("team == -3 时 watch 应为 true")
	}
}

// TestGetPacketTeamDataEarlyReturn 固定 Boolean 为 true 时的早退.
func TestGetPacketTeamDataEarlyReturn(t *testing.T) {
	parse := gameio.NewGameInputStreamFromBytes(buildTeamPacket(5, true, 0, nil), 151)
	pr := player.NewPlayerRelay(nil, "uuid", "init")

	if err := GetPacketTeamData(parse, pr); err != nil {
		t.Fatalf("GetPacketTeamData: %v", err)
	}
	if pr.Site != 5 {
		t.Errorf("Site = %d, want 5", pr.Site)
	}
	if pr.Name != "init" {
		t.Errorf("Name = %q, 不应被早退路径改动", pr.Name)
	}
}

// TestGetPacketTeamDataRequiresParseVersion 固定 parseVersion == 0 时报错.
//
// 原版在这里抛 ParseException("need To Parse The Version").
func TestGetPacketTeamDataRequiresParseVersion(t *testing.T) {
	parse := gameio.NewGameInputStreamFromBytes(buildTeamPacket(1, false, 0, buildTeamInner(0, nil)), 0)
	pr := player.NewPlayerRelay(nil, "uuid", "init")

	if err := GetPacketTeamData(parse, pr); err == nil {
		t.Fatal("parseVersion == 0 时应返回错误")
	}
}

// TestGetPacketTeamDataSkipsAbsentRows 固定不在房间里的行被整行跳过.
func TestGetPacketTeamDataSkipsAbsentRows(t *testing.T) {
	const selfSite = 2
	rows := []teamRow{
		{present: false},
		{present: true, site: selfSite, selfSite: selfSite, team: 5, name: "me"},
	}

	parse := gameio.NewGameInputStreamFromBytes(buildTeamPacket(selfSite, false, int32(len(rows)), buildTeamInner(151, rows)), 151)
	pr := player.NewPlayerRelay(nil, "uuid", "init")

	if err := GetPacketTeamData(parse, pr); err != nil {
		t.Fatalf("GetPacketTeamData: %v", err)
	}
	if pr.Name != "me" {
		t.Errorf("Name = %q, want %q", pr.Name, "me")
	}
}

// TestGetPacketTeamDataAcrossParseVersions 跑一遍所有 parseVersion 分支,
// 确认按版本跳过的那几段长度不会错位 (错位的话读出来的会是乱码).
func TestGetPacketTeamDataAcrossParseVersions(t *testing.T) {
	for _, version := range []int{55, 91, 97, 125, 149, 156, 176} {
		const selfSite = 3
		rows := []teamRow{
			{present: true, site: selfSite, selfSite: selfSite, team: 1, name: "me"},
		}

		parse := gameio.NewGameInputStreamFromBytes(buildTeamPacket(selfSite, false, int32(len(rows)), buildTeamInner(version, rows)), version)
		pr := player.NewPlayerRelay(nil, "uuid", "init")

		if err := GetPacketTeamData(parse, pr); err != nil {
			t.Fatalf("version %d: GetPacketTeamData: %v", version, err)
		}
		if pr.Name != "me" {
			t.Errorf("version %d: Name = %q, want %q", version, pr.Name, "me")
		}
	}
}
