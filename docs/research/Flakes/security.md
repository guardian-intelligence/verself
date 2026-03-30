# Nix Flakes Security Model

## `narHash` and Content-Addressed Inputs

Every locked flake input carries a `narHash`: the SHA-256 (in SRI format, e.g., `sha256-...`) of the NAR (Nix Archive) serialization of the fetched source tree.

NAR is a deterministic, canonical archive format — it serializes the file tree in a defined order with no timestamps, no ownership metadata, and no filesystem-specific data. The same file tree always produces the same NAR, and thus the same `narHash`.

**Supply chain guarantee**: `flake.lock` pins each dependency to a specific `rev` (commit hash) AND `narHash`. When Nix fetches the input, it verifies the fetched content matches the `narHash`. If the repository has been force-pushed or a tag moved to a different commit (a common supply chain attack vector), the `narHash` mismatch causes the fetch to fail:

```
error: hash mismatch in fixed-output derivation ...
  expected: sha256-<locked-hash>
  got:      sha256-<actual-hash>
```

This makes `flake.lock` a stronger supply chain artifact than a `yarn.lock` or `go.sum` that only pins version strings — the content itself is pinned, not just the version label.

**Binary cache substitution**: `narHash` enables Nix to compute the expected store path for a derivation without building it. The store path is derived from the content hash, so if the binary cache has a path with the same hash, it can be substituted. This is the mechanism enabling `cache.nixos.org` to serve pre-built binaries for nixpkgs.

## The Content-Addressed Store

Standard Nix store paths are input-addressed: the path is derived from the derivation inputs (what went into the build), not the output content. This means even if two derivations produce identical content, they get different store paths if their inputs differ.

Nix has experimental support for content-addressed (CA) derivations, where the store path is derived from the output content. CA derivations provide stronger deduplication and enable "trust-less" binary cache sharing (any two builds of the same CA derivation produce the same store path regardless of build environment).

CA derivations are separate from flake content-addressing — `narHash` in `flake.lock` addresses the source tree, not the build outputs.

For the forge-metal use case: the `narHash` in `flake.lock` guarantees that `clickhouse-common-static-26.3.2.3-amd64.tgz` fetched from `packages.clickhouse.com` has a specific content. If ClickHouse ever replaces that tarball at the same URL (a known supply chain attack), the build fails rather than silently using the replacement binary.

## `nixConfig` Attack Surface

See [gotchas.md](gotchas.md#9-nixconfig-security-accept-flake-config-is-a-root-escalation-vector) for the full attack analysis.

Summary: `accept-flake-config = true` allows any flake to set `post-build-hook`, enabling root code execution. This was exploited in the wild at `nix-ci.com`. Keep `accept-flake-config` at its default (false).

Safe settings that can be set in `nixConfig` without confirmation:
- `bash-prompt`, `bash-prompt-prefix`, `bash-prompt-suffix`
- `flake-registry`
- `commit-lock-file-summary`

All other settings require user confirmation or `accept-flake-config`.

## Flake Lock as SBOM Component

The `flake.lock` file provides a machine-readable Software Bill of Materials (SBOM) for all Nix-managed dependencies:
- Every input pinned to exact commit hash
- Every input content-verified via `narHash`
- Transitive closure of all Nix dependencies captured

Tools like `sbomnix` (github.com/tiiuae/sbomnix) can generate CycloneDX/SPDX SBOMs from `flake.lock` and the Nix store. Determinate Systems' `flake-checker` (github.com/DeterminateSystems/flake-checker) validates:
1. Nixpkgs inputs point to a supported release branch (not end-of-life)
2. Nixpkgs was updated within the last 30 days (security patches)
3. GitHub-hosted nixpkgs inputs are from the `NixOS` org (not forks)

## The World-Readable Store

All of `/nix/store` is readable by all users. This is intentional for sharing build artifacts but has implications:
- Any secret embedded in a Nix expression (including `flake.nix`) is world-readable once evaluated
- The result of `nix build` stores all build outputs world-readably
- Credentials passed as `buildInputs` or `environmentVariables` are world-readable in the store

Mitigations: `sops-nix` and `agenix` store encrypted secrets in the repo and decrypt them at activation time (outside the store), not at build time.

## Signing and Trust

Nix supports signed binary caches. The cache.nixos.org key (`cache.nixos.org-1`) is embedded in the default Nix configuration. Binary cache entries are signed with this key; Nix verifies signatures before using substituted paths.

Custom binary caches should be configured with:
```nix
nix.settings.trusted-public-keys = ["mycache:..."];
nix.settings.substituters = ["https://mycache.example.com"];
```

The `narHash` in `flake.lock` provides an additional layer: even if a binary cache were compromised and served a different binary, the `narHash` mismatch would be detected when re-fetching the source.

## Hermetic Evaluation and Reproducibility

Flakes guarantee hermetic evaluation (pure mode) but not bit-for-bit reproducibility of build outputs. The distinction:
- **Hermetic evaluation**: the Nix expression evaluation is deterministic given the same inputs
- **Reproducible builds**: the compiled artifacts are bit-identical across builds

Many derivations in nixpkgs achieve reproducible builds, but this is a property of the build process (controlled compilation flags, no embedded timestamps) rather than a flake guarantee.

The `narHash` in `flake.lock` guarantees the source is what you expect. Whether the build from that source is reproducible depends on the derivation.

## Registry Security

The global flake registry (`https://channels.nixos.org/flake-registry.json`) is fetched over HTTPS and its contents are not signed. Compromise of this endpoint could redirect `nixpkgs` to a malicious repository. Mitigations:
- Pin nixpkgs to an explicit GitHub URL (not via registry) in `flake.nix`
- The `flake.lock` pins the actual commit regardless of where the registry points, so the risk is only at `nix flake update` time

Issue [#7422](https://github.com/NixOS/nix/issues/7422) proposes removing system/user registries to reduce this attack surface.
