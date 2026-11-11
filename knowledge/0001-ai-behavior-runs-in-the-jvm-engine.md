> 来源：HA-Contract@bab2a05（2026-11-07），原路径 knowledge/0001-ai-behavior-runs-in-the-jvm-engine.md（契约仓知识库回迁）。

# ADR-0001: AI Behavior Runs Entirely in the JVM Engine

- **Status**: accepted

## Context

The Go server (Avalon) re-hosts a game server whose original runs on the
JVM with the game engine in-process. AI players are part of the game: pathfinding,
decision-making, combat execution, and difficulty behavior all live inside the game
engine. A question that recurs during maintenance is where AI behavior is
implemented in the Go port, and whether any Go-side AI code can be removed.

## Decision

All AI behavior (pathfinding, decision-making, combat, difficulty execution) runs
inside the JVM game engine hosted by the helper process. The Go side never
implements AI behavior. Go contributes only three kinds of adaptation code:

1. **Command forwarding** — AI creation and engine-side operations are forwarded to
   the helper as v2-ext protocol operations: `room.addai`, `room.sync`,
   `room.removeplayer`, `game.result` (the four D-9 extensions; a fifth operation
   `room.settings` covers room settings and is not AI behavior).
2. **Profile registration** — the engine creates the AI; the created profile is
   returned in the `room.addai` response (`ai` field: PlayerProfile + `position`)
   and registered into the room roster on the Go side, wrapped as the `AiPlayer`
   marker type. The engine does not emit a `player.join` event for AIs, so the
   response is the only registration path. (Upstream wraps the engine-created AI
   into its in-process `room.playerManage` synchronously inside `addAI`; the
   response field replaces that in-process step — deliberate deviation approved
   by the author 2026-11-04, DECISIONS D-9.)
3. **Room lifecycle** — the per-second check thread handles the all-AI situation:
   warn once (`aiWarn`), and once no human players remain, end the match after the
   `maxOnlyAIGameIngTime` countdown. Settlement data for AI-driven matches is
   fetched with `game.result` and broadcast (`survive.player`).

## Consequences

- AI capability is exactly as portable as the helper protocol: with a v2-ext helper,
  `addai` works end to end; without a helper, `addai` degrades with a "not portable"
  message and the server keeps running.
- AI-related Go code is small (on the order of 250–300 lines) but load-bearing:
  deleting it breaks the `addai` command, AI removal, the all-AI match lifecycle,
  and settlement broadcasting. There is no redundant AI implementation to delete.
- Difficulty handling is split: per-player difficulty of existing AIs is Go-side
  bookkeeping (`SetAiDifficulty`), while the engine default difficulty for newly
  created AIs is engine-side and answered "not portable" by the `ai <difficulty>`
  command.
- Known gap: the `ServerCacheFlag.ai` flag that arms the all-AI countdown branch has
  no writer in Go yet. Upstream maintains it in the TEAM_LIST rewrite path; that
  maintenance has not been ported, so the countdown branch exists but is currently
  never armed.

## Evidence

All paths absolute into the Avalon checkout
(`/home/lnb/Desktop/HA/rw/Avalon`); anchors verified at commit `599b0e3`.

- v2-ext operation constants and validation:
  `internal/game/external/protocol.go::OpRoomAddAI`
  (`OpRoomAddAI = "room.addai"`),
  `internal/game/external/protocol.go::OpRoomSync`
  (`room.sync`),
  `internal/game/external/protocol.go::OpRoomRemovePlayer`
  (`room.removeplayer`),
  `internal/game/external/protocol.go::OpGameResult`
  (`game.result`); AI response field
  `internal/game/external/protocol.go::AI`, result
  payload `internal/game/external/protocol.go::GameResult`.
- Backend methods forwarding to the helper:
  `internal/game/external/ext.go::AddAI`,
  `internal/game/external/ext.go::SyncAllPlayer`,
  `internal/game/external/ext.go::RemovePlayer`,
  `internal/game/external/ext.go::GameResult`.
- Registration chain:
  `internal/game/manage/playermanage.go::AddAI`,
  `internal/game/room/serverroom.go::AddAI`
  (wraps the engine profile).
- `AiPlayer` marker type:
  `internal/game/player/aiplayer.go::AiPlayer`
  (struct), `internal/game/player/aiplayer.go::IsAi`
  (`IsAi() bool { return true }`); base `Player.IsAi()` returns false at
  `internal/game/player/player.go::IsAi`;
  remove-on-kick hook
  `internal/game/player/player.go::RemoveOnKick`.
- Commands:
  `internal/command/clientcommands.go::addai`
  (`addai`, gated by `EnableAI`, calls `PlayerManage.AddAI`),
  `internal/command/clientcommands.go::PlayerPositionAI`
  (`ai <difficulty>`).
- Lifecycle:
  `internal/data/temp/servercacheflag.go::ai` and
  `internal/data/temp/servercacheflag.go::aiWarn`
  (`ai`/`aiWarn` flags),
  `internal/game/manage/callmanage.go::MaxOnlyAIGameIngTime`
  (all-AI countdown branch),
  `internal/game/manage/callmanage.go::SetGameOverData`
  (settlement via `game.result` → `survive.player`). Flag writer gap: `SetAi`
  (`internal/data/temp/servercacheflag.go::SetAi`)
  has no callers in non-test code.
- Protocol entries: `docs/HELPER-PROTOCOL.md` —
  v2-ext section
  `docs/HELPER-PROTOCOL.md (field: 扩展操作（protocol v2-ext)`,
  operation table
  `docs/HELPER-PROTOCOL.md (field: 扩展操作一览)`,
  `ai` response field
  docs/HELPER-PROTOCOL.md (field: 应答字段扩展：`ai`)
  ("the engine does not trigger a player.join event for AIs"), Go-side consumers
  `docs/HELPER-PROTOCOL.md (field: Go 侧消费点)`.
- Audit conclusion (full sweep of AI/bot-related Go code): no AI
  behavior implementation exists on the Go side; all AI logic executes in the helper
  JVM engine; all Go AI code is forwarding/registration/lifecycle adaptation.
