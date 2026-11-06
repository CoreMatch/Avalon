package ip

import (
	gonet "net"
	"testing"
)

// TestGetPrivateIp 对应 IpUtils.getPrivateIp: 结果要么为空 (无可用网卡),
// 要么是一个非回环的 IPv4 地址.
func TestGetPrivateIp(t *testing.T) {
	got := GetPrivateIp()
	if got == "" {
		t.Skip("当前环境没有可用的非回环 IPv4 网卡")
	}
	parsed := gonet.ParseIP(got)
	if parsed == nil {
		t.Fatalf("GetPrivateIp 返回了非法 IP: %q", got)
	}
	if parsed.To4() == nil {
		t.Fatalf("GetPrivateIp 应返回 IPv4: %q", got)
	}
	if parsed.IsLoopback() {
		t.Fatalf("GetPrivateIp 不应返回回环地址: %q", got)
	}
}
