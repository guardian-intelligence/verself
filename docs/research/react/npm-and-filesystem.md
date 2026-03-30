# npm, node_modules, and Filesystem I/O Patterns

> How `npm ci` works under the hood, what it does to the filesystem, and how
> ZFS copy-on-write interacts with JavaScript's extreme small-file workload.
>
> Sources: npm/cli source, pnpm benchmarks, ZFS tuning guides, Node.js docs
>
> Conducted 2026-03-30.

## npm ci internals

`npm ci` is the CI-specific install command. It differs from `npm install`:

1. **Deletes `node_modules` entirely** before installing (recursive `rm -rf`)
2. **Reads only `package-lock.json`** — never modifies the lockfile
3. **Uses Arborist** (`@npmcli/arborist`) to build the ideal dependency tree from the lockfile
4. **Uses Pacote** to fetch packages from the registry (or cache)
5. **Uses cacache** (content-addressable cache at `~/.npm/_cacache`) to store/retrieve tarballs
6. **Extracts tarballs** directly into `node_modules/` — no linking step

**Key I/O characteristics:**
- The `rm -rf node_modules` step alone can take seconds on a large project (deleting 50K-100K files)
- Each package is extracted from a `.tgz` tarball into individual files
- The result is a deeply nested directory tree with enormous file counts

## node_modules by the numbers

