// Package ip 对应 net.rwhps.server.util.IpUtils / IPCountry
package ip

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

var ipv4Pattern = regexp.MustCompile(`^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$`)

const (
	ipv4MaxOctetValue          = 255
	ipv6MaxHexGroups           = 8
	ipv6MaxHexDigitsPerGroup   = 4
	maxUnsignedShort           = 0xffff
	base16                     = 16
)

// ErrIPUnknow 对应 VariableException("[IP-Check IPV4-IPV6 Unknown]")
var ErrIPUnknow = errors.New("IP-Check IPV4-IPV6 Unknown")

// IpToLong24NoError 与 IpToLong24 相同, 但解析失败时返回空串 (与原版抛异常的兼容点不同, 这里选择宽容)
func IpToLong24NoError(strIP string) string {
	s, err := IpToLong24(strIP, false)
	if err != nil {
		return ""
	}
	return s
}

// IpToLong24 对应 ipToLong24(strIp, separateAddress)
func IpToLong24(strIP string, separateAddress bool) (string, error) {
	if isIPv4Address(strIP) {
		return "[IPV4]-" + ipv4ToLongs(strIP, separateAddress), nil
	}
	if isIPv6Address(strIP) {
		return "[IPV6]-" + ipv6ToLongs(ipv6Format(strIP), separateAddress), nil
	}
	return "", ErrIPUnknow
}

// ipv4ToLongs 对应 ipv4ToLongs
func ipv4ToLongs(strIP string, separateAddress bool) string {
	if strIP == "0" {
		return strIP
	}
	parts := strings.Split(strIP, ".")
	var v [4]int64
	for i := 0; i < 4 && i < len(parts); i++ {
		n, _ := strconv.ParseInt(parts[i], 10, 64)
		v[i] = n
	}
	result := (v[0] << 24) + (v[1] << 16) + (v[2] << 8)
	if separateAddress {
		result += v[3]
	}
	return strconv.FormatInt(result, 10)
}

// ipv6ToLongs 对应 ipv6ToLongs
func ipv6ToLongs(ipv6 string, separateAddress bool) string {
	raw := strings.Split(ipv6, ":")
	ipSlices := make([]string, 0, len(raw))
	for _, s := range raw {
		if s == "" {
			continue
		}
		ipSlices = append(ipSlices, s)
	}
	var arr [2]int64
	for i := 0; i <= 7 && i < len(ipSlices); i++ {
		num, _ := strconv.ParseInt(ipSlices[i], 16, 64)
		if separateAddress {
			num = 0
		}
		right := num << (16 * i)
		length := i >> 2
		arr[length] |= right
	}
	return strconv.FormatInt(arr[0], 10) + "@" + strconv.FormatInt(arr[1], 10)
}

// isIPv4Address 对应 isIPv4Address
func isIPv4Address(name string) bool {
	m := ipv4Pattern.FindStringSubmatch(name)
	if m == nil || len(m) != 5 {
		return false
	}
	for i := 1; i <= 4; i++ {
		seg := m[i]
		n, err := strconv.Atoi(seg)
		if err != nil || n > ipv4MaxOctetValue {
			return false
		}
		if len(seg) > 1 && strings.HasPrefix(seg, "0") {
			return false
		}
	}
	return true
}

// isIPv6Address 对应 isIPv6Address
func isIPv6Address(inet6Address string) bool {
	containsCompressedZeroes := strings.Contains(inet6Address, "::")
	if containsCompressedZeroes && strings.Index(inet6Address, "::") != strings.LastIndex(inet6Address, "::") {
		return false
	}
	if (strings.HasPrefix(inet6Address, ":") && !strings.HasPrefix(inet6Address, "::")) ||
		(strings.HasSuffix(inet6Address, ":") && !strings.HasSuffix(inet6Address, "::")) {
		return false
	}
	octets := splitDropEmpty(inet6Address, ":")
	if containsCompressedZeroes {
		octetList := append([]string{}, octets...)
		if strings.HasSuffix(inet6Address, "::") {
			octetList = append(octetList, "")
		} else if strings.HasPrefix(inet6Address, "::") && len(octetList) > 0 {
			octetList = octetList[1:]
		}
		octets = octetList
	}
	if len(octets) > ipv6MaxHexGroups {
		return false
	}
	validOctets := 0
	emptyOctets := 0
	for index, octet := range octets {
		if octet == "" {
			emptyOctets++
			if emptyOctets > 1 {
				return false
			}
		} else {
			emptyOctets = 0
			if index == len(octets)-1 && strings.Contains(octet, ".") {
				if !isIPv4Address(octet) {
					return false
				}
				validOctets += 2
				continue
			}
			if len(octet) > ipv6MaxHexDigitsPerGroup {
				return false
			}
			octetInt, err := strconv.ParseInt(octet, base16, 32)
			if err != nil || octetInt < 0 || octetInt > maxUnsignedShort {
				return false
			}
		}
		validOctets++
	}
	return validOctets <= ipv6MaxHexGroups && (validOctets >= ipv6MaxHexGroups || containsCompressedZeroes)
}

// ipv6Format 对应 ipv6Format
func ipv6Format(ipv6 string) string {
	ipmat := strings.Replace(ipv6, "::", "&", 1)
	ipv6No0 := ipv6
	index := strings.Index(ipmat, "&")

	if strings.Contains(ipmat, "&") {
		n := len(ipmat) - len(strings.ReplaceAll(ipmat, ":", ""))
		if index == 0 || index == len(ipmat)-1 {
			i := 8 - n
			var str strings.Builder
			bj := ""
			if index == 0 {
				bj = "0:"
			} else {
				bj = ":0"
			}
			for j := 0; j < i; j++ {
				str.WriteString(bj)
			}
			ipv6No0 = strings.Replace(ipmat, "&", str.String(), 1)
		} else {
			split := splitDropEmpty(ipmat, "&")
			i := 8 - (n + 2)
			var str strings.Builder
			for j := 0; j < i; j++ {
				str.WriteString(":0")
			}
			str.WriteString(":")
			left := ""
			right := ""
			if len(split) > 0 {
				left = split[0]
			}
			if len(split) > 1 {
				right = split[1]
			}
			ipv6No0 = left + str.String() + right
		}
	}
	split := splitDropEmpty(ipv6No0, ":")
	var ipv6s strings.Builder
	for i, s := range split {
		switch len(s) {
		case 1:
			ipv6s.WriteString("000" + s)
		case 2:
			ipv6s.WriteString("00" + s)
		case 3:
			ipv6s.WriteString("0" + s)
		case 4:
			ipv6s.WriteString(s)
		}
		if i != len(split)-1 {
			ipv6s.WriteString(":")
		}
	}
	return ipv6s.String()
}

func splitDropEmpty(s, sep string) []string {
	raw := strings.Split(s, sep)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	return out
}
