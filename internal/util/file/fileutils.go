// Package file 对应 net.rwhps.server.util.file.FileUtils
//
// 与原版一致: 所有 "绝对" 路径 (以 / 开头) 都相对于程序所在目录 (BasePath),
// 而不是文件系统根目录. BasePath 的解析顺序:
//  1. -Drwhps.basePath=...
//  2. 环境变量 RWHPS_BASE_PATH
//  3. 可执行文件所在目录
//  4. 当前工作目录
package file

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"rwhps/internal/util/systemprops"
)

// FileUtils 简易文件工具, 对应 FileUtils
type FileUtils struct {
	Path string
}

var (
	baseOnce sync.Once
	baseMu   sync.RWMutex
	basePath string
)

// BasePath 返回数据根目录 (总以 / 结尾, 对应 FileUtils.defaultFilePath)
func BasePath() string {
	baseOnce.Do(func() {
		baseMu.Lock()
		basePath = pathProcess(resolveBasePath())
		baseMu.Unlock()
	})
	baseMu.RLock()
	defer baseMu.RUnlock()
	return basePath
}

// SetBasePath 对应 FileUtils.setFilePath
func SetBasePath(p string) {
	baseOnce.Do(func() {})
	baseMu.Lock()
	basePath = pathProcess(p)
	baseMu.Unlock()
}

// resolveBasePath 按优先级解析根目录
func resolveBasePath() string {
	if v, ok := systemprops.GetOrEnv("rwhps.basePath"); ok && v != "" {
		return v
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// pathProcess 对应 FileUtils.pathProcess: 统一斜杠并保证以 / 结尾
func pathProcess(path string) string {
	cache := path
	// Windows 的绝对路径形如 /A:/a/a.jar, 需要去掉前导 /
	if len(cache) > 2 && cache[0] == '/' && cache[2] == ':' {
		cache = cache[1:]
	}
	custom := strings.ReplaceAll(cache, "\\", "/")
	if strings.HasSuffix(custom, "/") {
		return custom
	}
	return custom + "/"
}

// cehckFolderPath 对应 FileUtils.cehckFolderPath (原版拼写就是 cehck)
func cehckFolderPath(defPath, path string) string {
	if path == "" {
		return defPath
	}
	to := path
	if strings.HasPrefix(to, "/") {
		to = to[1:]
	}
	if strings.HasSuffix(to, "/") {
		to = to[:len(to)-1]
	}
	if strings.HasSuffix(defPath, "/") {
		return defPath + to
	}
	return defPath + "/" + to
}

// GetFolder 获取目录 (会创建目录), 对应 FileUtils.getFolder
func GetFolder(path string) *FileUtils {
	to := cehckFolderPath(BasePath(), path)
	_ = os.MkdirAll(to, 0o755)
	return &FileUtils{Path: to}
}

// GetFile 获取文件, 对应 FileUtils.getFile
func GetFile(path string) *FileUtils {
	return &FileUtils{Path: cehckFolderPath(BasePath(), path)}
}

// GetPath 只拼接路径不创建, 对应 FileUtils.getPath
func GetPath(name string) string {
	return cehckFolderPath(BasePath(), name)
}

// SplicePath 对应 FileUtils.splicePath
func SplicePath(splice1, splice2 string) string {
	return cehckFolderPath(splice1, splice2)
}

// ToFile 获取子文件/子目录
func (f *FileUtils) ToFile(name string) *FileUtils {
	return &FileUtils{Path: filepath.Join(f.Path, name)}
}

// Name 返回文件名
func (f *FileUtils) Name() string { return filepath.Base(f.Path) }

// PathString 返回完整路径
func (f *FileUtils) PathString() string { return f.Path }

// Exists 是否存在
func (f *FileUtils) Exists() bool {
	_, err := os.Stat(f.Path)
	return err == nil
}

// IsFolder 是否为目录
func (f *FileUtils) IsFolder() bool {
	fi, err := os.Stat(f.Path)
	return err == nil && fi.IsDir()
}

// CreateFolder 创建目录
func (f *FileUtils) CreateFolder() {
	_ = os.MkdirAll(f.Path, 0o755)
}

// ReadFileStringData 读取文件字符串; 文件不存在时创建并返回空串
func (f *FileUtils) ReadFileStringData() string {
	if !f.Exists() {
		_ = os.MkdirAll(filepath.Dir(f.Path), 0o755)
		_ = os.WriteFile(f.Path, []byte{}, 0o644)
		return ""
	}
	b, err := os.ReadFile(f.Path)
	if err != nil {
		return ""
	}
	return string(b)
}

// ReadFileBytes 读取字节
func (f *FileUtils) ReadFileBytes() ([]byte, error) {
	return os.ReadFile(f.Path)
}

// WriteFile 写入字符串
func (f *FileUtils) WriteFile(content string) {
	_ = os.MkdirAll(filepath.Dir(f.Path), 0o755)
	_ = os.WriteFile(f.Path, []byte(content), 0o644)
}

// WriteFileBytes 写入字节
func (f *FileUtils) WriteFileBytes(content []byte) {
	_ = os.MkdirAll(filepath.Dir(f.Path), 0o755)
	_ = os.WriteFile(f.Path, content, 0o644)
}

// Delete 删除文件
func (f *FileUtils) Delete() {
	_ = os.Remove(f.Path)
}

// ListFiles 列出目录下所有文件 (递归), 仅返回满足后缀名的文件
func (f *FileUtils) ListFiles(suffix string) []*FileUtils {
	var out []*FileUtils
	_ = filepath.Walk(f.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if suffix == "" || strings.HasSuffix(strings.ToLower(path), strings.ToLower(suffix)) {
			out = append(out, &FileUtils{Path: path})
		}
		return nil
	})
	return out
}
