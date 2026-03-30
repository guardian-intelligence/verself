# React/JS Toolchain Landscape — Compiler, Linting, Testing

> The JavaScript toolchain is undergoing a Rust rewrite wave. Every major tool has a
> faster Rust/Go/Zig replacement that changes the CI time equation. This document
> maps the landscape as of March 2026.
>
> Sources: React blog, oxc benchmarks, Biome docs, Vitest docs, Bun docs
>
> Conducted 2026-03-30.

## React Compiler (formerly React Forget)

The React Compiler is a **build-time tool** that automatically memoizes React components
and hooks. It eliminates the need for manual `useMemo`, `useCallback`, and `React.memo`.

**Release timeline:**
- May 2024: Experimental release
- April 2025: Release Candidate
- **October 2025: React Compiler 1.0** (stable)

**How it works:**
1. Runs as a Babel plugin or SWC transform during the build
2. Analyzes component render functions for memoization opportunities
3. Inserts automatic memoization boundaries based on dependency analysis
4. Output code includes React's `useMemoCache` hook for caching

**Build-time impact:**
- Adds a compilation pass over every component file
- Meta reports no significant build time increase — the compiler is fast because it
  operates on ASTs already parsed by SWC
- **Runtime impact is the selling point:** Up to 12% faster initial loads, 2.5x faster
  interactions on Meta Quest Store

**Ecosystem integration:**
- Supported in Next.js, Expo, and Vite as of v1.0
- New Next.js apps can start with the compiler enabled by default

**Applicability to forge-metal:** The React Compiler adds negligible build overhead but
significantly changes the *output* — more memoized components mean different runtime
behavior. If benchmark projects adopt it, build phases won't change much but the compiled
output will differ. Not a CI optimization lever.

## The Rust linting revolution

### oxlint (Oxidation Compiler project)