**Measured baseline** (1,507 packages, moderate project):
- 41,087 files, 5,240 directories, 258 MB logical (387 MB on-disk), ~1,300 tarballs
  Source: [Demystifying npm installation](https://dev.to/pavel-zeman/demystifying-npm-package-installation-insights-analysis-and-optimization-tips-4nmj)

**Estimated for forge-metal benchmark projects:**

| Project | node_modules size | Approximate file count | Typical install time (npm ci, cold) |
|---------|-------------------|----------------------|-------------------------------------|
| next-learn (small) | ~200-300 MB | ~20K-30K files | ~15-30s |
| taxonomy (medium) | ~400-600 MB | ~40K-60K files | ~30-60s |
| cal.com (large monorepo) | ~800 MB - 1.2 GB | ~80K-120K files | ~60-120s |

**The fundamental problem:** JavaScript's ecosystem produces an extreme number of small files.
A typical `node_modules` directory contains:
- Tens of thousands of `.js` files (average 2-10 KB each)
- Thousands of `.json` files (package.json, tsconfig.json manifests)
- Thousands of `.d.ts` type definition files
- README.md, LICENSE, CHANGELOG.md in every package

## fsync pressure from npm

npm uses `graceful-fs` as its filesystem layer, which wraps Node.js's `fs` module with
retry logic for `EMFILE` (too many open file descriptors).

**CORRECTION: npm's bulk extraction does NOT call fsync.** This is a critical finding
from deeper investigation. npm extracts tarballs directly into `node_modules/` using
streaming decompression. The extraction path does NOT call `fsync` on each file.

Only `write-file-atomic` (used for `package-lock.json` and a handful of metadata files)
calls `fs.fsync()`. The `write-file-atomic` pattern is:
1. `open` (temp file with murmurhex suffix)
2. `write` (contents)
3. `fsync` (force to disk) — **only for these few files, not bulk package content**
4. `rename` (atomic swap)
5. `close`

Source: [write-file-atomic](https://github.com/npm/write-file-atomic),
[cacache](https://github.com/npm/cacache)

**The real bottleneck is metadata operations.** For 41K files, npm performs:
- ~41K `open`/`write`/`close` sequences (file creation)
- ~5K `mkdir` calls (directory creation)
- ~41K `chmod` calls (permission setting)
- Plus symlinks in `.bin/`

Each of these is a metadata operation that hits the ZFS Intent Log (ZIL) on the zvol.

**OBuilder's seccomp fsync bypass** (see `docs/research/obuilder.md`) provides a 3.9x
speedup for `apt-get` but **limited benefit for npm** since npm doesn't call fsync during
bulk extraction. The `eatmydata` approach (LD_PRELOAD fsync suppression) gave ~33% overall
Docker build speedup, mostly from apt-get, not npm.
Source: [eatmydata Docker builds](https://wildwolf.name/speeding-up-docker-builds-with-eatmydata/)

**Applicability to forge-metal:** fsync interception is less impactful than initially
thought for the `deps` phase. The real optimization is **skipping `npm ci` entirely**
when the lockfile is unchanged, or using the golden image's pre-populated `node_modules`
with `npm install` instead of `npm ci` (which nukes everything). For builds, `next build`
does call fsync when writing `.next/` output, so interception still helps there.

## Copy-on-write interaction with node_modules

### The ZFS+node_modules problem

When a golden image contains pre-populated `node_modules` and a ZFS clone is created:

1. **Best case (lockfile unchanged):** `npm ci` still deletes and re-creates `node_modules`.
   This means every file in the clone's `node_modules` must be written fresh, touching
   every block. The ZFS clone advantage (COW) is lost for this directory.

2. **Alternative: skip npm ci when lockfile matches.** If the golden image's lockfile hash
   matches the PR's lockfile, skip the `deps` phase entirely. Use the pre-warmed
   `node_modules` from the golden image (zero COW cost). This is what forge-metal's
   `lockfile_changed` field in CIEvent is designed to track.

3. **The COW amplification problem:** `npm ci`'s `rm -rf node_modules` doesn't just unlink
   files — it triggers metadata writes on every directory in the tree. On a COW filesystem,
   this means writing new metadata blocks for every directory traversed during deletion.
   Then extracting packages writes new data blocks for every file.

### ZFS recordsize tuning for node_modules

ZFS recordsize is the **maximum** logical block size for a dataset. Default is 128 KiB.

For files **smaller** than recordsize, ZFS automatically uses the smallest power-of-2 block
that fits the file. A 3 KB file uses a 4 KB block regardless of recordsize setting.

**Key insight:** Since most files in `node_modules` are 2-10 KB, they already get small
blocks regardless of recordsize. Changing recordsize to 4K or 8K would NOT significantly
help `node_modules` — the small files are already efficiently stored.

**Where recordsize matters:** The `.next/cache` directory contains fewer, larger files
(webpack serialized modules, cached pages). These benefit from the default 128K recordsize.

**IMPORTANT: zvol volblocksize is FIXED, not dynamic like recordsize.** This is the
critical difference. A ZFS dataset with `recordsize=128K` stores small files efficiently
(dynamic block sizing). But a **zvol** with `volblocksize=128K` uses 128K blocks for
EVERY write, regardless of size. A 4K ext4 write on a 128K zvol copies a full 128K block.

**Write amplification for zvol volblocksize:**

| volblocksize | 4K random write amplification | Sequential throughput |
|-------------|------------------------------|----------------------|
| 4K | 1x (no amplification) | Lower (more metadata) |
| 8K | 2x | Moderate |
| **16K** | **4x** | **Good compromise** |
| 64K | 16x | Better sequential |
| 128K | 32x | Best sequential |

Source: [ZFS zvol write amplification](https://github.com/openzfs/zfs/issues/11407)

**Recommendation for forge-metal zvols:**
- **Set `volblocksize=16K`** — ZFS 2.2 default, good compromise between random I/O
  (4x amplification for 4K writes, acceptable for CI) and sequential throughput
- **Enable zstd compression** — 3-4x ratio on JS/JSON content. Reduces actual I/O
  significantly. Most npm files are highly compressible text.
- **Consider a ZFS special vdev** for metadata — can halve random I/O latency for the
  stat-heavy module resolution workload.
  Source: [ZFS special vdev](https://www.xda-developers.com/i-added-a-metadata-vdev-to-my-zfs-pool-and-everything-got-faster/)
- For the ext4 inside the zvol: use 4K block size (default), `noatime` mount option

### The special_small_blocks optimization

ZFS `special_small_blocks` directs small blocks to a special vdev (typically fast SSD/NVMe).
For a node_modules workload with many small files, this could route all the 4K blocks to
the fastest storage while keeping large blocks on bulk storage. However, forge-metal's
target is NVMe-only pools, so this optimization doesn't apply.

## Package manager comparison for CI

Benchmarks from [pnpm.io/benchmarks](https://pnpm.io/benchmarks) (2026-03-30):

| Scenario | npm | pnpm | pnpm v11 | Yarn | Yarn PnP |
|----------|-----|------|----------|------|----------|
| Clean install | 31.3s | 7.6s | 9.9s | 7.4s | 3.6s |
| Cache + lockfile | 7.5s | 2.0s | 3.5s | 5.2s | 1.3s |
| Cache + lockfile + node_modules | 1.3s | 0.68s | 0.56s | 5.0s | n/a |

**Syscall comparison (measured):**
- npm: ~1,000,000 syscalls for a typical install
- Bun: ~165,000 syscalls (6x fewer) — the dominant speed factor
  Source: [Bun install performance](https://betterstack.com/community/guides/scaling-nodejs/bun-install-performance/)

**pnpm's advantage:** Content-addressable global store + hard links. When installing
packages, pnpm creates hard links from the global store to `node_modules/.pnpm/`, then
symlinks from `node_modules/<package>` to the `.pnpm` store.

**pnpm caveat for forge-metal:** Hard links require the global store and `node_modules`
to be on the **same filesystem**. In Firecracker VMs where the zvol is the sole filesystem,
this works. But if the global store were on a separate volume, hard links would fail and
pnpm would fall back to copying.

**Why this matters for ZFS clones:**
- **pnpm with warm global store:** Only creates hard links and symlinks. Minimal data
  blocks written to the clone. Much less COW amplification than npm ci.
- **npm ci:** Extracts every tarball fresh. Maximum data blocks written.
- **Yarn PnP:** No `node_modules` at all. Packages resolved from `.pnp.cjs` + zip files.
  Zero extraction overhead. Fastest option but requires ecosystem compatibility.

**Monorepo comparison:** A monorepo with 10 packages and shared dependencies:
- npm: ~1.2 GB (`node_modules` per workspace)
- pnpm: <300 MB (hard links, single copy of each version)

**Applicability to forge-metal:** Switching benchmark workloads from `npm ci` to `pnpm install`
could reduce the `deps` phase by 3-4x. However, this would diverge from the actual CI
workflow of the benchmarked projects (most use npm or yarn). The benchmark should reflect
reality. Consider tracking both: `npm ci` for fidelity, `pnpm install` for "what's possible."

## Node.js V8 memory during builds

Next.js builds are memory-intensive:

| Build phase | Typical heap usage | What consumes memory |
|-------------|-------------------|---------------------|
| deps (npm ci) | 200-400 MB | Package resolution graph, extraction buffers |
| lint (ESLint) | 300-600 MB | AST for every file in scope, rule state |
| typecheck (tsc) | 400-800 MB | Full TypeScript program, type table |
| build (next build) | 500 MB - 2+ GB | Webpack/Turbopack module graph, source maps |

For the cal.com monorepo, builds can exceed 2 GB heap. Default Node.js heap limit is
~1.7 GB (v8's default `--max-old-space-size`). Projects commonly set
`NODE_OPTIONS='--max-old-space-size=4096'` to avoid OOM.

**V8 garbage collection impact:** During builds, V8's garbage collector fires frequently
as the AST/module graph grows. Mark-sweep GC pauses can be 50-200ms each. On a 60s build,
total GC time might be 2-5s (3-8% overhead).

## ESLint I/O patterns

ESLint traverses all files matching its configuration:
1. `stat()` every file in the project (respecting `.eslintignore`)
2. Read each file into memory
3. Parse into AST (using `@typescript-eslint/parser` for TS)
4. Run rules against AST
5. Write cache file (`.eslintcache`) if enabled

**Module resolution overhead:** The TypeScript parser used by ESLint performs its own module
resolution for type-aware rules. This means stat() calls for every possible module path
(trying `.ts`, `.tsx`, `.js`, `.jsx`, `.d.ts` extensions at each resolution step). For a
project with 1000 imports, this can mean 5000+ stat() calls just for resolution.

**ESLint cache:** When `.eslintcache` is present and enabled, ESLint only re-lints files
whose mtime has changed. The cache is a JSON file mapping file paths to lint results.
On a golden image with warm cache, the lint phase could be nearly instant for unchanged files.

## TypeScript compiler memory and I/O

From [Arpad Borsos's research](https://swatinem.de/blog/optimizing-tsc/):

**Baseline measurements:**
- Empty project (82 files, 22K lines): 61 MB memory, 1.28s
- With `aws-sdk` types (345 files, 396K lines, 1.18M AST nodes): **465 MB memory**, 5.38s
- With `--skipLibCheck`: 375 MB memory, 3.05s

**AST node memory:** TypeScript maintains ~1 million AST nodes for a medium project. Each
node is 104-160 bytes (V8 object overhead). Total AST memory: ~100-180 MB.

**Module resolution stat() storm (measured):** `tsc` resolves every import by trying
multiple file paths. For `import { foo } from './bar'`, it tries:
- `./bar.ts`, `./bar.tsx`, `./bar.d.ts`, `./bar.mts`, `./bar.cts`
- `./bar/index.ts`, `./bar/index.tsx`, `./bar/index.d.ts`
- `./bar.js`, `./bar.jsx`, `./bar.cjs`, `./bar.mjs` (16 total with index variants)

Each attempt is a `stat()` call. **Measured: in a 1,500-file project, `isFile()` was
called 15,000 times** — a 10x overhead over actual file count. Only 2,500 unique paths
were checked, showing massive redundancy from repeated resolution of the same modules.
Source: [Marvin Hagemeister](https://marvinh.dev/blog/speeding-up-javascript-ecosystem-part-2/)

A simple `Map<string, boolean>` cache for stat results yields **15% speedup**. Using
`fs.statSync(file, { throwIfNoEntry: false })` instead of try/catch eliminates stack
trace creation, adding another **7% improvement** to ESLint (which uses the same
resolution).

**`--incremental` with `.tsbuildinfo`:** TypeScript can persist its dependency graph to
`.tsbuildinfo`. On subsequent runs, it only re-checks files whose dependencies changed.
This is the TypeScript equivalent of Next.js's build cache.

## Applicability to forge-metal

1. **Lockfile-hash skip is the #1 optimization** (not fsync interception — npm doesn't
   call fsync during bulk extraction). When golden image lockfile matches PR lockfile,
   skip `npm ci` entirely. This eliminates the most expensive phase. Track via
   `lockfile_changed` in CIEvent.

2. **Use `npm install` instead of `npm ci`** when the golden image has pre-populated
   `node_modules`. `npm ci` unconditionally deletes everything. `npm install` preserves
   existing files and only updates what changed. This preserves the ZFS COW advantage.

3. **Pre-warm all caches in golden image:** `.next/cache`, `.eslintcache`, `.tsbuildinfo`
   for each benchmark project. On unchanged source files, lint + typecheck + build become
   incremental.

4. **ZFS zvol tuning:** Use `volblocksize=16K` (ZFS 2.2 default), enable zstd compression
   (3-4x ratio on JS/JSON). Consider a ZFS special vdev for metadata to halve random I/O
   latency. Mount ext4 with `noatime`.

5. **Consider pnpm for golden image preparation.** Even if benchmarks use `npm ci`,
   the golden image itself could be built with pnpm to minimize initial disk usage.
   The `npm ci` benchmark measures real-world behavior; the golden image is an optimization.

6. **Track stat() calls per phase.** Use `strace -c -f` or gVisor's syscall logging to
   count stat() calls during lint and typecheck phases. High stat() counts indicate module
   resolution overhead — addressable via TypeScript path mappings or `--moduleResolution bundler`.
