# Nix Flakes: Outputs Schema, CI, `nix develop`, and Tooling

Covers: complete flake output schema, `nix flake check` internals, `nix develop` mechanics, GitHub Actions CI, `nix profile`, and the `nix-systems` flake.

---

## Complete Flake Outputs Schema

### System-Scoped Outputs (must be namespaced by architecture)

| Output | Required type | CLI consumer |
|--------|--------------|-------------|
| `packages.<system>.<name>` | derivation | `nix build .#<name>` |
| `packages.<system>.default` | derivation | `nix build .` |
| `apps.<system>.<name>` | app object | `nix run .#<name>` |
| `apps.<system>.default` | app object | `nix run .` |
| `checks.<system>.<name>` | derivation | `nix flake check` (built) |
| `devShells.<system>.<name>` | derivation | `nix develop .#<name>` |
| `devShells.<system>.default` | derivation | `nix develop` |
| `formatter.<system>` | derivation | `nix fmt` |
| `legacyPackages.<system>` | attrset (opaque) | `nix build .#<name>`, `nix shell` |

### Global Outputs (no system scoping)

| Output | Required type | Consumer |
|--------|--------------|---------|
| `overlays.<name>` / `overlays.default` | `final: prev: {}` | downstream `nixpkgs.overlays` |
| `nixosModules.<name>` / `nixosModules.default` | module | `nixosSystem { modules = [...]; }` |
| `nixosConfigurations.<hostname>` | NixOS config | `nixos-rebuild switch --flake .#<hostname>` |
| `darwinConfigurations.<hostname>` | nix-darwin config | `darwin-rebuild switch --flake .#<hostname>` |
| `hydraJobs.<attr>.<system>` | derivation (nested) | Hydra CI server |
| `templates.<name>` / `templates.default` | template object | `nix flake init -t .#<name>` |
| `lib.<name>` | arbitrary value | `inputs.mypkg.lib.<name>` |

Backward-compatibility single-value variants (`devShell.<system>`, `overlay`, `nixosModule`) are still recognized but deprecated.

### Object Type Schemas

**App object** — `type` must be the literal string `"app"`:
```nix
apps.x86_64-linux.deploy = {
  type = "app";
  program = "${pkgs.writeShellScript "deploy" ''${pkgs.ansible}/bin/ansible-playbook ...''}";
};
```
`program` must be an absolute path to a file in the Nix store. `nix run` resolves fallback order: `apps.<system>.<name>` → `packages.<system>.<name>` (runs `<out>/bin/<meta.mainProgram or name>`) → `legacyPackages.<system>.<name>`.

**Template object**:
```nix
templates.go-service = {
  path = ./templates/go-service;
  description = "A minimal Go service with Nix devShell";
};
```
`nix flake init -t .#go-service` copies `path` into the current directory.

**Overlay function**: must use `prev` (not `pkgs`) to avoid infinite recursion; use `final` for self-referential overrides:
```nix
overlays.default = final: prev: {
  myTool = prev.myTool.overrideAttrs (_: { version = "2.0"; });
};
```

### `checks` vs `packages`

| | `packages` | `checks` |
|--|-----------|---------|
| Purpose | Build artifacts | Validation gates |
| `nix flake check` | Validates type only | **Always built** |
| Individual run | `nix build .#packages.x86_64-linux.foo` | `nix build .#checks.x86_64-linux.foo` |
| Output value | Used by consumer | Ignored — only exit code matters |

### `formatter` and `nix fmt`

`nix fmt` reads `formatter.<system>`, executes it, and forwards path arguments. The formatter is not limited to Nix — `treefmt` is the standard multi-language choice:

```nix
formatter.x86_64-linux = pkgs.treefmt;
# or for Nix-only
formatter.x86_64-linux = pkgs.nixfmt-rfc-style;
```

### DeterminateSystems Flake Schemas

