# Avalon Knowledge Base

ADR-style memos about Avalon's design and operations (migrated back from
HA-Contract on 2026-11-07; this repository owns its own knowledge entries).

Division of labor (see root `AGENTS.md`): this directory records decisions
and history (why something was decided, what was ruled out, how it changed);
usage and operations (current state, how-to, troubleshooting) live in
`docs/`. Where they overlap, the authoritative side keeps the content and the
other side leaves a one-line pointer.

| Entry | Title | Status |
|---|---|---|
| [0001](./0001-ai-behavior-runs-in-the-jvm-engine.md) | AI Behavior Runs Entirely in the JVM Engine | Accepted |
| [0002](./0002-game-engine-as-external-dependency.md) | Game Engine as External Dependency | Accepted |
| [0003](./0003-single-room-model.md) | Single Room Model | Accepted |
| [0004](./0004-simulation-state-ownership.md) | Simulation State Ownership | Accepted |
| [0005](./0005-upstream-lineage-and-derivative-status.md) | Upstream Lineage and Derivative Status (Copycat Audit) | Accepted |
| [0006](./0006-out-of-process-plugin-extension-points.md) | Out-of-Process Plugin Extension Points (v1) | Accepted |
| [0007](./0007-v2-extension-candidate-survey.md) | v2 Extension Surface Survey (Candidate Catalog) | Proposed |
| [0008](./0008-custom-map-selection.md) | Custom Map Selection (Current State) | Accepted |
| [0009](./0009-zero-mod-lobby-and-mod-assembly.md) | Zero-Mod Lobby and Mod Assembly | Accepted |
| [0010](./0010-plugin-v2-implementation-status.md) | Plugin Extension Points v2 — Implementation Status | Accepted |
| [0011](./0011-map-distribution.md) | Map Distribution (Engine Streaming + Content Manifest) | Accepted |

Anchor checker: `bash /home/lnb/Desktop/HA/HA-Contract/scripts/check-knowledge-anchors.sh`
(run from this repository's root).
