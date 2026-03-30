# Nix Flakes Ecosystem

Community tools, debates, and alternatives. Focuses on practical tradeoffs relevant to a bare-metal CI platform.

## `flake-utils` — What It Actually Does

Source: `github.com/numtide/flake-utils/lib.nix`, `eachSystem` function.

```nix
eachSystem = eachSystemOp (
  f: attrs: system:
  let ret = f system;
  in builtins.foldl' (attrs: key:
    attrs // { ${key} = (attrs.${key} or {}) // { ${system} = ret.${key}; }; }
  ) attrs (builtins.attrNames ret)
);
```

For each system, it calls `f system` and merges the results. If `f "x86_64-linux"` returns `{ packages.foo = ...; devShells.default = ...; }`, the fold produces:
```nix
{
  packages.x86_64-linux.foo = ...;
  devShells.x86_64-linux.default = ...;
}
```

`eachDefaultSystem` iterates `["x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin"]`.

**Non-obvious behavior**: If `builtins.currentSystem` is set (impure mode with `--impure`) and the current system is not in the `systems` list, `eachSystemOp` appends the current system. This means impure evaluation can silently add your current architecture to the output, which differs from what CI produces.

`eachSystemPassThrough` does NOT inject the `${system}` key — it merges the return value directly. Useful for outputs that don't need per-system nesting (e.g., `nixosConfigurations`, `lib`).

**The criticism of `flake-utils`**: It uses `builtins.foldl'` over `builtins.attrNames ret`, which forces evaluation of the return attrset's attribute names for every system. For flakes with many outputs, this means the fold runs N_systems × N_output_types times. More importantly, all systems are evaluated eagerly — you cannot skip evaluation for `aarch64-darwin` if you only care about `x86_64-linux`. The `flake-parts` approach (below) evaluates lazily per system.

## `flake-parts` — The Module System Approach

`github.com/hercules-ci/flake-parts` restructures flake outputs using the NixOS module system. Instead of a function returning a merged attrset, each output is declared as a module option.

```nix
# Using flake-parts
{
  inputs.flake-parts.url = "github:hercules-ci/flake-parts";
  outputs = inputs@{ flake-parts, ... }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      systems = [ "x86_64-linux" "aarch64-linux" ];
      perSystem = { pkgs, system, ... }: {
        packages.default = pkgs.hello;
        devShells.default = pkgs.mkShell { buildInputs = [ pkgs.go_1_25 ]; };
      };
      flake = {
        nixosConfigurations.myhost = inputs.nixpkgs.lib.nixosSystem { ... };
      };
    };
}
```

**Key advantages over `flake-utils`:**
1. **Lazy per-system evaluation**: `perSystem` is only evaluated for the system being requested. No cross-system fold.
2. **Module composition**: Multiple flake-parts modules can be `imports`-ed without collision. Each module only declares its own outputs. Community modules exist for common patterns (e.g., `treefmt-nix`, `git-hooks.nix`).
3. **Type checking**: Module options have types; wrong output shapes throw early with clear errors.
4. **`self'` and `inputs'`**: Inside `perSystem`, these provide `self` and `inputs` pre-scoped to the current system, avoiding the `${system}` suffix everywhere.

**When `flake-utils` is fine**: For small flakes (one or two outputs, two or three systems), `flake-utils` is simpler and has no dependencies. For forge-metal's current `flake.nix` structure, `flake-utils` is appropriate.

## The "Flakes Are Experimental" Situation

Flakes have been in "experimental" status since Nix 2.4 (2021). As of early 2026:
- All major Nix tooling (nixpkgs, home-manager, NixOS, devenv, etc.) assumes flakes
- The Determinate Systems Nix installer enables flakes by default
- The official NixOS installer still doesn't enable flakes by default
- RFC 0049 is merged but the CLI stabilization process is separate from the feature flag

The `--extra-experimental-features nix-command flakes` requirement means:
1. Nix daemon config (`/etc/nix/nix.conf`) must include `experimental-features = nix-command flakes`
2. Without it, `nix flake`, `nix build .#`, `nix develop` all fail
3. The feature flag does NOT affect correctness — it's purely a stability signal

Practical approach: add to `/etc/nix/nix.conf`:
```
experimental-features = nix-command flakes
```
Or pass `--extra-experimental-features nix-command flakes` to every nix invocation (noisy).

## `dream2nix` for Language-Specific Packaging

