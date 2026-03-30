# Nix Daemon Worker Protocol, pkgs.extend/appendOverlays, nix-direnv, and lib Flake Output

## A: Nix Worker Protocol and Daemon Trust Model

The Nix daemon is the security boundary for multi-user installations. Every `nix build`, `nix develop`, and store access goes through it.

### The UNIX Socket and Peer Credential Check

The daemon listens on `/nix/var/nix/daemon-socket/socket` (mode 0666 — world-writable, but the kernel provides trust via credentials).

When a client connects, the daemon uses `SO_PEERCRED` (via `unix::getPeerInfo()`) to retrieve the connecting process's UID and GID without any input from the client:

```cpp
peer = unix::getPeerInfo(remote.get());
auto [trusted, userName] = authPeer(peer);
```

The `authPeer()` function then converts the numeric UID to a username via `getpwuid()`, and the GID to a group name via `getgrgid()`. It checks against two nix.conf settings:

```cpp
const Strings & trustedUsers = authorizationSettings.trustedUsers;
const Strings & allowedUsers = authorizationSettings.allowedUsers;

if (matchUser(user, group, trustedUsers))
    trusted = Trusted;

if ((!trusted && !matchUser(user, group, allowedUsers)) ||
    group == settings.getLocalSettings().buildUsersGroup)
    throw Error("user '%1%' is not allowed to connect...");
```

`matchUser()` supports:
- Exact username: `"alice"`
- Group membership: `"@wheel"`, `"@sudo"`
- Wildcard: `"*"` (all users)

The `nixbld*` users (in `buildUsersGroup`) are always rejected at the door — build users connect only via internal fork, never via the socket directly.

### Trust Levels

The daemon recognizes two trust states at the protocol level:

| State | Who | Capabilities |
|-------|-----|--------------|
| `Trusted` | root, or users listed in `trusted-users` | All operations; can set any nix.conf option; can build input-addressed derivations; can create permanent GC roots; can repair store paths; can skip signature checks |
| `NotTrusted` | Users in `allowed-users` but not `trusted-users` | Build/query only; cannot override sandbox; cannot skip signatures; daemon re-signs paths before accepting; cannot create perm roots |

Once the trust flag is set, it is passed as a `TrustedFlag` parameter to `processConnection()`, which enforces it at every operation:

```cpp
// In performOp(), called for each opcode:
if (mode == bmRepair && !trusted)
    throw Error("repairing is not allowed because you are not in 'trusted-users'");

// For BuildDerivation:
if (!(drvType.isCA() || trusted))
    throw Error("you are not privileged to build input-addressed derivations");

// For AddToStoreNar with dontCheckSigs:
if (!trusted && dontCheckSigs)
    dontCheckSigs = false;  // force signature verification
if (!trusted)
    info.ultimate = false;  // mark as non-ultimate
```

**Key security property**: CA (content-addressed) derivations are allowed for untrusted users because "their output paths are verified by their content alone" — the hash of the output itself is the verification. Input-addressed derivations require trust because the output path is derived from the derivation closure, not the output content, and a compromised derivation could produce a different output at the same path.

### `trusted-users` vs `allowed-users`

```
# /etc/nix/nix.conf (or nix.settings in NixOS)
trusted-users = root @wheel alice
allowed-users = *
```

- `trusted-users`: Full daemon capabilities. Setting `@wheel` here is common but means any wheel user can disable the sandbox (`--option sandbox false`), set `post-build-hook`, and do anything the daemon can do. Effectively equivalent to passwordless sudo for Nix operations.
- `allowed-users`: Basic build/query access. Default is `*` (everyone). Setting this to a specific group restricts who can use Nix at all.
- Users in neither list: Connection is rejected with "user X is not allowed to connect to the Nix daemon."

The `root` user is always trusted. In the NixOS module, the `nix` user group is often added to `trusted-users` for CI runners.

### Worker Protocol Framing

**Magic numbers** (hex literals in the protocol):
- `WORKER_MAGIC_1 = 0x6e697863` ("nixc" in ASCII)
- `WORKER_MAGIC_2 = 0x6478696f` ("dxio" in ASCII)

**Handshake sequence**:
1. Client sends `WORKER_MAGIC_1` + client protocol version
2. Daemon reads magic, validates it equals `WORKER_MAGIC_1`, rejects otherwise
3. Daemon sends `WORKER_MAGIC_2` + daemon protocol version
4. Client reads magic, validates it equals `WORKER_MAGIC_2`
5. Both sides negotiate the effective protocol version (min of both)
6. If protocol >= 1.38: feature set exchange via `intersectFeatures()`

