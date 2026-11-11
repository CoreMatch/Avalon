# 0012. Lobby Boot and Client Start Decoupling (`cascadeLobby` / `clientStart`)

## Status

Accepted (author decision, 2026-11-11; DECISIONS D-26)

## Context

Entry 0009's zero-mod lobby shipped as `cascadeLobby`
(`internal/data/bean/configs.go::CoreConfig`). One boolean carried two
independent meanings:

1. **Boot behavior** — the boot start command (`defStartCommand`) only creates
   the room (helper `room.create`), never starts a match (`room.start`).
2. **Client start permission** — the client `/start` command is rejected
   unconditionally (message `err.lobbyNoStart`).

Meaning 1 is a hard deployment requirement for every backend: with a real
boot-start, a 0-player room is terminated within one second by the room check
thread (`internal/game/manage/callmanage.go::StartCheckThread`, no-AI 0-player
branch → `Gr()`), and players can no longer join afterwards (2026-11-11
real-client reproduction; `docs/OPERATIONS.md` §2.1). So every deployed
backend — lobby and sub-servers alike — must set `cascadeLobby: true`.

But meaning 2 then leaked into sub-servers: a mod-room backend where players
are supposed to play a match could be entered and waited in, yet `/start` was
refused forever. The room was permanently stuck in the lobby state. The
interim fix (sub-servers inherit `cascadeLobby: true` and accept "can enter,
can wait, cannot start") was explicitly recorded as a known limitation
pending this decoupling.

## Decision

Split the two meanings. `cascadeLobby` keeps its name and its boot semantics;
a new independent key `clientStart` controls client-side start permission.

- `clientStart` is a tri-state string
  (`internal/data/bean/configs.go::CoreConfig.ClientStart`):
  - `""` (default) — legacy semantics: client start allowed iff
    `cascadeLobby` is false (`CoreConfig.ClientStartAllowed`). Existing
    deployments upgrading without the key keep exactly their old behavior;
    the change is silent by design only in the "no change" direction.
  - `"allow"` — client start permitted even with `cascadeLobby: true`
    (the sub-server form).
  - `"deny"` — client start refused even with `cascadeLobby: false`.
  - Any other value fails startup explicitly
    (`internal/data/bean/configs.go::ValidateCoreConfig`, wired in
    `internal/app/bootstrap.go::Runtime.Start` right after config load).
    No silent correction — wrong semantics must not run.

- Boot path and match path are now distinct call chains:
  `internal/game/seam.go::GameSession.StartGameAtBoot` (boot: lobby branch =
  create-only) vs `internal/game/seam.go::GameSession.StartGame` (match:
  always a real start), mirrored at the room level by
  `internal/game/room/serverroom.go::ServerRoom.StartGameAtBoot` /
  `ServerRoom.StartGame`. The console/boot command
  (`internal/command/start.go::registerStartGame`) uses the boot path; the
  client `/start` command uses the match path.

- The `/start` gate (`internal/command/clientcommands.go::RegisterClientCommands`)
  consults `ClientStartAllowed` instead of `CascadeLobby` directly. The
  auto-start countdown fires `/start` through the same command handler with a
  nil player (`internal/game/room/event.go::registerDefaultEvents`), so it is
  gated identically — a legacy lobby never auto-starts either.

- Refusal messages stay distinct: lobby semantics → `err.lobbyNoStart`
  (unchanged); explicit `deny` → new key `err.startDisabled` (all four
  bundles).

Deployment forms after the split:

| form | `cascadeLobby` | `clientStart` |
|---|---|---|
| landing lobby (default backend) | true | "" |
| sub-server / mod room | true | "allow" |
| legacy standalone | false | "" |

## Boundary cases (defined, not implicit)

- **Too few players** — unchanged: `startMinPlayerSize` check with message
  `start.playerNo`, evaluated after the gate.
- **Already in a match** — unchanged: `err.startGame`.
- **After game over** — the room returns to the idle state (`Gr` /
  `cleanData`); `/start` starts a fresh match, same as any unstarted room.
- **Boot with `clientStart: "allow"`** — still create-only; the permission
  only affects the client/auto path, never the boot path.

## Rejected alternatives

- **Repurpose `cascadeLobby` (change its meaning or split it into two
  booleans with new names)** — rejected by the author: every existing
  deployment's config would silently change meaning. The old key keeps its
  name and its boot semantics; the new key defaults to legacy behavior.
- **A `bootStart`/`defStartCommand`-only fix** (sub-servers boot with an
  empty start command) — does not restore `/start`; the refusal was
  hard-coded in the command gate, not a consequence of boot behavior.
- **Letting the auto-start countdown bypass the gate** — rejected: the
  countdown goes through the same command entry on purpose, so a legacy
  lobby can never start by any path.

## Verification

- Unit: tri-state × both lobby forms for the gate
  (`internal/command/clientcommands_test.go`), boot-vs-match split at both
  seam levels (`internal/game/seam_test.go`,
  `internal/game/room/serverroom_test.go`), config validation and
  missing-key default (`internal/data/bean/configs_test.go`).
- Integration with the real JVM engine
  (`internal/net/gameserver_e2e_test.go::TestE2ELobbyBootThenClientStart`):
  boot creates the room with engine `room.status` not Running; `/start`
  through the room command handler then performs a real `room.start`
  (engine `room.status` Running).
- Boundary of verification: engine registration anti-cheat (core-unit
  checksum, entry 0009 §1.1) only a real RW client can pass — "two human
  clients playing a full match" remains unverifiable by probes.
