> 来源：HA-Contract@bab2a05（2026-11-07），原路径 knowledge/0002-game-engine-as-external-dependency.md（契约仓知识库回迁）。

# ADR-0002: The Game Engine Is an External Dependency

- **Status**: accepted

## Context

The upstream server does not implement the game itself. It loads the closed-source
`rusted-warfare.jar` inside its own JVM and uses ASM bytecode rewriting to attach
the game's server-side main loop to its event bus. The Go port must provide the
same game behavior from a process that cannot execute JVM bytecode.

## Decision

The game engine is kept as an **external dependency**: a separate JVM process
(the "helper") hosts the engine, and the Go server talks to it over the helper
protocol (JSON lines over the helper's stdin/stdout, protocol version 2).

- **Why external**: a Go process cannot execute Java bytecode and has no equivalent
  of the upstream's bytecode rewriting, so the simulation layer cannot be ported
  1:1. The Go side declares only the operations the network and command layers
  actually call, and deliberately provides no fake implementation — a start without
  a backend must fail explicitly rather than produce an empty room that "looks like
  it runs".
- **What the protocol carries**: v2 covers the room/connection lifecycle and packet
  relay (`hello`, `room.create/destroy/start/stop/status`, `conn.open/close/packet`,
  plus `player.*`/`chat`/`command` events back to Go). v2-ext adds the operations
  that upstream performs on in-process engine objects (`room.addai`, `room.sync`,
  `room.removeplayer`, `game.result`, `room.settings`), without changing the wire
  version — a v2-only helper answers `unsupported` and Go degrades those calls to
  logged no-ops.
- **Degradation without a helper**: the server still starts and serves connections.
  Match start returns the readable error `game: no simulation backend available`.

## Consequences

- Building and deploying the helper (JDK 17 required; JDK 25 crashes the Kotlin
  2.0.0 compiler) is part of operating a full-featured server; a host without it
  runs a degraded server that cannot start matches.
- Every engine-side capability needs a protocol operation; new capabilities
  (e.g. room settings for the Cascade facade) extend v2-ext rather than the Go
  process.
- Misconfiguration is checked statically at startup (parameters present, files
  exist) instead of surfacing as an opaque EOF at first match start; a broken helper
  configuration falls back to the no-backend state with a warning instead of
  preventing startup.
- Cost: one extra process to supervise, one protocol to keep in sync, and a hard
  rule that unknown helper message types are fatal (silent ignoring would let the
  two sides drift).

## Evidence

All paths absolute into the Avalon checkout
(`/home/lnb/Desktop/HA/Avalon`); anchors verified at commit `599b0e3`.

- Boundary rationale:
  `/home/lnb/Desktop/HA/Avalon/internal/game/seam.go::Simulation` — upstream
  loads the closed-source jar in-JVM and rewires it via ASM; "Go 进程无法执行
  Java 字节码" (a Go process cannot execute Java bytecode), no fake
  implementations.
- Explicit-failure design:
  `/home/lnb/Desktop/HA/Avalon/internal/game/seam.go::ErrNotImplemented`
  (`ErrNotImplemented = "game: no simulation backend available"`),
  `/home/lnb/Desktop/HA/Avalon/internal/game/seam.go::Unimplemented`
  (`Unimplemented` placeholder;
  `/home/lnb/Desktop/HA/Avalon/internal/game/seam.go::Backend` defaults to it).
- v2 operation set and validation:
  `/home/lnb/Desktop/HA/Avalon/docs/HELPER-PROTOCOL.md (field: 操作（Go -> helper)`;
  transport and wire format
  `/home/lnb/Desktop/HA/Avalon/docs/HELPER-PROTOCOL.md (field: 传输与线格式)`;
  events
  `/home/lnb/Desktop/HA/Avalon/docs/HELPER-PROTOCOL.md (field: 事件（helper -> Go)`.
- v2-ext purpose and compatibility:
  `/home/lnb/Desktop/HA/Avalon/docs/HELPER-PROTOCOL.md (field: 版本与向后兼容)`
  (same wire version; v2-only helpers answer `unsupported`; Go normalizes to
  `game.ErrUnsupported` and degrades to logged no-ops);
  `/home/lnb/Desktop/HA/Avalon/internal/game/seam.go::ErrUnsupported`
  (`ErrUnsupported`),
  `/home/lnb/Desktop/HA/Avalon/internal/game/seam.go::RoomControl`
  (`RoomControl` interface).
- Degradation path:
  `/home/lnb/Desktop/HA/Avalon/internal/app/simulation.go::loadSimulation`
  (no backend → `Unimplemented`, "开局返回可读的错误"),
  `/home/lnb/Desktop/HA/Avalon/internal/app/simulation.go::GameHelperPath`
  (helper not configured is the silent default),
  `/home/lnb/Desktop/HA/Avalon/internal/app/simulation.go::attachSimulation`
  (broken helper config → warn + fall back to no backend);
  `/home/lnb/Desktop/HA/Avalon/internal/app/bootstrap.go::ErrNotImplemented`
  ("没有后端时 start 会返回 ErrNotImplemented").
- JDK constraint: JDK 25 crashes the Kotlin 2.0.0 compiler; build with JDK 17.
  Not independently re-verified.
