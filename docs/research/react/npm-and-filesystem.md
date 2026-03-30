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

| Project | node_modules size | Approximate file count | Typical install time (npm ci, cold) |
|---------|-------------------|----------------------|-------------------------------------|
| next-learn (small) | ~200-300 MB | ~20K-30K files | ~15-30s |
| taxonomy (medium) | ~400-600 MB | ~40K-60K files | ~30-60s |
| cal.com (large monorepo) | ~800 MB - 1.2 GB | ~80K-120K files | ~60-120s |

These are rough estimates. The exact numbers depend on lockfile state and deduplication.

**The fundamental problem:** JavaScript's ecosystem produces an extreme number of small files.
A typical `node_modules` directory contains:
- Tens of thousands of `.js` files (average 2-10 KB each)
- Thousands of `.json` files (package.json, tsconfig.json manifests)
- Thousands of `.d.ts` type definition files
- README.md, LICENSE, CHANGELOG.md in every package

## fsync pressure from npm

npm uses `graceful-fs` as its filesystem layer, which wraps Node.js's `fs` module with
retry logic for `EMFILE` (too many open file descriptors).

**The fsync problem:** When npm extracts packages, it writes files and calls `fs.writeFileSync`
on many of them. Node.js's `fs.writeFileSync` does NOT call `fsync` by default — but many
packages in the npm ecosystem use `write-file-atomic` or similar libraries that DO call
`fsync`/`fdatasync` to ensure durability.

The `write-file-atomic` package (used by npm internals and many popular packages) writes to
a temp file, calls `fsync`, then `rename`. This pattern generates:
1. `open` (temp file)
2. `write` (contents)
3. `fsync` (force to disk)
4. `rename` (atomic swap)
5. `close`

**Measured impact:** OBuilder's research (see `docs/research/obuilder.md`) found that
intercepting `fsync`/`fdatasync`/`msync`/`sync`/`syncfs`/`sync_file_range` via seccomp
and returning success without syncing provides a "massive speedup for `npm install` style
workloads." This is safe in CI because if the build crashes, the result is discarded.

**Applicability to forge-metal:** gVisor's `runsc` supports seccomp profiles. A profile
that intercepts sync syscalls could eliminate the single biggest I/O bottleneck in the
`deps` phase. The golden image already has `node_modules` pre-populated, but for
projects where the lockfile changes (every real PR), `npm ci` will delete and re-extract.

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

**Recommendation for forge-metal zvols:**
- **Don't tune recordsize for node_modules** — small files already get small blocks
- **Do set `xattr=sa`** — stores extended attributes inline in the inode instead of in
  hidden subdirectories. Reduces I/O for metadata-heavy workloads.
- **Do set `dnodesize=auto`** — allows larger dnodes to store extended attributes inline.
- For zvols specifically, `volblocksize` is the relevant parameter (fixed, not dynamic
  like recordsize). The ext4 filesystem inside the zvol handles its own block allocation.
  A 4K `volblocksize` matches ext4's default block size.

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

**pnpm's advantage:** Content-addressable global store + hard links. When installing
packages, pnpm creates hard links from the global store to `node_modules/.pnpm/`, then
symlinks from `node_modules/<package>` to the `.pnpm` store.

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

**Module resolution stat() storm:** `tsc` resolves every import by trying multiple file
paths. For `import { foo } from './bar'`, it tries:
- `./bar.ts`, `./bar.tsx`, `./bar.d.ts`
- `./bar/index.ts`, `./bar/index.tsx`, `./bar/index.d.ts`
- `./bar.js`, `./bar.jsx` (for JS projects)

Each attempt is a `stat()` call. For a project with 500 source files averaging 10 imports
each, that's potentially 5000 imports × 6-9 stat() attempts = 30K-45K stat() calls.

**`--incremental` with `.tsbuildinfo`:** TypeScript can persist its dependency graph to
`.tsbuildinfo`. On subsequent runs, it only re-checks files whose dependencies changed.
This is the TypeScript equivalent of Next.js's build cache.

## Applicability to forge-metal

1. **fsync interception is the #1 optimization for deps phase.** Seccomp profile in gVisor
   intercepting `fsync`/`fdatasync`/`sync`/`syncfs`/`sync_file_range` → return 0. Safe
   because CI results are discarded on crash. See OBuilder's proven approach.

2. **Lockfile-hash skip:** When golden image lockfile matches PR lockfile, skip `npm ci`
   entirely. This eliminates the most expensive phase. Track via `lockfile_changed` in
   CIEvent.

3. **Pre-warm all caches in golden image:** `.next/cache`, `.eslintcache`, `.tsbuildinfo`
   for each benchmark project. On unchanged source files, lint + typecheck + build become
   incremental.

4. **ZFS zvol tuning:** Set `xattr=sa` and `dnodesize=auto` on the golden zvol. Use 4K
   `volblocksize` to match ext4 default. Don't bother with `special_small_blocks` on
   NVMe-only pools.

5. **Consider pnpm for golden image preparation.** Even if benchmarks use `npm ci`,
   the golden image itself could be built with pnpm to minimize initial disk usage.
   The `npm ci` benchmark measures real-world behavior; the golden image is an optimization.

6. **Track stat() calls per phase.** Use `strace -c -f` or gVisor's syscall logging to
   count stat() calls during lint and typecheck phases. High stat() counts indicate module
   resolution overhead — addressable via TypeScript path mappings or `--moduleResolution bundler`.
