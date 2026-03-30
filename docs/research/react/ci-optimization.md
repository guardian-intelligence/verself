# CI Optimization for React/Next.js Workloads

> Concrete strategies for reducing CI time on the forge-metal platform, informed by
> how Vercel, Turborepo, and open-source projects optimize their builds.
>
> Sources: Next.js CI docs, Turborepo docs, GitHub Actions guides, project-specific CI configs
>
> Conducted 2026-03-30.

## Current benchmark targets

forge-metal's updated KPI: **p99.9 for a 1000-line PR change in a JS/TS monorepo.**

The three benchmark projects represent the spectrum:

| Project | Complexity | Phases | Expected baseline CI time |
|---------|-----------|--------|---------------------------|
| next-learn | Small (tutorial) | deps, lint, build | 1-3 min |
| taxonomy | Medium (shadcn/ui demo) | deps, lint, typecheck, build | 3-5 min |
| cal.com | Large monorepo | deps, lint, typecheck, build, test | 5-15 min |

## Next.js CI caching — the official strategy

Next.js stores build cache at `.next/cache`. The official cache key strategy:

```yaml
# GitHub Actions recommended config
key: ${{ runner.os }}-nextjs-${{ hashFiles('**/package-lock.json') }}-${{ hashFiles('**/*.js', '**/*.jsx', '**/*.ts', '**/*.tsx') }}
restore-keys: |
  ${{ runner.os }}-nextjs-${{ hashFiles('**/package-lock.json') }}-
```

**What this means:** The cache is keyed on lockfile hash + source hash. If only source
files change (typical PR), the cache partially hits — npm modules are cached, and Next.js
can do incremental compilation.

**ZFS clone advantage:** Instead of downloading/uploading cache artifacts (GitHub Actions
caching takes 30-60s for large caches), the golden image already has the full cache on
disk. ZFS clone makes it available in ~1.7ms. This eliminates the cache restore overhead
entirely.

## The five optimization levers

### Lever 1: Skip deps when lockfile unchanged

This is the single highest-impact optimization for most PRs.

**How it works:**
1. Golden image has `node_modules` + lockfile hash stored
2. CI job compares PR's `package-lock.json` hash to golden image's
3. If match → skip `npm ci` entirely (use pre-warmed `node_modules`)
4. If mismatch → run `npm ci` (with fsync bypass for speed)

