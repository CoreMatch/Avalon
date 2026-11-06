// Package compression 对应 net.rwhps.server.util.compression
package compression

import (
	"archive/zip"
	"errors"
	"io"
	"path"
	"strings"
)

// ErrZipEntryNotFound 对应 FileException("CANNOT_FIND_FILE")
var ErrZipEntryNotFound = errors.New("zip: cannot find file")

// ZipFileNames 对应 AbstractDecoder.getTheFileNameOfTheSpecifiedSuffixInTheZip
//
// 返回 zip 内所有以 endWith 结尾的条目名, 去掉最后一个 "." 之后的后缀.
// 与原版一致: endWith 不带点 (例如 "tmx"), 判断用的是裸后缀匹配.
func ZipFileNames(zipPath, endWith string) ([]string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	out := make([]string, 0, 8)
	for _, f := range r.File {
		name := f.Name
		if !strings.HasSuffix(name, endWith) {
			continue
		}
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[:i]
		}
		out = append(out, name)
	}
	return out, nil
}

// ZipEntryBytes 对应 AbstractDecoder.getTheFileBytesOfTheSpecifiedSuffixInTheZip
//
// 找出第一个"以 suffix 结尾且去掉后缀后包含 nameContains"的条目, 返回其内容.
// suffix 带点 (例如 ".tmx"), 与 MapType.fileType 一致.
func ZipEntryBytes(zipPath, suffix, nameContains string) ([]byte, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	for _, f := range r.File {
		name := f.Name
		if !strings.HasSuffix(name, suffix) {
			continue
		}
		base := path.Base(name)
		if i := strings.LastIndex(base, "."); i >= 0 {
			base = base[:i]
		}
		if !strings.Contains(base, nameContains) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	return nil, ErrZipEntryNotFound
}
