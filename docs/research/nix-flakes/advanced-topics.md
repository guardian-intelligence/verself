# Nix Advanced Topics: Source-Level Research

Focused research on `buildGoModule`, `nix copy` internals, `buildEnv`, CA derivations, `nixConfig` security, lazy trees, and `nix flake metadata`. All source citations are to `github.com/NixOS` unless otherwise noted.

---

## Topic 1: `buildGoModule` and `vendorHash`

Source: `pkgs/build-support/go/module.nix` in nixpkgs.

### What `vendorHash` actually hashes

`buildGoModule` is a two-phase build. The first phase creates a derivation named `${pname}-go-modules` that runs either `go mod vendor` (default) or `go mod download` (`proxyVendor = true`) inside a fixed-output derivation (FOD).

The FOD hash (`vendorHash`) is set as:
```nix
outputHashMode = "recursive";
outputHash = finalAttrs.vendorHash;
```

`outputHashMode = "recursive"` means the NAR serialization of the *entire output directory* is hashed with SHA-256. For the default (non-`proxyVendor`) path, the output directory is literally the `vendor/` tree. For `proxyVendor`, it is `$GOPATH/pkg/mod/cache/download` minus the `sumdb/` subdirectory.

The final build derivation then uses this verified tree:
- `proxyVendor = false`: `cp -r --reflink=auto "$goModules" vendor` — copies the verified vendor dir in place
- `proxyVendor = true`: `export GOPROXY="file://$goModules"` — points Go at the verified module cache

`GOPROXY=off` and `GOSUMDB=off` are set in the final build, so no network access is possible. `-mod=vendor` is appended to `GOFLAGS` automatically when `proxyVendor = false`.

### `vendorHash = null`

When `vendorHash = null`, `goModules` evaluates to `""` (empty string):
```nix
goModules =
  if (finalAttrs.vendorHash == null) then
    ""
  else
    (stdenv.mkDerivation { ... });
```

This signals that the source tree already contains a `vendor/` directory. The first FOD phase is skipped entirely. The final build uses the pre-vendored directory from `src`. The build will fail with `exit 10` if no `vendor/` directory is present when `vendorHash = null`.

**Gotcha**: If a `vendor/` directory exists in the source but `vendorHash` is set to a non-null value, the module-fetch phase will also fail with `exit 10` because the script detects the pre-existing vendor dir and errors out (unless `deleteVendor = true`).

### `proxyVendor = true` vs default

| | `proxyVendor = false` (default) | `proxyVendor = true` |
|--|--|--|
| Fetch command | `go mod vendor` | `go mod download` |
| Output content | `vendor/` directory | `$GOPATH/pkg/mod/cache/download` |
| Final build | `-mod=vendor` flag | `GOPROXY=file://$goModules` |
| Use case | Pure Go deps | CGO deps, case-insensitive FS conflicts |
| Hash stability | Stable across platforms | **Platform-dependent on Go 1.22+** |

