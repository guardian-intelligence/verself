# Nix Flakes Research

This corpus covers Nix Flakes with emphasis on non-obvious, technically precise, and operationally relevant information for a forge-metal production deployment.

## Contents

- [internals.md](internals.md) — How flakes work under the hood: registry, lock format, eval cache, fetching pipeline, `self` reference
- [gotchas.md](gotchas.md) — Sharp edges: dirty trees, `self.outPath` vs `sourceInfo`, CI failures, `--impure`, `nixConfig` security
- [dependency-management.md](dependency-management.md) — The `follows` mechanism, diamond dependency problem, `flake-utils` controversy, `flake-parts`
- [performance.md](performance.md) — Eval cache, copy-to-store overhead, `nix copy` over SSH, `legacyPackages` caching
- [security.md](security.md) — `narHash`, content-addressed store, supply chain guarantees, `nixConfig` / `accept-flake-config` attack surface
- [community-and-stabilization.md](community-and-stabilization.md) — Experimental status, RFC 49/136, Determinate Systems fork, alternatives

## Relationship to This Repo

`forge-metal/flake.nix` uses `flake-utils.lib.eachDefaultSystem` and `nixpkgs.legacyPackages.${system}`. The gotchas and performance notes in this corpus apply directly to that file and to the `nix copy --to ssh://...` step in `make deploy`.
