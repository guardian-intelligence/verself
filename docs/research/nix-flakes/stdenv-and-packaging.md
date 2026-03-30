# Nixpkgs stdenv, Packaging Patterns, and Determinism

Source-grounded deep dive into stdenv build phases, cross-compilation, `nixpkgs.config` in flakes, `pkgs.testers`, `passthru`/`meta` conventions, and `nix build --check` determinism checking.

---

## Topic 1: `stdenv` Build Phases

Source: `ryantm.github.io/nixpkgs/stdenv/stdenv/` (nixpkgs manual mirror), `pkgs/stdenv/generic/setup.sh`, and `doc/stdenv/stdenv.chapter.md`.

### Phase Execution Order

The default phase list is:

```
unpackPhase → patchPhase → configurePhase → buildPhase → checkPhase
→ installPhase → fixupPhase → installCheckPhase → distPhase
```

`distPhase` is disabled by default (only runs when `doDist = true`). `checkPhase` and `installCheckPhase` are also disabled by default.

Each phase is a shell function sourced from `setup.sh`. The generic builder (`genericBuild`) calls them in order via `runPhase`. Overriding a phase completely replaces it; the hooks (`preBuild`, `postBuild`, etc.) are how you add behavior without replacing the whole phase.

### Phase Details

#### `unpackPhase`

Extracts `$src` (or each element of `$srcs`) into the build directory. Supports:
- `.tar.gz`, `.tar.bz2`, `.tar.xz`, `.tar.zst` — unpacked with `tar`
- `.zip` — unpacked with `unzip`
- Directory in the Nix store — copied with `cp -r`

After unpacking, the builder `cd`s into the first directory it finds (or `$sourceRoot` if set, or the result of running `$setSourceRoot`).

Control variables:
- `dontUnpack = true` — skip entirely
- `src` / `srcs` — source archive(s) or store paths
- `sourceRoot` — explicitly name the directory to `cd` into after unpacking
- `setSourceRoot` — shell command that sets `$sourceRoot` dynamically
- `unpackCmd` — override the unpack command for custom archive types
- `preUnpack` / `postUnpack` — hook points

#### `patchPhase`

Applies patches from the `patches` list using the `patch` command. Each element of `patches` must be a Nix store path (string or path literal).

Control variables:
- `dontPatch = true` — skip
- `patches` — list of patch files
- `patchFlags` — arguments passed to `patch` (default: `"-p1"`)
- `prePatch` / `postPatch` — hook points

#### `configurePhase`

Runs `./configure` (or `$configureScript`) with flags assembled from `$configureFlags` and automatically appended `--prefix=$prefix`.

Control variables:
- `dontConfigure = true` — skip
- `configureScript` — path to configure script (default: `./configure`)
- `configureFlags` — list or space-separated string of flags; each element is passed as a separate argument
- `configureFlagsArray` — bash array for flags containing spaces
- `dontAddPrefix = true` — suppress automatic `--prefix` flag
- `prefix` — install prefix (default: `$out`)
- `preConfigure` / `postConfigure` — hook points

**Non-obvious**: `configureFlags` is a string by convention in many packages but the `setup.sh` splits it by whitespace. Use `configureFlagsArray` for any flag that contains spaces (e.g., `--with-foo=some path with spaces`).

#### `buildPhase`

Runs `make` (or `$makefile`) with `$makeFlags` and `$buildFlags`.

Control variables:
- `dontBuild = true` — skip entirely (essential for pure-Nix packages, scripting packages, etc.)
- `makefile` — path to the Makefile (default: none; `make` uses its own lookup)
- `makeFlags` / `makeFlagsArray` — flags passed to every `make` invocation
- `buildFlags` / `buildFlagsArray` — flags passed only during the build phase (not install)
- `preBuild` / `postBuild` — hook points

**Non-obvious**: `makeFlags` applies to ALL make phases (build, check, install). Use `buildFlags` to add flags only during the build step. For purely interpreted packages (Python, shell scripts, Nix expressions), set `dontBuild = true` to skip the phase entirely.

#### `checkPhase`

Runs `make $checkTarget` (default target: `check` or `test`). **Disabled by default** — requires explicit `doCheck = true`.

Control variables:
- `doCheck = true` — enable (off by default)
- `checkTarget` — make target to invoke (default: `check`, falls back to `test`)
- `checkFlags` / `checkFlagsArray` — make flags only for this phase
- `checkInputs` — build-time test dependencies added to PATH for check only
- `nativeCheckInputs` — same as `checkInputs` but explicitly native (important for cross)
- `preCheck` / `postCheck` — hook points

#### `installPhase`

Runs `make $installTargets` (default: `install`) to copy build outputs to `$out`.

Control variables:
- `dontInstall = true` — skip
- `installTargets` — make target(s) to run (default: `install`)
- `installFlags` / `installFlagsArray` — make flags only for this phase
- `preInstall` / `postInstall` — hook points

**Non-obvious**: If you write a custom `installPhase`, you are responsible for calling `runHook preInstall` and `runHook postInstall` at the start/end. Omitting these silently skips any hooks registered by `nativeBuildInputs` setup hooks (e.g., wrapping scripts from `makeWrapper`).

#### `fixupPhase`

Post-installation processing. This is where Nix makes installed artifacts store-path-clean. Key operations run in order:

1. **`patchShebangs $out`**: Rewrites shebang lines in all installed executables. `#!/usr/bin/env python3` becomes `#!/nix/store/<hash>-python3-3.x/bin/python3`. Handles `#!/usr/bin/env` specially — finds the interpreter in `$PATH` and substitutes the full store path. The function `patchShebangsAuto` is registered as a `fixupOutputHooks` hook and runs automatically.

2. **`patchelf`** (Linux only): Updates ELF interpreter (`PT_INTERP`) and `RPATH` of all ELF binaries to point into the Nix store. Ensures dynamically linked binaries find their libraries at `/nix/store/...` rather than `/lib`. Controlled by `dontPatchELF = true` to skip, or `noAuditTmpdir = true` to skip the tmpdir audit.