**Protocol versions** (as of Nix master, early 2026):
- Current/latest: `1.39` (encoded as `(1 << 8 | 39)`)
- Minimum supported: `1.18`

After the handshake, the connection enters an operation loop. Each message is framed as:
1. `uint64_t` opcode (one of `WorkerProto::Op` enum values)
2. Opcode-specific payload (strings, paths, flags — all length-prefixed)
3. Response: status byte followed by result data

### WorkerProto::Op Enum (complete list)

| Opcode | ID | Purpose |
|--------|-----|---------|
| `IsValidPath` | 1 | Check if store path exists |
| `QueryReferrers` | 6 | Get paths that reference a given path |
| `AddToStore` | 7 | Add a NAR to the store |
| `AddTextToStore` | 8 | Add a text file to the store |
| `BuildPaths` | 9 | Build derivations |
| `EnsurePath` | 10 | Ensure a path is available (download from substituter) |
| `AddTempRoot` | 11 | Add a temporary GC root |
| `AddIndirectRoot` | 12 | Add an indirect GC root (symlink in gcroots dir) |
| `SyncWithGC` | 13 | Wait until GC cycle completes |
| `FindRoots` | 14 | Find all GC roots |
| `QueryDeriver` | 18 | Get the derivation that produced a path |
| `SetOptions` | 19 | Set daemon options (trust-gated) |
| `CollectGarbage` | 20 | Run GC |
| `QuerySubstitutablePathInfo` | 21 | Query substituter for path info |
| `QueryDerivationOutputs` | 22 | Get output paths of a derivation |
| `QueryAllValidPaths` | 23 | List all store paths |
| `QueryPathInfo` | 26 | Get full path metadata (hash, refs, sigs) |
| `QueryDerivationOutputNames` | 28 | Get output names (out, dev, lib...) |
| `QueryPathFromHashPart` | 29 | Reverse hash lookup |
| `QuerySubstitutablePathInfos` | 30 | Batch substituter query |
| `QueryValidPaths` | 31 | Filter paths to those present locally |
| `QuerySubstitutablePaths` | 32 | Which paths are available from substituters |
| `QueryValidDerivers` | 33 | Get derivations for an output path |
| `OptimiseStore` | 34 | Hardlink-deduplicate identical files |
| `VerifyStore` | 35 | Check store integrity |
| `BuildDerivation` | 36 | Build a single derivation (requires trust for IA) |
| `AddSignatures` | 37 | Add signatures to a store path |
| `NarFromPath` | 38 | Stream a NAR for a store path |
| `AddToStoreNar` | 39 | Add a NAR to the store with metadata |
| `QueryMissing` | 40 | Find what needs to be built or fetched |
| `QueryDerivationOutputMap` | 41 | Map output names → store paths |
| `RegisterDrvOutput` | 42 | Register a CA derivation output |
| `QueryRealisation` | 43 | Get realisation info for a drv output |
| `AddMultipleToStore` | 44 | Add multiple NARs (streaming) |
| `AddBuildLog` | 45 | Store build log |
| `BuildPathsWithResults` | 46 | Build and return build results |
| `AddPermRoot` | 47 | Create a permanent GC root (requires trust) |

**Trust-gated operations**: `BuildDerivation` (for input-addressed), `AddPermRoot`, `VerifyStore` with repair, `SetOptions` (for trusted-only settings), `AddToStoreNar` with `dontCheckSigs`.

### The `ssh-ng://` Protocol

The `ssh-ng://` store type tunnels the full `WorkerProto` over SSH, as opposed to the older `ssh://` which uses the limited `ServeProto`.

```
ssh://host   → remote command: nix-store --serve --write    → ServeProto
ssh-ng://host → remote command: nix-daemon --stdio          → WorkerProto
```

The `--stdio` mode is activated when the daemon is invoked with `nix-daemon --stdio`. The daemon then reads/writes the worker protocol on stdin/stdout instead of a UNIX socket. Trust is determined differently in this mode:

```cpp
// In --stdio mode:
if (!processOps && (remoteStore = store.dynamic_pointer_cast<RemoteStore>()))
    forwardStdioConnection(*remoteStore);
else
    processStdioConnection(store, forceTrustClientOpt.value_or(Trusted));
```

