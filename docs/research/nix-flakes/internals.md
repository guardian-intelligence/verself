# Nix Flakes Internals

Source-code level analysis of how flakes work. All citations are to `github.com/NixOS/nix` unless otherwise noted.

## `flake.lock` Format

Lock file version is JSON at version 7 (versions 5-7 are supported; `< 5` or `> 7` throws). Source: `src/libflake/lockfile.cc`.

Top-level structure:
```json
{
  "version": 7,
  "root": "root",
  "nodes": {
    "root": { "inputs": { "nixpkgs": "nixpkgs", "flake-utils": "flake-utils" } },
    "nixpkgs": {
      "original": { "type": "github", "owner": "NixOS", "repo": "nixpkgs", "ref": "nixos-unstable" },
      "locked":   { "type": "github", "owner": "NixOS", "repo": "nixpkgs",
                    "rev": "7f8d4b088e2df7fdb6b513bc2d6941f1d422a013",
                    "lastModified": 1580555482,
                    "narHash": "sha256-OnpEWzNxF/AU4KlqBXM2s5PWvfI5/BS6xQrPvkF5tO8=" },
      "inputs": {},
      "flake": true
    }
  }
}
```

**Non-obvious fields:**
- `original`: the **unresolved** ref as written in `flake.nix`. Not a content address.
- `locked`: the fully resolved ref including `rev`, `narHash`, `lastModified`. All fetcher-specific attrs included.
- `flake`: boolean, defaults `true`, only written when `false` (e.g., non-flake inputs like `flake = false` sources).
- `parent`: optional `InputAttrPath`, only written for relative path inputs (`path:../foo`). Undocumented in most guides.
- The root node has no `original`/`locked` fields. Source: `lockfile.cc:dumpNode` lambda.
- `follows` is stored as a **JSON array** of path components in the lock file, not as a duplicate locked node. It is a pointer, not a copy.

## `narHash` vs `rev`

These serve different verification roles and are NOT interchangeable:

- `rev`: A Git commit SHA-1 (40 hex chars). Verifies *which commit* was fetched. Does not prove content — a malicious server could serve a different tree for the same commit hash (relevant for non-GitHub fetchers or self-hosted Git).
- `narHash`: SHA-256 of the NAR (Nix Archive) serialization of the entire source tree, in SRI format (`sha256-<base64>`). This is a *content hash* of the exact bytes that land in the Nix store. Verifies content integrity independently of the fetching protocol.

From `lockfile.cc` (`LockedNode::LockedNode` constructor):
```cpp
if (!lockedRef.input.isLocked(fetchSettings) && !lockedRef.input.isRelative()) {
    if (lockedRef.input.getNarHash())
        warn("Lock file entry '%s' is unlocked (e.g. lacks a Git revision) but is checked by NAR hash...");
    else
        throw Error("Lock file contains unlocked input '%s'...");
}
```

A `LockedNode` can be valid with only a `narHash` and no `rev` (e.g., tarball inputs). For GitHub inputs, both are always present. The `narHash` is the authoritative content seal; `rev` is a supplementary pointer. The eval cache (see below) uses `rev` as the primary fingerprint for git inputs, not `narHash`.

## `follows` Resolver

**Parsing level** (`src/libflake/flake.cc`, `parseFlakeInput`, lines ~178-182):
```cpp
auto follows(parseInputAttrPath(attr.value->string_view()));
follows.insert(follows.begin(), lockRootAttrPath.begin(), lockRootAttrPath.end());
input.follows = follows;
```

The `lockRootAttrPath` prefix is prepended. So if you're in a nested input context, a `follows = "nixpkgs"` becomes `["parentInput", "nixpkgs"]`. `follows` paths are **absolute** from the lock file root, not relative to the declaring flake.

**Locking level** (`computeLocks` in `flake.cc`, lines ~573-582):
```cpp
if (input.follows) {
    InputAttrPath target;
    target.insert(target.end(), input.follows->begin(), input.follows->end());
    node->inputs.insert_or_assign(id, target);
    continue;
}
```

