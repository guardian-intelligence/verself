# Nix: `.drv` Tooling, `mkShell`/`mkShellNoCC`, and Module System Internals

Covers three interlocking topics for experienced Nix users:
1. The derivation-level CLI (`nix-instantiate`, `nix show-derivation`, `nix-store --query`, `nix store` subcommands) and the `.drv` ATerm wire format.
2. `pkgs.mkShell` vs `pkgs.mkShellNoCC` — what `stdenv` actually injects into a dev shell and when each variant is appropriate.
3. `nixpkgs.lib` module system internals — `lib.evalModules`, the `lib.types` type system, the `mkIf`/`mkMerge`/`mkOverride` priority system, and compat helpers.

---

## Topic 1: Derivation-Level Tooling

### The Two-Phase Build Model: Instantiation vs Realisation

Every Nix build has two distinct phases that can be run independently:

**Phase 1 — Instantiation** (`nix-instantiate`, `nix eval .drvPath`):
Evaluates Nix expressions and writes `.drv` files into the Nix store. No building happens. The `.drv` file is itself a store path (e.g., `/nix/store/abc123-hello-2.12.drv`). Instantiation only requires evaluation, not a sandbox.

**Phase 2 — Realisation** (`nix-store --realise`, `nix build`):
Reads `.drv` files and executes their builders, producing output store paths. Requires the sandbox, build users, and substituters.

`nix build` combines both phases. Breaking them apart is useful for inspecting build plans before committing to a build.

### `nix-instantiate` — the Legacy Instantiator

**Source:** `nix-instantiate(1)` man page, `src/nix-instantiate/nix-instantiate.cc`

```bash
# Default: evaluate file, write .drv, print path
nix-instantiate '<nixpkgs>' -A hello
# Output: /nix/store/xxx-hello-2.12.drv

# --eval: evaluate expression and print result, do NOT write .drv
nix-instantiate --eval -E '"hello" + " world"'
# Output: "hello world"

# --eval --strict: force full evaluation (no lazy thunks left)
nix-instantiate --eval --strict -E '{ a = 1; b = 2; }'
# Without --strict, attrset members remain unevaluated thunks

# --parse: print AST only, useful for understanding parse errors
nix-instantiate --parse -E 'let x = 1; in x + 1'

# Stdin with -E and custom args
nix-instantiate -E 'import <nixpkgs> {}' --attr hello
```

**Gotcha: `--eval` discards derivations.** When `--eval` is given, any derivations created by the evaluated expression are hashed and discarded — they are NOT written to the Nix store. This is by design: `--eval` is a pure evaluation mode. If you need the `.drv` path, omit `--eval`.

**Gotcha: `--strict` can loop forever.** `--strict` forces evaluation of all list elements and all attribute set values recursively. If the expression contains an infinite data structure or a self-referential attrset (e.g., `pkgs` with its thousands of attributes), `--strict` will attempt to evaluate everything. Use only with expressions you know are finite.

**`--json` and `--xml` output modes:**
```bash
# Emit JSON representation of the evaluated value
nix-instantiate --eval --json -E '{ a = 1; b = [ 2 3 ]; }'
# Output: {"a":1,"b":[2,3]}

# Emit XML (same format as builtins.toXML)
nix-instantiate --eval --xml -E '{ a = 1; }'
```

**`-A` / `--attr` flag:** Selects an attribute from the top-level value. Required when the expression evaluates to a package set (like `<nixpkgs>`). Without `-A`, the entire package set would be evaluated, which fails.

### The `.drv` File Format: ATerm

`.drv` files are stored in the Nix store in **ATerm** (Annotated Term) format, a text-based serialization format. The grammar for a traditional derivation:

```
Derive(
  [outputs],        -- list of (name, path, hashAlgo, hash) tuples
  [inputDrvs],      -- list of (drvPath, [outputNames]) pairs
  [inputSrcs],      -- list of plain store paths (non-drv deps)
  "system",         -- build platform string, e.g. "x86_64-linux"
  "builder",        -- absolute path to builder executable
  [args],           -- list of builder arguments
  [env]             -- list of (name, value) string pairs
)
```

**Source:** `src/libstore/derivations.cc`, function `parseDerivation`. The parser `expect(str, 'D')` followed by either `"erive("` (classic) or `"rvWithVersion("` (new dynamic-derivations format) determines which variant to parse.

**Example of an actual `.drv` file** (pretty-printed; real files have no whitespace):
```
Derive(
  [("out","/nix/store/abcd-hello-2.12","","")],
  [("/nix/store/efgh-bash-5.2.drv",["out"]),
   ("/nix/store/ijkl-glibc-2.38.drv",["dev","lib","out"])],
  ["/nix/store/mnop-hello-2.12.tar.gz"],
  "x86_64-linux",
  "/nix/store/qrst-bash-5.2/bin/bash",
  ["-e","/nix/store/uvwx-builder.sh"],
  [("buildInputs",""),("name","hello-2.12"),("out","/nix/store/abcd-hello-2.12"),
   ("src","/nix/store/mnop-hello-2.12.tar.gz"),("system","x86_64-linux")]
)
```

