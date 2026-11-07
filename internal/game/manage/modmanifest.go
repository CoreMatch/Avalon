// Package manage 模组清单产生器.
//
// 契约依据: Cascade-Backend-v1 v1.3 "Room mod manifest"
// (HA-Contract docs/dev/Cascade/Cascade-Backend-v1.md,
// docs/api/openapi/cascade-backend-v1.yaml). 为客户端模组装配
// (RW-Mod-Injector, 其设计见 RW-Mod-Injector knowledge/0001) 提供
// 本子服游戏内核模组集的只读清单与文件字节.
//
// 权威来源是游戏内核的模组目录 (/data/mods, helper 的
// Data.Plugin_Mods_Path 同目录; 自定义地图流程已确立 Go 侧经文件层
// 直接管理该目录的先例). 不经过 helper 协议, 不改动 Helper.
//
// 信任模型 (契约 v1.3): 内容哈希 (SHA-256) 是装配的唯一信任依据,
// 契约层面排除按名字匹配. 清单里的 sha256/size 与内容端点服务的字节
// 必须严格一致 —— 两者取自同一份缓存产物.
package manage

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/CoreMatch/Avalon/internal/data/global"
	"github.com/CoreMatch/Avalon/internal/util/file"
	"github.com/CoreMatch/Avalon/internal/util/log"
)

