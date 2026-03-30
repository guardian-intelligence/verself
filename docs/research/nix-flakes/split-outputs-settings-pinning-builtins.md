# Split Outputs, `nix.*` NixOS Module, Flake Input Pinning, and Data-Transform Builtins

Four topics that appear scattered across other documents but each warrant dedicated depth: the full split-output system (beyond the stdenv-and-packaging.md sketch), the NixOS `nix.*` module options sourced directly from nixpkgs, strategic input pinning approaches, and the data-transformation builtins not covered in stdlib-and-builtins.md.

---

## Part 1: Multi-Output Packages — Full Reference

Source: `nixos/pkgs/build-support/setup-hooks/multiple-outputs.sh`, nixpkgs manual stdenv chapter, nixpkgs source `lib/multiple-outputs.sh`.

### Why Split Outputs

Every output produces a separate store path. The key motivation is **closure minimality**: a runtime dependency only needs the shared libraries (`$lib`), never the headers (`$dev`), documentation (`$doc`), or debug symbols (`$debug`). Without splitting, installing a C library would drag all its headers and docs into every consumer's closure.

Concrete closure comparison for `openssl`:
- `openssl` (default `$out`): binaries + man pages — ~5 MB
- `openssl.dev`: headers + pkg-config files — ~2 MB
- `openssl.lib`: shared libraries (`libssl.so`, `libcrypto.so`) — ~7 MB
- `openssl.man`: man pages — ~2 MB
- `openssl.debug`: DWARF debug symbols — ~30 MB

A Go program that links against OpenSSL at runtime only needs `openssl.lib` in its closure. Without split outputs it would also carry headers and docs.

### Declaring Outputs

```nix
stdenv.mkDerivation {
  pname = "mypkg";
  version = "1.0";
  outputs = [ "out" "dev" "lib" "man" "doc" "info" "debug" ];
  # ...
}
```

Rules:
- **First element is the default output.** `pkgs.mypkg` resolves to the first output. Consumers reference non-default outputs with `.name`: `pkgs.mypkg.dev`.
- The standard convention is `out` first unless the package is primarily a library, in which case `lib` or `out` (containing the shared library) comes first.
- Each name in `outputs` becomes an environment variable in the build environment containing the corresponding store path, e.g., `$dev`, `$lib`.

### Standard Output Names and `$outputXxx` Variables

`multiple-outputs.sh` uses these variables to route files during `fixupPhase`:

| Variable | Default fallback | Typical content |
|----------|-----------------|-----------------|
| `$outputDev` | `dev` → `out` | `include/`, `lib/pkgconfig/`, CMake find-modules, aclocal macros |
| `$outputLib` | `lib` → `out` | `lib/*.so`, `lib/*.so.*`, `lib/libexec/` |
| `$outputBin` | `bin` → `out` | `bin/`, `sbin/` |
| `$outputDoc` | `doc` → `out` | `share/doc/` |
| `$outputDevdoc` | `devdoc` | Developer API docs (gtk-doc, devhelp) |
| `$outputMan` | `man` → `$outputBin` | `share/man/man[01235678]/` |
| `$outputDevman` | `devman` → `$outputMan` | `share/man/man3/` (library function docs) |
| `$outputInfo` | `info` → `$outputBin` | `share/info/` |

The fallback chain (`dev` → `out`) means: if the `outputs` list does not include `dev`, `$outputDev` points at `$out` and headers stay there. This makes the system backward-compatible — packages that don't declare `dev` behave as before.

### How `fixupPhase` Routes Files

The `multiple-outputs.sh` hook runs in `fixupPhase` (after `installPhase`). It walks the installed tree and calls `moveToOutput` for each path that matches a pattern:

```
include/          → $outputDev
lib/pkgconfig/    → $outputDev
lib/cmake/        → $outputDev
share/aclocal/    → $outputDev
share/man/man3/   → $outputDevman
share/man/        → $outputMan
share/info/       → $outputInfo
share/doc/        → $outputDoc
```

The actual routing is done by shell functions:

```bash
# From multiple-outputs.sh — internal helper
moveToOutput() {
    local output="$2"
    local target="$(echo ${!output})"  # dereference variable
    local from="$prefix/$1"
    if [ -e "$from" ]; then
        mkdir -p "$target/$(dirname "$1")"
        mv "$from" "$target/$1"
    fi
}
```

Override a specific routing in your derivation:
```nix
postFixup = ''
  moveToOutput "lib/libfoo.a" "$dev"   # static lib → dev (not lib)
  moveToOutput "share/gtk-doc" "$devdoc"
'';
```

### Output Cross-References and Closure Direction

The critical constraint: **outputs earlier in the list may reference later outputs, but the reverse creates circular dependencies and bloated closures.**

Correct direction:
```
$dev → $out  (dev contains pkg-config that references $out's paths — OK)
$out → never $dev  (runtime lib should never pull in headers)
```

In nixpkgs, `dev` derivations automatically have `propagatedBuildInputs` set to `[ out ]` by `multiple-outputs.sh`. This means anyone who `buildInputs = [ mypkg.dev ]` automatically gets `mypkg` (the default output, `$out`) in their build environment.

### Referencing Non-Default Outputs

```nix
# In buildInputs / nativeBuildInputs:
buildInputs = [ openssl.dev ];        # headers for compilation
nativeBuildInputs = [ pkg-config ];   # finds .pc from openssl.dev
propagatedBuildInputs = [ openssl.lib ]; # runtime: linked shared library

# In a NixOS module:
environment.systemPackages = [
  curl              # default output (binary)
  curl.man          # man pages in addition
];

# In nix profile / nix build:
nix profile install nixpkgs#curl^man   # caret syntax for named output
nix build nixpkgs#openssl^dev
```

