# 0008. Custom Map Selection (Current State)

## Status

Accepted (current-state record; protocol v3)

## Context

Custom maps (server-side `.tmx` / zip-packed maps under `data/maps`, saves
under `data/saves`) are selectable for a room. This entry records what is
supported now and where the hard limits are. Companion facts: maps are parsed
by the engine only (entry 0002). Map distribution was added later (protocol
v4) and is documented in entry 0011 — the engine streams the selected map to
clients inside START_GAME at game start.

## Selection chain (as implemented)

1. **Registration** — `internal/game/manage/mapmanage.go::ReadMapAndSave`
   scans `data/maps` (`.tmx`, `.zip` containing tmx entries) and `data/saves`
   (`.rwsave`, zip) at boot and after `reloadmaps`. Registration order is kept
   in a slice (`mapsDataOrder`) because upstream index selection relied on an
   ordered map. Names failing `validMapName` (empty, `/`, `\`, `..`) are
   rejected — map names are concatenated into the engine load path.
2. **Selection** — player command `map <index>` (0-based, upstream semantics)
   in `internal/command/clientcommands.go`; server console `mapchange` takes
   the first registered map (`internal/command/servercommands.go`). Both only
   touch room state (`GameSession.Maps`).
3. **Start** — `internal/game/seam.go::GameSession.StartGame` materializes zip
   maps first (`MapData.ReadMap` extracts the tmx into `data/maps`, upstream
   `readMap` semantics), then sends `room.start` with `mapType` +
   `mapFile` (protocol v3 fields; omitted for default maps).
4. **Engine load** — helper `GoBridge.roomStart` custom branch sets the engine
   path `/SD/rusted_warfare_maps/<mapFile>` (remapped to `data/maps` by
   `FileLoaderRedirections`) with the map-type enum; default maps keep the
   `maps/skirmish/<mapPlayer><mapName>.tmx` branch unchanged.

## Protocol v3

`ProtocolVersion = 3` on both sides (Avalon
`internal/game/external/protocol.go::ProtocolVersion`, helper
`HelperMain.kt::PROTOCOL_VERSION`). v3 adds optional `room.start` fields
`mapType` (`CustomMap`/`SavedGames`) and `mapFile`. The version itself is the
compatibility gate: the hello handshake rejects mismatches, so an old helper
can never silently treat a custom-map start as a default-map start.

## Distribution model (superseded by entry 0011)

At v3 acceptance the server did not transmit map files; every client had to
pre-install a selected custom map. Protocol v4 (entry 0011) added map
distribution: the engine streams the selected map inside START_GAME, gated by
a SHA-256/size content manifest registered at map scan time.

## Failure distinguishability (v4 update in entry 0011)

| Failure | Distinguishable? | Signal |
|---|---|---|
| Map not registered / bad index | yes | command replies `map.no` / `err.noNumber`; room state untouched |
| Unsafe map name | yes | rejected at registration (logged) and again in helper (`unsafe mapFile`) |
| Server-side map file missing (File type) | yes | `StartGame` fails with `game.ErrMapSourceUnavailable`; room stays unstarted |
| Server-side map content differs from manifest | yes (v4) | `StartGame` fails with `game.ErrMapContentMismatch`; helper answers `map content mismatch: ...` |
| Engine fails to load the map (corrupt tmx) | partially | helper `room.start` error / fatal event surfaces as start failure; reason text is engine-provided |
| Client missing the map | resolved (v4) | engine streams the map inside START_GAME; client loads from the stream (entry 0011). Client-side load failure / refusal is classified by the gameserver disconnect heuristic |

## Limits

- No mod content via maps (unit definitions are mod territory; see the mod
  investigation notes in the SamuelPortal PARITY doc). Map file transfer
  itself exists since v4 (entry 0011).
- `SavedGames` maps ride the same branch (`.rwsave`) but have no dedicated
  selection UI beyond the shared index.
- Zip maps are materialized next to plain maps; `MapData.Clean` removes the
  extracted copy when the room asks for cleanup.

## Evidence anchors

- `internal/game/manage/mapmanage.go::validMapName`
- `internal/game/manage/mapmanage.go::MapDataByIndex`
- `internal/game/seam.go::GameSession.StartGame`
- `internal/game/external/protocol.go::ProtocolVersion`
- `internal/command/clientcommands.go` — `map` command registration
- helper: `/home/lnb/Desktop/HA/rw/Avalon-Helper/Helper/src/main/kotlin/io/github/corematch/avalonhelper/plugin/internal/headless/inject/go/GoBridge.kt::roomStart`
