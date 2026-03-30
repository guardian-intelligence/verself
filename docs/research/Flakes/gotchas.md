# Nix Flakes Sharp Edges and Gotchas

## 1. Untracked Files Are Invisible

The single most common confusion: **any file not tracked by git is silently invisible to Nix**. A freshly created `flake.nix` won't be recognized until `git add` is run. The error is a cryptic `/nix/store/...` path message, not "file not found." This affects all files imported from `self`, not just `flake.nix` itself.

Implication for this repo: `pkgs.lib.cleanSourceWith` in `flake.nix` uses `filter` to exclude `result`/`results`/`.direnv`, but the primary protection against leaking build artifacts into the store is git's `.gitignore`. New files added to the repo must be staged before `nix build .#server-profile` can see them.

## 2. Dirty Trees and Missing `shortRev`

When a git repository has uncommitted changes, the flake object loses revision attributes:
- `self.rev` — **absent**
- `self.shortRev` — **absent**
- `self.dirtyRev` — present (added in Nix ~2.14), e.g., `"abc1234-dirty"`
- `self.dirtyShortRev` — present, short form

The forge-metal `flake.nix` uses `self.shortRev or "dev"` as a fallback:
```nix
"-X main.version=${self.shortRev or "dev"}"
```
and
```nix
name = "forge-metal-server-${self.shortRev or "dev"}";
```

This is correct defensive practice. Without the `or "dev"` fallback, the expression throws an error on dirty trees. The common pattern `"0.1.${self.shortRev or "dirty"}"` also works.