**Key properties of ATerm format:**
- Strings are double-quoted with C-style escaping (`\"`, `\\`)
- Lists use `[a,b,c]` syntax (no spaces)
- Tuples use `("a","b")` syntax
- The format is stable across Nix versions for `Derive()`; `DrvWithVersion()` is experimental

### `nix derivation show` — The Modern Inspector

**Replaces:** `nix show-derivation` (deprecated alias still works)

```bash
# Show derivation JSON for a flake output
nix derivation show .#packages.x86_64-linux.default

# Show the full dependency closure recursively
nix derivation show --recursive .#packages.x86_64-linux.default

# Show by .drv path directly
nix derivation show /nix/store/abc123-hello-2.12.drv
```

**JSON output fields:**
```json
{
  "/nix/store/abc-hello-2.12.drv": {
    "name": "hello-2.12",
    "outputs": {
      "out": {
        "path": "/nix/store/xyz-hello-2.12",
        "method": null,
        "hashAlgo": null,
        "hash": null
      }
    },
    "inputSrcs": ["/nix/store/mnop-hello-2.12.tar.gz"],
    "inputDrvs": {
      "/nix/store/efgh-bash-5.2.drv": { "outputs": ["out"] }
    },
    "system": "x86_64-linux",
    "builder": "/nix/store/qrst-bash-5.2/bin/bash",
    "args": ["-e", "/nix/store/uvwx-builder.sh"],
    "env": {
      "name": "hello-2.12",
      "out": "/nix/store/xyz-hello-2.12",
      "src": "/nix/store/mnop-hello-2.12.tar.gz"
    }
  }
}
```

For **fixed-output derivations**, the `outputs` entry has `method` (`"flat"`, `"nar"`, `"text"`, `"git"`), `hashAlgo` (`"sha256"`, `"sha512"`, `"blake3"`), and `hash` (hex string) populated.

**Surprising fact:** The environment variables (`env`) in a `.drv` are the *actual* strings passed to the builder at build time, after all Nix-level coercions have been applied. Derivation references become their output paths. This means you can read `env` to see exactly what `$src`, `$out`, `$buildInputs` etc. will contain — without building anything.

### `nix-store --query` — Graph Traversal

The legacy `nix-store --query` (aliased `--q`) is the workhorse for store graph analysis. The modern equivalents are in `nix path-info` and `nix store diff-closures`.

**Direction of edges matters:**

| Flag | Direction | Meaning |
|------|-----------|---------|
| `--references` | Outgoing | Direct dependencies of a store path (what it uses) |
| `--referrers` | Incoming | What store paths currently reference this path |
| `--requisites` | Outgoing, transitive | Full closure: all paths needed to use this path |
| `--referrers-closure` | Incoming, transitive | All paths that transitively reference this path |

```bash
# What does hello directly depend on?
nix-store --query --references /nix/store/xyz-hello-2.12
# Output: /nix/store/aaa-glibc-2.38, /nix/store/bbb-bash-5.2

# Full closure (everything needed to run hello)
nix-store --query --requisites /nix/store/xyz-hello-2.12
# Output: every transitive dependency

# What currently-alive store paths depend on glibc?
nix-store --query --referrers /nix/store/aaa-glibc-2.38
# Output: everything in the store that links against glibc

# Which derivation built this path?
nix-store --query --deriver /nix/store/xyz-hello-2.12
# Output: /nix/store/abc-hello-2.12.drv (or "unknown-deriver" if lost)

# What GC roots keep this path alive?
nix-store --query --roots /nix/store/xyz-hello-2.12
# Output: /result -> /nix/store/xyz-hello-2.12 (if a result symlink exists)
```

**Gotcha: `--referrers` is live-store-only.** `--referrers` queries the Nix store's reference graph database, which tracks only *currently-present* paths. If a referrer has been GC'd, it will not appear. This means `--referrers` results can differ between machines even with identical store contents.

**Gotcha: `--deriver` returns `"unknown-deriver"` for paths that were copied (not built) on the current machine.** The deriver information is not stored in the NAR file; it is only recorded when a path is built locally. Binary cache substitution does not record the deriver.

**`--graph` flag for visualization:**
```bash
nix-store --query --graph /nix/store/xyz-hello-2.12 | dot -Tsvg > graph.svg
```
Outputs a Graphviz DOT format of the direct reference graph.

### `nix-store --realise` vs `nix build`

```bash
# Step 1: instantiate (evaluation only — fast)
DRV=$(nix-instantiate '<nixpkgs>' -A hello)
echo $DRV  # /nix/store/abc-hello-2.12.drv

# Step 2: realise (build/fetch — can be slow)
nix-store --realise $DRV
# Output: /nix/store/xyz-hello-2.12

# Equivalent to both steps combined:
nix build nixpkgs#hello
```