### `pkg.all` — List of All Outputs

Every derivation with multiple outputs has an `all` attribute that is the list of all output store paths:

```nix
# Access all outputs:
pkgs.openssl.all
# => [ /nix/store/...-openssl-3.3.1  /nix/store/...-openssl-3.3.1-dev  /nix/store/...-openssl-3.3.1-lib  ... ]
```

Practical uses:
- `nix build` a package by passing `.all` to ensure all outputs are built
- Binary cache population: push all outputs at once

```bash
# Push all outputs of a package to a binary cache:
nix store sign --key-file /etc/nix/secret-key $(nix path-info --recursive nixpkgs#openssl)
# Or using pkg.all in a Nix expression for a push script
```

### `meta.outputsToInstall`

Controls which outputs `nix profile install` installs by default (without the `^output` caret syntax):

```nix
meta.outputsToInstall = [ "out" "man" ];  # install binaries + man pages by default
```

If not set, defaults to `[ (head outputs) ]` — just the first output.

For library-only packages where the first output is `lib`, setting `outputsToInstall = [ "lib" "dev" ]` lets `nix profile install nixpkgs#zlib` give both the shared library and headers.

`meta.outputsToInstall` lives in `meta`, not `passthru`. It is set with:
```nix
stdenv.mkDerivation {
  # ...
  meta = {
    outputsToInstall = [ "out" "man" ];
  };
}
```

### Real-World Example: `glibc`

`glibc` is the canonical multi-output example in nixpkgs. Its closure decomposition:

| Output | Size | Contains |
|--------|------|----------|
| `glibc` (= `out`) | ~250 MB | Locale data (`share/locale/`), runtime fallback |
| `glibc.lib` | ~5 MB | `libc.so.6`, `libm.so.6`, `libpthread.so.0`, etc. |
| `glibc.dev` | ~20 MB | `include/` headers, `lib/*.a` static libs |
| `glibc.man` | ~2 MB | Man pages |
| `glibc.debug` | ~300 MB | DWARF debug info |

A dynamically linked program at runtime only needs `glibc.lib` in its closure. Without split outputs, every program would carry ~250 MB of locale data.

### The `debug` Output and `separateDebugInfo`

```nix
stdenv.mkDerivation {
  pname = "myservice";
  version = "1.0";
  separateDebugInfo = true;  # automatically adds "debug" to outputs
  # ...
}
```

What `separateDebugInfo = true` does in `fixupPhase`:
1. Compiles with `-g` (debug info in object files)
2. After `installPhase`, strips binaries in `$out` using `strip --strip-debug`
3. Extracts DWARF info and saves to `$debug/lib/debug/.build-id/XX/YYYYYY...`

The `.build-id` path format is what `gdb`, `lldb`, and `debuginfod` use to locate external debug files. nixpkgs's `cache.nixos.org` hosts `debug` outputs for all packages that set `separateDebugInfo = true`, enabling `gdb` to fetch symbols on demand via debuginfod without pre-installing the `debug` output.

To keep debug info in `$out` for a single package in an overlay:
```nix
nixpkgs.overlays = [
  (final: prev: {
    myservice = prev.enableDebugging prev.myservice;
  })
];
```

`pkgs.enableDebugging pkg` is a nixpkgs function that adds `enableDebugging = true` and ensures `-g` is not stripped.

### Overriding Which Output Is Default

To make `$lib` the default output (so `pkgs.zlib` gives the shared library, not the combined package):

```nix
stdenv.mkDerivation {
  pname = "zlib";
  outputs = [ "out" "dev" ];  # "out" is first = default
  # zlib places the .so in "out", headers in "dev"
}
```

Alternatively, when you want `lib` to be first (and thus default):
```nix
outputs = [ "lib" "dev" "out" ];
# Now pkgs.mypkg == pkgs.mypkg.lib
```

nixpkgs convention: use `out` as first output for packages where the binary/main artifact is the primary consumer interest; use `lib` first for pure libraries.

---

## Part 2: The NixOS `nix.*` Module

Source: `nixos/modules/config/nix.nix`, `nixos/modules/services/system/nix-daemon.nix`, `nixos/modules/services/misc/nix-gc.nix`, `nixos/modules/services/misc/nix-optimise.nix`, `nixos/modules/config/nix-channel.nix`, `nixos/modules/config/nix-flakes.nix` in nixpkgs.

### Module Structure

The NixOS `nix.*` namespace is split across multiple modules that were separated during the 24.05 refactor:

| Module file | Manages |
|-------------|---------|
| `config/nix.nix` | `nix.settings`, `nix.extraOptions`, `nix.checkConfig`, generates `/etc/nix/nix.conf` |
| `services/system/nix-daemon.nix` | `nix.package`, `nix.enable`, `nix.nrBuildUsers`, `nix.daemonCPUSchedPolicy`, systemd service |
| `services/misc/nix-gc.nix` | `nix.gc.*`, systemd timer for GC |
| `services/misc/nix-optimise.nix` | `nix.optimise.*`, systemd timer for optimise |
| `config/nix-channel.nix` | `nix.channel.enable`, `nix.nixPath` |
| `config/nix-flakes.nix` | `nix.registry` |

### `nix.settings`

```nix
nix.settings = {
  # Type: attrsOf (either confAtom (listOf confAtom))
  # confAtom = null | bool | int | float | str | path | package
  # Default: {}
};
```

`nix.settings` is a **freeform** submodule — any key is accepted and maps directly to a `nix.conf` key. Structured options are defined within the submodule for the most common keys (providing types and documentation); all others pass through as-is.

