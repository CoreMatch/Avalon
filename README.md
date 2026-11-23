<div align="center">
  <h1>Avalon</h1>
  <p><strong>Real Rusted Warfare servers, operated with confidence.</strong></p>
  <p><a href="./README.zh-CN.md">简体中文</a></p>
</div>

Avalon is a dedicated-server runtime for Rusted Warfare 1.15 / protocol 176. It gives operators a Go networking, room, administration, and console layer backed by real JVM game simulation.

## Why Avalon

- **Run rooms with control.** Manage players, moderation, and live rooms from one operator-facing runtime.
- **Be discoverable.** Connect server-list and Relay workflows without giving up administrative control.
- **Play the real game.** Protocol v4 drives the JVM simulation, AI, and match results.
- **Extend without coupling.** Out-of-process plugins add behavior without loading third-party code into the engine.
- **Stream verified content.** Custom maps and mods are checked before distribution to clients.
- **Contain failures and scale the edge.** Helper crash isolation protects the host, while Cascade adds a shared public entrance.

## Learn more

- [Operations](docs/OPERATIONS.md) — rooms, routing, content, and plugin operations
- [Distribution](docs/DISTRIBUTION.md) — supported runtime and distribution forms
- [Avalon-Helper](https://github.com/CoreMatch/Avalon-Helper) — the JVM simulation companion
- [HA Contract](https://github.com/CoreMatch/HA-Contract) — canonical integration contracts
- [HA Wiki](https://wiki.mcnb.dev/) — ecosystem deployment and development guides

## Limits

Avalon is not pure Go, an official Rusted Warfare server, or a promise of upstream feature parity; real matches require Avalon-Helper.

## License and provenance

[AGPL-3.0](LICENSE) · Derived from [RW-HPS](https://github.com/RW-HPS/RW-HPS); upstream copyright notices are retained.
