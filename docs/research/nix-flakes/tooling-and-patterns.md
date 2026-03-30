# Nix Flakes: Tooling and Patterns

Covers: `nix eval` for CI automation, nixpkgs overlay fixpoint mechanics, `pkgs.writeShellApplication` for `apps` outputs, NixOS module exports from flakes, remote builders with flakes, and `nix repl` for debugging.

---

## Topic 1: `nix eval` for CI Automation

### What `nix eval` Does vs `nix build`

`nix eval` evaluates a Nix expression and prints the result to stdout. It **does not build derivations** by default — it only runs the Nix evaluator. `nix build` evaluates the expression and then realises (builds) the resulting derivation.

Key distinction: `nix eval` can access `.drvPath` (the store path to the `.drv` file) without building the package. The `.drv` file itself is produced during evaluation (instantiation), not during building.

### Output Format Flags

```bash
# --raw: print string without quotes or escaping (use for shell capture)
nix eval --raw .#packages.x86_64-linux.default.version
# Output: 1.2.3   (no quotes, no newline)

# --json: print value as JSON (use for structured data)
nix eval --json .#packages.x86_64-linux.default.meta
# Output: {"description":"...","license":{...}}

# Default (no flag): Nix pretty-print with quotes and types visible
nix eval .#foo
# Output: "bar"
```

The `--raw` flag only works when the expression evaluates to a string. Applying it to a derivation or attrset aborts.

### Getting the Store Path of a Derivation Without Building

There are two complementary approaches:

**1. `.drvPath` — get the `.drv` instantiation path**

```bash
# Get the .drv file path (the build plan, not the output)
nix eval --raw .#packages.x86_64-linux.default.drvPath
# Output: /nix/store/xxxxxxxx-bmci-1.2.3.drv

# Equivalent using nix derivation show
nix derivation show .#packages.x86_64-linux.default
# Outputs full JSON of the build plan including all inputs/outputs
```

**2. `.outPath` — get the output store path (before building)**

```bash
nix eval --raw .#packages.x86_64-linux.default.outPath
# Output: /nix/store/xxxxxxxx-bmci-1.2.3
# Note: this path may not exist yet if the derivation hasn't been built
```

**3. Using `nix path-info` (requires paths to already exist)**

`nix path-info` is for querying *already-built* store paths. It does NOT build or substitute installables — it queries existing store state. Use `nix eval .drvPath` or `nix build --dry-run` instead when the build hasn't happened yet.

```bash
# Only works for already-built paths
nix path-info nixpkgs#hello
nix path-info --json nixpkgs#hello   # includes closure size, signatures, etc.
nix path-info --derivation .#default  # show .drv path for already-built output
```

### Extracting Values: Complete Pattern Reference

```bash
# Extract a string attribute from a package
nix eval --raw .#packages.x86_64-linux.default.version

# Extract package name
nix eval --raw .#packages.x86_64-linux.default.name

# Evaluate an inline expression against a flake attribute
nix eval --apply 'x: x.version' .#packages.x86_64-linux.default

# Extract nested JSON for CI consumption
nix eval --json .#packages.x86_64-linux.default.meta

# List all available packages (returns JSON array of names)
nix eval --apply builtins.attrNames --json .#packages.x86_64-linux

# Get the devShell derivation path without building it
nix eval --raw .#devShells.x86_64-linux.default.drvPath
```

### Checking If a Flake Attribute Exists

`nix eval` exits non-zero if an attribute does not exist. The error output goes to stderr; this is usable in CI:

```bash
# Shell-level existence check (exit code based)
if nix eval .#packages.x86_64-linux.myPkg > /dev/null 2>&1; then
  echo "attribute exists"
fi

# Within Nix code: use builtins.hasAttr
# In an --apply expression:
nix eval --json --apply 'outputs: builtins.hasAttr "myPkg" outputs.packages.x86_64-linux' .#

# builtins.tryEval catches throw/assert errors but NOT missing attribute errors
# (missing attr generates an abort-level error that tryEval cannot catch)
# Correct pattern: use builtins.hasAttr before attempting attribute access
```

**Critical limitation**: `builtins.tryEval` only catches errors from `throw` and `assert`. Attribute-access errors (missing attr) are `abort`-level and are NOT caught by `tryEval`. Always use `builtins.hasAttr` for existence checks.

### `--impure` vs Pure Evaluation

By default, flakes evaluate in **pure mode**: no access to environment variables, no `builtins.currentTime`, no `builtins.getEnv`, no mutable paths outside the Nix store. This is enforced at the Nix evaluator level.

```bash
# Pure (default for flakes): hermetic, reproducible
nix eval .#packages.x86_64-linux.default.version

# Impure: allows builtins.getEnv, builtins.currentTime, builtins.currentSystem
# Required for: reading env vars into Nix, using non-flake paths, legacy --file usage
nix eval --impure --expr 'builtins.getEnv "HOME"'

# --file always implies --impure
nix eval --file ./mypkgs hello.name
```

In CI, never use `--impure` unless explicitly needed — it breaks reproducibility and the eval result won't be cached.

