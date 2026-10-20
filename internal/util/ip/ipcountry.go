package ip

// Q7=C: 暂不实现 IP 归属地查询, 返回空占位以保持对外字段存在.
// 原版使用 IP2Location 数据库 + 内置 ip2location.7z, 后续需要时再实现.

// GetIpCountry 对应 IPCountry.getIpCountry
func GetIpCountry(iP string) string {
	return ""
}

// GetIpCountryAll 对应 IPCountry.getIpCountryAll
// 原版格式为 "countryLong|region|city", 空值拼接结果为 "||"
func GetIpCountryAll(iP string) string {
	return "||"
}
