# Nix Flakes Research

Nix Flakes is an experimental (but widely adopted) feature that provides hermetic, reproducible package definitions with content-addressed lockfiles. Introduced in Nix 2.4 (2021) via RFC 0049, flakes have become the de facto standard for Nix projects despite never being officially stabilized as of 2026.

## Documents

| File | Contents |
|------|----------|
| [internals.md](internals.md) | Source-level: lockfile format, `follows` resolver, eval cache, fetcher, `self` assembly |
| [gotchas.md](gotchas.md) | Sharp edges: dirty trees, untracked files, `legacyPackages`, impure eval |
| [ecosystem.md](ecosystem.md) | `flake-utils` vs `flake-parts`, community debates, alternatives |

## Key Facts for forge-metal

The `flake.nix` in this repo uses:
- `nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable"` — single nixpkgs instance, avoids diamond dependency binary cache misses
- `flake-utils.lib.eachDefaultSystem` — iterates across `["x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin"]`
- `self.packages.${system}.default` inside `serverProfile` — references the Go binary derivation from within the same flake; works because `self` is lazily evaluated
- `self.shortRev or "dev"` — `self.shortRev` throws in dirty worktrees; the `or` catches the eval error

The eval cache is **disabled** for local path flakes and impure evaluation. `nix develop .` (path flake) never hits the eval cache — every shell invocation re-evaluates `flake.nix`.

## Why Flakes

| Without Flakes | With Flakes |
|----------------|-------------|
| `nix-channel` per-user state; channels diverge between machines | `flake.lock` pins all inputs; same commit = same build |
| `NIX_PATH` implicit global; easy to contaminate | Pure eval by default; network/env access blocked |
| No first-class multi-output structure | Typed schema: `packages`, `devShells`, `nixosConfigurations`, etc. |
| `default.nix` fetches at eval time | All fetching done at lock time; eval is hermetic |

## Historical Context

RFC 0049 was authored by Eelco Dolstra and merged in 2020. Flakes remain behind `--extra-experimental-features nix-command flakes` in the Nix installer defaults. The Determinate Systems Nix installer enables them by default, which is why most new projects assume they're available.
