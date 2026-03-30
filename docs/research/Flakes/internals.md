# Nix Flakes Internals

## What a Flake Is

A flake is a directory containing a `flake.nix` at its root that declares:
- `description` — a literal string (expressions are not allowed here; Nix will error with "expected a string but got a thunk")
- `inputs` — an attribute set of dependency declarations
- `outputs` — a function `{ self, inputA, inputB, ... }: { ... }` returning standardized attribute sets
- `nixConfig` — optional nix.conf overrides (see security notes)

Critically, `flake.nix` must be inside a Git repository for all practical purposes. Files not staged with `git add` are invisible to Nix during evaluation — they are not copied to the store and cannot be imported. This surprises nearly every new flake user.

## flake.lock Format

The lock file is JSON at schema version 7 (current as of Nix 2.x). Structure:

```json
{
  "version": 7,
  "root": "<root-node-name>",
  "nodes": {
    "<root-node-name>": {
      "inputs": { "nixpkgs": "nixpkgs_1", "flake-utils": "flake-utils_1" }
    },
    "nixpkgs_1": {
      "locked": {
        "type": "github", "owner": "NixOS", "repo": "nixpkgs",
        "rev": "abc123...",
        "lastModified": 1710000000,
        "narHash": "sha256-..."
      },
      "original": { "type": "github", "owner": "NixOS", "repo": "nixpkgs" },
      "inputs": {}
    }
  }
}
```

Key design points:
- The graph represents a DAG of all transitive inputs, not just direct ones.
- The root node deliberately omits `locked` and `original` attributes — because modifying `flake.lock` would invalidate its own content hash.
- Each non-root node carries both `original` (what the user wrote) and `locked` (fully resolved with commit hash and `narHash`).
- The `inputs` field in each node maps local input names to node keys, enabling deduplication via `follows`.
- `narHash` is the SHA-256 (SRI format) of the NAR serialization of the fetched source tree. This enables binary cache substitution by computing the expected store path without fetching.

