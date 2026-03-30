# Nix Flakes Gotchas

Non-obvious behaviors, sharp edges, and surprising interactions. Annotated with root causes from source code where available.

## Dirty Trees and `self.shortRev`

The pattern `self.shortRev or "dev"` is ubiquitous but misunderstood. Here's exactly what happens:

**Source**: `src/libfetchers/git-utils.cc` (`getWorkdirInfo`, lines 539-570) and `src/libfetchers/git.cc` (`getAccessorFromWorkdir`, lines 1025-1053).

Nix calls libgit2's `git_status_foreach_ext` with `GIT_STATUS_OPT_INCLUDE_UNMODIFIED` and `GIT_STATUS_OPT_EXCLUDE_SUBMODULES`. Critically, `GIT_STATUS_OPT_INCLUDE_UNTRACKED` is **absent**. Untracked files are invisible to Nix's git status check.

When a tracked file is modified (`isDirty = true`):
```cpp
input.attrs.insert_or_assign("dirtyRev", headRev + "-dirty");
input.attrs.insert_or_assign("dirtyShortRev", headRev.substr(0, 7) + "-dirty");
// rev, shortRev, revCount are NOT set
```

When dirty, `self.rev` and `self.shortRev` **are absent** (attribute missing). The `or` operator in Nix handles missing-attribute errors on the left-hand side, so `self.shortRev or "dev"` returns `"dev"` on a dirty tree. Note: `or` specifically catches `MissingAttribute` errors — it is not a general exception handler.

**Untracked files**: Adding an untracked file does NOT trigger dirty. It also cannot be accessed from `flake.nix` — the `AllowListSourceAccessor` only allows paths that appear in `wd.files` (tracked set). Accessing an untracked file from `flake.nix` throws "file not allowed by the source filter". Fix: `git add --intent-to-add <file>` to stage it without content.

**CI implication**: If your CI system checks out a commit without using `git` (e.g., downloads a tarball), `self.shortRev` will be set from the tarball's locked `rev`. If CI does a shallow clone with uncommitted files from the runner, you may get `"dev"` in your binary version strings unexpectedly.

## `legacyPackages` vs `packages`

`legacyPackages` is a naming convention to prevent `nix flake show` from recursively evaluating all packages. Source: `github.com/NixOS/nixpkgs/flake.nix` lines ~218-230.

nixpkgs comment:
> "The 'legacy' in `legacyPackages` doesn't imply that the packages exposed through this attribute are 'legacy' packages. Instead, `legacyPackages` is used here as a substitute attribute name for `packages` ... `nix flake show nixpkgs` [becomes] unusably slow due to the sheer number of packages the Nix CLI needs to evaluate. But when the Nix CLI sees a `legacyPackages` attribute it displays `omitted`."

The `packages` output schema requires a flat attrset of derivations (`packages.<system>.<name> = <drv>`). The CLI recurses into it to validate every value is a derivation. For ~100,000 packages this is impractical. `legacyPackages` has no CLI-enforced schema — it can contain nested attrsets (`pythonPackages`, `haskellPackages`, etc.).

**Gotcha**: Using `pkgs = nixpkgs.packages.${system}` will NOT give you the full nixpkgs package set. Use `pkgs = nixpkgs.legacyPackages.${system}` or `pkgs = import nixpkgs { inherit system; }`. This is a common confusion for newcomers.

## The Eval Cache Is Disabled for Local Development

`nix develop .` uses a `path:` flake ref. `LockedFlake::getFingerprint` returns `std::nullopt` for path inputs (no `rev` can be determined). The eval cache is completely bypassed.

Every `nix develop .` invocation re-evaluates `flake.nix` from scratch. For large flakes (many packages, complex module compositions), this can be slow (500ms–2s). The `nix-direnv` tool mitigates this by pinning the flake to its HEAD commit and creating a GC root for the eval cache entry — but this means the shell environment doesn't update on uncommitted `flake.nix` changes.

## Impure Eval Disables the Cache

Even for non-path flakes, if `--impure` is passed (setting `pureEval = false`), the eval cache is disabled. Source: `eval-cache.cc` checks `pureEval` before attempting cache lookup. Note: `allow-import-from-derivation` is an orthogonal setting controlling whether IFD is permitted — it has no effect on `pureEval` or the eval cache.

## `flake.lock` Is Committed Per-Repo, Not Per-Input

The lock file describes the entire input closure of your flake, not just your direct inputs. If `flake-utils` depends on `systems`, the `systems` input appears in YOUR `flake.lock` even though you never declared it. `nix flake update` updates all transitive inputs.

**Gotcha with `follows`**: If you declare `foo.inputs.bar.follows = "nixpkgs"` but `foo` does not have a `bar` input, Nix silently ignores the follows. No error. This can cause you to think you've unified dependencies when you haven't.