When `computeLocks` encounters a `follows`, it inserts a `Node::Edge` variant holding an `InputAttrPath` — not a `ref<LockedNode>`. The actual resolution happens in `LockFile::findInput` via `doFind`, which walks the lock graph following path indirections. `doFind` detects cycles (throws `Error: follow cycle detected`).

**Diamond dependency resolution**: When two inputs both `follow = "nixpkgs"`, they both point to the same `InputAttrPath` in the lock graph, resolving to exactly the same store path. This is structurally different from npm/cargo dependency resolution: there is no semver merging, just pointer identity. If both inputs share the same nixpkgs, they build from the same binary cache entries. If they have separate nixpkgs inputs, they each have their own and binary cache may not be hit.

The correct pattern for this flake to ensure a single nixpkgs:
```nix
inputs = {
  nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  flake-utils.url = "github:numtide/flake-utils";
  flake-utils.inputs.nixpkgs.follows = "nixpkgs";  # prevents second nixpkgs copy
};
```
(Note: `flake-utils` actually has no `nixpkgs` input as of current version — but the pattern matters when using `home-manager`, `devenv`, etc.)

## `self` Assembly

From `src/libflake/call-flake.nix` in the Nix source:

```nix
self = rec {
  outputs = flake.outputs (inputs // { self = outputs // sourceInfo // { ... }; });
  # ...
  __internal = { inherit locks lockedFlake fetchedInputs; };
};
```

The merge order is: `outputs` first, then `sourceInfo` fields override (which includes `outPath`, `lastModified`, `narHash`). This means if your flake accidentally outputs an attribute named `outPath`, `sourceInfo.outPath` will shadow it. The `self` passed into `outputs` is the final merged attribute set.

`self.outPath` is the store path of the flake source tree (same as `builtins.toString self`). For a git input, this is the NAR-extracted source. For a path input (local dev), this is the filtered source tree after applying the `AllowListSourceAccessor`.

`self.packages` is not a copy; it is literally `self.outputs.packages`. No indirection. Accessing `self.packages.x86_64-linux.default` from within the flake's own output is perfectly valid (lazy) — the evaluator only forces the thunk when it's needed.

The lazy self-reference trap: `outputs = self: { x = self.x + 1; }` causes infinite recursion at force time. But `outputs = self: { foo = self.packages.default; }` is fine because the value is a thunk, not evaluated until `foo` is forced.

## Eval Cache

**Fingerprint construction** (`src/libflake/flake.cc`, `LockedFlake::getFingerprint`, lines ~979-1001):

```cpp
std::optional<Fingerprint> LockedFlake::getFingerprint(...) const
{
    if (lockFile.isUnlocked(fetchSettings))
        return std::nullopt;  // no cache for unlocked flakes

    auto fingerprint = flake.lockedRef.input.getFingerprint(store);
    if (!fingerprint)
        return std::nullopt;  // no cache for local path flakes

    *fingerprint += fmt(";%s;%s", flake.lockedRef.subdir, lockFile);
    // lockFile serializes the entire lock graph as JSON inline

    if (auto revCount = flake.lockedRef.input.getRevCount())
        *fingerprint += fmt(";revCount=%d", *revCount);
    if (auto lastModified = flake.lockedRef.input.getLastModified())
        *fingerprint += fmt(";lastModified=%d", *lastModified);

    return hashString(HashAlgorithm::SHA256, *fingerprint);
}
```

For a git input, the fingerprint string is approximately:
```
<gitrev>[;s][;e][;l];<subdir>;<lockfile-json-serialization>[;revCount=N][;lastModified=N]
```

This string is SHA-256 hashed to become the SQLite filename.

**SQLite cache location** (`src/libexpr/eval-cache.cc`):
```cpp
auto cacheDir = getCacheDir() / "eval-cache-v6";
auto dbPath = cacheDir / (fingerprint.to_string(HashFormat::Base16, false) + ".sqlite");
```

