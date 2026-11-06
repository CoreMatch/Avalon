// Package jsonutil 提供与 Gson 行为一致的序列化/反序列化
package jsonutil

import (
	"bytes"
	"encoding/json"
	"errors"
)

// ToPrettyJSON 对应 toPrettyPrintingJson (Gson pretty printing, 2 空格缩进, 不转义 HTML)
func ToPrettyJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return ""
	}
	out := buf.Bytes()
	// Encoder.Encode 会追加换行, 去掉结尾换行以匹配 Gson
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return string(out)
}

// ToJSON 对应 toJson (紧凑)
func ToJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return ""
	}
	out := buf.Bytes()
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return string(out)
}

// FromJSON 对应 toGson (反序列化), 空串返回错误
func FromJSON(data string, v any) error {
	if data == "" {
		return errors.New("empty json data")
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(data)))
	return dec.Decode(v)
}
