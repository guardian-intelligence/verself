# Next.js Build Pipeline — SWC, Turbopack, and Build Internals

> Deep dive into what happens during `next build` and how each compilation phase maps to
> I/O, CPU, and memory pressure. Primary focus: understanding the build to optimize it
> for ZFS+Firecracker CI.
>
> Sources: Next.js docs (v16.2.1), SWC docs, Turbopack API reference, Vercel blog
>
> Conducted 2026-03-30.

## The five CI phases and what they actually do

forge-metal's benchmark workloads run five sequential phases. Here's what happens inside
each one at the syscall/process level:

| Phase | Command | Dominant cost | I/O pattern |
|-------|---------|---------------|-------------|
| deps | `npm ci` | Extract tarballs → write 10K-100K small files | Write-heavy, many fsyncs, inode-heavy |
| lint | `npm run lint` (ESLint) | Parse + lint every .ts/.tsx file | Read-heavy, stat-heavy (module resolution) |
| typecheck | `npx tsc --noEmit` | Full type-check, no emit | Read-heavy, memory-heavy (AST in heap) |
| build | `npm run build` | SWC transpile + Turbopack/webpack bundle + SSG | CPU+memory, write .next/ output |
| test | `npm test` | Jest + jsdom or Node test runner | Mixed, varies by test suite |

## SWC compiler — the compilation engine