**Structured keys within `nix.settings`**:

| Key | Type | NixOS default | Notes |
|-----|------|---------------|-------|
| `substituters` | `listOf str` | `["https://cache.nixos.org/"]` | Appended with `mkAfter` by default; user values prepend |
| `trusted-public-keys` | `listOf str` | `["cache.nixos.org-1:6NCH..."]` | Verify binary cache signatures |
| `trusted-substituters` | `listOf str` | `[]` | Substituters non-root users may add |
| `require-sigs` | `bool` | `true` | Reject unsigned narinfo |
| `trusted-users` | `listOf str` | `["root"]` | Full daemon rights; `@wheel` for group |
| `allowed-users` | `listOf str` | `["*"]` | Who can connect to daemon at all |
| `max-jobs` | `int` or `"auto"` | `"auto"` | Parallel local builds; `"auto"` = nproc |
| `cores` | `int` | `0` | CPUs per job; `0` = all cores |
| `sandbox` | `bool` or `"relaxed"` | `true` (Linux) | `"relaxed"` allows `__noChroot = true` derivations |
| `extra-sandbox-paths` | `listOf str` | `[]` | Host paths to expose inside sandbox |
| `auto-optimise-store` | `bool` | `false` | Hardlink dedup after each build |
| `system-features` | `listOf str` | `["nixos-test" "benchmark" "big-parallel" "kvm"]` + arch features | Affects `requiredSystemFeatures` routing |

**Freeform keys** (not schema-validated but frequently used):

```nix
nix.settings = {
  experimental-features = [ "nix-command" "flakes" "ca-derivations" ];
  keep-outputs = true;
  keep-derivations = true;
  log-lines = 50;            # lines of build log shown on failure
  warn-dirty = false;        # suppress dirty flake warning
  accept-flake-config = false; # refuse nix.conf overrides from flakes (security)
  narinfo-cache-negative-ttl = 0;  # always retry missing paths
  connect-timeout = 5;       # seconds before substituter timeout
};
```

**Legacy option renames**: before NixOS 22.05, many settings lived directly under `nix.*`. The module now emits `mkRenamedOptionModuleWith` deprecation warnings and maps them:

```
nix.binaryCaches          → nix.settings.substituters
nix.binaryCachePublicKeys → nix.settings.trusted-public-keys
nix.trustedUsers          → nix.settings.trusted-users
nix.allowedUsers          → nix.settings.allowed-users
nix.useSandbox            → nix.settings.sandbox
nix.buildCores            → nix.settings.cores
nix.maxJobs               → nix.settings.max-jobs
nix.autoOptimiseStore     → nix.settings.auto-optimise-store
nix.systemFeatures        → nix.settings.system-features
```

Using the old names still works but emits a warning.

**How settings become `nix.conf`**: the module calls `pkgs.formats.nixConf {}.generate "nix.conf" cfg.settings`. This serializes the attrset as key-value pairs. Lists become space-separated values. Bools become `true`/`false`. The file is placed at `/etc/nix/nix.conf`.

**`nix.extraOptions`**: raw text appended verbatim after the generated settings. Use for:
- Settings not yet in the `semanticConfType` schema
- Comments inside `nix.conf`
- Settings requiring special formatting

```nix
nix.extraOptions = ''
  # allow wheel group to use substituters
  trusted-users = root @wheel
  keep-outputs = true
'';
```

When both `nix.settings.key = value` and `nix.extraOptions` contain the same key, the `extraOptions` line is appended after — behavior depends on Nix's last-wins or error semantics for the specific key. Prefer `nix.settings` for structured keys.

**Security implications of `trusted-users`**: per the source comment, trusted users are "essentially equivalent to giving that user root access to the system." They can:
- Override `substituters` to fetch from untrusted caches
- Pass `--option sandbox false` to disable sandboxing
- Import unsigned NARs into the store

Add users to `trusted-users` only deliberately. The `@wheel` group shorthand is common for admin machines.

### `nix.gc` — Automated Garbage Collection

Source: `nixos/modules/services/misc/nix-gc.nix`.

```nix
nix.gc = {
  automatic = true;            # enable systemd timer (default: false)
  dates = "weekly";            # systemd calendar expression (default: "03:15")
  options = "--delete-older-than 30d";  # flags to nix-collect-garbage (default: "")
  randomizedDelaySec = "45min"; # jitter (default: "0")
  persistent = true;           # catch up if system was off (default: true)
};
```

The generated systemd unit runs `nix-collect-garbage ${cfg.options}`. The `dates` value is a systemd `OnCalendar` expression: `"weekly"`, `"daily"`, `"Mon 03:00"`, `"*-*-* 03:15:00"`.

Common `options` patterns:
```
"--delete-older-than 30d"    # delete generations older than 30 days
"--delete-older-than 7d"     # aggressive: 7 days
"--max-freed $((64 * 1024**3))"  # free at most 64 GB per run
""                            # delete all old generations (dangerous on production)
```

Note: `--delete-older-than Nd` deletes **profile generations** older than N days, then performs a full GC sweep. It does not directly delete store paths by age — the age is determined by when the profile generation was created.

The `persistent = true` default means if the machine was off at the scheduled time, GC runs on next boot. Set `persistent = false` to skip missed runs.

### `nix.optimise` — Automated Store Optimization

Source: `nixos/modules/services/misc/nix-optimise.nix`.

```nix
nix.optimise = {
  automatic = true;            # enable systemd timer (default: false)
  dates = [ "03:45" ];         # list of systemd calendar expressions (default: ["03:45"])
};
```

