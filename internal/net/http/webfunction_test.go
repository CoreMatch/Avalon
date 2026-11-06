package http

import "testing"

// TestStringURLDataResolveToMap 对应 WebFunction.stringUrlDataResolveToJson
func TestStringURLDataResolveToMap(t *testing.T) {
	got := StringURLDataResolveToMap("a=b&c=d")
	if got["a"] != "b" || got["c"] != "d" {
		t.Fatalf("解析结果 = %v", got)
	}
	if m := StringURLDataResolveToMap(""); len(m) != 0 {
		t.Fatalf("空串应返回空表, got %v", m)
	}
}

// TestHeadResolveToCookie 对应 WebFunction.headResolveToCookie
func TestHeadResolveToCookie(t *testing.T) {
	accept := NewAcceptWeb("/x", "", "", &HttpRequestLite{
		Headers: map[string][]string{"Cookie": {"k1=v1; k2=v2"}},
	})
	got := HeadResolveToCookie(accept)
	if got["k1"] != "v1" || got["k2"] != "v2" {
		t.Fatalf("解析结果 = %v", got)
	}
	if m := HeadResolveToCookie(NewAcceptWeb("/x", "", "", &HttpRequestLite{})); len(m) != 0 {
		t.Fatalf("无 Cookie 头应返回空表, got %v", m)
	}
}