Next.js replaced Babel with [SWC](https://swc.rs/) (Rust) in v12. Every `.ts`/`.tsx`/`.js`/`.jsx`
file passes through SWC for transpilation.

**Performance claims (from Next.js docs):**
- Compilation: **17x faster than Babel**
- Minification: **7x faster than Terser** (default since v13)
- Fast Refresh: **~3x faster** than Babel-based
- Overall builds: **~5x faster** than Babel-based

**What SWC does during `next build`:**
1. Parses each source file into an AST (Rust)
2. Applies transforms: JSX → `React.createElement`/`jsx-runtime`, TypeScript stripping,
   styled-components/Emotion/Relay transforms, `removeConsole`, decorator support
3. Minifies output bundles (replaces Terser)
4. Generates source maps (optional, memory-intensive)

**Key detail for CI:** SWC is CPU-bound and parallelizes across cores. It does **not**
type-check — that's `tsc`'s job. This means build and typecheck can theoretically run
in parallel (Next.js does not do this by default, but CI can).

**Profiling:** `experimental.swcTraceProfiling: true` generates Chromium trace files at
`.next/swc-trace-profile-${timestamp}.json`. Loadable in `chrome://tracing` or
[Perfetto](https://ui.perfetto.dev/).

## Turbopack — the bundler

Turbopack is an incremental bundler written in Rust, integrated into Next.js.

**Status timeline:**

| Version | Milestone |
|---------|-----------|
| v15.0.0 | Turbopack stable for `next dev` |
| v15.3.0 | Experimental support for `next build` |
| v15.5.0 | Beta support for `next build --turbopack` |
| **v16.0.0** | **Turbopack is the default bundler** for both `next dev` and `next build` |

**Architecture differences from webpack:**
- **Unified graph:** Single computation graph for client + server + edge. Webpack runs
  separate compilers and stitches results.
- **Incremental computation:** Function-level caching. Results memoized and only recomputed
  when inputs change. Parallelized across CPU cores.
- **Lazy bundling:** Only bundles what's actually requested (dev) or needed (build). Reduces
  peak memory.
- **No plugin system:** Webpack plugins don't work. Supports webpack *loaders* via config.
  This means `webpack-bundle-analyzer` and similar tools are not available with Turbopack.

**Turbopack caching (Next.js 16):**

| Option | Dev default | Build default |
|--------|-------------|---------------|
| `turbopackFileSystemCacheForDev` | `true` | N/A |
| `turbopackFileSystemCacheForBuild` | N/A | `false` (opt-in) |

When filesystem cache is enabled for builds, Turbopack persists its computation graph to
disk. Subsequent builds skip work whose inputs haven't changed. **This is the Turbopack
equivalent of `.next/cache` for webpack.**

**Experimental build-time flags worth testing:**

| Flag | What it does | Build default |
|------|-------------|---------------|
| `turbopackTreeShaking` | Advanced module-fragment tree shaking | `false` |
| `turbopackScopeHoisting` | Concatenate modules (like webpack's ModuleConcatenationPlugin) | `true` |
| `turbopackRemoveUnusedImports` | Dead import elimination | `true` |
| `turbopackRemoveUnusedExports` | Dead export elimination | `true` |

**Trace files for debugging:** `NEXT_TURBOPACK_TRACING=1 next build` generates
`.next/dev/trace-turbopack` for analysis.

## The `.next` output directory

After `next build`, the `.next` directory contains:

```
.next/
├── cache/              ← Build cache (webpack chunks, images, fetch cache)
│   ├── webpack/        ← Serialized webpack module graph (or turbopack equivalent)
│   ├── images/         ← Optimized image cache
│   └── fetch-cache/    ← ISR/SSG data cache
├── server/             ← Server-side compiled code
│   ├── app/            ← App Router compiled routes
│   ├── pages/          ← Pages Router compiled routes
│   ├── chunks/         ← Server-side code-split chunks
│   └── *.nft.json      ← File trace manifests (from @vercel/nft)
├── static/             ← Client-side static assets
│   ├── chunks/         ← Client JS bundles (code-split)
│   ├── css/            ← Extracted CSS
│   └── media/          ← Static imports (images, fonts)
├── build-manifest.json ← Maps pages → required JS chunks
├── react-loadable-manifest.json
├── trace                ← Build trace data
└── next-server.js.nft.json ← Production server file dependencies
```

**Size:** For a large project like cal.com, `.next` can be 200-400MB. The `cache/` subdirectory
alone can be 100-200MB for webpack builds. Turbopack's cache format is different and typically
smaller.

**Key insight for forge-metal:** The `.next/cache` directory is the critical artifact for
build caching. If it's present and valid at build start, subsequent builds can skip
recompilation of unchanged modules. The cache key is:
- lockfile hash (package-lock.json)
- source file hashes (*.js, *.jsx, *.ts, *.tsx)

This means golden images with pre-warmed `.next/cache` could dramatically reduce build times.
However, the cache must match the exact source tree — a different commit invalidates it.

## File tracing with @vercel/nft

During `next build`, Next.js uses [`@vercel/nft`](https://github.com/vercel/nft) (Node File
Trace) to statically analyze `import`, `require`, and `fs` usage to determine all files that
a page might load. Results are output as `.nft.json` files.

This is primarily for deployment optimization (standalone output mode) but the traces are
useful for understanding I/O patterns: they show exactly which files from `node_modules`
are read during the build.

## Build lifecycle hooks

Next.js 16 added `compiler.runAfterProductionCompile`:

```js
module.exports = {
  compiler: {
    runAfterProductionCompile: async ({ distDir, projectDir }) => {
      // Runs after compilation, before type-checking and SSG
    },
  },
}
```

This hook could be used to collect build artifacts (sourcemaps, traces) for CI telemetry.

## Webpack build workers

Since v14.1.0, Next.js runs webpack compilations in a **separate Node.js worker process**.
This caps memory usage of the main process and allows the OS to reclaim worker memory after
compilation. Important for cgroup-monitored CI where `memory.peak` is tracked.

The worker is enabled by default unless the project has custom webpack config.

## Memory pressure during build

Next.js builds are memory-hungry. The official docs recommend:

- **`--experimental-debug-memory-usage`** (v14.2+): Prints heap/GC stats continuously.
  Auto-takes heap snapshots near OOM. Send `SIGUSR2` to take manual snapshots.
- **`experimental.webpackMemoryOptimizations: true`**: Reduces peak memory at cost of
  slightly slower compilation.
- **Disable source maps**: `productionBrowserSourceMaps: false` and
  `experimental.serverSourceMaps: false` reduce memory significantly.
- **Disable type-checking in build**: `typescript.ignoreBuildErrors: true` skips the
  "Running TypeScript" step, which can consume 300-500MB alone for large codebases.

**Applicability to forge-metal:** cgroup v2 `memory.peak` tracking will capture this. If
builds OOM in the microVM, we need to either increase VM memory or split type-checking
from the build phase. The benchmark already runs `tsc --noEmit` as a separate phase —
we could disable Next.js's built-in type-check during build to avoid double-checking.

## Parallel compilation opportunities

Next.js has experimental flags for parallelism:

```js
module.exports = {
  experimental: {
    parallelServerCompiles: true,    // Parallel server compilation
    parallelServerBuildTraces: true,  // Parallel file tracing
  },
}
```

These can reduce wall-clock build time on multi-core machines but increase peak memory.

## What happens during `next build` — step by step

1. **Load config** — Parse `next.config.js`, resolve plugins
2. **Collect pages/routes** — Scan `app/` and `pages/` directories
3. **Client compilation** — SWC transpile + Turbopack/webpack bundle all client-side code
4. **Server compilation** — Same for server components, API routes, middleware
5. **Type-checking** — Run `tsc` in worker (unless disabled). This is the "Running TypeScript" step
6. **Static generation (SSG)** — Pre-render static pages, generate HTML
7. **File tracing** — `@vercel/nft` analyzes dependencies for standalone output
8. **Write manifests** — `build-manifest.json`, route manifests, etc.

Steps 3-4 are the heaviest. With Turbopack, they use the unified graph and share work.
Steps 3-5 could theoretically run in parallel (type-checking doesn't depend on bundling).

## Applicability to forge-metal

1. **Golden image pre-warming:** Pre-populate `.next/cache` in the golden zvol for each
   benchmark project. This turns the first build on a clone from cold to warm. Expected
   speedup: 30-50% for incremental builds.

2. **SWC trace profiling:** Enable `swcTraceProfiling` in benchmark runs to generate
   per-build flame graphs. Store traces in ClickHouse as wide event attachments.

3. **Separate build and typecheck:** Since the benchmark already runs `tsc --noEmit` as
   a separate phase, disable Next.js's built-in type-check during `next build` with
   `typescript.ignoreBuildErrors: true`. Saves ~300-500MB peak memory and avoids
   double type-checking.

4. **Turbopack filesystem cache:** Enable `turbopackFileSystemCacheForBuild` in benchmark
   projects to test cache-warm vs cache-cold build times on ZFS clones.

5. **Memory tracking:** Use `--experimental-debug-memory-usage` in benchmark runs to
   correlate heap pressure with cgroup `memory.peak` measurements.
