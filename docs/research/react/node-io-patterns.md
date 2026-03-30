# Node.js / npm Filesystem I/O Patterns for CI on ZFS

Research notes on filesystem behavior of Node.js CI workloads (npm ci, Next.js build, ESLint,
TypeScript) as background for forge-metal's ZFS zvol golden image architecture.

Conducted 2026-03-29.

---

## 1. npm ci filesystem behavior

### File counts

A measured npm install of a project with 1,507 packages created:
- **41,087 files**
- **5,240 directories**
- **258 MB** logical size (387 MB on-disk)
- **~1,300 tarballs** extracted from cache

Source: [Demystifying npm package installation](https://dev.to/pavel-zeman/demystifying-npm-package-installation-insights-analysis-and-optimization-tips-4nmj)

Larger projects (React/Next.js with many deps) routinely reach **50,000-100,000+ files**.
Atom had ~40,000 files; a cross-platform calculator reached ~200,000 files.

Source: [Consider methodologies for reducing file count](https://github.com/nodejs/node/issues/14872)

### What npm ci does differently

`npm ci` unconditionally **deletes the entire `node_modules/` directory** before installing.
This means every CI run creates 40,000-100,000+ files from scratch. On ZFS zvol clones where
`node_modules` is pre-populated in the golden image, this is the single most wasteful operation
-- it destroys the warm cache that the clone was designed to provide.

**Implication for forge-metal:** If the golden image pre-populates `node_modules`, the CI
workflow should use `npm install` (which preserves existing files) rather than `npm ci` (which
nukes everything). Alternatively, use `--ignore-scripts` with `npm ci` and pre-seed from cache.

### npm's internal write pipeline

npm's install pipeline:

1. **Resolve** -- Arborist builds ideal dependency tree
2. **Fetch** -- Pacote downloads tarballs into cacache (content-addressable cache)
3. **Extract** -- Tarballs extracted to temp staging dirs, then moved into `node_modules/`
4. **Reify** -- bin links, lifecycle scripts, lockfile save

Key libraries in the write path:

| Library | Role | fsync? |
|---------|------|--------|
| `cacache` | Content-addressable cache writes | **No** -- uses `fs.writeFile` with `wx` flag, no explicit fsync |
| `write-file-atomic` | Atomic writes for package.json, lockfile | **Yes** -- calls `fs.fsync()` by default (can disable with `{fsync: false}`) |
| `@npmcli/arborist` | Tree reification, lockfile save | Uses `write-file-atomic` for lockfile/shrinkwrap |
| `tar` | Tarball extraction | No fsync -- streaming extraction |

**Where fsync actually happens:** `write-file-atomic` is used primarily for writing
`package-lock.json`, `package.json`, and shrinkwrap files -- **not** for the bulk file
extraction into `node_modules/`. The tens of thousands of extracted package files go through
`cacache` and tar streaming, neither of which calls fsync.

The atomic write pattern is: write to temp file (named with murmurhex) -> `fs.fsync(fd)` ->
`fs.rename(tmp, target)`.

Source: [write-file-atomic](https://github.com/npm/write-file-atomic),
[cacache content/write.js](https://github.com/npm/cacache/blob/main/lib/content/write.js)

### npm v9 / v10 / v11 changes

| Version | Release | Key changes |
|---------|---------|-------------|
| npm v9 | Oct 2022 | Standardized defaults, deprecated `--global-style`/`--legacy-bundling` in favor of `--install-strategy` |
| npm v10 | Oct 2023 | `npm sbom` command, workspace link improvements, package-lock modes for query/audit |
| npm v11 | Dec 2024 | Security improvements, `--ignore-scripts` now applies to all lifecycle scripts including `prepare` |

npm v11 does not include a fundamental rewrite of the I/O path. The core Arborist/cacache/tar
pipeline remains architecturally the same since npm v7.

Source: [npm changelog](https://docs.npmjs.com/cli/v11/using-npm/changelog/),
[npm releases](https://github.com/npm/cli/releases)

---

## 2. node_modules on Copy-on-Write filesystems

### The core problem: massive small-file metadata

node_modules is pathologically bad for CoW filesystems:

- **40,000-100,000 files** means 40,000-100,000 individual metadata operations
- Most files are **small** (< 4 KB for `.js`, `package.json`, `LICENSE`, `README.md`)
- Directory tree is **deeply nested** (6-10 levels typical)
- Every `stat()`, `readdir()`, and `open()` traverses the metadata tree

### ZFS dataset (recordsize) behavior with small files

ZFS datasets handle small files well because recordsize is **dynamic** -- a 500-byte file
uses a single 512-byte block regardless of whether recordsize is 128K. Files smaller than
recordsize automatically use the smallest power-of-2 block that fits. This means recordsize
tuning has **no effect on small files** in datasets.

Source: [Klara Systems - Tuning recordsize](https://klarasystems.com/articles/tuning-recordsize-in-openzfs/)

### ZFS zvol (volblocksize) behavior -- critically different

For forge-metal's architecture (ext4 inside ZFS zvol), the situation is different:

- **volblocksize is fixed**, not dynamic (unlike recordsize for datasets)
- Cannot be changed after zvol creation
- Default is 16 KiB (since ZFS 2.2; was 8 KiB before)
- ext4 has a maximum block size of 4 KB

**Write amplification math:**

| volblocksize | Guest writes 4K | ZFS writes | Write amplification |
|-------------|-----------------|------------|---------------------|
| 4K | 4K | 4K data + ~8K metadata | 3x |
| 16K | 4K | 16K read-modify-write + 8K metadata | 6x |
| 64K | 4K | 64K read-modify-write + 8K metadata | 18x |

For node_modules workloads (dominated by small random writes during extraction), smaller
volblocksize reduces write amplification. **4K volblocksize matches ext4's block size** but
increases metadata overhead. 16K (the ZFS 2.2 default) is a reasonable compromise.

**For sequential I/O (build output, webpack bundles):** larger volblocksize helps. 16K zvol
performed catastrophically for 1MB sequential writes (~12-13% throughput of 64K zvol).

Source: [ZFS write amplification](https://github.com/openzfs/zfs/issues/6584),
[Proxmox volblocksize discussion](https://forum.proxmox.com/threads/not-a-problem-ext4-block-optimisation-on-zvols.128544/),
[16K recommendation discussion](https://github.com/openzfs/zfs/issues/14771)

### Recommendation for forge-metal

The golden image zvol workload is **bimodal**:

1. **npm ci / install phase:** dominated by small random writes (40K+ files, mostly < 4KB)
2. **Build phase:** dominated by sequential writes (webpack/turbopack output, .next/ directory)

**volblocksize = 16K** is the right compromise:
- Only 4x write amplification for 4K random writes (vs 3x at 4K volblocksize)
- Adequate sequential throughput for build output
- Matches the ZFS 2.2 default, well-tested path
- ext4 will internally handle 4K blocks; the 16K ZFS block holds 4 ext4 blocks

If the golden image **pre-populates node_modules** (avoiding the install phase entirely), the
workload shifts heavily toward sequential build I/O, making 16K or even 32K more attractive.

### ZFS special allocation class

For NVMe-backed pools, a **special vdev** can store metadata and small blocks on faster
storage. Setting `special_small_blocks=16K` would store all node_modules files < 16K on the
special vdev, keeping the main vdev free for sequential build I/O. This can halve random I/O
latency for metadata-heavy operations.

Source: [ZFS special vdev](https://www.xda-developers.com/i-added-a-metadata-vdev-to-my-zfs-pool-and-everything-got-faster/)

---

## 3. fsync in the Node.js / npm ecosystem

### Where fsync lives

| Component | Calls fsync? | Details |
|-----------|-------------|---------|
| `cacache` (npm cache) | **No** | Uses `fs.writeFile` with `wx` flag |
| `write-file-atomic` | **Yes, by default** | `fs.fsync(fd)` before rename; disable with `{fsync: false}` |
| `graceful-fs` | **No** | Handles EMFILE (too many open files) via retry/queue, no fsync |
| `tar` (npm tar extraction) | **No** | Streaming extraction, no explicit sync |
| Node.js `fs.writeFile` | **No** | Writes to kernel buffer, no explicit flush |

**Key insight:** The bulk of npm's I/O (extracting tens of thousands of files into
node_modules) does **not** call fsync. The fsync calls come from `write-file-atomic` writing
a handful of metadata files (package-lock.json, package.json, shrinkwrap). This means
**disabling fsync (via eatmydata or seccomp) has minimal impact on npm install specifically**.

### OBuilder's seccomp fsync bypass

OBuilder (OCaml CI system) uses a seccomp filter with runc >= v1.0.0-rc92 to intercept
all sync syscalls and return errno 0 ("success") without actually syncing:

- `apt-get install shared-mime-info`: **18.5s -> 4.7s** (3.9x speedup)
- The speedup is largest for package managers that call fsync aggressively (dpkg, rpm)
- npm does NOT call fsync aggressively, so the benefit is smaller

Source: [OBuilder fast-sync](https://github.com/ocurrent/obuilder)

### eatmydata / LD_PRELOAD approach

`libeatmydata` intercepts fsync/fdatasync/sync/msync/open(O_SYNC) via LD_PRELOAD and makes
them no-ops:

- Docker build with apt-get + npm: **8m10s -> 5m28s** (~33% faster)
- The improvement comes mostly from apt-get, not from npm
- For database workloads (Postgres, MySQL): **10x-100x speedup** from disabling fsync

Source: [eatmydata Docker builds](https://wildwolf.name/speeding-up-docker-builds-with-eatmydata/),
[Farewell to fsync](https://lobste.rs/s/urhsse/farewell_fsync_10x_faster_database_tests)

### Forge-metal implication

Since npm's bulk extraction doesn't call fsync, the seccomp fsync bypass (already planned
from OBuilder research) will have **limited benefit for the npm install phase** but
**significant benefit for other operations** in the CI pipeline:
- `apt-get` / `dpkg` operations (if any system packages are installed)
- Database operations in test suites
- Any tool using `write-file-atomic` in tight loops

The main I/O bottleneck for npm is **file creation metadata overhead** (mkdir, open, write,
close, rename for tens of thousands of files), not fsync.

---

## 4. Alternative package managers: I/O patterns

### Benchmark comparison (pnpm "alotta-files" project)

| Scenario | npm | pnpm | Yarn | Yarn PnP |
|----------|-----|------|------|----------|
| Clean install | 31.3s | 7.6s | 7.4s | 3.6s |
| With cache + lockfile + node_modules | 1.3s | 677ms | 5.0s | n/a |
| With cache + lockfile | 7.5s | 2.0s | 5.2s | 1.3s |
| With cache only | 12.1s | 5.4s | 7.3s | 3.0s |
| With lockfile only | 10.7s | 4.9s | 5.3s | 1.3s |

Source: [pnpm benchmarks](https://pnpm.io/benchmarks)

### pnpm content-addressable store

pnpm's key I/O optimization: **hardlinks from a global store** instead of copying files.

- Every version of every package stored once in `~/.local/share/pnpm/store/`
- Files in `node_modules/` are hardlinks to the store
- On Btrfs/APFS: uses **reflinks** (CoW clones) instead of hardlinks
- Saves 50-70% disk space across multiple projects

**ZFS/zvol interaction:** pnpm hardlinks require the store and node_modules to be on the
**same filesystem**. Inside a Firecracker VM with a zvol-backed rootfs, this works if the
store is on the same zvol. But the store **cannot** be shared across VMs (separate zvols =
separate filesystems). Each golden image clone would need its own copy of the store, which
defeats the disk-sharing benefit.

**However:** pnpm with `--prefer-offline` and a pre-populated store in the golden image
would give very fast installs -- the hardlink operation is metadata-only, no data copying.

Source: [pnpm FAQ](https://pnpm.io/faq), [pnpm motivation](https://pnpm.io/motivation),
[pnpm cross-filesystem discussion](https://github.com/orgs/pnpm/discussions/3651)

### Bun install

Bun's package manager is the fastest by a wide margin:

**System call comparison:**
- **Bun**: ~165,000 syscalls for a typical install
- **npm**: ~1,000,000 syscalls for the same install
- **6x fewer syscalls** -- the dominant factor in speed

**Why Bun is fast:**
1. Written in **Zig** -- compiles to native code, no Node.js/libuv overhead
2. **Direct system calls** -- no string conversions, thread pool handoffs, event loop
3. **Structure of Arrays** binary lockfile (bun.lockb) -- cache-friendly memory layout
4. **Optimal decompression** -- reads gzip trailer to pre-allocate exact buffer size
5. **Platform-specific cloning:**
   - macOS: `clonefile()` (recursive CoW clone in one syscall)
   - Linux: `hardlink` (default), falls back to `copy_file_range()`
6. **io_uring** for async I/O on Linux kernels 5.6+

**Real-world:** cold installs that take 40s with npm complete in < 5s with Bun.

**ZFS interaction:** Bun's hardlink backend on Linux means files share the same ZFS blocks.
If the golden image pre-populates `node_modules/` with Bun, cloning the zvol gives instant
access with zero additional I/O.

Source: [Why bun install is fast](https://betterstack.com/community/guides/scaling-nodejs/bun-install-performance/),
[bun install docs](https://bun.com/docs/pm/cli/install)

### Forge-metal recommendation

For the golden image strategy (pre-populated node_modules on zvol clone):

1. **Best case: no install at all.** Golden image includes node_modules. Clone = ready.
2. **If install needed: Bun** -- 6x fewer syscalls, hardlink backend, io_uring.
3. **If npm required: `npm install`** (not `npm ci`) to preserve existing files.
4. **pnpm** is fast but hardlink model has cross-filesystem limitations in VM architecture.

---

## 5. Node.js V8 startup and memory during Next.js builds

### Peak memory during builds

Next.js builds are memory-intensive. Real-world measurements from GitHub issues:

| Project size | Peak heap | Notes |
|-------------|-----------|-------|
| Small (< 50 pages) | ~1-1.5 GB | Default Node.js heap sufficient |
| Medium (50-200 pages) | ~2-3 GB | May need `--max-old-space-size=4096` |
| Large (200+ pages) | ~4+ GB | Routinely hits 4 GB heap, needs 6-8 GB allocation |
| Very large (monorepo) | 4-9 GB | Reports of 9 GB RSS requiring server restart |

**Specific measurement:** Mark-Compact GC events during TypeScript type-checking phase
showed heap at **4,044 MB -> 4,027 MB** even on a 32 GB system, failing with "Ineffective
mark-compacts near heap limit."

Source: [Next.js issue #76704](https://github.com/vercel/next.js/issues/76704),
[Next.js issue #79588](https://github.com/vercel/next.js/issues/79588)

### V8 garbage collection impact on builds

- ESLint spent **2.43 seconds** on GC alone during a single lint run (due to 20M+
  `BackwardTokenCommentCursor` instantiations)
- V8 incremental marking targets **< 5ms** per marking step
- Webpack generates path info that creates GC pressure across thousands of modules
- `experimental.webpackMemoryOptimizations: true` (Next.js 15+) reduces peak memory at
  cost of slightly slower compilation

Source: [V8 trash talk](https://v8.dev/blog/trash-talk),
[Next.js memory guide](https://nextjs.org/docs/app/guides/memory-usage)

### Memory optimization levers

| Lever | Effect | Available since |
|-------|--------|----------------|
| `experimental.webpackBuildWorker: true` | Run webpack in separate worker, reduce main process memory | Next.js 14.1 (default if no custom webpack) |
| `experimental.webpackMemoryOptimizations: true` | Reduce peak webpack memory | Next.js 15.0 |
| `productionBrowserSourceMaps: false` | Skip source map generation | Always |
| `typescript.ignoreBuildErrors: true` | Skip type-checking during build (dangerous) | Always |
| `--max-old-space-size=N` | Increase V8 heap limit | Node.js |
| `next build --experimental-debug-memory-usage` | Print heap/GC stats continuously | Next.js 14.2 |

### Forge-metal Firecracker VM sizing

For CI VMs running Next.js builds:
- **Minimum:** 2 GB RAM for small projects
- **Recommended:** 4 GB RAM for typical Next.js projects
- **Safe:** 6-8 GB RAM for large projects with TypeScript type-checking
- V8 will use up to `--max-old-space-size` (default ~1.4 GB on older Node, ~4 GB on modern)
- Set `NODE_OPTIONS="--max-old-space-size=4096"` as default in golden image

---

## 6. ESLint and TypeScript I/O patterns

### Module resolution: the hidden I/O bottleneck

**Module resolution takes more time than parsing source code.** This is the most
counter-intuitive finding: the filesystem traversal to find where modules live is slower
than actually reading and parsing them.

**Stat call explosion:** For a file at depth 8 importing `foo`, Node.js module resolution
checks 8 directories upward for `node_modules/foo/`:

```
Layout/node_modules/foo/
components/node_modules/foo/
DetailPage/node_modules/foo/
features/node_modules/foo/
src/node_modules/foo/
my-project/node_modules/foo/
marvinh/node_modules/foo/
/node_modules/foo/
```

For each candidate, TypeScript/ESLint checks **8 extensions** (.js, .jsx, .cjs, .mjs, .ts,
.tsx, .mts, .cts), doubled for index files. A single import can trigger **128 stat() calls**
in the worst case.

**Measured:** ~15,000 `isFile()` invocations but only ~2,500 unique paths (83% redundant).
Adding a caching layer: **30% total performance gain**.

Source: [Speeding up JS ecosystem - module resolution](https://marvinh.dev/blog/speeding-up-javascript-ecosystem-part-2/)

### ESLint performance breakdown

Measured on Vite's repository (144 files):

| Component | Time | % of total |
|-----------|------|-----------|
| TypeScript AST conversion | ~22% | Converting TS AST to estree format |
| Selector engine (esquery) | ~25% | Rule matching via CSS-like selectors |
| GC overhead | 2.43s | 20M+ object instantiations |
| Parser (@typescript-eslint/parser) | 2.1s | vs 0.6s for @babel/eslint-parser |

**ESLint vs alternatives:**
- ESLint: 5.85s
- Custom JS linter: 0.52s (11x faster)
- Rust-based rslint: 0.45s (13x faster)

Source: [Speeding up JS ecosystem - ESLint](https://marvinh.dev/blog/speeding-up-javascript-ecosystem-part-3/)

### TypeScript type-checking

The four phases of `tsc`:
1. **Program construction** (parsing + module resolution) -- I/O bound
2. **Binding** -- CPU bound
3. **Type checking** -- CPU bound (typically **95%** of total time for large projects)
4. **Emit** -- I/O bound (writing .js/.d.ts files)

**File watchers:** Opening a JS file with node_modules present can spawn **20,000+ file
watchers**, potentially hitting the Linux inotify limit (default 8192).

Source: [TypeScript performance tracing](https://github.com/microsoft/TypeScript/wiki/Performance-Tracing),
[TypeScript watcher explosion](https://github.com/microsoft/TypeScript/issues/49474)

### tsgo (TypeScript 7 native compiler)

Microsoft's Go-based TypeScript compiler, available as `@typescript/native-preview`:

| Metric | tsc | tsgo | Speedup |
|--------|-----|------|---------|
| Total compilation | 0.28s | 0.026s | **10.8x** |
| Type checking | 0.10s | 0.003s | **~30x** |
| Memory usage | 68,645K | 23,733K | **2.9x less** |
| VS Code project load | 9.6s | 1.2s | **8x** |

**Forge-metal implication:** If the CI pipeline uses `tsgo` for type-checking, the
type-check phase drops from minutes to seconds, making the build phase (webpack/turbopack)
the dominant bottleneck instead.

Source: [TypeScript native port announcement](https://devblogs.microsoft.com/typescript/typescript-native-port/),
[tsgo benchmarks](https://www.pkgpulse.com/blog/tsgo-vs-tsc-typescript-7-go-compiler-2026)

---

## 7. ZFS tuning for Node.js CI workloads on zvols

### volblocksize selection for ext4-inside-zvol

Since forge-metal uses ext4 inside ZFS zvols (Firecracker sees `/dev/vda` as a block device):

| volblocksize | Pros | Cons |
|-------------|------|------|
| 4K | Matches ext4 block size, minimal write amplification | High metadata overhead, more ZFS blocks to manage |
| 8K | Legacy ZFS default, reasonable compromise | Slight write amplification for 4K I/O |
| **16K** | **ZFS 2.2 default, good balance, well-tested** | **4x amplification for 4K random writes** |
| 32K | Good sequential throughput | 8x amplification for 4K random writes |
| 64K | Best sequential throughput | 16x amplification for 4K, poor for npm workloads |

**Recommendation: 16K volblocksize** for the golden image zvol. This is the ZFS 2.2 default
and provides the best compromise between:
- Random small writes (npm install / file creation) -- 4x amplification is acceptable
- Sequential writes (build output) -- adequate throughput
- Metadata overhead -- moderate
- Clone overhead -- each COW operation copies a 16K block

### Compression

**Always enable compression.** node_modules is highly compressible (JavaScript source,
JSON metadata, READMEs):

| Compression | Ratio for JS/JSON | CPU overhead |
|-------------|-------------------|-------------|
| lz4 | ~2.5x | Negligible |
| zstd (default) | ~3-4x | Low |
| zstd-3 | ~3.5-4.5x | Low-moderate |

With compression, the 16K volblocksize penalty for small files is mitigated: a 2K .js file
compressed to 800 bytes still occupies a 16K ZFS block, but the actual disk I/O is ~1K
(compressed block + metadata).

### Clone cost with node_modules

When the golden image zvol has a pre-populated node_modules:

1. `zfs clone` is metadata-only (~1.7ms)
2. First read of any file: served from ARC (ZFS cache) if warm, or from disk
3. First **write** to any file: triggers COW of the containing 16K block
4. `npm install` modifying existing files: each modified file COWs one 16K block
5. Creating new files: allocates new 16K blocks (no COW penalty)

**Worst case for npm ci:** If `npm ci` deletes and recreates all 41,000+ files, it:
- Marks ~41,000 file blocks for COW (but doesn't actually copy -- deletion is metadata)
- Creates ~41,000 new blocks (16K each = ~640 MB of writes before compression)
- With zstd compression: ~160-200 MB actual disk I/O

**Best case (golden image match):** If the golden image matches the project's dependencies
exactly and the CI uses `npm install` (not `npm ci`), npm detects no changes needed:
- **Zero new blocks written** for the install phase
- Clone overhead: only the bytes written by the build phase

### ZFS special vdev for metadata acceleration

For NVMe pools, adding a special vdev (mirrored NVMe pair) and setting
`special_small_blocks=16384` would store:
- All ZFS metadata (indirect blocks, dnode blocks)
- All file data blocks <= 16K (which includes most node_modules files)
- On the faster NVMe device, keeping the main pool for large build artifacts

This can **halve random I/O latency** for metadata-heavy operations like module resolution
(thousands of stat() calls).

Source: [ZFS special vdev performance](https://www.xda-developers.com/i-added-a-metadata-vdev-to-my-zfs-pool-and-everything-got-faster/),
[OpenZFS special allocation](https://github.com/openzfs/zfs/discussions/14542)

---

## Summary: highest-impact optimizations for forge-metal

Ordered by expected impact on CI job latency:

| # | Optimization | Expected impact | Complexity |
|---|-------------|----------------|------------|
| 1 | **Pre-populate node_modules in golden image** | Eliminate install phase entirely (saves 7-31s) | Medium -- requires project-specific golden images or multi-layer approach |
| 2 | **Use `npm install` not `npm ci`** | Preserve golden image's pre-populated node_modules | Trivial -- config change |
| 3 | **volblocksize = 16K** for golden zvol | Balance random/sequential I/O, match ZFS 2.2 default | Trivial -- set at zvol creation |
| 4 | **Enable zstd compression** | 3-4x reduction in actual disk I/O | Trivial -- pool/dataset property |
| 5 | **Use Bun for package installation** if project allows | 6x fewer syscalls, hardlink backend, io_uring | Low -- drop-in for most projects |
| 6 | **seccomp fsync bypass** (OBuilder pattern) | 33% faster for apt-get operations; limited benefit for npm | Medium -- seccomp filter in Firecracker/gVisor |
| 7 | **tsgo for type-checking** | 10x faster type-check phase | Low -- drop-in replacement |
| 8 | **Firecracker VM: 4 GB RAM minimum** | Prevent OOM during Next.js builds | Trivial -- VM config |
| 9 | **ZFS special vdev** for metadata | 2x faster metadata ops (stat, readdir) | Medium -- requires additional NVMe device |
| 10 | **NODE_OPTIONS max-old-space-size=4096** in golden image | Prevent heap OOM for large projects | Trivial -- env var |

### Key non-obvious findings

1. **npm's bulk extraction does NOT call fsync.** The seccomp fsync bypass helps apt-get
   (3.9x) but not npm install. The npm I/O bottleneck is metadata ops (mkdir/open/close),
   not sync.

2. **Module resolution is slower than parsing.** 83% of file lookups are redundant. Caching
   gives 30% speedup. This means **warm filesystem caches** (from ZFS ARC) matter more than
   raw disk speed for ESLint/TypeScript.

3. **npm ci destroys the golden image advantage.** It unconditionally deletes node_modules.
   Use `npm install` to preserve pre-populated dependencies.

4. **volblocksize is fixed at creation.** Unlike recordsize for datasets, you cannot change
   volblocksize later. Get it right at golden image creation time.

5. **TypeScript type-checking uses 95% of tsc time.** Module resolution (I/O-bound) is the
   remaining 5%. tsgo makes type-checking 30x faster, shifting the bottleneck to build I/O.

6. **Bun uses 6x fewer syscalls than npm** -- the dominant factor. It's not about network
   speed or cache hits; it's about reducing kernel transitions.

7. **pnpm hardlinks don't work across zvols.** Each Firecracker VM gets its own filesystem
   from a separate zvol clone. pnpm's cross-project dedup doesn't help in this architecture.

8. **Next.js build heap can reach 4+ GB.** Firecracker VMs need at least 4 GB RAM, ideally
   6-8 GB, for large project builds. TypeScript 5.6+ has a memory regression that makes
   this worse.
