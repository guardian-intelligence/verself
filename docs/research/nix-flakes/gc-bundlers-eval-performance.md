# Nix: GC Internals, `nix bundle`/Bundlers, and Eval Performance

Three topics that are individually underdocumented and collectively form the production-readiness layer of a Nix-based system:

1. **Garbage collection internals** — every root type, the mark/sweep algorithm, `keep-outputs`/`keep-derivations`, store optimisation, and reference scanning.
2. **`nix bundle` and the `bundlers` output type** — the flake schema, CLI mechanics, and the ecosystem of bundler implementations.
3. **Nix language evaluation model and performance** — thunk lifecycle, force semantics, string/path coercion traps, and practical profiling tools.

---

## Part 1: Garbage Collection Internals

### 1.1 The GC Root Universe

A store path survives garbage collection if and only if it is reachable from at least one **GC root**. Nix collects roots from several places:

#### Filesystem roots (`/nix/var/nix/gcroots/`)

The primary root directory. Every symlink directly inside it, or in any subdirectory, whose target resolves to a `/nix/store/` path is a live root. The collector traverses recursively but refuses to follow non-store symlinks *inside* the reached paths to prevent infinite recursion.

```
/nix/var/nix/gcroots/
├── bar -> /nix/store/d718ef…-foo           # direct root
├── auto/
│   └── dn54lcypm8f8… -> /home/alice/result # indirect root (auto-created)
└── per-user/
    └── alice/
        └── channels -> /nix/store/…        # per-user channel root
```

The `auto/` subdirectory is maintained by the `--indirect` mechanism (see §1.4). The `per-user/` directory holds roots created by `nix-env` and channel operations for individual users.

#### Profile symlink chain

`/nix/var/nix/profiles/` is itself scanned as a gcroots directory. The profile symlink chain looks like:

```
/nix/var/nix/profiles/default          -> default-42-link
/nix/var/nix/profiles/default-42-link  -> /nix/store/zzz…-user-environment
```

Every generation link (`default-N-link`) is a GC root. This means **all generations** of all profiles are live by default. `nix-collect-garbage -d` ("delete old") first runs `nix-env --delete-generations old` across every profile in `/nix/var/nix/profiles`, removing all generation links except the current one, and then runs the GC. This is the standard "free up space" command; without `-d` the GC can only delete paths unreferenced by any generation.

Per-user profiles live at `/nix/var/nix/profiles/per-user/$USER/` and follow the same pattern.

Time-based deletion: `nix-collect-garbage --delete-older-than 30d` removes profile generations older than 30 days before running the GC. Equivalent to `nix-env --delete-generations 30d` across all profiles.

#### Temporary roots (`/nix/store/.tmp-*`)

When a Nix process starts building a derivation it calls `addTempRoot()`, which writes the target store path as a null-terminated string into a per-process file and acquires a **shared** (non-blocking) GC lock via `FdLock`. The GC itself must acquire an **exclusive** lock before it can proceed. If the exclusive lock cannot be taken immediately (because builders hold shared locks), the GC connects to a Unix domain socket at `$NIX_STORE_DIR/gc-socket/socket` and registers roots there. The socket server in `serverThread` receives path strings and acknowledges with `"1"`. Importantly, Nix stores only the hash part of the path (ignoring suffixes like `.lock`, `.chroot`, `.check`) in `tempRoots` so that helper files for the same build are automatically covered.

This two-phase mechanism (file lock + socket fallback) guarantees that a path being built by one process cannot be GC'd by another process running simultaneously.

#### Runtime roots (live process scan)

`findRuntimeRoots()` scans the running system for store paths referenced by live processes. On Linux it reads:

- `/proc/$pid/exe` — the executable symlink
- `/proc/$pid/cwd` — the process working directory
- `/proc/$pid/fd/*` — all open file descriptors
- `/proc/$pid/maps` — memory-mapped files (catches `.so` libraries loaded at runtime)
- `/proc/$pid/environ` — environment variables

For each of these, the implementation applies a boost regex built from the configured store directory:

```cpp
boost::regex storePathRegex(
    quoteRegexChars(config.storeDir) +
    R"(/[0-9a-z]+[0-9a-zA-Z\+\-\._\?=]*)"
);
```

This matches any substring that looks like a Nix store path. On macOS and other systems without `/proc`, Nix shells out to `lsof` and parses its output with the pattern `^n(/.*?)$` to extract open file paths.