**`nix-store --realise` key behaviors:**
- Accepts one or more `.drv` paths or output paths.
- If given an output path (not `.drv`), checks if it's valid; if not, traces back to the deriver and builds it.
- `--dry-run`: prints what would be built/fetched without executing.
- `--check`: builds a second time and compares output hashes (reproducibility check, same as `nix build --check`).
- `--keep-failed`: preserves the failed build directory at `/tmp/nix-build-XXX-YYY/` for inspection.
- Exit codes above 100 indicate build failures; multiple failures with `--keep-going` are ORed together.

### `nix store` Subcommands — The Modern CLI

These are the new-style `nix store` commands (experimental, under `nix-command` feature flag). They overlap with `nix-store` but have a cleaner interface.

**`nix store ls`** — Browse store paths (including remote caches):
```bash
# List files in a store path with long format
nix store ls --long /nix/store/xyz-hello-2.12/bin

# List recursively from a binary cache (no local download needed!)
nix store ls --store https://cache.nixos.org/ \
  --long --recursive /nix/store/xyz-hello-2.12

# JSON output
nix store ls --json /nix/store/xyz-hello-2.12
```

**Surprising use case:** `nix store ls --store https://cache.nixos.org/` lets you inspect the directory tree of a cached path without downloading it. The binary cache protocol can serve NAR file listings without transmitting the full NAR.

**`nix store cat`** — Print a file from any store:
```bash
# Print a file's contents (works with remote stores!)
nix store cat --store https://cache.nixos.org/ \
  /nix/store/xyz-hello-2.12/bin/hello

# Pipe through hexdump to inspect binaries
nix store cat /nix/store/xyz-hello-2.12/bin/hello | hexdump -C
```

`nix store cat` and `nix store ls` work against remote binary caches because the cache protocol supports NAR file listing and partial fetches via the `.narinfo` metadata file which contains the NAR URL and file offsets.

**`nix store diff-closures`** — Show package changes between two store paths:
```bash
# What changed between two system generations?
nix store diff-closures \
  /nix/var/nix/profiles/system-100-link \
  /nix/var/nix/profiles/system-101-link

# Example output:
# acpi-call: 2020-04-07-5.8.16 → 2020-04-07-5.8.18, +0 KiB
# dolphin: 20.08.1 → 20.08.2, +13.9 KiB
# kdeconnect: 20.08.2 → ∅, -6597.8 KiB   (removed)
# htop: ∅ → 3.0.5, +231.7 KiB            (added)
```

**Output format:** Each line is `name: old-version → new-version, ±size`. Packages with empty version strings render as `ε` (epsilon). Size threshold: changes smaller than 8 KiB are omitted. This is directly useful for auditing `nix flake update` changes before deploying.

**`nix store make-content-addressed`** — Rewrite paths to CA form:
```bash
nix store make-content-addressed /nix/store/xyz-hello-2.12
# Output: /nix/store/NEW-HASH-hello-2.12 (different path!)
```

Input-addressed paths (the default) derive their hash from the build graph — they require cryptographic signatures to be trusted when imported from another store. Content-addressed paths derive their hash from the *actual file contents* — they are self-verifying and can be imported without signatures. This is the foundation of the CA derivations feature (see `advanced-topics.md`).

**`nix store sign`** — Sign paths for binary cache trust:
```bash
# Generate a key pair
nix-store --generate-binary-cache-key mycache.example.com \
  /etc/nix/private-key /etc/nix/public-key.pub

# Sign a closure (all paths in the closure need signing)
nix store sign --key-file /etc/nix/private-key \
  --recursive /nix/store/xyz-hello-2.12

# Verify signatures on a path
nix store verify --sigs-needed 1 /nix/store/xyz-hello-2.12
```

A store path is considered trusted by a binary cache client when it has at least one signature from a key in the client's `trusted-public-keys` list. `nix store sign` writes signatures into the store's SQLite database (`/nix/var/nix/db/db.sqlite`), where they are served via the `nix-serve` or `nix serve` protocol.

**`nix store repair`** — Re-fetch corrupted paths:
```bash
nix store repair /nix/store/xyz-hello-2.12
```

Verifies the store path hash, then re-downloads from substituters if the hash doesn't match. Requires available substituters. There is a **TOCTOU race** during repair: the old path is moved out of the way and replaced atomically, but if the process is interrupted mid-swap, the store entry may be absent or half-replaced. This window is documented in the Nix manual.

**`nix store gc`** — Collect garbage:
```bash
nix store gc            # delete unreachable paths
nix store gc --dry-run  # print what would be deleted
nix store gc --max 10G  # stop after freeing 10 GB
```

Contrast with `nix-collect-garbage -d` which also removes old generations of user profiles before GC. `nix store gc` only removes unreachable paths from the current roots.

