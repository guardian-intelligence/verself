# Golden Image Warming Strategy for Next.js CI

> Concrete implementation plan for pre-warming the golden ZFS zvol with all caches
> needed for fast incremental CI builds. This document synthesizes findings from the
> entire React research corpus into an actionable specification.
>
> Conducted 2026-03-30.

## The golden image advantage

Traditional CI (GitHub Actions, CircleCI, etc.) restores caches per-job:
1. Download cache artifact (30-60s for large caches)
2. Extract to disk (10-30s)
3. Run build (uses cache)
4. Upload updated cache (20-40s)

**forge-metal eliminates steps 1, 2, and 4.** The golden zvol IS the cache. ZFS clone
makes it available in ~1.7ms. No network transfer, no extraction, no upload.

But this only works if the golden image contains the right caches.

## What to pre-warm

### Layer 1: System-level caches

| Cache | Path | What it does | Pre-warming step |
|-------|------|-------------|-----------------|
| npm cache | `~/.npm/_cacache/` | Content-addressable tarball store | `npm ci` for each project |
| Node.js compile cache | `~/.cache/node-compile-cache/` | V8 bytecode for required modules | Set `NODE_COMPILE_CACHE` + run tools |
| V8 code cache | (automatic in Node.js 22+) | Compiled bytecode for `.js` files | First run of each tool caches it |

**NODE_COMPILE_CACHE (Node.js 22+):** Set `NODE_COMPILE_CACHE=/path/to/cache` in the
golden image's environment. When Node.js compiles a CJS or ESM module, it persists V8
bytecode to disk. Subsequent loads skip parsing+compilation.

