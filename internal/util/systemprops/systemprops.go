// Package systemprops 对应 Java System.getProperties() 的简化实现
package systemprops

import (
	"os"
	"strings"
	"sync"
)

var (
	mu    sync.RWMutex
	props = map[string]string{}
)

// Init 解析命令行参数中的 -Dkey=value 形式
func Init(args []string) {
	mu.Lock()
	defer mu.Unlock()
	for _, a := range args {
		if strings.HasPrefix(a, "-D") {
			kv := a[2:]
			if idx := strings.Index(kv, "="); idx >= 0 {
				props[kv[:idx]] = kv[idx+1:]
			} else {
				props[kv] = ""
			}
		}
	}
}

// Get 获取属性
func Get(key string) (string, bool) {
	mu.RLock()
	defer mu.RUnlock()
	v, ok := props[key]
	return v, ok
}

// Set 设置属性
func Set(key, value string) {
	mu.Lock()
	defer mu.Unlock()
	props[key] = value
}

// GetOrEnv 先查系统属性, 再查环境变量
func GetOrEnv(key string) (string, bool) {
	if v, ok := Get(key); ok {
		return v, true
	}
	return os.LookupEnv(key)
}
