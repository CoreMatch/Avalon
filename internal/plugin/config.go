// Package plugin 实现进程外插件扩展点 (ADR-0006 / DECISIONS D-25,
// 契约 avalon-plugin-v1 v1.0.0).
//
// 插件是独立进程/服务, 自己起 HTTP 服务; Avalon 是调用方. JVM 引擎壳不加载任何
// 第三方代码, mod 不开放扩展 —— 扩展面只在 Avalon 治理面.
//
// v1 只有两个扩展点:
//
//   - room.create: 建房前准入 (同步, 可否决, 至多一次, 超时/错误 fail-open)
//   - game.over:   对局结束追加处理 (异步, 不可否决, 至少一次, WAL 重放)
//
// 未配置插件 (无 Plugins.json 或 entries 为空) 时 hub 空转: 准入立即放行且
// 不产生任何网络 I/O, 行为与未启用时完全一致.
package plugin

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"github.com/CoreMatch/Avalon/internal/util/file"
	"github.com/CoreMatch/Avalon/internal/util/jsonutil"
)

// 默认阈值 (ADR-0006 "Sync/async semantics")
const (
	DefaultAdmissionTimeoutMs = 2500
	DefaultDeliveryTimeoutMs  = 10000
	DefaultOutboxCapacity     = 128
	DefaultMaxEventAgeHours   = 24

	// minSecretBytes 共享密钥最小长度 (16 字节, 32 个 hex 字符)
	minSecretBytes = 16
)

// Config 对应 /data/Plugins.json
type Config struct {
	// Version 配置 schema 版本, 当前必须为 1
	Version int `json:"version"`

	// AdmissionTimeoutMs room.create 单次调用与启动期 manifest 协商的绝对上限
	AdmissionTimeoutMs int `json:"admissionTimeoutMs"`
	// DeliveryTimeoutMs game.over 单次投递尝试的上限
	DeliveryTimeoutMs int `json:"deliveryTimeoutMs"`
	// OutboxCapacity 每插件 outbox 容量, 满则丢最旧
	OutboxCapacity int `json:"outboxCapacity"`
	// MaxEventAgeHours 超过该年龄的事件在投递时丢弃
	MaxEventAgeHours int `json:"maxEventAgeHours"`

	Entries []Entry `json:"entries"`
}

// Entry 单个插件的运营配置
type Entry struct {
	// Name 插件名, 唯一, 用于日志/状态/outbox 文件名
	Name string `json:"name"`
	// BaseURL 插件 HTTP 服务基址, 例如 http://127.0.0.1:9800
	BaseURL string `json:"baseUrl"`
	// Secrets 共享密钥列表 (hex); 第一个为签名用主密钥, 全部用于验签
	// (列表语义支持轮换重叠期)
	Secrets []string `json:"secrets"`
	// Enabled 是否启用; 缺省 (null) 视为 true, false 显式停用
	Enabled *bool `json:"enabled"`
}

func (e Entry) enabled() bool { return e.Enabled == nil || *e.Enabled }

// PluginsFile 对应 Config.json 一族的路径风格: /data/Plugins.json
func PluginsFile() *file.FileUtils {
	return file.GetFolder("/data").ToFile("Plugins.json")
}

// NewConfig 创建带默认值的配置 (空 entries)
func NewConfig() Config {
	return Config{
		Version:            1,
		AdmissionTimeoutMs: DefaultAdmissionTimeoutMs,
		DeliveryTimeoutMs:  DefaultDeliveryTimeoutMs,
		OutboxCapacity:     DefaultOutboxCapacity,
		MaxEventAgeHours:   DefaultMaxEventAgeHours,
	}
}

// loadConfig 读取并校验 /data/Plugins.json.
//
// 与 bean.LoadOrInit 的差异: 解析失败**不**回写默认值 —— 插件是显式配置,
// 静默重置会把运营错误藏掉 (与 Cascade 网关 "配置非法一律启动显式失败"
// 同一纪律). 文件缺失/为空才写默认值.
func loadConfig() (Config, error) {
	f := PluginsFile()
	data := f.ReadFileStringData()
	if strings.TrimSpace(data) == "" {
		cfg := NewConfig()
		f.WriteFile(jsonutil.ToPrettyJSON(cfg))
		return cfg, nil
	}

	cfg := Config{}
	if err := jsonutil.FromJSON(data, &cfg); err != nil {
		return cfg, fmt.Errorf("plugin: parse Plugins.json: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// validate 校验并补齐默认值; 任何非法项都是启动错误
func (c *Config) validate() error {
	if c.Version != 1 {
		return fmt.Errorf("plugin: unsupported Plugins.json version %d (want 1)", c.Version)
	}
	if c.AdmissionTimeoutMs <= 0 {
		c.AdmissionTimeoutMs = DefaultAdmissionTimeoutMs
	}
	if c.DeliveryTimeoutMs <= 0 {
		c.DeliveryTimeoutMs = DefaultDeliveryTimeoutMs
	}
	if c.OutboxCapacity <= 0 {
		c.OutboxCapacity = DefaultOutboxCapacity
	}
	if c.MaxEventAgeHours <= 0 {
		c.MaxEventAgeHours = DefaultMaxEventAgeHours
	}

	seen := make(map[string]bool, len(c.Entries))
	for i, e := range c.Entries {
		if strings.TrimSpace(e.Name) == "" {
			return fmt.Errorf("plugin: entry %d: empty name", i)
		}
		if seen[e.Name] {
			return fmt.Errorf("plugin: duplicate entry name %q", e.Name)
		}
		seen[e.Name] = true

		u, err := url.Parse(e.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("plugin: entry %q: baseUrl must be an absolute http(s) URL, got %q", e.Name, e.BaseURL)
		}

		if len(e.Secrets) == 0 {
			return fmt.Errorf("plugin: entry %q: at least one secret is required", e.Name)
		}
		for j, s := range e.Secrets {
			// 用标准库严格解码: 奇数长度 / 非法字符 / 空白都拒绝
			// (algorithms.DecodeHex 对奇数长度补零, 不适合密钥校验).
			raw, err := hex.DecodeString(s)
			if err != nil || len(raw) < minSecretBytes {
				return fmt.Errorf("plugin: entry %q: secret %d must be hex of at least %d bytes", e.Name, j, minSecretBytes)
			}
		}
	}
	return nil
}

// outboxFileName 把插件名净化成安全的文件名片段
func outboxFileName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "plugin"
	}
	return b.String()
}