Default `getCacheDir()` is `~/.cache/nix`. Database at `~/.cache/nix/eval-cache-v6/<sha256>.sqlite`.

**Schema** — the `Attributes` table:
```sql
create table if not exists Attributes (
    parent  integer not null,
    name    text,
    type    integer not null,   -- FullAttrs=0, String=1, Bool=2, Int=3, ListOfStrings=4,
                                --  Placeholder=5, Missing=6, Failed=7
    value   text,
    context text,
    primary key (parent, name)
);
```

**What invalidates the cache:**
1. Any change to `flake.lock` (lock JSON is embedded verbatim in fingerprint)
2. Any new git commit (changes `rev`)
3. Changes to `revCount` or `lastModified`
4. The eval cache is disabled entirely if `pureEval` is false (i.e., `nix develop`, `nix build path:.` with impure eval never hit cache)
5. Local path flakes (`path:.`) never have a fingerprint — cache is disabled

**Practical implication for this repo**: `nix develop .` run from the repo directory uses a `path:` flake ref, so the eval cache is always bypassed. `nix develop github:owner/repo` or a pinned commit would use the cache. The `nix-direnv` tool (for `direnv` integration) works around this by pinning the flake to its own commit ref and storing a GC root to prevent the SQLite file from being collected during `nix-collect-garbage`.

## FlakeRef Parsing Order

`parseFlakeRefWithFragment` tries three parsers in order (`flakeref.cc`):

1. `parseFlakeIdRef` — matches `flake-id[/ref][#fragment]` syntax (e.g., `nixpkgs`, `nixpkgs/nixos-24.05`). Produces `FlakeRef` with `scheme = "flake"` (indirect type). Resolved via registry lookup.
2. `parseURLFlakeRef` — matches explicit URL schemes (`github:`, `gitlab:`, `git+https:`, `tarball+https:`, etc.)
3. `parsePathFlakeRefWithFragment` — filesystem paths. If no `.git` is found walking upward, uses `scheme = "path"`. If `.git` is found, re-encodes as `scheme = "git+file"` with flake `subdir` derived from the relative path. This is why subdirectory flakes in a git repo get `dir=subdir` in their flake ref.

The `dir` query parameter is stripped from the URL and stored separately as `FlakeRef::subdir` — it belongs to the flake ref layer, not the fetcher layer. `canonicalize()` in `flakeref.cc` handles old lock files that incorrectly embedded `?dir=subdir` inside the fetcher URL.

## Registry Lookup Order

Source: `src/libfetchers/registry.cc`, `getRegistries`, lines ~168-172:

```cpp
registries.push_back(getFlagRegistry());      // --override-flake CLI flag
registries.push_back(getUserRegistry());      // ~/.config/nix/registry.json
registries.push_back(getSystemRegistry());    // /etc/nix/registry.json
registries.push_back(getGlobalRegistry());    // channels.nixos.org/flake-registry.json
```

Lookup iterates in order (Flag first = highest precedence) and uses `goto restart` on any match to allow chained resolutions (up to 100 hops before cycle error). For `UseRegistries::Limited` (used for nested inputs during locking), only Flag and Global registries are consulted — User and System registries are skipped. This is intentional: nested inputs should not be affected by the user's local overrides.

## `builtins.fetchTree` and Feature Flag Coupling

`builtins.fetchTree` is artificially gated behind the `flakes` feature flag in the Nix evaluator (issue [NixOS/nix#5541](https://github.com/NixOS/nix/issues/5541)). This means you cannot use `builtins.fetchTree` (which is the lower-level primitive underlying all flake input fetching) without enabling flakes. This is widely criticized as an arbitrary coupling — `fetchTree` is useful independently of flakes. The issue was acknowledged by maintainers but not resolved as of early 2026.

Practical consequence: if you want to use `fetchTree` in a non-flake context (e.g., a `default.nix` that needs to fetch a git repo by rev), you must enable flakes anyway, or use the older `builtins.fetchGit` which has a slightly different interface.