Runs `nix-store --optimise` on schedule. The `dates` option is a **list** (unlike `nix.gc.dates` which is a single string) — can run at multiple times. The timer has `Persistent = true` and `RandomizedDelaySec = 1800` (30-minute jitter) hardcoded in the module.

`nix-store --optimise` walks the entire store finding files with identical content and replacing them with hardlinks. This is equivalent to `auto-optimise-store = true` but run on-demand rather than per-build.

**Interaction with `auto-optimise-store`**: the two are complementary. `auto-optimise-store` runs after each build (low overhead per-build but adds latency). `nix.optimise` runs in a batch sweep. For CI systems with many rapid builds, batch mode (`nix.optimise.automatic`) avoids per-build optimization overhead.

### `nix.registry` — Pinned Flake Registry

Source: `nixos/modules/config/nix-flakes.nix`.

```nix
nix.registry = {
  # Type: attrsOf (submodule { from; to; flake; exact; })
  # Default: {}
};
```

Each attribute becomes a registry entry. The submodule has four options:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `from` | `referenceAttrs` | `{ type = "indirect"; id = <attrname>; }` | The reference to match (auto-set from attrname) |
| `to` | `referenceAttrs` | derived from `flake` | The resolved reference |
| `flake` | `nullOr attrs` | `null` | Set to a flake input to auto-populate `to` |
| `exact` | `bool` | `true` | If false, prefix-match (e.g., `nixpkgs` matches `nixpkgs/nixos-24.11`) |

**Critical pattern: pinning the system registry to the flake's nixpkgs input.**

Without this, `nix run nixpkgs#hello` on a NixOS machine fetches whatever nixpkgs revision the flake registry resolves to — which may differ from the nixpkgs used to build the system. With this:

```nix
{ inputs, ... }: {
  nix.registry.nixpkgs.flake = inputs.nixpkgs;
}
```

Now `nix run nixpkgs#hello` uses the exact nixpkgs rev in `flake.lock`, ensuring binary cache hits and eliminating registry mismatch. The `flake =` shorthand auto-populates:
```nix
nix.registry.nixpkgs.to = {
  type = "path";
  path = inputs.nixpkgs.outPath;
  narHash = inputs.nixpkgs.narHash;
  lastModified = inputs.nixpkgs.lastModified;
  rev = inputs.nixpkgs.rev;
};
```

Multiple registry entries:
```nix
nix.registry = {
  nixpkgs.flake = inputs.nixpkgs;
  home-manager.flake = inputs.home-manager;
  n.flake = inputs.nixpkgs;  # shorthand alias
};
```

### `nix.nixPath` — `NIX_PATH` / `<nixpkgs>` Search Path

Source: `nixos/modules/config/nix-channel.nix`.

```nix
nix.nixPath = [
  # Type: listOf str
  # Default (when nix.channel.enable = true):
  #   [ "nixpkgs=/nix/var/nix/profiles/per-user/root/channels/nixos"
  #     "nixos-config=/etc/nixos/configuration.nix"
  #     "/nix/var/nix/profiles/per-user/root/channels" ]
  # Default (when nix.channel.enable = false):
  #   []
];
```

To pin `<nixpkgs>` to the flake's input (common in flakes-only setups):
```nix
nix.nixPath = [ "nixpkgs=${inputs.nixpkgs}" ];
```

This makes `import <nixpkgs> {}` in `nix-shell` scripts and legacy tooling use the same nixpkgs as your flake. Without it, `<nixpkgs>` resolves to the channel (if channels are enabled) or fails (if channels are disabled).

### `nix.channel.enable` — Disable Channels for Flakes-Only

Source: `nixos/modules/config/nix-channel.nix`.

```nix
nix.channel.enable = false;
# Type: bool
# Default: true
```

When `false`:
- `nix-channel` binary is removed from `$PATH` (`rm --force $out/bin/nix-channel` in `extraSetup`)
- `NIX_PATH` defaults to `[]` (empty)
- `/root/.nix-channels` is not initialized
- `/nix/var/nix/profiles/per-user/root/channels` is not created

Recommended for flakes-only NixOS configurations. Combine with `nix.registry` and `nix.nixPath` to retain compatibility for legacy tooling:

```nix
{
  nix.channel.enable = false;
  nix.registry.nixpkgs.flake = inputs.nixpkgs;
  nix.nixPath = [ "nixpkgs=${inputs.nixpkgs}" ];
}
```

Available since NixOS 23.11 when the channel module was split from the main nix module.

### `nix.package` — Nix Version Override

```nix
nix.package = pkgs.nixVersions.stable;
# Type: package
# Default: pkgs.nix (follows nixpkgs's default Nix version)
```

Common overrides:
```nix
nix.package = pkgs.nixVersions.stable;     # latest stable (tracks releases)
nix.package = pkgs.nixVersions.nix_2_24;   # pin exact minor version
nix.package = pkgs.lix;                     # Lix fork
```

`pkgs.nixVersions` is an attrset with keys like `nix_2_18`, `nix_2_24`, `stable`, `latest`. The NixOS module source shows that `nixPackage.pname != "lix"` is checked — the daemon service has Lix-specific conditional logic.

### Recommended Flakes-Only NixOS `nix.*` Configuration