**`nix store info` / `nix store ping`** — Test store connectivity:
```bash
nix store ping                           # local store
nix store ping --store daemon            # daemon store
nix store ping --store https://cache.nixos.org/  # binary cache
```

Returns store URI and version. `nix store info` is the current name; `nix store ping` is a deprecated alias.

### Why Prefer `nix-store` for Some Tasks

The new `nix store` subcommands are still experimental (behind `nix-command` feature). The legacy `nix-store` is stable and present in all Nix versions ≥ 1.x. For scripts that must run on diverse Nix installations, use `nix-store`. For interactive use where `nix-command` is enabled (any modern flake-using setup), prefer `nix store`.

---

## Topic 2: `pkgs.mkShell` vs `pkgs.mkShellNoCC`

### The Core Distinction

From `pkgs/top-level/all-packages.nix`:
```nix
mkShell = callPackage ../build-support/mkshell { };
mkShellNoCC = mkShell.override { stdenv = stdenvNoCC; };
```

`mkShellNoCC` is literally `mkShell` with `stdenv` replaced by `stdenvNoCC`. Everything else is identical.

**What `stdenvNoCC` is:** A stdenv that includes the standard build infrastructure (bash, coreutils, findutils, diffutils, sed, grep, gawk, tar, gzip, bzip2, xz, file, patchelf, etc.) but **omits the C/C++ compiler toolchain** (gcc/clang, binutils, cc-wrapper). This is useful for languages that bring their own toolchain (Go, Rust, Node.js, Python) and don't want GCC cluttering the environment.

### What stdenv Injects Into a Shell

When `nix develop` activates a `mkShell` derivation, stdenv's `setup.sh` runs and populates:

**`PATH`** — populated with `bin/` directories of all `nativeBuildInputs`. For `mkShell` (using full `stdenv`), this includes:
- `gcc` (or `clang` on Darwin) — wrapped as `cc` and `CC`
- `binutils` — `ld`, `ar`, `nm`, `ranlib`, `strip`, `objdump`
- `make` — GNU Make
- `bash`, `coreutils`, `findutils`, `sed`, `grep`, `gawk`
- `pkg-config` — if declared as a build input
- `patchelf` — for ELF manipulation on Linux

**Compiler environment variables** (set by `cc-wrapper/setup-hook.sh`):
```bash
CC=gcc          # (or the wrapped cc binary path)
CXX=g++
NIX_CC=/nix/store/xxx-gcc-wrapper-13.2.0
NIX_CFLAGS_COMPILE="-isystem /nix/store/xxx-glibc-2.38-dev/include ..."
```

The `role_post` suffix mechanism means cross-compilation environments set `CC_FOR_BUILD`, `CXX_FOR_BUILD`, etc. for the build-platform compiler alongside `CC`, `CXX` for the host-platform compiler.

**Build phase bash functions** — stdenv's `setup.sh` also exports `buildPhase`, `configurePhase`, `installPhase`, etc. as bash functions. Inside `nix develop`, you can run `genericBuild` to execute the full build pipeline manually.

**`PKG_CONFIG_PATH`** — populated by `pkg-config` setup hooks from all `buildInputs`/`propagatedBuildInputs` that contain `.pc` files. Only available when `pkg-config` is a `nativeBuildInput`.

**`CFLAGS`, `LDFLAGS`** — NOT set directly; instead `NIX_CFLAGS_COMPILE` and `NIX_LDFLAGS` are used by the cc-wrapper to inject flags transparently. Real `CFLAGS` are separate and user-controlled.

### `mkShell` Source Code Walkthrough

From `pkgs/build-support/mkshell/default.nix` (annotated):

```nix
lib.extendMkDerivation {
  constructDrv = stdenv.mkDerivation;

  # These are mkShell-specific; don't pass them through to mkDerivation
  excludeDrvArgNames = [ "packages" "inputsFrom" ];

  extendDrvArgs = _finalAttrs:
    { name ? "nix-shell",
      packages ? [],      # convenience alias for nativeBuildInputs
      inputsFrom ? [],    # inherit deps from other derivations
      ...
    }@attrs:
    let
      # mergeInputs collects a specific input category from all inputsFrom
      # entries, but excludes the inputsFrom derivations themselves
      mergeInputs = name:
        (attrs.${name} or [])
        ++ lib.subtractLists inputsFrom
             (lib.flatten (lib.catAttrs name inputsFrom));
    in {
      inherit name;

      # packages goes into nativeBuildInputs (added to PATH)
      nativeBuildInputs = packages ++ (mergeInputs "nativeBuildInputs");

      # buildInputs/propagatedBuildInputs come through unchanged
      buildInputs = mergeInputs "buildInputs";
      propagatedBuildInputs = mergeInputs "propagatedBuildInputs";
      propagatedNativeBuildInputs = mergeInputs "propagatedNativeBuildInputs";

      # shellHook: concatenated from all inputsFrom (reversed) then self
      shellHook = lib.concatStringsSep "\n"
        (lib.catAttrs "shellHook" (lib.reverseList inputsFrom ++ [ attrs ]));

      phases = attrs.phases or [ "buildPhase" ];

      # buildPhase just records the environment — the real output is the
      # env vars captured by `nix develop`, not this file
      buildPhase = attrs.buildPhase or ''
        { echo "------------------------------------------------------------";
          echo " WARNING: the existence of this path is not guaranteed.";
          echo " It is an internal implementation detail for pkgs.mkShell.";
          echo "------------------------------------------------------------";
          echo;
          export;
        } >> "$out"
      '';

      preferLocalBuild = attrs.preferLocalBuild or true;
    };
}
```