3. **Strip debug symbols**: Strips debug info from binaries in `$stripDebugList` directories (default: `lib lib32 lib64 libexec bin sbin`). Controlled by `dontStrip = true`. With `separateDebugInfo = true`, strips and saves debug symbols to `$debug/lib/debug/.build-id/XX/YYYY...`.

4. **Move `sbin/` → `bin/`**: Unless `dontMoveSbin = true`, files in `$out/sbin` are moved to `$out/bin` and a compat symlink is created.

5. **Move to `share/`**: Items in `$forceShare` directories (default: `man doc info`) are moved to `$out/share/`.

6. **Multiple-output file distribution**: The `multiple-outputs.sh` hook routes files to their appropriate output (`$dev`, `$man`, `$doc`, `$info`) based on path patterns.

Control variables:
- `dontFixup = true` — skip entire phase
- `dontStrip = true` — skip stripping
- `dontPatchELF = true` — skip ELF patching
- `dontPatchShebangs = true` — skip shebang rewriting
- `dontMoveSbin = true` — keep `sbin/` as-is
- `forceShare` — list of directories to move to `share/` (default: `[ "man" "doc" "info" ]`)
- `stripDebugList` — directories to strip debug symbols from
- `preFixup` / `postFixup` — hook points

#### `installCheckPhase`

Tests the **installed** package (in `$out`) rather than the build directory. **Disabled by default** — requires explicit `doInstallCheck = true`.

Control variables:
- `doInstallCheck = true` — enable (off by default)
- `installCheckTarget` — make target (default: `installcheck`)
- `installCheckFlags` / `installCheckFlagsArray` — make flags
- `installCheckInputs` / `nativeInstallCheckInputs` — dependencies
- `preInstallCheck` / `postInstallCheck` — hook points

#### `distPhase`

Creates a source distribution tarball via `make dist`. **Disabled by default** — requires `doDist = true`.

Control variables:
- `doDist = true` — enable
- `distTarget` — make target (default: `dist`)
- `distFlags` / `distFlagsArray` — make flags
- `tarballs` — list of expected output tarballs (default: `*.tar.gz`)
- `preDist` / `postDist` — hook points

### The Hook System

The pattern `pre<Phase>` / `post<Phase>` are shell variables containing bash code that is executed by `runHook` at phase boundaries. Any string assigned to these variables runs as eval'd bash at that point.

Setup hooks (from `nativeBuildInputs`) register additional phase hooks using `addEnvHooks`, `fixupOutputHooks`, or by directly appending to `preConfigureHooks`, etc.

**Critical rule**: When overriding a phase function completely, you MUST call `runHook preXxx` and `runHook postXxx` yourself:
```bash
buildPhase() {
  runHook preBuild
  # your build commands
  runHook postBuild
}
```

Without `runHook`, setup hooks from `nativeBuildInputs` that register `preBuild` code will not fire.

### `makeWrapper` and `wrapProgram`

Source: `pkgs/build-support/setup-hooks/make-wrapper.sh`.

`makeWrapper` creates a shell wrapper script that sets up environment variables and then `exec`s the original program. `wrapProgram` is the standard convenience function built on top of it.

```bash
wrapProgram $out/bin/foo \
  --prefix PATH : ${lib.makeBinPath [ git nodejs ]} \
  --set MY_CONF $out/share/foo/config \
  --unset HOME \
  --run "export EXTRA_FLAGS='--fast'"
```

`wrapProgram` replaces the binary at `$out/bin/foo` by:
1. Moving the original to `$out/bin/.foo-wrapped`
2. Creating a shell script at `$out/bin/foo` that sets the specified env vars and `exec`s `.foo-wrapped "$@"`

**Why it's needed**: Nix store paths are immutable — you cannot bake runtime paths into a binary by modifying it. The wrapper script is generated in the build phase with the exact store paths and then installed. This is how Nix programs find other Nix-installed tools at runtime without relying on the user's `$PATH`.

**`makeBinaryWrapper`** (newer, preferred): creates a compiled C binary wrapper instead of a shell script. Added to `nativeBuildInputs` as `makeBinaryWrapper`. The wrapped binary can be used as a shebang interpreter (shell scripts cannot). Usage is identical to `makeWrapper`.

Must be in `nativeBuildInputs`:
```nix
nativeBuildInputs = [ makeWrapper ];
# or
nativeBuildInputs = [ makeBinaryWrapper ];
```

### Multiple Outputs: `$out`, `$dev`, `$lib`, `$man`, `$doc`

Source: `pkgs/build-support/setup-hooks/multiple-outputs.sh`, `ryantm.github.io/nixpkgs/stdenv/multiple-output/`.

Nix derivations can produce multiple outputs, each at a separate store path. This allows users to install only what they need (e.g., just `$lib` for runtime, `$dev` only in development).

**Declaring outputs**:
```nix
outputs = [ "out" "dev" "lib" "man" "doc" ];
```

Each name in `outputs` becomes an environment variable in the build containing the corresponding store path.

**Standard output names and what goes there** (set via `$outputXxx` environment variables):

| Variable | Default name | Typical content |
|----------|-------------|-----------------|
| `$outputDev` | `dev` or `out` | Headers (`include/`), pkg-config files (`lib/pkgconfig/`), CMake files, aclocal macros |
| `$outputBin` | `bin` or `out` | User-facing executables in `bin/` |
| `$outputLib` | `lib` or `out` | Shared/static libraries in `lib/`, `libexec/` |
| `$outputDoc` | `doc` or `out` | User documentation in `share/doc/` |
| `$outputDevdoc` | `devdoc` | Developer API docs (gtk-doc, devhelp) |
| `$outputMan` | `man` or `$outputBin` | Man pages sections 0–2 in `share/man/` |
| `$outputDevman` | `devman` or `$outputMan` | Man page section 3 (library functions) |
| `$outputInfo` | `info` or `$outputBin` | Info pages in `share/info/` |

The `fixupPhase` with `multiple-outputs.sh` automatically moves files to the right output based on path patterns. For example, `include/` goes to `$dev`; `share/man/` goes to `$man`.