```nix
{ pkgs, inputs, ... }: {
  nix = {
    package = pkgs.nixVersions.stable;

    settings = {
      experimental-features = [ "nix-command" "flakes" ];
      trusted-users = [ "root" "@wheel" ];
      substituters = [
        "https://cache.nixos.org/"
        "https://nix-community.cachix.org"
      ];
      trusted-public-keys = [
        "cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY="
        "nix-community.cachix.org-1:mB9FSh9qf2dCimDSUo8Zy7bkq5CX+/rkCWyvRCUSeBw="
      ];
      auto-optimise-store = true;
    };

    gc = {
      automatic = true;
      dates = "weekly";
      options = "--delete-older-than 30d";
    };

    # Pin system registry to flake inputs
    registry.nixpkgs.flake = inputs.nixpkgs;

    # Disable legacy channels
    channel.enable = false;
    nixPath = [ "nixpkgs=${inputs.nixpkgs}" ];
  };
}
```

---

## Part 3: Flake Input Pinning Strategies

### What `flake.lock` Guarantees and Does Not Guarantee

`flake.lock` pins every transitive input to a specific NAR hash + git rev. It guarantees:
- Same source code on every machine
- Binary cache hits (same content = same store path)
- No silent dependency drift

It does NOT guarantee:
- That the locked revision is free of security vulnerabilities
- That two flakes using the same input name use the same revision (different locks)
- That updates to one input don't conflict with another

### The `follows` Diamond Deduplication Pattern

When `flake-parts` and `home-manager` both declare `inputs.nixpkgs`, without `follows` you get two nixpkgs copies in the lock — two evaluation closures, two sets of store paths, two sets of binary cache queries.

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

    home-manager = {
      url = "github:nix-community/home-manager";
      inputs.nixpkgs.follows = "nixpkgs";  # deduplicate
    };

    flake-parts = {
      url = "github:hercules-ci/flake-parts";
      inputs.nixpkgs-lib.follows = "nixpkgs";
    };

    sops-nix = {
      url = "github:Mic92/sops-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };
}
```

This is the **pin-all** (monorepo) pattern: every transitive `nixpkgs` reference is collapsed to one. Binary cache hit rate is maximized because all packages are built against the same nixpkgs commit.

**Selective follows** (more flexible):
Only pin the shared inputs that affect binary cache hits (nixpkgs), leave idiosyncratic inputs (tool-specific dependencies that don't export packages) alone:

```nix
# Only nixpkgs follows, leave tool-specific inputs free:
inputs.agenix.inputs.nixpkgs.follows = "nixpkgs";
# But NOT: inputs.agenix.inputs.some-internal-dep.follows = ...
```

**Tradeoff**: full `follows` maximizes cache hits but means your nixpkgs version must satisfy all consumers. If a tool requires a minimum nixpkgs version and you pin an older one via `follows`, builds may fail.

### Single Input Update

```bash
# Update only nixpkgs, leave everything else locked:
nix flake update nixpkgs       # Nix ≥ 2.19 syntax
# Old syntax (deprecated in Nix 2.19, removed later):
nix flake lock --update-input nixpkgs

# Update multiple specific inputs:
nix flake update nixpkgs home-manager
```

The `nix flake update <name>` form (Nix ≥ 2.19) accepts one or more input names. Without a name, all inputs update.

### Automated Update CI Pattern

```yaml
# .github/workflows/update-flake.yml
name: Update flake.lock
on:
  schedule:
    - cron: "0 0 * * 1"  # Monday midnight UTC
jobs:
  update:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: DeterminateSystems/nix-installer-action@main
      - uses: DeterminateSystems/magic-nix-cache-action@main
      - name: Update flake.lock
        run: nix flake update --commit-lock-file
      - name: Open PR
        uses: peter-evans/create-pull-request@v6
        with:
          title: "chore: update flake.lock"
          branch: "flake-lock-update"
```

The `--commit-lock-file` flag commits the updated `flake.lock` automatically. The PR is reviewed like any dependency update — `nix flake metadata` can diff what changed:

```bash
# Before merge, diff the lock changes:
nix flake metadata --json | jq .locks.nodes
# Or:
git diff flake.lock
```

### `flake = false` for Non-Flake Source Inputs

```nix
inputs.myLib = {
  url = "github:some-org/my-lib";
  flake = false;  # skip flake evaluation; treat as pure source tree
};
```

With `flake = false`:
- The input is fetched and locked (NAR hash)
- It is NOT evaluated as a flake (no `outputs` function called)
- `inputs.myLib` is just a store path (the source tree)
- Accessing it: `inputs.myLib.outPath` or just `inputs.myLib` (coerces to store path)

Use when you need a source tree (for `src =` in a derivation) but the repo has no `flake.nix`, or when you want to avoid the evaluation overhead of a dependency's flake:

```nix
outputs = { self, nixpkgs, myLib }: {
  packages.x86_64-linux.default = nixpkgs.legacyPackages.x86_64-linux.stdenv.mkDerivation {
    pname = "my-app";
    src = myLib;  # the source tree directly
    # ...
  };
};
```

### `npins` — Pinning Without Flakes

`npins` (https://github.com/andir/npins) is a dependency pinning tool that works without the `flakes` experimental feature. It predates flakes and remains useful when:
- You need to pin non-flake inputs alongside flake inputs
- You prefer a simpler JSON format over `flake.lock`
- You're maintaining legacy Nix code that uses `<nixpkgs>`

**Installation**:
```bash
nix-shell -p npins  # from nixpkgs
# or use the latest development version:
nix shell -f https://github.com/andir/npins/archive/master.tar.gz
```

**Initialization**:
```bash
npins init  # creates npins/default.nix and npins/sources.json
# By default adds nixpkgs-unstable; --bare to skip
npins init --bare
```

**Adding sources** (multiple types supported):

```bash
# GitHub release tracking:
npins add github NixOS nixpkgs --branch nixos-unstable
npins add github ytdl-org youtube-dl