## `nix flake check` Does Not Run Tests

`nix flake check` validates the schema of outputs (e.g., checks that `packages.<system>.<name>` are derivations, checks that `nixosConfigurations` are NixOS systems) but does NOT run `nix build` on all outputs by default. Add `--build` to actually build.

It also runs `nix-store --check-validity` on the cached outputs if available, but this does not exercise runtime behavior.

## Flake Inputs Are Fetched at Evaluation Time, Not Lock Time

This is a subtle distinction: `nix flake lock` **locks** the inputs (resolves refs to `rev` + `narHash`), but does NOT fetch them. The actual downloads happen when `nix build` or `nix develop` **evaluates** the flake and forces a thunk that references an input. This is why `nix develop` on a fresh machine can still be slow even with a committed `flake.lock` — it needs to fetch all inputs.

`nix flake archive` can pre-fetch all inputs into the local Nix store before evaluation. Useful for airgapped CI: run `nix flake archive .` on a build server with internet access, push the Nix store, then evaluate on the airgapped machine.

## `--no-update-lock-file` vs `--no-write-lock-file`

- `--no-update-lock-file`: Do not update `flake.lock`. Throw an error if any input is unlocked. Use in CI to enforce the lock file is committed.
- `--no-write-lock-file`: Evaluate with updated locks but do not write them back to disk. Useful for quick experiments without modifying the on-disk lock file.
- `--recreate-lock-file`: Ignore existing lock file entirely and re-resolve all inputs from scratch.

In CI, always use `nix build --no-update-lock-file .#server-profile` to catch accidentally uncommitted `flake.lock` changes.

## Sandboxed Build Can't Access Network Even with `--impure`

`--impure` allows evaluation-time impurity (e.g., `builtins.getEnv`, `builtins.currentTime`), but build-time network access is still controlled by the **sandbox**. A derivation that tries to `curl` during build will fail even in impure mode unless `sandbox = false` in `nix.conf` or the derivation sets `__noChroot = true` (requires `allowUnsafeNativeCodeDuringEvaluation`). These are separate concerns.

## `self` Infinite Recursion in Outputs

The following causes infinite recursion at evaluation:
```nix
outputs = { self, ... }: {
  packages.x86_64-linux.default = self.packages.x86_64-linux.default;  # OK (same thunk)
  lib.foo = self.lib.foo + " bar";  # INFINITE RECURSION at force time
};
```

The second form causes recursion because forcing `self.lib.foo` forces the same thunk that is currently being evaluated. The rule: you can reference `self.X` in your outputs if X is a different attribute (not the one being defined). This is tracked in [NixOS/nix#8300](https://github.com/NixOS/nix/issues/8300).

## `specialArgs` vs `_module.args` for NixOS Modules

When using `nixpkgs.lib.nixosSystem`:
```nix
nixosSystem {
  specialArgs = { inherit inputs; };  # injected at MODULE LOADING time
  modules = [ ({ inputs, ... }: { ... }) ];
}
```

`specialArgs` injects values before modules are loaded (available to `imports = [ ... ]` paths). `_module.args` injects after loading (available to module option values but NOT to `imports`). Trying to use `_module.args.inputs` inside an `imports = [ ]` list will throw "infinite recursion" because modules have not finished loading.

## Locked URL with `narHash` as Flake Input Crashes

If you use a tarball input with only `narHash` (no `rev`):
```nix
foo = {
  url = "https://example.com/foo.tar.gz";
  narHash = "sha256-...";
};
```

Nix validates `narHash` format at evaluation time. If the format is wrong (e.g., wrong SRI format, wrong hash length), you get a cryptic "expected hash of type 'sha256'" error that points at the wrong line. The fix is to run `nix flake prefetch <url>` to get the correct `narHash`. Tracked in [NixOS/nix#9303](https://github.com/NixOS/nix/issues/9303).

## `inputsFrom` in `mkShell` Doesn't Inherit Shell Hooks

`pkgs.mkShell { inputsFrom = [ pkgs.somePackage ]; }` takes the `buildInputs` and `nativeBuildInputs` of `somePackage` but does **not** inherit `shellHook`, `postInstall`, or other phases. If a dependency has setup hooks that you rely on (e.g., `pkg-config` auto-setup), you may need to add them explicitly.

## Binary Cache Misses from Multiple nixpkgs Instances

If your flake has two separate nixpkgs instances (e.g., `nixpkgs` for packages and `nixpkgs-stable` for a specific tool), derivations from each instance will have different store paths even if the content would be identical. Binary caches are keyed by store path, not content. Every derivation built from `nixpkgs-stable` must be rebuilt or cached separately. This is a common source of unexpectedly long CI times when using multiple nixpkgs inputs.