**Key insights from the source:**

1. **`packages` is syntactic sugar for `nativeBuildInputs`**. The `mergeInputs` function puts `packages` into `nativeBuildInputs`. The distinction matters because `nativeBuildInputs` are added to `PATH`; `buildInputs` are for C library headers/libs but not PATH.

2. **`inputsFrom` uses `mergeInputs` to extract, not include.** It gathers `buildInputs`, `nativeBuildInputs`, etc. from each derivation in `inputsFrom`, then *subtracts* the `inputsFrom` derivations themselves. This means you get a package's compile-time dependencies without the package itself being a dependency.

3. **`shellHook` concatenation order:** `lib.reverseList inputsFrom ++ [ attrs ]`. This means the *last* item in `inputsFrom` has its `shellHook` run *first*, and the shell's own `shellHook` runs last. This is the reverse of what most people expect.

4. **The output file is an implementation detail.** The `buildPhase` writes environment variable exports to `$out`. This file is used internally by `nix develop` to capture the shell environment; it is not meant to be consumed directly. The warning in the build output is intentional.

5. **`preferLocalBuild = true`** — devShells are preferred to be built locally rather than substituted. This avoids remote builders adding latency to the common `nix develop` invocation.

### `packages` vs `buildInputs` vs `nativeBuildInputs`

For a `mkShell` specifically (not a regular derivation):

| Attribute | Effect in `nix develop` | Use case |
|-----------|------------------------|----------|
| `packages` | Added to PATH (same as `nativeBuildInputs`) | Tools you want in your shell (linters, language servers, etc.) |
| `nativeBuildInputs` | Added to PATH | Same as `packages`; use either |
| `buildInputs` | Headers/libs visible, but NOT in PATH | C libraries, pkg-config `.pc` files |
| `propagatedBuildInputs` | Propagated into dependents | Rarely needed in a dev shell |
| `inputsFrom` | Extracts all of the above from listed derivations | Get all of a package's build dependencies |

**Common mistake:** Putting `pkgs.openssl` in `packages` when you want to compile against OpenSSL. This adds `openssl/bin/openssl` to PATH but does not make the headers (`openssl/include/`) or pkg-config file visible to the C compiler. The correct approach is:

```nix
pkgs.mkShell {
  packages = [ pkgs.pkg-config ];           # tool in PATH
  buildInputs = [ pkgs.openssl.dev ];       # headers + .pc files
}
```

### When to Use `mkShellNoCC`

Use `mkShellNoCC` when:
- Your project is a pure scripting/interpreted language (Python, Node.js, Ruby, Shell) with no C FFI compilation.
- You're packaging a Go, Rust, or Zig program that provides its own toolchain.
- You want a minimal, fast-loading devShell closure (omitting GCC shrinks the closure by ~200-400 MB).
- You want to ensure no C compiler "accidentally" satisfies a dependency (catch misconfigured `buildInputs` early).