Source: [nix flake reference manual](https://nix.dev/manual/nix/2.24/command-ref/new-cli/nix3-flake)

## Flake Input Types

The fetcher dispatch table:

| Type | Syntax | Locked attributes |
|------|--------|-------------------|
| `indirect` | `nixpkgs` | resolved via registry |
| `github` | `github:NixOS/nixpkgs/nixos-unstable` | `rev`, `narHash`, `lastModified` |
| `git` | `git+https://github.com/...` | `rev`, `ref`, `revCount`, `narHash` |
| `path` | `./local/dir` or `/abs/path` | `narHash` |
| `tarball` | `tarball+https://...tar.gz` | `narHash` |
| `file` | `file+https://...` | `narHash` |
| `gitlab` | `gitlab:owner/repo` | same as github |
| `sourcehut` | `sourcehut:~owner/repo` | same as github |

`github:` inputs are fetched as tarballs (not git clones), which is why `revCount` is unavailable for GitHub inputs — counting ancestors requires a full git history.

Git fetches use a shallow clone cached in `~/.cache/nix/` and backed by libgit2.

## The Flake Registry

The global registry lives at `https://channels.nixos.org/flake-registry.json` (fetched on first use, cached). It maps indirect names like `nixpkgs` → `github:NixOS/nixpkgs/nixpkgs-unstable`.

The registry JSON at [github.com/NixOS/flake-registry](https://github.com/NixOS/flake-registry/blob/master/flake-registry.json) contains 38 entries as of 2025, including `nixpkgs`, `home-manager`, `flake-utils`, `flake-parts`, `devenv`, `helix`, `disko`, `hydra`, `cachix`, `fenix`, `poetry2nix`, `nur`, `agenix`, `sops-nix`.

Resolution algorithm: given a reference R with type `indirect`, find a registry entry whose `from` matches R, then unify the entry's `to` with R (applying any `rev` or `ref` from R onto `to`). Example: `nixpkgs/23.11` resolves by taking `github:NixOS/nixpkgs/nixpkgs-unstable` and substituting `ref = "23.11"`.

There are three registry layers (system > user > flake-level `nixConfig.flake-registry`) with system/user registries overriding per-flake settings — a non-obvious source of environment divergence. This is why `--override-input nixpkgs github:NixOS/nixpkgs/nixpkgs-23.11` is the safe per-invocation override.

Important: using indirect inputs in `flake.nix` inputs section (e.g., `inputs.nixpkgs.url = "nixpkgs"`) means the registry is consulted at lock time, but the lock file pins the resolved reference. Subsequent evaluations use the locked `rev`, not the registry. The registry matters only for initial lock generation and `nix flake update`.

Concern: system/user registries create "global mutable state" that can cause `nix flake update` to produce different results for different users. Issue [#7422](https://github.com/NixOS/nix/issues/7422) proposes removing or restricting system/user registries.

## The `self` Reference

The `outputs` function always receives `self` as its first argument. `self` is a lazy attribute set with two categories of attributes:

**Source metadata** (from `sourceInfo`):
- `self.outPath` — store path to the directory containing `flake.nix` (since Nix 2.14)
- `self.sourceInfo.outPath` — store path to the root of the fetched source (equals `self.outPath` unless `?dir=` is used)
- `self.rev` — the full Git commit hash (absent if working tree is dirty)
- `self.shortRev` — first 7 chars of `rev` (absent if dirty; see gotchas)
- `self.dirtyRev` — the `rev` with `-dirty` suffix appended (present only when dirty)
- `self.dirtyShortRev` — short form of `dirtyRev`
- `self.lastModified` — Unix timestamp (int) of last git commit
- `self.lastModifiedDate` — formatted `%Y%m%d%H%M%S`
- `self.narHash` — SHA-256 SRI hash of the store tree

**Computed outputs** (lazy, can cause infinite recursion if accessed carelessly):
- `self.packages`, `self.devShells`, etc. — the evaluated outputs of this flake

The critical `self.outPath` / `self.sourceInfo.outPath` distinction (introduced in Nix 2.14): when a flake lives in a subdirectory of a git repo (via `?dir=subdir`), `self.outPath` points to the `subdir/` containing `flake.nix`, while `self.sourceInfo.outPath` points to the git repository root. Before 2.14, `self.outPath` inconsistently pointed to the repo root. Most flakes are at the repo root, so both are identical in the common case.

Source: [Release 2.14 notes](https://nix.dev/manual/nix/2.33/release-notes/rl-2.14); [commit 5d834c40](https://git.proot.pl/wroclaw-git-backups/nix/commit/5d834c40d0a1e397cc650f88b1544ee2e5912400)

## `self` as String Coercion

Any attribute set with an `outPath` field can be coerced to a string in Nix. Because flake inputs (including `self`) have `outPath`, you can write:

```nix
"${self}/path/to/file"      # equivalent to "${self.outPath}/path/to/file"
"${inputs.nixpkgs}/lib"     # works, no need for inputs.nixpkgs.sourceInfo.outPath
```

The verbose `sourceInfo.outPath` form is unnecessary for the common case.

## `self.outputs` vs Top-Level `self` Attributes

From `call-flake.nix`: `result = outputs // sourceInfo // { inherit outPath inputs outputs sourceInfo _type; }`. The `outputs` attribute set is merged onto `self` directly AND also available as `self.outputs`. So:

```nix
self.packages.x86_64-linux.default == self.outputs.packages.x86_64-linux.default  # identical
```

This means `self.packages` and `self.outputs.packages` are the same object — `self.outputs` is a convenience accessor to the un-merged outputs function result, not a separate namespace.

## Hermetic Evaluation

Flakes evaluate in pure mode by default. The following builtins are disabled or throw errors:
- `builtins.currentSystem` — non-hermetic; forces the user to pass `system` explicitly
- `builtins.currentTime` — not defined in pure eval
- `builtins.getEnv` — always returns `""` in pure mode
- `builtins.storePath` — throws in pure eval
- `builtins.filterSource` / `builtins.path` without `filter` argument — restricted

What still escapes hermetic evaluation (surprising):
- **The `nixConfig` attribute itself** is read before pure eval begins, so it can influence the Nix daemon settings that affect all builds
- **Impure derivations** (`__impure = true`) can access the network at build time
- **`--impure` flag** re-enables `currentSystem`, `getEnv`, and other builtins — needed for NIXPKGS_ALLOW_UNFREE, for reading host system state, for devenv
- **The registry** consulted at lock time is fetched from the network (not hermetic at lock time)
- **`builtins.fetchurl` without hash** works in impure mode; with hash (fixed-output), it works in pure mode too

An important nuance from nix.dev: "Even in pure mode, reproducibility is not actually guaranteed" — the evaluation is hermetic with respect to environment, but the Nix store contents themselves can differ if the binary cache serves different content (though `narHash` prevents this for locked inputs).

## Evaluation Caching (SQLite)

Nix caches flake evaluation results in a per-user SQLite database. The cache key is derived from the flake's content hash (via `flake.lock`), meaning the cache is invalidated when:
- `flake.lock` changes (any input update)
- The flake source changes (for local flakes: when git commit changes)
- The working directory is dirty (local flakes have no stable fingerprint)

The critical performance gotcha: **`nix develop path:` does not use the eval cache** because computing a fingerprint for a path-based flake requires reading the entire directory. From the Nix issue tracker: "relatively small flakes can take 10+ seconds to evaluate on modern hardware and this is a big barrier to relying on flakes for dev shells." The workaround is using `nix-direnv` (which has its own caching layer) or always being inside a git repo so the flake is treated as `git+file://`.

The eval cache is shared across commands but is process-local (not multi-process safe). Issue [#3794](https://github.com/NixOS/nix/issues/3794) documents "SQLite database is busy" errors under concurrent evaluation.

Disable the eval cache with `--option eval-cache false` or `--no-eval-cache` (the flag name varies by Nix version). This is sometimes needed when the cache produces stale results after in-place store changes.

Known bug: evaluation errors are cached ([#3872](https://github.com/NixOS/nix/issues/3872)), so a transient failure (network timeout fetching a dependency) can cause subsequent runs to fail from cache without a network call.

## Fetching Pipeline (Internals)

The fetching pipeline in `src/libfetchers/`:
1. Parse `FlakeRef` from URL-like syntax
2. Dispatch to `InputScheme` (GitInputScheme, GitHubInputScheme, TarballInputScheme, etc.)
3. If indirect: resolve via registry to concrete URL
4. Fetch and verify against `narHash` in lock file
5. Return store path + locked metadata

`call-flake.nix` is the internal wrapper that constructs `self`, resolves inputs via the lock file, and calls the `outputs` function. See [source-code-deep-dive.md](source-code-deep-dive.md) for the complete annotated source.

Git repositories are cloned shallow into `~/.cache/nix/` using `libgit2`. HTTP downloads use `libcurl` with connection pooling, retry with exponential backoff, and brotli/gzip decompression.

### `flake = false` Inputs

Setting `flake = false` on an input tells `call-flake.nix` to skip flake evaluation entirely for that node. The result is just `sourceInfo // { inherit sourceInfo outPath; }` — only the fetched source tree, no `outputs`, no `inputs`. This is the correct way to include arbitrary source archives (vendored code, data files) as flake dependencies.

```nix
inputs.mySource = {
  url = "github:user/some-repo";
  flake = false;   # don't try to evaluate flake.nix, just fetch the tree
};
# Usage: "${inputs.mySource}/path/to/file"
```

Non-flake inputs expose: `outPath` (store path), `sourceInfo` (metadata). They do NOT expose `packages`, `devShells`, or any outputs.

### `parent` Field in Lock Nodes (Undocumented)

Lock file nodes for relative path inputs have a `parent` field not documented in the official manual. This field contains the node key of the containing flake. The `call-flake.nix` source uses it to resolve `isRelative` nodes: their `sourceInfo` is inherited from the parent, and their `outPath` is constructed as `parentNode.outPath + "/" + node.locked.path`. This is how monorepos can have sub-flakes with `path:./subpackage` inputs that share the same fetched tree.

## `nix copy` Over SSH

`nix copy --to ssh://user@host .#server-profile` works as follows:
1. Evaluate the flake to get the store path of `server-profile`
2. Compute the transitive closure (all `/nix/store` paths needed at runtime)
3. Connect to remote via SSH, query which paths are already present
4. Transfer only missing paths as NAR archives (compressed)
5. Register paths in remote's Nix store database

The modern `ssh-ng://` protocol (e.g., `--to ssh-ng://host`) is more efficient than legacy `ssh://`: it uses the Nix daemon protocol over SSH rather than raw NAR streams, enabling parallel transfers and better error handling.

The closure is the fundamental unit: "you tell Nix the what (the package to copy) and the entire dependency tree comes along with it." Binary cache hits on the remote are respected — if a dependency is already in the remote store, it is skipped.

Note: `nix copy` connects twice in the legacy protocol — once to query missing paths, once to send. The `ssh-ng://` protocol merges these into a single connection.
