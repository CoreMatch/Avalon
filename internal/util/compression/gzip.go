// Package compression 提供 gzip 编解码 (对应 util.compression.decoder.GzipDecoder 等)
package compression

import (
	"bytes"
	"compress/gzip"
	"io"
)

// GzipEncode 压缩为 gzip 字节序列 (等价 Java GZIPOutputStream)
func GzipEncode(data []byte) []byte {
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	_, _ = w.Write(data)
	_ = w.Close()
	return out.Bytes()
}

// GzipDecode 解压 gzip 字节序列 (等价 Java GZIPInputStream)
func GzipDecode(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	return io.ReadAll(r)
}
