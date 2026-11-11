# 0011. Map Distribution (Engine Streaming + Content Manifest)

## Status

Accepted (protocol v4)

## Context

Entry 0008 shipped custom map selection with an explicit non-goal: no map
file transfer, clients had to pre-install custom maps. Official Rusted
Warfare hosts stream maps to joining clients automatically ("maps stream from
the host; only units must be pre-installed"). This entry records the 2026-11
work that closed that gap.

## Key finding: the transport already existed

Forensics (bytecode of the closed-source engine + a live probe against the
real helper) established that the engine's host→client map streaming is built
in **and already wired in the headless flow**:

- Host side: the start-game sender (engine class `ad.a(c,Z)`, reached via
  `Multiplayer.multiplayerStart()` → `ae()`) takes the custom-map branch when
  the map-type enum is the custom value, opens the map file at the engine
  path field and writes `[int32 size][raw bytes]` into the START_GAME packet
  (log: "Sending map stream of size: N"). On open failure it logs
  "Map error starting game" and sends no START_GAME at all.
- Client side: the START_GAME handler reads `[byte][int32 enum]`, and for the
  custom branch reads the `[size][bytes]` stream and loads the map directly
  from it (no local copy needed; log: "Note: Game was started from a custom
  map on server.").
- Helper wiring: `GoBridge.roomStart` sets the map-type enum + file name;
  `FileLoaderRedirections` remaps `/SD/rusted_warfare_maps` to `data/maps`;
  the broadcast reaches clients through `PlayerConnectX` → helper protocol →
  Go relay (transparent, 50 MB packet ceiling).

The live probe (fake client speaking the real wire protocol, including the
registration checksum/integrity/extra challenges) received a START_GAME whose
map stream was byte-identical to the registered file.

What was actually missing: integrity (no manifest, no hash — a same-name
different-content file was accepted silently) and failure semantics
(everything degraded into "the connection dropped").

## Design (protocol v4)

Same abstraction as mod delivery (entry 0009 lineage; `ModManifest`):
content manifest = `{name, file, sha256, size}`; the content hash is the only
trust anchor; same name with different content is never treated as present.
Transport stays the engine's START_GAME stream — no extra port, no side
channel.

1. **Registration** — `internal/game/manage/mapmanage.go::ReadMapAndSave`
   computes SHA-256 + size for every registered map. For zip-packed maps the
   hash covers the entry that will be materialized (i.e. exactly the bytes
   the engine will stream).
2. **Start-time verification (Go)** —
   `internal/game/seam.go::GameSession.StartGame` calls
   `internal/game/gamemaps.go::MapData.VerifyOnDisk` after materialization:
   missing file → `game.ErrMapSourceUnavailable`; size/hash drift or missing
   manifest → `game.ErrMapContentMismatch`. The room stays unstarted.
3. **Start-time verification (helper)** — protocol v4 adds `mapSha256` /
   `mapSize` to `room.start` (mandatory for non-default maps; enforced by
   `validateRequest`). `GoBridge.roomStart` re-checks the on-disk bytes
   before handing them to the engine and answers with typed errors
   (`map file missing: X` / `map content mismatch: X (...)`). Version gate
   as with v3: hello handshake rejects mismatches, no silent downgrade.
4. **Failure classification (gameserver)** —
   `internal/net/gameserver/registry.go::routeStartGame` inspects the
   START_GAME prefix read-only (byte + enum + stream size) and records
   delivery per connection; a write failure is classified as transfer
   interrupted. `internal/net/gameserver/gameserver.go::mapDeliveryOutcome`
   classifies a disconnect after delivered-but-unaccepted map stream as
   client refusal/load failure (the official protocol has no explicit refuse
   packet; accepting is evidenced by ACCEPT_START_GAME/TICK/GAMECOMMAND).

## The four failure paths

| Failure | Where detected | Signal |
|---|---|---|
| Server does not have the map | Go `StartGame` / helper `roomStart` | `game.ErrMapSourceUnavailable` / `map file missing: X`; room stays unstarted |
| Content mismatch (same name, different bytes) | Go `VerifyOnDisk` / helper `verifyMapFile` | `game.ErrMapContentMismatch` / `map content mismatch: X (...)` |
| Transfer interrupted | Go `routeStartGame` write error; helper fatal/exit | typed log `map delivery: transfer interrupted ...` |
| Client refuses / fails to load | Go disconnect heuristic | typed log `map delivery: client refused or failed to load map ...` |

## Limits

- The stream carries the materialized `.tmx`. Zip-packed maps with custom
  tileset textures only stream the tmx; textures that are not built-in assets
  still need pre-installation (same behavior as official hosts — the client
  parses the stream as tmx XML).
- The official client has no manifest negotiation: it always loads the
  streamed bytes. The manifest's consumers are the server components (Go
  registry, helper pre-flight check).
- Client refusal is a heuristic (delivered + no accept/in-game packet before
  disconnect); the wire protocol provides no explicit refusal.

## Evidence anchors

- `internal/game/manage/mapmanage.go::ReadMapAndSave`
- `internal/game/manage/mapmanifest.go::BuildMapManifest`
- `internal/game/gamemaps.go::MapData.VerifyOnDisk`
- `internal/game/seam.go::GameSession.StartGame`
- `internal/game/external/protocol.go::ProtocolVersion`
- `internal/game/external/simulation.go::Backend.Start`
- `internal/net/gameserver/registry.go::routeStartGame`
- `internal/net/gameserver/gameserver.go::mapDeliveryOutcome`
- helper: `/home/lnb/Desktop/avalon/Avalon-Helper/Helper/src/main/kotlin/io/github/corematch/avalonhelper/plugin/internal/headless/inject/go/GoBridge.kt::roomStart`
- helper: `/home/lnb/Desktop/avalon/Avalon-Helper/Helper/src/main/kotlin/io/github/corematch/avalonhelper/helper/HelperMain.kt`
