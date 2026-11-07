# ADR-0006: Out-of-Process Plugin Extension Points (v1)

- **Status**: accepted
- **Contract**: HA-Contract `docs/api/openapi/avalon-plugin-v1.yaml`, version 1.0.0
  (registered before implementation, per the contract-first rule)

## Context

Avalon is the Go governance layer in front of the JVM engine shell
(ADR-0002). Two boundaries are already settled and are not revisited here:

- The JVM engine (Avalon-Helper) loads **no third-party code**. The upstream
  jar/GraalVM plugin machinery was deliberately removed from the helper; the
  engine stays a sealed shell (see the survey at
  `/home/lnb/Desktop/HA/PLUGIN-SYSTEM-SURVEY-20261107.md`, sections A.2/A.3).
- Mods are **not** an extension surface. Mod files are placed for the closed
  engine to parse; consistency is enforced by engine-side unit checksums.
  There is no mod plugin interface by design.

The only extension surface is therefore Avalon's governance plane: room
lifecycle, player join/leave, game end, and ops actions. Plugins are
**independent processes/services** speaking HTTP with Avalon; Avalon never
executes plugin code and plugins never link against Avalon.

v1 implements exactly two extension points; everything else is backlog:

1. `game.over` — after a match ends: async push, no veto.
2. `room.create` — before a room is created: synchronous admission, vetoable.

## Decision

### Transport

HTTP + JSON, Avalon is the **caller**, each plugin is an HTTP server.

- One plugin = one base URL + one shared-secret list, declared by the
  operator in Avalon's config (no runtime discovery, no code download).
- Avalon calls the plugin; v1 has no plugin→Avalon calls.
- Go standard library only (`net/http`, `crypto/hmac`). No framework, no
  external middleware, no new module dependencies.

### Event catalog (v1)

Two events. Names are stable identifiers used in config, manifests, and
endpoint paths.

| Event | Endpoint (plugin-side) | Sync | Vetoable | Delivery |
|---|---|---|---|---|
| `room.create` | `POST {base}/v1/events/room.create` | yes | yes | at-most-once (no retry) |
| `game.over` | `POST {base}/v1/events/game.over` | no | no | at-least-once (retry + replay) |

Supporting endpoint: `GET {base}/v1/manifest` (version negotiation, see below).

#### Common envelope fields (all events)

| Field | Type | Meaning |
|---|---|---|
| `eventId` | string | `evt_` + 26-char Crockford ULID; generated once at emit time, stable across retries and replays |
| `event` | string | event name (`room.create` / `game.over`) |
| `eventVersion` | int | schema version of this event (v1: always `1`) |
| `occurredAt` | string | RFC 3339 UTC timestamp of the emit |
| `server.serverId` | string | `ConfigServer.ServerID` |
| `server.serverName` | string | `Config.ServerName` |
| `server.coreVersion` | string | `global.ServerCoreVersion` |

#### `room.create` request fields (in addition to the envelope)

| Field | Type | Meaning |
|---|---|---|
| `room.kind` | string | `server` (boot-time singleton room), `cascade` (control-plane `POST /v1/rooms/create`), `relay` (relay-protocol room) |
| `room.code` | string | room code/id: `ServerID` for `server`; the requested code for `cascade`; the requested custom id for `relay`, empty string when the relay id is auto-generated |
| `room.maxPlayers` | int | requested max players |
| `room.isMod` | bool | relay mod-room flag (`false` for non-relay kinds) |
| `creator.name` | string | `console` for `server`; `cascade` for `cascade`; the creating player's name for `relay` |
| `creator.address` | string | source IP when known, else empty string |

#### `room.create` response fields (plugin → Avalon, HTTP 200)

| Field | Type | Meaning |
|---|---|---|
| `allowed` | bool | the decision |
| `reason` | string | mandatory when `allowed=false`; human-readable; Avalon truncates to 200 runes and strips control characters before display |

Any non-2xx status, invalid JSON, missing `allowed`, or a failed response
signature is an **error**, not a decision (see failure semantics).

#### `game.over` request fields (in addition to the envelope)

| Field | Type | Meaning |
|---|---|---|
| `room.kind` | string | always `server` in v1 (only server-mode rooms run a simulation) |
| `room.code` | string | the room id (`ServerID`) |
| `result` | object\|null | `null` when the engine produced no settlement data (e.g. forced end); otherwise the fields below |
| `result.mapName` | string | map name |
| `result.gameTime` | int | engine-reported game time (seconds) |
| `result.allPlayerList` | string[] | all player names |
| `result.winPlayerList` | string[] | winner names |
| `result.playerData` | object | player name → (stat key → int), engine-defined stat keys |
| `result.replayName` | string | replay file name |

