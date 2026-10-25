// Package manage 对应 net.rwhps.server.game.manage.MapManage
package manage

import (
	"path"
	"strings"

	"rwhps/internal/data/global"
	"rwhps/internal/game"
	"rwhps/internal/util/algorithms"
	"rwhps/internal/util/compression"
	"rwhps/internal/util/file"
	"rwhps/internal/util/log"
)

// Maps 对应 MapManage.maps: 当前房间使用的地图
var Maps = game.NewGameMaps()

// MapsData 对应 MapManage.mapsData: 按地图名索引的可选地图
var MapsData = map[string]*game.MapData{}

// GameMaps 对应 MapManage.gameMaps: 内置地图名 -> "显示名@人数标记"
var GameMaps = map[string]string{}

// gameMapsOrder 保持原版 ObjectMap 的插入顺序
var gameMapsOrder []string

func init() {
	for _, kv := range [...][2]string{
		{"Beachlanding(2p)[byhxyy]", "Beach landing (2p) [by hxyy]@[p2]"},
		{"BigIsland(2p)", "Big Island (2p)@[p2]"},
		{"DireStraight(2p)[byuber]", "Dire_Straight (2p) [by uber]@[p2]"},
		{"FireBridge(2p)[byuber]", "Fire Bridge (2p) [by uber]@[p2]"},
		{"Hills(2p)[ByTstis&KPSS]", "Hills_(2p)_[By Tstis & KPSS]@[p2]"},
		{"IceIsland(2p)", "Ice Island (2p)@[p2]"},
		{"Lake(2p)", "Lake (2p)@[p2]"},
		{"SmallIsland(2p)", "Small_Island (2p)@[p2]"},
		{"Twocoldsides(2p)", "Two_cold_sides (2p)@[p2]"},
		{"Hercules(2vs1p)[byuber]", "Hercules_(2vs1p) [by_uber]@[p3]"},
		{"KingoftheMiddle(3p)", "King of the Middle (3p)@[p3]"},
		{"Depthcharges(4p)[byhxyy]", "Depth charges (4p) [by hxyy]@[p4]"},
		{"Desert(4p)", "Desert (4p)@[p4]"},
		{"IceLake(4p)[byhxyy]", "Ice Lake (4p) [by hxyy]@[p4]"},
		{"Islandfreeze(4p)[byhxyy]", "Island freeze (4p) [by hxyy]@[p4]"},
		{"Islands(4p)", "Islands (4p)@[p4]"},
		{"LavaMaze(4p)", "Lava Maze (4p)@[p4]"},
		{"LavaVortex(4p)", "Lava Vortex (4p)@[p4]"},
		{"MagmaIsland(4p)", "Magma Island (4p)@[p4]"},
		{"Manipulation(4p)[ByTstis]", "Manipulation_(4p)_[By Tstis]@[p4]"},
		{"Nuclearwar(4p)[byhxyy]", "Nuclear war (4p) [by hxyy]@[p4]"},
		{"Crossing(6p)", "Crossing (6p)@[p6]"},
		{"ShoretoShore(6p)", "Shore to Shore (6p)@[p6]"},
		{"ValleyPass(6p)", "Valley Pass (6p)@[p6]"},
		{"BridgesOverLava(8p)", "Bridges Over Lava (8p)@[p8]"},
		{"Coastline(8p)[byhxyy]", "Coastline (8p) [by hxyy]@[p8]"},
		{"HugeSubdivide(8p)", "Huge Subdivide (8p)@[p8]"},
		{"Interlocked(8p)", "Interlocked (8p)@[p8]"},
		{"InterlockedLarge(8p)", "Interlocked Large (8p)@[p8]"},
		{"IsleRing(8p)", "Isle Ring (8p)@[p8]"},
		{"LargeIceOutcrop(8p)", "Large Ice Outcrop (8p)@[p8]"},
		{"LavaBiogrid(8p)", "Lava Bio-grid(8p)@[p8]"},
		{"LavaDivide(8p)", "Lava Divide(8p)@[p8]"},
		{"ManyIslands(8p)", "Many Islands (8p)@[p8]"},
		{"RandomIslands(8p)", "Random Islands (8p)@[p8]"},
		{"Tornadoeye(8p)[byhxyy]", "Tornado eye (8p) [by hxyy]@[p8]"},
		{"TwoSides(8p)", "Two Sides (8p)@[p8]"},
		{"Volcano(8p)", "Volcano (8p)@[p8]"},
		{"VolcanoCrater(8p)", "Volcano Crater(8p)@[p8]"},
		{"TwoSidesRemake(10p)", "Two Sides Remake (10p)@[z;p10]"},
		{"ValleyArena(10p)[byuber]", "Valley Arena (10p) [by_uber]@[z;p10]"},
		{"ManyIslandsLarge(10p)", "Many Islands Large (10p)@[z;p10]"},
		{"CrossingLarge(10p)", "Crossing Large (10p)@[z;p10]"},
		{"Kingdoms(10p)[byVulkan]", "Kingdoms (10p) [by Vulkan]@[z;p10]"},
		{"LargeLavaDivide(10p)", "Large Lava Divide (10p)@[z;p10]"},
		{"EnclosedIsland(10p)", "Enclosed Island (10p)@[z;p10]"},
		{"TwoLargeIslands(10p)", "Two_Large_Islands_(10p)@[z;p10]"},
		{"Wetlands(10p)", "Wetlands (10p)@[z;p10]"},
	} {
		GameMaps[kv[0]] = kv[1]
		gameMapsOrder = append(gameMapsOrder, kv[0])
	}
}

