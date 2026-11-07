# ADR-0010: Plugin Extension Points v2 — Implementation Status

- **Status**: accepted (current-state record; supersedes the survey's open
  questions for what it covers)
- **Contract**: HA-Contract `docs/api/openapi/avalon-plugin-v1.yaml`,
  version 1.1.0 (three-tier event contract)
- **Input**: `knowledge/0007-v2-extension-candidate-survey.md` (candidate
  catalog); this entry records what v2 actually implemented and what it
  explicitly did not.

## Context

v2 ships the "doable now" (N) column of the 0007 survey under the settled
architecture: out-of-process plugins, three event tiers (observe / veto /
deliver), no JVM-side loading, no silent self-update. Scope was limited to
`internal/plugin` plus documentation/contract; packages being changed by the
concurrent mod-manifest track were not touched. That scope limit, not just
design judgment, determines several "not done" items below.

## Decision (what v2 implements)

### The three tiers

| Tier | Delivery | Failure | Events |
|---|---|---|---|
| observe | best-effort: in-memory queue only, drop-oldest on backpressure, no retry, no persistence, no replay after restart | delivery failure drops the event and counts it | player.join, player.leave, player.banned, player.unbanned, player.ipbanned, game.start, player.chat, unit.operation, factory.build (default mode) |
| veto | synchronous request/response, bounded timeout, at-most-once | timeout/error/malformed → fail-open, counted | room.create (v1), player.join, unit.operation, factory.build (granted mode) |
| deliver | WAL-backed, retried with backoff, replayed after restart, eventId dedup | unchanged from v1 | game.over |

All tiers share v1's envelope (eventId/event/eventVersion/occurredAt/server),
bidirectional HMAC signatures, manifest negotiation, per-plugin single
worker (FIFO across tiers), and WARN+counter visibility.

### Event wiring (all via the room event bus, no boot-chain changes)

Every v2 subscription is registered inside
`internal/plugin/default.go::RegisterRoomEvents` — the seam the boot chain
already calls since v1 — so no file outside `internal/plugin` was modified:

- `player.join` (observe + optional veto): listens to
  `internal/game/event/events.go::PlayerJoinEvent`, fired by
  `internal/net/gameserver/registry.go::handleHelperEvent`.
  **Veto runs in a detached goroutine**: the helper event dispatch runs on
  the single pump goroutine that also carries all request replies and game
  packet routing (`internal/game/external/backend.go::pump`), so a
  synchronous network wait there would stall every player's traffic. The
  player is already registered when the event fires, so an asynchronous
  kick is observationally identical to a synchronous one — the veto
  manifests as "kicked immediately after joining, with the plugin's reason"
  (`internal/game/player/player.go::KickPlayer`). A vetoed join emits no
  observe notification.
- `player.leave`: listens to `PlayerLeaveEvent`, fired by
  `internal/net/gameserver/gameserver.go::Disconnect`. Payload carries
  `banRemainingMs` (remaining rejoin lockout, derived from `KickTime`) and
  `inGame`. **The disconnect cause (voluntary/kicked/network drop) is not
  obtainable and is not in the payload** — no placeholder field was added.
- `player.banned` / `player.unbanned` / `player.ipbanned`: listen to the
  existing `PlayerBanEvent` / `PlayerUnBanEvent` / `PlayerIpBanEvent`
  (fired by console/client admin commands and the Cascade facade).
- `game.start`: listens to `ServerGameStartEvent` (fired by the client
  `start` command in `internal/command/clientcommands.go::RegisterClientCommands`,
  where admin `/start` and the auto-start countdown converge). Payload is a
  start-time snapshot: map, maxPlayers, startTime, and the in-room player
  roster from `PlayerManage.PlayerGroup`.
- `player.chat`: listens to `PlayerChatEvent`, fired by
  `internal/net/gameserver/gameserver.go::ReceiveChat` **after** the
  length/mute-all/filter checks — muted players' messages never reach
  plugins.
- `unit.operation` / `factory.build`: listen to
  `PlayerOperationUnitEvent` / `PlayerOperationFactoryBuildUnitEvent`, fired
  by `internal/net/gameserver/gameserver.go::ReceiveCommand` (Avalon parses
  the client game-command stream). Observe mode is always on when
  subscribed; veto mode additionally requires both gates below. A veto sets
  `ResultStatus=false`, stopping the packet before the engine; the veto
  reason goes to the log only (there is no player-facing channel mid-game).

### Veto gating (default-deny)

`room.create` keeps v1 semantics (subscription implies admission
participation). For `player.join`, `unit.operation`, and `factory.build`,
a plugin is consulted as a vetoer only when **both** hold:

1. the plugin's manifest declares `veto: true` for the event, and
2. the operator grants the event in the plugin's `vetoEvents` config list.

Either one missing → the plugin receives the event in observe mode only.
`vetoEvents` accepts only the three grantable events; `room.create` or
unknown names fail the boot (strict config discipline).

Timeouts: `player.join` vetoes use `admissionTimeoutMs` (default cap
2500 ms); `unit.operation` / `factory.build` vetoes use the new
`gameplayVetoTimeoutMs` (default cap **250 ms**) because they block the
issuing player's command stream. Both are capped further by the plugin's
self-declared manifest `timeoutMs`. All vetoes fail open on
timeout/error/malformed responses, counted and WARN-logged.