Additionally, Linux-specific kernel config paths are checked: `/proc/sys/kernel/modprobe`, `/proc/sys/kernel/fbsplash`, `/proc/sys/kernel/poweroff_cmd`.

**Known gap:** Command-line arguments (`/proc/$pid/cmdline`) are not scanned. A daemon invoked as `xautolock -locker /nix/store/…/bin/xlock` holds a reference only via cmdline; GC will collect the referenced package once it is no longer reachable from profiles. This is a documented limitation (NixOS/nix#3108).

### 1.2 The Mark Phase

`collectGarbage()` runs a **mark phase** using `deleteReferrersClosure()`, which does a breadth-first traversal starting from each candidate path:

1. For each path in the store, check if it is reachable from any root via the **referrers** closure (`queryGCReferrers()` — paths that reference the current path).
2. If `keep-derivations = true` (default), follow deriver edges: `.drv` files referenced by a live output are kept.
3. If `keep-outputs = true` (non-default), follow output edges: build inputs of a live `.drv` are kept.
4. Paths are cached in `referrersCache` to avoid re-scanning.
5. Paths reachable from any root are marked "alive"; unreachable paths are queued for deletion.

The result is the **live set** — all paths that are referenced transitively from any root.

### 1.3 The Sweep Phase and `--max-freed`

The sweep phase calls `topoSortPaths()` to produce a topological ordering of dead paths (leaves first), then for each path:

1. `invalidatePathChecked()` — removes the path from the SQLite store database.
2. `deleteFromStore()` — removes the path directory from disk.
3. Freed bytes are accumulated.
4. If `options.maxFreed > 0` and `freedBytes >= maxFreed`, `GCLimitReached` is thrown, stopping the sweep immediately.

This means `--max-freed 10G` will stop after freeing approximately 10 GB. The store is left in a consistent state — partial deletion is safe because every deleted path was unreachable from roots.

Preview commands:
```bash
nix-store --gc --print-dead    # list what would be deleted
nix-store --gc --print-live    # list what would be kept
nix-store --gc --dry-run       # count bytes without deleting
```

### 1.4 `nix-store --add-root` and `--indirect`

```bash
# Direct root: symlink must live inside a gcroots subdirectory
nix-store --add-root /nix/var/nix/gcroots/myapp result

# Indirect root: symlink can live anywhere (e.g., current directory)
nix-build '<nixpkgs>' -A hello --add-root /home/alice/result --indirect
```

Without `--indirect`, the root symlink must be inside a directory that the GC scans. With `--indirect`, `addIndirectRoot()` creates a **hashed symlink** in `/nix/var/nix/gcroots/auto/`:

```
/nix/var/nix/gcroots/auto/dn54lcypm8f8… -> /home/alice/result
/home/alice/result                        -> /nix/store/abc…-hello-2.12
```

The GC follows the chain: `auto/dn54…` → `/home/alice/result` → `/nix/store/abc…`. If `/home/alice/result` is deleted, the `auto/` symlink becomes dangling and the GC ignores it (dangling symlinks are silently skipped). This is exactly the behaviour `nix build` relies on: the `./result` symlink it creates is an indirect root, and when you `rm result`, the store path becomes collectible.

**Limitation:** Indirect roots cannot be moved or renamed. The `auto/` symlink records the original path; if you move `result` to `result2`, the old `auto/` symlink points nowhere and the root is silently lost.

### 1.5 `keep-outputs` and `keep-derivations` (nix.conf)

These two settings change what the GC traverses beyond the direct closure of GC roots.

| Setting | Default | Effect |
|---------|---------|--------|
| `keep-derivations` | `true` | `.drv` files whose outputs are GC roots are kept, even if the `.drv` has no symlink root of its own |
| `keep-outputs` | `false` | Build-time inputs of live `.drv` files are kept |

With defaults (`keep-derivations = true`, `keep-outputs = false`): you can always trace a store path back to its derivation (useful for `nix-store -q --deriver`), but build-time-only tools (compilers, test runners used only at build time) are collected after the build succeeds.

With `keep-outputs = true` as well: all build inputs are kept as long as the `.drv` is live. This is the developer configuration — set both in `~/.config/nix/nix.conf` to prevent `nix develop` shell dependencies from vanishing during a GC while you are working:

```
keep-outputs = true
keep-derivations = true
```

**The GC documentation warning:** Using `nix-store --gc --ignore-liveness` with `--delete` overrides both settings, which can delete derivations still needed for outputs that are rooted. Nix disables `keep-outputs`/`keep-derivations` when `--ignore-liveness` is active to avoid confusing half-deletions.

### 1.6 `nix-store --optimise` and `auto-optimise-store`

`nix-store --optimise` (equivalently `nix store optimise`) scans the entire store and replaces duplicate files with **hardlinks** to a single canonical inode. Typical space savings: 25–35%.

**Algorithm:**

1. For each regular file in the store, compute the hash over its **NAR serialisation** (which includes the executable bit, so `foo` and executable `foo` with identical bytes are still different). Symlinks are hashed by their target string, not destination.
2. Look up the hash in `/nix/store/.links/` (the hardlink index directory).
3. If no entry exists: create a hardlink in `.links/` as the canonical reference.
4. If an entry exists: create a temporary hardlink from the canonical reference, then **atomically rename** it over the current file. The original inode is now shared.
5. Race handling: if another process wins the `.links/` race, the error is caught and the code falls through gracefully.

**`auto-optimise-store = true`** (nix.conf): triggers per-path deduplication incrementally after every store path is added, using the same `.links/` index. The tradeoff: each build now runs `optimisePath_()` on every output file, which adds measurable latency — particularly for workloads that produce many small files (e.g., system environments, `node_modules` closures). For these workloads, periodic manual `nix store optimise` (e.g., in a cron job) is preferable to `auto-optimise-store`.

### 1.7 Store Path Reference Scanning

After every build, Nix scans all output files to discover which other store paths they reference. This is how the runtime closure is computed.

**Mechanism (`RefScanSink`):** The scanner operates as a streaming sink (not a regex over a loaded buffer). It processes the file byte-by-byte, maintaining a partial-match state for each candidate hash prefix. The 32-character Nix base32 hash component of each input store path is extracted. As data flows through the sink, any occurrence of a known 32-char hash segment anywhere in the binary (text files, ELF binaries, shell scripts, compiled bytecode) is recorded.

The scan:
1. Converts each input store path to its 32-char hash component (`/nix/store/[HASH]-name` → `[HASH]`).
2. Streams every file in the output through `RefScanSink`.
3. Any hash found → the corresponding store path is a reference.
4. Per-file sinks: a fresh sink is created for each file so cross-file contamination cannot occur.

**String mangling:** Nix compilers and builders deliberately mangle store path strings in binary outputs to avoid spurious self-references. For example, when building `pkgs.hello`, the builder replaces the source `.drv` hash in any strings it writes with a placeholder, so the output does not falsely reference its own build inputs.

**Derivation attributes that control reference checking:**

```nix
derivation {
  # Only these store paths may appear in the output
  allowedReferences = [ pkgs.glibc pkgs.openssl "out" ];

  # These store paths must NOT appear in the output
  disallowedReferences = [ pkgs.bash ];

  # The entire output closure may only reference these (recursive)
  allowedRequisites = [ pkgs.glibc ];

  # The entire output closure must not reference these (recursive)
  disallowedRequisites = [ pkgs.python3 ];
}
```

`allowedReferences`/`disallowedReferences` check only direct references found in the output files. `allowedRequisites`/`disallowedRequisites` check the **full transitive closure**. NixOS uses `allowedReferences = []` on initial ramdisk derivations to verify they carry no accidental store dependencies.

With `__structuredAttrs = true`, per-output checking is available via `outputChecks.out.allowedReferences = [...]`. The `ignoreSelfRefs` attribute controls whether self-references (a path referencing itself) count toward these checks.

---

## Part 2: `nix bundle` and Bundler Outputs

### 2.1 The `bundlers` Flake Output Schema

A bundler is a flake output of the form:

```nix
bundlers.<system>.<name> = drv: derivation { ... };
```

A bundler is a **function** that accepts an arbitrary value (typically a derivation or an `apps` entry) and returns a derivation. The returned derivation, when built, produces a self-contained artifact.

Minimal example in a `flake.nix`:

```nix
{
  outputs = { self, nixpkgs }: {
    bundlers.x86_64-linux = rec {
      # identity bundler: pass-through
      identity = drv: drv;

      # lock a specific version regardless of input
      toHello = _: nixpkgs.legacyPackages.x86_64-linux.hello;

      default = identity;
    };
  };
}
```

The `default` attribute is used when no `#name` is specified in the `--bundler` flag.

### 2.2 `nix bundle` CLI

```bash
# Syntax
nix bundle [options] <installable>

# Use the default bundler (github:NixOS/bundlers#default)
nix bundle nixpkgs#hello
# Creates ./hello (a self-contained executable on Linux)

# Use a named bundler from the default repo
nix bundle --bundler github:NixOS/bundlers#toDockerImage nixpkgs#hello

# Use a bundler from a custom flake
nix bundle --bundler ./my-flake#myBundler nixpkgs#curl

# The installable can be any flake attribute, not just packages
nix bundle --bundler github:NixOS/bundlers#toArx .#apps.x86_64-linux.myapp
```

`nix bundle` only works on **Linux**. It:
1. Evaluates the bundler function by passing the resolved installable derivation (or app definition) as the argument.
2. Builds the derivation returned by the bundler.
3. Creates a symlink (or copies, depending on the bundler) in the current directory.

If no `--bundler` is given, Nix tries `bundlers.<system>.default` from `github:NixOS/bundlers`.

### 2.3 The Official `github:NixOS/bundlers` Repository

The canonical bundler collection. Marked **UNSTABLE** — breaking changes may occur without notice. Available bundlers as of 2024:

| Bundler | What it produces | Key dependency |
|---------|-----------------|----------------|
| `toArx` | Self-extracting archive using `arx` bootstrapper | `nix-bundle` (matthewbauer) |
| `toAppImage` | Type 2 AppImage (SquashFS + runtime) | `nix-appimage` (ralismark) |
| `toDockerImage` | Docker image tarball via `dockerTools.buildLayeredImage` | nixpkgs dockerTools |
| `toRPM` | RPM package | rpm toolchain |
| `toDEB` | Debian `.deb` package | dpkg toolchain |
| `toBuildDerivation` | The `.drv` file itself as the output | — |
| `toReport` | JSON report of closure metadata | — |

Usage:
```bash
nix bundle --bundler github:NixOS/bundlers#toDockerImage nixpkgs#hello
nix bundle --bundler github:NixOS/bundlers#toArx nixpkgs#hello
nix bundle --bundler github:NixOS/bundlers#toDEB nixpkgs#curl
```

### 2.4 `matthewbauer/nix-bundle` (the `toArx` predecessor)

The original `nix-bundle` project (now maintained at `nix-community/nix-bundle`). It underpins the `toArx` bundler.

**Architecture:**

1. `nix-store --export` dumps the closure as a NAR stream.
2. `arx` (Archive eXecutor) wraps the NAR + a startup script into a self-extracting shell archive using a minimal ELF bootstrapper.
3. At runtime, the archive extracts everything into `./nix/` in a temporary directory, then uses `nix-user-chroot` (Linux namespaces, no root required) to bind-mount `./nix` at `/nix`, making store paths resolve correctly.
4. The target binary is then exec'd inside the chroot.

**Characteristics:**
- Slow startup: extracts entire closure on first run.
- Large: Firefox bundles reach ~150 MB.
- Linux only (requires user namespaces, available since kernel 3.8).
- No runtime dependency: no Nix, no glibc from host required.
- Cannot target cross-architecture (bundles the host's architecture).

### 2.5 `ralismark/nix-appimage` (the `toAppImage` implementation)

A newer, faster alternative that produces **Type 2 AppImages**.

**Architecture:**

1. The Nix closure is packed into a **SquashFS** image (not extracted up-front).
2. A static AppImage runtime binary is prepended to the SquashFS.
3. On execution, the runtime mounts the SquashFS via FUSE and exec's `AppRun`, a wrapper that exposes the bundled `/nix/store` before launching the target binary.

**Differences from `nix-bundle`/`toArx`:**

| Aspect | `toArx` (nix-bundle) | `toAppImage` (nix-appimage) |
|--------|---------------------|----------------------------|
| Payload format | Self-extracting archive | SquashFS (lazy FUSE mount) |
| Startup cost | High (full extraction) | Low (mount on demand) |
| glibc dependency | None (static bootstrap) | None (static AppImage runtime) |
| OpenGL on non-NixOS | Works | Problematic |
| Root-dir files | Visible | Not visible to bundled app |

**Usage via bundler interface:**
```bash
nix bundle --bundler github:ralismark/nix-appimage nixpkgs#hello
```

### 2.6 `toDockerImage` vs Direct `dockerTools.buildLayeredImage`

`github:NixOS/bundlers#toDockerImage` is a convenience wrapper that calls `pkgs.dockerTools.buildLayeredImage` on the input derivation, auto-generating a name and using the default configuration. It is appropriate for quick one-liners:

```bash
nix bundle --bundler github:NixOS/bundlers#toDockerImage nixpkgs#hello
docker load < ./hello
docker run hello /bin/hello
```

`pkgs.dockerTools.buildLayeredImage` directly (in a `flake.nix`) gives full control: custom `config` (entrypoint, env, exposed ports), `maxLayers`, `extraCommands`, `fakeRootCommands`, `/etc/passwd` via `fakeNss`. Use the bundler for exploration; use `buildLayeredImage` directly for production images.

### 2.7 `pkgs.appimageTools` (wrapping, not bundling)

`pkgs.appimageTools` is for the **inverse** operation: running an upstream-provided AppImage under NixOS, not creating one from a Nix derivation. It provides:

- `wrapType1 { name; src; }` — wraps a Type 1 AppImage (squashfs v1)
- `wrapType2 { name; src; }` — wraps a Type 2 AppImage (squashfs v2)
- `extract { name; src; }` — extracts an AppImage to a store path

To create an AppImage **from** a Nix derivation, use `github:ralismark/nix-appimage` via `nix bundle`. To **run** an upstream AppImage under NixOS, use `pkgs.appimageTools.wrapType2`.

---

## Part 3: Nix Language Evaluation Model and Performance

### 3.1 The Thunk Lifecycle

Nix is a **call-by-need** (lazy) language. Every expression that is not immediately available as a primitive value is represented as a **thunk** — a suspended computation.

**Internal value types** (from `src/libexpr/eval.hh`):

| Type tag | Meaning |
|----------|---------|
| `tThunk` | Unevaluated expression + its environment |
| `tApp` | Unapplied function application `f x` |
| `tPrimOpApp` | Partially applied built-in function |
| `tBlackHole` | A thunk currently being evaluated (cycle detection) |
| `nInt`, `nFloat`, `nString`, `nPath`, `nBool`, `nNull` | Primitive values (no thunk) |
| `nAttrs` | Attribute set (header is resolved; individual values may be thunks) |
| `nList` | List (elements may be thunks) |
| `nFunction` | Lambda (not a thunk; the body is evaluated lazily on call) |

**Thunk creation rules** (verified with `NIX_SHOW_STATS=1`):

- Atomic literals (`1`, `"hello"`, `./foo`, `true`, `null`) → no thunk.
- Direct variable references (`let x = 1; in x`) → no thunk for `x` itself.
- `let`-bindings with non-trivial right-hand sides → thunk.
- Attribute set members (`{ a = expr; }`) → thunk per member.
- List elements (`[ expr1 expr2 ]`) → thunk per element.
- Function applications (`f x`) → `tApp` thunk until forced.

**Forcing and memoization:** `forceValue()` in `eval.cc` is called whenever a thunk must produce an actual value. It:

1. Checks the value type. If already a concrete type, returns immediately.
2. If `tBlackHole`: throws `"infinite recursion encountered"` — cycles in `let`-bindings are detected this way.
3. Sets the thunk to `tBlackHole` before evaluating (re-entrancy guard).
4. Evaluates the expression in the captured environment.
5. **Overwrites the thunk in-place** with the resulting value.

Step 5 is the **memoization**: the thunk slot is mutated to hold the concrete value. Subsequent accesses to the same binding return the cached value without re-evaluation. This is why `let x = expensive; in x + x` evaluates `expensive` only once.

### 3.2 `builtins.seq` and `builtins.deepSeq` — Force Semantics

```nix
# seq: evaluate e1 to Weak Head Normal Form, return e2
builtins.seq e1 e2

# deepSeq: evaluate e1 fully (all nested attrs/list elements), return e2
builtins.deepSeq e1 e2
```

**Weak Head Normal Form (WHNF):** the outermost constructor is known. For an attribute set, WHNF means "yes, this is an attrset with these keys" — but the values remain as thunks. For a list, WHNF means "yes, this is a list of N elements" — but each element may be a thunk.

**Critical implication for `lib.foldl'`:** `lib.foldl'` (`nixpkgs/lib/lists.nix`) is the strict variant of `foldl`. It forces the accumulator to WHNF at each step using `builtins.seq`. For a simple integer accumulator, this prevents thunk chain buildup. But for an **attribute set** accumulator (e.g., merging attrs across a large list), forcing to WHNF only forces the top-level attrset constructor — all the values remain as thunks. The thunk chain still builds up inside the attrset values. To fully prevent this, use `builtins.deepSeq acc` inside the fold body:

```nix
# This still accumulates thunks inside the attrset:
lib.foldl' (acc: item: acc // { "${item.name}" = item.value; }) {} bigList

# This forces all values at every step (expensive but prevents stack overflow):
builtins.foldl' (acc: item:
  let next = acc // { "${item.name}" = item.value; };
  in builtins.deepSeq next next
) {} bigList
```

`deepSeq` is semantically equivalent to `seq` followed by a full recursive force of the first argument's spine. Use it sparingly — forcing a large attrset at every fold step is O(n²).

### 3.3 String vs Path Types — Coercion Rules and Traps

Nix has two distinct types that look similar but behave very differently:

| Aspect | `String` (`"foo"`) | `Path` (`./foo`) |
|--------|--------------------|-----------------|
| Type | string with context | filesystem path |
| Interpolation `"${x}"` | concatenated with context propagated | **copies to Nix store** and becomes a store path string |
| `toString x` | identity | converts to absolute path string (NO store copy) |
| `builtins.toPath x` | deprecated | — |

**The path-in-interpolation side effect:** `"${./mydir}"` is not just a string conversion. Nix calls `copyToStore` on `./mydir`, adds it to the Nix store with a hash based on the directory contents, and substitutes the resulting store path (e.g., `/nix/store/abc123-mydir`) into the string. This happens **at evaluation time**, not at build time. It is a controlled impurity that makes the path content-addressed.

**The `outPath` coercion trap:** Attribute sets with an `outPath` attribute can be interpolated into strings:

```nix
let drv = derivation { ... };
in "${drv}"          # interpolates to drv.outPath (e.g., /nix/store/…-hello-2.12)
```

This works on derivations because `mkDerivation` sets `outPath`. The trap: interpolating a derivation adds it to the **string context**, encoding an implicit build dependency. If you then `builtins.readFile "${drv}/result.json"`, Nix will try to **build the derivation during evaluation** (IFD — import from derivation) — an expensive and often unintended operation.

Coercion precedence: `__toString` > `outPath` > error. If both are defined, `__toString` wins.

**`toString` vs interpolation:** `toString ./foo` converts a path to its string representation without copying to the store. `"${./foo}"` triggers the store copy. Use `toString` when you want the path string without the side effect.

### 3.4 `builtins.toJSON` / `builtins.fromJSON` as Deep Force

```nix
# builtins.toJSON forces all values recursively and returns a JSON string
builtins.toJSON { a = 1; b = [ 2 3 ]; }
# => '{"a":1,"b":[2,3]}'

# Round-trip: forces evaluation of every attr, returns a new (fully evaluated) attrset
let x = builtins.fromJSON (builtins.toJSON someAttrs); in x
```

`builtins.toJSON` is effectively a `deepSeq` for attribute sets and lists that only contain JSON-serialisable values (numbers, strings, booleans, null, lists, attrsets). It recursively forces all thunks to produce the JSON string. The `fromJSON` call then reconstructs a fully-evaluated copy.

This is occasionally used as a workaround for cases where you need to ensure an attrset is fully evaluated before passing it somewhere, but it has two limitations:
1. Derivations are serialised as their output path string — the derivation metadata is lost.
2. Paths in the attrset trigger `copyToStore` (see §3.3).
3. Functions cannot be serialised and will error.

### 3.5 Eval Profiling Tools

#### `NIX_SHOW_STATS=1`

Prints evaluation statistics to stderr after evaluation:

```bash
NIX_SHOW_STATS=1 nix-instantiate '<nixpkgs>' -A hello
```

Output includes:
- `nrValues` / `nrValuesFreed` — thunks/values allocated and freed
- `nrEnvs` / `nrEnvsFreed` — environment frames
- `nrThunks` — thunks created
- `nrLookups` — attribute lookups
- `nrOpUpdates` — `//` operator calls (each is O(n) in attrset size)
- `nrListConcats` — list concatenation calls

`NIX_SHOW_STATS_PATH=/tmp/stats.json` writes the output as JSON instead of printing to stderr.

`NIX_COUNT_CALLS=1` additionally prints a table of how many times each built-in and nixpkgs function was called.

#### `--eval-profiler flamegraph` (Nix ≥ 2.30)

```bash
nix-instantiate '<nixpkgs>' -A hello \
  --eval-profiler flamegraph \
  --eval-profile-file nix.profile \
  --eval-profiler-frequency 100   # samples per second

flamegraph.pl nix.profile > flamegraph.svg
```

Profile entries include the call site and function name, e.g.:
```
/nix/store/…/pkgs/top-level/default.nix:167:5:primop import
```

This is the correct tool for identifying hot evaluation paths in large flakes.

#### `nix-eval-jobs`

```bash
# Evaluate all packages in nixpkgs in parallel with 8 workers
nix-eval-jobs --flake 'nixpkgs#legacyPackages.x86_64-linux' \
  --gc-roots-dir ./gcroots \
  --workers 8 \
  --max-memory-size 4096
```

`nix-eval-jobs` evaluates flake attributes **in parallel** by spawning multiple worker processes (one `nix` evaluator per worker). Each worker independently evaluates a subset of the attribute set. Output is **newline-delimited JSON** (JSONLines), one derivation per line, streamed as evaluation completes.

Key flags:
- `--workers N` — number of evaluator processes (default: hardware thread count).
- `--max-memory-size MB` — worker processes restart when they exceed this RSS limit.
- `--gc-roots-dir DIR` — registers GC roots for all evaluated derivations, preventing GC-during-build races.
- `--force-recurse` — recurse into attribute sets (required for nixpkgs).

Use in CI: unlike `nix flake check` (which evaluates everything in one process and fails atomically), `nix-eval-jobs` allows per-attribute failure isolation and parallel job dispatch. It is the engine behind Hydra-style CI.

### 3.6 Large Flake Performance Patterns

#### `inputs.nixpkgs.lib` vs `pkgs.lib`

`inputs.nixpkgs.lib` is available directly as a flake input without importing nixpkgs:

```nix
{
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  outputs = { self, nixpkgs }: {
    # inputs.nixpkgs.lib is available without instantiating any packages
    someOutput = nixpkgs.lib.strings.toUpper "hello";
  };
}
```

`pkgs.lib` requires first importing nixpkgs: `let pkgs = import nixpkgs { inherit system; }; in pkgs.lib`. Importing nixpkgs creates a full package set evaluation context — thousands of thunks, even if only `lib` is accessed. Because `import` calls are not memoized across flake inputs the way `legacyPackages` is, importing nixpkgs multiple times in a flake can cause redundant work.

**Rule:** Use `nixpkgs.lib` (the flake input directly) for any library function in flake `outputs` code that does not need a built package. Reserve `pkgs = import nixpkgs { inherit system; }` for when you actually need packages.

#### `nixpkgs.legacyPackages` vs `import nixpkgs {}`

`legacyPackages.<system>` is defined in the nixpkgs flake as an attribute set (not a function call). Because Nix memoizes attribute accesses but not function calls, multiple flakes that `follows` the same nixpkgs and access `nixpkgs.legacyPackages.x86_64-linux` share a single evaluation of the package set.

If flake B depends on flake A, and both do `import nixpkgs { inherit system; }`, they each trigger a separate evaluation. If both use `nixpkgs.legacyPackages.${system}` with `inputs.nixpkgs.follows = "A/nixpkgs"`, only one evaluation happens.

**Rule:** Published flakes (libraries, NixOS modules) should use `legacyPackages` to avoid multiplying evaluation costs in dependency trees.

#### `flake-parts` `perSystem` lazy evaluation

`flake-parts` introduces `perSystem` as a module-based `eachSystem`. The key performance property: `perSystem` bodies are thunks keyed by system. Accessing `self.packages.x86_64-linux` only evaluates the `perSystem` body for `x86_64-linux`, not for `aarch64-linux`. This differs from `flake-utils.eachDefaultSystem`, which evaluates all systems eagerly when any system output is accessed.

For a flake with expensive per-system evaluations (building lots of derivations), `flake-parts` lazy per-system evaluation can cut evaluation time significantly when only one system is needed (e.g., `nix build` on an x86_64 machine).

#### `builtins.mapAttrs` laziness

`builtins.mapAttrs f attrs` is **lazy per key**: the function `f` is not called for any key until that key's value is accessed. Unlike `builtins.map` on lists (which materialises the entire mapped list structure as thunks immediately), `mapAttrs` on a large attrset does not force any `f` calls at all until attributes are accessed.

However: `builtins.attrValues (builtins.mapAttrs f attrs)` forces **all values**, because `attrValues` accesses every attribute. Similarly, `builtins.toJSON (builtins.mapAttrs f attrs)` forces all values. The laziness of `mapAttrs` only helps when you access a subset of the resulting attributes.

#### The attribute path evaluation trap

The nixpkgs Python package set (`pkgs.python3Packages`) uses a `fix`-based overlay mechanism where the package set is a self-referential attrset. Accessing any single package (e.g., `pkgs.python3Packages.requests`) forces the entire `python3Packages` fixpoint computation, because the override mechanism must evaluate the full scope to resolve `self`/`super` references within the set.

This means:
```nix
# This evaluates the entire python3Packages attrset:
pkgs.python3Packages.tensorflow

# This is fine (no python3Packages eval):
pkgs.python3.withPackages (ps: [ ps.requests ])
```

The trap is not unique to Python — any attrset built with `lib.fix` or `lib.extends` (nixpkgs overlays in general) has this property to varying degrees. The practical advice: avoid accessing package set attributes in flake-level code that runs on every system during `nix flake show` or CI checks.

---

## Summary

| Topic | Key source |
|-------|-----------|
| GC roots filesystem layout | `/nix/var/nix/gcroots/`, `per-user/`, `auto/` |
| Runtime root scanning | `findRuntimeRoots()` in `src/libstore/local-gc.cc` — `/proc/*/maps`, `/proc/*/fd`, `/proc/*/environ`, `/proc/*/exe` |
| Temporary roots protocol | `addTempRoot()`, Unix socket at `gc-socket/socket`, shared GC lock |
| `--add-root --indirect` | `gcroots/auto/[hash] -> external-symlink -> store-path`; dangling = silently dropped |
| `keep-outputs`/`keep-derivations` | Both default-true keeps `.drv`; only `keep-outputs=true` keeps build inputs |
| `nix-store --optimise` | NAR-hash dedup via `.links/` hardlink index; `auto-optimise-store` adds per-build latency |
| Reference scanning | Streaming `RefScanSink` matching 32-char base32 hash substrings |
| `allowedReferences` family | Post-build assertion on output references (direct or transitive) |
| `bundlers` schema | `bundlers.<system>.<name> = drv: derivation;` |
| `nix bundle` CLI | Calls bundler function, builds result, creates `./name` on Linux only |
| `toArx` | `nix-bundle` + `arx`; self-extracting archive; slow startup; no root |
| `toAppImage` | `nix-appimage`; SquashFS + FUSE; fast startup; no root; no glibc dep |
| `toDockerImage` | Thin wrapper over `dockerTools.buildLayeredImage` |
| Thunk lifecycle | `tThunk` → `forceValue()` → in-place overwrite with concrete value |
| `seq` vs `deepSeq` | `seq` = WHNF only; `deepSeq` = full recursive force |
| `lib.foldl'` gotcha | Forces accumulator to WHNF; attrset values inside remain thunks |
| Path interpolation side effect | `"${./dir}"` copies to store at eval time; `toString ./dir` does not |
| `outPath` coercion | Derivation in `"${drv}"` adds to string context; can trigger IFD |
| `NIX_SHOW_STATS=1` | Thunk/value/env counters; `NIX_SHOW_STATS_PATH` for JSON output |
| `--eval-profiler flamegraph` | Sampling profiler; output is flamegraph.pl-compatible |
| `nix-eval-jobs` | Multi-process parallel evaluation; JSONLines streaming; `--gc-roots-dir` |
| `nixpkgs.lib` vs `pkgs.lib` | Use `inputs.nixpkgs.lib` — avoids full nixpkgs import |
| `legacyPackages` vs `import` | `legacyPackages` is memoized across `follows`; `import` is not |
| `mapAttrs` laziness | Per-key lazy; `attrValues` forces all; `toJSON` forces all |
| `python3Packages` fixpoint trap | Any single attribute access forces the entire overlay-built package set |
