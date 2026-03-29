# ZFS Snapshot Ecosystem Research

Research notes on projects using ZFS copy-on-write for fast isolation — database cloning,
build caching, CI acceleration, and ephemeral infrastructure.

Conducted 2026-03-29 as background research for forge-metal's ZFS golden image clone architecture.

## Projects studied

| Project | Language | What it does | Focus |
|---------|----------|-------------|-------|
| [OBuilder](obuilder.md) | OCaml | ZFS snapshots per build step + runc sandboxing | Build cache, seccomp fsync bypass, crash recovery |
| [DBLab Engine](dblab.md) | Go | Thin-clone Postgres databases via ZFS | Dual-pool rotation, pre-snapshot dance, metadata layer |
| [Velo](velo.md) | TypeScript | Git-like Postgres branching on ZFS | Clone-then-swap, atomic state, CHECKPOINT pattern |
| [go-zfs](go-zfs.md) | Go | ZFS CLI wrapper library | API comparison, go-zfs vs go-libzfs tradeoffs |
| [Impermanence](impermanence.md) | Nix | Ephemeral root filesystem via ZFS rollback-on-boot | Opt-in persistence, boot sequence, CI runner patterns |
| [Incus](incus.md) | Go | System container & VM manager with deep ZFS driver | Ghost graveyard GC, forkzfs namespace isolation, GUID migration, seccomp interceptor |
| [Firecracker](firecracker.md) | N/A | MicroVM memory+CPU snapshots for instant VM resume | Comparison with ZFS approach, tradeoffs for CI |

## Cross-cutting patterns

**Everyone shells out to `zfs` CLI.** DBLab, OBuilder, Velo, go-zfs, Incus — nobody uses libzfs
bindings in production. The CLI is ZFS's stable API. go-libzfs exists but has SIGSEGV panics,
breaks on every ZFS upgrade, and has a global mutex. Incus has 4,400 lines of subprocess calls.

**Everyone invents their own metadata layer.** ZFS has no built-in way to track
clone→parent relationships:

| Project | Metadata store |
|---------|---------------|
| OBuilder | SQLite + `@snap` tag as completion marker |
| DBLab | Custom ZFS user properties (`dle:branch`, `dle:parent`, base64 messages) |
| Velo | JSON file with atomic write + fsync + backup |
| Incus | Custom ZFS properties (`incus:content_type`) + dataset naming conventions + Cowsql (distributed SQLite) |
| Impermanence | Nix module declarations |

**Three solutions to "update golden image while clones exist":**

| Project | Strategy |
|---------|----------|
| OBuilder | Promotion dance (`zfs promote` + rename + deferred destroy) |
| DBLab | Dual-pool rotation (refresh idle pool, swap active pointer) |
| Velo | Clone-then-swap (clone → verify → rename → destroy old) |
| Incus | Ghost graveyard (rename to `deleted/` namespace, recursive GC when dependents die) |

## Techniques most applicable to forge-metal

1. **OBuilder's seccomp fsync bypass** — intercept sync syscalls, return success. If build
   crashes, result is discarded. Eliminates biggest I/O bottleneck in npm workloads.

2. **DBLab's async clone creation** — return `StatusCreating` immediately, provision in
   goroutine. Don't block the scheduler on container startup.

3. **OBuilder's `@snap` crash recovery** — presence of snapshot = completed build. No
   external state needed. Orphan detection on agent restart = "clone without @snap → destroy."

4. **Velo's Rollback class** — LIFO cleanup stack for multi-step operations. Applicable to
   sandbox lifecycle (clone → setup → run → cleanup).

5. **DBLab's dual-pool rotation** — for golden image refresh without downtime. Alternative
   to forge-metal's current rolling 25% fleet refresh.

6. **Velo's recordsize tuning** — match ZFS recordsize to application page size. For
   ClickHouse, tune to match mark/granule size.

7. **Incus's `forkzfs` namespace isolation** — re-exec self in a clean mount namespace
   before running ZFS commands. Prevents bind-mount pollution from running containers
   breaking `zfs rename`.

8. **Incus's GUID-based incremental migration** — exchange snapshot GUIDs before `zfs send`,
   only transfer missing snapshots. The right protocol for multi-node golden image sync.

9. **Incus's ghost graveyard GC** — soft-delete to `deleted/` namespace, recursive cleanup
   when dependents die. Most automated solution to the clone-dependency problem.
