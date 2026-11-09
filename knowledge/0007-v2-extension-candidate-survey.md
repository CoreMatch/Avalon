# ADR-0007: v2 Extension Surface Survey — Candidate Catalog

- **Status**: proposed (design input for v2 scoping; not a decision record)
- **Builds on**: ADR-0006 (v1 extension points) and DECISIONS D-25
- **Superseded in part**: what v2 actually implemented (and what it
  explicitly did not, including N-column items deferred by the
  internal/plugin-only scope limit) is recorded in
  `knowledge/0010-plugin-v2-implementation-status.md`. This entry remains
  the candidate catalog and the basis for future rounds.

## Context

v1 shipped two out-of-process extension points: `room.create` (synchronous,
vetoable) and `game.over` (asynchronous, non-vetoable). This entry surveys
every remaining candidate extension surface against the **actual** code paths,
records whether each payload is obtainable, and sorts candidates into three
categories. It is input for choosing the v2 scope; nothing here is approved
work.

### Ground rules

1. **Categories**
   - **N — doable now**: the signal/seam already exists in Avalon's Go code
     with an obtainable payload; adding the extension point is pure
     incremental wiring of the same shape as v1 (no structural change to
     existing semantics).
   - **A — needs Avalon changes**: possible, but requires new Avalon
     infrastructure first (a new seam, a new channel, or emit points that do
     not exist yet).
   - **X — not doable**: the signal lives in the closed engine
     (rusted-warfare.jar inside the helper JVM) with no protocol channel;
     building it anyway would produce a fake interface. Touching the helper
     is out of scope by constraint.
2. **Failure semantics**: every candidate must fit one of v1's two buckets —
   **sync/vetoable** (bounded timeout, fail-open on error/timeout, reason
   required for a veto) or **async/non-vetoable** (queued, at-least-once,
   eventId dedup). Candidates that are inbound invocations (actions,
   queries) are noted as such rather than forced into an event bucket.
3. **Payload honesty**: fields the code cannot produce are written as
   "not obtainable", never designed as empty placeholders.

## 1. Player enter / leave

### 1.1 `connection.new` — connection-level admission — N, sync/vetoable

- Hook path: `internal/net/decoder.go` already calls
  `internal/net/core/hooks.go::NetConnectNewEventHook` and disconnects the
  connection when it returns true. Current sole owner: the IP/24 ban check
  in `internal/app/eventglobal.go::registerEventGlobal`.
- Payload: `ConnectionAgreement` — IP (including the /24 key), protocol
  type. Player identity (name/UUID) is **not obtainable** at this stage —
  registration has not happened.
- Notes: rejection here is a bare disconnect; **no readable reason can be
  delivered** (no kick channel exists before registration). If the reason
  must reach the player, use 1.2 instead. The hook is a single-owner
  function today; plugin admission must compose with the ban check, not
  replace it.

### 1.2 `player.join` — admission after registration (veto = kick with reason) — N, sync/vetoable

- Hook path: `internal/net/gameserver/registry.go::handleHelperEvent` fires
  `internal/game/event/events.go::PlayerJoinEvent` right after
  `AddAbstractPlayer`. The room's default listeners
  (`internal/game/room/event.go::registerDefaultEvents`) already implement
  "kick-on-join with reason" for name/ban/kick-timer violations — the veto
  mechanism to reuse.
- Payload: full player profile — name, connectHexID (UUID), team,
  host/bot/spectator flags, admin flags, IP via the connection. Obtainable.
- Honesty note: the engine has already accepted the connection by this point
  (helper is not touchable), so this is "kick immediately after join with a
  readable reason", **not** "refuse the connection". The brief room glimpse
  is unavoidable.

### 1.3 `player.join` / `player.leave` — notifications — N, async

- Same fire sites: `registry.go::handleHelperEvent` (join) and
  `internal/net/gameserver/gameserver.go::Disconnect` firing
  `internal/game/event/events.go::PlayerLeaveEvent` (leave).
- Payload join: as 1.2. Payload leave: same profile fields plus `KickTime` /
  `MuteTime`. **Not obtainable**: the disconnect cause (voluntary exit vs
  kick vs network drop) — `Disconnect` carries no cause; the only hint is
  `KickTime` being set, which identifies kicks but not network drops.

### 1.4 Relay player join / leave — A, sync/vetoable (join) + async (both)