In `--stdio` mode, the caller (the SSH connection from the local daemon) is implicitly trusted — there is no SO_PEERCRED check because stdin/stdout have no peer credentials. The `--force-trusted` / `--force-untrusted` flags override this. The local Nix daemon establishes the SSH connection; if the local user is trusted locally, that trust extends to the remote operation.

`forwardStdioConnection` uses `select()` and `splice()` to bidirectionally relay data between the SSH stdio and the remote daemon socket — it acts as a transparent proxy rather than re-implementing the protocol.

**Practical implication for forge-metal**: `nixos-rebuild --target-host root@server` uses `ssh-ng://` internally. The local user needs to be able to SSH as root (or with sudo), not specifically in `trusted-users` on the remote — the remote daemon grants the operation as root.

### Substituter Trust

Paths fetched from binary caches (substituters) are verified against `trusted-public-keys`:

```
# nix.conf
trusted-public-keys = cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY=
substituters = https://cache.nixos.org
```

- `require-sigs = true` (default): all substituted paths must be signed by a trusted key
- `require-sigs = false`: accept any path from any substituter (dangerous)
- `trusted-substituters`: additional substituters that regular (non-trusted) users can add; they still require signatures
- Without `trusted-substituters`, only users in `trusted-users` can add substituters via `--option extra-substituters`

The `TrustedFlag` in the daemon affects substituter behavior: untrusted clients cannot set `dontCheckSigs = true` on `AddToStoreNar`, so every path they import must pass signature validation.

---

## B: `pkgs.extend` vs `pkgs.appendOverlays` and nixpkgs Instance Patterns

### How `extend` and `appendOverlays` Are Implemented

These are not part of `lib.makeExtensible` on the nixpkgs package set. They are defined directly in `pkgs/top-level/stage.nix` as part of the `otherPackageSets` overlay that is applied to every pkgs instance:

```nix
# From pkgs/top-level/stage.nix (otherPackageSets overlay)
extend = f: self.appendOverlays [ f ];

appendOverlays = extraOverlays:
  if extraOverlays == []
  then self
  else nixpkgsFun { overlays = args.overlays ++ extraOverlays; };
```

**Critical insight**: `appendOverlays` calls `nixpkgsFun` — it re-invokes the nixpkgs top-level fixpoint constructor with an augmented overlay list. It does NOT re-evaluate nixpkgs from scratch (the stdlib, stdenv phases, etc. are memoized via Nix thunks), but it does create a new fixpoint pass. The identity optimization (`if extraOverlays == [] then self`) prevents unnecessary re-evaluation when called with an empty list.

`extend` is literally a one-overlay shorthand for `appendOverlays`.

The nixpkgs package set fixpoint itself is built with `lib.fix toFix`, where `toFix` chains all overlays using `lib.foldl' lib.extends`:

```nix
# Conceptually (simplified from stage.nix):
pkgs = lib.fix (lib.foldl' lib.extends (allPackages args) args.overlays);
```

### Pattern Reference

| Pattern | When to use | What it does |
|---------|-------------|--------------|
| `import nixpkgs { system; overlays; config; }` | Never in flakes if you have the input | Full nixpkgs eval from scratch; two calls with the same args share thunks but are independent fixpoints |
| `inputs.nixpkgs.legacyPackages.${system}` | Primary pattern in flakes | Reuses the already-evaluated nixpkgs from the flake input |
| `pkgs.extend (self: super: { ... })` | Add one overlay to an existing pkgs | Creates a new fixpoint pass; does not re-fetch nixpkgs |
| `pkgs.appendOverlays [ ov1 ov2 ]` | Add multiple overlays | Same as chained `extend` calls; one fixpoint pass per call |
| `pkgs.override { system = "aarch64-linux"; }` | Rarely needed | Reconfigures the nixpkgs instance; in flakes, prefer `pkgs.pkgsCross.aarch64-multiplatform` |

### The `import nixpkgs {}` Footgun in Flakes

```nix
# WRONG in a flake — creates a separate nixpkgs evaluation
outputs = { self, nixpkgs }: {
  packages.x86_64-linux.foo = (import nixpkgs { system = "x86_64-linux"; }).hello;
};

# CORRECT — reuses the flake-evaluated nixpkgs
outputs = { self, nixpkgs }: {
  packages.x86_64-linux.foo = nixpkgs.legacyPackages.x86_64-linux.hello;
};
```

