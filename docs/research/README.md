# forge-metal Research

Research notes on infrastructure (ZFS copy-on-write, Firecracker, secrets management) and
workload optimization (React/Next.js build pipelines, JS toolchain) for forge-metal's
bare-metal CI platform.

## Research areas

### React/Next.js Build Optimization

| Document | Focus |
|----------|-------|
| [React/Next.js Overview](react/) | Build pipeline, toolchain landscape, CI optimization strategies |
| [Build Pipeline](react/build-pipeline.md) | SWC (17x vs Babel), Turbopack (default in v16), `.next` structure, memory pressure |
| [npm & Filesystem](react/npm-and-filesystem.md) | npm ci internals, fsync pressure, ZFS recordsize, pnpm comparison |
| [Toolchain Landscape](react/toolchain-landscape.md) | React Compiler 1.0, oxlint (50-100x), Biome, Vitest, Rust rewrite wave |
| [CI Optimization](react/ci-optimization.md) | Five levers, projected timelines, monorepo patterns, Turborepo/Nx |

### ZFS Snapshot Ecosystem

| Project | Language | What it does | Focus |
|---------|----------|-------------|-------|
| [OBuilder](obuilder.md) | OCaml | ZFS snapshots per build step + runc sandboxing | Build cache, seccomp fsync bypass, crash recovery |
| [DBLab Engine](dblab.md) | Go | Thin-clone Postgres databases via ZFS | Dual-pool rotation, pre-snapshot dance, metadata layer |
| [Velo](velo.md) | TypeScript | Git-like Postgres branching on ZFS | Clone-then-swap, atomic state, CHECKPOINT pattern |
| [go-zfs](go-zfs.md) | Go | ZFS CLI wrapper library | API comparison, go-zfs vs go-libzfs tradeoffs |
| [Impermanence](impermanence.md) | Nix | Ephemeral root filesystem via ZFS rollback-on-boot | Opt-in persistence, boot sequence, CI runner patterns |
| [Incus](incus.md) | Go | System container & VM manager with deep ZFS driver | Ghost graveyard GC, forkzfs namespace isolation, GUID migration, seccomp interceptor |
| [Firecracker](firecracker.md) | N/A | MicroVM memory+CPU snapshots for instant VM resume | Comparison with ZFS approach, tradeoffs for CI |
| [Firecracker Deep Dive](firecracker-vm/) | N/A | REST API, jailer, seccomp, production deployments | Jailer+zvol integration, Fly.io/E2B/Koyeb/Actuated patterns |

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

### React/Next.js CI optimization (from react/ research)

10. **Lockfile-hash deps skip** — compare golden image lockfile hash to PR lockfile. If match,
    skip `npm ci` entirely. ~90% of PRs don't change dependencies. Saves 60-120s.

11. **Turbopack filesystem cache on golden image** — pre-build with
    `turbopackFileSystemCacheForBuild: true`. ZFS clone preserves the cache. Subsequent
    builds only recompile changed modules. Build time: 5-15s instead of 30-120s.

12. **Pre-warm `.eslintcache` and `.tsbuildinfo`** — ESLint and TypeScript both support
    incremental checking. Warm caches in golden image make lint+typecheck near-instant
    for unchanged files.

13. **TypeScript `--skipLibCheck`** — skips type-checking `.d.ts` library files. 43% faster
    type-checking, 24% less memory. Safe for CI (libraries are checked by their own CI).

14. **oxlint as comparison metric** — 50-100x faster than ESLint. Add as a parallel lint
    phase in benchmarks to quantify the "ESLint tax."

### ZFS and infrastructure (from ZFS ecosystem research)

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