`game.over` responses: any 2xx is an ack (body ignored, signature still
verified). The payload carries player names and engine stats only — no IPs,
no UUIDs — matching the minimal-disclosure rule already applied to the
replay files written by the room's default game-over listener.

### Hook points (real code paths)

Extension points attach where rooms are actually created and where games
actually end:

- **Boot-time singleton room** (`server` kind): the admission check runs in
  the boot chain before the room is constructed
  (`internal/app/bootstrap.go::Start`, immediately before the
  `room.NewServerRoom` call that feeds `room.SetDefault`). A veto fails the
  boot with the plugin's reason.
- **Cascade control plane** (`cascade` kind): admission runs inside
  `internal/net/gateway/rooms.go::createRoom` (the `POST /v1/rooms/create`
  facade). A veto answers `403 FORBIDDEN` with the plugin's reason in the
  contract error shape.
- **Relay rooms** (`relay` kind): admission runs in
  `internal/net/netconnectprotocol/realize/gameversionrelay.go::newRelayId`,
  before `room.GetRelayNew`. A veto is delivered to the requesting client
  through the same relay error channel used by the other rejection branches
  (`SendRelayServerType`), and no room is created.
- **Game end**: the hub subscribes to the room's
  `event.ServerGameOverEvent`, fired by
  `internal/game/room/serverroom.go::cleanData` (reached from `Gr()`;
  settlement data arrives via the check thread in
  `internal/game/manage/callmanage.go::StartCheckThread`). The subscription
  is wired in the boot chain exactly like the existing built-in subscribers
  (`internal/net/httpapi/httpapi.go::RegisterRoomEvents`,
  `internal/net/uplist/uplist.go::RegisterRoomEvents`).

Surveyed but deliberately not hooked in v1 (backlog): player join
(`internal/net/gameserver/registry.go::handleHelperEvent` fires
`event.PlayerJoinEvent`), player leave
(`internal/net/gameserver/gameserver.go` fires `event.PlayerLeaveEvent`),
game start (`internal/command/clientcommands.go` fires
`event.ServerGameStartEvent`), and ops actions (kick/ban paths in the
gateway and console commands).

### Sync/async semantics and failure policy, per event

**`room.create` — synchronous, vetoable, fail-open on error.**

- Avalon calls every enabled, ready, subscribed plugin **in parallel**; each
  call is bounded by its effective timeout (below). The admission decision
  waits for all calls to settle, so worst-case latency is the slowest single
  plugin timeout, never a sum.
- Any valid `allowed=false` vetoes the room. Multiple vetoes concatenate
  their reasons (`pluginName: reason`, joined with `; `).
- **Timeout threshold**: effective timeout = `min(manifest timeoutMs,
  admissionTimeoutMs)`; `admissionTimeoutMs` defaults to **2500 ms** and is
  the absolute cap.
- **Timeout/error policy: fail-open (allow).** Reasons:
  - Plugins are auxiliary governance. The built-in admission controls (UUID/IP
    bans, kick timers, name validation in the room's default
    `PlayerJoinEvent` listener) stay primary and are untouched.
    fail-closed would let a dead third-party process brick room creation,
    violating the isolation rule below.
  - Consistent with the project's default-open trust posture (the Cascade
    trust modes default to `open`, D-24).
  - Every fail-open is logged at WARN with plugin name and cause, and
    counted, so the degraded state is visible to ops.
- A veto's reason reaches the requester: relay clients get it on the relay
  error channel; the Cascade caller gets it in the `403` error message; a
  boot-time veto goes to the console and fails the boot.

**`game.over` — asynchronous, non-vetoable, retried.**

- Emit enqueues into a per-plugin outbox and returns immediately; the room's
  game-over flow never waits on any plugin.
- Each attempt is bounded by `deliveryTimeoutMs` (default **10 s**).
- Failures (timeout, connection refused, non-2xx, bad signature) retry with
  exponential backoff 1 s → 60 s (cap), unlimited attempts, bounded by the
  outbox capacity and max-age drops below.
- There is nothing to veto: responses are acks only.

### Delivery guarantees, event ids, dedup

- `game.over` is **at-least-once**, including across Avalon restarts: the
  outbox is a write-ahead log (one JSONL file per plugin under
  `/data/plugins/outbox/`). An entry is appended at enqueue time and removed
  only after a signed 2xx ack. On boot, leftover entries are replayed.
- Crash between ack and WAL removal duplicates the event on replay — by
  design. `eventId` makes replays idempotent: plugins **must** deduplicate on
  `eventId` and keep dedup state for at least **24 hours** (Avalon never
  delivers events older than `maxEventAgeHours`, default 24 h, so no
  duplicate can arrive outside that window).