- Paths: join =
  `internal/net/netconnectprotocol/realize/gameversionrelay.go::AddRelayConnect`;
  leave = the relay disconnect path using
  `internal/game/room/relayroom.go::WithDisconnectLock`.
- Payload: join — player name, registerPlayerId (UUID), IP, site, room ID;
  obtainable. Leave — site/room; obtainable.
- Why A: relay rooms have **no event bus**; these are inline code paths.
  Emit points (and a join-admission call before `AddRelayConnect`, rejection
  delivered via the same RELAY_117 channel as v1 relay room.create) must be
  added.

## 2. Kick / ban / mute

### 2.1 `player.kicked` — N, async

- Choke point: **every** kick path ends in
  `internal/game/player/player.go::KickPlayer` — console kick
  (`internal/command/servercommands.go::registerServerCommands`), client
  kick/kickx (`internal/command/clientcommands.go::RegisterClientCommands`),
  relay kickx (`internal/command/relay/relayclientcommands.go`), Cascade
  gateway kick (`internal/net/gateway/rooms.go::kickPlayer`), ban-triggered
  kicks, and join-validation kicks.
- Payload: player profile + kick text + ban duration; obtainable at the
  choke point.
- Notes: plugin-initiated kicks (section 6) would also flow through here —
  the event doubles as the audit trail. AI removal uses a different path
  (`ServerRoom.RemovePlayer`) and would need its own note in the payload if
  included.

### 2.2 `player.banned` / `player.unbanned` — N, async

- Hook path: `internal/game/event/events.go::PlayerBanEvent`,
  `PlayerUnBanEvent`, `PlayerIpBanEvent` are already fired by console ban
  commands, the client admin ban command (D-10 corrected it to fire against
  the target), and the Cascade gateway ban
  (`internal/net/gateway/rooms.go::banPlayer`).
- Payload: player profile; obtainable. The ban list itself lives in
  `internal/data/global/administration.go` (BanUUID/BanIP/BanIP24 and
  UnBan*/ClearBanAll).

### 2.3 `player.muted` — A, async

- Path: the console `mute` command in
  `internal/command/servercommands.go::registerServerCommands` writes
  `Player.MuteTime`; enforcement is the chat filter registered in
  `internal/app/eventglobal.go::registerEventGlobal`. No event exists.
- Payload: player + mute duration; obtainable.
- Why A: an emit point must be added (small, but new).

## 3. Room state changes

### 3.1 `game.start` notification — N, async

- Hook path: the client `start` command in
  `internal/command/clientcommands.go::RegisterClientCommands` fires
  `internal/game/event/events.go::ServerGameStartEvent` after
  `room.StartGame()` succeeds.
- Payload: the event struct is empty today, but everything worth sending is
  obtainable at the fire site from room state (map, player list, settings).
  Enrichment is wiring, not new data.

### 3.2 `game.start` admission — N, sync/vetoable

- Hook path: same `start` command, inserted before the `room.StartGame()`
  call. **All match starts converge here**: admin `/start`, and the
  auto-start countdown (`internal/game/room/event.go::registerDefaultEvents`
  drives `ClientHandler` with `/start`). The console `start` command
  (`internal/command/start.go::registerStartGame`) is *server* startup
  (port opening), not match start — deliberately out of scope for this
  candidate.
- Payload: room snapshot + player list; obtainable.
- Veto presentation: system message to the admin who attempted the start
  (same channel the command already uses for errors).

### 3.3 `game.pause` — X

- Rusted Warfare multiplayer has **no pause concept**. The only "pause" in
  the codebase is the internal task scheduler
  (`internal/core/thread` pausing the CallTeamTask during a match) —
  bookkeeping, not game state. Shipping a pause event/hook would be a fake
  interface.

### 3.4 `game.end` — already v1 (`game.over`, including forced ends with `result=null`).

### 3.5 Room settings changes — A, async

- Paths: Cascade facade `internal/net/gateway/rooms.go::applySettings`,
  client setting commands in
  `internal/command/clientcommands.go::RegisterClientCommands` (engine-side
  fields go through v2-ext `room.settings`), map name writes, `allmute`
  toggle.
- Payload: changed fields; obtainable per path, but there is **no unified
  representation** today.
- Why A: needs a unified emit point across several call sites.

### 3.6 Relay room lifecycle — A, async