### Observe-tier delivery mechanics

Observe records share the per-plugin FIFO queue with deliver records but
are never written to the WAL (`internal/plugin/outbox.go::Outbox` persists
only `Persist` records). Consequences, all contract-stated:

- plugin down → observe events buffer in memory (bounded by
  `outboxCapacity`, drop-oldest) and are delivered when negotiation
  succeeds, unless they expire (`maxEventAgeHours`) or lose version
  intersection (dropped and counted at negotiation time);
- Avalon restart → observe events are gone (never persisted);
- delivery failure (timeout/5xx/bad signature) → dropped immediately, no
  retry; connection-level failure → plugin goes unready and the manifest
  backoff path applies, same as v1.

## Explicitly NOT implemented in v2 (and why)

From the 0007 N-column, deferred by the **scope limit** (this round may
touch only `internal/plugin` + docs/contract), not by feasibility:

- **`connection.new` admission** (0007 §1.1): needs composing
  `internal/net/core/hooks.go::NetConnectNewEventHook`, owned by
  `internal/app/eventglobal.go::registerEventGlobal` — outside scope.
- **`game.start` admission** (0007 §3.2): the veto point is before
  `room.StartGame()` inside the client `start` command
  (`internal/command/clientcommands.go`) — outside scope. The observe
  notification is implemented; the pre-start veto is not.
- **`player.kicked` audit via the KickPlayer choke point** (0007 §2.1):
  the emit belongs in `internal/game/player/player.go::KickPlayer` —
  outside scope. Partial substitute implemented: `player.leave` carries
  `banRemainingMs`, which identifies kicks/bans **with** a rejoin window;
  kicks with `timeSec=0` leave no trace in v2 payloads (documented gap,
  not a silent empty field).

Excluded by direction (unchanged from the task, aligned with 0007's X/A
judgments) — none of these have interfaces, stubs, or config surface:

- engine-interior streams: kills/damage/economy/positions/survive state
  (closed engine; Go mirrors are dead fields — see
  `internal/game/player/playerdata.go::Data`);
- ops-command interception (no intercept seam in
  `internal/util/game/command/command.go::HandleMessageWith`; building one
  is a structural change, 0007 §5.2/P3);
- plugin→player write channel (intervention actions, 0007 §6) and the
  plugin→Avalon query channel (0007 §7) — both need new Avalon HTTP
  surfaces;
- relay-side events (relay player join/leave, relay room close, 0007
  §1.4/§3.6) — need emit points in the relay paths, outside scope;
- JVM-side loading of any kind; any download/update mechanism.

## Consequences

- Zero-regression holds: with no `Plugins.json` (or empty entries) every v2
  listener fast-paths on a nil hub; with plugins configured but not granted
  veto, gameplay paths never issue synchronous calls (`HasVetoConsult`
  short-circuits on the grant table).
- The observe tier trades durability for freshness; lifecycle consumers
  needing at-least-once must rely on `game.over` (deliver) or a future
  per-event tier promotion — a contract change, deliberately not smuggled
  in now.
- High-frequency events (`player.chat`, `unit.operation`) can saturate a
  slow plugin's queue; drop-oldest plus counters is the designed loss
  behavior, visible via the `plugin` console status.

## Evidence

- Tier machinery and queue:
  `/home/lnb/Desktop/HA/Avalon/internal/plugin/outbox.go::Outbox`,
  `/home/lnb/Desktop/HA/Avalon/internal/plugin/hub.go::enqueueEvent`,
  `/home/lnb/Desktop/HA/Avalon/internal/plugin/eventv2.go::tierFor`
- Veto core and gating:
  `/home/lnb/Desktop/HA/Avalon/internal/plugin/hub.go::admitCore`,
  `/home/lnb/Desktop/HA/Avalon/internal/plugin/hub.go::HasVetoConsult`,
  `/home/lnb/Desktop/HA/Avalon/internal/plugin/hub.go::vetoConsulted`
- Config surface:
  `/home/lnb/Desktop/HA/Avalon/internal/plugin/config.go::validate`
  (gameplayVetoTimeoutMs / vetoEvents validation)
- Wiring (all v2 listeners):
  `/home/lnb/Desktop/HA/Avalon/internal/plugin/default.go::RegisterRoomEvents`
- Pump-goroutine constraint behind the detached join admission:
  `/home/lnb/Desktop/HA/Avalon/internal/game/external/backend.go::pump`
- Event fire sites consumed:
  `/home/lnb/Desktop/HA/Avalon/internal/net/gameserver/registry.go::handleHelperEvent`,
  `/home/lnb/Desktop/HA/Avalon/internal/net/gameserver/gameserver.go::ReceiveChat`,
  `/home/lnb/Desktop/HA/Avalon/internal/net/gameserver/gameserver.go::ReceiveCommand`,
  `/home/lnb/Desktop/HA/Avalon/internal/net/gameserver/gameserver.go::Disconnect`
- Contract: [xref]
  `/home/lnb/Desktop/HA/HA-Contract/docs/api/openapi/avalon-plugin-v1.yaml (field: 1.1.0)`
- Survey input: `knowledge/0007-v2-extension-candidate-survey.md`; v1
  baseline: `knowledge/0006-out-of-process-plugin-extension-points.md`
