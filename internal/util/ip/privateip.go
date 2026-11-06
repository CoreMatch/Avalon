// Package ip 对应 net.rwhps.server.util.IpUtils 的网络接口部分
package ip

import (
	gonet "net"
)

// GetPrivateIp 对应 IpUtils.getPrivateIp(ipv4 = true):
// 遍历全部网卡, 跳过回环/虚拟/未启用的, 返回首个 IPv4 地址; 没有时返回空串.
//
// 上游只被 UpList 插件以默认参数 (取 IPv4) 调用, 因此这里只实现 IPv4 分支;
// 上游的 IPv6 分支无调用方, 不移植.
//
// 与原版差异: Java 的 isVirtual 在 Go 没有直接对应物 (Go 的网卡标志不区分
// 物理/虚拟), 只按 "已启用且非回环" 过滤; 取址顺序与原版一致 (按网卡枚举顺序).
func GetPrivateIp() string {
	ifaces, err := gonet.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&gonet.FlagLoopback != 0 || iface.Flags&gonet.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*gonet.IPNet)
			if !ok {
				continue
			}
			if v4 := ipNet.IP.To4(); v4 != nil {
				return v4.String()
			}
		}
	}
	return ""
}
