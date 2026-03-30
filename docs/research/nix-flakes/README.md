# Nix Flakes Research

Nix Flakes is an experimental (but widely adopted) feature providing hermetic, reproducible package definitions with content-addressed lockfiles. Introduced in Nix 2.4 (2021) via RFC 0049, flakes are the de facto standard for Nix projects despite never being officially stabilized as of 2026.

## Documents

| File | Contents |
|------|----------|
| [internals.md](internals.md) | Lockfile format, `follows` resolver, eval cache, fetcher, `self` assembly |
| [source-code-deep-dive.md](source-code-deep-dive.md) | Annotated `call-flake.nix` source, lazy self-reference trap, `builtins.getFlake`, `nix-direnv` |
| [gotchas.md](gotchas.md) | Dirty trees, untracked files, `legacyPackages`, impure eval, `self` recursion |
| [advanced-topics.md](advanced-topics.md) | `buildGoModule`/`vendorHash`, `nix copy` SSH/NAR, `buildEnv`, CA derivations, `nixConfig` security, lazy trees, `nix flake metadata` |
| [ecosystem.md](ecosystem.md) | `flake-utils` vs `flake-parts`, binary caches, hermetic evaluation limits |
| [outputs-and-ci.md](outputs-and-ci.md) | Complete output schema, `nix flake check`, `nix develop` internals, GitHub Actions CI, `nix profile`, `nix-systems` |
| [community.md](community.md) | RFC 136 stabilization plan, Lix/Determinate forks, design criticisms, `flake-compat`, alternatives |

## Key Facts for forge-metal

The `flake.nix` in this repo uses:
- `nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable"` — single nixpkgs instance, avoids diamond dependency binary cache misses
- `flake-utils.lib.eachDefaultSystem` — iterates across `["x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin"]`
- `self.packages.${system}.default` inside `serverProfile` — references the Go binary derivation from within the same flake; works because `self` is lazily evaluated
- `self.shortRev or "dev"` — `self.shortRev` throws in dirty worktrees; the `or` catches the eval error
- `buildGoModule` with `vendorHash` for `cmd/bmci` — hash covers the NAR of `vendor/`; update with `lib.fakeHash` trick after `go.mod` changes
- `nix copy --to ssh://host` uses `ServeProto` (legacy); `ssh-ng://` uses full daemon protocol; compression off by default

The eval cache is **disabled** for local path flakes. `nix develop .` never hits the eval cache — every shell invocation re-evaluates `flake.nix`. Use `nix-direnv` to cache devShell builds.

## Why Flakes

| Without Flakes | With Flakes |
|----------------|-------------|
| `nix-channel` per-user state; channels diverge between machines | `flake.lock` pins all inputs; same commit = same build |
| `NIX_PATH` implicit global; easy to contaminate | Pure eval by default; network/env access blocked |
| No first-class multi-output structure | Typed schema: `packages`, `devShells`, `nixosConfigurations`, etc. |
| `default.nix` fetches at eval time | All fetching at lock time; eval is hermetic |

## Historical Context

RFC 0049 was authored by Eelco Dolstra but was **withdrawn** after the implementation was already merged — a procedural anomaly that raised governance questions. Flakes remain behind `--extra-experimental-features nix-command flakes` in the official Nix installer defaults. RFC 136 (accepted August 2023) established a phased stabilization plan; see [community.md](community.md) for current phase status.
