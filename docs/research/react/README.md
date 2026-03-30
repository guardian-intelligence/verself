# React/Next.js Build Optimization Research

Deep technical research on React/Next.js build pipelines, toolchain performance, and CI
optimization strategies for forge-metal's ZFS+Firecracker CI platform.

Conducted 2026-03-30.

## Documents

| Document | Focus |
|----------|-------|
| [Build Pipeline](build-pipeline.md) | Next.js build phases, SWC compiler (17x vs Babel), Turbopack (default in v16), `.next` directory structure, memory pressure, profiling |
| [npm & Filesystem](npm-and-filesystem.md) | `npm ci` internals, node_modules I/O patterns, fsync pressure, ZFS recordsize tuning, pnpm comparison, TypeScript stat() storms |
| [Toolchain Landscape](toolchain-landscape.md) | React Compiler 1.0, oxlint (50-100x vs ESLint), Biome, Vitest, the Rust rewrite wave |
| [CI Optimization](ci-optimization.md) | Five optimization levers, projected timelines, monorepo patterns (Turborepo/Nx), emerging tools |
| [gVisor Filesystem](gvisor-filesystem.md) | Rootfs overlay (82x fsstress speedup), directfs (2x stat), dcache tuning, fsync behavior in gVisor |
| [Competitive Landscape](competitive-landscape.md) | Vercel Hive (Firecracker on bare metal), WunderGraph (13s builds), E2B snapshots, isolation benchmarks |
| [Node I/O Patterns](node-io-patterns.md) | npm ci syscall counts (measured), fsync analysis, zvol volblocksize, package manager I/O, memory profiling |

## Key findings

**The build pipeline has five distinct bottlenecks**, each with a different optimization:

| Phase | Bottleneck | Optimization | Expected speedup |
|-------|-----------|-------------|-----------------|
| deps (`npm ci`) | fsync + tarball extraction | Lockfile-hash skip OR fsync interception | 0s (skip) or 2-3x |
| lint (ESLint) | Single-threaded JS parser | ESLint cache or oxlint replacement | 5-100x |
| typecheck (`tsc`) | Memory-heavy type inference | `--incremental` + `--skipLibCheck` + `.tsbuildinfo` | 2-5x |
| build (`next build`) | Module bundling | Turbopack filesystem cache | 3-10x |
| test (Jest) | Startup overhead | Vitest or Bun test | 5-50x |

**Turbopack is now the default bundler in Next.js 16.** This changes the caching model
entirely. Turbopack's filesystem cache (`turbopackFileSystemCacheForBuild`) is opt-in
but provides function-level memoization. Webpack's disk cache is coarser.

**The Rust rewrite wave is real.** Every major JS tool now has a 10-100x faster Rust
replacement. The remaining irreducible bottleneck is `tsc --noEmit` — no Rust replacement
exists for TypeScript's type-checker. TypeScript's `--incremental` mode is the only
mitigation.

**ZFS clone gives forge-metal a structural advantage over traditional CI.** GitHub Actions
spends 30-60s downloading/uploading cache artifacts per job. forge-metal's golden image
provides the same caches via a 1.7ms ZFS clone. This is a ~20,000x improvement on cache
restore time.

**gVisor filesystem configuration is make-or-break.** Without rootfs overlay, gVisor's
filesystem overhead is 80x+ for write-heavy workloads (fsstress: 262s → 3.18s with overlay).
Directfs makes `stat(2)` 2x faster — critical for TypeScript's 30K-45K stat() calls during
module resolution. Default dcache (1000 entries) is too small for node_modules (50K+ files).

**Vercel uses the same stack.** Vercel's Hive build platform uses Firecracker on bare metal,
the same approach as forge-metal. But Vercel doesn't use ZFS — their pre-warming takes seconds
vs. forge-metal's 1.7ms ZFS clone. WunderGraph achieved 13s commit-to-production on Fly.io
Machines (Firecracker) using OverlayFS caching.

**The #1 optimization is skipping deps.** ~90% of PRs don't change `package-lock.json`.
Comparing lockfile hashes and skipping `npm ci` when unchanged eliminates the most
expensive phase. forge-metal already has the `lockfile_changed` field in CIEvent.

## Cross-cutting patterns with other research

**fsync interception** — documented in [OBuilder research](../obuilder.md) for CI workloads.
npm's `write-file-atomic` calls fsync on every package file. Intercepting this via gVisor's
seccomp is safe for ephemeral CI (if crash → destroy clone, no data integrity risk).

**Cache-in-golden-image** — analogous to [DBLab's warm snapshot approach](../dblab.md).
Pre-populate `.next/cache`, `.eslintcache`, `.tsbuildinfo`, `node_modules` in the golden
zvol. Every clone starts with warm caches.

**Incremental builds on COW clones** — the ZFS clone is structurally a "cached previous
build." Source file changes trigger COW only for modified blocks. Build tools see the
full previous build output and can do incremental compilation.

## Projected optimized pipeline

For a 1000-line PR on cal.com (worst case):

| Phase | Current | Optimized | Lever |
|-------|---------|-----------|-------|
| ZFS clone | 2ms | 2ms | — |
| git checkout | 5-10s | 2-5s | shallow + sparse |
| deps | 60-120s | **0s** | lockfile skip |
| lint | 15-30s | **1-5s** | cache or oxlint |
| typecheck | 15-45s | **3-10s** | incremental |
| build | 30-120s | **5-15s** | Turbopack cache |
| test | 10-30s | **2-10s** | Vitest / cache |
| **Total** | **135-355s** | **13-45s** | **5-10x** |