Measured impact: 130ms → 80ms for a TypeScript fixture (~38% startup reduction).
Source: [Node.js 22.1.0 release](https://nodejs.org/en/blog/release/v22.1.0)

**Pre-warming:** Run each CI tool once in the golden image with NODE_COMPILE_CACHE set:
```bash
export NODE_COMPILE_CACHE=/var/cache/node-compile-cache

# Warm npm
npm --version

# Warm eslint
cd /workspaces/taxonomy && npx eslint --version

# Warm tsc
cd /workspaces/taxonomy && npx tsc --version

# Warm next
cd /workspaces/taxonomy && npx next --version
```

This caches the V8 bytecode for npm's ~200 modules, ESLint's ~150 modules, TypeScript's
~50 modules, and Next.js's ~300 modules. Total: ~700 module compilations saved per job.

### Layer 2: Per-project caches

For each benchmark project in the golden image:

| Cache | Path | What it does | Pre-warming step |
|-------|------|-------------|-----------------|
| node_modules | `<project>/node_modules/` | Installed dependencies | `npm ci` |
| lockfile hash | `<project>/.lockfile-hash` | Golden image lockfile fingerprint | `sha256sum package-lock.json` |
| Next.js build cache | `<project>/.next/cache/` | Webpack/Turbopack compilation cache | `npm run build` |
| ESLint cache | `<project>/.eslintcache` | Per-file lint results | `npm run lint` |
| TypeScript buildinfo | `<project>/tsconfig.tsbuildinfo` | Incremental type-check state | `npx tsc --noEmit --incremental` |

### Layer 3: Project-specific setup

#### next-learn (small)
```bash
cd /workspaces/next-learn/dashboard/final-example
npm ci
npm run lint        # warms .eslintcache
npm run build       # warms .next/cache
sha256sum package-lock.json > .lockfile-hash
```

#### taxonomy (medium)
```bash
cd /workspaces/taxonomy
npm ci
npm run lint        # warms .eslintcache
npx tsc --noEmit --incremental  # warms tsconfig.tsbuildinfo
npm run build       # warms .next/cache
sha256sum package-lock.json > .lockfile-hash
```

#### cal.com (large monorepo)
```bash
cd /workspaces/cal.com
npm ci
npm run lint        # warms .eslintcache
npx tsc --noEmit --incremental  # warms tsconfig.tsbuildinfo
npm run build       # warms apps/web/.next/cache
npm test -- --passWithNoTests   # warms Jest cache
sha256sum package-lock.json > .lockfile-hash
```

## React Server Components: build-time implications

RSC changes the compilation model. Bundlers (Turbopack/webpack) must:

1. **Build a unified module graph** spanning server and client environments
2. **Identify `"use client"` boundaries** — these mark environment transitions
3. **Create Client References** — server-side imports of client components are replaced
   with serializable reference objects containing module ID, export name, and bundle URLs
4. **Generate RSC Payload format** — the "Flight" protocol serializes the component tree
   for streaming to the client

Source: [How Parcel bundles RSC](https://devongovett.me/blog/parcel-rsc.html)

**Impact on golden image:** The unified graph is part of the build cache (`.next/cache`).
Pre-building with RSC-aware Turbopack means the module graph, Client References, and
Flight metadata are all cached. Incremental builds only reprocess components whose source
changed — the server/client boundary analysis is cached.

**Security note:** CVE-2025-55182 demonstrated a critical RCE via Flight payload
deserialization in React Server Components. CI workloads running untrusted code (PR builds)
should run inside Firecracker VMs with gVisor — the double isolation prevents exploitation
even if the Flight protocol has vulnerabilities.
Source: [CVE-2025-55182](https://www.offsec.com/blog/cve-2025-55182/)

## Cal.com monorepo structure

Cal.com is a Turborepo monorepo. Understanding its structure informs the golden image:

```
cal.com/
├── apps/
│   ├── web/           ← Main app (Next.js, app.cal.com) — THIS is the benchmark target
│   ├── website/       ← Marketing site
│   ├── api/           ← Public API
│   ├── swagger/       ← OpenAPI spec
│   └── docs/          ← Documentation
├── packages/
│   ├── ui/            ← Shared React components (@calcom/ui)
│   ├── lib/           ← Shared utilities
│   ├── prisma/        ← Database client
│   ├── trpc/          ← API layer
│   └── ...            ← 50+ internal packages
└── turbo.json         ← Task dependency graph
```

Source: [Cal.com Handbook](https://handbook.cal.com/engineering/codebase/monorepo-turborepo)

**Key for golden image:** When building `apps/web`, Turborepo first builds all dependent
packages (ui, lib, prisma, trpc, etc.). The golden image should have ALL package build
outputs pre-cached, not just `apps/web/.next/cache`.

**Turborepo local cache:** Turborepo stores task outputs in `node_modules/.cache/turbo/`.
Pre-warming this cache means subsequent `turbo run build --filter=apps/web` will find
cached outputs for all unchanged packages.

## Firecracker io_uring for disk I/O

Firecracker has an experimental io_uring async disk backend (PR #2754, merged 2021).
Current status: Developer Preview.

**Problem:** Default Firecracker serializes guest I/O requests. With NVMe backing, this
limits throughput to 4-5K IOPS — well below what NVMe can deliver.

**io_uring solution:** Parallelize block I/O using the Linux io_uring interface (kernel
5.1+). Expected to significantly improve IOPS for write-heavy workloads like `npm ci`.

Source: [firecracker-microvm/firecracker#1600](https://github.com/firecracker-microvm/firecracker/issues/1600)

**Applicability to forge-metal:** When io_uring backend is stable, enable it for all CI
VMs. The write-heavy `npm ci` and `next build` phases would benefit most. Monitor
Firecracker releases for graduation from Developer Preview.

## CI job flow on warm golden image

```
1. ZFS clone golden-zvol@ready → ci/job-abc          (~1.7ms)
2. Boot Firecracker VM with /dev/zvol/pool/ci/job-abc (~125ms snapshot, ~3s cold)
3. git fetch origin PR-ref && git checkout PR-sha     (2-5s, sparse if possible)
4. Compare lockfile hash:
   if sha256sum(package-lock.json) == cat(.lockfile-hash):
     → Skip npm ci (use golden image's node_modules)  (0s)
   else:
     → npm install (not npm ci — preserves existing)   (7-30s with warm cache)
5. ESLint:
   → eslint . (uses .eslintcache, only re-lints changed files)  (1-5s)
6. TypeScript:
   → tsc --noEmit --incremental (uses .tsbuildinfo)   (3-10s)
7. Next.js build:
   → next build (uses .next/cache, Turbopack cache)   (5-15s)
   (with typescript.ignoreBuildErrors: true — already checked in step 6)
8. Tests:
   → npm test (Jest/Vitest, only affected tests)      (2-10s)
9. Collect metrics:
   → zfs get written, cgroup stats, phase timings     (<1s)
10. VM exits, zfs destroy ci/job-abc                  (<1s)

Total: 14-70s (vs 135-355s baseline)
```

## Golden image refresh cadence

The golden image needs periodic refresh to stay useful:

| Event | Action |
|-------|--------|
| Lockfile change in main | Rebuild golden image with new `npm ci` |
| Major Next.js version bump | Rebuild — cache format may change |
| Weekly (even if no changes) | Rebuild — npm packages get security updates |
| Node.js version change | Rebuild — NODE_COMPILE_CACHE is version-specific |

**Refresh process (using DBLab's dual-pool rotation pattern):**
1. Build new golden image on a separate zvol
2. Run smoke test (build all projects, verify outputs)
3. Snapshot as `golden-zvol-new@ready`
4. Atomically swap: rename `golden-zvol` → `golden-zvol-old`, `golden-zvol-new` → `golden-zvol`
5. New clones use the fresh image; existing clones continue on old
6. Destroy old zvol when all dependent clones are released

This is the DBLab dual-pool rotation technique applied to forge-metal.
See `docs/research/dblab.md`.

## Environment variables for CI jobs

Set in the golden image's environment and inherited by all CI clones:

```bash
# Node.js optimization
NODE_COMPILE_CACHE=/var/cache/node-compile-cache
NODE_OPTIONS="--max-old-space-size=4096"

# Next.js CI mode
CI=true
NODE_ENV=production
NEXT_TELEMETRY_DISABLED=1

# TypeScript optimization
TSC_WATCHFILE=UseFsEventsWithFallbackDynamicPolling

# npm optimization
npm_config_prefer_offline=true
npm_config_audit=false
npm_config_fund=false

# Suppress interactive prompts
DEBIAN_FRONTEND=noninteractive
```

## Metrics to validate cache effectiveness

Track these in CIEvent to measure golden image quality:

| Metric | Good value | Action if bad |
|--------|-----------|--------------|
| `lockfile_changed` = 0 | >90% of jobs | Golden image lockfile matches most PRs |
| `npm_cache_hit` = 1 | >90% when lockfile unchanged | node_modules present and valid |
| `next_cache_hit` = 1 | >80% | .next/cache present and matching |
| `tsc_cache_hit` = 1 | >80% | .tsbuildinfo present and valid |
| `deps_install_ns` < 1s | When lockfile unchanged | Skip is working |
| `golden_age_hours` < 168 | Always (7 day max) | Refresh cadence is working |
| `zfs_written_bytes` | Track trend | Lower = more COW sharing = better golden image |

## Applicability to forge-metal

1. **Implement the three-layer warming.** System caches (NODE_COMPILE_CACHE), per-project
   caches (node_modules, .next/cache, .eslintcache, .tsbuildinfo), and Turborepo local
   cache (for cal.com).

2. **Use `npm install` instead of `npm ci`** in CI jobs. `npm ci` destroys the golden
   image's pre-populated `node_modules`. `npm install` preserves it and only updates
   changed packages.

3. **Set NODE_COMPILE_CACHE in golden image.** Free 38% startup reduction for every
   Node.js tool invocation (npm, eslint, tsc, next). No code changes needed.

4. **Disable Next.js type-checking during build.** Since `tsc --noEmit` runs as a separate
   phase, set `typescript.ignoreBuildErrors: true` in next.config.js to avoid double
   type-checking and save 300-500MB peak memory.

5. **Track golden image age.** `golden_age_hours` in CIEvent already exists. Alert if >168h
   (7 days) — stale golden images have low cache hit rates.

6. **Use DBLab's dual-pool rotation** for golden image refresh. Zero-downtime swap, existing
   clones unaffected.