**Expected impact:** Eliminates 30-120s for ~90% of PRs (most PRs don't change dependencies).

**forge-metal already tracks this:** `CIEvent.LockfileChanged` field exists. The
optimization is wiring it into the job runner.

### Lever 2: fsync interception for npm ci

When deps must be installed, intercept sync syscalls at the gVisor/seccomp level:

```
fsync, fdatasync, msync, sync, syncfs, sync_file_range → return 0 (success)
```

**Safety:** CI results are ephemeral. If the VM crashes mid-install, the entire clone is
destroyed. No data integrity risk.

**Proven by:** OBuilder (see `docs/research/obuilder.md`). Their seccomp policy returns
`errnoRet: 0` for all sync syscalls. Requires runc >= v1.0.0-rc92.

**gVisor variant:** gVisor already intercepts all syscalls. The `fsync` handler in gVisor
can be configured to no-op via a custom platform or seccomp filter.

**Expected impact:** 30-50% reduction in `npm ci` time based on OBuilder's experience with
npm workloads.

### Lever 3: Pre-warm all build caches in golden image

The golden image should contain warm caches for every benchmark project:

```
/workspaces/
├── next-learn/
│   ├── node_modules/       ← pre-installed
│   ├── .next/cache/        ← pre-built
│   ├── .eslintcache         ← pre-linted
│   └── .tsbuildinfo         ← pre-typechecked (if applicable)
├── taxonomy/
│   ├── node_modules/
│   ├── .next/cache/
│   ├── .eslintcache
│   └── tsconfig.tsbuildinfo
└── cal.com/
    ├── node_modules/
    ├── apps/web/.next/cache/
    ├── .eslintcache
    └── tsconfig.tsbuildinfo
```

**CI job flow with warm caches:**
1. ZFS clone golden image → get pre-warmed workspace
2. `git fetch` + `git checkout PR-branch` → only changed files differ
3. `npm ci` → skip if lockfile unchanged
4. `npm run lint` → ESLint cache hit for unchanged files
5. `npx tsc --noEmit` → incremental mode, only check changed files + dependents
6. `npm run build` → Turbopack/webpack cache hit for unchanged modules
7. `npm test` → run only affected tests (if test runner supports it)

**Expected impact:** Steps 4-6 become incremental (10-50% of cold time) for typical PRs.

### Lever 4: Parallel phase execution

Some CI phases can run in parallel:

```
                    ┌── lint ──────┐
ZFS clone → deps →  ├── typecheck ─┤ → build → test
                    └──────────────┘
```

Lint and typecheck are independent of each other. Both must complete before `build`
(Next.js build includes its own type-check step, but we can disable that).

**Implementation:** Run lint and typecheck as separate processes in the same cgroup.
Aggregate exit codes. Only proceed to build if both pass (or always proceed, since
forge-metal collects timing data for all phases regardless of exit code).

**Expected impact:** Overlapping lint + typecheck saves the shorter of the two (typically
lint is faster, saving 5-15s).

### Lever 5: Turbopack filesystem cache for builds

Enable `experimental.turbopackFileSystemCacheForBuild` in benchmark projects' `next.config.js`.

**How it works:** Turbopack persists its computation graph to `.next/cache`. On the next
build, it only recomputes modules whose source files changed.

**Cache on golden image:** Pre-build the project on the golden image with Turbopack cache
enabled. The cache persists through the ZFS clone. PR builds only recompile changed files.

**Expected impact:** For a 1000-line PR touching 5-20 files in a 500-file project,
Turbopack should skip compilation for 480-495 unchanged files (~95-99% cache hit).
Build time: 5-10s instead of 30-120s.

## Monorepo-specific optimizations

### Turborepo (used by cal.com)

[Turborepo](https://turborepo.dev/) is Vercel's monorepo build system. It wraps npm scripts
with:
- **Task graph:** Defines dependencies between tasks (e.g., `build` depends on `^build`)
- **Content-aware hashing:** Cache key = hash of source files + env vars + dependencies
- **Remote caching:** Share cache across machines via Vercel's servers

**`--filter` for affected packages:**
```bash
turbo run build --filter='...[origin/main]'
```
Only builds packages changed since `origin/main` and their dependents.

**Impact numbers:**
- Real-world: 20 min → 2 min CI with Turborepo caching (10x)
- Filter reduces affected jobs from 90 → 8 for a typical PR

**Applicability to forge-metal:** Turborepo's remote cache is external (Vercel servers).
forge-metal's ZFS clone approach provides a local equivalent — the golden image IS the
cache. For cal.com benchmarks, Turborepo's `--filter` could be used to only build
affected packages, but this requires knowing the PR's changed files at job start.

### Nx

[Nx](https://nx.dev/) is an alternative to Turborepo with similar features:
- Task graph + affected command (`nx affected:build`)
- Computation caching (local + remote)
- **Distributed task execution:** Split tasks across multiple machines

**Key difference from Turborepo:** Nx's affected analysis uses a project graph and git diff
to determine exactly which projects are affected. This is more precise than file-hash-based
caching.

## Emerging CI acceleration patterns

### Bun as drop-in replacement

[Bun](https://bun.sh/) can replace npm and Node.js in CI:
- `bun install`: Fastest package installer (~2x faster than pnpm, ~4x faster than npm ci)
- `bun test`: Built-in test runner, 20-50x faster than Jest for startup
- `bun run`: Script runner with no overhead (npm/npx add ~200ms per invocation)

**Limitation:** Next.js does not officially support Bun runtime. `bun install` works as a
package manager, but `next build` still runs on Node.js.

### oxlint + Biome for lint+format

Replace ESLint+Prettier with oxlint (lint) or Biome (lint+format):
- ESLint: 20-45s for 10K files → oxlint: 0.2-0.5s (100x faster)
- Prettier: 12s for 10K files → Biome: 0.3s (40x faster)
- Combined: 32-57s → 0.5-0.8s

**Caveat:** Framework-specific rules (`eslint-plugin-next`) are not yet available in
oxlint/Biome. For Next.js projects, either keep ESLint for those rules or accept reduced
coverage.

### Vitest for testing

For projects migrating from Jest to Vitest:
- 5-10x faster for large suites (parallel workers, smart re-runs)
- Native TypeScript support (no `ts-jest` transform overhead)
- `vitest --changed` runs only tests affected by git changes

### Incremental TypeScript in CI

TypeScript's `--incremental` flag with `.tsbuildinfo`:
- First run: Full check, writes `.tsbuildinfo`
- Subsequent runs: Only re-check changed files and dependents
- **Requirement:** `.tsbuildinfo` must persist between builds (golden image)
- **Measured impact:** 10-50% faster for typical PR changes

### Git clone optimization

The `git clone` step in CI is often overlooked but can take 5-30s for large repos.
forge-metal's golden image already has the repo, but PR builds need the latest changes.

**Strategies (from fastest to slowest):**

| Strategy | Command | Data transferred | Best for |
|----------|---------|-----------------|----------|
| Shallow fetch | `git fetch --depth=1 origin PR-ref` | Minimal (tip commit only) | All CI jobs |
| Treeless clone | `git clone --filter=tree:0` | Commits only, trees/blobs on demand | First clone |
| Blobless clone | `git clone --filter=blob:none` | Commits + trees, blobs on demand | Needs git log |
| Sparse checkout | `git sparse-checkout set apps/web` | Only selected paths | Monorepos |

**Real-world results:**
- GitLab website repo: full clone 6m26s → treeless clone **6.49s** (98.3% reduction)
- Chromium repo: 55.7 GB → 850 MB with treeless clone (93% reduction)

Source: [GitHub Blog: Partial Clone](https://github.blog/open-source/git/get-up-to-speed-with-partial-clone-and-shallow-clone/)

**forge-metal approach:** The golden image has a full clone. PR jobs do:
```bash
git fetch --depth=1 origin refs/pull/123/head:pr-branch
git checkout pr-branch
```
This fetches only the PR tip commit (~1-3s), not the full history. The golden image's
existing objects serve as a local cache for any shared blobs.

**Sparse checkout for cal.com:** Since the benchmark only builds `apps/web`, sparse
checkout could skip fetching 80% of the monorepo:
```bash
git sparse-checkout set apps/web packages/
```

## Putting it all together — projected CI timeline

For a 1000-line PR on cal.com (worst case benchmark):

**Current (no optimizations):**
| Phase | Time |
|-------|------|
| ZFS clone | 2ms |
| git clone | 5-10s |
| npm ci | 60-120s |
| lint | 15-30s |
| typecheck | 15-45s |
| build | 30-120s |
| test | 10-30s |
| cleanup | 1s |
| **Total** | **135-355s** |

**Optimized (all levers applied):**
| Phase | Time | Optimization |
|-------|------|-------------|
| ZFS clone | 2ms | (baseline) |
| git fetch + checkout | 2-5s | Shallow clone, sparse checkout |
| deps | 0s OR 15-30s | Skip if lockfile unchanged / fsync bypass |
| lint | 1-5s | ESLint cache (warm) or oxlint |
| typecheck | 3-10s | tsc --incremental + skipLibCheck |
| build | 5-15s | Turbopack filesystem cache |
| test | 2-10s | Vitest or Jest with cache |
| cleanup | 1s | (baseline) |
| **Total** | **14-76s** | **5-10x faster** |

**For the KPI (p99.9 of 1000-line PR):** The optimized pipeline targets <60s for typical
PRs and <120s for worst-case (dependency change + large monorepo). The golden image
approach with ZFS clones eliminates the caching overhead that plagues traditional CI
(downloading/uploading 200MB+ cache artifacts).

## What to measure

Metrics that matter for tracking optimization progress (already in CIEvent):

| Metric | Field | Why |
|--------|-------|-----|
| Lockfile change rate | `lockfile_changed` | What % of jobs can skip deps? |
| npm cache hit | `npm_cache_hit` | How often does the golden image's cache help? |
| Next.js cache hit | `next_cache_hit` | Build cache effectiveness |
| tsc cache hit | `tsc_cache_hit` | Typecheck cache effectiveness |
| ZFS bytes written | `zfs_written_bytes` | COW overhead per job |
| Peak memory | `memory_peak_bytes` | Are builds OOMing? |
| I/O bytes | `io_read_bytes`, `io_write_bytes` | Which phase is I/O bound? |
| Per-phase timing | `deps_install_ns` through `test_ns` | Where does time go? |

## Applicability to forge-metal

1. **Implement lockfile-hash skip first.** Highest impact, lowest complexity. Compare
   golden image lockfile hash to PR lockfile at job start. If match, skip deps entirely.

2. **Pre-warm all caches in golden image.** Build each benchmark project once on the golden
   image with all caches enabled. Snapshot after warming.

3. **Enable fsync interception in gVisor.** Apply to all CI phases, not just deps. Build
   tools also call fsync when writing `.next/` output.

4. **Run lint + typecheck in parallel.** Simple process-level parallelism in the job runner.
   Saves the overlap time.

5. **Enable Turbopack build cache.** Set `turbopackFileSystemCacheForBuild: true` in
   benchmark projects. Test cache effectiveness on ZFS clones.

6. **Track "optimized vs baseline" in CIEvent.** Add a field indicating whether the job
   used optimized settings (fsync bypass, cache skip, parallel phases). Compare distributions
   in ClickHouse.
