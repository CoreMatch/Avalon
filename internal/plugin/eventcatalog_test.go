package plugin

// 事件目录一致性 (代码 <-> 文档机器区块):
// 事件真相有三份 —— 本包代码 (eventv2.go::allEventNames)、HA-Contract
// openapi (avalon-plugin-v1.yaml 的 /events/* 路径)、开发者文档
// (docs/plugins/EVENT-CATALOG.md 的 plugin-events 区块). 本测试守住
// 代码 <-> 文档这一对 (两者都在本仓, Avalon CI 内即可执行);
// 三方完整比对由 HA-Contract scripts/check-avalon-plugin-event-drift.sh
// 在双仓就绪的环境执行.
//
// 纪律 (Cascade 契约测试假绿教训): 任何一侧读不到 / 解析为空 / 条目数为 0
// 一律测试失败, 绝不跳过.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// catalogDocPath 是 EVENT-CATALOG.md 相对本包测试工作目录的位置
const catalogDocPath = "../../docs/plugins/EVENT-CATALOG.md"

// catalogFence 机器区块的语言标签 (开始标记); 结束标记是裸 ``` 围栏
const catalogFence = "```plugin-events"

// readCatalogEvents 提取文档机器区块里的事件清单.
// 文件缺失 / 区块缺失 / 区块为空都返回错误 —— 不允许静默跳过.
func readCatalogEvents() ([]string, error) {
	data, err := os.ReadFile(catalogDocPath)
	if err != nil {
		return nil, err
	}
	var events []string
	inBlock := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !inBlock {
			if trimmed == catalogFence {
				inBlock = true
			}
			continue
		}
		if trimmed == "```" {
			break
		}
		if trimmed != "" {
			events = append(events, trimmed)
		}
	}
	if !inBlock {
		return nil, os.ErrNotExist // 区块被删或语言标签被改
	}
	return events, nil
}

// TestEventCatalogDocInSync 代码事件集与文档机器区块必须双向一致
func TestEventCatalogDocInSync(t *testing.T) {
	docEvents, err := readCatalogEvents()
	if err != nil {
		t.Fatalf("EVENT-CATALOG 机器区块不可读 (%s): %v —— 不得跳过, 必须修复",
			filepath.Clean(catalogDocPath), err)
	}
	if len(docEvents) == 0 {
		t.Fatalf("EVENT-CATALOG 机器区块为空 —— 不得跳过, 必须修复")
	}

	codeSet := map[string]bool{}
	for _, name := range EventNames() {
		codeSet[name] = true
	}
	docSet := map[string]bool{}
	for _, name := range docEvents {
		if docSet[name] {
			t.Fatalf("EVENT-CATALOG 机器区块重复列出 %q", name)
		}
		docSet[name] = true
	}

	var missingInDoc, extraInDoc []string
	for name := range codeSet {
		if !docSet[name] {
			missingInDoc = append(missingInDoc, name)
		}
	}
	for name := range docSet {
		if !codeSet[name] {
			extraInDoc = append(extraInDoc, name)
		}
	}
	sort.Strings(missingInDoc)
	sort.Strings(extraInDoc)

	if len(missingInDoc) > 0 || len(extraInDoc) > 0 {
		t.Fatalf("事件目录漂移: 文档缺 %v, 文档多出 %v —— 同步 internal/plugin/eventv2.go::allEventNames 与 docs/plugins/EVENT-CATALOG.md 机器区块",
			missingInDoc, extraInDoc)
	}
}
