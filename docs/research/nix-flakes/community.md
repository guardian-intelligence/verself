# Nix Flakes: Community, Stabilization, and Alternatives

## Experimental Status: Why It Persists

Flakes have been "experimental" since Nix 2.4 (November 2021). Enabling them requires:
```
experimental-features = nix-command flakes
```
in `nix.conf` or `/etc/nix/nix.conf`.

The experimental flag does not mean "unstable" in practice. Determinate Systems (the company founded by nixpkgs contributors) [argues explicitly](https://determinate.systems/blog/experimental-does-not-mean-unstable/) that flakes have had "remarkably few breaking changes" since their release and that the experimental label should have been removed years ago. Their Nix installer enables flakes by default.

Why the flag persists:
- RFC 49 (the original flakes RFC) was **withdrawn** after the implementation was already merged — a procedural anomaly that raised governance questions. The RFC was not accepted before merge.
- Known design problems remain: no parameter support, cross-compilation limitations, `legacyPackages` hack, registry mutability, `follows` ergonomics
- The NixOS Foundation governance crisis (2024) stalled progress; Eelco Dolstra resigned from the board

RFC 136 (accepted August 2023) established a phased stabilization plan:
1. Phase 1: Stabilize non-flake new CLI commands first (`nix show-derivation`, `nix eval`, etc.)
2. Phase 2: Separate store-layer from evaluation layer (RFC 134)
3. Phase 3: Stabilize flakes and flake-related CLI

As of 2025, Phase 1 is in progress (milestone #27 in NixOS/nix, 47% complete as of last update). No stabilization timeline has been set.

## Community Divergence: Lix and Determinate Nix

The governance crisis produced forks:
- **Lix** (`lix.systems`, `git.lix.systems/lix-project/lix`): Community fork of CppNix 2.18, started in 2024. Focuses on correctness, improved error messages, and community governance (no single commercial backer). Lix has ported additional components to Rust but is not a ground-up rewrite. It treats flakes as de-facto stable but keeps them behind the experimental feature flag for backward compatibility. Installed on NixOS via the `lix-module` flake (`git.lix.systems/lix-project/nixos-lix-module`), which replaces the `nix` package via overlay. Does NOT include lazy trees or FlakeHub integration. See [lang-advanced-determinate-fleet.md](lang-advanced-determinate-fleet.md) for full Lix vs Determinate Nix comparison.
- **Determinate Nix** (`determinate.systems`, `github.com/DeterminateSystems/nix-src`): Commercial downstream fork of CppNix, continuously rebased against upstream. Ships flakes as stable and enabled by default, includes lazy trees (enabled by default since 3.8.0), parallel evaluation, FlakeHub native integration (`fh` CLI, `flakehub:` URL scheme), and FlakeHub Cache binary cache. The Determinate installer uses `receipt.json` for clean uninstall. Also includes `Determinate Nixd` daemon for certificate management and automatic GC. See [lang-advanced-determinate-fleet.md](lang-advanced-determinate-fleet.md) for details.

Both forks treat flakes as stable despite the upstream experimental flag. The key distinction: Determinate Nix adds major new features (lazy trees, parallel eval, FlakeHub); Lix focuses on correctness, better error messages, and community ownership without adding proprietary capabilities.

## Key Design Criticisms

### 1. Flakes Solve Too Many Problems At Once

The jade.fyi analysis ("flakes aren't real") argues flakes couple three separate concerns:
- Version control integration (git-based source fetching)
- Dependency management (lock files)
- Project structure standardization (outputs schema)

These could be solved independently. The tight coupling forces complexity on small projects that need only one of these features.

### 2. Cross-Compilation Is Broken by Design

`packages.${system}` has only one system dimension. Cross-compilation needs two (`localSystem` + `crossSystem`). The traditional `callPackage` pattern resolves this automatically via nixpkgs internals, but flakes expose pre-evaluated attribute sets that lose this resolution context.

Implication: flakes cannot cleanly express "build on x86_64-linux targeting aarch64-linux" in a standard way. The community uses ad-hoc naming conventions or falls back to `legacyPackages` for cross-compiled packages.

### 3. No Configuration Support

Flakes provide "no configuration support except through hilarious abuses of `--override-input`." If you need to build your package with different feature flags, you must hardcode all variants in `flake.nix`. There's no equivalent of nixpkgs's `config` or `callPackage` parameter injection for external consumers to configure flake outputs.

### 4. Composition Is Functions, Not Flakes

The jade.fyi view: flakes are correctly understood as "entry points and dependency acquisition mechanisms," not as the unit of composition. Composition in Nix is functions (overlays, `callPackage`, `makeScope`). A flake should be a thin wrapper over a `packages.nix` or `overlay.nix` that contains the real logic.

Best practice: keep `flake.nix` minimal; extract logic into separately-loadable files that can move to nixpkgs unchanged.

### 5. Builtin Fetchers Block Evaluation

The jade.fyi analysis notes that "builtin fetchers block further evaluation while downloading." When a flake has many inputs (e.g., a monorepo with 20 language ecosystem dependencies each as flake inputs), all fetches happen before evaluation can proceed, serializing dependency resolution.

Mitigation: use fixed-output derivations (`pkgs.fetchurl`) instead of flake inputs for build-time dependencies that don't need to affect evaluation. Flake inputs are for things that must be available at evaluation time (nixpkgs, build frameworks); source archives can be normal derivations.

## `flake-utils` Controversy

`flake-utils` has >4,200 dependents in the flake registry. The controversy:
- **Pro**: reduces boilerplate, the most widely understood pattern
- **Con**: no type checking, generates malformed outputs silently, pollutes lock files, the `eachDefaultSystem` name misleads users into applying it to system-independent outputs

The nixos.wiki explicitly notes `flake-utils` is "largely discouraged" and recommends `flake-parts` or inline `genAttrs`.

`flake-utils` is itself used in `forge-metal/flake.nix`. Given that this repo uses it correctly (only for `packages` and `devShells`, not for `nixosConfigurations` or `overlays`), the risk is low. The main operational downside is the extra `flake-utils` node in `flake.lock`.

## `flake-compat`: Bridging Flakes and Traditional Nix

[`NixOS/flake-compat`](https://github.com/NixOS/flake-compat) reads `flake.lock` and calls the flake's `outputs` function from a traditional `default.nix`. This enables:
- `nix-shell` (no flakes support needed)
- `nix-build` targeting flake outputs
- Tools that don't support flakes

Pattern:
```nix
# shell.nix
(import (let lock = builtins.fromJSON (builtins.readFile ./flake.lock); in
  fetchTarball {
    url = lock.nodes.flake-compat.locked.url or "https://github.com/NixOS/flake-compat/archive/master.tar.gz";
    sha256 = lock.nodes.flake-compat.locked.narHash;
  }
) { src = ./.; }).shellNix
```

Limitations: requires `flake.lock` to already exist; `self.rev`/`self.shortRev` are unavailable (always `"dev"`); `nixConfig` is ignored. See [home-manager-darwin-compat.md](home-manager-darwin-compat.md) for full flake-compat coverage.

## Alternatives to Flakes

### For Dependency Pinning Without Flakes

**npins** (github.com/andir/npins):
- JSON-based pinning similar to Niv
- Can import entries from `flake.lock`
- Generates standard `fetchTarball`/`fetchGit` Nix expressions
- No experimental features required

**niv** (github.com/nmattia/niv):
- Original JSON-based pinning tool
- Narrower scope than flakes; does one thing

Both tools have the advantage of generating Nix expressions compatible with any Nix version, without the experimental flag.

### For Modular Configuration

**haumea** (github.com/nix-community/haumea):
- Filesystem-based module system
- Like Python's `__init__.py` but for Nix
- Supports visibility rules, automatic imports
- Used as foundation for `namaka` (snapshot testing for Nix)

### For Language Ecosystem Packaging

**dream2nix** (github.com/nix-community/dream2nix):
- Converts language-ecosystem lock files (package-lock.json, Cargo.lock, poetry.lock) to Nix
- Works with existing ecosystem lock files rather than replacing them
- Limitation: cannot act as primary dependency manager; relies on other tools maintaining lock files

## The nixpkgs Maintainer Perspective

nixpkgs maintainers have been skeptical of flakes for several reasons:
1. RFC 49 withdrawal: the flake RFC process was not completed before shipping
2. `legacyPackages` naming perpetuates the framing that the "right way" to use nixpkgs involves wrapping it in a flake
3. The `follows` burden falls on nixpkgs consumers rather than being automatic
4. Flakes increase nixpkgs's surface area for breakage (e.g., changes to nixpkgs's `flake.nix` can break all downstream flakes that pin nixpkgs)
5. Many nixpkgs contributions come from non-flake workflows; requiring flakes would narrow contributions

## Production Reality

Despite experimental status, flakes are the dominant pattern for new Nix projects. GitHub shows a "clear rise for flakes and nothing else" in Nix repository creation since 2021 (Determinate Systems analysis). The de-facto standard for:
- NixOS system configurations (`nixosConfigurations`)
- Development shells (`devShells`)
- Package publishing
- CI (GitHub Actions via `DeterminateSystems/nix-installer-action` + `nix flake check`)

FlakeHub (Determinate Systems) provides versioned, semver-compatible flake distribution — addressing the "no versioning beyond git commit hash" limitation of the base flake system. FlakeHub inputs use HTTPS tar.gz URLs (`https://flakehub.com/f/NixOS/nixpkgs/0.1.*.tar.gz`), not a custom URL scheme. A companion `fh` CLI (`github.com/DeterminateSystems/fh`) handles `fh add nixpkgs` and related operations. FlakeHub Cache is a separate binary cache service for CI. See [lang-advanced-determinate-fleet.md](lang-advanced-determinate-fleet.md) §2.2 for the full breakdown.