`github:DeterminateSystems/flake-schemas` formalizes 17+ output types including `homeConfigurations`, `ociImages`, `bundlers`, `schemas`. Adding this as a flake input enables `nix flake show` to display richer type information and run schema-aware validation:

```nix
outputs = { self, flake-schemas, ... }: {
  schemas = flake-schemas.schemas;
  # ... rest of outputs
};
```

---

## `nix flake check` Deep Dive

### What Gets Validated

| Output type | Validation |
|------------|-----------|
| `packages`, `devShells`, `checks` | Must be derivations |
| `apps` | Must conform to app schema (`type = "app"`, `program` = store path) |
| `nixosConfigurations.<name>` | Evaluates `.config.system.build.toplevel`; must be a derivation. Does NOT build the full system unless explicitly included in `checks`. |
| `overlays` | Must be Nixpkgs overlay functions |
| `nixosModules` | Must be NixOS module values |
| `templates` | Must be template definitions |
| `hydraJobs` | Evaluated as nested derivation set (like `hydra-eval-jobs`) |
| `legacyPackages` | Evaluated like `nix-env --query --available` |

Flags:
- `--no-build` — validate evaluation only; skip building `checks` derivations. Fast for catching type errors.
- `--all-systems` — check all system architectures, not just current.
- `--keep-going` — report all failures rather than stopping at first.

### Writing `checks` Derivations

Every check must produce `$out`. Exit code = pass/fail. Output content is discarded.

**Minimal pattern** (`pkgs.runCommand`):
```nix
checks.x86_64-linux.shellcheck = pkgs.runCommand "shellcheck" {
  nativeBuildInputs = [ pkgs.shellcheck ];
} ''
  shellcheck ${./scripts/deploy.sh}
  mkdir $out
'';
```

**`pkgs.runCommandLocal`** — skips binary cache lookup (prefer for fast local checks):
```nix
checks.x86_64-linux.fmt = pkgs.runCommandLocal "fmt-check" {
  nativeBuildInputs = [ pkgs.nixfmt-rfc-style ];
} ''
  nixfmt --check ${./flake.nix}
  mkdir $out
'';
```

**Go test suite**:
```nix
checks.x86_64-linux.go-tests = pkgs.stdenvNoCC.mkDerivation {
  name = "go-tests";
  src = ./.;
  nativeBuildInputs = [ pkgs.go ];
  dontBuild = true;
  doCheck = true;
  checkPhase = "go test ./...";
  installPhase = "mkdir $out";
};
```

Individual checks can be run independently:
```bash
nix build .#checks.x86_64-linux.shellcheck
```

`nix repl` inspection:
```
nix repl
:lf .
outputs.checks.x86_64-linux.<TAB>   # tab-complete available checks
```

---

## `nix develop` Internals

### How the devShell Environment Is Built