- `room.create` is **at-most-once**: no retry. A lost admission call is a
  fail-open allow, never a stalled room.
- Avalon emits each `ServerGameOverEvent` fire exactly once per plugin
  (single enqueue site); double fires on the room side are already guarded
  by the `forcedReturn` re-entry flag in `cleanData`'s caller `Gr()`.

### Ordering and backpressure

- **One worker goroutine per plugin**, fed by that plugin's FIFO outbox:
  delivery order to a plugin is exactly emit order, which trivially preserves
  per-room ordering.
- `room.create` never queues (inline parallel calls with timeouts).
- Outbox capacity: `outboxCapacity` per plugin, default **128**. When full,
  the **oldest** entry is dropped (newest retained), counted and logged.
  Rationale: for statistics-style consumers the freshest results matter most,
  and dropping the oldest preserves the relative order of survivors.
- Entries older than `maxEventAgeHours` are dropped at delivery time
  (counted). A plugin that is down longer than the age window loses events —
  that loss is explicit, bounded, and logged, not silent unbounded growth.

### Authentication (both directions) and key rotation

Pre-shared secrets per plugin (hex, at least 16 bytes), configured as a list
to support rotation overlap.

- **Avalon proves itself to the plugin**: every request carries
  `X-Avalon-Timestamp` (unix seconds) and
  `X-Avalon-Signature: v1=<hex>`, where the signature is
  `HMAC-SHA256(timestamp + "." + raw_body, secret)`. The plugin rejects
  requests whose timestamp is off by more than 300 s or whose signature does
  not verify under one of its secrets.
- **The plugin proves itself to Avalon**: every response Avalon accepts
  (manifest, admission decision, ack) must carry the same two headers
  computed over the response body with the shared secret. Avalon verifies
  against the plugin's secret list; an invalid signature makes the response
  an error (admission: fail-open; ack: retry; manifest: unready). Identity
  in v1 is thus "holds the shared secret at the configured base URL";
  operators wanting transport-level identity run TLS (their choice, out of
  contract).
- **Rotation**: Avalon signs with the first secret in the list and verifies
  with any of them. Procedure: add the new secret at the front of both
  Avalon's and the plugin's lists, restart Avalon (config is read at boot),
  then remove the old secret. Overlap is bounded only by operator action —
  the same list-overlap pattern used for Cascade pinned fingerprints (D-24).

### Version negotiation

- Protocol id: `avalon-plugin/v1` (exact match in v1).
- Each event schema is versioned independently (`eventVersion`). The plugin
  manifest declares which versions it supports per event; Avalon emits the
  highest mutually supported version. In v1 Avalon only emits version 1, so
  "no intersection" means Avalon does not deliver that event to that plugin
  (WARN-logged; other events still flow).
- Negotiation happens via the manifest endpoint:
  - at boot, for every enabled plugin (parallel, per-plugin timeout =
    `admissionTimeoutMs`);
  - re-negotiation is triggered only by connection-level delivery failures
    (dial refused/reset — evidence the plugin process died/restarted), with
    backoff 5 s → 5 min. HTTP-level errors (timeout, 5xx) do not
    re-trigger negotiation.
- A plugin whose manifest is missing, unsigned, unparseable, or declares an
  incompatible protocol is **unready**: admission skips it (fail-open
  contribution + WARN), `game.over` events still enqueue and are delivered
  once negotiation succeeds; buffered events whose type/version did not
  negotiate are dropped (counted).

### Plugin registration and discovery

Declarative operator config, no auto-discovery. `/data/Plugins.json`
(loaded through the same bean `LoadOrInit` mechanism as the other config
files):

```json
{
  "version": 1,
  "admissionTimeoutMs": 2500,
  "deliveryTimeoutMs": 10000,
  "outboxCapacity": 128,
  "maxEventAgeHours": 24,
  "entries": [
    {
      "name": "example-stats",
      "baseUrl": "http://127.0.0.1:9800",
      "secrets": ["<hex, >= 16 bytes>", "<previous key during rotation>"],
      "enabled": true
    }
  ]
}
```

- Missing file → written with empty `entries` → hub is inert (zero
  regression).
- Structurally invalid config or invalid entries (duplicate name, bad URL,
  short/odd-length secret) fail the boot explicitly — explicit config does
  not silently degrade (same rule as the Cascade gateway, D-17/D-24).
- The plugin's own manifest (served at `GET {base}/v1/manifest`) declares:

