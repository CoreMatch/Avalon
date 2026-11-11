# 0005. Upstream Lineage and Derivative Status (Copycat Audit)

## Status

Accepted (audit record; reproducible — see §7)

## Context

Avalon is the Go re-hosting of the RW-HPS game server. The question this audit
answers: **how much of Avalon is still genealogically derived from upstream
RW-HPS** (original author deng-rui; personal repository deleted). Audit date
2026-11-07; read-only analysis (no code or license changes). Companion audit
for the Java/Kotlin side lives in the Avalon-Helper repository
(`docs/LINEAGE-COPYCAT.md`).

## Baseline

- **Upstream tree**: RW-HPS org repository (`https://github.com/RW-HPS/RW-HPS.git`)
  master @ **`239a7773`**, available locally through the Avalon-Helper
  checkout's `upstream` remote (workspace path `/home/lnb/Desktop/HA/rw/Avalon-Helper`).
- **Topological fact**: in the Avalon-Helper checkout,
  `git merge-base master upstream/master` = `239a7773` = upstream HEAD — the
  fork point **is** the upstream HEAD, so the fork's Kotlin tree was
  byte-identical to upstream at fork time. The deleted personal repository of
  deng-rui is therefore not needed as a baseline.
- **Avalon's git history cannot reveal the lineage**: all 138 commits (the 129
  Go-Server commits migrated from RW-HPS via `git filter-repo` plus later
  work) are authored by SamuelCheston. Lineage is instead established by
  correspondence comments and structural comparison against the upstream tree.

## Method

1. **Correspondence-marker coverage** — Go files self-document their upstream
   counterparts (`对应 net.rwhps.server.X` / `照搬上游` comments).
2. **Structural spot-checks** — constants, magic numbers, algorithm step
   order, and naming habits compared file-pair by file-pair against upstream
   Kotlin (preserved upstream typos are the strongest single indicator).
3. **Resource blob diffs** — embedded assets compared byte-for-byte against
   the upstream tree.

## Findings: per-directory classification

| Directory | Classification | Evidence |
|---|---|---|
| `internal/io` | **translation port** | `internal/io/packettype/packettype.go` reproduces upstream `PacketType.kt` constant-by-constant — **including the upstream typo `UPDATA_CLASS_RECEIVE`**; `gamestream.go` (61 functions) mirrors `GameInputStream`/`GameOutputStream` |
| `internal/net` (except `gateway/`) | **translation port** | decoder/mixprotocol/rcondecoder/service/staticdata map one-to-one to upstream; `gameserver/` = Go edition of `GameVersionServer`+`HeadlessTypeConnect` chain; `netconnectprotocol/` = TypeRelay/GameVersionRelay family; `http/` = StartHttp/WebData; `httpapi/` = upstream beta-http plugin port (decision D-14); `uplist/` = UPList module port |
| `internal/game` (except `external/`, `seam.go`) | **translation port** | room/player/manage/gamemaps/event correspond to the same-named upstream classes |
| `internal/command` | **translation port** | core/servercommands/clientcommands/relay/ex-vote correspond to upstream command classes |
| `internal/data` | **translation port** | `data.go` value-by-value from `Data.kt`; bean/administration/statisticians/cache correspond |
| `internal/util`, `internal/struct`, `internal/core` | **translation port** | log/file/algorithms/compression/str/mathutil/ip/console and Seq/ObjectMap/Threads correspond to the upstream util family (`javastr` = port of `String.hashCode`) |
| `internal/app` | **mixed** | bootstrap/initialization/runtime/events port Main/Initialization/Application/EventGlobal; `cascade.go` original |
| `internal/timetask` | **rewrite (semantic port)** | Go rewrite of the upstream Quartz-wrapper semantics; code newly written |
| `internal/resource/assets` | **upstream as-is (data files)** | `eula/Main-China.md`, `eula/Main-English.md`, `URL.ini` blob-identical to upstream; `bundles/HPS_*.properties` = upstream + exactly 2 added keys (`serverCommands.addai`, `server.load.gameCore`) |
| `cmd/rwhps` | **port skeleton** | thin entry corresponding to upstream `Main` |
| `internal/game/external`, `internal/net/gateway` | **original** | helper wire protocol (v2/v2-ext) and Cascade backend contract — no upstream counterpart |
| `dist/`, `docs/`, `.github/` | **original** | distribution system written for this repository |