- Close: `internal/game/room/relayroom.go::RemoveRoom` (host-left /
  last-player-left call sites in gameversionrelay.go, plus the TTL cleaner
  `relayroom.go::CleanRoom`). Start flag:
  `internal/game/room/relayroom.go::SetIsStartGame`.
- Payload: room ID, size, admin, close cause (TTL vs explicit); obtainable
  per call site.
- Why A: no events exist on relay paths (same reason as 1.4).

## 4. In-game player behavior signals

Avalon parses the client game-command packet stream
(`internal/net/gameserver/gameserver.go::ReceiveCommand`), so a narrow but
**real** slice of in-game behavior is visible. Everything else is engine
interior.

### 4.1 `unit.operation` — N, async by default; sync/vetoable opt-in

- Hook path: `ReceiveCommand` fires
  `internal/game/event/events.go::PlayerOperationUnitEvent` (currently zero
  listeners — the signal is live but unused).
- Payload: player, action kind (`enums.GameCommandActions`), unit name,
  x/y — obtainable.
- Veto: already implemented — a listener clearing `ResultStatus` stops the
  packet before it reaches the engine.
- Risk: **high frequency** during battle (every unit order). Must be
  per-plugin opt-in with its own backpressure budget; veto mode needs a
  tighter timeout than room.create because it blocks live gameplay.

### 4.2 `factory.build` — N, async by default; sync/vetoable opt-in

- Same `ReceiveCommand` path, fires
  `internal/game/event/events.go::PlayerOperationFactoryBuildUnitEvent`.
- Payload: player, actionID; obtainable. Same veto mechanism and frequency
  caveats as 4.1.

### 4.3 `chat` — notification N (async); veto A

- Hook path: `internal/net/gameserver/gameserver.go::ReceiveChat` fires
  `internal/game/event/events.go::PlayerChatEvent` after the
  length/mute-all/filter checks pass.
- Payload: player + filtered message text; obtainable.
- Notes: messages from muted players are filtered **before** the event and
  never reach plugins. Veto would need Avalon changes: the event fires after
  the checks and carries no packet handle, so the STOPPED mechanism is not
  exposed to listeners today.

### 4.4 Map pings (summon/nuke/clone markers) — A, async

- Path: the `c_6_` ActionID branch inside `ReceiveCommand`, consumed by a
  per-player one-shot callback; no event exists. Low value; listed for
  completeness.

### 4.5 Kills / damage / economy / positions / survive state — X

- These live in the closed engine. The Go-side mirrors are dead:
  `internal/game/player/playerdata.go::Data` states explicitly that the six
  stat fields (unitsKilled … experimentalsLost) are never carried by the
  helper protocol and stay zero; `Survive` is never written anywhere in the
  codebase. The **only** real combat stats are the settlement snapshot in
  `GameOverData.PlayerData` at game end — already delivered by v1
  `game.over`. Extending the helper protocol to stream stats is touching the
  helper, which is out of scope; emitting always-zero fields would be a fake
  interface.

## 5. Ops actions

### 5.1 `ops.action` notifications (audit) — A, async

- Paths are scattered: console commands
  (`internal/command/servercommands.go::registerServerCommands`), client
  admin commands (`internal/command/clientcommands.go::RegisterClientCommands`),
  relay admin commands (`internal/command/relay/relayclientcommands.go`),
  Cascade facade (`internal/net/gateway/rooms.go::kickPlayer`,
  `internal/net/gateway/rooms.go::banPlayer`).
- Payload: actor (console / cascade / player-admin), action, target;
  obtainable per call site, no unified representation.
- Two routes: (a) emit at per-action choke points — kick via
  `player.go::KickPlayer` (2.1), ban already evented (2.2) — incremental;
  (b) a generic command pre/post hook —
  `internal/util/game/command/command.go::HandleMessageWith` has **no
  intercept seam** today, so (b) is a structural change. Recommend (a).

### 5.2 `ops.action` admission (veto an ops command) — A, sync/vetoable

- Why A: needs the new intercept seam in the command handler (same seam as
  5.1(b)).
- Boundary: veto-only — a plugin may refuse an action with a reason; it may
  **not** rewrite arguments or substitute execution. Fail-open on
  timeout/error, aligned with v1.

### 5.3 Cross-room ops — A