The wrong pattern has a subtle effect: the `import nixpkgs {}` call creates a separate nixpkgs instance. Nix's thunk memoization means the internal nixpkgs library functions are shared (same store path = same thunk), but the fixpoint overlay chain runs separately. This means:
1. Your overlays from `nixpkgs.overlays` (NixOS module) do not apply to the separately-imported instance.
2. Binary cache hits may be identical (same store paths), but you've done unnecessary eval work.
3. If you have two `import nixpkgs { system = "x86_64-linux"; }` calls in different modules, they each re-run the overlay chain.

### The `nixpkgs.config` Baked-In-At-Import-Time Trap

```nix
# WRONG: config is baked in at import time, extend cannot override it
let
  pkgs = nixpkgs.legacyPackages.x86_64-linux;
  pkgs' = pkgs.extend (self: super: {
    config = super.config // { allowUnfree = true; };  # has no effect
  });
in pkgs'.someUnfreePackage   # still fails
```

`nixpkgs.config` (including `allowUnfree`, `allowBroken`, `permittedInsecurePackages`) is evaluated during the initial `import nixpkgs {}` call and threaded through the stdenv stages. An overlay cannot retroactively change it. To use `allowUnfree`, you must pass it at import time:

```nix
# Correct approach in a flake:
pkgs = import nixpkgs { system = "x86_64-linux"; config.allowUnfree = true; };

# Or use the NixOS module option (applies to the system nixpkgs instance):
nixpkgs.config.allowUnfree = true;
```

`legacyPackages` uses `import ./. { inherit system; }` with default config (no `allowUnfree`). If you need unfree packages, you must use `import nixpkgs`.

### The Double-nixpkgs Diamond Dependency Problem

When multiple flake inputs each declare their own `nixpkgs` dependency:

```
your-flake
├── nixpkgs (nixos-unstable, commit A)
├── home-manager
│   └── nixpkgs (nixos-unstable, commit B)  ← different!
└── some-other-tool
    └── nixpkgs (nixos-unstable, commit C)  ← different again!
```

Each distinct nixpkgs commit = a separate eval. Binary cache misses occur because the store paths from commit A's nixpkgs differ from commit B's. Fix with `follows`:

```nix
inputs = {
  nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  home-manager = {
    url = "github:nix-community/home-manager";
    inputs.nixpkgs.follows = "nixpkgs";  # force same commit
  };
};
```

With `follows`, all three nodes resolve to the same nixpkgs commit, meaning all packages come from the same eval and hit the same binary cache entries.

### `pkgs.__splicedPackages`

This internal attribute exists in cross-compilation contexts. When you use `pkgs.pkgsCross.aarch64-linux`, the resulting pkgs has:
- `pkgs.buildPackages` — packages for the build machine (x86_64)
- `pkgs.targetPackages` — packages for the target (aarch64)
- `pkgs.__splicedPackages` — the merged view used by `callPackage` internally to resolve `nativeBuildInputs` vs `buildInputs` automatically

Do not reference `__splicedPackages` directly. Use `pkgsCross`, `buildPackages`, or the standard `callPackage` mechanism which splices automatically.

---

## C: `nix-direnv` Internals