# PyPI packages:
npins add pypi streamlit --at 1.9.0

# Generic git:
npins add git https://example.com/repo.git -b main

# Nix channel (provides programs.sqlite DB):
npins add channel nixos-24.11

# GitLab (with token for private):
npins add gitlab my-org my-private-repo --token "$GITLAB_TOKEN"
```

**`npins/sources.json` format**:
```json
{
  "pins": {
    "nixpkgs": {
      "type": "GitHub",
      "owner": "NixOS",
      "repo": "nixpkgs",
      "branch": "nixos-unstable",
      "revision": "abc123...",
      "url": "https://github.com/NixOS/nixpkgs/archive/abc123.tar.gz",
      "hash": "sha256-..."
    }
  },
  "version": 2
}
```

The hash uses SRI format (sha256-...) compatible with `fetchTarball`/`fetchzip`.

**`npins/default.nix`** (generated, do not edit):
```nix
# Auto-generated by npins
let
  # Use fetchTarball for GitHub/GitLab; fetchGit for bare git
  fetch = { url, hash, ... }: builtins.fetchTarball { inherit url; sha256 = hash; };
in
  builtins.mapAttrs (_: fetch) (builtins.fromJSON (builtins.readFile ./sources.json))
```

**Integration with `flake.nix`**:
```nix
# Use npins inside a flake for non-flake dependencies:
outputs = { self, nixpkgs }: let
  sources = import ./npins;
  # sources.mylib is a store path to the tarball contents
in {
  packages.x86_64-linux.default = nixpkgs.legacyPackages.x86_64-linux.stdenv.mkDerivation {
    src = sources.mylib;
    # ...
  };
};
```

**Update workflow**:
```bash
npins update             # update all pins to latest
npins update nixpkgs     # update only one
npins show               # list current pins with versions
npins verify             # check hashes match upstream (no network: compare stored hash)
```

**Freeze/unfreeze** (prevent specific pins from updating):
```bash
npins freeze nixpkgs     # exclude from `npins update`
npins unfreeze nixpkgs
```

**Import from flake.lock**:
```bash
npins import-flake       # imports all inputs from flake.lock into sources.json
```

**Comparison with `niv`**:

| Feature | npins | niv |
|---------|-------|-----|
| Source types | GitHub, GitLab, git, PyPI, channel, Gitea | GitHub, GitLab, git |
| Hash format | SRI (`sha256-...`) | `sha256` hex |
| Import from flake.lock | Yes | No |
| Freeze/pin | Yes | No |
| Active development (2025) | Yes | Minimal |
| License | EUPL-1.2 | MIT |

### FlakeHub Semver Pinning

FlakeHub (https://flakehub.com) is Determinate Systems' flake registry that adds semver-range pinning. Instead of pinning to a git rev, you pin to a version range and FlakeHub resolves it server-side.

**URL format**:
```nix
inputs.nixpkgs.url = "https://flakehub.com/f/NixOS/nixpkgs/0.1";
# Cargo-style: "at least 0.1, never 0.2+"
inputs.agenix.url = "https://flakehub.com/f/ryantm/agenix/0.13";
# Wildcard:
inputs.nixpkgs.url = "https://flakehub.com/f/NixOS/nixpkgs/0.2405.*";
# Exact:
inputs.nixpkgs.url = "https://flakehub.com/f/NixOS/nixpkgs/=0.2405.1";
```

**Version constraint syntax** (Cargo-compatible):
- `0.13` — caret range: `>=0.13.0 <0.14.0`
- `0.2405.*` — wildcard on patch
- `*` — highest published version
- `=3.2.1` — exact pin, no updates

**`flake.lock` behavior**: FlakeHub resolves the constraint to a specific tarball URL and stores that in `flake.lock`. The lock is still deterministic — the range is resolved once at lock time and frozen. Running `nix flake update` resolves the range again and updates the lock to the newest version within the range.

**When to use**: for stable third-party flakes that use semver properly. For nixpkgs specifically, the 0.2405.* pattern tracks a specific NixOS release month (24.05 = `0.2405`). This is primarily a Determinate Systems ecosystem pattern; most flakes on GitHub don't publish to FlakeHub.

### Security Considerations for Input Updates

`nix flake update` is a **security event**, not a routine maintenance task. Each input update can introduce:
- New package versions with vulnerabilities
- Changed behavior in build system hooks
- Upstream supply-chain compromises

Best practices:
- Review `nix flake metadata` diff before accepting updates
- Run `nix flake check` after updating
- Use `nix flake update <single-input>` rather than updating everything at once
- Audit inputs.nixpkgs updates through NixOS release notes, not just the git rev
- Pin critical infrastructure inputs explicitly rather than tracking `nixos-unstable`

```bash
# See what changed in a flake.lock update before accepting:
git diff flake.lock
nix flake metadata --json | jq '.locks.nodes | to_entries[] | {key, rev: .value.locked.rev}'
```

---

## Part 4: Data-Transformation Builtins

Source: `nix.dev/manual/nix/stable/language/builtins` — section "Built-in Functions".

These builtins complement the `lib.attrsets` and `lib.lists` functions covered in stdlib-and-builtins.md but are at the `builtins.*` level (available in pure expressions without importing nixpkgs).

### `builtins.groupBy`

```
groupBy :: (a -> String) -> [a] -> AttrSet  (of [a])
```

Added: Nix 2.5

Groups a list by the string returned by the key function. Returns an attrset where each key maps to the list of elements that produced that key.

```nix
builtins.groupBy (builtins.substring 0 1) [ "foo" "bar" "baz" "qux" ]
# => { b = [ "bar" "baz" ]; f = [ "foo" ]; q = [ "qux" ]; }

