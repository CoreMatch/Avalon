// Package systemutils 对应 net.rwhps.server.util.SystemUtils
package systemutils

import (
	"os"
	"runtime"
)

// OSName 对应 os.name
//
// 原版读 JVM 的 os.name, 取值形如 "Windows 10" / "Linux" / "Mac OS X";
// Go 只有 runtime.GOOS ("windows" / "linux" / "darwin"), 这里按 Java 的
// 习惯写法映射, 让 version 命令与统计数据的输出贴近原版.
// 未知平台退回 GOOS 原值.
func OSName() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	case "darwin":
		return "Mac OS X"
	default:
		return runtime.GOOS
	}
}

// OSArch 对应 os.arch
//
// Java 在 Linux/Windows 上分别报 "x86_64"/"amd64", 在 arm64 上报 "aarch64";
// Go 报 "amd64"/"arm64". 按 Java on Linux 的写法映射.
func OSArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	case "386":
		return "x86"
	default:
		return runtime.GOARCH
	}
}

// AvailableProcessors 对应 Runtime.getRuntime().availableProcessors()
func AvailableProcessors() int { return runtime.NumCPU() }

// IsWindows 对应 isWindows
func IsWindows() bool { return runtime.GOOS == "windows" }

// PID 对应 ProcessHandle.current().pid()
func PID() int { return os.Getpid() }

// 以下原版成员没有移植, 原因:
//
//   - defaultEncoding (sun.stdout.encoding): Go 的 string 就是 UTF-8 字节序列,
//     没有"输出编码"这个中间层, 也没有等价物可读.
//   - javaHeap / javaTotalMemory / javaFreeMemory: JVM 堆概念. Go 侧对应
//     runtime.MemStats.HeapAlloc / HeapSys / HeapIdle, 但语义并不相同
//     (Go 的堆由 GC 动态伸缩, 不是预先划定的), 因此没有做成一一对应的函数,
//     需要的地方直接读 runtime.MemStats.
//   - javaName / javaVendor / javaVersion: 没有 JVM. 若要在 version 命令里
//     显示运行时信息, 应显示 runtime.Version().
//   - isJavaVersionAtLeast: 同上, 没有 Java 版本可比较.