`github.com/nix-community/dream2nix` provides a framework for packaging language ecosystems (npm, cargo, pip, etc.) into Nix derivations with proper lock file integration. It differs from `buildGoModule` / `buildNpmPackage` in that it reads the upstream lock file (e.g., `package-lock.json`) directly rather than requiring a separate `vendorHash`.

For the forge-metal use case (JS/TS monorepos):
- `dream2nix` can produce a Nix derivation from `package-lock.json` without computing a separate vendor hash
- The resulting derivation uses the lock file as the source of truth, so `npm ci` semantics are preserved
- Relevant to golden image builds: if `node_modules` is pre-built via Nix, the golden zvol has reproducible cached dependencies

## `devenv` and `devShell` Alternatives

`github.com/cachix/devenv` wraps flake devShells with a NixOS module system for declaring development environments. It adds:
- Process management (start/stop multiple processes in dev with `devenv up`)
- Language-specific setup (auto-configure Python virtualenv, Node.js corepack, etc.)
- Test runners and pre-commit hooks as modules

For forge-metal's dev shell (`nix develop`), `devenv` would be overkill — the current `mkShell` is appropriate for toolchain-only shells without process management.

## Binary Cache and Cachix

`cachix.org` provides hosted Nix binary caches. Derivations built locally or in CI can be pushed to Cachix, and subsequent users pull pre-built outputs. Relevant for forge-metal:

- The golden image (`server-profile`) is ~2GB. Without a binary cache, every team member who runs `make server-profile` builds from source.
- With Cachix, the first build pushes, subsequent runs download. Download is typically faster than build even on fast machines.
- `nix copy --to s3://bucket?region=...&secret-key=...` can use S3/R2 as a binary cache. This integrates with Backblaze B2 (which is on the allowed-exceptions list for this project).

Setting up an S3-backed binary cache in `flake.nix` (clients pull via HTTPS, not the S3 protocol — the `s3://` scheme is for `nix copy --to`, not substituters):
```nix
nixConfig = {
  # Substituters use HTTPS; the S3 bucket must have public read or be fronted by a CDN/worker:
  extra-substituters = [ "https://my-bucket.s3.us-east-005.backblazeb2.com" ];
  extra-trusted-public-keys = [ "my-cache:pubkey..." ];
};
```

For the upload side, `nix copy` uses the `s3://` scheme:
```bash
nix copy --to 's3://my-bucket?region=us-east-005&endpoint=https://s3.us-east-005.backblazeb2.com' \
  /nix/store/...-server-profile
```

See [binary-caches-and-fetchers.md](binary-caches-and-fetchers.md) for the full S3 parameter reference and self-hosted cache server options (harmonia, nix-serve-ng, attic).

## Hermetic Evaluation: What Still Escapes

Flakes are described as "hermetic" but several things escape the sandbox:
1. **`builtins.currentSystem`**: available in impure mode; changes outputs silently
2. **`builtins.currentTime`**: available in impure mode; can vary between evaluations
3. **`builtins.getEnv`**: available in impure mode; can read arbitrary environment variables
4. **`import-from-derivation` (IFD)**: building a derivation during evaluation, then importing its output as a Nix expression. This makes evaluation depend on build outputs, breaking the eval/build separation. Disabled in pure mode by default but present in nixpkgs for some use cases.
5. **`builtins.fetchurl` / `builtins.fetchTarball`**: These bypass the lock file entirely and fetch at evaluation time. They accept a `sha256` for content verification but are still considered impure because the network is accessed at eval time. Flakes cannot use these without `--impure`.

The actual hermetic guarantee of flakes: **evaluation is deterministic given a fixed `flake.lock`**, not that evaluation is fully isolated from the system.

## Community Sentiment

**Nixpkgs maintainers critical of flakes**:
- Flakes changed the evaluation model in ways that conflicted with existing `callPackage` patterns
- `legacyPackages` is a workaround for a flake schema limitation
- The "experimental" flag has been used to justify shipping breaking changes

**Practical consensus** (2024-2026):
- Use flakes for new projects; avoid mixing flake and non-flake Nix in the same repo
- Use `flake-parts` for complex flakes with many modules; `flake-utils` for simple ones
- Pin `nixpkgs` aggressively; avoid `nixpkgs.url = "github:NixOS/nixpkgs"` without a branch/tag
- `nixos-unstable` (as used in forge-metal) has more recent packages but breaks occasionally; `nixos-24.11` is the stable channel