// ModManifestEntry 对应契约 RoomModEntry (openapi v1.3)
type ModManifestEntry struct {
	Name   string `json:"name"`
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// ModManifest 对应契约 RoomModManifest (openapi v1.3).
// Sig/SigAlg 为发布者签名预留位, v1.3 恒为 nil (序列化为 null).
type ModManifest struct {
	Mods   []ModManifestEntry `json:"mods"`
	Sig    *string            `json:"sig"`
	SigAlg *string            `json:"sigAlg"`
}

// modFileWhitelist 契约 file 字段白名单字符集 (^[A-Za-z0-9._-]+$)
var modFileWhitelist = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// maxModFileSize 单个模组产物字节上限, 超限条目跳过并记日志.
// var 而非 const, 便于测试调低.
var maxModFileSize int64 = 512 << 20

// zipFixedTime 目录型模组打包的固定时间戳 (确定性产物: 同一目录内容
// 打包出的字节恒定, 清单哈希与内容端点字节因此天然一致)
var zipFixedTime = time.Unix(0, 0)

// ValidModFileName 契约 file 白名单: 字符集命中且不是裸 "." / ".."
func ValidModFileName(name string) bool {
	return modFileWhitelist.MatchString(name) && name != "." && name != ".."
}

type modManifestCache struct {
	fingerprint string
	manifest    *ModManifest
	artifacts   map[string][]byte
}

var (
	modManifestMu sync.Mutex
	modManifestCh modManifestCache
)

// BuildModManifest 产生当前模组清单 (目录指纹未变时复用缓存).
// 目录不存在/不可读返回错误; 空目录返回空清单 (契约: 零模组房间合法).
// 返回的清单只读, 调用方不得修改.
func BuildModManifest() (*ModManifest, error) {
	dir := file.GetFolder(global.PluginModsPath).PathString()
	fp, err := modsFingerprint(dir)
	if err != nil {
		return nil, err
	}

	modManifestMu.Lock()
	defer modManifestMu.Unlock()
	if modManifestCh.manifest != nil && modManifestCh.fingerprint == fp {
		return modManifestCh.manifest, nil
	}

	manifest, artifacts, err := buildModManifest(dir)
	if err != nil {
		return nil, err
	}
	modManifestCh = modManifestCache{fingerprint: fp, manifest: manifest, artifacts: artifacts}
	return manifest, nil
}

// ModArtifact 返回 file 对应的可服务字节 (必须命中当前清单).
// 内部先刷新清单再查找, 保证与清单同源自洽.
func ModArtifact(name string) ([]byte, bool, error) {
	if _, err := BuildModManifest(); err != nil {
		return nil, false, err
	}
	modManifestMu.Lock()
	defer modManifestMu.Unlock()
	b, ok := modManifestCh.artifacts[name]
	return b, ok, nil
}

// resetModManifestCache 清空缓存 (测试用)
func resetModManifestCache() {
	modManifestMu.Lock()
	modManifestCh = modManifestCache{}
	modManifestMu.Unlock()
}

// modsFingerprint 目录指纹: 全部条目的相对路径+大小+mtime+类型.
// 用于缓存失效判定 (模组集变化即重建).
func modsFingerprint(dir string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(dir, p)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		fi, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		fmt.Fprintf(h, "%s|%d|%d|%t\n", filepath.ToSlash(rel), fi.Size(), fi.ModTime().UnixNano(), d.IsDir())
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// buildModManifest 枚举模组目录并构造清单与产物:
//   - zip 文件: 原样服务 (引擎直接挂载);
//   - 目录: 确定性打包为 zip 后服务 (引擎同样直接挂载, 内容解析一致);
//   - 其它形态 (散文件等) v1 不支持, 跳过并记日志.
//
// 文件名冲突 (如目录 Bar 与 Bar.zip 并存): zip 文件优先.
func buildModManifest(dir string) (*ModManifest, map[string][]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}

	manifest := &ModManifest{Mods: []ModManifestEntry{}}
	artifacts := map[string][]byte{}
	fromZipFile := map[string]bool{}

	for _, e := range entries { // ReadDir 已按文件名排序
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		src := filepath.Join(dir, name)

		switch {
		case e.IsDir():
			fileName := name + ".zip"
			if !ValidModFileName(fileName) {
				log.Warnf("[ModManifest]", "skip mod dir {0}: packaged name {1} is outside the contract file whitelist", name, fileName)
				continue
			}
			data, zipErr := zipDirectory(src)
			if zipErr != nil {
				log.Warnf("[ModManifest]", "skip mod dir {0}: package failed: {1}", name, zipErr)
				continue
			}
			if int64(len(data)) > maxModFileSize {
				log.Warnf("[ModManifest]", "skip mod dir {0}: packaged size exceeds the limit", name)
				continue
			}
			addModEntry(manifest, artifacts, fromZipFile, name, fileName, data, false)

		case strings.HasSuffix(strings.ToLower(name), ".zip"):
			fi, statErr := os.Stat(src)
			if statErr != nil {
				log.Warnf("[ModManifest]", "skip mod file {0}: {1}", name, statErr)
				continue
			}
			if fi.Size() > maxModFileSize {
				log.Warnf("[ModManifest]", "skip mod file {0}: size exceeds the limit", name)
				continue
			}
			data, readErr := os.ReadFile(src)
			if readErr != nil {
				log.Warnf("[ModManifest]", "skip mod file {0}: {1}", name, readErr)
				continue
			}
			displayName := name[:len(name)-len(".zip")]
			addModEntry(manifest, artifacts, fromZipFile, displayName, name, data, true)

		default:
			log.Debugf("[ModManifest]", "skip unsupported mod entry: {0}", name)
		}
	}
	return manifest, artifacts, nil
}

// addModEntry 写入一条清单条目与产物; 文件名冲突时 zip 文件优先于目录打包.
func addModEntry(m *ModManifest, artifacts map[string][]byte, fromZipFile map[string]bool,
	name, fileName string, data []byte, isZipFile bool) {
	if _, exists := artifacts[fileName]; exists {
		if fromZipFile[fileName] || !isZipFile {
			log.Warnf("[ModManifest]", "mod file name conflict on {0}: keeping the zip file entry", fileName)
			return
		}
		// 已有的是目录打包产物, 用真正的 zip 文件替换
		for i := range m.Mods {
			if m.Mods[i].File == fileName {
				m.Mods = append(m.Mods[:i], m.Mods[i+1:]...)
				break
			}
		}
	}
	sum := sha256.Sum256(data)
	m.Mods = append(m.Mods, ModManifestEntry{
		Name:   name,
		File:   fileName,
		SHA256: hex.EncodeToString(sum[:]),
		Size:   int64(len(data)),
	})
	artifacts[fileName] = data
	fromZipFile[fileName] = isZipFile
}

// zipDirectory 确定性打包目录: 词序遍历 + 固定时间戳.
// 产物作为单文件 zip 交给客户端, 客户端不摘包, 引擎直接挂载.
func zipDirectory(dir string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(dir, p)
		if relErr != nil {
			return relErr
		}
		hdr := &zip.FileHeader{
			Name:     filepath.ToSlash(rel),
			Method:   zip.Deflate,
			Modified: zipFixedTime,
		}
		w, hdrErr := zw.CreateHeader(hdr)
		if hdrErr != nil {
			return hdrErr
		}
		f, openErr := os.Open(p)
		if openErr != nil {
			return openErr
		}
		defer f.Close()
		_, copyErr := io.Copy(w, f)
		return copyErr
	})
	if err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
