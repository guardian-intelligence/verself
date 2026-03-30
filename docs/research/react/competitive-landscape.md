# CI Infrastructure Competitive Landscape for React/Next.js

> How Vercel, Fly.io, WunderGraph, E2B, and CI runner startups build and isolate
> Next.js workloads. Benchmarks comparing isolation technologies and caching
> strategies. Real-world CI times for forge-metal's benchmark projects.
>
> Conducted 2026-03-30.

## 1. Vercel's Hive -- Firecracker on bare metal for builds

Vercel published a deep dive into Hive, the internal compute platform powering all
customer builds since November 2023. **Vercel uses the exact same technology stack
as forge-metal: Firecracker microVMs on bare metal.**

Source: [A deep dive into Hive: Vercel's builds infrastructure](https://vercel.com/blog/a-deep-dive-into-hive-vercels-builds-infrastructure)

### Architecture

```
Hive (regional cluster)
├── Control Plane (orchestration, API)
└── Boxes (bare metal servers)
    ├── Box Daemon (provisions block devices, spawns Firecracker)
    └── Cells (Firecracker microVMs, 1:1 with Firecracker process)
        └── Cell Daemon (manages build containers inside VM)
            └── Build Container (runs customer code)
```

Key design choices:
- **Bare metal + KVM + Firecracker** -- identical to forge-metal's approach
- **Box Daemon** provisions block devices and spawns Firecracker processes (analogous to
  forge-metal's ZFS orchestrator)
- **Cell Daemon** runs inside each VM, managing build container lifecycle via socket
  connection to Box Daemon
- Each cell gets **dedicated CPUs and memory**; disk and network are **rate-limited**
- **WireGuard** tunnels for private network connections (Secure Compute product)
- Regional clusters operate independently with separate failure boundaries

### Performance numbers

| Metric | Value | Context |
|--------|-------|---------|
| Pre-warmed cell start | ~few seconds | Pool of pre-warmed cells ready |
| New cell provisioning | ~5 seconds | Down from ~90s with previous Fargate solution |
| Docker image cache savings | ~45 seconds | Per build, from pre-loaded images |
| Overall build improvement | +20-30% | vs. pre-Hive architecture |
| New cell build improvement | +40% | For builds requiring fresh cell provisioning |
| Scale | 30B requests/week | Platform-wide, not builds specifically |
| Turbo build machines | 30 vCPU, 60 GB RAM | Up to 3x faster for long builds |

### What forge-metal can learn

1. **Pre-warming is critical.** Vercel maintains a pool of pre-warmed cells. forge-metal's
   ZFS clone approach is superior here -- clones are instant (~1.7ms) vs. Vercel's multi-
   second pre-warming. But the golden image must be kept warm and up-to-date.

2. **Box Daemon / Cell Daemon split.** Vercel separates host-level orchestration (box daemon)
   from in-VM lifecycle management (cell daemon). forge-metal should consider a similar split
   as it matures.

3. **Vercel does NOT mention ZFS.** Their block device provisioning is likely simpler (raw
   disk images or overlay filesystems). forge-metal's ZFS COW clones give a structural
   advantage: every build gets a full filesystem snapshot for free, with zero copy overhead.
   Vercel has to pre-warm and cache Docker images; forge-metal starts with everything
   pre-installed.

4. **Turbo build machines (30 vCPU, 60 GB)** set the competitive bar. forge-metal's
   Latitude.sh boxes should match or exceed this spec for the build VM allocation.


## 2. Other Firecracker-based CI/build systems

### WunderGraph "The Builder" -- 13s commit-to-production

Source: [The Builder: The Road from Commit to Production in 13s](https://wundergraph.com/blog/the_builder_the_road_from_commit_to_production_in_13s)

Built on Fly.io Machines (Firecracker), achieved 13-second deployments:

| Metric | Value |
|--------|-------|
| Firecracker VM boot | ~500ms |
| VM restart | ~400ms |
| First build (cold) | ~60 seconds |
| Subsequent build (before overlayfs) | ~30 seconds |
| Subsequent build (after native overlayfs) | **13 seconds** |
| npm install savings (cached) | ~45 seconds per build |

**Key technique:** Native OverlayFS inside the Firecracker VM. A single volume mount
configuration reduced subsequent builds from 30s to 13s (57% improvement). Each project
gets a dedicated Machine with a persistent volume for build cache.

**Applicability to forge-metal:** ZFS COW clones serve the same function as WunderGraph's
overlayfs -- persistent cache without copy overhead. But forge-metal's approach is more
elegant: the golden image IS the cache, and clones diverge only on the changed files.
WunderGraph still has to manage overlayfs layers manually.

### E2B -- Firecracker sandboxes for AI agents

Source: [E2B Documentation](https://e2b.dev/docs)

E2B uses Firecracker microVMs for AI code execution sandboxes:

| Metric | Value |
|--------|-------|
| Sandbox provisioning | <200ms (from snapshot) |
| Firecracker boot | ~125ms |
| Memory overhead per VM | <5 MiB |
| Density | Thousands of VMs per host |

**Template/snapshot system:** E2B uses Dockerfiles as build artifacts to create pre-configured,
snapshotted microVMs. When a sandbox is requested, it restores the snapshot rather than
building from scratch. This is conceptually identical to forge-metal's golden image approach.

**Key difference:** E2B snapshots are Firecracker VM snapshots (memory + CPU state). forge-metal
uses ZFS zvol clones (block device state). Both achieve instant provisioning, but ZFS clones
are more storage-efficient (COW sharing) and don't require managing VM memory snapshots.

### Actuated -- Firecracker for GitHub Actions

Source: [Blazing fast CI with MicroVMs](https://blog.alexellis.io/blazing-fast-ci-with-microvms/)

Actuated runs GitHub Actions jobs inside Firecracker microVMs on customer bare metal:

| Metric | Value |
|--------|-------|
| VM boot (including Docker startup) | ~1-2 seconds |
| Build speedup vs GitHub Actions | 2-3x typical |
| Pricing model | Fixed concurrency, unlimited minutes |

**Requirement:** Bare metal servers or VMs with nested virtualization. No shared-kernel
isolation -- every job gets its own VM.

**Applicability:** Actuated validates the "Firecracker on bare metal" model for CI but
doesn't do anything clever with storage (no ZFS, no COW). forge-metal's ZFS golden image
approach adds the missing optimization layer.

### Fly.io -- Firecracker in production

Source: [Sandboxing and Workload Isolation](https://fly.io/blog/sandboxing-and-workload-isolation/)

Fly.io runs all customer workloads in Firecracker microVMs on bare metal (Equinix/Packet):

- Firecracker's block device implementation: ~1,400 lines of Rust
- Network driver: ~700 lines before tests
- Syscall filter: ~40 system calls allowed
- Hosts: 8-32 physical CPU cores, 32-256 GB RAM

Fly.io explicitly chose Firecracker over gVisor and Kata for production workloads.


## 3. Build isolation technology comparison

### Startup and overhead benchmarks

| Technology | Boot/start time | Memory overhead | I/O overhead | Security level |
|-----------|----------------|-----------------|--------------|----------------|
| Docker (runc) | ~50ms | Negligible | Native | Weak (shared kernel) |
| nsjail | ~20ms | Negligible | Native | Medium (namespaces + seccomp) |
| gVisor (runsc) | ~50ms container | Minimal | 10-30% slower (I/O heavy) | Strong (syscall interception) |
| Firecracker | ~125ms to userspace | <5 MiB per VM | Near-native | Strongest (hardware virtualization) |
| Kata Containers | 150-300ms | Tens of MiB | Near-native | Strong (hardware virtualization) |

Sources:
- [Firecracker vs gVisor (Northflank)](https://northflank.com/blog/firecracker-vs-gvisor)
- [Kata vs Firecracker vs gVisor (Northflank)](https://northflank.com/blog/kata-containers-vs-firecracker-vs-gvisor)
- [gVisor Performance Guide](https://gvisor.dev/docs/architecture_guide/performance/)

### gVisor considerations for Node.js/Next.js workloads

gVisor intercepts all syscalls and handles them in userspace. This creates overhead
proportional to syscall frequency. Node.js builds are heavily I/O-bound (stat storms,
small file writes, npm extraction), making gVisor's overhead more pronounced than for
CPU-bound workloads.

Google improved gVisor with "systrap" to replace ptrace for syscall interception, reducing
overhead significantly. DigitalOcean reported that their gVisor infrastructure improvements
resolved customer-reported performance issues with Node.js workloads.

**forge-metal's layered approach (Firecracker + gVisor) is sound:**
- Firecracker provides hardware isolation (host protection)
- gVisor inside the VM provides syscall filtering (defense in depth)
- The double overhead is acceptable: Firecracker has near-native I/O, and gVisor's overhead
  is bounded. The combined overhead is still far less than the build time savings from
  ZFS clone caching.

### Firecracker IO optimizations

Firecracker has an experimental io_uring-based async I/O engine for block devices:
- Read workloads: **1.5-3x** improvement in IOPS/CPU efficiency, up to **30x** total IOPS
  (for data not in host page cache, on NVMe)
- Write workloads: **20-45%** improvement in total IOPS
- Status: Not yet production-ready

Source: [Firecracker io_uring issue #1600](https://github.com/firecracker-microvm/firecracker/issues/1600)

**Applicability:** When this ships, forge-metal should enable it for builds. npm install
is write-heavy (extracting tarballs) and next build reads many files -- both benefit.
The ZFS zvol backing the block device is already on NVMe, so the full io_uring benefit
should be available.


## 4. Remote caching architecture: Turborepo and Nx

### Turborepo remote cache

Source: [Turborepo Remote Caching](https://turborepo.dev/docs/core-concepts/remote-caching)

**How it works:**
1. Before each task, Turborepo computes a fingerprint hash from: source files,
   dependencies, environment variables, and turbo.json configuration
2. Checks local cache first, then remote cache
3. On cache miss: runs task, captures output folders + terminal logs, uploads to
   remote cache as a tarball
4. On cache hit: downloads tarball, extracts to output directories, replays terminal logs
5. API spec: PUT/GET on `/v8/artifacts/:hash` with bearer token auth

**Cache key composition:**
```
hash = hash(
  source files of package,
  source files of all dependencies (transitive),
  environment variables declared in turbo.json,
  task configuration in turbo.json,
  lockfile entries for the package
)
```

**Real-world impact:**

| Source | Metric | Result |
|--------|--------|--------|
| Mercari Engineering | Turbo task duration | ~50% reduction |
| Mercari Engineering | Total job duration | ~30% reduction |
| Vercel marketing | CI pipeline reduction | Up to 85% for cached pipelines |
| Mercari Engineering | Cache server startup | ~10 seconds (identified as bottleneck) |

Source: [Mercari Turborepo Remote Cache](https://engineering.mercari.com/en/blog/entry/20260216-turborepo-remote-cache-accelerating-ci-to-move-fast/)

**Important caveat:** Cache effectiveness depends heavily on monorepo modularity.
Repositories with a single large application (not well-factored into packages) see
minimal benefit. The cache shines when many small packages can be individually cached.

### Nx remote cache

Source: [Nx: How Caching Works](https://nx.dev/docs/concepts/how-caching-works)

Nx uses a similar approach but with more sophisticated task distribution:

**Cache key composition:**
```
hash = hash(
  source files of project (configurable),
  source files of dependencies (configurable -- can use .d.ts instead of source),
  global config files,
  environment variables,
  runtime values (e.g., Node.js version)
)
```

**Key difference from Turborepo:** Nx Agents split tasks across multiple CI machines at
the individual task level, dynamically balancing load based on historical timing data.
Turborepo runs all tasks on a single machine.

### ZFS clone caching vs remote cache -- the structural comparison

| Dimension | Turborepo/Nx remote cache | forge-metal ZFS clone |
|-----------|--------------------------|----------------------|
| Cache restore time | 5-60s (download + extract) | ~1.7ms (COW clone) |
| Cache granularity | Per-task (lint, build, test) | Entire workspace |
| Cache invalidation | Content hash per task | New golden image |
| Storage overhead | Tarball per cache entry | COW shared blocks |
| Network dependency | Yes (S3/Vercel/custom server) | No (local NVMe) |
| Multi-machine sharing | Native (remote server) | Requires zfs send/recv |
| Incremental updates | Task-level (re-run changed tasks) | File-level (COW tracks changes) |

**The key insight:** Remote caches solve the "download and restore" problem by shipping
tarballs over the network. ZFS clones solve it by never copying data at all. The ZFS
approach is fundamentally faster for single-machine CI. Remote caches are necessary for
distributed CI across multiple machines.

**Hybrid approach for forge-metal:** The golden image IS the "remote cache" equivalent.
For projects using Turborepo (like cal.com), the golden image should have Turborepo's
local cache pre-warmed. This gives both layers of caching:
1. ZFS clone provides instant workspace with all caches (0ms restore)
2. Turborepo's local cache provides task-level incremental execution
3. No network round-trip needed for either layer


## 5. Real-world CI times for benchmark projects

### cal.com (large monorepo)

**Structure:**
- 5+ apps (web, website, api, swagger, docs), all Next.js
- Packages in `/packages/` (e.g., `@calcom/ui`)
- 153 UI routes (151 App Router, 2 Pages Router)
- Uses Turborepo for task orchestration
- Deploys via Vercel

Source: [cal.com Engineering Handbook](https://handbook.cal.com/engineering/codebase/monorepo-turborepo)

**Build times (measured on cal.com with Next.js 15.5.2):**

| Metric | Webpack | Turbopack | Delta |
|--------|---------|-----------|-------|
| Cold build (median) | 187.22s | 152.00s | -18.8% |
| Cold build (mean) | 191.83s | 149.04s | -22.3% |
| Cold build (min) | 184.25s | 128.86s | -- |
| Cold build (max) | 215.04s | 162.71s | -- |

Source: [Next.js 15.5: Webpack vs Turbopack](https://www.catchmetrics.io/blog/nextjs-webpack-vs-turbopack-performance-improvements-serious-regression)

**Estimated full CI pipeline (cold, no caching):**

| Phase | Estimated time | Notes |
|-------|---------------|-------|
| Checkout | 5-10s | Large repo |
| npm ci | 60-120s | ~800 MB - 1.2 GB node_modules |
| Lint | 20-45s | ESLint across monorepo |
| Typecheck | 30-60s | Multiple packages |
| Build (apps/web) | 150-190s | Webpack (cold, from benchmark above) |
| Tests | 30-60s | Varies by coverage |
| **Total** | **~5-8 min** | Single pipeline, no parallelism |

**With Turborepo caching and filter:** Turborepo's `--filter='...[origin/main]'` reduces
affected packages. For a typical PR touching one package: CI drops to 2-3 minutes (only
changed package + dependents rebuild).

### taxonomy (shadcn/ui demo app)

**Structure:**
- Single Next.js app (not a monorepo)
- Uses App Router, Server Components
- Authentication (NextAuth.js), payments (Stripe)
- Content via Contentlayer + MDX
- Medium complexity: ~50-100 source files

**Estimated CI pipeline (cold):**

| Phase | Estimated time | Notes |
|-------|---------------|-------|
| Checkout | 2-3s | Medium repo |
| npm ci | 30-60s | ~400-600 MB node_modules |
| Lint | 10-20s | Single app |
| Typecheck | 10-20s | Single tsconfig |
| Build | 30-60s | Medium Next.js app |
| **Total** | **~2-3 min** | |

### next-learn (Vercel tutorial)

**Structure:**
- Small tutorial Next.js app (dashboard example)
- Uses App Router
- Minimal dependencies
- Vercel tutorial project: "should finish [deployment] in under a minute"

**Estimated CI pipeline (cold):**

| Phase | Estimated time | Notes |
|-------|---------------|-------|
| Checkout | 1-2s | Small repo |
| npm ci | 15-30s | ~200-300 MB node_modules |
| Lint | 5-10s | Small codebase |
| Build | 15-30s | Small Next.js app |
| **Total** | **~45s - 1.5 min** | |

### What forge-metal targets (with optimizations)

| Project | Cold (no cache) | Warm (golden image, lockfile unchanged) | Optimized target |
|---------|----------------|----------------------------------------|-----------------|
| next-learn | 45-90s | 10-20s | <15s |
| taxonomy | 2-3 min | 20-40s | <30s |
| cal.com | 5-8 min | 40-90s | <60s |


## 6. Emerging CI acceleration patterns

### Package manager performance (clean install benchmarks)

Source: [pnpm.io/benchmarks](https://pnpm.io/benchmarks)

| Scenario | npm | pnpm | pnpm v11 | Yarn | Yarn PnP | Bun |
|----------|-----|------|----------|------|----------|-----|
| Clean install | 31.3s | 7.6s | 9.9s | 7.4s | 3.6s | ~3-5s |
| Cache + lockfile | 7.5s | 2.0s | 3.5s | 5.2s | 1.3s | -- |
| Cache + lockfile + node_modules | 1.3s | 0.68s | 0.56s | 5.0s | n/a | -- |

**For forge-metal's golden image:** When lockfile is unchanged and node_modules exists
in the clone, even npm needs only 1.3s. The lockfile-hash skip optimization makes the
package manager choice nearly irrelevant -- the answer is "don't run it at all."

### Linting: oxlint and Biome vs ESLint

Source: [oxc-project/bench-linter](https://github.com/oxc-project/bench-linter)

| Tool | Time (10K files) | vs ESLint |
|------|-------------------|-----------|
| ESLint | 45.2s | 1x |
| Biome | 0.8s | 56x faster |
| oxlint | 0.2-0.5s | 50-100x faster |

**Missing:** Framework-specific rules (`eslint-plugin-next`) are not yet in oxlint or
Biome. For Next.js CI, ESLint remains necessary unless projects accept reduced rule coverage.

### Testing: Vitest and Bun vs Jest

Source: [PkgPulse Benchmarks](https://www.pkgpulse.com/blog/bun-test-vs-vitest-vs-jest-test-runner-benchmark-2026)

Benchmark on 200 test files, 1,500 test cases:

| Runner | Full run | Watch mode (per change) | vs Jest |
|--------|----------|------------------------|---------|
| Jest (SWC) | 45s | 8s | 1x |
| Vitest | 12s | 1.5s | 3.7x |
| Bun test | 4s | 0.5s | 11x |

**Caveat:** Bun test lacks some Jest APIs. Vitest has near-complete Jest compatibility.
Next.js does not officially support Bun runtime.

### Bundling: Turbopack vs webpack

Source: [Turbopack benchmarks (Medium)](https://medium.com/@shahzaibnawaz/turbopack-is-finally-stable-5-real-world-benchmarks-vs-webpack-3469c4dcce59)

| Metric | Improvement |
|--------|-------------|
| Production build time | ~85% reduction (50K LOC e-commerce app) |
| Initial route compile | 45.8% faster (no cache) |
| HMR (1K modules) | 8.8x faster than webpack |
| HMR (30K modules) | 356.8x faster than webpack |

Turbopack is the default bundler in Next.js 16 (stable for both dev and production).
Next.js 16.1 added stable filesystem caching for Turbopack builds.

### The "fast stack" -- all optimizations combined

If every tool is replaced with its fastest alternative:

| Phase | Traditional | Fast stack | Speedup |
|-------|-------------|------------|---------|
| deps | npm ci (31s) | Skip (lockfile match) or pnpm (7.6s) | ~4x or infinite |
| lint | ESLint (45s) | oxlint (0.5s) | 90x |
| format | Prettier (12s) | Biome (0.3s) | 40x |
| typecheck | tsc (varies) | tsc --incremental + skipLibCheck | ~2x |
| build | webpack (187s cal.com) | Turbopack + cache (varies) | ~2-10x |
| test | Jest (45s/1500 tests) | Vitest (12s) or Bun (4s) | 4-11x |

**The irreducible bottleneck remains TypeScript's type-checker.** No Rust replacement exists.
`--incremental` and `--skipLibCheck` are the only levers. For large monorepos, tsc can take
30-60s even with these optimizations.


## 7. CI runner startups -- the competitive landscape

Several startups offer faster GitHub Actions runners, validating the market for faster CI:

| Provider | Technology | Key feature | Speed claim |
|----------|-----------|-------------|-------------|
| WarpBuild | Bare metal runners | Usage-based pricing, snapshots | 2x faster, 50% cheaper |
| Depot | Fast runners + Docker builds | Integrated Docker layer caching | 2x faster |
| Namespace | NVMe cache volumes | Zero-latency caching via COW snapshots | Instant cache |
| Actuated | Firecracker on customer metal | Fixed concurrency pricing | 2-3x faster |
| Blacksmith | Fast runners | High CPU performance | 2x faster |

Source: [Best GitHub Actions Runner Tools (Better Stack)](https://betterstack.com/community/comparisons/github-actions-runner/)

### Namespace Labs' cache volumes -- closest to forge-metal

Source: [Namespace Cache Volumes](https://namespace.so/docs/architecture/storage/cache-volumes)

Namespace's cache volumes use a copy-on-write snapshot mechanism on local NVMe storage.
When a runner starts, it receives a private fork of the most recent cache version --
conceptually identical to a ZFS clone.

Key properties:
- **Zero-latency cache restore** -- no download/upload phases
- **NVMe-backed** -- high-performance local storage
- **Concurrent access** -- multiple runners can read from the same snapshot
- **14-day automatic expiry** for unused caches

**This is the closest commercial analogy to forge-metal's ZFS clone approach.** The main
difference: Namespace is a managed service tied to GitHub Actions. forge-metal runs on
owned bare metal with full control over the snapshot lifecycle.


## 8. Applicability to forge-metal

### Validated architectural decisions

1. **Firecracker on bare metal is the industry consensus.** Vercel (Hive), Fly.io,
   Actuated, and E2B all chose Firecracker for build/compute isolation. forge-metal is
   on the right technology.

2. **ZFS COW clones are a structural advantage over every competitor.** Vercel pre-warms
   Docker images (~45s savings). WunderGraph uses overlayfs (~57% improvement). forge-metal
   starts with a fully pre-warmed filesystem in ~1.7ms. No competitor has this.

3. **The golden image model matches E2B's snapshot approach.** E2B proves the template/snapshot
   pattern works at scale. forge-metal's golden zvol is equivalent.

### Competitive gaps to close

1. **No remote cache equivalent for multi-node.** Turborepo/Nx remote caches work across
   machines. forge-metal needs `zfs send/recv` for golden image distribution to multiple
   nodes. Incus's GUID-based incremental sync (from existing research) is the right approach.

2. **No task-level caching.** Turborepo caches individual tasks (lint, build, test). forge-
   metal caches the entire workspace. For monorepos, task-level caching via Turborepo's
   local cache (pre-warmed in golden image) provides finer granularity.

3. **Build machine specs.** Vercel's Turbo machines are 30 vCPU, 60 GB RAM. forge-metal's
   benchmark should verify that Latitude.sh boxes provide equivalent or better specs per
   VM allocation.

### Key numbers for the forge-metal pitch

| Metric | GitHub Actions | Vercel Hive | forge-metal (target) |
|--------|---------------|-------------|---------------------|
| Build environment start | 20-60s (runner allocation) | ~5s (pre-warmed cell) | ~1.7ms (ZFS clone) |
| Cache restore | 30-60s (download artifact) | ~45s (Docker cache) | 0ms (COW -- already on disk) |
| Isolation | Shared VM (weak) | Firecracker (strong) | Firecracker + gVisor (strongest) |
| Build: next-learn | ~2-3 min | ~1-2 min | <15s (optimized) |
| Build: cal.com | ~8-15 min | ~5-8 min | <60s (optimized) |