# Group derivations by their license:
builtins.groupBy (drv: drv.meta.license.spdxId or "unknown") (builtins.attrValues pkgs)
# => { MIT = [ ...pkgs... ]; "GPL-2.0-only" = [ ...pkgs... ]; unknown = [ ...pkgs... ]; }
```

Elements preserve their original order within each group. The key function must return a string (attrset keys are always strings in Nix).

Practical use in nixpkgs: `pkgs.lib.groupBy` is the same function, aliased from `builtins.groupBy`. Used in platform detection code to group architecture variants.

### `builtins.zipAttrsWith`

```
zipAttrsWith :: (String -> [a] -> b) -> [AttrSet] -> AttrSet
```

Transposes a list of attrsets into an attrset of lists, then applies a merge function. All keys from all attrsets are collected; the merge function receives the key name and the list of values for that key (from whichever attrsets had that key).

```nix
builtins.zipAttrsWith
  (name: values: { inherit name values; })
  [ { a = "x"; } { a = "y"; b = "z"; } ]
# => { a = { name = "a"; values = [ "x" "y" ]; };
#      b = { name = "b"; values = [ "z" ]; }; }
```

If an attrset doesn't have a key, it simply doesn't contribute a value — the list will be shorter for that key. There is NO null-padding for missing values.

```nix
# Merge multiple package overlays by concatenating lists:
builtins.zipAttrsWith
  (name: values: builtins.concatLists values)
  [ { buildInputs = [a]; } { buildInputs = [b]; extraDeps = [c]; } ]
# => { buildInputs = [a b]; extraDeps = [c]; }
```

**`builtins.zipAttrs`** (no `With`): the degenerate case — always collects into lists without a merge function:

```nix
builtins.zipAttrs [ { a = 1; } { a = 2; b = 3; } ]
# => { a = [1 2]; b = [3]; }
```

`lib.attrsets.zipAttrsWith` is identical. The stdlib function is the canonical way to call it in nixpkgs code.

### `builtins.partition`

```
partition :: (a -> Bool) -> [a] -> { right :: [a]; wrong :: [a] }
```

Splits a list into two sublists based on a predicate. Returns an attrset with `right` (predicate true) and `wrong` (predicate false).

```nix
builtins.partition (x: x > 10) [ 1 23 9 3 42 ]
# => { right = [ 23 42 ]; wrong = [ 1 9 3 ]; }

# Split packages into those with and without tests:
builtins.partition (pkg: pkg.doCheck or false) (builtins.attrValues pkgs)
# => { right = [ ...tested pkgs... ]; wrong = [ ...untested pkgs... ]; }
```

Both sublists preserve the original relative order of elements. Use `partition` instead of two separate `builtins.filter` calls when you need both halves — it's one pass over the list.

### `builtins.intersectAttrs`

```
intersectAttrs :: AttrSet -> AttrSet -> AttrSet
```

Returns the subset of the second attrset (`e2`) whose keys also exist in the first attrset (`e1`). Values come from `e2`; the first attrset only contributes its key names.

```nix
builtins.intersectAttrs { a = null; c = null; } { a = 1; b = 2; c = 3; d = 4; }
# => { a = 1; c = 3; }
```

Performance: O(n log m) where n is the smaller set. In nixpkgs, this is the core of `callPackage`'s argument extraction:

```nix
# callPackage mechanism (simplified):
let
  fn = { zlib, openssl, stdenv }: stdenv.mkDerivation { ... };
  fnArgs = builtins.functionArgs fn;   # => { zlib = true; openssl = true; stdenv = true; }
  pkgsSubset = builtins.intersectAttrs fnArgs pkgs;  # only the needed attrs from pkgs
in fn pkgsSubset
```

`builtins.intersectAttrs` is how `callPackage` avoids passing the entire `pkgs` attrset (which would force evaluation of all packages).

### `builtins.removeAttrs`

```
removeAttrs :: AttrSet -> [String] -> AttrSet
```

Returns a new attrset with the named keys removed. Non-existent keys are silently ignored.

```nix
builtins.removeAttrs { x = 1; y = 2; z = 3; } [ "a" "x" "z" ]
# => { y = 2; }

# In package wrappers: remove meta attributes before passing to builder:
builtins.removeAttrs args [ "override" "overrideDerivation" "passthru" ]
```

Common in `makeOverridable` wrappers and `callPackage` to strip non-derivation attributes before passing to `builtins.derivation`. Unlike `removeAttrs`, `builtins.intersectAttrs` is additive (keep what matches); `removeAttrs` is subtractive (remove what's named).

### `builtins.catAttrs`

```
catAttrs :: String -> [AttrSet] -> [a]
```

Collects the value of a named attribute from each attrset in a list, **skipping** elements that don't have the attribute. Equivalent to `map (x: x.${name}) (filter (x: x ? name) list)` but in one pass.

```nix
builtins.catAttrs "a" [ { a = 1; } { b = 0; } { a = 2; } ]
# => [ 1 2 ]  (b=0 element is skipped, not errored)