Source: [github.com/NixOS/nix/issues/6034](https://github.com/NixOS/nix/issues/6034)

## 3. GitHub Actions: Detached HEAD Appears Dirty

A well-known CI gotcha: GitHub Actions PR builds check out in a detached HEAD state with no branch references in `.git/refs/heads/`. Nix's git fetcher validates the repository by checking for branch references, incorrectly flagging the tree as dirty when the working tree is actually clean.

Root cause (from issue #5302): Nix assumed `refs/heads/` must have entries for a valid checkout. GitHub Actions only fetches commits, not branches.

Workaround (before the fix was merged):
```bash
nix build ./?rev=$(git rev-parse HEAD)
```

The fix (PR #7759) changed Nix to use git itself for HEAD validation rather than checking the `refs/heads/` directory.

Status: Fixed in Nix, but older Nix versions on CI runners may still exhibit this. Pin your Nix version in CI.

Source: [github.com/NixOS/nix/issues/5302](https://github.com/NixOS/nix/issues/5302)

## 4. `--no-write-lock-file` vs `--no-update-lock-file`

These flags have distinct and easily confused semantics:

- `--no-update-lock-file` — **do not check for or fetch** new dependency versions; fail if the lock file would need changes
- `--no-write-lock-file` — compute new lock file in memory but **do not persist** it to disk

Using both together is incoherent: updates are pulled and used during the build, but the lock file on disk doesn't reflect what was built. This silently breaks reproducibility. Issue [#9320](https://github.com/NixOS/nix/issues/9320) proposes making these flags mutually exclusive.

**CI best practice**: use `--no-update-lock-file` to assert the lock file is up to date. CI should fail if someone pushes a `flake.nix` change that requires a lock file update they forgot to commit.

## 5. Local Path Flakes Copy Everything to the Store

When evaluating a local flake at a `path:` reference (not a git repo), Nix copies the **entire directory** to `/nix/store` before evaluation. This includes build artifacts, `node_modules`, `.git`, and anything else present.

Issue [#5551](https://github.com/NixOS/nix/issues/5551): a user's non-git path flake tried to copy "hundreds of thousands of files comprising many gigabytes of data" on every `nix develop` invocation.

Inside a git repo, `git ls-files` is used to filter: only tracked files are copied. This is a major performance difference — always be inside a git repo.

Optimization (proposed but not merged as of 2025): if the outputs function omits `self` from its parameter list, skip the store copy entirely.

## 6. `description` Must Be a Literal String

The `description` field at the top level of `flake.nix` cannot use `let` bindings or computed expressions. Attempting this:
```nix
let version = "1.0"; in
{
  description = "My package ${version}";  # ERROR: "expected a string but got a thunk"
  ...
}
```
fails because the top-level attributes are parsed before the Nix evaluator runs. The `description` must be a bare string literal.

## 7. `builtins.currentSystem` Is Banned in Pure Eval

Calling `builtins.currentSystem` inside a flake throws an error by default. This forces all system-specific outputs to be explicitly keyed by system string:

```nix
outputs = { self, nixpkgs }: {
  packages.x86_64-linux.default = ...;   # must be explicit
  # NOT: packages.${builtins.currentSystem}.default = ...;  # ERROR
};
```

To support multiple systems, the pattern is either manual `genAttrs` or `flake-utils.lib.eachDefaultSystem`. This is also why cross-compilation is awkward in flakes — the flake cannot query the host system at evaluation time.

Workaround when you need host system info: `--impure` re-enables `builtins.currentSystem`. Used by devenv and similar tools.

## 8. `--impure` Flag: What It Re-enables

Passing `--impure` lifts the following restrictions:
- `builtins.currentSystem` — returns the actual host system
- `builtins.getEnv` — reads actual environment variables
- Access to `$NIX_PATH` and the Nix search path
- `builtins.fetchurl` without hash

Required for:
- `NIXPKGS_ALLOW_UNFREE=1 nix build --impure` (the recommended way to allow unfree in flakes — `~/.config/nixpkgs/config.nix` does NOT work for flakes)
- devenv (`--impure` lets it read the running environment)
- Any expression that reads environment variables (e.g., `builtins.getEnv "HOME"`)

## 9. `nixConfig` Security: `accept-flake-config` Is a Root Escalation Vector

`nixConfig` in `flake.nix` sets Nix daemon options. Only a few options (bash prompt settings, `flake-registry`, `commit-lock-file-summary`) are applied without confirmation. All others require the user to confirm or set `accept-flake-config = true` in `nix.conf`.

**Critical vulnerability**: `accept-flake-config = true` (or `--accept-flake-config` CLI flag) allows any flake to set `post-build-hook` — a script that runs as root after every build. Proof of concept from issue [#9649](https://github.com/NixOS/nix/issues/9649):

```bash
NIX_CONFIG="post-build-hook = /path/to/malicious/script" nix build nixpkgs#hello --rebuild
```

This creates an arbitrary root code execution path for any trusted user. The attack was exploited in the wild against `nix-ci.com`.

Also dangerous: `substituters` (arbitrary binary caches), `builders` (arbitrary build machines), `allow-import-from-derivation`.

Safe settings: `extra-substituters`, `bash-prompt-*`, `flake-registry`. Do not add `accept-flake-config = true` to your `nix.conf`. The default behavior (prompt per new setting) is the safe mode.

Source: [notashelf.dev](https://notashelf.dev/posts/reject-flake-content); [github.com/NixOS/nix/issues/9649](https://github.com/NixOS/nix/issues/9649)

## 10. Relative Path Inputs Break Across Machines

Using relative path inputs like `inputs.local.url = "path:../sibling-repo"` causes issues:
- `flake.lock` stores an absolute path (the resolved path at lock time)
- On a different machine or CI, that absolute path won't exist
- Result: `nix flake update` or `nix build` fails with "path does not exist"

This is a common trap when developing with local flake checkouts as inputs. The workaround (per Julia Evans' notes) of deleting `flake.lock` before each build is a sign of this underlying issue.

For CI: use `--override-input local path:/abs/path/on/ci` or pin the dependency to a git URL.

## 11. `nix flake check` Evaluates All Systems (Historical Bug, Fixed)

Before the fix in PR #7759, `nix flake check` attempted to evaluate `checks.${system}` for **all systems** declared in the flake, not just the current host system. This caused errors like:

```
a 'x86_64-darwin' with features {} is required to build '/nix/store/...java-home.drv',
but I am a 'x86_64-linux'
```

The fix (Nix 2.x, post-2022) added `--all-systems` flag and changed the default to only check the current system's outputs.

Remaining gotcha: `nix flake check` tries to build everything in `checks.${system}` on the current system. If your `checks` output references a derivation that requires a different system, it will attempt and fail to cross-build unless properly configured.

## 12. Secrets in `flake.nix` Are World-Readable

The Nix store at `/nix/store` is world-readable. Any secret embedded in `flake.nix` (API keys, tokens, passwords) or any file imported into the store via `self` is readable by all users on the system. Use `sops-nix` or `agenix` to manage secrets.

## 13. Git Submodule Files Are Not Included by Default

Since Nix 2.27, opt-in support exists:
```nix
inputs.self.submodules = true;  # fetch submodules
inputs.self.lfs = true;         # fetch Git LFS files
```

Without these, submodule directories appear empty. This silently breaks builds that depend on submodule content with no clear error message.

## 14. Output Lookup Priority for `nix run`

`nix run .#foo` searches in this order: `apps.${system}.foo` → `packages.${system}.foo` → `legacyPackages.${system}.foo`. First match wins. This can cause unexpected behavior if a package and an app have the same name — the `apps` entry will shadow the package.

## 15. `nix develop path:` Bypasses Eval Cache

Commands of the form `nix develop path:/some/dir` (explicit path-based flake reference) skip the evaluation cache entirely. Computing a content fingerprint for a path flake requires hashing the entire directory, which is slower than using the git-based fingerprint. Always use `nix develop` (implicit current dir) inside a git repo.