- Server mode is single-room (ADR-0003), so cross-room surface exists only
  in relay mode: broadcast to all relay rooms
  (`internal/game/room/relayroom.go::SendAllMsg`), TTL cleaning
  (`relayroom.go::CleanRoom`), relay console commands
  (`internal/command/relay/relaycommands.go`). No events on these paths;
  notifications async; veto here has little value.

## 6. Plugin intervention on players (reverse actions)

Primitives that already exist in Avalon (none require the helper):

| Action | Existing primitive |
|---|---|
| System message | `internal/game/player/player.go::SendSystemMessage` |
| Chat in a player's name | `internal/game/player/player.go::SendMessage` |
| Popup with reply callback | `internal/game/player/player.go::SendPopUps` |
| Kick (+rejoin window) | `internal/game/player/player.go::KickPlayer` |
| Mute | write `Player.MuteTime` (enforced by the chat filter) |
| Ban / unban | `internal/data/global/administration.go::BanUUID` and siblings |
| Jump to another server | `internal/game/player/player.go::PlayerJumpsToAnotherServer` (domain-format validated) |
| Room broadcast / kick-all | `internal/game/manage/callmanage.go::SendSystemMessage`, `internal/game/manage/callmanage.go::KillAllPlayer` |

- Category: **A** — v1 is push-only; intervention needs a plugin→Avalon
  action channel (new surface).
- Semantics: these are inbound invocations, not events. Aligned with v1
  they fall on the async side: executed with an ack, **at-most-once
  execution** with a caller-supplied idempotency key (a retried "kick" must
  not kick twice).
- **Permission boundary (must be settled before building)**:
  - default-deny: every action type requires an explicit grant in the
    plugin's config entry; no grant = refused and counted;
  - no arbitrary console-command execution — that is full server control and
    violates the ops boundary (the existing web-panel
    `run/ServerCommand` route behind webToken must not become the plugin
    channel);
  - scope limited to this Avalon instance's rooms/players;
  - audit log line per intervention (plugin, action, target);
  - per-plugin rate limits;
  - ban/kick/jump are high-risk: separate grants; `jump` deserves extra
    scrutiny — the primitive validates domain format, not trustworthiness.

## 7. Reverse channel: plugin → Avalon queries

- Feasibility: **A** (no such surface in v1; a new Avalon HTTP endpoint).
- Data obtainable today (all in Avalon memory, no helper involvement):
  - room snapshot: `internal/game/seam.go::GameSession.Snapshot` and the
    gateway summary builder (`internal/net/gateway/rooms.go::buildSummary`);
  - player list: `PlayerManage` (name/UUID/team/slot/admin/host/bot);
  - relay room table: `internal/game/room/relayroom.go::RelayAllIP`,
    `AllSize`, `RoomAllSize`;
  - plugin hub status: `internal/plugin/hub.go::Status`.
- **Exposure boundary**:
  - read-only in v2; mutation stays out of the query channel (section 6 is
    the only action path, separately granted);
  - minimal fields: player IPs are ops-sensitive and stay out unless an
    explicit ops-scope grant; no config secrets of any kind (webToken,
    plugin secrets, Cascade keys);
  - auth reuses the v1 HMAC scheme reversed: the plugin signs the request,
    Avalon verifies and signs the response.
- Mounting choice (standalone listener vs reusing the existing web surface
  `internal/net/http`) is a v2 design decision, out of scope here.

## Not-doable summary (X), with reasons

| Candidate | Reason |
|---|---|
| `game.pause` | no pause concept in RW multiplayer; only scheduler bookkeeping exists |
| kills/damage/economy/positions/survive streams | closed engine; Go mirrors are dead fields (never populated); helper protocol extension forbidden |
| refuse-connection-with-reason | engine accepts connections before Avalon sees a player; only bare-disconnect (1.1) or kick-after-register (1.2) are real |
| mod-level events | mods are parsed by the closed engine; Avalon only places files |
| anything requiring helper changes | out of scope by constraint |

## Failure-semantics assignment

