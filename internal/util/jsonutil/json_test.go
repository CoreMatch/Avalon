package jsonutil

import (
	"strings"
	"testing"
)

// TestToJSON 紧凑输出: 无尾换行, 不转义 HTML (对齐 Gson)
func TestToJSON(t *testing.T) {
	got := ToJSON(map[string]string{"k": "<a>&b"})
	want := `{"k":"<a>&b"}`
	if got != want {
		t.Fatalf("ToJSON = %q, want %q", got, want)
	}
	if strings.HasSuffix(got, "\n") {
		t.Fatal("ToJSON 不应带尾换行")
	}
}

// TestToPrettyJSON 2 空格缩进, 无尾换行
func TestToPrettyJSON(t *testing.T) {
	got := ToPrettyJSON(map[string]int{"a": 1})
	want := "{\n  \"a\": 1\n}"
	if got != want {
		t.Fatalf("ToPrettyJSON = %q, want %q", got, want)
	}
}

// TestToJSONMarshalError 无法序列化的值返回空串
func TestToJSONMarshalError(t *testing.T) {
	if got := ToJSON(make(chan int)); got != "" {
		t.Fatalf("chan 不可序列化, 应返回空串, got %q", got)
	}
	if got := ToPrettyJSON(make(chan int)); got != "" {
		t.Fatalf("chan 不可序列化, 应返回空串, got %q", got)
	}
}

// TestFromJSON 往返与错误路径
func TestFromJSON(t *testing.T) {
	var v struct {
		A int    `json:"a"`
		B string `json:"b"`
	}
	if err := FromJSON(`{"a":7,"b":"x"}`, &v); err != nil {
		t.Fatal(err)
	}
	if v.A != 7 || v.B != "x" {
		t.Fatalf("FromJSON = %+v", v)
	}

	if err := FromJSON("", &v); err == nil {
		t.Fatal("空串应报错")
	}
	if err := FromJSON("{bad json", &v); err == nil {
		t.Fatal("坏 JSON 应报错")
	}
}