The Go 1.22 platform-dependency issue (nixpkgs #300640): in Go 1.22, the ordering of some internal struct fields changed, causing `proxyVendor` output to differ between Go 1.21 and 1.22 builds. Anyone using `proxyVendor = true` with a Go 1.22 bump needed to recompute `vendorHash`.

The CGO use case: `go mod vendor` only vendors Go source files. It deliberately excludes C sources in vendored packages. If a Go dep wraps a C library (e.g., via cgo), `go mod vendor` leaves out the `.h` and `.c` files. `proxyVendor = true` downloads the full module cache including C sources, so the linker can find them.

### `gomod2nix` vs `buildGoModule`

`buildGoModule` bundles all dependencies into a single FOD. Every dependency change invalidates the entire `vendorHash` and refetches everything.

`gomod2nix` (`github.com/nix-community/gomod2nix`) takes a different approach:
- A CLI tool (`gomod2nix`) reads `go.mod`/`go.sum` and generates a `gomod2nix.toml` file that lists each module with its own hash.
- Each module gets its **own fixed-output derivation** in the Nix store.
- Dependencies are linked by symlinks, not copied into a single vendor tree.
- The builder function is `buildGoApplication` (not `buildGoModule`).

Result: adding one new dependency only fetches that one module. The other 50 modules remain cached. With `buildGoModule`, the entire vendor hash changes on any dep change.

The downside: `gomod2nix.toml` must be regenerated every time `go.mod` changes. This adds a generation step to the workflow.

**When to use which for forge-metal's Go binary (`cmd/bmci`)**:
- For a project with stable, slow-changing deps: `buildGoModule` with `vendorHash` is simpler.
- For a project with frequent dep changes or CGO dependencies: `gomod2nix` gives better CI cache reuse.
- The current `cmd/bmci` has no CGO and modest deps, so `buildGoModule` is appropriate unless dep churn becomes a problem.

---

## Topic 2: `nix copy` over SSH

### Protocol: `ssh://` vs `ssh-ng://`

There are two SSH store types:

**`ssh://` (legacy, default for `nix copy --to ssh://host`)**
- URL: `ssh://[user@]host[:port]`
- Remote command: `nix-store --serve --write`
- Protocol: `ServeProto` (the "serve protocol"), a limited subset
- Source: `src/libstore/legacy-ssh-store.cc` — uses `ServeProto::BasicClientConnection`

**`ssh-ng://` (experimental, full access)**
- URL: `ssh-ng://[user@]host[:port]`
- Remote command: `nix-daemon --stdio`
- Protocol: `WorkerProto` (full daemon protocol)
- Source: `src/libstore/ssh-store.cc` — extends `RemoteStore` directly
- Access: full store operations, not just substitution

For `nix copy`, both work. `ssh-ng` is preferred for correctness (all daemon features) and may become default eventually (issue #8035). On the remote side, `ssh-ng` requires a running Nix installation with `nix-daemon`.

### Compression

Compression is **off by default**. Source: `src/libstore/ssh-store` documentation — the `compress` option defaults to `false`.

Enabling it: append `?compress=true` to the store URL:
```bash
nix copy --to "ssh://host?compress=true" /nix/store/...
```

When `compress = true`, the `SSHMaster` passes `-C` to the `ssh` command (source: `ssh.cc`, `addCommonSSHOpts`):
```cpp
if (compress)
    args.push_back(OS_STR("-C"));
```

This is OpenSSH's built-in `-C` (zlib compression), applied at the SSH transport layer. It compresses the NAR byte stream inside the SSH tunnel.

**Why it is off by default**: NAR content is often already-compressed binaries and ELF objects. SSH zlib compression on pre-compressed data wastes CPU with minimal or negative benefit. On a LAN pushing a 2GB Nix closure, the bandwidth savings are small and CPU overhead is real. Over WAN with high-latency links, compression can help.

Additional options via `NIX_SSHOPTS` environment variable (applied to every SSH invocation):
```bash
NIX_SSHOPTS="-o Compression=yes -o CompressionLevel=6" nix copy --to ssh://host ...
```

### The NAR format

NAR (Nix Archive) is a deterministic, canonical alternative to tar designed for content addressing.

**Wire format** (from `src/libutil/archive.cc` and the NAR spec):

The magic header is the string `"nix-archive-1"`. All strings are length-prefixed and padded:
```
str(s) = uint64_le(len(s)) || s || zero_padding_to_8_bytes
```

EBNF grammar:
```
nar        = str("nix-archive-1"), nar-obj
nar-obj    = str("("), nar-obj-inner, str(")")
nar-obj-inner
           = str("type"), str("regular"),
             [str("executable"), str("")],
             str("contents"), str(bytes)
           | str("type"), str("symlink"), str("target"), str(target)
           | str("type"), str("directory"),
             {str("entry"), str("("),
              str("name"), str(name),
              str("node"), nar-obj, str(")")
             }
```

Directory entries are sorted lexicographically. This strict ordering is what makes NAR canonical — given the same file tree, there is exactly one possible NAR serialization.

**Key differences from tar**:
- Tar embeds timestamps, UID/GID, file mode beyond executable bit, and extended attributes. NAR strips all of this. Two machines building the same derivation produce bit-identical NARs.
- Tar allows multiple entries for the same path (last wins or undefined). NAR has no such ambiguity.
- Tar is a stream format with no canonical ordering. NAR has strict alphabetical directory ordering.
- NAR only models three file types: regular file, symlink, directory. Device nodes, FIFOs, and sockets are unsupported (throw at serialization time).
- NAR preserves only the executable bit (not read/write/setuid/setgid).

**What `nix copy` transfers**: for each store path being copied, Nix serializes it as a NAR, frames it in the wire protocol as length-delimited chunks, and sends it over the SSH tunnel. The receiving side verifies the NAR hash against the path info it fetched first.

**NAR hashes in `flake.lock`**: the `narHash` field in `flake.lock` is the SHA-256 of the NAR serialization of the fetched source tree, in SRI format (`sha256-<base64>`). This is computed by the fetcher after extracting the source.

---

## Topic 3: `buildEnv` Internals

Source: `pkgs/build-support/buildenv/default.nix` and `builder.pl` in nixpkgs.

### What `buildEnv` does

`buildEnv` creates a directory containing symlinks into the store paths of specified packages. The entire builder is a Perl script (`builder.pl`) invoked via `runCommand`. The Nix expression sets environment variables; the Perl script reads them and creates `$out`.

The core data structure is `%symlinks`, a hash mapping relative path → `[target_store_path, priority]`.

### `ignoreCollisions` behavior

The collision handling in `builder.pl`:

```
ignoreCollisions = false (default):
  Two packages provide the same non-directory path:
    if checkCollisionContents = true: compare file contents bit-for-bit
      - identical contents → skip (no error), keep first
      - different contents → die "two given paths contain a conflicting subpath"
    if checkCollisionContents = false: always die

ignoreCollisions = true:
  Emit a warning but continue. The first package in `paths` order wins.
  Note: ignoreCollisions==1 warns; the propagated-packages loop uses ignoreCollisions==2
  (silently ignore, no warning) for lower-priority propagated deps.
```

Relevant Perl:
```perl
if ($ignoreCollisions) {
    warn "colliding subpath (ignored): $targetRef and $oldTargetRef\n" if $ignoreCollisions == 1;
    return;
} elsif ($checkCollisionContents && checkCollision($oldTarget, $target)) {
    return;
} else {
    die "two given paths contain a conflicting subpath:\n  $targetRef and\n  $oldTargetRef\n...";
}
```

**Gotcha**: `ignoreCollisions = true` silently drops the second file — not a merge. If you have two packages where one provides `/bin/foo` (version A) and another provides `/bin/foo` (version B), only the first in `paths` will be symlinked. This can cause subtle runtime errors where you thought you were using version B but got version A.

**`checkCollisionContents = true` (default)**: files with the same path but identical bytes are silently accepted. This handles the common case of two packages both shipping the same license file. `File::Compare` is used for byte-level comparison.

### `pathsToLink` filter

`pathsToLink` defaults to `["/"]`, meaning all paths are symlinked. When set to e.g. `["/bin" "/share/man"]`:

The Perl script uses two functions:
- `isInPathsToLink($path)`: true if `$path` is exactly one of the elements, or is a subdirectory of one.
- `hasPathsToLink($path)`: true if `$path` is a prefix of any element. Used to traverse down to the target directories.

This means `pathsToLink = ["/bin"]` will:
1. Create `$out` as a directory (the root `""` is always added).
2. Traverse into any `bin/` directory found in any input package.
3. Create `$out/bin/` as a real directory.
4. Symlink individual files inside `bin/` to their source package paths.

Files in `/lib`, `/share`, etc. are entirely skipped — not even traversed.

**Gotcha with `pathsToLink`**: if a package ships a binary in `/bin/` but its runtime data in `/share/foo/`, using `pathsToLink = ["/bin"]` will include the binary symlink but the binary may fail at runtime if it tries to find its data files via the path in `buildEnv`'s output.

### `buildEnv` vs `symlinkJoin`

`symlinkJoin` (from `pkgs/build-support/trivial-builders/default.nix`) is a thin wrapper around `buildEnv`:

```nix
symlinkJoin = args_@{
  name,
  paths,
  preferLocalBuild ? true,
  allowSubstitutes ? false,
  postBuild ? "",
  ...
}: buildEnv {
  inherit name paths postBuild;
  ignoreCollisions = true;   # ← KEY difference
};
```

`symlinkJoin` hard-codes `ignoreCollisions = true`. It is intended for combining packages where collisions are expected and the first-one-wins behavior is acceptable. `buildEnv` exposes collision handling as a parameter.

Additional `buildEnv`-only features not available in `symlinkJoin`:
- `pathsToLink` (filter to specific subdirectories)
- `extraOutputsToInstall` (include non-default outputs like `dev`, `man`)
- `extraPrefix` (root the result under a subdirectory, e.g., `"$out/share"`)
- `includeClosures` (add all transitive closure paths to the env)
- `manifest` (create a `$out/manifest` symlink for Nix profile compatibility)
- `checkCollisionContents` (bit-level comparison before erroring)

**For `server-profile` in this repo**: `buildEnv` with specific `pathsToLink` is appropriate to ensure only desired paths are symlinked and collision errors surface immediately. `symlinkJoin` would silently shadow conflicting files.

### Priority system

Each package in `paths` gets a `meta.priority` attribute (default: `lib.meta.defaultPriority`, which is 5). Lower numbers = higher priority. When two packages conflict on a non-directory path:
- If the incoming package has **lower** priority number (higher priority), it overwrites the existing symlink.
- If it has **higher** priority number (lower priority), it is skipped.
- If equal priority: collision handling logic applies (warn or die).

The `propagated-user-env-packages` mechanism: packages can declare runtime dependencies that should also be linked into the env. These are processed after all explicit `paths` entries and assigned incrementing priority counters starting at 1000, so they never override explicit packages.

---

## Topic 4: Content-Addressed (CA) Derivations

### Input-addressed vs content-addressed

**Input-addressed (traditional)**: store path derived from the hash of the derivation inputs — the name, builder, environment variables, dependencies. The path is computed *before* the build runs. Rebuilding the same derivation always produces the same path, but two different compilers producing bit-identical output get different paths.

**Content-addressed (CA)**: store path derived from the hash of the actual *output content* (the NAR). The path is not known until after the build completes. Two different derivations producing byte-identical output get the same store path.

### Enabling CA derivations

CA derivations are behind the `ca-derivations` experimental feature flag. In nixpkgs-unstable, a derivation can opt in:

```nix
stdenv.mkDerivation {
  __contentAddressed = true;
  outputHashAlgo = "sha256";
  outputHashMode = "recursive";
  # Note: outputHash must NOT be set (that would be a fixed-output derivation)
  ...
}
```

This is distinct from a **fixed-output derivation** (FOD) which has both `outputHashAlgo` and `outputHash` set. A FOD is for fetchers: you declare what the output must be. A CA derivation computes the hash after the build and stores the result keyed by that hash.

The distinction from CA manual:
- **Fixed CA**: hash is declared in the derivation (`outputHash` is set). Build fails if output does not match. Used for fetchers (`fetchurl`, `fetchgit`, etc.).
- **Floating CA**: hash is not declared. Nix computes it after the build and uses it to form the store path. Requires `ca-derivations` experimental flag.

`config.contentAddressedByDefault = true` (a nixpkgs overlay option) marks all `mkDerivation` calls as CA, enabling whole-nixpkgs CA builds. This is experimental and not used in production nixpkgs.

### Why CA derivations enable better binary cache sharing

With input-addressing, two people building with slightly different `stdenv` (e.g., one has an extra patch) produce derivations with different hashes even if the output is identical. Their store paths differ, so binary cache entries are not shared.

With content-addressing, the store path is `$(hash(output_content))`. If two derivations (from different build graphs) happen to produce identical output, they resolve to the same store path. A binary cache hit works regardless of where the derivation came from.

**Early cutoff optimization**: with CA derivations, if a dependency changes but the dependency's output is unchanged (e.g., a comment change in a library → rebuild → same `.so`), the store path of the output is unchanged, so all downstream dependents are cache hits. With input-addressing, any change in a dependency's derivation invalidates all downstream paths even if the actual binary is identical.

This is the primary motivation for CA derivations in large nixpkgs builds: avoid cascading rebuilds caused by non-semantic changes to dependencies.

### Trust model change

Input-addressed paths: when you substitute from a binary cache, you trust the cache server and the signature. If the cache is compromised, you get a malicious binary at the expected path.

CA paths: the store path *is* the hash of the content. If the cache returns something with the wrong content, the hash check fails and the substitution is rejected. This enables "trustless" binary cache sharing — multiple users sharing a store do not need to trust each other's builds, only the content hashes.

Source: NixOS Wiki Ca-derivations — "CA paths enable several users to share the same store without trusting each other."

### Current status (as of early 2026)

`ca-derivations` is still experimental. Required nix.conf settings:
```
experimental-features = nix-command flakes ca-derivations
```

Nixpkgs does not yet use floating CA derivations in `nixpkgs-unstable` for the main package set. Only select packages or overlays use `__contentAddressed`. The feature is considered stable enough for production use by Determinate Systems but is not merged into the upstream Nix stable branch.

---

## Topic 5: `nixConfig` in `flake.nix` — Security Model

### What `nixConfig` is

`nixConfig` is an attribute set in `flake.nix` that requests Nix configuration changes when the flake is evaluated:

```nix
nixConfig = {
  extra-substituters = [ "https://my-cache.example.com" ];
  extra-trusted-public-keys = [ "my-cache.example.com:abcdef..." ];
};
```

When `nix` encounters a flake with `nixConfig`, it prompts the user:
```
do you want to allow configuration setting 'extra-substituters' to be set to '...' (y/N)?
```

### Whitelisted settings (no prompt needed)

Only a small set of settings can be applied without any confirmation prompt regardless of `accept-flake-config`:
- `bash-prompt`
- `bash-prompt-prefix`
- `bash-prompt-suffix`
- `flake-registry`
- `commit-lockfile-summary`

All other settings require either user confirmation per-invocation or `accept-flake-config = true` in nix.conf.

### `accept-flake-config` risk

`accept-flake-config = true` (in `nix.conf` or `nix.settings.accept-flake-config` in NixOS) bypasses all prompts and automatically applies every `nixConfig` setting in every flake. From the security analysis:

> "Being able to modify your nix.conf on your system is equivalent to having full control of the Nix daemon."

Attack vectors with `accept-flake-config = true`:
1. **Malicious substituter**: add `extra-substituters = ["https://attacker.example.com"]` with a trusted key. All builds will attempt to fetch from the attacker's cache, which can serve compromised binaries with valid signatures (using the planted key).
2. **Bypass signature checking**: set `require-sigs = false` to accept unsigned store paths from any cache.
3. **Hook injection**: `post-build-hook` can only be set in system `nix.conf` or by trusted users — but settings from `nixConfig` run at elevated context in some configurations.

### Trusted vs untrusted settings

Nix settings have three privilege levels:
1. **Freely settable**: `max-jobs`, `cores`, `build-dir`, etc. Can be set by any user.
2. **Trusted-user only** (require `trusted-users` membership): `allowed-users`, `secret-key-files`, `post-build-hook`, `diff-hook`, `run-diff-hook`. These can only be set in system `nix.conf` or by a user listed in `trusted-users`.
3. **`nixConfig`-settable with prompt**: most settings fall here — settable via flake `nixConfig` after user confirmation or with `accept-flake-config = true`.

Adding any user to `trusted-users` is essentially equivalent to giving them root on the Nix store. The daemon runs as root and executes builds; trusted users can direct it to run arbitrary code.

### Practical guidance for this repo

The `flake.nix` in forge-metal does not need `nixConfig` for correctness. If added in the future (e.g., to advertise a binary cache):
- Never set `accept-flake-config = true` on any host.
- Only add `extra-substituters` and `extra-trusted-public-keys` — and rotate the signing key regularly.
- The operator running `make deploy` will see the prompt once per new setting; this is the correct security UX.

---

## Topic 6: Lazy Trees / Lazy Fetch

### What the problem is (eager fetching)

Currently (standard Nix), evaluating any flake expression causes all declared inputs to be fetched and copied to the Nix store immediately, even if those inputs are only referenced by a small fraction of the flake's outputs. For a flake with 10 inputs (nixpkgs, flake-utils, home-manager, etc.), evaluating `nix build .#myPackage` fetches all 10 inputs upfront.

For nixpkgs itself (a common input), this means copying several hundred MB of source into the store on every new machine or after a `nix flake update`, even if only one package from nixpkgs is needed.

### What lazy trees does

Lazy trees (implemented in Determinate Nix; PR #13225 for upstream Nix) introduces a virtual filesystem layer:

1. Each flake input gets a "virtual" store path at a randomized location (e.g., `/nix/store/<random-hash>-source`) that does not exist on disk.
2. During evaluation, Nix reads files through this virtual layer on demand. The virtual FS fetches only the specific files that evaluation actually accesses.
3. Only when a virtual store path is referenced by a concrete derivation (i.e., used as `src =`) does Nix perform "devirtualization" — materializing the actual content at a content-addressed path.

The key change from lazy trees v1 (PR #6530): v1 used separate root filesystems per source tree, causing backward compatibility issues. v2 (PR #13225) mounts virtual paths inside `/nix/store/` itself, which avoids the compatibility problems.

### Performance numbers (from Determinate Nix benchmarks)

| Metric | Without lazy trees | With lazy trees | Improvement |
|--------|-------------------|-----------------|-------------|
| Wall time (eval) | ~10.8s | ~3.5s | ~3x faster |
| Disk usage | 433 MB | 11 MB | ~40x less |

General claim: "3x or more wall-time reduction and 20x or more disk usage reduction for nixpkgs evaluations."

### Status and gotchas

- **Determinate Nix**: shipped in 3.5.2, rolled out to 20% of users in 3.8.0 (July 2025).
- **Upstream Nix**: PR #13225 not merged as of early 2026. Core maintainer roberth's objection: using random hashes for virtual paths introduces non-determinism into language semantics — two evaluations of the same expression can see different virtual paths, violating Nix's referential transparency guarantees.
- **`src = ./.` warning**: lazy trees emits warnings for `src = ./.` patterns because this lacks a proper string context to trigger devirtualization. The recommended pattern becomes:
  ```nix
  src = builtins.path { path = ./.; name = "source"; };
  ```
- **`toString <path>`**: the PR introduces a new string context type for devirtualization. `toString ./some/file` in a lazy context doesn't create a store reference but still triggers a warning.

### Relationship to RFC 0133

RFC 0133 is the Nix RFC that proposed lazy fetching of flake inputs. It predates and motivated the lazy trees implementation. The RFC described the desired semantics; lazy trees is the implementation attempt. The RFC itself is in "draft" status and its formal disposition tracks the upstream PR #13225 merge status.

### Impact on forge-metal

The `nix copy .#server-profile` and `nix develop .` commands in this repo run from a local path flake. With lazy trees, running `make deploy` (which builds and copies the server profile) would only materialize nixpkgs source files actually referenced by the `server-profile` closure, rather than the full nixpkgs source tree.

Given that `server-profile` is a large multi-package closure, the practical speedup would be modest at evaluation time — most inputs are genuinely needed. The larger win is for `nix develop .`, where only the dev shell derivation is evaluated and most of nixpkgs' source content is not needed.

---

## Topic 7: `nix flake metadata` vs `nix flake show` vs `nix flake info`

### `nix flake info`

`nix flake info` is an alias for `nix flake metadata`. They are the same command. Source: NixOS/nix issue #4613 ("`nix flake list-inputs` + `nix flake info` = `nix flake metadata`").

### `nix flake metadata` — JSON structure

`nix flake metadata --json` outputs:

```json
{
  "description": "string from flake.nix description field",
  "lastModified": 1748000000,
  "locked": {
    "lastModified": 1748000000,
    "narHash": "sha256-...",
    "owner": "NixOS",
    "repo": "nixpkgs",
    "rev": "abc123...",
    "type": "github"
  },
  "locks": { ... },
  "original": {
    "id": "nixpkgs",
    "type": "indirect"
  },
  "originalUrl": "flake:nixpkgs",
  "path": "/nix/store/...-source",
  "resolved": {
    "owner": "NixOS",
    "repo": "nixpkgs",
    "type": "github"
  },
  "resolvedUrl": "github:NixOS/nixpkgs",
  "revision": "abc123...",
  "revCount": 691234,
  "url": "github:NixOS/nixpkgs/abc123..."
}
```

**Fields explained**:
- `original` / `originalUrl`: the flake reference exactly as written by the user or in `flake.nix`. For `nixpkgs` (an indirect ref), `type = "indirect"`.
- `resolved` / `resolvedUrl`: the flake reference after registry lookup (e.g., `nixpkgs` → `github:NixOS/nixpkgs`). For non-registry refs, equals `original`.
- `locked` / `url`: the fully pinned reference including `rev` and `narHash`. This is the value stored in `flake.lock`.
- `description`: the `description` attribute from the flake's `flake.nix`. Empty string if absent.
- `path`: the Nix store path where the flake source is stored after fetching.
- `revision`: Git/Mercurial commit hash. Absent for tarball inputs.
- `revCount`: number of ancestors of the commit (git rev-list count). Not available for GitHub flakes via the REST API (only via full git clone).
- `lastModified`: Unix timestamp of the commit for VCS inputs; most recent file mtime for tarballs.
- `locks`: the complete parsed contents of `flake.lock` — the same JSON structure as the lock file itself (version, nodes, root).

**Non-obvious**: `locks` includes all transitive inputs, not just direct inputs. Running `nix flake metadata .` on forge-metal's flake will show the full resolved lock tree including nixpkgs' own inputs (if any).

### `nix flake show`

`nix flake show` evaluates the flake and displays its *outputs* (packages, devShells, checks, apps, overlays, nixosModules, etc.) as a tree:

```
github:NixOS/nixpkgs/...
├───legacyPackages
│   ├───aarch64-darwin: Attr set with 101153 packages
│   ├───aarch64-linux: Attr set with 100645 packages
│   ├───x86_64-darwin: Attr set with 100898 packages
│   └───x86_64-linux: Attr set with 100994 packages
└───...
```

Key differences:

| | `nix flake metadata` | `nix flake show` |
|--|--|--|
| What it shows | Flake source metadata and inputs | Flake output schema |
| Evaluation | Fetches the flake, reads `flake.nix` partially | Fully evaluates all outputs |
| Network | May fetch to get lock info | Same, plus evaluates outputs |
| JSON | Lock structure, paths, hashes | Output attribute tree with types |
| Gotcha | None | Fails on `legacyPackages` (too large) unless `--allow-import-from-derivation` or `--legacy` |

`nix flake show --json` output schema:
```json
{
  "packages": {
    "x86_64-linux": {
      "default": {
        "description": "...",
        "name": "bmci-0.1.0",
        "type": "derivation"
      }
    }
  },
  "devShells": { ... },
  "apps": { ... }
}
```

The `type` field in `show` output can be: `"derivation"`, `"nixos-configuration"`, `"nixos-module"`, `"app"`, `"template"`, or `"unknown"`.

**Practical use**: `nix flake metadata` is what you use to inspect lock file contents programmatically or check if an input is up to date. `nix flake show` is what you use to discover what a flake exports. For CI systems, `nix flake metadata --json | jq .locks.nodes.nixpkgs.locked.rev` is a common idiom to extract the pinned nixpkgs commit.

---

## Cross-Cutting Notes for forge-metal

### `buildGoModule` and `cmd/bmci`

The `cmd/bmci` binary is currently built in `flake.nix` as a `buildGoModule` derivation. The `vendorHash` must be updated whenever `go.mod` or `go.sum` changes. The correct workflow:

```bash
# After updating go.mod:
nix build .#bmci 2>&1 | grep "got:"
# Copy the "got:" hash and set vendorHash to it
```

Or use `vendorHash = lib.fakeHash` to intentionally trigger a build failure that reveals the correct hash.

If `cmd/bmci` develops CGO dependencies (e.g., for ZFS ioctl bindings), switch to `proxyVendor = true` and recompute the hash.

### `nix copy` for server profile deployment

The `make deploy` flow runs `nix copy --to ssh://host /nix/store/...-server-profile`. This uses the legacy SSH store by default (the `ssh://` URL scheme). The remote side runs `nix-store --serve`.

To enable compression for WAN deployments:
```makefile
NIX_COPY_FLAGS ?= --to "ssh://$(HOST)?compress=true"
```

To use the full daemon protocol (required for CA derivation substitution):
```makefile
NIX_COPY_FLAGS ?= --to "ssh-ng://$(HOST)"
```

The `ssh-ng://` protocol requires `nix-daemon` to be running on the remote host. Since forge-metal nodes run NixOS or have Nix installed, this is typically available.

### CA derivations and the server profile

The `server-profile` derivation is a `buildEnv`. It is not currently CA. If `ca-derivations` were enabled, the server profile's store path would change based on its actual content rather than its input derivation hash. This would mean two deploys that produce identical binaries share the same store path — useful if multiple branches produce the same closure.

For the current single-node use case, this is not a practical concern. The more relevant optimization is lazy trees for faster `nix flake show` and `nix build` evaluation on the CI operator's machine.