**Referencing non-default outputs**:
```nix
buildInputs = [ zlib.dev ];          # get headers
nativeBuildInputs = [ pkg-config ];  # finds .pc from zlib.dev
propagatedBuildInputs = [ zlib.lib ]; # runtime: shared library
```

In downstream derivations, `pkgs.zlib` gives the default output (`lib`). `pkgs.zlib.dev` gives headers. `pkgs.zlib.man` gives man pages.

**`moveToOutput`**: manually relocate a file or directory to a named output during the build:
```bash
moveToOutput "lib/libfoo.a" "$dev"   # move static lib to dev output
```

**Binary cache impact**: each output is a separate store path. `nix-env` installs `outputsToInstall` (default: the first output, usually `out`). Non-default outputs must be explicitly requested: `nix profile install nixpkgs#coreutils^man`.

### `separateDebugInfo`

Setting `separateDebugInfo = true` in a `stdenv.mkDerivation` call:
1. Compiles with `-g` (debug symbols enabled)
2. After building, strips the main output
3. Saves the debug symbols as DWARF files at `$debug/lib/debug/.build-id/XX/YYYY...`

The `debug` output is added to `outputs` automatically. GDB and other debuggers use the `.build-id` format to locate external debug files.

In nixpkgs, `gdb` is configured via `debuginfod` to fetch debug info from `cache.nixos.org` (which hosts `separateDebugInfo`-generated debug outputs). This means `gdb` on NixOS can find debuginfo for any nixpkgs package without pre-installing the debug output.

### `installShellFiles`

Source: `ryantm.github.io/nixpkgs/hooks/installShellFiles/`.

A setup hook that simplifies installing man pages and shell completions.

Usage:
```nix
nativeBuildInputs = [ installShellFiles ];
postInstall = ''
  installManPage doc/foobar.1
  installShellCompletion --cmd foobar \
    --bash <($out/bin/foobar --bash-completion) \
    --fish <($out/bin/foobar --fish-completion) \
    --zsh  <($out/bin/foobar --zsh-completion)
'';
```

`installManPage` accepts one or more paths. It reads the section number from the filename extension (`.1`, `.3`, `.8`, etc.) and installs to the correct man section directory. Supports `.gz` compressed pages.

`installShellCompletion` options:
- `--bash` / `--fish` / `--zsh` — explicit shell type
- `--name NAME` — override the completion filename for the following path
- `--cmd NAME` — synthesizes correct filenames for all shells from a single command name
- Accepts process substitutions (`<(cmd)`) as input — useful when the binary generates completions on demand (common pattern: `--bash-completion`, `--fish-completion`, `--zsh-completion` flags)

**Why non-obvious**: the auto-detection by extension only works if the completion file is named correctly (e.g., `foobar.bash`, `_foobar` for zsh). Use `--cmd` when the program generates completions dynamically.

Files are installed into `$out/share/man/`, `$out/share/bash-completion/completions/`, `$out/share/fish/vendor_completions.d/`, and `$out/share/zsh/site-functions/` respectively.

---

## Topic 2: Cross-Compilation with Nix/Flakes

Sources: `nix.dev/tutorials/cross-compilation.html`, `ayats.org/blog/nix-cross`, `nixcademy.com/posts/cross-compilation-with-nix/`, nixpkgs manual.

### The Three-Platform Model

Nix inherits the GNU `autoconf` three-platform model:

| Name | Variable | Meaning |
|------|----------|---------|
| **build** | `stdenv.buildPlatform` | Machine running the compiler |
| **host** | `stdenv.hostPlatform` | Machine that will execute the output |
| **target** | `stdenv.targetPlatform` | Machine the output will produce code for |

The `target` platform is only relevant for compilers and linkers (tools that produce code for another platform). For most packages, `host == target`.

**Native compilation**: `build == host == target` — the usual case.
**Cross compilation**: `build != host` — compiling on x86_64-linux to produce aarch64-linux binaries.

`stdenv.hostPlatform.config` returns a GNU triple like `"aarch64-unknown-linux-gnu"`.

### `nativeBuildInputs` vs `buildInputs` in Cross Context

This distinction only matters for cross compilation:

| Attribute | Platform | Example |
|-----------|----------|---------|
| `nativeBuildInputs` | Build (compile-time tools) | `cmake`, `pkg-config`, `meson`, `perl` (build scripts) |
| `buildInputs` | Host (linked libraries) | `zlib`, `openssl`, `boost` |

In native builds, both resolve to the same platform and the distinction is irrelevant (both go to `$PATH`). In cross builds:
- `nativeBuildInputs` gets the **x86_64-linux** version of `cmake` (what the builder machine runs)
- `buildInputs` gets the **aarch64-linux** version of `zlib` (what the final binary links against)

Using `cmake` in `buildInputs` during cross compilation would try to link against an aarch64 `cmake` — which is nonsensical.

**`targetBuildInputs`** (rarely used): dependencies that the output tool itself produces code for. Relevant when packaging a cross-compiler.

### `buildPackages` and `hostPackages`

These are aliases in `pkgsCross` instances:

- `pkgsCross.aarch64-multiplatform.buildPackages` — the **build-platform** package set (x86_64-linux tools)
- `pkgsCross.aarch64-multiplatform.hostPackages` — same as `pkgsCross.aarch64-multiplatform` (the host/target packages)
- `pkgsCross.aarch64-multiplatform.targetPackages` — same as `hostPackages` for most packages (not compilers)

In a cross-compiling `stdenv.mkDerivation`:
```nix
nativeBuildInputs = [ pkgs.buildPackages.cmake ];  # explicitly build-platform cmake
buildInputs = [ pkgs.zlib ];                        # auto-resolved to host-platform zlib
```

When you use `callPackage` with `pkgsCross`, the resolution happens automatically — `callPackage` passes the cross-aware `pkgs` which routes `nativeBuildInputs` to build packages and `buildInputs` to host packages.

### `pkgs.pkgsCross`