### `--apply` Flag

`--apply expr` transforms the evaluated value before printing:

```bash
# Get attribute names of all packages
nix eval --apply builtins.attrNames --json .#packages.x86_64-linux

# Map over a list
nix eval --apply 'pkgs: map (p: p.name) (builtins.attrValues pkgs)' --json .#packages.x86_64-linux

# Extract only specific fields from a derivation
nix eval --apply 'drv: { inherit (drv) name version; }' --json .#packages.x86_64-linux.default
```

### `--write-to` for Generating Files

```bash
# Write string output directly to a file
nix eval --raw --write-to /tmp/version .#packages.x86_64-linux.default.version

# Write an attrset of strings to a directory tree
nix eval --write-to /tmp/config .#some-attrset-of-strings
```

### CI Pattern: Version Pinning Script

```bash
#!/usr/bin/env bash
VERSION=$(nix eval --raw .#packages.x86_64-linux.default.version)
DRV_PATH=$(nix eval --raw .#packages.x86_64-linux.default.drvPath)
echo "Building version ${VERSION} from ${DRV_PATH}"
nix build .#packages.x86_64-linux.default
```

### `nix-eval-jobs`: Parallel Evaluation for Large Flakes

For CI systems evaluating many derivations (e.g., full nixpkgs), `nix-eval-jobs` (`github:nix-community/nix-eval-jobs`) streams JSON derivation records as they evaluate. Each record includes the `.drv` path, allowing dynamic build step creation. It also creates GC roots for each evaluated `.drv`, preventing race conditions with `nix-collect-garbage` during parallel builds.

