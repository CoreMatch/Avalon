> 来源：HA-Contract@bab2a05（2026-11-07），原路径 knowledge/0004-simulation-state-ownership.md（契约仓知识库回迁）。

# ADR-0004: Simulation State Ownership

- **Status**: accepted

## Context

Two processes hold state about the same match: the JVM engine (inside the helper)
and the Go server. Without a clear ownership rule, both sides drift — the Go side
starts caching what the engine knows better, or the engine's results get re-derived
from Go-side bookkeeping.

## Decision

**The engine is authoritative for simulation state.** Units, positions, and the
progress/outcome of the battle exist only in the engine. The Go side keeps just
what the protocol and server behavior need, in three distinct roles:

1. **Mirrored from the wire** — extracted from packets in transit, never computed:
   the TEAM_LIST packet (ptype 115) is parsed on the forwarding path to backfill
   the connection's own `site` / `team` / `name` into the player relay.
2. **Registered from engine messages** — player profiles arrive via `player.join` /
   `player.leave` / `player.update` events; AI profiles arrive in the `room.addai`
   response (`id`/`name`/`team`/`position`). The room roster (`PlayerManage`)
   stores these registrations, including per-player bookkeeping such as
   `aiDifficulty`.
3. **Go-side bookkeeping** — fields the server itself owns: room lifecycle state
   (`IsStartGame`, `StartTime`/`EndTime`, maps, `ReplayFileName`, `Sync`,
   `MuteAll`), one-shot lifecycle flags (`ServerCacheFlag`: settlement announced,
   forced-close warned, one-player warned, all-AI state), and the settlement
   payload staged for the game-over event.

Live facts about the match are read from the engine on demand, not cached in Go:
`Running()` and `PlayerCount()` delegate to the simulation backend.

## Consequences

- Settlement is engine-produced end to end: the check thread pulls `game.result`
  (the upstream `getGameOverData()` shape: `gameTime`, `allPlayerList`,
  `winPlayerList`, `mapName`, per-player six-stat `playerData`, `replayName`),
  stages it on the room, and broadcasts the winners (`survive.player`). When the
  engine reports no outcome yet, Go skips the broadcast — it never invents results.
- Anything that needs real-time simulation detail (unit positions, battle progress)
  must come from engine packets or operations; adding Go-side caches of such state
  violates this rule and needs an explicit decision.
- The one-shot flags are Go's own invention surface: they must be reset per match
  (they are, in the room's data cleanup), and flags whose upstream writer lives in
  the TEAM_LIST rewrite path (the all-AI flag) stay inert until that writer is
  ported.

## Evidence

All paths absolute into the Avalon checkout
(`/home/lnb/Desktop/HA/Avalon`); anchors verified at commit `599b0e3`.

- Engine authority and live reads:
  `/home/lnb/Desktop/HA/Avalon/internal/game/seam.go::Simulation` (`Simulation`
  interface — Start/Stop/Running/PlayerCount belong to the backend),
  `/home/lnb/Desktop/HA/Avalon/internal/game/seam.go::PlayerCount` and
  `/home/lnb/Desktop/HA/Avalon/internal/game/seam.go::Running`
  (`GameSession.PlayerCount`/`Running` delegate to the simulation).
- TEAM_LIST mirroring:
  `/home/lnb/Desktop/HA/Avalon/internal/io/packettype/packettype.go::TEAM_LIST`
  (`TEAM_LIST = 115`);
  `/home/lnb/Desktop/HA/Avalon/internal/net/netconnectprotocol/realize/gameversionrelay.go::SendPacketExtractInformation`
  (extraction on the forwarding path, `TEAM_LIST` case inside);
  `/home/lnb/Desktop/HA/Avalon/internal/net/netconnectprotocol/universalanalysis.go::GetPacketTeamData`
  (`GetPacketTeamData` backfills site/team/name; note the upstream setter quirk
  where `team` ends up equal to `site`).
- Registration:
  `/home/lnb/Desktop/HA/Avalon/docs/HELPER-PROTOCOL.md (field: PlayerProfile)`
  (player events and `PlayerProfile` shape),
  /home/lnb/Desktop/HA/Avalon/docs/HELPER-PROTOCOL.md (field: 应答字段扩展：`ai`)
  (AI profile in the `room.addai` response);
  `/home/lnb/Desktop/HA/Avalon/internal/game/seam.go::AIProfile` (`AIProfile`);
  roster registration chain
  `/home/lnb/Desktop/HA/Avalon/internal/game/manage/playermanage.go::AddAI`,
  `/home/lnb/Desktop/HA/Avalon/internal/game/room/serverroom.go::AddAI`.
- Go-side bookkeeping:
  `/home/lnb/Desktop/HA/Avalon/internal/game/seam.go::GameSession`
  (`GameSession` fields),
  `/home/lnb/Desktop/HA/Avalon/internal/data/temp/servercacheflag.go::ServerCacheFlag`
  (one-shot flags incl. `ai`),
  `/home/lnb/Desktop/HA/Avalon/internal/game/player/player.go::SetAiDifficulty`
  (`SetAiDifficulty`).
- Settlement flow:
  `/home/lnb/Desktop/HA/Avalon/internal/game/manage/callmanage.go::SetGameOverData`
  (`game.result` → `SetGameOverData` → `survive.player` broadcast; no outcome →
  skip),
  `/home/lnb/Desktop/HA/Avalon/internal/game/external/protocol.go::GameResult`
  (`GameResult` payload); result field shapes in
  /home/lnb/Desktop/HA/Avalon/docs/HELPER-PROTOCOL.md (field: 应答字段扩展：`result`).