`pkgsCross` is an attribute set in nixpkgs where each key is a predefined cross-compilation target. Source: `lib/systems/examples.nix`.

**Selected `pkgsCross` names**:

| Attribute | Target triple | Use case |
|-----------|--------------|----------|
| `aarch64-multiplatform` | `aarch64-unknown-linux-gnu` | ARM64 Linux (Raspberry Pi 4, AWS Graviton) |
| `aarch64-multiplatform-musl` | `aarch64-unknown-linux-musl` | Static ARM64 with musl libc |
| `aarch64-android` | `aarch64-unknown-linux-android` | Android ARM64 |
| `armv7l-hf-multiplatform` | `armv7l-unknown-linux-gnueabihf` | ARMv7 with hardware float |
| `riscv64` | `riscv64-unknown-linux-gnu` | RISC-V 64-bit |
| `mingwW64` | `x86_64-w64-mingw32` | Windows 64-bit (MinGW) |
| `raspberryPi` | `armv6l-unknown-linux-gnueabihf` | Raspberry Pi Zero/1 |
| `musl64` | `x86_64-unknown-linux-musl` | x86_64 with musl libc |
| `wasi32` | `wasm32-unknown-wasi` | WebAssembly |

Full list at `pkgs/lib/systems/examples.nix` in the nixpkgs source.

### Cross-Compilation in a Flake

**Pattern 1: `pkgsCross` inside existing system packages**
```nix
packages = flake-utils.lib.eachDefaultSystem (system: let
  pkgs = import nixpkgs { inherit system; };
in {
  # Build for current system
  default = pkgs.callPackage ./package.nix {};
  # Cross-compile aarch64 from wherever we're running
  aarch64 = pkgs.pkgsCross.aarch64-multiplatform.callPackage ./package.nix {};
});
```

**Pattern 2: `packages.aarch64-linux.default` built on x86_64-linux**

This is a distinction between the *output attribute key* (what the artifact targets) and the *build platform* (the machine running the build). The flake schema uses system as the target/host platform for the output, not the build platform:

```nix
# If you ARE on aarch64-linux, this is a native build
# If you're on x86_64-linux, this requires either:
#   a) A remote aarch64-linux builder
#   b) Cross-compilation setup (crossSystem)
packages.aarch64-linux.default = let
  pkgs = import nixpkgs { system = "aarch64-linux"; };
in pkgs.callPackage ./package.nix {};
```

For true cross-compilation (build x86_64, produce aarch64):
```nix
packages.aarch64-linux.default = let
  pkgs = import nixpkgs {
    localSystem = "x86_64-linux";   # build platform
    crossSystem = "aarch64-linux";  # host/target platform
  };
in pkgs.callPackage ./package.nix {};
```

`pkgsCross.aarch64-multiplatform` is essentially `import nixpkgs { localSystem = currentSystem; crossSystem = { config = "aarch64-unknown-linux-gnu"; }; }`.

### Binary Cache Misses with Cross Compilation

Cross-compiled packages have different store paths than native-compiled packages because the derivation's inputs include the cross-compilation toolchain. Even if the final binary is identical to a native arm64 build, its store path differs.

`cache.nixos.org` only caches packages built natively for their target platform. Cross-compiled packages from x86_64 → aarch64 will **miss the cache** even if the same package has an aarch64-native cached build. The store path diverges because the derivation includes the cross-toolchain in its dependency hash.

**Practical consequence**: for CI producing aarch64 images on x86_64 hosts, expect longer build times due to cache misses. Using an actual aarch64 builder (via `--builders`) or emulation (QEMU binfmt_misc) with a native-aarch64 nixpkgs instance will get cache hits.

---

## Topic 3: `nixpkgs.config` in Flakes

Source: `ryantm.github.io/nixpkgs/using/configuration/`, nixpkgs manual Chapter on package configuration.

### The Core Problem: `legacyPackages` Ignores Local Config

`nixpkgs.legacyPackages.${system}` is a pre-instantiated nixpkgs from the flake input. It was evaluated WITHOUT your `~/.config/nixpkgs/config.nix` or any per-call config. There is no mechanism to inject config into an already-evaluated nixpkgs instance.

```nix
# WRONG: uses the pre-evaluated legacyPackages — ignores allowUnfree
pkgs = nixpkgs.legacyPackages.${system};

# CORRECT: fresh nixpkgs instantiation with explicit config
pkgs = import nixpkgs { inherit system; config.allowUnfree = true; };
```

This is the single most common source of "unfree package won't build in flake" confusion.

### Setting `allowUnfree`

Three mechanisms, in order of scope:

**1. In `flake.nix` (per-flake)**:
```nix
pkgs = import nixpkgs {
  inherit system;
  config.allowUnfree = true;
};
```

**2. Environment variable (per-invocation, requires `--impure`)**:
```bash
NIXPKGS_ALLOW_UNFREE=1 nix build --impure .#vscode
```
The `--impure` flag is required because flake evaluation is pure by default, blocking `builtins.getEnv`.

**3. User config file `~/.config/nixpkgs/config.nix`** — only works with `nix-env`, `nix-build`, and `nix-shell`; NOT with flake commands unless `pkgs = import nixpkgs {}` is used (which reads the config from the filesystem at eval time).

### `allowUnfreePredicate` — Selective Unfree

Instead of allowing all unfree packages, you can approve specific packages by name:
```nix
pkgs = import nixpkgs {
  inherit system;
  config.allowUnfreePredicate = pkg:
    builtins.elem (lib.getName pkg) [
      "vscode"
      "slack"
      "cuda"
    ];
};
```

`lib.getName pkg` returns the package's pname (the name without version). The predicate is called with the derivation attrset; you can inspect any attribute including `meta.license`.

### `allowBroken`

`meta.broken = true` marks a package as known-broken. Unlike unfree packages, the check-meta mechanism for broken packages **throws an error at evaluation time**.

- `config.allowBroken = true` bypasses the evaluation error.
- `NIXPKGS_ALLOW_BROKEN=1` also bypasses it (requires `--impure` in flake context).
- `allowBrokenPredicate` (function form) for selective allowance.