`nix-direnv` (https://github.com/nix-community/nix-direnv) solves the dev workflow problem that `nix develop` re-evaluates the entire flake on every shell entry.

### The Problem

For a local path flake (e.g., `flake .`), Nix disables the eval cache entirely (see [source-code-deep-dive.md](source-code-deep-dive.md)). Every `cd` into the directory triggers a full flake evaluation. Cold evaluation of nixpkgs takes 5–30 seconds. Even warm evaluations take 0.5–2 seconds. Over 100 shell entries per day, that's 50–3000 seconds wasted.

Additionally, `nix develop` creates no persistent GC root for the devShell build. After `nix-collect-garbage`, the devShell is deleted and must be rebuilt from scratch (another 30–120 seconds depending on network).

### How nix-direnv Caches

The `.envrc` file uses the `use flake` directive, which calls nix-direnv's `use_flake()` function.

**Cache key**: SHA1 of the flake expression string (typically `"."` or `"github:org/repo"`), appended to the profile name. The profile name becomes:

```
${direnv_layout_dir}/flake-profile-<sha1>
${direnv_layout_dir}/flake-profile-<sha1>.rc
```

`direnv_layout_dir` defaults to `$XDG_CACHE_HOME/direnv/layouts/<sha256-of-pwd>` (one directory per project, based on the directory's path).

**Cache invalidation**: nix-direnv watches:
- `~/.direnvrc` and `~/.config/direnv/direnvrc`
- `<flake-dir>/flake.nix`
- `<flake-dir>/flake.lock`
- `<flake-dir>/devshell.toml` (if present)

When any watched file is newer than the cached `profile.rc`, nix-direnv triggers a rebuild.

**The nix invocation**:
```bash
# Actual command run on cache miss:
nix --no-warn-dirty \
    --extra-experimental-features "nix-command flakes" \
    print-dev-env \
    --profile "${layout_dir}/flake-profile-<sha1>" \
    "$flake_uri"
```

`nix print-dev-env` outputs a shell script (bash `export` statements) capturing the devShell environment. This script is saved to `profile.rc` and sourced on subsequent entries.

### GC Root Strategy

The profile symlink itself (`${layout_dir}/flake-profile-<sha1>`) acts as a Nix GC root because direnv's `layout_dir` is under the user's home directory — but this alone is not sufficient to prevent GC.

nix-direnv additionally registers the build closure in the `flake-inputs/` subdirectory:

```bash
_nix_add_gcroot() {
    local storepath=$1
    local symlink=$2
    _nix build --out-link "$symlink" "$storepath"
}
# Creates symlinks like:
# ${layout_dir}/flake-inputs/<hash-part>  →  /nix/store/...
```

The `--out-link` flag causes `nix build` to register the output as an indirect GC root in `/nix/var/nix/gcroots/auto/`. This is a permanent GC root that survives `nix-collect-garbage`. The profile path itself is not in the gcroots directory — only the symlinks created by `--out-link` are. This is more durable than the temproot created by `nix develop`.

nix-direnv also runs `touch -h` on existing root symlinks to update their timestamps, which prevents some GC implementations (like `nh clean`) from collecting them by age.

### Fallback on Eval Error

```bash
if tmp_profile_rc=$(_nix print-dev-env --profile "$tmp_profile" "$flake_uri"); then
    # success: update profile_rc with new environment
    cp "$tmp_profile.rc" "$profile_rc"
else
    if [[ $_nix_direnv_allow_fallback -eq 1 ]]; then
        _nix_direnv_warning "Evaluating current devShell failed. Falling back..."
        export NIX_DIRENV_DID_FALLBACK=1
        # source the old profile_rc (previous working environment)
    else
        return 1  # hard failure
    fi
fi
```

Default behavior: `_nix_direnv_allow_fallback=1` (fallback enabled). If `flake.nix` has a syntax error or a build failure, the previous working devShell is loaded instead of dropping to a broken shell. The `NIX_DIRENV_DID_FALLBACK=1` env var can be checked in shell prompts to signal the stale state.

To disable fallback (strict mode): call `nix_direnv_disallow_fallback` in `.envrc` before `use flake`.

### Configuration

| Variable/Function | Effect |
|-------------------|--------|
| `nix_direnv_manual_reload` | Disable auto-reload; require explicit `nix-direnv-reload` command |
| `nix_direnv_disallow_fallback` | Fail hard on eval error (no previous env fallback) |
| `NIX_DIRENV_FALLBACK_NIX` | Override the Nix binary path (for alternative Nix distributions) |
| `NIX_DIRENV_DID_FALLBACK=1` | Set by nix-direnv when running fallback env; check in PS1 |
| `watch_file <file>` | Add additional files to watch (direnv built-in, not nix-direnv specific) |

There is no `use_flake!` (bang variant) in nix-direnv. The README documents that `watch_file` can be used before `use flake` to add extra watch files. Forced re-evaluation can be triggered by running `nix-direnv-reload` or by touching `flake.lock`.

### Performance Profile

Typical wall-clock times for a nixpkgs-based devShell:
- **First entry (cold, no cache)**: 10–30 seconds (full `nix print-dev-env` evaluation + build of any missing packages)
- **Subsequent entries (warm, cache valid)**: 50–200 ms (read `profile.rc`, run `eval "$(< profile.rc)"`)
- **Entry after `flake.lock` change (rebuild)**: 5–20 seconds (re-eval of flake, packages usually cached in binary cache)
- **`nix develop` without nix-direnv**: same as cold every time for local path flakes

### Integration Notes

**With flake-parts**: nix-direnv calls `nix print-dev-env --profile <path> "."` which resolves to `devShells.${system}.default`. The `perSystem` module in flake-parts must define `devShells.default` (not bare `devShells`) for this to work. If you use a non-default devShell:

```bash
# .envrc
use flake ".#myShell"
# → resolves to devShells.${system}.myShell
```

**With legacy `shell.nix`**: use the `use nix` directive instead of `use flake`. It calls `nix-shell` (the classic tool), not `nix develop`.

**Requirements**: Bash >= 4.4, direnv >= 2.21.3.

---

## D: `lib` Flake Output Conventions

The `lib` flake output is the least standardized output type — there is no schema validation for it, unlike `packages` or `apps`.

### `inputs.nixpkgs.lib` vs `pkgs.lib`

This is a critical performance distinction:

```nix
# EXPENSIVE: requires a full nixpkgs eval for the system
pkgs = nixpkgs.legacyPackages.x86_64-linux;
lib = pkgs.lib;  # forces nixpkgs evaluation

# CHEAP: no system eval, pure library functions only
lib = inputs.nixpkgs.lib;  # available immediately from the flake input
```

`inputs.nixpkgs.lib` is the nixpkgs `lib/` directory as a standalone attribute set. It requires no `system` parameter and no package set instantiation. The nixpkgs `flake.nix` exports it as:

```nix
# From nixpkgs/flake.nix (simplified):
lib = (import ./lib).extend (final: prev: {
    nixos = import ./nixos/lib { lib = final; };
    nixosSystem = args: import ./nixos/lib/eval-config.nix { ... };
});
```

So `inputs.nixpkgs.lib` includes not just the stdlib (`lib.strings`, `lib.lists`, etc.) but also `lib.nixosSystem` and `lib.nixos`. Use `inputs.nixpkgs.lib` for any lib-only operations in flake `outputs` functions.

### Custom `lib` Flake Output Pattern

The conventional pattern for a flake that exports a library:

```nix
outputs = { self, nixpkgs }: {
  lib = import ./lib { lib = nixpkgs.lib; };
};
```

Where `./lib/default.nix` is:
```nix
{ lib }:
{
  myFunction = x: lib.strings.toUpper x;
  anotherHelper = { name, value }: lib.nameValuePair name value;
}
```

**Design principles for lib-exporting flakes**:
1. `lib` functions should be pure Nix — no dependency on `pkgs` or system
2. The flake should NOT pin nixpkgs in `flake.lock` (forcing all consumers to that specific nixpkgs version). Instead: make nixpkgs optional or use `inputs.nixpkgs.follows = "nixpkgs"` convention
3. The lib output is a flat or nested attrset of functions — there is no enforced schema

### `lib.extend` for Augmenting nixpkgs.lib

`lib.extend` uses the same fixpoint mechanism as pkgs overlays (`lib.makeExtensible`):

```nix
# lib/fixed-points.nix:
makeExtensibleWithCustomName = extenderName: rattrs:
  fix' (self:
    (rattrs self) // {
      ${extenderName} = f: makeExtensibleWithCustomName extenderName (extends f rattrs);
    }
  );

makeExtensible = makeExtensibleWithCustomName "extend";
```

`nixpkgs.lib` is itself a `makeExtensible` set (it has `lib.extend`). So you can add functions without re-importing nixpkgs:

```nix
# Correct: extend nixpkgs.lib with your functions
customLib = nixpkgs.lib.extend (final: prev: {
    myFunc = x: prev.strings.toUpper x;
    # final = the extended lib (with myFunc)
    # prev = the original nixpkgs.lib
});
```

**The `prev.pkgs.lib` infinite recursion trap** (documented in [tooling-and-patterns.md](tooling-and-patterns.md)) applies equally here: if you're inside a pkgs overlay and want to extend lib:

```nix
# WRONG: traverses through pkgs causing infinite recursion
final: prev: { lib = prev.pkgs.lib // { myFunc = ...; }; }

# CORRECT: use lib directly
final: prev: { lib = prev.lib.extend (_: _: { myFunc = ...; }); }
```

### The `lib` Output Schema in Practice

Common conventions (none enforced by `nix flake check`):

```nix
# Flat lib output (simple)
lib = {
  myFunction = x: ...;
  myOtherFunction = y: ...;
};

# Namespaced (recommended for larger libs)
lib = {
  strings = { myStringFn = ...; };
  attrsets = { myAttrFn = ...; };
};

# Extending nixpkgs.lib (for lib-augmenting flakes)
lib = nixpkgs.lib.extend (final: prev: {
  myFunction = ...; # accessible as lib.myFunction
});
```

`nix flake check` does NOT validate the `lib` output at all — it is completely opaque to the checker. Unlike `packages` (which verifies each attribute is a derivation) or `nixosConfigurations` (which evaluates the config), `lib` is skipped entirely.

### `flake-parts` and `lib`

`flake-parts` exposes its own `lib` output:
- `inputs.flake-parts.lib.mkFlake` — the entry point function
- `inputs.flake-parts.lib.evalFlakeModule` — evaluate a flake module without wiring up outputs

When writing a flake that re-exports `lib` from its own modules, the idiomatic flake-parts pattern is:

```nix
outputs = inputs: inputs.flake-parts.lib.mkFlake { inherit inputs; } {
  flake = {
    lib = import ./lib { lib = inputs.nixpkgs.lib; };
  };
};
```

The `flake = { ... }` section in flake-parts outputs pass-through values that are not system-dependent. `lib` always goes in `flake`, never in `perSystem`.

### Version Pinning for `lib`-Only Flakes

If your flake exports only `lib` (no packages, no NixOS modules), you have two options:

**Option A: No nixpkgs dependency at all** (preferred for pure Nix function libraries that don't use nixpkgs.lib):
```nix
{
  inputs = {};  # no nixpkgs
  outputs = { self }: {
    lib = { myFunc = x: x + 1; };
  };
}
```

**Option B: Soft nixpkgs dependency** (for libs that need nixpkgs.lib utilities):
```nix
{
  inputs.nixpkgs = {
    url = "github:NixOS/nixpkgs/nixos-unstable";
    # No flake.lock pin forces consumers to use their own nixpkgs
    # This is achieved by not committing flake.lock, or by using:
  };
  outputs = { self, nixpkgs }: {
    lib = import ./lib { lib = nixpkgs.lib; };
  };
}
```

Convention: lib-only flakes should document `inputs.nixpkgs.follows = "nixpkgs"` in their README so consumers can override the nixpkgs version without friction.

---

## Connections to forge-metal

**Daemon trust model**: The Ansible deployment in `ansible/roles/base/` should ensure the CI runner user (if any) is in `allowed-users` but NOT in `trusted-users`. Trusted-users on a multi-tenant CI host means any job can disable the sandbox.

**`pkgs.extend` in NixOS VM tests**: The `docker-vms-scripts-fmt-security.md` document shows a pattern using `nixpkgs.legacyPackages.x86_64-linux.extend` for VM test derivations. This is the correct pattern — it avoids re-importing nixpkgs.

**`nix-direnv` for forge-metal dev**: The `flake.nix` uses `mkShell` via `flake-utils.lib.eachDefaultSystem`. Adding a `.envrc` with `use flake` and installing nix-direnv eliminates the per-shell eval cost. The GC root from `--out-link` ensures the Go toolchain and Terraform in the devShell survive `nix-collect-garbage`.

**`inputs.nixpkgs.lib`**: The flake currently accesses `pkgs.lib` inside `eachDefaultSystem`. For any flake-level lib operations (not system-specific), prefer `nixpkgs.lib` directly.

---

## Sources

- `src/libstore/include/nix/store/worker-protocol.hh` — Op enum (ID 1–47), magic numbers (0x6e697863, 0x6478696f), protocol versions 1.18–1.39
- `src/libstore/worker-protocol.cc` — serialization, TrustedFlag enum, ClientHandshakeInfo
- `src/libstore/daemon.cc` — `processConnection()`, per-opcode trust checks
- `src/nix/unix/daemon.cc` — `authPeer()` with SO_PEERCRED, `trusted-users`/`allowed-users` check, `--stdio` mode
- `pkgs/top-level/stage.nix` — `extend` and `appendOverlays` implementations
- `lib/fixed-points.nix` — `fix`, `fix'`, `extends`, `makeExtensible`, `makeExtensibleWithCustomName`
- `nixpkgs/flake.nix` — `lib` output definition using `lib.extend`, `legacyPackages` definition
- `nix-community/nix-direnv` `direnvrc` — `use_flake()`, cache key computation, GC root strategy, fallback logic