// GameMapsOrder 返回内置地图名的注册顺序
func GameMapsOrder() []string {
	out := make([]string, len(gameMapsOrder))
	copy(out, gameMapsOrder)
	return out
}

// ReadMapAndSave 对应 MapManage.readMapAndSave
//
// 扫描 maps 目录下的 .tmx/.zip 与 saves 目录下的 .rwsave/.zip, 把可选地图登记到 MapsData.
// 文件名可能是 Base64 编码的, 先尝试解码.
func ReadMapAndSave() {
	readList(file.GetFolder(global.ServerMapsPath), func(name, suffix, fileName string) {
		switch suffix {
		case ".tmx":
			MapsData[name] = game.NewMapData(game.CustomMap, game.MapFile, name)
		case ".zip":
			zipMaps, err := compression.ZipFileNames(fileName, "tmx")
			if err != nil {
				log.LogError("ZIP READ", err)
				return
			}
			for _, zipMapName := range zipMaps {
				MapsData[zipMapName] = game.NewMapDataZip(game.CustomMap, game.MapZip, zipMapName, name)
			}
		}
	})

	readList(file.GetFolder(global.ServerSavePath), func(name, suffix, fileName string) {
		switch suffix {
		case ".rwsave":
			MapsData[name] = game.NewMapData(game.SavedGames, game.MapFile, name)
		case ".zip":
			zipSaves, err := compression.ZipFileNames(fileName, "save")
			if err != nil {
				log.LogError("ZIP READ", err)
				return
			}
			for _, zipSaveName := range zipSaves {
				MapsData[zipSaveName] = game.NewMapDataZip(game.SavedGames, game.MapZip, zipSaveName, name)
			}
		}
	})
}

// readList 对应 MapManage.readList
//
// 回调参数依次为: 地图名 (去后缀, 必要时已 Base64 解码)、后缀 (含点)、原始文件名.
func readList(folder *file.FileUtils, process func(name, suffix, fileName string)) {
	for _, f := range folder.ListFiles("") {
		original := f.Name()
		if algorithms.Base64IsBase64(original) {
			if decoded, err := algorithms.Base64DecodeString(original); err == nil {
				original = decoded
			}
		}
		idx := strings.LastIndex(original, ".")
		if idx < 0 {
			continue
		}
		suffix := original[idx:]
		name := original[:idx]
		process(name, suffix, f.Name())
	}
}

// Reset 清空地图索引 (测试用, 原版无对应)
func Reset() {
	MapsData = map[string]*game.MapData{}
	Maps = game.NewGameMaps()
}

// BaseName 返回路径的文件名 (供诊断输出)
func BaseName(p string) string { return path.Base(p) }