**Note from nixpkgs check-meta.nix**: The `allowBroken` config option IS implemented (contrary to some outdated forum posts). The check at `pkgs/stdenv/generic/check-meta.nix` reads `config.allowBroken or false` before throwing.

### `permittedInsecurePackages`

For packages with known CVEs or security issues:
```nix
config.permittedInsecurePackages = [
  "openssl-1.1.1w"
  "python-2.7.18.8"
];
```

The exact name-version string must match what nixpkgs reports. Find it from the error message when attempting to build: "Package 'openssl-1.1.1w' is marked as insecure".

### `packageOverrides` vs Overlays

`packageOverrides` is the older mechanism in `nixpkgs.config`:
```nix
config.packageOverrides = pkgs: {
  vim = pkgs.vim.override { python3 = pkgs.python311; };
};
```

Key differences from overlays:

| | `packageOverrides` | Overlays |
|--|-------------------|---------|
| Composability | Single function, last one wins | Composed list, all apply in order |
| Access to `final` | No — only `prev` (called as `pkgs`) | Yes — `final: prev:` both available |
| Distribution | Cannot be exported from a flake | Can be exported as `overlays.default` |
| `self`-reference | No | Yes — use `final.myPkg` to ref overridden |
| Use case | Simple local overrides | Published, composable package modifications |

`packageOverrides` is evaluated once and merged. Overlays form a fixpoint (via `lib.fix`/`lib.extends`) that allows self-referential overrides. Use overlays for anything beyond trivial per-user customizations.

### `allowUnsupportedSystem`

Setting `config.allowUnsupportedSystem = true` (or `NIXPKGS_ALLOW_UNSUPPORTED_SYSTEM=1`) bypasses the platform availability check. Normally, evaluating a package on a platform not in `meta.platforms` throws an evaluation error. This config suppresses it.

---

## Topic 4: `pkgs.testers` — The Modern Test Framework

Sources: `ryantm.github.io/nixpkgs/builders/testers/` (nixpkgs manual), nixpkgs source `pkgs/build-support/testers/`.

### Overview

`pkgs.testers` is a collection of test-support functions added to nixpkgs to standardize how packages expose tests. Key functions:

### `testers.testVersion`

Verifies a package's main binary can run and reports the expected version.

```nix
passthru.tests.version = testers.testVersion {
  package = finalAttrs.finalPackage;
  # Optional overrides:
  command = "weed version";        # default: "${package.meta.mainProgram or package.pname} --version"
  version = "v${version}";        # default: package.version
  # versionOutput = "stderr";     # check stderr instead of stdout
};
```

The test runs `$command`, captures stdout/stderr, and checks that the `version` string appears in the output. Even this minimal test catches: wrong binary linked, dynamic linking failures at startup, and accidental version-string mismatch from stale `src`.

### `testers.testBuildFailure`

Wraps a derivation that is *expected to fail* and captures its exit code and log:

```nix
failed = testers.testBuildFailure (stdenv.mkDerivation {
  name = "should-fail";
  builder = builtins.toFile "b" ''
    echo "dying" >&2
    exit 7
  '';
});

# In another derivation:
runCommand "check-failure" {} ''
  grep "dying" ${failed}/testBuildFailure.log
  [[ 7 = $(cat ${failed}/testBuildFailure.exit) ]]
  touch $out
''
```

Output structure under `$out`:
- `result/` — the partial build output (whatever the builder created before failing)
- `testBuildFailure.log` — captured build log (stderr + stdout)
- `testBuildFailure.exit` — exit code as a text file

**Non-obvious**: `testBuildFailure` itself always *succeeds* as a Nix derivation — it captures failure rather than propagating it. This means you can use it in `checks` and it won't make `nix flake check` fail; the wrapping derivation that validates the failure is what actually runs and may fail.

### `testers.hasPkgConfigModules`

Verifies a package exposes the expected `pkg-config` (`.pc`) modules:

```nix
passthru.tests.pkg-config = testers.hasPkgConfigModules {
  package = finalAttrs.finalPackage;
  moduleNames = [ "libfoo" "libfoo-2.0" ];  # if omitted, uses meta.pkgConfigModules
};
```

Internally runs `pkg-config --modversion <module>` for each listed module. Fails if any module is not found or doesn't load. Useful for catching packaging bugs where `.pc` files end up in the wrong output (`dev` vs `out`).

### `testers.invalidateFetcherByDrvHash`

Forces a fetcher to re-fetch every time the fetcher derivation changes, bypassing the normal FOD caching:

```nix
passthru.tests.fetchgit = testers.invalidateFetcherByDrvHash fetchgit {
  name = "nix-source";
  url = "https://github.com/NixOS/nix";
  rev = "9d9dbe6ed05854e03811c361a3380e09183f4f4a";
  hash = "sha256-7DszvbCNTjpzGRmpIVAWXk20P0/XTrWZ79KSOGLrUWY=";
};
```

**Why it exists**: FODs are normally cached by their output hash. If you change only the fetcher's arguments (e.g., a bug in `fetchgit`'s argument handling), Nix reuses the cached output because the output hash matches. `invalidateFetcherByDrvHash` appends the derivation's input hash to the fetcher's `name`, making the output path unique per derivation. The fetcher runs fresh every time the derivation changes.

This is exclusively useful when testing fetchers themselves — not for normal package fetching.

### `testers.runNixOSTest` / `testers.nixosTest`

Run a full NixOS VM test as a Nix derivation. Uses QEMU.

```nix
# testers.runNixOSTest — preferred, modern API
pkgs.testers.runNixOSTest ({ lib, ... }: {
  name = "my-service-test";
  nodes.machine = { pkgs, ... }: {
    services.myService.enable = true;
    environment.systemPackages = [ pkgs.curl ];
  };
  testScript = ''
    machine.start()
    machine.wait_for_unit("myService.service")
    machine.succeed("curl -f http://localhost:8080/health")
  '';
})
```