# Extract all package names from a list of derivation-like attrsets:
builtins.catAttrs "pname" packageList
# => [ "curl" "openssl" ... ]  (skips any element without pname)
```

This is different from `map (x: x.name)` which would throw an error if any element lacks the attribute. `catAttrs` silently skips them — appropriate when the attribute is optional.

### `builtins.genericClosure`

```
genericClosure :: { startSet :: [{ key :: comparable; ... }]; operator :: ({ key; ... } -> [{ key; ... }]) } -> [{ key; ... }]
```

Computes the transitive closure of a relation via BFS/DFS traversal. Each node must have a `key` attribute with a comparable value (int, float, bool, string, path, or list-of-comparable). Visited nodes are deduplicated by `key`.

```nix
# Collatz sequence generator (from the manual):
builtins.genericClosure {
  startSet = [ { key = 5; } ];
  operator = { key, ... }:
    if key == 1 then []
    else if builtins.bitAnd key 1 == 0
         then [ { key = key / 2; } ]
         else [ { key = 3 * key + 1; } ];
}
# => [ { key = 5; } { key = 16; } { key = 8; } { key = 4; } { key = 2; } { key = 1; } ]
```

Practical nixpkgs use: `nix-store --query --closure` is implemented using this same BFS algorithm. It's also used for dependency graph traversal in derivation meta-analysis tools.

The `startSet` elements can have additional attributes beyond `key`; those are preserved and passed to `operator`. The return includes all reachable nodes (including start nodes), deduplicated by `key`.

**Key constraint**: the `key` must be a comparable type. Attrsets and functions are NOT valid keys. Use a string (derivation store path, package name) as the key when traversing dependency graphs:

```nix
# Transitive dependency closure (conceptual):
builtins.genericClosure {
  startSet = [ { key = drv.drvPath; drv = drv; } ];
  operator = { drv, ... }:
    map (dep: { key = dep.drvPath; drv = dep; })
        (drv.buildInputs or []);
}
```

### `builtins.sort`

```
sort :: (a -> a -> Bool) -> [a] -> [a]
```

Returns a new sorted list using a comparator function. The comparator returns `true` if the first argument should come before the second (strictly less-than semantics). This is a **stable sort** — equal elements preserve their original relative order.

```nix
builtins.sort builtins.lessThan [ 483 249 526 147 42 77 ]
# => [ 42 77 147 249 483 526 ]

builtins.sort (a: b: a > b) [ 1 3 2 ]
# => [ 3 2 1 ]  (descending)

# Sort packages by name:
builtins.sort (a: b: a.name < b.name) packageList

# Sort by multiple criteria:
builtins.sort (a: b:
  if a.priority != b.priority
  then a.priority < b.priority
  else a.name < b.name
) items
```

Note: `builtins.lessThan` is the built-in comparator for numbers and strings. String comparison is lexicographic (byte order), not locale-aware.

The sort is implemented in C++ as a standard comparison sort; the "stable" guarantee comes from the implementation in Nix's evaluator, not a formal language guarantee.

### `builtins.concatMap`

```
concatMap :: (a -> [b]) -> [a] -> [b]
```

Maps a function that returns a list over each element, then concatenates all resulting lists (flattens one level). Equivalent to `builtins.concatLists (map f list)` but implemented as a single pass and therefore more efficient.

```nix
builtins.concatMap (x: [ x (-x) ]) [ 1 2 3 ]
# => [ 1 (-1) 2 (-2) 3 (-3) ]

# Expand each package's outputs into separate entries:
builtins.concatMap (pkg: pkg.outputs or [ "out" ]) packageList
```

`lib.lists.concatMap` is the same function. Both `lib.lists.concatMap` and `builtins.concatMap` are aliases for the same underlying operation; prefer `builtins.concatMap` in pure expressions (no nixpkgs import needed), `lib.concatMap` in nixpkgs code for consistency.

**Difference from `flatten`**: `concatMap` flattens exactly one level. `builtins.concatLists` also flattens one level. `lib.flatten` flattens all levels recursively and is much more expensive (and rarely what you want for structured data).

### Summary: Data-Transformation Builtin Quick Reference

| Builtin | Signature | Key behavior |
|---------|-----------|--------------|
| `groupBy` | `(a→str) → [a] → {str:[a]}` | Groups list by key function |
| `zipAttrsWith` | `(str→[a]→b) → [AttrSet] → AttrSet` | Merges attrset list; no null-padding for missing |
| `zipAttrs` | `[AttrSet] → AttrSet` | Like above but always collects into lists |
| `partition` | `(a→bool) → [a] → {right:[a];wrong:[a]}` | Split into two sublists |
| `intersectAttrs` | `AttrSet → AttrSet → AttrSet` | Keep keys from e2 that exist in e1 |
| `removeAttrs` | `AttrSet → [str] → AttrSet` | Remove named keys (missing = ignore) |
| `catAttrs` | `str → [AttrSet] → [a]` | Collect one field; skip missing |
| `genericClosure` | `{startSet;operator} → [node]` | BFS transitive closure; nodes need `key` |
| `sort` | `(a→a→bool) → [a] → [a]` | Stable sort with comparator |
| `concatMap` | `(a→[b]) → [a] → [b]` | flatMap; one level flatten; efficient |

---

## Factual Notes on Related Existing Documents

### `stdenv-and-packaging.md` — Multiple Outputs Section

The existing coverage is accurate. One addition: the `meta.outputsToInstall` field is set in `meta`, not `passthru`. The existing document does not mention this field at all — it controls which outputs `nix profile install` fetches by default.

### `stdlib-and-builtins.md` — `zipAttrsWith` Note

The existing document covers `lib.attrsets.zipAttrsWith` under the stdlib section (not under builtins). The note there correctly states "Missing keys produce shorter lists, not null-padded" — confirmed accurate.

### `daemon-protocol-pkgs-extend-direnv-lib.md` — `trusted-users`

The document mentions `trusted-users` in the context of daemon protocol auth. The NixOS module source confirms the security warning: users in `trusted-users` are "essentially equivalent to giving that user root access." This is consistent with the daemon protocol document's framing.
