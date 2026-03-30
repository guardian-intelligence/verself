# Nix Flakes Performance

## Evaluation Cache Architecture

The eval cache is a SQLite database per user, located at `~/.cache/nix/eval-cache-v*/`. It stores evaluated flake output attribute values keyed by:
1. The flake's `narHash` (from `flake.lock`)
2. The flake's content hash

Because `flake.lock` transitively captures content hashes of all dependencies, the cache key effectively encodes the entire dependency tree. Changing any input (even deep transitive ones) invalidates the cache.

**Cache invalidation triggers**:
- `flake.lock` changes (any input version bump)
- Source file changes in a local git flake (on every commit)
- Working directory dirty state (no fingerprint possible; cache bypassed entirely)

**Cache miss symptoms**: every `nix develop` invocation re-evaluating nixpkgs, taking 10–20 seconds on modern hardware for a typical configuration.

**Bug: evaluation errors are cached** ([#3872](https://github.com/NixOS/nix/issues/3872)). A transient failure (network timeout during first eval) caches the error, causing subsequent invocations to fail from cache without retrying. Fix: `--option eval-cache false` or delete the cache.

**Disable the cache**:
```bash
nix develop --option eval-cache false
nix build --no-eval-cache   # flag name varies by Nix version
```

## Copy-to-Store Overhead for Local Flakes

Every local flake evaluation copies source files to `/nix/store` before evaluation begins. This is **eager, not lazy** — it happens even if the outputs function never accesses `self.outPath`.

**Without git** (path: flake): copies the entire directory tree including build artifacts, `node_modules`, `.git`, etc. Can be multiple gigabytes. Issue [#5551](https://github.com/NixOS/nix/issues/5551) documents real-world cases with "hundreds of thousands of files."

**With git** (inside a git repo): uses `git ls-files` to filter to tracked files only. This is the major reason to always keep your flake in a git repository — even with no commits, `git init` + `git add flake.nix` dramatically reduces the copy overhead.

**Optimization proposal** (issue [#5551](https://github.com/NixOS/nix/issues/5551), not merged): if the outputs function signature omits `self`, skip the copy entirely. The Nix team has considered but not implemented this.

**Practical mitigation** in this repo: `pkgs.lib.cleanSourceWith` with a filter excludes `result`, `results`, and `.direnv`:
```nix
src = pkgs.lib.cleanSourceWith {
  src = ./.;
  filter = path: type:
    let baseName = baseNameOf (toString path);
    in !(baseName == "result" || baseName == "results" || baseName == ".direnv");
};
```
This applies to the Go binary derivation's source, not to the overall flake copy, but reduces the size of the `src` store path used for building.

## `path:` vs `git+file://` Performance

| Reference type | Files copied | Eval cache | Use case |
|----------------|-------------|------------|----------|
| `path:/dir` (non-git) | All files, including untracked | Not cached | Avoid |
| `path:.` (inside git) | Only git-tracked files | Cached via narHash | Common |
| `git+file:///path` | Only git-tracked files, uses git metadata | Cached | Explicit |

The documentation says `path:` inside a git repository is treated as `git+file:` — but issue [#5836](https://github.com/NixOS/nix/issues/5836) notes this is not always inferred correctly. Using explicit `git+file:///path` guarantees git-based behavior.

`path:` flakes outside git repos have no fingerprinting mechanism, so the eval cache cannot be used. Every `nix develop path:/some/dir` re-evaluates from scratch.

## `nix copy` Transfer Performance

`nix copy --to ssh://host .#server-profile` transfers the runtime closure. Performance factors:

1. **Closure size**: the server-profile in this repo is ~2GB (Nix closure, before compression). Transfer time dominates for initial deploys.
2. **Binary cache hits on remote**: if the remote has a partial closure (e.g., unchanged nixpkgs packages), only new/changed paths are transferred.
3. **Compression**: NAR archives are compressed in transit. ZSTD compression (available in newer Nix) is faster than bzip2.
4. **Protocol**: `ssh-ng://` (daemon-to-daemon) is more efficient than `ssh://` (raw NAR stream). Use `--to ssh-ng://user@host` when the remote has the Nix daemon.
5. **Parallelism**: `--option max-jobs N` controls parallel store path transfers.

For incremental deploys: if only the Go binary changed, the closure diff is small (one store path + its direct dependencies). Nix computes the diff before transferring, so deploy time scales with what changed, not total closure size.

## `legacyPackages` Caching vs `import nixpkgs {}`

`nixpkgs.legacyPackages.${system}` is defined once in nixpkgs's `flake.nix` as a lazy attribute set. When accessed via a flake input, it is memoized by the Nix evaluator for the duration of the evaluation session.

Calling `import nixpkgs { system = "x86_64-linux"; }` directly creates a new thunk each call. Nix does memoize the `import nixpkgs` call itself (the function), but **function calls are not memoized** — each call with the same arguments returns a fresh unevaluated thunk. In practice, for a flake with multiple inputs that all use nixpkgs, using `legacyPackages` through the shared `follows` mechanism avoids re-evaluating the nixpkgs package set multiple times.

The performance difference is most visible in large configurations with 10+ flake inputs each using nixpkgs.

## `nix flake show` Performance

`nix flake show` on nixpkgs takes seconds to minutes because it must evaluate enough of the attribute tree to display it. `legacyPackages` entries are displayed as "(omitted)" to avoid evaluating all 80,000+ packages.

For your own flake, `nix flake show` evaluates all non-`legacyPackages` outputs. If you have many `packages.${system}` entries or `nixosConfigurations`, this can be slow. Adding `nix flake check` to CI is faster than `nix flake show` because `check` evaluates outputs in parallel.

## `nix-direnv`: Production Eval Cache Solution

The official eval cache bypasses for `path:` flakes makes `nix develop` slow for large configurations. `nix-direnv` solves this for development:

- Calls `nix print-dev-env` once and caches the result in `.direnv/` keyed by `flake.nix` + `flake.lock` hash
- Creates a **GC root** for the cached derivation — critical because without it, `nix-collect-garbage` removes the cached shell, forcing re-evaluation
- Watches `flake.nix`, `flake.lock`, `shell.nix`, and `default.nix` for changes; re-evaluates only on change
- Falls back to the previous working devShell if new evaluation fails

Without `nix-direnv`, `nix develop` creates no GC root — the evaluated environment is garbage-collected after the session.

Source: [github.com/nix-community/nix-direnv](https://github.com/nix-community/nix-direnv)

## `nix flake archive`: Pre-fetching Inputs for Offline Use

`nix flake archive` copies the **source trees** of all flake inputs (not build outputs) to a Nix store. Unlike `nix copy` (which copies build output closures), `nix flake archive` copies the flake DAG nodes from `flake.lock`.

```bash
# Pre-fetch all inputs to a local cache before entering network-isolated CI:
nix flake archive --to file:///tmp/flake-inputs-cache

# Inspect the transfer without executing:
nix flake archive --dry-run --json | jq '{inputs: .inputs | keys}'
```

Use case: airgapped CI where `nix build` must evaluate the flake without internet. Archive first, then run builds in a `--network none` environment.

## `nix flake check` Validation Cost

`nix flake check` builds (not just evaluates) every derivation in `checks.${system}`. This is potentially expensive:

```nix
checks.x86_64-linux = {
  unit-tests = pkgs.runCommand "tests" {} ''${self.packages.x86_64-linux.default}/bin/test && touch $out'';
};
```

Use `--no-build` to only evaluate (type-check outputs, verify attribute structure) without building derivations.

The `--all-systems` flag extends checking to all systems. Without it, only the current system's outputs are checked (fixed in Nix post-2022 from the bug where all systems were always checked).
