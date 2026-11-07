package plugin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Manifest 是插件的自我描述 (契约 schemas.Manifest), 由插件在
// GET {base}/v1/manifest 提供.
type Manifest struct {
	Schema   string                    `json:"schema"`
	Protocol string                    `json:"protocol"`
	ID       string                    `json:"id"`
	Version  string                    `json:"version"`
	Events   map[string]EventCapability `json:"events"`
	// TimeoutMs 插件自报处理预算; Avalon 用自己的上限取 min 封顶
	TimeoutMs int `json:"timeoutMs"`
	// MaxConcurrency v1 保留: 实际恒为单 worker (保序), >1 按 1 处理
	MaxConcurrency int `json:"maxConcurrency"`
}

// EventCapability 单个事件的受支持版本集
type EventCapability struct {
	Versions []int `json:"versions"`
}

// supports 判断插件是否支持某事件的 v1 版本, 返回协商出的版本号
// (v1 中 Avalon 只发版本 1, 无交集返回 0)
func (m *Manifest) supports(eventName string) int {
	if m == nil {
		return 0
	}
	cap, ok := m.Events[eventName]
	if !ok {
		return 0
	}
	for _, v := range cap.Versions {
		if v == emitVersion {
			return emitVersion
		}
	}
	return 0
}

// validate 校验 manifest 结构与协议版本
func (m *Manifest) validate() error {
	if m.Schema != ManifestSchema {
		return fmt.Errorf("manifest schema %q != %q", m.Schema, ManifestSchema)
	}
	if m.Protocol != ProtocolV1 {
		return fmt.Errorf("manifest protocol %q != %q", m.Protocol, ProtocolV1)
	}
	if strings.TrimSpace(m.ID) == "" {
		return fmt.Errorf("manifest id is empty")
	}
	if strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("manifest version is empty")
	}
	return nil
}

// maxResponseBytes 应答正文读取上限 (失败隔离: 畸形大应答不得吃内存)
const maxResponseBytes = 64 * 1024

// fetchManifest 拉取并校验插件 manifest (签名双向校验).
//
// 成功时返回协商好的事件版本表 (事件名 -> 版本); 插件未声明或版本无交集的
// 事件不在表内.
func (p *Plugin) fetchManifest(ctx context.Context) (map[string]int, *Manifest, error) {
	url := strings.TrimRight(p.entry.BaseURL, "/") + "/v1/manifest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	signRequest(req, p.secrets[0], nil)

	resp, err := p.hub.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if len(body) > maxResponseBytes {
		return nil, nil, fmt.Errorf("manifest response exceeds %d bytes", maxResponseBytes)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("manifest status %d", resp.StatusCode)
	}
	if err := verifyResponse(p.secrets, resp, body); err != nil {
		return nil, nil, fmt.Errorf("manifest response: %w", err)
	}

	m := &Manifest{}
	if err := jsonUnmarshal(body, m); err != nil {
		return nil, nil, fmt.Errorf("manifest parse: %w", err)
	}
	if err := m.validate(); err != nil {
		return nil, nil, err
	}

	negotiated := make(map[string]int, 2)
	for _, name := range []string{EventRoomCreate, EventGameOver} {
		if v := m.supports(name); v > 0 {
			negotiated[name] = v
		}
	}
	return negotiated, m, nil
}