`testScript` is a Python script — the test driver provides a Python API for interacting with VMs (`machine.start()`, `machine.succeed()`, `machine.wait_for_unit()`, etc.).

**As a flake `checks` output**:
```nix
checks.x86_64-linux.integration = pkgs.testers.runNixOSTest {
  name = "integration";
  nodes.server = { ... }: { ... };
  testScript = "server.succeed(\"my-check\")";
};
```

`nix flake check` builds this derivation. If the test script exits non-zero, the build fails.

**`testers.nixosTest` vs `testers.runNixOSTest`**: `runNixOSTest` is the newer function that properly receives `pkgs` from the calling context, enabling binary cache hits for VM images. `nixosTest` is the older alias that wraps `nixpkgs.lib.nixosTest` and may use a different nixpkgs instance. Prefer `runNixOSTest` for flake-based tests.

### `testers.testEqualDerivation`

Verifies two derivations produce identical build instructions (same `.drv` file):

```nix
testers.testEqualDerivation
  "hello with doCheck should be the same derivation"
  pkgs.hello
  (pkgs.hello.overrideAttrs (o: { doCheck = true; }))
```

Fails if the derivations differ. Useful for ensuring a change to package configuration doesn't accidentally invalidate the binary cache (if the `.drv` is different, the store path changes).

### `testers.shellcheck`

Runs `shellcheck` on shell scripts in a package:
```nix
passthru.tests.shellcheck = testers.shellcheck { package = finalAttrs.finalPackage; };
```

Checks all shell scripts in the package's output directories.

### Using Testers in Flake `checks`

```nix
checks.x86_64-linux = {
  version = pkgs.testers.testVersion { package = myPkg; };
  pkg-config = pkgs.testers.hasPkgConfigModules { package = myPkg; moduleNames = [ "libmypkg" ]; };
  integration = pkgs.testers.runNixOSTest { name = "smoke"; nodes.m = {...}: {}; testScript = "m.succeed(\"true\")"; };
};
```

`nix flake check` builds all `checks` derivations. Non-derivation values in `checks` cause an evaluation error.

---

## Topic 5: `passthru` and `meta` Attributes

Sources: `ryantm.github.io/nixpkgs/stdenv/meta/`, `github.com/NixOS/nixpkgs/blob/master/pkgs/stdenv/generic/check-meta.nix`, `wiki.nixos.org/wiki/Nixpkgs/Update_Scripts`.

### `passthru`

`passthru` is an attribute set on a derivation that is NOT part of the build environment (not exported as env vars) but is accessible from Nix expressions via `pkg.passthru.X` or simply `pkg.X` (the latter works because derivations are attrsets and `passthru` attrs are merged into the top-level).

```nix
stdenv.mkDerivation (finalAttrs: {
  pname = "myapp";
  version = "1.0";
  passthru = {
    tests.version = testers.testVersion { package = finalAttrs.finalPackage; };
    tests.pkg-config = testers.hasPkgConfigModules { package = finalAttrs.finalPackage; moduleNames = [ "myapp" ]; };
    updateScript = nix-update-script { };
  };
})
```

The `finalAttrs` pattern (using `mkDerivation` with a function argument) gives you `finalAttrs.finalPackage` — a reference to the fully-evaluated derivation, needed for tests that take the package as argument.

### `passthru.tests` in nixpkgs CI

nixpkgs CI (Hydra) runs `pkg.passthru.tests` as part of the build job for a package. When a package is updated, Hydra builds not just the package itself but also all derivations in `passthru.tests`. This is how version bump PRs get automatic test coverage.

The nixpkgs `pkgs/README.md` convention: `passthru.tests` should contain tests that verify correct runtime behavior of the *installed* package. Contrast with `checkPhase`/`installCheckPhase` which test during build.

You can run tests locally:
```bash
nix build nixpkgs#myapp.tests.version
nix build nixpkgs#myapp.tests.pkg-config
```

### `passthru.updateScript` and `nix-update`

`passthru.updateScript` is a derivation or executable path that, when run, updates the package's version and hash in nixpkgs. The r-ryantm bot runs these scripts automatically.

Common forms:
```nix
# Use the nix-update tool (most common):
passthru.updateScript = nix-update-script { };

# With version tracking on a branch:
passthru.updateScript = nix-update-script { extraArgs = [ "--version=branch" ]; };

# Use gitUpdater for GitHub releases:
passthru.updateScript = gitUpdater { rev = "v${version}"; };

# Custom script:
passthru.updateScript = writeShellScript "update-myapp" ''
  set -eu
  version=$(curl -s https://api.github.com/repos/foo/myapp/releases/latest | jq -r .tag_name)
  update-source-version myapp "$version"
'';
```

Manual invocation:
```bash
nix-shell maintainers/scripts/update.nix --argstr package myapp
# Or with nix-update directly:
nix-update myapp
```

The `update.nix` script in nixpkgs uses `pkg.passthru.updateScript` to find and run the update command.

### `meta.mainProgram`

Specifies the primary executable in `$out/bin/` for a package. Used by `nix run`:

```nix
meta.mainProgram = "rg";  # for ripgrep; without this, nix run would try to run "ripgrep"
```

`nix run nixpkgs#ripgrep` resolution order:
1. `apps.<system>.ripgrep` — explicit app object
2. `packages.<system>.ripgrep` — looks for `${pkg.meta.mainProgram or pkg.pname}` in `$out/bin/`
3. `legacyPackages.<system>.ripgrep` — same logic

Without `meta.mainProgram`, `nix run nixpkgs#ripgrep` would try to execute `$out/bin/ripgrep`, which works because the binary happens to match the pname. But for packages where the binary name differs from the pname (e.g., pname `"ripgrep"`, binary `"rg"`), `meta.mainProgram = "rg"` is required.

**nixpkgs issue #219567**: Missing `meta.mainProgram` is a known problem for many packages. The nixpkgs contribution guidelines now require `mainProgram` for all packages that install executables.

### `meta.platforms`

A list of Nix system strings (or `lib.platforms.*` sets) where the package is supported:

```nix
meta.platforms = lib.platforms.linux;        # all Linux platforms
meta.platforms = lib.platforms.unix;         # Linux + Darwin + BSD
meta.platforms = lib.platforms.all;          # every platform nixpkgs knows
meta.platforms = [ "x86_64-linux" "aarch64-linux" ];  # explicit list
```

`lib.platforms.*` values: `linux`, `darwin`, `unix`, `windows`, `freebsd`, `openbsd`, `netbsd`, `illumos`, `cygwin`, `all`.

When a package is evaluated on a platform NOT in `meta.platforms`, nixpkgs throws at evaluation time (unless `config.allowUnsupportedSystem = true`). This prevents accidentally building packages that will fail at compile time on unsupported platforms.

`meta.badPlatforms` is the complement — platforms explicitly known to NOT work, even if listed in `meta.platforms`.

### `meta.broken`

`meta.broken = true` marks a package as currently non-functional. The check in `check-meta.nix` throws at **evaluation time**:

```
error: Package 'foo-1.0' in .../foo/default.nix:5 is marked as broken, refusing to evaluate.
```

This fires before any build attempt. Bypasses:
- `config.allowBroken = true` in nixpkgs config
- `NIXPKGS_ALLOW_BROKEN=1` (requires `--impure` in flake context)

`meta.broken` can be a condition:
```nix
meta.broken = stdenv.hostPlatform.isDarwin;  # broken only on macOS
```

**`nix flake check` handling**: when `nix flake check` evaluates `packages.<system>.<name>` and the package has `meta.broken = true`, the check fails with an evaluation error. This is intentional — broken packages should not be in `packages` outputs; they should be removed or gated behind overlays.

### `meta.license`

License information. Format:

```nix
# Single license (preferred):
meta.license = lib.licenses.mit;

# Multiple licenses (AND: must satisfy all):
meta.license = with lib.licenses; [ gpl2Plus lgpl21Plus ];

# The lib.licenses attrset contains:
# mit, gpl2Only, gpl2Plus, gpl3Only, gpl3Plus, lgpl21Only, lgpl21Plus,
# asl20 (Apache 2.0), bsd2, bsd3, mpl20, isc, cc0,
# unfree, unfreeRedistributable, unfreeRedistributableFirmware, ...
```

Each `lib.licenses.X` value is an attrset:
```nix
lib.licenses.mit = {
  spdxId = "MIT";
  fullName = "MIT License";
  url = "https://spdx.org/licenses/MIT.html";
  free = true;
  redistributable = true;
};
```

The `unfree` check in `check-meta.nix` evaluates `!(meta.license.free or true)`. So `meta.license = lib.licenses.unfree` sets `free = false`, triggering the unfree check.

### `meta.unsupported` vs `meta.broken`

These are often confused:

- `meta.broken = true` — package is known-broken for ALL uses on ALL platforms. The package cannot be built successfully. Bypass with `allowBroken`.
- `meta.unsupported` is not a first-class meta attribute. Unsupported platforms are expressed via `meta.badPlatforms`. A package on a `badPlatform` throws with "is not available on the requested hostPlatform" (different from the broken message). Bypass with `allowUnsupportedSystem`.

The `check-meta.nix` code sets `unsupported = hasUnsupportedPlatform attrs` — this is a computed property exposed in the check result, not something you set in `meta`.

### `meta.priority` and `buildEnv` Collision Resolution

`meta.priority` (also settable as `meta.defaultPriority`) is an integer (default: `5`). Lower number = higher priority.

In `buildEnv` (and `nix profile`, which uses `buildEnv` internally):
- When two packages provide the same file path, the package with **lower priority number** wins.
- Equal priority → collision error (or warning with `ignoreCollisions`).

Setting a higher priority (lower number):
```nix
meta.priority = 4;  # wins over default priority 5
```

The `lowPrio` / `hiPrio` nixpkgs functions:
```nix
pkgs.lowPrio pkgs.foo   # sets meta.priority = 10 (loses to everything at default)
pkgs.hiPrio pkgs.foo    # sets meta.priority = 1 (wins over everything at default)
```

**Practical use**: `lib.meta.lowPrio` is applied to packages that provide common files (e.g., `man` pages) that other packages also provide. `hiPrio` is used when you want to pin a specific version of a package in a `buildEnv` or `nix profile`.

### `allowUnfreePredicate` Config Option

```nix
config.allowUnfreePredicate = pkg:
  builtins.elem (lib.getName pkg) [ "vscode" "nvidia-x11" ] ||
  (lib.getName pkg == "slack" && pkg.version == "4.35.131");
```

Called with the package derivation attrset. Return `true` to permit. More flexible than `allowUnfree = true` — you can approve specific packages while rejecting all others.

---

## Topic 6: `nix build --check` and Determinism

Sources: `nix.dev/manual/nix/2.25/advanced-topics/diff-hook`, `reproducible.nixos.org`, `github.com/grahamc/r13y.com`, nixpkgs issue tracker.

### Terminology: Deterministic vs Reproducible

In Nix documentation and practice, these terms have distinct meanings:

- **Deterministic**: Given identical inputs (source, build env, dependencies), the build produces identical output. Nix's sandbox *attempts* to ensure this by fixing the hostname, user IDs, timestamps (`SOURCE_DATE_EPOCH`), locale, etc. A build is deterministic if running it twice on the same machine produces the same bits.

- **Reproducible**: The build produces identical output when run on *different* machines, at different times, with different hardware. Reproducible implies deterministic plus: no embedded build timestamps, no path-to-build-dir leakage, no machine-specific hardware constants, etc.

Nix builds are sandboxed to be highly reproducible, but not all packages achieve bit-perfect reproducibility. Common culprits: embedded timestamps (despite `SOURCE_DATE_EPOCH`), sort-order dependencies on hash maps (Python, Rust sometimes), parallel build races, and locale-dependent sort orders.

### `nix build --rebuild`

The `--rebuild` flag forces a build even when the output is already in the Nix store:

```bash
nix build --rebuild nixpkgs#hello
```