Sources:
- [nix eval reference manual](https://releases.nixos.org/nix/nix-2.13.5/manual/command-ref/new-cli/nix3-eval.html)
- [nix path-info reference](https://nix.dev/manual/nix/2.18/command-ref/new-cli/nix3-path-info)
- [nix-eval-jobs](https://github.com/nix-community/nix-eval-jobs)
- [NixOS Discourse: find derivation path for a flake](https://discourse.nixos.org/t/find-the-derivation-path-for-a-flake/27572)

---

## Topic 2: Nixpkgs Overlays — Fixpoint Mechanics

### The Fixpoint Pattern (`lib.fix`)

From `lib/fixed-points.nix` in nixpkgs:

```nix
fix = f:
  let x = f x;
  in x;
```

This computes the fixed point of `f`: a value `x` such that `x = f x`. It works because Nix is lazily evaluated — `x` is a thunk that forces `f x` only when attributes are accessed, allowing the self-referential definition to succeed without infinite loop (as long as accessing any attribute doesn't require all attributes to be computed simultaneously).

The entire nixpkgs package set is a fixpoint: `pkgs = fix (self: { stdenv = ...; hello = self.callPackage ./hello {}; ... })`.

### How `lib.extends` Composes an Overlay

```nix
extends = overlay: f:
  (final:
    let prev = f final;
    in prev // overlay final prev
  );
```

`extends overlay f` produces a new fixpoint function. When the fixpoint is computed:
1. `f final` evaluates the original package set (lazily)
2. `overlay final prev` evaluates the overlay, receiving both the final result and the pre-overlay packages
3. The `//` operator merges them, with overlay attributes winning

The overlay receives:
- `final` — the complete package set AFTER all overlays are applied (use for self-referential deps)
- `prev` — the package set BEFORE this particular overlay is applied (use for overriding existing pkgs)

### `lib.composeExtensions` and `lib.composeManyExtensions`

```nix
composeExtensions = f: g: final: prev:
  let fApplied = f final prev;
      prev' = prev // fApplied;
  in fApplied // g final prev';
```

Compose two overlays: `f` is applied first, then `g` sees `prev` merged with `f`'s changes (`prev'`). This means each overlay in the chain sees all previous overlays' additions in its `prev`.

```nix
composeManyExtensions = lib.foldr (x: y: composeExtensions x y) (final: prev: {});
```

Right-fold composition — the rightmost overlay in the list is applied first (closest to base nixpkgs). Used in nixpkgs itself to compose `nixpkgs.overlays`.

### `final` vs `prev`: The Critical Rule

**Use `prev` when overriding an existing attribute by the same name:**
```nix
# CORRECT: overriding hello
final: prev: {
  hello = prev.hello.overrideAttrs (old: { version = "2.12"; });
}

# INFINITE RECURSION: final.hello references itself
final: prev: {
  hello = final.hello.overrideAttrs (old: { version = "2.12"; });
  #       ^^^^^^^^^^^ tries to access final.hello which is this very definition
}
```

**Use `final` when referencing OTHER packages that may themselves be overridden:**
```nix
# CORRECT: pkgB depends on pkgA; use final.pkgA so downstream overlays to pkgA are visible
final: prev: {
  pkgB = prev.callPackage ./b.nix { dependency = final.pkgA; };
}

# INCORRECT: prev.pkgA ignores any later overlay overriding pkgA
final: prev: {
  pkgB = prev.callPackage ./b.nix { dependency = prev.pkgA; };
}
```

### `pkgs.extend` vs `nixpkgs.overlays` (NixOS option)

**`pkgs.extend`** — imperative, one-shot, creates a new pkgs instance:
```nix
let pkgs' = pkgs.extend (final: prev: { foo = ...; });
in pkgs'.foo
```

**`nixpkgs.overlays`** (NixOS module option) — declarative, composable, applied at the nixpkgs instantiation:
```nix
nixpkgs.overlays = [
  (final: prev: { myTool = prev.callPackage ./my-tool.nix {}; })
  inputs.someFlake.overlays.default
];
```

**`nixpkgs.config.packageOverrides`** — legacy mechanism; equivalent to a single overlay with only `prev` available (no `final`). Cannot reference other overrides. Avoid in modern Nix.

### `makeScope` and the Scope Pattern

```nix
makeScope = newScope: f:
  let self = {
    callPackage = self.newScope {};
  } // f self // {
    newScope = scope: newScope (self // scope);
    overrideScope = g: makeScope newScope (extends g f);
    packages = f;
  };
  in self;
```

Scopes (e.g., `pkgs.python3Packages`, `pkgs.haskellPackages`, `pkgs.gnome`) are fixpoints built with `makeScope`. To overlay a scope:
```nix
# Correct: use overrideScope
pythonPackages = prev.python3.pkgs.overrideScope (pyFinal: pyPrev: {
  mylib = pyPrev.callPackage ./mylib.nix {};
});

# Also correct: pythonPackagesExtensions for all Python versions
pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
  (pyFinal: pyPrev: { mylib = pyPrev.callPackage ./mylib.nix {}; })
];
```

### The Python Overlay Scoping Trap

**The non-obvious problem**: `pkgs.python3Packages.callPackage` inside a top-level overlay will NOT pick up your Python overlay:

```nix
# WRONG: python3Packages here is the original scope, not your overridden one
final: prev: {
  myPythonEnv = prev.python3.withPackages (ps: [ ps.mylib ]);
  # ps.mylib doesn't exist yet because this overlay hasn't been applied to the Python scope
}
```

**Correct pattern**: use `overrideScope` to target the Python sub-scope:
```nix
final: prev: {
  python3 = prev.python3.override {
    packageOverrides = pyFinal: pyPrev: {
      mylib = pyPrev.callPackage ./mylib.nix {};
    };
  };
}
```

Or use `pythonPackagesExtensions` which automatically propagates to all Python versions.

### Nested Attribute Merging (the `//` shallow-merge trap)

The `//` operator is shallow:
```nix
{ a = { b = 1; }; } // { a = { c = 2; }; }
# = { a = { c = 2; }; }   -- b is LOST
```

For `lib` extensions, use `lib.extend`:
```nix
lib = prev.lib.extend (libFinal: libPrev: {
  myFunc = x: x + 1;
});
# NOT: lib = prev.lib // { myFunc = x: x + 1; };  -- safe only for flat additions
```

For `gnome`/other scopes, use `overrideScope`:
```nix
gnome = prev.gnome.overrideScope (gnomeFinal: gnomePrev: {
  gnome-terminal = gnomePrev.gnome-terminal.overrideAttrs ...;
});
```

### The `prev.pkgs.lib` Infinite Recursion Trap

Since every nixpkgs instance is self-referential (`pkgs.pkgs === pkgs`), using `prev.pkgs.lib` instead of `prev.lib` introduces an extra fixpoint indirection that causes infinite recursion:

```nix
# WRONG: prev.pkgs.lib traverses through pkgs which re-enters the overlay chain
final: prev: { lib = prev.pkgs.lib // { myFunc = ...; }; }

# CORRECT: access lib directly
final: prev: { lib = prev.lib.extend (_: _: { myFunc = ...; }); }
```

Sources:
- `lib/fixed-points.nix` in nixpkgs (fetched directly)
- `lib/customisation.nix` in nixpkgs (fetched directly)
- [Mastering Nixpkgs Overlays — nixcademy.com](https://nixcademy.com/posts/mastering-nixpkgs-overlays-techniques-and-best-practice/)
- [Nix Overlays — nixos.wiki](https://nixos.wiki/wiki/Overlays)
- [Nix overlays: add attribute to "lib" — phip1611.de](https://phip1611.de/blog/nix-overlays-add-attribute-to-lib-and-avoid-infinite-recursion-error/)

---

## Topic 3: `pkgs.writeShellApplication` for `apps` Flake Output

### `writeShellApplication` vs `writeShellScript`

Both live in `pkgs/build-support/trivial-builders/default.nix`.

**`writeShellScript`** — minimal wrapper:
```nix
writeShellScript name text:
  writeTextFile {
    inherit name text;
    executable = true;
    text = ''
      #!${runtimeShell}
      ${text}
    '';
    checkPhase = ''
      ${stdenv.shellDryRun} "$target"   # bash -n syntax check only
    '';
  }
```

Produces: `/nix/store/<hash>-<name>` (a single executable file, NOT in a `bin/` subdirectory).

**`writeShellApplication`** — production-grade wrapper with strict mode, PATH management, and shellcheck:

```nix
writeShellApplication {
  name,
  text,
  runtimeInputs ? [],          # packages to add to PATH
  runtimeEnv ? null,           # attrset of env vars to export
  bashOptions ? ["errexit" "nounset" "pipefail"],  # set -o options
  checkPhase ? null,           # override default shellcheck phase
  excludeShellChecks ? [],     # shellcheck codes to suppress (e.g. "SC2016")
  extraShellCheckFlags ? [],   # extra flags passed to shellcheck
  meta ? {},
  passthru ? {},
  derivationArgs ? {},
  inheritPath ? true,          # whether script inherits $PATH from caller
}
```

Produces: `/nix/store/<hash>-<name>/bin/<name>` (a `bin/` directory, compatible with `apps` output `program` field).

### What `runtimeInputs` Does Exactly

`runtimeInputs` runs `lib.makeBinPath` on the list of derivations, producing a colon-separated PATH string. This is prepended to the shebang section of the generated script:

```bash
#!/usr/bin/env bash
# (generated preamble)
export PATH="/nix/store/<hash>-jq-1.7/bin:/nix/store/<hash>-curl-8.x/bin:${PATH}"
set -o errexit
set -o nounset
set -o pipefail

# (your script text)
```

With `inheritPath = false`, `${PATH}` is omitted — the script runs in a completely isolated PATH containing only the declared `runtimeInputs`.

### The `checkPhase` and ShellCheck Integration

Default `checkPhase` (when not overridden) runs two checks:
1. `stdenv.shellDryRun "$target"` — `bash -n`, catches syntax errors
2. `shellcheck-minimal` (a stripped-down shellcheck binary in nixpkgs) with any `excludeShellChecks` and `extraShellCheckFlags` applied

If `shellcheck` is unavailable for the platform (e.g., cross-compilation targets), the shellcheck step is silently skipped — the `bash -n` syntax check still runs.

To disable shellcheck entirely for a specific check code:
```nix
excludeShellChecks = [ "SC2016" "SC2155" ];
```

To add shellcheck flags (e.g., target a specific shell dialect):
```nix
extraShellCheckFlags = [ "--shell=bash" "--severity=warning" ];
```

### Complete `apps` Output Pattern

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let pkgs = nixpkgs.legacyPackages.${system};
      in {
        apps.deploy = {
          type = "app";   # MUST be the literal string "app"
          program = "${
            pkgs.writeShellApplication {
              name = "deploy";
              runtimeInputs = [ pkgs.ansible pkgs.openssh ];
              text = ''
                ansible-playbook \
                  -i ansible/inventory/hosts.ini \
                  ansible/playbooks/site.yml "$@"
              '';
            }
          }/bin/deploy";
          # program must be an absolute path to an executable file in the Nix store
        };

        apps.default = self.apps.${system}.deploy;
      }
    );
}
```

Invocation: `nix run .#deploy` or `nix run .` (for default).

### Key Constraint: `program` Must Be an Absolute Store Path

The `program` field in an `apps` object must be a string containing an absolute path. Using derivation interpolation (`"${drv}/bin/name"`) is the standard way — the derivation is built when `nix run` is invoked, and the interpolation resolves to the store path.

`nix flake check` validates that `type = "app"` is present and `program` is a valid store path string. If you put a derivation object directly (not a string path), `nix flake check` fails.

Sources:
- Nixpkgs trivial-builders source (fetched via raw GitHub)
- [noogle.dev/f/pkgs/writeShellApplication](https://noogle.dev/f/pkgs/writeShellApplication)
- [nixos.asia/en/writeShellApplication](https://nixos.asia/en/writeShellApplication)
- [Runnable Flakes — tonyfinn.com](https://tonyfinn.com/blog/nix-from-first-principles-flake-edition/nix-9-runnable-flakes/)
- [writeShellApplication PR #141400 — NixOS/nixpkgs](https://github.com/NixOS/nixpkgs/pull/141400)

---

## Topic 4: NixOS Module Exports from Flakes

### Correct `nixosModules` Export Pattern

```nix
# flake.nix
outputs = { self, nixpkgs, ... }: {
  nixosModules = {
    default = import ./modules/myservice.nix;
    myFeature = import ./modules/feature.nix;
  };
  # Singular form (deprecated but still recognized)
  # nixosModule = import ./modules/myservice.nix;
};
```

The module file itself is a standard NixOS module — a Nix function or attrset:

```nix
# modules/myservice.nix
{ config, pkgs, lib, ... }:
{
  options.services.myservice = {
    enable = lib.mkEnableOption "my service";
    port = lib.mkOption {
      type = lib.types.port;
      default = 8080;
      description = "Port to listen on";
    };
  };
  config = lib.mkIf config.services.myservice.enable {
    systemd.services.myservice = {
      description = "My Service";
      wantedBy = [ "multi-user.target" ];
      serviceConfig.ExecStart = "${pkgs.mypackage}/bin/mypackage";
    };
  };
}
```

### Referencing the Exporting Flake's Own Packages from Within a Module

The challenge: `pkgs` inside a module is the consumer's nixpkgs instance, not the exporting flake's packages. Three patterns:

**Pattern 1: `specialArgs` (recommended for `imports` access)**

Pass `self` via `specialArgs` in the consuming flake's `nixosSystem` call:

```nix
# Consumer's flake.nix
nixosConfigurations.myhost = nixpkgs.lib.nixosSystem {
  specialArgs = { inherit inputs self; };
  modules = [ inputs.myflake.nixosModules.default ];
};
```

Inside the module, `self` is now a module argument:
```nix
{ config, pkgs, lib, self, ... }:
{
  config.environment.systemPackages = [
    # Reference the exporting flake's packages for the current system
    self.packages.${pkgs.stdenv.hostPlatform.system}.myTool
  ];
}
```

**Pattern 2: `_module.args`**

```nix
# Inside a module (cannot be used in imports = [...] due to infinite recursion)
{ config, pkgs, lib, ... }:
{
  _module.args.myPkg = self.packages.${pkgs.stdenv.hostPlatform.system}.myTool;
}
```

**Critical**: `_module.args` values CANNOT be used in `imports = [...]` — this causes infinite recursion because `imports` is resolved before `_module.args`. Use `specialArgs` for anything needed in `imports`.

**Pattern 3: nixpkgs overlay (most ergonomic)**

Export an overlay alongside the module, and have the module apply it:

```nix
# In the consuming flake or in the module itself
nixpkgs.overlays = [ inputs.myflake.overlays.default ];
# Now pkgs.myTool is available everywhere
```

### `pkgs.system` vs `pkgs.stdenv.hostPlatform.system`

When inside a NixOS module, use `pkgs.stdenv.hostPlatform.system` (not `pkgs.system` — that attribute doesn't reliably exist on all nixpkgs instances). This is the standard way to get the current system string for cross-referencing your flake's packages:

```nix
self.packages.${pkgs.stdenv.hostPlatform.system}.myTool
```

### `disabledModules` — Removing Upstream NixOS Modules

```nix
disabledModules = [
  # By relative path from modulesPath (NixOS built-in modules)
  "services/web-apps/immich.nix"

  # By absolute path
  "/path/to/some/module.nix"

  # By unique key (for modules declared with `key = "..."` in their meta)
  { key = "some-module-unique-key"; }
];
```

**Important gotchas**:
- Paths must be exact; no wildcards
- `disabledModules = [ "services/web-apps" ]` will NOT disable all modules in that directory — files are imported individually
- This is commonly used to replace an upstream module with a custom implementation: `disabledModules = ["services/foo.nix"]` + `imports = [./custom-foo.nix]`

### `nixosModules.default` Convention

By convention, `nixosModules.default` is the primary module for a flake that provides a single service. Consumers use:

```nix
modules = [ inputs.myflake.nixosModules.default ];
```

Multiple modules (e.g., a base module + optional feature modules) use named keys:

```nix
modules = [
  inputs.myflake.nixosModules.core
  inputs.myflake.nixosModules.extraFeature
];
```

### `lib.mkOption` Patterns

```nix
options.myService = {
  # Boolean toggle (generates a standard enable option with doc)
  enable = lib.mkEnableOption "my service description";

  # Package option with default from nixpkgs
  package = lib.mkPackageOption pkgs "mypackage" {
    default = [ "mypackage" ];
  };

  # Typed option with default
  port = lib.mkOption {
    type = lib.types.port;    # validated as 1-65535
    default = 8080;
    description = lib.mdDoc "Port the service listens on.";
  };

  # Nullable option
  configFile = lib.mkOption {
    type = lib.types.nullOr lib.types.path;
    default = null;
    description = "Path to config file; null uses defaults.";
  };

  # Attrset of submodules
  vhosts = lib.mkOption {
    type = lib.types.attrsOf (lib.types.submodule {
      options = {
        hostname = lib.mkOption { type = lib.types.str; };
        ssl = lib.mkOption { type = lib.types.bool; default = true; };
      };
    });
    default = {};
  };
};
```

### `extendModules` for Composition

To extend an existing NixOS configuration without modifying it:

```nix
# Extend configuration from another flake
nixosConfigurations.myhost = inputs.upstream.nixosConfigurations.basehost.extendModules {
  modules = [ ./my-extra-module.nix ];
  specialArgs = { myArg = "value"; };
};
```

Sources:
- [NixOS Discourse: nixosModules pass-through](https://discourse.nixos.org/t/how-to-pass-through-nixosmodules-in-flakes/18064)
- [NixOS Discourse: add flake package to system configuration](https://discourse.nixos.org/t/how-to-add-a-flake-package-to-system-configuration/14460)
- [NixOS modules — wiki.nixos.org](https://wiki.nixos.org/wiki/NixOS_modules)
- [specialArgs pattern — NixOS & Flakes Book](https://nixos-and-flakes.thiscute.world/nixos-with-flakes/nixos-flake-and-module-system)
- [Extending NixOS configurations — determinate.systems](https://determinate.systems/blog/extending-nixos-configurations/)

---

## Topic 5: Remote Builders with Flakes

### Fundamental Principle: Evaluation Is Always Local

With flakes, `nix eval` and the derivation graph construction always happen on the **local machine**. Only the actual building of derivations is sent to remote builders. This means:

1. `flake.nix` is evaluated locally (pure, hermetic)
2. The resulting `.drv` files are instantiated locally
3. The `.drv` files (and their input sources) are copied to the remote builder
4. The remote builder runs the build and returns the output paths
5. Outputs are copied back to the local store

### `/etc/nix/machines` File Format

Eight space-separated fields per line:

```
ssh-ng://user@hostname  system  /path/to/ssh/key  maxJobs  speedFactor  supportedFeatures  mandatoryFeatures  hostPublicKey
```

| Field | Example | Description |
|-------|---------|-------------|
| Store URI | `ssh-ng://builder@192.168.1.10` | Protocol + user + host. `ssh-ng://` uses full Nix daemon protocol (preferred); `ssh://` uses legacy ServeProto |
| System(s) | `x86_64-linux` or `x86_64-linux,aarch64-linux` | Comma-separated; use `aarch64-linux` for Apple Silicon cross-builds |
| SSH key | `/root/.ssh/builder_ed25519` | Private key path; must be passphrase-free for daemon use |
| Max jobs | `4` | Concurrent builds; `-` or `0` = unlimited |
| Speed factor | `1` | Relative speed; Nix schedules more jobs to faster machines |
| Supported features | `nixos-test,big-parallel,kvm` | Must match `requiredSystemFeatures` on derivations |
| Mandatory features | `-` | Features that MUST appear in derivation; `-` = none |
| Host public key | `base64(ed25519 pubkey)` | Prevents MITM; obtain with `base64 -w0 /etc/ssh/ssh_host_ed25519_key.pub` |

Example complete entry:
```
ssh-ng://remotebuild@builder.internal x86_64-linux /root/.ssh/builder_key 8 1 nixos-test,big-parallel,kvm - AAAA...base64pubkey...
```

### `nix.conf` Configuration

```ini
# Reference machines file (this is the default if file exists)
builders = @/etc/nix/machines

# Or inline (semicolon-separated)
builders = ssh-ng://user@host1 x86_64-linux /root/.ssh/key 4 ; ssh-ng://user@host2 aarch64-linux /root/.ssh/key 4

# Allow remote builders to fetch dependencies from binary caches
# instead of receiving them from the local machine
builders-use-substitutes = true

# Force ALL builds to remote (0 local jobs)
max-jobs = 0
```

### NixOS `nix.buildMachines` Option

The structured NixOS module approach (preferred over raw `/etc/nix/machines`):

```nix
nix.distributedBuilds = true;
nix.settings.max-jobs = 0;          # force remote, no local builds
nix.extraOptions = ''builders-use-substitutes = true'';

nix.buildMachines = [
  {
    hostName = "builder.internal";
    sshUser = "remotebuild";
    sshKey = "/root/.ssh/builder_key";
    # Use current system string dynamically
    system = pkgs.stdenv.hostPlatform.system;
    # Or multiple systems: systems = ["x86_64-linux" "aarch64-linux"];
    supportedFeatures = [ "nixos-test" "big-parallel" "kvm" ];
    maxJobs = 8;
    speedFactor = 2;
    publicHostKey = "c3NoLWVkMjU1MTkgQUFBQ...base64...";
  }
];
```

### SSH Key Setup for the Nix Daemon

The Nix daemon runs as `root`. The key must be accessible by root without passphrase:

```bash
# On the LOCAL machine (as root)
ssh-keygen -t ed25519 -f /root/.ssh/builder_key -N ""

# On the REMOTE builder, add to authorized_keys
# /etc/ssh/authorized_keys.d/remotebuild  (or ~/.ssh/authorized_keys)
ssh-ed25519 AAAA... (pubkey content)

# On the REMOTE builder's nix.conf, mark the user as trusted
# /etc/nix/nix.conf
trusted-users = remotebuild

# Test connectivity (as root on local)
ssh -i /root/.ssh/builder_key remotebuild@builder.internal "nix --version"
```

In NixOS, configure via:
```nix
# On remote builder NixOS config
nix.settings.trusted-users = [ "remotebuild" ];
users.users.remotebuild = { isSystemUser = true; group = "remotebuild"; };
users.groups.remotebuild = {};
```

### `builders-use-substitutes` Behavior

When `false` (default): local machine sends all required inputs to the remote builder over SSH. Slow if the remote builder needs many dependencies.

When `true`: remote builder fetches its own build inputs from binary caches (e.g., `cache.nixos.org`) directly. Faster when remote builder has good internet. **Requires**: the remote builder must have binary cache access configured in its own `nix.conf`.

### `--max-jobs 0` vs `nix.settings.max-jobs = 0`

```bash
# CLI: force all builds remote for a single command
nix build --max-jobs 0 .#default

# NixOS: permanent setting (forces all nixos-rebuild to use remote)
nix.settings.max-jobs = 0;
```

Setting `max-jobs = 0` means the local machine will not run any build — it becomes a pure build coordinator. If all remote builders are unavailable, the build fails (does not fall back to local).

### `--builders` CLI Flag and Trust Restriction

```bash
# Specify builder inline (requires trusted-user status)
nix build --builders "ssh-ng://user@host x86_64-linux" .#default

# This fails if the calling user is not in trusted-users:
# "ignoring the client-specified setting 'builders', because it is a restricted setting"
```

The `--builders` CLI flag is a restricted setting. Only users in `trusted-users` can set it. Workaround: add user to `trusted-users` OR configure builders in `/etc/nix/nix.conf` system-wide.

### Flake Evaluation Stays Local: Practical Implication

Because evaluation is local, `nix eval` commands always run on the local machine regardless of remote builder configuration. This means:

```bash
# These always run locally, even with max-jobs=0 and remote builders configured
nix eval .#packages.x86_64-linux.default.version
nix flake check --no-build
nix flake show

# Only the actual build phase goes remote
nix build .#default   # evaluation local, build remote
```

Sources:
- [Setting up distributed builds — nix.dev](https://nix.dev/tutorials/nixos/distributed-builds-setup.html)
- [Remote Builds — nix.dev manual 2.34](https://nix.dev/manual/nix/2.34/advanced-topics/distributed-builds)
- [Distributed Building — NixOS & Flakes Book](https://nixos-and-flakes.thiscute.world/development/distributed-building)
- [The wonders of Nix remote builders — heitorpb.github.io](https://heitorpb.github.io/bla/wonders-of-nix-remote-builders/)
- [Remote Builds TIL — fnordig.de](https://fnordig.de/til/nix/remote-builds.html)

---

## Topic 6: `nix repl` for Debugging Flakes

### Starting the REPL with a Flake

```bash
# Load the current directory's flake
nix repl
nix-repl> :lf .

# Equivalent: load on startup
nix repl --expr 'import ./.'

# Load a specific flake by URL
nix repl
nix-repl> :lf github:NixOS/nixpkgs/nixos-unstable
nix-repl> :lf nixpkgs    # using flake registry

# For older Nix (< 2.19.2): requires repl-flake experimental feature
nix --extra-experimental-features repl-flake repl
```

After `:lf .`, all flake outputs are directly in scope:
```
nix-repl> outputs.packages.x86_64-linux.default.name
"bmci-1.0.0"
nix-repl> inputs.nixpkgs.rev
"abc123..."
```

### Complete Special Command Reference

| Command | Description |
|---------|-------------|
| `:?` | Show all available special commands |
| `:l <file>` | Load Nix expression from file into scope |
| `:lf <ref>` | Load Nix flake (by URL or path) into scope; adds `outputs`, `inputs`, `sourceInfo` |
| `:a <expr>` | Add all attributes from result set into current scope |
| `:r` | Reload all files (re-evaluates after changes; use after editing flake.nix) |
| `:b <expr>` | Build a derivation; prints output store path |
| `:bl <expr>` | Build a derivation; creates GC roots in current working directory |
| `:i <expr>` | Build derivation and install into current Nix profile |
| `:sh <expr>` | Build derivation's dependencies, then start `nix-shell` with those deps |
| `:u <expr>` | Build derivation itself, then start `nix-shell` with it |
| `:e <expr>` | Open a file/function in `$EDITOR` for inspection |
| `:p <expr>` | Evaluate and pretty-print **recursively** (forces all thunks; use carefully on large sets) |
| `:t <expr>` | Describe the type of the result (int, string, lambda, set, etc.) |
| `:doc <expr>` | Show documentation for a builtin function (e.g., `:doc builtins.mapAttrs`) |
| `:log <expr>` | Show build log for a derivation (must have already been built) |
| `:te [bool]` | Enable, disable, or toggle trace output for evaluation errors |
| `:q` | Exit the REPL |

### `:lf .` Behavior in Detail

`:lf <ref>` performs `builtins.getFlake` on the reference, then:
- Adds `inputs` (the flake inputs attrset) to scope
- Adds `outputs` (the flake outputs attrset) to scope
- Adds `sourceInfo` (source metadata: `rev`, `revCount`, `lastModified`, etc.) to scope

After `:lf .` in the forge-metal flake:
```
nix-repl> outputs.packages.<TAB>       # tab-complete system names
nix-repl> outputs.packages.x86_64-linux.default.drvPath
"/nix/store/abc-bmci-1.0.0.drv"
nix-repl> inputs.nixpkgs.legacyPackages.x86_64-linux.hello.version
"2.12.1"
```

### `:b` vs `:bl` — GC Root Difference

```
nix-repl> :b outputs.packages.x86_64-linux.default
# Builds the package; output path printed
# No GC root created — nix-collect-garbage can delete it

nix-repl> :bl outputs.packages.x86_64-linux.default
# Builds the package; creates a GC root symlink in the current directory
# e.g.: ./result -> /nix/store/hash-bmci-1.0.0
# Safe from garbage collection until symlink is removed
```

### Inspecting a Derivation's Environment in the REPL

Derivations are attribute sets with all their environment variables exposed:

```
nix-repl> :lf .
nix-repl> d = outputs.packages.x86_64-linux.default
nix-repl> d.buildInputs         # list of build-time dependencies
nix-repl> d.nativeBuildInputs   # list of host-built tools
nix-repl> d.src                 # source derivation
nix-repl> d.src.outPath         # source store path
nix-repl> d.drvPath             # the .drv file path
nix-repl> d.outPath             # expected output path
nix-repl> builtins.readFile d.drvPath   # read the .drv file contents
```

To see ALL attributes of a derivation (large output):
```
nix-repl> :p d    # WARNING: forces evaluation of all lazy attrs
```

### Reloading After Changes (No `--watch`)

There is no `--watch` flag in `nix repl` (as of Nix 2.34). The workflow is:

```
# 1. Edit flake.nix in your editor
# 2. In the repl:
nix-repl> :r    # reload all files
nix-repl> :lf . # re-load the flake (if not already in auto-scope)
```

Alternatively, restart the repl entirely:
```bash
nix repl --file default.nix   # traditional nix expressions
# After changes: Ctrl-D, then re-run
```

### `builtins.trace` for Debugging Evaluation

`builtins.trace` evaluates its first argument, prints it to **stderr**, and returns its second argument. It is the primary debugging tool for tracing evaluation order:

```nix
# In your flake.nix or a .nix file:
myPkg = builtins.trace "evaluating myPkg" (
  pkgs.callPackage ./my-pkg.nix {}
);

# With value inspection:
value = let
  x = computeSomething;
in builtins.trace "x = ${builtins.toJSON x}" x;
```

In `nix repl`, traces print to stderr inline:
```
nix-repl> outputs.packages.x86_64-linux.traceExample
trace: evaluating myPkg
«derivation /nix/store/...»
```

Enable/disable trace output in the repl:
```
nix-repl> :te true    # enable traces
nix-repl> :te false   # disable traces
nix-repl> :te         # toggle current state
```

### `--debugger` — Interactive Error Debugging

```bash
nix repl --debugger
# OR
nix eval --debugger .#packages.x86_64-linux.default
```

When evaluation hits an error (including `throw`, `assert`, type errors), `--debugger` drops into an interactive sub-REPL instead of aborting. Inside:
- All in-scope variables at the error site are accessible
- `:env` shows the current environment
- Standard REPL commands work
- `:q` exits the debugger and continues (or aborts)

### Tracing Without Building: Evaluating Large Flakes

To trace evaluation of a large flake without triggering any builds:

```bash
# Show all flake outputs (eval only, no build)
nix flake show .

# Evaluate a specific attribute to check it type-checks
nix eval --json .#packages.x86_64-linux.default.meta

# Check evaluation of all outputs without building anything
nix flake check --no-build

# Use --read-only to skip derivation instantiation (faster, may skip some attrs)
nix eval --read-only .#packages.x86_64-linux.default.version
```

### `:doc` for Builtin Documentation

```
nix-repl> :doc builtins.mapAttrs
# Outputs:
# Synopsis: mapAttrs f attrset
# Apply function f to every element in attrset...

nix-repl> :doc builtins.fetchTarball
nix-repl> :doc builtins.tryEval
```

`:doc` only works for builtin functions, not nixpkgs library functions. For `lib` functions, use `:e pkgs.lib.mapAttrs` to open the source in `$EDITOR`.

### The `repl` Flake Output Pattern

Some flakes export a `repl` output for convenient inspection:

```nix
# In flake.nix
outputs = { self, nixpkgs, ... }:
  let pkgs = nixpkgs.legacyPackages.x86_64-linux;
  in {
    # ...
    # Custom repl entry that pre-imports useful things
    repl = {
      inherit (pkgs) lib;
      inherit pkgs;
      flake = self;
    };
  };
```

Then: `nix repl .#repl` (or `:a outputs.repl` after `:lf .`) drops you directly into a scope with `lib`, `pkgs`, and `flake` available.

Sources:
- [nix repl reference — nix.dev manual 2.34](https://nix.dev/manual/nix/2.34/command-ref/new-cli/nix3-repl.html)
- [Debugging Derivations — NixOS & Flakes Book](https://nixos-and-flakes.thiscute.world/best-practices/debugging)
- [nix repl tips — tsawyer87.github.io](https://tsawyer87.github.io/posts/nix_repl_tips/)
- [a quick nix repl tutorial — gist.github.com/crabdancing](https://gist.github.com/crabdancing/fd14d7e4fb209e13012d071046fbed37)
- [nix flakes repl — blog.rjpc.net](https://blog.rjpc.net/posts/2023-12-18-nix-flakes-repl.html)
- [Use repl to inspect a flake — NixOS Discourse](https://discourse.nixos.org/t/use-repl-to-inspect-a-flake/28275)