Use `mkShell` (the default, with full stdenv) when:
- Your project has native extensions or builds C/C++ code.
- You're calling into C libraries via FFI (Python ctypes, Node N-API, etc.).
- You need `make`, `cmake`, `autoconf` (these can be added explicitly, but stdenv's presence is a signal).
- `inputsFrom` pulls in a derivation that requires a C toolchain.

**`stdenvNoCC` still includes bash** — unlike what the name might imply, `stdenvNoCC` is not a minimal POSIX environment. It includes bash, coreutils, and other build infrastructure. It just lacks `gcc`/`clang`, `binutils`, and the `cc-wrapper`.

### `shellHook` Timing and `nix develop` Environment Capture

The environment capture sequence in `nix develop`:

1. Nix builds the `mkShell` derivation (or uses cached result).
2. `nix develop` reads the captured environment from `inputDerivation` (see `outputs-and-ci.md`).
3. `nix develop` launches a new bash subprocess with that environment.
4. The `shellHook` variable is sourced via `eval "$shellHook"` in the new subprocess.
5. The interactive prompt appears.

**This means `shellHook` runs in the already-configured environment.** You can reference `$CC`, `$PATH`, `nix-store` paths, etc. The `shellHook` is NOT part of the build derivation — it never runs during `nix build`. It only runs during `nix develop` and `nix-shell`.

**`shellHook` variable interpolation pitfall:**
```nix
# WRONG: the variable is interpolated at flake evaluation time
shellHook = "export DB_URL=${config.services.postgresql.listenAddresses}";

# RIGHT: escape for shell, interpolate at shell launch time
shellHook = ''
  export DB_URL="${toString config.services.postgresql.port}"
'';
```

**Nix string interpolation in `shellHook`** uses `${}` at Nix evaluation time. Shell variable references use `$VAR` or `${VAR}` at runtime. Conflicts arise when you mean shell interpolation but write Nix interpolation syntax.

---

## Topic 3: `nixpkgs.lib` Module System Internals

### The Module System in One Paragraph

The NixOS module system is a set of Nix functions (`lib.evalModules`, `lib.mkOption`, `lib.types.*`, etc.) that implement a typed, mergeable configuration language on top of plain Nix. It solves the problem of composing configurations from many independent modules without requiring explicit inter-module coordination. Each module declares which options exist and optionally defines values for those options. The module system validates types, merges values according to type-specific rules, and resolves conflicts using a numeric priority system.

### `lib.evalModules` — The Entry Point

**Source:** `lib/modules.nix`, function `evalModules`

```nix
lib.evalModules {
  modules = [ module1 module2 ./file.nix ];
  specialArgs = { inherit pkgs; };   # extra args passed to all module functions
  prefix = [];                        # path prefix for option names in errors
}
```

**Return value:** An attrset with:
- `config` — the final merged configuration (what you read options from)
- `options` — the option declarations with metadata (type, default, description)
- `_module` — internal module system state (not for consumers)
- `warnings` — list of deprecation warnings triggered by used options

`nixpkgs.lib.nixosSystem` is a thin wrapper around `evalModules` that adds `nixpkgs`-specific `specialArgs` and the NixOS module path.

### Module Structure

A module is a function `{ config, options, lib, pkgs, ... }: { ... }` (or a plain attrset). The three top-level keys are:

```nix
{ lib, config, ... }: {
  # 1. Declare what options exist
  options = {
    services.myapp.enable = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Enable myapp";
    };
    services.myapp.port = lib.mkOption {
      type = lib.types.port;   # 0-65535
      default = 8080;
    };
  };

  # 2. Define values for options (yours or others')
  config = {
    # Only set these options when myapp is enabled
    systemd.services.myapp = lib.mkIf config.services.myapp.enable {
      description = "My Application";
      serviceConfig.ExecStart = "${pkgs.myapp}/bin/myapp";
    };
  };

  # 3. Import other modules
  imports = [ ./myapp-config.nix ];
}
```

**Shortcut:** When a module has no `options` key, the top level is treated as `config`. This allows simple modules:
```nix
{ ... }: {
  networking.hostName = "myhost";
  time.timeZone = "UTC";
}
```

### `imports` Resolution

`imports` is processed by `collectStructuredModules`, which:
1. Recursively expands all `imports` lists depth-first.
2. `disabledModules = [ ./some-module.nix ]` removes a module from the collected set.
3. The final list is flattened into a single ordered list.
4. Import order does NOT determine merge order for most types (merging is type-driven, not order-driven).

**However, import order matters for `shellHook`** (mkShell-specific) and for `mkOverride` tie-breaking at the same priority level. The general rule: don't rely on import order; use explicit priority functions instead.

### `lib.mkOption` Type System

**Source:** `lib/types.nix`

Every option requires a `type`. Types determine:
1. **Validation**: what values are accepted.
2. **Merging**: how multiple definitions of the same option are combined.

**Primitive types** (all use `mergeEqualOption` — definitions must agree):
```nix
lib.types.bool          # true/false; error if two modules set to different values
lib.types.int           # signed integer
lib.types.str           # string; error if two modules define it differently
lib.types.path          # /absolute/path; coerced from strings
lib.types.package       # derivation or store path
lib.types.float         # floating-point
```

**Aggregate types** (merge by combining):
```nix
lib.types.listOf lib.types.str    # lists: concatenated
lib.types.attrsOf lib.types.int   # attr sets: recursively merged
lib.types.lines                   # strings: joined with newlines (unlike str)
lib.types.commas                  # strings: joined with commas
```

**Compound types:**
```nix
lib.types.nullOr lib.types.str       # null | str; merges if both non-null
lib.types.either lib.types.int lib.types.str  # int | str
lib.types.enum [ "a" "b" "c" ]      # one of the listed values
lib.types.oneOf [ t1 t2 t3 ]        # union of multiple types

# Submodule: a nested module with its own options/config
lib.types.submodule {
  options.host = lib.mkOption { type = lib.types.str; };
  options.port = lib.mkOption { type = lib.types.port; default = 80; };
}
```

**`submodule` merging:** When multiple definitions of an `attrsOf (submodule ...)` option exist, each attribute's submodule is independently evaluated with `lib.evalModules`. This is how NixOS options like `services.*` work — each service is its own sub-evaluation.

**Merge error for `str`:** Attempting to define a `str` option in two modules is a common mistake:
```nix
# Module A
config.networking.hostName = "host-a";

# Module B
config.networking.hostName = "host-b";

# Error: The option `networking.hostName' has conflicting definition values:
# - In `/etc/nixos/module-a.nix': "host-a"
# - In `/etc/nixos/module-b.nix': "host-b"
```

Fix with `mkDefault` in the lower-priority definition (or restructure so only one module sets it).

### The Priority System: `mkIf`, `mkMerge`, `mkOverride`

**Source:** `lib/modules.nix`, priority constants

```nix
mkOptionDefault = mkOverride 1500;  # built-in option defaults
mkDefault      = mkOverride 1000;  # "this is a default, override me"
                  # (100 is defaultOverridePriority, undocumented internal)
mkImageMediaOverride = mkOverride 60;   # NixOS installer override
mkForce        = mkOverride 50;    # "I really mean this"
mkVMOverride   = mkOverride 10;    # NixOS test VM override
```

**Lower number = higher precedence.** `mkForce` (50) beats `mkDefault` (1000).

**`mkOverride priority value`** wraps a value with `{ _type = "override"; inherit priority content; }`. The module system's merge function extracts these wrappers and picks the highest-precedence value.

**Practical priority levels:**
```nix
# Option default (set by mkOption default =):
options.foo.default = lib.mkOptionDefault "x"; # priority 1500

# Module-level default (use when writing library modules):
config.foo = lib.mkDefault "y";   # priority 1000

# User config (no wrapper — implicit priority 100):
config.foo = "z";                 # priority 100

# Force (use sparingly — prevents users from overriding):
config.foo = lib.mkForce "w";     # priority 50
```

**`mkIf condition value`** — deferred conditional:
```nix
config = {
  # The entire attrset is conditionally included
  systemd.services.myapp = lib.mkIf config.services.myapp.enable { ... };
};
```

**Critical behavior:** `mkIf` does NOT evaluate `value` at the time of module collection. The condition is checked during the final merge pass, after all modules are collected. This prevents "infinite recursion" when a condition references another option that depends on this one.

**`mkMerge [val1 val2 ...]`** — explicit multi-value merge:
```nix
config = lib.mkMerge [
  { systemd.services.myapp = { ... }; }          # always applies
  (lib.mkIf isLinux { systemd.services.... = {}; }) # conditional
];
```

`mkMerge` is the correct way to produce multiple config contributions from a single module. Without it, you can only have one `config` attrset per module.

**`mkMerge` + `mkIf` interaction:**
```nix
# CORRECT: mkIf inside mkMerge
config = lib.mkMerge [
  (lib.mkIf enableA { optA = "a"; })
  (lib.mkIf enableB { optB = "b"; })
];

# WRONG: nested mkIf doesn't compose well for multiple conditions
config.optA = lib.mkIf enableA "a";  # OK for single option
# But you can't conditionally set multiple options in one mkIf this way
```

### `mkRenamedOptionModule` and `mkRemovedOptionModule`

**Source:** `lib/modules.nix`

These are module-returning functions (they generate a complete NixOS module) used for deprecation handling.

```nix
# In a nixpkgs module:
imports = [
  (lib.mkRenamedOptionModule
    [ "services" "nginx" "enable" ]      # old path
    [ "services" "nginx" "enabled" ]     # new path (must still exist)
  )
];
```

**What `mkRenamedOptionModule` generates:**
```nix
# Roughly equivalent to:
{ options, config, ... }: {
  options.services.nginx.enable = lib.mkOption {
    visible = false;   # hidden from documentation
    # apply: emit warning, forward to new option
  };
  config.services.nginx.enabled = lib.mkAliasDefinitions
    options.services.nginx.enable;
}
```

When a user sets the old option, a deprecation trace is emitted and the value is forwarded to the new option.

**`mkRemovedOptionModule`** is harder — it creates a stub option that throws an error if defined:
```nix
# Source from lib/modules.nix:
mkRemovedOptionModule = optionName: replacementInstructions: { options, ... }: {
  options = setAttrByPath optionName (mkOption {
    visible = false;
    apply = x: throw "The option `${showOption optionName}' can no longer be used...";
  });
  config.assertions = [{
    assertion = !opt.isDefined;
    message = "The option definition `${showOption optionName}' ... no longer has any effect";
  }];
};
```

Both functions take list-of-strings paths (e.g., `[ "services" "nginx" "enable" ]`), not dotted strings.

**`mkAliasOptionModule`** — silent alias (no warning):
```nix
# Creates visible alias with no deprecation warning:
(lib.mkAliasOptionModule
  [ "services" "nginx" "enabled" ]   # from
  [ "services" "nginx" "enable" ]    # to
)
```

### `config` vs `options` Namespace

**The fundamental separation:**
- `options.*` — declares that an option *exists*, with its type, default, and description. Only meaningful at module declaration time.
- `config.*` — assigns a *value* to a declared option. What matters at evaluation time.

When you omit `options`/`config` keys (flat module), the entire attrset is treated as `config`. But you cannot mix option declarations and config definitions at the same level — the module system checks for `options` key presence to decide the interpretation.

**Accessing option metadata at config time:**
```nix
{ options, config, ... }: {
  config = {
    # options.services.foo is the option *declaration*, not the value
    # Check if an option is defined in any module:
    warnings = lib.optional (!options.services.foo.isDefined)
      "services.foo is not configured";
  };
}
```

**`options.<name>.isDefined`** — true if any module has set this option. Useful for conditional behavior.

**`options.<name>.value`** — the merged value (same as `config.<name>`).

**`options.<name>.files`** — list of files that define this option (for error messages).

### Evaluation Order and Infinite Recursion

The module system uses Nix's natural lazy evaluation to handle circular references between `config` values. `lib.mkIf config.services.foo.enable { ... }` is safe because:
1. `mkIf` stores its condition as unevaluated thunk.
2. The merge pass evaluates conditions lazily.
3. If `config.services.foo.enable` itself is defined unconditionally, Nix evaluates it on demand.

**Where infinite recursion occurs:**
```nix
# INFINITE RECURSION: config.foo depends on config.bar which depends on config.foo
{ config, ... }: {
  config.foo = config.bar + 1;
  config.bar = config.foo + 1;  # cycle!
}
```

**Safe pattern using `mkDefault` to break cycles:**
```nix
{ config, lib, ... }: {
  options.services.foo.port = lib.mkOption { type = lib.types.port; default = 80; };
  config.services.foo.port = lib.mkDefault
    (if config.services.https.enable then 443 else 80);
}
```

This works because the condition (`config.services.https.enable`) does not itself depend on `config.services.foo.port`.

### Common Module System Mistakes

**1. Setting `str` option in two modules without priority:**
The fix is to wrap the lower-priority module's value with `lib.mkDefault`:
```nix
# Module A (library): set the default
config.networking.hostName = lib.mkDefault "unnamed-host";
# Module B (user): override it
config.networking.hostName = "myhost";
```

**2. Using `//` instead of `lib.mkMerge` for module configs:**
```nix
# WRONG: // shallow-merges, losing second definition if keys overlap
config = { optA = "a"; } // (lib.mkIf cond { optA = "b"; optB = "c"; });

# RIGHT: let the module system merge
config = lib.mkMerge [
  { optA = "a"; }
  (lib.mkIf cond { optA = lib.mkOverride 50 "b"; optB = "c"; })
];
```

**3. Forgetting that `mkForce` propagates:**
If a module wraps a submodule's option in `mkForce`, that priority propagates through submodule evaluation. Downstream modules may be unable to override an option that should be user-configurable.

**4. Importing the same module twice:**
The module system deduplicates modules by their filesystem path (using the path as a key). Importing the same `.nix` file twice results in a single inclusion. However, inline modules (anonymous attrsets) cannot be deduplicated and will be included twice, leading to merge conflicts.

**5. `assert` in module config:**
`assert` inside a `config` definition runs at evaluation time, not at option-merge time. It cannot reference other options that haven't been merged yet. Use `config.assertions` instead:
```nix
config.assertions = [{
  assertion = config.services.foo.port > 1024;
  message = "Port must be > 1024 for unprivileged service";
}];
```

---

## Cross-Topic: Drv Tooling + Module System Synergy

For `nixosConfigurations`, the `.drv` file of the whole system is accessible:

```bash
# Get the .drv path of the full NixOS system without building
nix eval --raw .#nixosConfigurations.myhost.config.system.build.toplevel.drvPath
# Output: /nix/store/abc123-nixos-system-myhost.drv

# Show its full derivation (huge — use head or jq)
nix derivation show /nix/store/abc123-nixos-system-myhost.drv | jq keys

# Diff two system configurations before building either
nix store diff-closures \
  $(nix eval --raw .#nixosConfigurations.myhost-v1.config.system.build.toplevel.drvPath) \
  $(nix eval --raw .#nixosConfigurations.myhost-v2.config.system.build.toplevel.drvPath)
```

**Note:** `diff-closures` on `.drv` paths diffs the *build plans*, not the *outputs*. This is useful but shows derivation names, not output sizes. For output sizes, build both systems first and diff the realized paths.

For `forge-metal`: before deploying a `make deploy`, you can inspect what NixOS module changes produced:
```bash
# Inspect what the new server-profile derivation includes
nix derivation show .#packages.x86_64-linux.server-profile | jq '.[] | .env | keys'
```