[oxlint](https://oxc.rs/docs/guide/usage/linter.html) is a Rust-based linter from the
[oxc project](https://oxc.rs/). It reimplements ESLint rules in Rust.

**Performance:**
- **50x-100x faster than ESLint** depending on core count
- MacBook Pro M2 Max: 499ms vs ESLint 31.0s (**62x** speedup)
- MacBook Pro M4 Max: 177ms vs ESLint 21.0s (**118x** speedup)

Source: [oxc-project/bench-linter](https://github.com/oxc-project/bench-linter)

**Rule coverage:**
- ~400+ rules implemented (as of March 2026)
- Covers most of `eslint:recommended`, `@typescript-eslint/recommended`,
  `eslint-plugin-react`, `eslint-plugin-react-hooks`, `eslint-plugin-import`
- **Missing:** Custom ESLint rules, some framework-specific plugins (`eslint-plugin-next`)
- **New (March 2026):** JavaScript plugin support in alpha — allows writing custom rules

**How it works:**
- Single Rust binary, no Node.js dependency
- Parses files using oxc's own parser (not `@typescript-eslint/parser`)
- Runs rules in parallel across files
- No `node_modules` resolution needed for the linter itself

**Important:** `next lint` was **removed in Next.js 16**. Projects should use ESLint
directly (`npx eslint .`). This changes the lint phase invocation in benchmark workloads.
Source: [Next.js ESLint guide 2026](https://thelinuxcode.com/nextjs-eslint-a-practical-modern-guide-for-2026/)

**Applicability to forge-metal:** Replacing ESLint with oxlint in the `lint` phase would
reduce it from seconds-to-minutes down to sub-second. However, benchmark fidelity requires
using the same tools as the benchmarked projects. Consider adding an `oxlint` phase as a
**comparison metric** rather than replacing ESLint.

### Biome (formerly Rome)

[Biome](https://biomejs.dev/) is an all-in-one toolchain: linter + formatter in a single
Rust binary. Replaces both ESLint and Prettier.

**Performance (2025-2026 benchmarks):**
- Linting 10K files: ESLint 45.2s vs Biome **0.8s** (~56x)
- Formatting 10K files: Prettier 12.1s vs Biome **0.3s** (~40x)
- Combined: ESLint+Prettier ~57s vs Biome **~1.1s**

**Coverage:**
- 423+ lint rules (as of Biome v2.3)
- 97% Prettier-compatible formatting
- **Type-aware linting** added in v2.0 (March 2025)
- **Missing:** ~20% of common ESLint rules, most framework-specific plugins

**Status:**
- v1.0 August 2023, v2.0 March 2025
- 15M+ monthly downloads (vs ESLint's 79M)
- Biome 2.0 introduced type-aware linting (previously a major gap)

**vs oxlint:** Biome is an integrated tool (lint + format). oxlint is lint-only but has
broader rule coverage and the oxc parser is also used by Rolldown (Vite's future bundler).
In practice, both achieve similar speedups over ESLint.

## TypeScript alternatives for type-checking

### Isolated declarations (`--isolatedDeclarations`)

TypeScript 5.5 (June 2024) added `--isolatedDeclarations`, which enforces that each file's
exported types can be generated without cross-file type inference. This enables:
- **Parallel declaration emit** — each file can be processed independently
- **Faster type-checking** — no global type inference needed for `.d.ts` generation
- **Third-party type-checkers** — tools like `stc` (Rust) or `oxc` can generate `.d.ts`
  without reimplementing all of TypeScript's type system

This doesn't speed up `tsc --noEmit` directly but enables future tools to replace it.

### TypeScript performance tuning for CI

**`--skipLibCheck`**: Skips type-checking of `.d.ts` files (library types). Measured impact:
- aws-sdk project: 465 MB → 375 MB memory, 5.38s → 3.05s (**43% faster**)
- Safe for CI: library types are checked by their own CI, not yours

**`--incremental` with `.tsbuildinfo`**: Persists dependency graph to disk. On subsequent
runs, only re-checks changed files and their dependents. Requires `.tsbuildinfo` in the
golden image.

**Project references (`--build`)**: For monorepos, allows checking individual packages
independently. Each package gets its own `.tsbuildinfo` and can be cached separately.

**`--generateTrace traceDir`**: Generates Chromium trace files showing time spent on each
compilation phase (parsing, binding, checking, emit). Critical for finding type-checking
bottlenecks.

## Test runner landscape

### Vitest

[Vitest](https://vitest.dev/) is a Vite-native test runner. For Next.js projects:
- Uses the same transform pipeline as Vite (esbuild/SWC)
- **3.7x faster than Jest** (200 files, 1500 tests: Jest 45s → Vitest 12s)
- Built-in TypeScript support without `ts-jest`
- Compatible with Jest's API (`describe`, `it`, `expect`)
- Better ESM support than Jest
- `vitest --changed` runs only tests affected by git changes

### Bun as test runner

[Bun](https://bun.sh/) includes a built-in test runner (`bun test`):
- No transpilation step — Bun natively runs TypeScript
- **11x faster than Jest** (200 files, 1500 tests: Jest 45s → Bun 4s)
  Source: [PkgPulse test runner benchmarks](https://www.pkgpulse.com/blog/bun-test-vs-vitest-vs-jest-test-runner-benchmark-2026)
- Jest-compatible API
- Not yet widely adopted in Next.js ecosystem (Next.js itself doesn't support Bun runtime)

### Jest (status quo)

Most Next.js projects still use Jest. Next.js has deep Jest integration via `next/jest`:
- Auto-mocks CSS/image imports
- Uses SWC for transpilation (replaces `babel-jest`)
- Loads `.env` automatically

**The CI time problem:** Jest startup is slow (~2-5s before first test runs) due to module
resolution and transform setup. For projects with fast tests, startup dominates.

## The full "Rust rewrite" stack

If every tool in the JS toolchain were replaced with its Rust equivalent:

| Tool | Current | Rust replacement | Speedup |
|------|---------|-----------------|---------|
| Package manager | npm (31s clean) | pnpm (7.6s) or Bun (fastest) | 4x |
| Linter | ESLint (45s/10K files) | oxlint (0.5s) or Biome (0.8s) | 50-100x |
| Formatter | Prettier (12s/10K files) | Biome (0.3s) | 40x |
| Transpiler | Babel (old) | SWC (already default in Next.js) | 17x |
| Bundler | webpack | Turbopack (already default in Next.js 16) | varies |
| Type-checker | tsc | None yet (stc abandoned, oxc partial) | 1x |
| Test runner | Jest | Vitest (5-10x) or Bun (20-50x) | 5-50x |

**The bottleneck shifts:** With fast linting (oxlint) and fast bundling (Turbopack), the
remaining bottleneck is `tsc --noEmit`. TypeScript's type-checker has no fast replacement
and uses single-threaded in-process checking. For large projects, it remains the slowest
phase.

## Applicability to forge-metal

1. **oxlint as comparison metric.** Add an `oxlint` phase alongside ESLint in benchmark
   workloads. Compare wall-clock times to quantify the "linting tax" of ESLint.

2. **TypeScript is the irreducible bottleneck.** Once npm ci (fsync bypass), ESLint (oxlint),
   and build (Turbopack cache) are optimized, `tsc --noEmit` will dominate CI time. Focus
   optimization on `--skipLibCheck`, `--incremental`, and pre-warmed `.tsbuildinfo`.

3. **Vitest tracking.** As benchmark projects migrate from Jest to Vitest, the `test` phase
   will shrink dramatically. Track which test runner each project uses in CIEvent metadata.

4. **React Compiler is not a CI lever.** It adds negligible build time and changes runtime
   behavior. Not relevant for CI optimization.

5. **The "fast stack" target.** For the updated KPI (p99.9 for 1000-line PR):
   - deps: skip if lockfile unchanged, fsync bypass if not → 0-15s
   - lint: oxlint → <1s (or ESLint with cache → 5-15s)
   - typecheck: tsc --incremental + skipLibCheck → 3-15s
   - build: Turbopack + filesystem cache → 5-30s
   - test: Vitest → 1-10s
   - **Total: 10-70s** (vs current 60-300s baseline)
