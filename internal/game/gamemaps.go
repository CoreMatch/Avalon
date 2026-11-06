// Package game (Go-Server) 对应 net.rwhps.server.game.GameMaps
package game

import (
	"errors"

	"rwhps/internal/data/global"
	"rwhps/internal/util/compression"
	"rwhps/internal/util/file"
)

// MapType 对应 GameMaps.MapType, 每个取值带一个地图文件后缀
type MapType int

const (
	// DefaultMap 官方地图
	DefaultMap MapType = iota
	// CustomMap 自定义地图
	CustomMap
	// SavedGames 游戏里保存的进度
	SavedGames
)

var mapTypeFileTypes = [...]string{".tmx", ".tmx", ".rwsave"}
var mapTypeNames = [...]string{"DefaultMap", "CustomMap", "SavedGames"}

// FileType 对应 MapType.fileType
func (m MapType) FileType() string {
	if m >= 0 && int(m) < len(mapTypeFileTypes) {
		return mapTypeFileTypes[m]
	}
	return ".tmx"
}

// String 返回枚举名
func (m MapType) String() string {
	if m >= 0 && int(m) < len(mapTypeNames) {
		return mapTypeNames[m]
	}
	return "DefaultMap"
}

// MapFileType 对应 GameMaps.MapFileType
type MapFileType int

const (
	// MapFile 以文件形式提供
	MapFile MapFileType = iota
	// MapZip 以 ZIP 提供 (文件在 ZIP 内)
	MapZip
	// MapWebDownload 以 WebFile 形式提供 (需要下载)
	MapWebDownload
)

var mapFileTypeNames = [...]string{"File", "Zip", "WebDownLoad"}

// String 返回枚举名
func (m MapFileType) String() string {
	if m >= 0 && int(m) < len(mapFileTypeNames) {
		return mapFileTypeNames[m]
	}
	return "File"
}

// ErrMapSourceUnavailable 表示地图既不在本地也没有可用的下载来源
var ErrMapSourceUnavailable = errors.New("game: map source unavailable")

// MapData 对应 GameMaps.MapData
type MapData struct {
	// MapType 地图类型
	MapType MapType
	// MapFileName 地图名 (不含后缀)
	MapFileName string

	mapFileType MapFileType
	zipFileName string
	mapClean    bool
	mapFile     *file.FileUtils
}

// NewMapData 对应 MapData(mapType, mapFileType, mapFileName)
func NewMapData(mapType MapType, mapFileType MapFileType, mapFileName string) *MapData {
	return &MapData{MapType: mapType, mapFileType: mapFileType, MapFileName: mapFileName}
}

// NewMapDataZip 对应 MapData(mapType, mapFileType, mapFileName, zipFileName)
func NewMapDataZip(mapType MapType, mapFileType MapFileType, mapFileName, zipFileName string) *MapData {
	return &MapData{MapType: mapType, mapFileType: mapFileType, MapFileName: mapFileName, zipFileName: zipFileName}
}

// ReadMap 对应 MapData.readMap: 把地图从 zip 里解出来落到磁盘, 供游戏内核按路径读取
//
// 与原版差异: 原版在 File 类型下会走到 bytesMap!! 抛 NPE (bytesMap 从未赋值),
// 这里改为返回 ErrMapSourceUnavailable.
func (d *MapData) ReadMap() error {
	dir := global.ServerMapsPath
	if d.MapType == SavedGames {
		dir = global.ServerSavePath
	}
	folder := file.GetFolder(dir)
	target := folder.ToFile(d.MapFileName + d.MapType.FileType())
	if target.Exists() {
		return nil
	}

	var bytesMap []byte
	switch d.mapFileType {
	case MapFile:
		// 文件形式不需要再落一份, 但目标不存在说明调用方用错了类型
		return ErrMapSourceUnavailable
	case MapZip:
		if d.zipFileName == "" {
			return ErrMapSourceUnavailable
		}
		b, err := compression.ZipEntryBytes(folder.ToFile(d.zipFileName).PathString(), d.MapType.FileType(), d.MapFileName)
		if err != nil {
			return err
		}
		bytesMap = b
	case MapWebDownload:
		// 原版也是空分支, 下载逻辑从未实现
		return ErrMapSourceUnavailable
	}

	d.mapClean = true
	d.mapFile = target
	d.mapFile.WriteFileBytes(bytesMap)
	return nil
}

// Clean 对应 MapData.clean: 清理服务器为读取而临时落盘的地图
func (d *MapData) Clean() {
	if d.mapClean && d.mapFile != nil {
		d.mapFile.Delete()
		d.mapFile = nil
	}
}

// FileType 返回本地图数据的来源类型
func (d *MapData) FileType() MapFileType { return d.mapFileType }

// ZipFileName 返回 zip 包名 (非 zip 来源时为空)
func (d *MapData) ZipFileName() string { return d.zipFileName }

// GameMaps 对应 GameMaps: 当前房间使用的地图
type GameMaps struct {
	// MapType 地图类型
	MapType MapType
	// MapData 地图数据
	MapData *MapData
	// MapName 地图名
	MapName string
	// MapPlayer 地图人数标记
	MapPlayer string
}

// NewGameMaps 对应 GameMaps() 的字段默认值
func NewGameMaps() *GameMaps {
	return &GameMaps{
		MapType:   DefaultMap,
		MapName:   DefaultMapName,
		MapPlayer: DefaultMapPlayer,
	}
}
