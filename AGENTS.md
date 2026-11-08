# AGENTS.md —— 文档分工

本仓两处文档区分工明确，写内容或挪内容前先按此判定归属。

## docs/ —— 使用与运维

只写**现状**：怎么部署、怎么配置、怎么操作、怎么开发对接、出错怎么办。
读者是部署者、运维与插件开发者。不写设计史（"原本打算…后来改成…"）。

- `OPERATIONS.md`：配置面 / 大厅与路由 / 地图分发 / 插件三档 / 故障对照
- `DISTRIBUTION.md`：分发形态、启动与升级
- `HELPER-PROTOCOL.md`：helper 线协议（实现权威 `internal/game/external/protocol.go`）
- `plugins/`：插件开发者文档与模板
- `BASELINE.md`：回归对照基线

## knowledge/ —— 决策与历史

只写**为什么**：为什么这么定、当时排除了什么、后来怎么变的。ADR 风格，
编号条目，英文。不写操作手册（怎么配、怎么开的步骤放 docs/）。

## 重叠处理规则

1. 同一件事只在一侧权威记录；另一侧删掉重复内容，留一句指向。
2. 决策只在 knowledge 而 docs 读者需要结论：docs 写结论 + 指向 knowledge，
   不整段搬。
3. 挪删前确认不是唯一记录；是唯一记录的，先挪到归属侧再删。

## 写作纪律

- 代码引用用 `文件::符号` 锚点，不写行号；跨仓引用行标 [xref]。
- knowledge 里指向本仓文件的锚点改动后必须跑校验（见下）。
- 修改 `knowledge/*.md` 后在仓库根目录跑：
  `bash /home/lnb/Desktop/HA/HA-Contract/scripts/check-knowledge-anchors.sh`。