Does NOT compare to the existing store path. Simply rebuilds from scratch (discards existing store entry for the purposes of this build). The rebuilt output REPLACES the existing entry if it differs. Used to force a fresh build when you suspect store corruption or want to update a cached result.

### `nix build --check`

The `--check` flag rebuilds a derivation that's **already in the store** and compares the two outputs:

```bash
nix build --check nixpkgs#hello
```

**Behavior**:
1. The derivation must already be built (else: "path … is not valid")
2. Builds the derivation again in a sandbox
3. Compares the new output to the existing store entry bit-for-bit
4. If outputs match: exit 0 (deterministic)
5. If outputs differ: exit 1 (non-deterministic)

**With `--keep-failed`**:
```bash
nix build --check --keep-failed nixpkgs#hello
```
When the outputs differ, the second build's output is preserved at `<store-path>.check` (e.g., `/nix/store/abc123-hello-2.12.1.check`). This path is NOT a GC root — it may be deleted by `nix-store --gc`.

**Using diffoscope to diagnose**:
```bash
nix build --check --keep-failed nixpkgs#hello
nix run nixpkgs#diffoscope -- \
  /nix/store/abc123-hello-2.12.1 \
  /nix/store/abc123-hello-2.12.1.check
```

`diffoscope` recursively decompiles archives, ELF binaries, and other formats to pinpoint exactly which bytes differ and why. Output example: "ELF section `.debug_str` differs: timestamp embedded at offset 0x4f2."

### The `diff-hook` Mechanism

Configure automatic diffing in `nix.conf`:

```ini
diff-hook = /etc/nix/diff-hook.sh
run-diff-hook = true
```

`diff-hook` is invoked when `--check` finds a discrepancy. Arguments:
- `$1` — path to the existing store output (first build)
- `$2` — path to the new build output (second build)
- `$3` — path to the `.drv` derivation file

Example hook using diffoscope:
```bash
#!/bin/sh
exec >&2
echo "Non-determinism found in derivation: $3"
/run/current-system/sw/bin/diffoscope --no-progress "$1" "$2" || true
```

The hook runs with the same user/group as the build but has no write access to the Nix store. It must complete before Nix continues.

**`build-repeat` setting**: sets the number of times to rebuild for reproducibility checking:
```ini
build-repeat = 1   # build once extra, compare
build-repeat = 2   # build twice extra, compare all three
```

With `run-diff-hook = true` and `build-repeat >= 1`, every build is automatically checked for reproducibility. This is expensive but used on the r13y.com infrastructure.

### `--keep-going` vs `--keep-failed`

These are different flags with different scopes:

| Flag | Scope | Effect |
|------|-------|--------|
| `--keep-going` | Multi-derivation builds | Continue building other derivations when one fails; report all failures at end |
| `--keep-failed` | Single derivation | Keep the failed build's output directory at `/tmp/nix-build-...` for inspection |

`--keep-going` is useful for `nix flake check` to see ALL check failures rather than stopping at the first:
```bash
nix flake check --keep-going
```

`--keep-failed` is useful for debugging a single failing derivation:
```bash
nix build --keep-failed .#myPackage
# Failed build dir is at /tmp/nix-build-myPackage-*
```

### `--no-build-output` vs `--print-build-logs`

| Flag | Effect |
|------|--------|
| `--no-build-output` (`-Q`) | Suppress build log output to terminal entirely |
| `--print-build-logs` (`-L`) | Always print build logs to terminal (not just on failure) |
| default | Print log only on build failure |

`--print-build-logs` is useful for CI where you want full build output in the CI log even on success.

### The R13Y Project

**URL**: `r13y.com` (reproducibility checking website), source: `github.com/grahamc/r13y.com`.

**What it does**: Builds specific NixOS system closures (ISO images: minimal and Gnome) twice — at different times, on different hardware, with different kernels — and compares the outputs bit-for-bit. Reports which store paths in the closure are reproducible.

**Methodology**:
1. Build a NixOS ISO image with `nix build`
2. Build the same ISO again (potentially different machine/kernel)
3. Compare each store path in both closures using their NAR hashes
4. Report the percentage of paths that match

The distinction between runtime vs. build-time dependencies: "runtime dependencies" reports only include packages in the final ISO; "build-time dependencies" includes everything used to construct it (compilers, etc.).

**Current status** (early 2026): NixOS minimal ISO consistently achieves >97% reproducibility. Full Gnome ISO is lower due to third-party fonts and complex build processes. Live stats at `reproducible.nixos.org`.

**Maintained by**: Graham Christensen (grahamc), core NixOS contributor. Built with Rust + Buildkite for automation.

**Relationship to `nix build --check`**: r13y.com uses the same underlying mechanism (`build-repeat` + `diff-hook`) at infrastructure scale to catch non-determinism across packages. Individual developers use `nix build --check` for targeted testing.

### Practical Workflow for Diagnosing Non-Determinism

```bash
# Step 1: Build the package
nix build nixpkgs#suspectPkg

# Step 2: Rebuild with --check --keep-failed
nix build --check --keep-failed nixpkgs#suspectPkg

# Step 3: If exit code 1, use diffoscope
nix run nixpkgs#diffoscope -- \
  /nix/store/<hash>-suspectPkg-* \
  /nix/store/<hash>-suspectPkg-*.check

# Common findings from diffoscope:
# - Embedded build timestamp (fix: SOURCE_DATE_EPOCH is already set; check the specific tool)
# - Parallel build race condition (fix: serialize, or add to known-nondeterministic list)
# - Python .pyc files with timestamps (fix: PYTHONDONTWRITEBYTECODE or touch -t)
# - Rust build IDs (usually fine; just hashes of input files)
```

**`SOURCE_DATE_EPOCH`**: Nix's stdenv sets `SOURCE_DATE_EPOCH` to the Unix timestamp of the latest `mtime` in the source tree (or the flake's `lastModified`). Most build tools (tar, gzip, zip, Python) now respect this for reproducible timestamps. Packages that ignore `SOURCE_DATE_EPOCH` are the primary source of non-determinism.