`nix develop` does not run a full `nix build`. It builds a modified derivation that runs only the `env-setup` phase (stdenv's initialization), captures the resulting environment variables and shell functions, then launches an interactive bash shell with that environment.

The captured environment includes:
- `PATH`: prepended with all `nativeBuildInputs` bin paths
- All `pkgs.mkShell` attrs coercible to strings → exported as env vars
- stdenv phase functions (`buildPhase`, `checkPhase`, etc.) as bash functions
- `TMP`, `TMPDIR`, `TEMPDIR`, `TEMP`
- Build-system variables (`HOST_PATH`, `CONFIG_SHELL`)

### `nix print-dev-env` vs `nix develop`

| | `nix develop` | `nix print-dev-env` |
|--|--------------|---------------------|
| Effect | Launches interactive subshell | Prints bash script to stdout |
| JSON output | No | `--json` flag available |
| Use case | Interactive dev | `direnv`, CI, scripting |
| `shellHook` | Runs automatically | Emitted as `eval "$shellHook"` |

**`--json` output structure**:
```json
{
  "bashFunctions": { "buildPhase": "...", "preBuild": "..." },
  "variables": {
    "src": { "type": "exported", "value": "/nix/store/..." },
    "postUnpackHooks": { "type": "array", "value": [...] }
  }
}
```

**Known bug** ([NixOS/nix#8253](https://github.com/NixOS/nix/issues/8253)): `nix print-dev-env` emits `eval "$shellHook"` even when no `shellHook` is defined. When sourced in bash with `set -u`, this fails with `shellHook: unbound variable`. Workaround: define `shellHook = ""` explicitly.

### `pkgs.mkShell` Mechanics

`mkShell` routes attrs as follows:
- `packages` → `nativeBuildInputs` (executables in PATH)
- `buildInputs` → passed through as-is (for libraries/headers needed by C code in the shell)
- `inputsFrom` → `mergeInputs` collects `buildInputs`, `nativeBuildInputs`, `propagatedBuildInputs`, `propagatedNativeBuildInputs` from each listed derivation; `shellHook` strings from `inputsFrom` are reversed then concatenated, with the current module's `shellHook` appended last (so `inputsFrom` hooks run before the module's own hook; see `drv-tooling-mkshell-modules.md` for full ordering details)
- Unknown attrs coercible to strings → exported as env vars

The `inputsFrom` derivations themselves are excluded from the resulting `buildInputs` to avoid circular build-time references.

### `shellHook` Execution Semantics

| Command | Runs `shellHook`? |
|---------|------------------|
| `nix develop` | Yes |
| `nix-shell` | Yes |
| `nix shell` (no flake) | **No** |
| `nix run` | **No** |

### Environment Customization Flags

```bash
nix develop --ignore-env -k HOME -k TERM   # clean env; keep only specified vars
nix develop --set-env MY_VAR value          # inject a variable
nix develop --unset-env SOME_VAR            # remove a variable
nix develop --phase build                   # run only the build phase and exit
nix develop --configure                     # shorthand for --phase configure
```

### The `inputDerivation` GC Root Pattern

When running `nix develop .`, the built devShell is not kept as a GC root after the session ends — `nix-collect-garbage` removes it. To persist the devShell environment as a GC root (e.g., for CI or `nix-direnv`):

```bash
nix build .#devShells.x86_64-linux.default.inputDerivation
```

This builds and pins the input closure, making the environment available for future `nix develop` invocations without network access.

### Prompt Customization via `nixConfig`

The `bash-prompt`, `bash-prompt-prefix`, and `bash-prompt-suffix` settings can be set in `flake.nix`'s `nixConfig` without user confirmation:

```nix
nixConfig.bash-prompt-prefix = "(forge-metal) ";
```

This is one of the handful of settings whitelisted to apply without the `accept-flake-config` security prompt (see `advanced-topics.md`).

---

## GitHub Actions CI with Flakes

### Minimal Recommended Workflow (2025)

```yaml
on:
  pull_request:
  push:
    branches: [main]

jobs:
  nix-ci:
    runs-on: ubuntu-latest
    permissions:
      id-token: write   # required for FlakeHub cache auth
      contents: read
    steps:
      - uses: actions/checkout@v4
      - uses: DeterminateSystems/determinate-nix-action@v3
      - uses: DeterminateSystems/flakehub-cache-action@main
      - run: nix flake check --no-build  # fast: validate eval only
      - run: nix flake check             # builds all checks.<system>.*
```

Note: `magic-nix-cache-action` is the predecessor; the current recommended cache action is `flakehub-cache-action`.

### `magic-nix-cache-action` / `flakehub-cache-action` Architecture

- Runs a local daemon on `127.0.0.1:37515` (configurable via `listen`)
- Intercepts Nix's binary cache protocol (narinfo + nar requests)
- Backend: **GitHub Actions native cache** (same storage as `actions/cache`)
- Fork PRs: can READ but cannot WRITE to the cache (prevents cache poisoning from external PRs)
- On cache rate limit (HTTP 429): degrades gracefully, continues build, defers uploads
- Paths already in `cache.nixos.org` are not re-cached (no value in duplicating public cache)
- Claimed savings: 30–50%+ CI time reduction

### `determinate-nix-action` Defaults vs Official Nix Installer

`DeterminateSystems/determinate-nix-action@v3` differs from the official Nix installer in several ways:

| Setting | Determinate | Official |
|---------|-------------|---------|
| `experimental-features` | `nix-command flakes` enabled | Disabled by default |
| `auto-optimise-store` | `true` (Linux) | `false` |
| `max-jobs` | `auto` | `1` |
| `extra-nix-path` | `nixpkgs=flake:nixpkgs` | Not set |
| Channels | Not configured | `nix-channel --update` runs |
| KVM | Activated when available | Not managed |
| Receipt | `/nix/receipt.json` | Not stored |

### Automated Lock File Updates

`DeterminateSystems/update-flake-lock` runs `nix flake update` on a schedule and creates a PR:

```yaml
# .github/workflows/update-flake-lock.yml
on:
  schedule:
    - cron: '0 0 * * 0'  # weekly
  workflow_dispatch:

jobs:
  update:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: DeterminateSystems/determinate-nix-action@v3
      - uses: DeterminateSystems/update-flake-lock@vX
        with:
          inputs: "nixpkgs"          # only update nixpkgs; leave others pinned
          sign-commits: false
          pr-title: "flake.lock: update nixpkgs"
```

Selective update: `inputs: "nixpkgs rust-overlay"` runs `nix flake update nixpkgs rust-overlay`.

### `flake-checker-action` (Lock File Health)

`DeterminateSystems/flake-checker-action` validates `flake.lock` without running a build:
- Warns if nixpkgs is older than 30 days
- Verifies root nixpkgs is from the NixOS GitHub org (prevents supply-chain substitution)
- Supports CEL conditions for custom validation
- Reports findings as GitHub Actions step summary (Markdown)
- Exits with code 0 by default (reports-only); set `fail-mode: true` to block PRs

### CI Tip: `--no-update-lock-file` Enforcement

```yaml
- run: nix flake check --no-update-lock-file
```

Throws an error if `flake.lock` is absent or would need updating. Catches accidentally uncommitted lock file changes before they reach production.

---

## `nix profile` with Flakes

### How `nix profile install .#dev-tools` Works

`nix profile install` resolves the flake output, builds it, and symlinks it into the active profile (`~/.nix-profile` by default). Each profile generation is a GC root; the profile symlink chain is:

```
~/.nix-profile
  → /nix/var/nix/profiles/per-user/$USER/profile
    → profile-N-link  (GC root)
      → /nix/store/<hash>-profile/  (buildEnv output)
```

Pinned flake installs:
```bash
nix profile install nixpkgs#ripgrep                    # from registry
nix profile install nixpkgs/nixos-24.11#ripgrep        # pinned branch
nix profile install nixpkgs/d73407e8e6...#ripgrep      # pinned commit
nix profile install .#dev-tools                        # from local flake
nix profile install nixpkgs#bash^man                   # non-default output (man pages)
```

### `manifest.json` Structure (Version 1)

```json
{
  "version": 1,
  "elements": [
    {
      "active": true,
      "attrPath": "legacyPackages.x86_64-linux.ripgrep",
      "originalUrl": "flake:nixpkgs",
      "uri": "github:NixOS/nixpkgs/a3a3dda3bacf61e8a39258a0ed9c924eeca8e293",
      "storePaths": ["/nix/store/...-ripgrep-14.1.0"]
    }
  ]
}
```

- `originalUrl`: the ref as typed; used by `nix profile upgrade` to re-resolve
- `uri`: locked resolved ref (exact commit hash); used to reproduce installs
- `active: false`: package is in manifest but symlinks are removed (soft uninstall)

### Profile Management

```bash
nix profile list              # index, mutable ref, immutable ref, store path
nix profile remove <index>    # remove by index (see nix profile list)
nix profile upgrade <index>   # re-resolve originalUrl and update
nix profile rollback          # revert to previous generation
nix profile rollback --to N   # revert to specific generation
nix profile history           # all versions with timestamps and diffs
nix profile diff-closures     # closure differences between versions
nix profile wipe-history --older-than 30d  # prune old generations
```

### Incompatibility with `nix-env`

Once `nix profile` has written `manifest.json`, `nix-env` refuses all operations on that profile:
> "profile is incompatible with 'nix-env'; please use 'nix profile' instead"

This is intentional — the two formats are incompatible. Delete `manifest.json` to revert to `nix-env` management (losing flake provenance tracking).

---

## The `nix-systems` Flake

### Why It Exists

`flake-utils` hardcodes `defaultSystems = ["x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin"]` in its source. Downstream flakes cannot change this list without forking. Additionally, `flake-utils` has a structural flaw: `eachDefaultSystem` applies per-system scoping to ALL outputs indiscriminately, producing malformed output keys like `overlays.x86_64-linux.default` instead of the correct `overlays.default`. Source: [ayats.org/blog/no-flake-utils](https://ayats.org/blog/no-flake-utils).

The "1000 instances problem": without `follows` coordination, each flake depending on `flake-utils` adds its own locked copy to `flake.lock`, causing redundant downloads and eval overhead. Source: [nixcademy.com/posts/1000-instances-of-flake-utils](https://nixcademy.com/posts/1000-instances-of-flake-utils/).

### What `nix-systems` Provides

Each `nix-systems` sub-flake exports a single Nix list — no functions, no attrs:

| Input URL | Systems |
|-----------|---------|
| `github:nix-systems/default` | `x86_64-linux`, `aarch64-linux`, `x86_64-darwin`, `aarch64-darwin` |
| `github:nix-systems/default-linux` | `x86_64-linux`, `aarch64-linux` |
| `github:nix-systems/default-darwin` | `x86_64-darwin`, `aarch64-darwin` |
| `github:nix-systems/x86_64-linux` | `x86_64-linux` only |

### Usage Without `flake-utils`

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    systems.url = "github:nix-systems/default";
  };

  outputs = { nixpkgs, systems, ... }:
    let
      eachSystem = nixpkgs.lib.genAttrs (import systems);
    in {
      packages   = eachSystem (system: { default = nixpkgs.legacyPackages.${system}.hello; });
      devShells  = eachSystem (system: { default = nixpkgs.legacyPackages.${system}.mkShell {}; });
      overlays.default = final: prev: {};  # NOT wrapped in eachSystem
    };
}
```

### Consumer Override

A downstream flake can change the systems list via `follows`:
```nix
inputs.upstream.inputs.systems.follows = "systems";
inputs.systems.url = "github:nix-systems/x86_64-linux";  # CI: Linux only
```

Or inline without a flake:
```bash
nix flake show --override-input systems github:nix-systems/x86_64-linux
```

### The Pure `nixpkgs.lib.genAttrs` Alternative

For flakes that don't need consumer override, `flake-utils` can be eliminated entirely:

```nix
let
  systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
  forAllSystems = nixpkgs.lib.genAttrs systems;
in {
  packages = forAllSystems (system: { ... });
}
```

`genAttrs :: [string] -> (string -> attrset) -> attrset` — same type as `eachSystem` but without the lock file dependency or malformed-output risk.

**When to use each approach for forge-metal**:
- Current `flake-utils` usage is safe (only applied to `packages` and `devShells`, not `overlays`)
- Migrating to `nixpkgs.lib.genAttrs` with an inline system list removes the `flake-utils` input entirely, simplifying `flake.lock`
- `nix-systems` is useful when forge-metal grows a published flake API that downstream consumers need to customize