```json
{
  "schema": "avalon-plugin/manifest",
  "protocol": "avalon-plugin/v1",
  "id": "example-stats",
  "version": "0.3.0",
  "events": { "game.over": {"versions": [1]}, "room.create": {"versions": [1]} },
  "timeoutMs": 2000,
  "maxConcurrency": 1
}
```

  - `timeoutMs`: the plugin's self-declared processing budget; Avalon caps
    it (`min` with its own caps), so a hostile or buggy manifest cannot stall
    admissions.
  - `maxConcurrency`: declared but reserved — v1 always runs one worker per
    plugin (the ordering guarantee); values > 1 are logged and treated as 1.

### Failure isolation

A plugin that dies, hangs, or lies must not affect the match flow:

- Every HTTP call runs under an explicit context timeout; response bodies are
  capped at 64 KiB; JSON is decoded into strict structs.
- Worker goroutines recover panics; a crashed worker is restarted with its
  outbox intact.
- Admission never blocks longer than the slowest plugin's effective timeout;
  errors fail open (above).
- `game.over` enqueue is non-blocking; the outbox is bounded (capacity +
  max age), so a dead plugin cannot grow memory without bound.
- Malformed admission responses (valid JSON but missing `reason` on a deny)
  are treated as errors → fail-open, because an unexplained deny is not a
  usable player-facing decision.

### Distribution requirements (ecosystem rule)

- Plugin artifacts must be distributed **content-addressed**: GitHub
  Releases (or equivalent immutable hosting) with a published SHA-256
  checksum per artifact. Personal domains are forbidden — the upstream
  ecosystem died exactly this way when `plugin.data.der.kim` lapsed
  (survey §C.2.3).
- Avalon itself never downloads or executes plugin code: operators install
  and run plugin processes; Avalon only speaks HTTP to the configured base
  URL. There is therefore no runtime checksum step in Avalon — the checksum
  discipline belongs to the install procedure, and this entry makes it a
  condition of being listed/recommended.

### Ops visibility

A read-only console command `plugin` lists each configured plugin with its
state (ready/unready/disabled), negotiated events, and counters
(delivered, retried, dropped, denied, fail-open errors). No mutation from
the console in v1.

## Backlog (explicitly not v1)

- `player.join` / `player.leave` / `game.start` notifications.
- Veto or confirmation hooks on ops actions (kick/ban).
- Plugin→Avalon read-only query API.
- Hot reload of `Plugins.json` (v1 reads config at boot).
- `maxConcurrency > 1` workers per plugin.
- Persistent dead-letter export for events dropped after max age.
- Relay room close / lifecycle events beyond creation.

## Consequences

- Empty config = byte-for-byte today's behavior: the hub constructs zero
  plugins, admission returns allow without any network I/O, and the
  game-over subscription enqueues into nothing.
- Boot-time admission means a plugin can veto the singleton room at boot;
  the server then fails to start with the plugin's reason on the console.
  Plugins that need to start after Avalon cannot veto that boot — the
  runtime-meaningful admission points are the relay and cascade paths.
- The strict response-signature rule means a plugin that fails to sign
  responses will see its acks rejected and its events retried; that is the
  intended visible failure, logged with counters.
- v1 pins one worker per plugin; raising concurrency later requires
  per-room ordering keys, deliberately deferred.

## Evidence

- Boot chain and room construction:
  `/home/lnb/Desktop/HA/Avalon/internal/app/bootstrap.go::Start`
- Singleton-room model: `knowledge/0003-single-room-model.md`
- Game-over fire site:
  `/home/lnb/Desktop/HA/Avalon/internal/game/room/serverroom.go::cleanData`;
  settlement fetch:
  `/home/lnb/Desktop/HA/Avalon/internal/game/manage/callmanage.go::StartCheckThread`
- Cascade create-room facade:
  `/home/lnb/Desktop/HA/Avalon/internal/net/gateway/rooms.go::createRoom`;
  error shape and `FORBIDDEN` code:
  `/home/lnb/Desktop/HA/Avalon/internal/net/gateway/control.go::writeErr`
- Relay room creation:
  `/home/lnb/Desktop/HA/Avalon/internal/net/netconnectprotocol/realize/gameversionrelay.go::newRelayId`
- Existing room-event subscriber pattern:
  `/home/lnb/Desktop/HA/Avalon/internal/net/httpapi/httpapi.go::RegisterRoomEvents`
- Player join/leave fire sites (backlog anchors):
  `/home/lnb/Desktop/HA/Avalon/internal/net/gameserver/registry.go::handleHelperEvent`,
  `/home/lnb/Desktop/HA/Avalon/internal/game/room/event.go::registerDefaultEvents`
- Contract registration:
  `/home/lnb/Desktop/HA/HA-Contract/docs/api/openapi/avalon-plugin-v1.yaml (field: avalon-plugin/v1)`
- Ecosystem survey (distribution lesson, isolation survey):
  `/home/lnb/Desktop/HA/PLUGIN-SYSTEM-SURVEY-20261107.md`