Marker coverage: **167 of 171 non-test Go files (97.7%) carry explicit
upstream-correspondence comments**. The 4 unmarked files are precisely the
Cascade-original ones: `internal/app/cascade.go`,
`internal/net/gateway/{jtistore,preamble,trust}.go`.

## Translation-port inventory (the actual locus of license risk)

`internal/io`, `internal/net` (except `gateway/`), `internal/game` (except
`external/` and `seam.go`), `internal/command`, `internal/data`,
`internal/util`, `internal/struct`, `internal/core`, the `internal/app` boot
chain, `cmd/`, and `internal/resource/assets`.

These directories are section-by-section rewrites of upstream Kotlin.
**Translation is a canonical derivative form under copyright law; a difference
of programming language is not a defense.** This inventory is where the
derivative status actually attaches.

## Genuinely original parts

- `internal/game/external/` — the helper wire protocol (v2/v2-ext): an
  architecture with no upstream counterpart (upstream kept the engine
  in-process; see entry 0002).
- `internal/net/gateway/` — the Cascade backend contract implementation
  (mTLS/`CASC` preamble/JWT session gating/`/v1` control plane/trust modes).
- `internal/app/cascade.go`, `internal/timetask/` (rewrite), `dist/`,
  `docs/`, `.github/`.

## Conclusion: derivative ≠ risk

- **Verdict**: Avalon **is a derivative work** of upstream RW-HPS, in the form
  of translation/adaptation rather than verbatim copying. The rename surgery
  (repository/package/artifact names) changed identity, not lineage — the
  expression-level debt (structure, naming, constants, algorithm order,
  preserved typos) remains demonstrable and is documented above.
- **Legal posture**: AGPL-3.0 **permits** derivative works. Its obligations are
  license propagation, preserved copyright/license notices, and source
  availability. Avalon keeps an AGPL `LICENSE` and a provenance note in
  `README.md` — the compliant posture for exactly this situation.
- **Risk arises only from** denying the lineage or stripping the license —
  the opposite of the current posture. "Being a derivative work" is therefore
  not, by itself, a legal risk for Avalon.

## Reproduction

All commands verified on audit day (Avalon @ `c251706`, Avalon-Helper
checkout at `/home/lnb/Desktop/HA/rw/Avalon-Helper` with `upstream/master` @ `239a7773`).

```bash
# 1. Correspondence-marker coverage (expect 167/171)
cd /home/lnb/Desktop/HA/rw/Avalon
total=$(git ls-files '*.go' | grep -v _test.go | wc -l)
marked=$(git grep -l '对应\|照搬\|上游' -- '*.go' | grep -v _test.go | wc -l)
echo "$marked/$total"

# 2. The preserved upstream typo (expect hits in BOTH trees)
grep -n 'UPDATA_CLASS_RECEIVE' internal/io/packettype/packettype.go
git -C /home/lnb/Desktop/HA/rw/Avalon-Helper show \
  upstream/master:Server-Core/src/main/java/net/rwhps/server/io/packet/type/PacketType.kt \
  | grep -n 'UPDATA_CLASS_RECEIVE'

# 3. Resource blobs vs upstream (expect 3x IDENTICAL, bundles +2 lines)
for f in eula/Main-China.md eula/Main-English.md URL.ini; do
  diff -q "internal/resource/assets/$f" \
    <(git -C /home/lnb/Desktop/HA/rw/Avalon-Helper show "upstream/master:Server-Core/src/main/resources/$f") \
    >/dev/null 2>&1 && echo "IDENTICAL: $f" || echo "DIFFERS: $f"
done
diff internal/resource/assets/bundles/HPS_en_US.properties \
  <(git -C /home/lnb/Desktop/HA/rw/Avalon-Helper show \
    upstream/master:Server-Core/src/main/resources/bundles/HPS_en_US.properties) \
  | grep -c '^<'

# 4. Fork-point topology (expect 239a7773 twice)
git -C /home/lnb/Desktop/HA/rw/Avalon-Helper merge-base master upstream/master
git -C /home/lnb/Desktop/HA/rw/Avalon-Helper rev-parse upstream/master

# 5. Avalon-Helper side line attribution: see Avalon-Helper scripts/lineage-report.sh
```

## Evidence anchors

- `internal/io/packettype/packettype.go::UPDATA_CLASS_RECEIVE` — preserved upstream typo
- `internal/resource/assets/eula/Main-China.md` — blob-identical to upstream
- `internal/resource/assets/bundles/HPS_en_US.properties` — upstream + 2 keys
- `internal/app/cascade.go`, `internal/net/gateway/trust.go` — original (no correspondence marker)
