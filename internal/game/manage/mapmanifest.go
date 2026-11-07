// Package manage 地图清单.
//
// 与模组清单 (modmanifest.go) 同一套抽象: 内容清单 = {名字, 文件名,
// SHA-256, 大小}, 内容哈希是唯一信任依据 (契约层面排除按名字匹配),
// 同名不同内容不当作已有.
//
// 两者的差异只在传输面: 模组由客户端外挂器经 HTTP 拉取, 地图由引擎在
// 开局时随 START_GAME 包流送给客户端 (官方内置通道, 无需额外端口).
// 清单在这里的作用是让服务端自己知道"这张图叫什么、多大、什么哈希":
//   - 登记时 (ReadMapAndSave) 计算并随 MapData 保存;
//   - 开局前 (GameSession.StartGame -> MapData.VerifyOnDisk) 复核盘上字节;
//   - 经协议 v4 的 room.start (mapSha256/mapSize) 交给 helper 在流送前再核一次.
package manage

import (
	"sort"
)

// MapManifestEntry 地图清单条目, 字段语义与 ModManifestEntry 一致
type MapManifestEntry struct {
	Name   string `json:"name"`
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// MapManifest 当前已登记地图的内容清单 (只读视图)
type MapManifest struct {
	Maps []MapManifestEntry `json:"maps"`
}

// BuildMapManifest 以登记顺序生成当前地图清单.
//
// 清单缺失 (登记时未能计算哈希) 的条目不带哈希/大小 —— 开局校验会按
// ErrMapContentMismatch 明确失败, 不会静默放行.
func BuildMapManifest() *MapManifest {
	out := &MapManifest{Maps: []MapManifestEntry{}}
	names := make([]string, 0, len(mapsDataOrder))
	names = append(names, mapsDataOrder...)
	sort.Strings(names)
	for _, name := range names {
		data, ok := MapsData[name]
		if !ok {
			continue
		}
		out.Maps = append(out.Maps, MapManifestEntry{
			Name:   name,
			File:   data.MapFileName + data.MapType.FileType(),
			SHA256: data.ContentSHA256(),
			Size:   data.ContentSize(),
		})
	}
	return out
}

// LookupManifest 按地图名取清单条目 (未登记返回 ok=false)
func LookupManifest(name string) (MapManifestEntry, bool) {
	data, ok := MapsData[name]
	if !ok {
		return MapManifestEntry{}, false
	}
	return MapManifestEntry{
		Name:   name,
		File:   data.MapFileName + data.MapType.FileType(),
		SHA256: data.ContentSHA256(),
		Size:   data.ContentSize(),
	}, true
}