| Candidate | Bucket | On plugin timeout/error |
|---|---|---|
| 1.1 connection.new admission | sync/vetoable | fail-open (accept connection) |
| 1.2 player.join admission | sync/vetoable | fail-open (player stays) |
| 1.3 join/leave notifications | async | retry/outbox per v1 |
| 1.4 relay join admission / join-leave notifications | sync/vetoable + async | fail-open / retry |
| 2.1 kicked, 2.2 banned, 2.3 muted | async | retry/outbox per v1 |
| 3.1 game.start notification | async | retry/outbox per v1 |
| 3.2 game.start admission | sync/vetoable | fail-open (start proceeds) |
| 3.5 settings change, 3.6 relay lifecycle | async | retry/outbox per v1 |
| 4.1/4.2 unit.operation, factory.build | async default; sync/vetoable opt-in | fail-open (operation passes) |
| 4.3 chat notification | async | retry/outbox per v1 |
| 5.2 ops admission | sync/vetoable | fail-open (action proceeds) |
| 6 intervention actions | inbound invocation | at-most-once + idempotency key |
| 7 queries | inbound invocation | plain request/response, no retries |

All sync items inherit v1's absolute timeout cap and WARN+counter
visibility; no sync item may block the match flow beyond its cap.

## Suggested v2 slice (input, not decision)

- **P0 — category N, pure increment**: player.join (notification +
  admission), player.leave, game.start (notification + admission),
  player.kicked, banned/unbanned notifications, unit.operation /
  factory.build (async-first, veto strictly opt-in).
- **P1 — category A, small emits**: muted notification, relay join/leave
  and room-close events, settings-change notification.
- **P2 — category A, new surfaces**: read-only query channel, intervention
  action API with default-deny grants.
- **P3 — structural, review carefully**: generic command intercept for ops
  admission/audit.

## Evidence

- Join/leave/chat/command fire sites:
  `/home/lnb/Desktop/HA/rw/Avalon/internal/net/gameserver/registry.go::handleHelperEvent`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/net/gameserver/gameserver.go::Disconnect`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/net/gameserver/gameserver.go::ReceiveChat`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/net/gameserver/gameserver.go::ReceiveCommand`
- Event types:
  `/home/lnb/Desktop/HA/rw/Avalon/internal/game/event/events.go::PlayerJoinEvent`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/game/event/events.go::PlayerLeaveEvent`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/game/event/events.go::PlayerChatEvent`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/game/event/events.go::PlayerOperationUnitEvent`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/game/event/events.go::PlayerOperationFactoryBuildUnitEvent`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/game/event/events.go::ServerGameStartEvent`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/game/event/events.go::PlayerBanEvent`
- Connection-level hooks:
  `/home/lnb/Desktop/HA/rw/Avalon/internal/net/core/hooks.go::NetConnectNewEventHook`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/net/core/hooks.go::NetConnectCloseEventHook`
- Default room listeners (kick-on-join precedent, auto-start convergence):
  `/home/lnb/Desktop/HA/rw/Avalon/internal/game/room/event.go::registerDefaultEvents`
- Match start paths:
  `/home/lnb/Desktop/HA/rw/Avalon/internal/command/clientcommands.go::RegisterClientCommands`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/command/start.go::registerStartGame`
- Intervention primitives:
  `/home/lnb/Desktop/HA/rw/Avalon/internal/game/player/player.go::KickPlayer`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/game/player/player.go::SendSystemMessage`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/game/player/player.go::SendMessage`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/game/player/player.go::SendPopUps`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/game/player/player.go::PlayerJumpsToAnotherServer`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/data/global/administration.go::BanUUID`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/game/manage/callmanage.go::KillAllPlayer`
- Dead mirrors (X evidence):
  `/home/lnb/Desktop/HA/rw/Avalon/internal/game/player/playerdata.go::Data`
- Relay paths:
  `/home/lnb/Desktop/HA/rw/Avalon/internal/net/netconnectprotocol/realize/gameversionrelay.go::AddRelayConnect`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/game/room/relayroom.go::RemoveRoom`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/game/room/relayroom.go::CleanRoom`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/game/room/relayroom.go::SendAllMsg`
- Command handler (no intercept seam):
  `/home/lnb/Desktop/HA/rw/Avalon/internal/util/game/command/command.go::HandleMessageWith`
- Queryable snapshots:
  `/home/lnb/Desktop/HA/rw/Avalon/internal/game/seam.go::Snapshot`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/net/gateway/rooms.go::buildSummary`,
  `/home/lnb/Desktop/HA/rw/Avalon/internal/plugin/hub.go::Status`
- v1 baseline: `knowledge/0006-out-of-process-plugin-extension-points.md`
