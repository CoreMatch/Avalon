// Package file 对应 net.rwhps.server.util.file.FileUtils
package file

import (
	"os"
	"path/filepath"
	"strings"
)

// FileUtils 简易文件工具, 对应 FileUtils
type FileUtils struct {
	Path string
}

// GetFolder 获取目录
func GetFolder(path string) *FileUtils {
	return &FileUtils{Path: path}
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
