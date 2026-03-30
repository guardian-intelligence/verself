# Nix Flakes: Deployment, Secrets, Source Filtering, and Closure Analysis

Source-grounded research on deployment mechanics, secrets management, source filtering, closure analysis, and channel strategy — with direct relevance to forge-metal's build and deploy pipeline.

---

## Topic 1: SOPS + age Secrets Management with Nix Flakes

**Primary source:** https://github.com/Mic92/sops-nix (README, March 2026)

### How sops-nix integrates with flakes

Add `sops-nix` as a flake input and wire it into `nixosSystem` modules:

```nix
{
  inputs.sops-nix.url = "github:Mic92/sops-nix";
  inputs.sops-nix.inputs.nixpkgs.follows = "nixpkgs";

  outputs = { self, nixpkgs, sops-nix }: {
    nixosConfigurations.myhostname = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        ./configuration.nix
        sops-nix.nixosModules.sops   # <-- this is the NixOS module
      ];
    };
  };
}
```

The module is imported once and then configured declaratively via `sops.*` options in any NixOS module.

### The `sops.secrets.<name>` option and activation-time decryption

`sops.secrets.<name>` is a NixOS option that describes a single secret. The sops file (e.g. `secrets/example.yaml`) **is added to the Nix store** in encrypted form — this is safe because the ciphertext cannot be decrypted without the private key.

At `nixos-rebuild switch` (the activation phase), the sops-nix activation script:
1. Reads the encrypted sops file from the Nix store.
2. Decrypts it using the available `age` key or SSH host key.
3. Writes each secret to a new directory `/run/secrets.d/<N>/`.
4. Atomically symlinks `/run/secrets` to the new directory.

The secrets live in `tmpfs` (`/run/`) and are **never persisted to disk** (unless you configure otherwise). `/run/secrets` is a symlink to `/run/secrets.d/1`.

Full example configuration:

```nix
{
  sops.defaultSopsFile = ./secrets/example.yaml;
  sops.age.sshKeyPaths = [ "/etc/ssh/ssh_host_ed25519_key" ];
  sops.age.keyFile = "/var/lib/sops-nix/key.txt";
  sops.age.generateKey = true;   # generate key if not present

  sops.secrets.example-key = {};
  sops.secrets."myservice/my_subdir/my_secret" = {};
  # After activation: /run/secrets/example-key, /run/secrets/myservice/my_subdir/my_secret

  # Per-secret permissions:
  sops.secrets.example-secret.mode = "0440";
  sops.secrets.example-secret.owner = config.users.users.nobody.name;
  sops.secrets.example-secret.group = config.users.users.nobody.group;

  # Restart systemd units on secret change:
  sops.secrets."home-assistant-secrets.yaml".restartUnits = [ "home-assistant.service" ];

  # Symlink to a path a service expects:
  sops.secrets."home-assistant-secrets.yaml".path = "/var/lib/hass/secrets.yaml";
}
```

**Critical: secrets for user password hashing** must use `neededForUsers = true` to decrypt to `/run/secrets-for-users` *before* NixOS creates users:

```nix
{
  sops.secrets.my-password.neededForUsers = true;
  users.users.mic92.hashedPasswordFile = config.sops.secrets.my-password.path;
}
```

### The age key management problem in the Nix store context

The Nix store is world-readable (`/nix/store` has mode 755, individual paths have mode 444 or 555). **Private keys must never enter the Nix store.** sops-nix solves this by having the private key exist outside the store in two ways:

1. **SSH host key** (`sops.age.sshKeyPaths`): The host's existing `/etc/ssh/ssh_host_ed25519_key` is re-used. It is converted to `age` format on-the-fly during activation. This key exists outside the store as part of standard SSH host key infrastructure.

2. **Dedicated age key file** (`sops.age.keyFile`): A file at e.g. `/var/lib/sops-nix/key.txt`. With `sops.age.generateKey = true`, sops-nix creates this file on first boot if absent. On systems with Impermanence, this path must be in a persisted directory.

The **public** keys (for encryption) go into `.sops.yaml` at the repo root:

```yaml
keys:
  - &admin_alice age12zlz6lvcdk6eqaewfylg35w0syh58sm7gh53q5vvn7hd7c6nngyseftjxl
  - &server_host age1rgffpespcyjn0d8jglk7km9kfrfhdyev6camd3rck6pn8y47ze4sug23v3
creation_rules:
  - path_regex: secrets/[^/]+\.(yaml|json|env|ini)$
    key_groups:
    - age:
      - *admin_alice
      - *server_host
```

To get a server's `age` public key from its SSH host key:
```
ssh-keyscan example.com | ssh-to-age
# or
cat /etc/ssh/ssh_host_ed25519_key.pub | ssh-to-age
```

The `sops` command uses `SOPS_AGE_KEY_FILE` env var (default: `~/.config/sops/age/keys.txt`) to find the private key at edit/encrypt time. On the server, sops-nix reads it from `sops.age.keyFile`.

### sops-nix vs agenix: differences

| Aspect | sops-nix | agenix |
|--------|----------|--------|
| Encryption backend | `sops` (Mozilla) — supports age, GPG, AWS KMS, GCP KMS, Azure Key Vault, HCP Vault | `age` only (via ryantm/agenix) |
| Secret format | YAML, JSON, INI, dotenv, binary; one file can hold many secrets | One `.age` file per secret |
| Key selection at build time | `.sops.yaml` `creation_rules` regex patterns per file | `secrets.nix` (separate file, not imported into NixOS config) per-file publicKeys list |
| CLI tool | `sops` binary (external, well-maintained by getsops org) | `agenix` binary |
| Complexity | More powerful, more configuration surface | Minimal — "very little code, so it should be easy for you to audit" (agenix README) |
| Password-protected SSH keys | Supported | Not supported — agenix does not support ssh-agent, so password-protected keys require entering password once per secret during rekey |
| Templates | `sops.templates` feature: embed secrets inside config files with placeholder substitution | Not built-in |
| Home-manager | `sops-nix.homeManagerModules.sops` | `agenix.homeManagerModules.default` |
| Secret file in store | Yes (encrypted) | Yes (encrypted) |
| Decryption timing | NixOS activation script | NixOS activation script |
| Output path | `/run/secrets/<name>` | `/run/agenix/<name>` |

**Key architectural difference:** sops encrypts once against a symmetric data key (DEK), then encrypts the DEK once per recipient public key. So adding a new recipient doesn't re-encrypt the actual secret, only the DEK wrapper. agenix encrypts the secret directly per recipient.

### Using sops in a `buildGoModule` or `mkShell` context (not NixOS)

sops-nix is fundamentally a **NixOS activation-time** tool. It does **not** decrypt secrets at `nix build` time, and cannot inject plaintext secrets into a `buildGoModule` derivation (doing so would expose them in the store).

For **developer shells**, sops-nix provides GPG shell hooks:

```nix
# shell.nix
mkShell {
  sopsPGPKeyDirs = [
    "${toString ./.}/keys/hosts"
    "${toString ./.}/keys/users"
  ];
  nativeBuildInputs = [
    (pkgs.callPackage sops-nix {}).sops-import-keys-hook
  ];
}
```

This hook imports `.asc`/`.gpg` public key files at shell entry so teammates can encrypt/edit secrets using `sops` directly.

For non-NixOS use (e.g. on a plain Ubuntu server running `nix-shell`), the pattern is:
1. Keep `SOPS_AGE_KEY_FILE=~/.config/sops/age/keys.txt` in environment.
2. Use `sops -d secrets.yaml` to decrypt ad-hoc before passing values to builds.
3. Do not pass secret values to `buildGoModule` — use runtime injection via config files or environment variables.

The README explicitly warns: "It is not possible to use secrets at evaluation time of nix code. This is because sops-nix decrypts secrets only in the activation phase."

### The `sops --age` flag and key lookup

`sops` uses the `age` recipients flag as:
```
sops --age age1abc...xyz -e secrets.yaml
```

For decryption, `sops` looks up private keys in this order:
1. `SOPS_AGE_KEY` environment variable (inline key material)
2. `SOPS_AGE_KEY_FILE` environment variable (path to key file)
3. Platform default: `~/.config/sops/age/keys.txt` on Linux, `~/Library/Application Support/sops/age/keys.txt` on macOS

Keys in the key file can be bare age private keys (`AGE-SECRET-KEY-1...`) or SSH Ed25519 private keys. Multiple keys can be listed, one per line; sops tries each against the file's encrypted DEK.

---

## Topic 2: `pkgs.lib.cleanSourceWith` Filter Mechanics

**Primary source:** https://github.com/NixOS/nixpkgs/blob/master/lib/sources.nix (fetched March 2026)

### What `cleanSourceWith` does and the filter function signature

`cleanSourceWith` is a composable wrapper around `builtins.filterSource`. Its key innovation is that it **composes** — you can chain multiple `cleanSourceWith` calls without creating intermediate Nix store copies. `builtins.filterSource` cannot be composed (you'd copy to the store at each application).

```nix
# This works:
lib.cleanSourceWith {
  filter = f;
  src = lib.cleanSourceWith {
    filter = g;
    src = ./.;
  };
}

# This fails — creates two intermediate store copies:
builtins.filterSource f (builtins.filterSource g ./.)
```

**Filter function signature:** `path: type: bool`

- `path` (string): the absolute path of the file/directory being considered
- `type` (string): `"regular"`, `"directory"`, `"symlink"`, or `"unknown"` — same types as `builtins.readDir`
- Returns `true` to include, `false` to exclude

**Full signature of `cleanSourceWith`:**
```nix
cleanSourceWith {
  src     # Path or cleanSourceWith result to filter
  filter  # Optional, default: _path: _type: true (include everything)
          # Combined with src.filter via &&
  name    # Optional store path name, default: src.name or "source"
}
```

**How composition works internally:** When a `cleanSourceWith` result is used as `src`, the function reads its `_isLibCleanSourceWith = true` tag and unwraps the `origSrc` and existing `filter`. The new filter becomes `path: type: newFilter path type && existingFilter path type`. This is lazy — the inner filter runs only when the outer filter passes.

### What `cleanSource` excludes by default (`cleanSourceFilter`)

`cleanSource = src: cleanSourceWith { filter = cleanSourceFilter; inherit src; };`

`cleanSourceFilter` excludes:
- `.git` (any type — prevents the entire git history from entering the store)
- `.svn`, `CVS`, `.hg`, `.jj`, `.pijul`, `_darcs` directories (VCS systems)
- Files ending in `~` (editor backup files, e.g. `foo.txt~`)
- Files matching `^\\.sw[a-z]$` or `^\\..*\\.sw[a-z]$` (vim swap files: `.swp`, `.swo`, etc.)
- Files ending in `.o` or `.so` (compiled objects)
- Symlinks starting with `result` (nix-build result symlinks — e.g. `result`, `result-1`)
- Files of type `"unknown"` (sockets, device files — cannot be stored in Nix store)

### Differences between `cleanSource`, `cleanSourceWith`, and `lib.sourceByRegex`

| Function | Behavior | Use case |
|----------|----------|----------|
| `cleanSource src` | Applies `cleanSourceFilter`, removes VCS dirs and editor artifacts | Simple: just clean up a directory |
| `cleanSourceWith { src; filter; }` | Composable custom filter on top of optional existing filter | Build up complex filters incrementally |
| `lib.sourceByRegex src regexes` | Whitelist-based: only includes paths matching any of the given POSIX regexes | Include precisely a set of file patterns |

`sourceByRegex` is a whitelist (include matching), while `cleanSource` is a blacklist (exclude matching). They can be combined via `cleanSourceWith`.

### `lib.sourceFilesBySuffices` and `lib.sourceByRegex`

**`lib.sourceFilesBySuffices src exts`:**
Includes all directories (so traversal continues) plus any regular file whose `baseNameOf` ends with one of the given suffixes. Example:
```nix
sourceFilesBySuffices ./. [ ".xml" ".c" ]
# includes ./dir/module.c and ./dir/subdir/doc.xml but not ./dir/module.h
```

**`lib.sourceByRegex src regexes`:**
Includes files whose path relative to `origSrc` matches any of the given POSIX extended regular expressions. Example:
```nix
sourceByRegex ./my-subproject [ ".*\\.py$" "^database\\.sql$" ]
```

Note: `sourceByRegex` computes the relative path by stripping the original (un-filtered) source path prefix. This is why it explicitly reads `src.origSrc` if `src` is already a filtered source.

### How the filter interacts with Nix store path computation

The store path hash for a `cleanSourceWith` result is computed by `builtins.path`, which hashes the NAR serialization of all included files. This means:

- Adding or removing a file that passes the filter **changes** the store path → cache miss → rebuild.
- Changes to files that are filtered out (e.g. editing `README.md` when your filter only includes `.go` files) **do not change** the store path → cache hit → no rebuild.

This is the primary motivation for source filtering in CI: without filtering, touching any file (including documentation, CI configs, editor settings) would invalidate the build cache.

**A non-obvious gotcha:** The filter receives the **absolute path** as a string, not a relative path. To compute a relative path you must strip the `origSrc` prefix. `sourceByRegex` does this correctly; if you write your own filter you must do the same.

### `lib.fileset`: the newer API

**Source:** https://github.com/NixOS/nixpkgs/blob/master/lib/fileset/README.md and nixpkgs manual `#sec-fileset`

`lib.fileset` (introduced ~2023) is a higher-level, type-safe replacement for `cleanSourceWith`. Key differences:

| Aspect | `cleanSourceWith` | `lib.fileset` |
|--------|-------------------|---------------|
| Mental model | Predicate filter on a directory tree | Mathematical set of files |
| Composition | Manual `&&` in filter | Set operations: `union`, `intersection`, `difference` |
| Lazy evaluation | Yes | Yes |
| Error messages | Minimal | Designed to be helpful ("throw early and helpful errors") |
| Git integration | No | `lib.fileset.gitTracked ./. ` — file set from git-tracked files |
| Empty directories | Preserved if dir passes filter | Not representable (mirrors git behavior) |
| Store path computation | `builtins.path` with filter | `builtins.path` with filter |
| Debug output | `lib.sources.trace` | `lib.fileset.trace`, `lib.fileset.traceVal` |

**Core `lib.fileset` API:**

```nix
# Add files to store
lib.fileset.toSource {
  root = ./.;       # becomes / in the store path
  fileset = lib.fileset.unions [
    ./src
    ./Makefile
    ../LICENSE      # can go above root if root is adjusted to ../
  ];
}

# Combinators
lib.fileset.union ./src ./tests
lib.fileset.intersection ./. (lib.fileset.gitTracked ./.)
lib.fileset.difference ./. ./secrets

# Filter by file properties
lib.fileset.fileFilter (file: file.hasExt "go") ./.

# From git-tracked files only
lib.fileset.gitTracked ./.
```

**`lib.fileset.gitTracked`** is a significant practical advantage over `cleanSourceWith`: it reads `.git/` to find which files are tracked and includes only those — no manual filter needed, no risk of including untracked build artifacts.

**`toSource` enforces influence tracking:** all files in the `fileset` must be within or under `root`. `toSource { root = ./dir; fileset = ./.; }` throws an error even if `./` only contains `dir/`, because the fileset's base path is `./`, which is above `root`.

**Interop with `cleanSourceWith`:**
```nix
lib.fileset.fromSource (lib.cleanSourceWith { ... })
```
converts a `cleanSourceWith` result into a file set for use with `lib.fileset` combinators.

---

## Topic 3: `nix store` Closure Analysis Commands

**Primary source:** Nix 2.28.6 reference manual, fetched from https://nix.dev/manual/nix/stable/

### `nix path-info --closure-size`

Prints the sum of NAR serialization sizes of the transitive closure of each specified path.

```bash
# Show closure sizes of all paths in a NixOS system closure, sorted:
nix path-info --recursive --closure-size /run/current-system | sort -nk2

# Show individual path size (-s) AND closure size (-S) with human-readable output:
nix path-info --recursive --size --closure-size --human-readable nixpkgs#rustc
# Output:
# /nix/store/01rr...-ncurses-6.2-dev    386.7 KiB   69.1 MiB
# /nix/store/0q78...-libpfm-4.11.0        5.9 MiB   37.4 MiB

# Show total size of entire Nix store:
nix path-info --json --all | jq 'map(.narSize) | add'

# Find all paths whose closure exceeds 1 GB:
nix path-info --json --all --closure-size \
  | jq 'map_values(.closureSize | select(. > 1e9)) | to_entries | sort_by(.value)'
```

**Flag semantics:**
- `-s` / `--size`: NAR size of the path itself
- `-S` / `--closure-size`: Sum of NAR sizes of the transitive closure
- `-h` / `--human-readable`: Human-friendly units (KiB, MiB, GiB)
- `-r` / `--recursive`: Apply to each path in the closure

**The most useful one-liner for CI closure bloat diagnosis:**
```bash
nix path-info -rsSh /nix/store/...-my-package | sort -nk2
```
This lists every path in the closure with its own size and cumulative closure size, sorted by size. The entries near the bottom are the biggest individual contributors.

### `nix store diff-closures`

```bash
nix store diff-closures /nix/var/nix/profiles/system-655-link \
                         /nix/var/nix/profiles/system-658-link
```

**Output format:**
```
acpi-call: 2020-04-07-5.8.16 → 2020-04-07-5.8.18
dolphin: 20.08.1 → 20.08.2, +13.9 KiB
kdeconnect: 20.08.2 → ∅, -6597.8 KiB
kdeconnect-kde: ∅ → 20.08.2, +6599.7 KiB
```

- `∅` means the package did not exist in that closure
- Only shows packages where version changed **or** size changed by more than 8 KiB
- "Package name" is the name component of the store path (everything before the version)
- If a package exists in multiple versions in both closures, only the changed versions are shown

This is the right tool to audit what `nix flake update` actually changes in production — compare the before/after system profiles.

### `nix why-depends`

```bash
# Show shortest dependency chain from hello to glibc:
nix why-depends nixpkgs#hello nixpkgs#glibc
# Output:
# /nix/store/v5sv61sszx301i0x6xysaqzla09nksnd-hello-2.10
# └───bin/hello: ….../nix/store/9l06v7fc38c1x3r2iydl15ksgz0ysb82-glibc-2.32/lib/ld-linux-x86-64...
#     → /nix/store/9l06v7fc38c1x3r2iydl15ksgz0ysb82-glibc-2.32

# Show ALL dependency paths (not just shortest):
nix why-depends --all nixpkgs#thunderbird nixpkgs#xorg.libX11

# Show why a build-time dependency exists (drv-level):
nix why-depends --derivation nixpkgs#geeqie nixpkgs#systemd
```

**How it works:** Nix computes runtime dependencies by scanning the NAR bytes of a store path for hash-part substrings of other store paths. If any file in package A contains the 32-char hash-part of package B's store path, A depends on B. `nix why-depends` shows which file contains the reference and the surrounding context. This is how you find e.g. a compiler accidentally embedded in a binary (via debug info, RPATH, or config string).

**`--derivation` flag:** examines `.drv` files instead of output paths — finds build-time dependencies (not visible in runtime closure).

**When to use:** when `nix path-info -S` shows a package has an unexpectedly large closure, use `nix why-depends my-package suspicious-large-dep` to find the chain.

### `nix store gc` vs `nix-collect-garbage -d`

**`nix store gc`:**
- Deletes only unreachable store paths (those not reachable from any GC root)
- GC roots include: active profiles, `/nix/var/nix/gcroots/`, `result` symlinks, running processes
- `--max n`: stop after freeing n bytes
- `--dry-run`: show what would be deleted

**`nix-collect-garbage -d`:**
- First deletes **old profile generations** (equivalent to `nix-env --delete-generations old` on all profiles in `~/.local/state/nix/profiles/` and `/nix/var/nix/profiles/per-user/`)
- Then runs `nix-store --gc` (equivalent to `nix store gc`)
- The `-d` flag is what makes old generations (and thus their unique store paths) eligible for collection
- Warning: deleting old generations makes rollback to those generations impossible

**Key difference:** `nix store gc` without `-d` will often free very little because profiles hold GC roots for all their generations. `nix-collect-garbage -d` first removes those roots.

### `nix store optimise`

```bash
nix store optimise
```

Scans `/nix/store` for regular files with identical contents and replaces them with hard links to a single instance. This saves disk space when multiple packages include the same file (common for fonts, locale data, CA certificates).

- Maintains a content-addressed index in `/nix/store/.links/`
- Can be done automatically on every store write by setting `auto-optimise-store = true` in `nix.conf`
- Incremental auto-optimisation is efficient because Nix only checks newly added files via `.links/`
- Does not affect the logical content of any store path (hard links are transparent to reads)

---

## Topic 4: nixpkgs Channel Strategy for CI Platforms

**Primary sources:** Hydra (hydra.nixos.org), NixOS channel status API (monitoring.nixos.org/prometheus), nixpkgs manual

### Channel taxonomy

**As of March 2026 (live data from monitoring.nixos.org):**

| Channel | Status | What it tracks |
|---------|--------|----------------|
| `nixos-unstable` | rolling | `master` branch of nixpkgs, after Hydra `nixos:unstable` jobset passes required tests |
| `nixos-unstable-small` | rolling | Same nixpkgs commits as `nixos-unstable` but evaluated against a smaller job set (fewer packages required to pass) → advances faster |
| `nixpkgs-unstable` | rolling | `master` branch, after Hydra `nixpkgs:unstable` jobset passes (no NixOS-specific tests required) |
| `nixos-25.11` | stable | `nixos-25.11` branch, periodic security/bugfix updates only |
| `nixos-25.11-small` | stable | Same as `nixos-25.11` with smaller required job set |
| `nixpkgs-25.11-darwin` | stable | `nixos-25.11` branch, tested only on Darwin (macOS) — no NixOS tests |

**Channels that no longer exist in the live API** (already unmaintained):
- `nixos-25.05`, `nixos-25.05-small`, `nixpkgs-25.05-darwin`

### nixos-unstable vs nixpkgs-unstable: the actual difference

Both track the same `master` branch, but with different required Hydra job sets:

- **`nixpkgs-unstable`** (`nixpkgs:unstable` Hydra jobset): Requires a set of core packages to build on `x86_64-linux`, `aarch64-linux`, `x86_64-darwin`, `aarch64-darwin`. No NixOS system-level tests required. **Advances faster** — from live Hydra data, evaluations happen every ~6-10 hours.

- **`nixos-unstable`** (`nixos:unstable` Hydra jobset): Requires all packages that `nixpkgs-unstable` requires, **plus** NixOS module tests, NixOS system builds, and integration tests. These require building full NixOS system closures. **Advances slower** — from live Hydra data, evaluations happen every 1-5 days.

**Live Hydra evaluation data (March 30, 2026):**
- `nixpkgs:unstable` last evaluated: `2026-03-30 07:39:36` (25 min, 17 sec). Evaluations visible: 2h ago, 10h ago, 19h ago, 1d ago, 1d ago... → roughly **every 8-10 hours**.
- `nixos:unstable` last evaluated: `2026-03-28 15:31:05` (47 min, 10 sec). Evaluations visible: 1d ago, 2d ago, 5d ago, 2026-03-21, 2026-03-20... → roughly **every 1-5 days**.

**Job count comparison:**
- `nixpkgs:unstable`: ~267,000+ jobs evaluated per run
- `nixos:unstable`: ~157,000 jobs (different, larger job kinds — NixOS module tests are heavier per job)

### `nixpkgs-24.11-darwin` vs `nixos-24.11`

- `nixos-24.11`: Includes all NixOS modules and tests. Used on Linux only. Contains `nixos.*` options in the module system.
- `nixpkgs-24.11-darwin`: Same nixpkgs revision but tested only against macOS jobs on Hydra. **Does not include NixOS modules** that require Linux. Used on macOS with `nix-darwin` or `home-manager`. There is no `nixos-24.11-darwin` because NixOS doesn't run on Darwin.

### When does nixos-unstable break?

`nixos-unstable` advances only when all required Hydra jobs pass. "Passes" means the builds succeeded — but Hydra always has some `Eval Errors` (packages that fail to evaluate/build), so "passes" means the **required** jobs pass, not all jobs. Required jobs are defined in the Hydra configuration.

Breaking causes:
- A nixpkgs commit introduces a Nix expression evaluation error in a module that blocks building the NixOS system derivation
- A kernel update breaks a required NixOS VM test
- A new package version introduces a dependency conflict

The channel simply stops advancing if required jobs fail. The `nixos-unstable-small` channel advances independently and with a smaller required set, so it often has a more recent commit than `nixos-unstable`.

### Channel update frequency summary for CI

For a CI platform that needs to pin reproducibly:
- **Flakes** (`inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable"`) are the correct approach — `flake.lock` pins to a specific git commit. Channel names in flake inputs are just branch names; the lock file pins the exact commit hash.
- `nixpkgs-unstable` gives newer packages faster than `nixos-unstable` on Linux if you don't need NixOS modules.
- For production CI reproducibility, pin to a specific commit hash or use a stable release (e.g. `nixos-25.11`).

---

## Topic 5: `nix copy` Full Bootstrap Flow

**Primary source:** Nix 2.28.6 reference manual, fetched March 2026

### Initial Nix bootstrap on a fresh Ubuntu server

The official installer is a shell script fetched over HTTPS:

```bash
# Multi-user (recommended — requires systemd):
sh <(curl --proto '=https' --tlsv1.2 -L https://nixos.org/nix/install) --daemon

# Single-user:
sh <(curl --proto '=https' --tlsv1.2 -L https://nixos.org/nix/install) --no-daemon
```

**What the multi-user installer does:**
1. Creates `/nix` directory (invokes `sudo` to set ownership to `root`)
2. Creates `nixbld` group and `nixbld1`...`nixbldN` users (build sandbox users)
3. Unpacks the Nix bootstrap store closure into `/nix/store`
4. Installs and starts `nix-daemon.service` (systemd unit)
5. Adds `source /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh` to shell profiles

After installation, the Nix daemon runs as root and owns `/nix/store`. Unprivileged users communicate with it via a Unix domain socket at `/nix/var/nix/daemon-socket/socket`.

The bootstrap closure embedded in the installer contains only the minimal set: `nix`, `bash`, `curl`, `coreutils`, `gzip`, `tar`. These are fetched from `https://releases.nixos.org/nix/nix-<version>/` as a NAR tarball.

### How `nix copy --to ssh://host` works

```bash
nix copy --to ssh://server /run/current-system
# or with -s (substitute on destination before fetching from local):
nix copy --substitute-on-destination --to ssh://server /run/current-system
```

**What happens:**
1. Local Nix computes the transitive closure of the store path(s) specified.
2. Local Nix opens an SSH connection to `server` and starts `nix-store --serve --write` on the remote.
3. The local Nix daemon queries the remote daemon (via the SSH pipe) for which store paths it already has.
4. The local Nix sends only the **missing** paths as NAR streams over the SSH connection.
5. The remote Nix receives and validates each NAR, then registers the path in its store database.

**With `--substitute-on-destination` (`-s`):** Before transferring from local, the remote Nix tries to fetch missing paths from its configured substituters (e.g. `cache.nixos.org`). Paths already in the binary cache are substituted on the remote without needing to transfer over SSH. This is faster when the remote has better connectivity to the binary cache than to the local machine.

**Store URL formats for `nix copy`:**
- `ssh://user@host` — uses `ssh://` store (limited, read-only queries via `nix-store`)
- `ssh-ng://user@host` — uses the newer store protocol over SSH (more efficient, supports newer Nix features)
- `file:///path` — local binary cache in NAR format
- `s3://bucket-name?region=us-east-1` — S3 binary cache

**NAR format:** Network Archive. Every Nix store path is serialized as a single NAR file regardless of whether it's a directory tree or single file. NARs are deterministic (same content = same bytes). The NAR hash is the store path hash.

### `nix-store --realise`

```bash
nix-store --realise /nix/store/abc...-mypackage.drv
nix-store -r /nix/store/abc...-mypackage.drv
```

Realising a `.drv` file means: either build it locally, or substitute the outputs from binary caches. It is the low-level operation that `nix build` calls internally.

**When needed after `nix copy`:** `nix copy` transfers store objects (built outputs). It does not build anything. If you copy a `.drv` file (derivation) but not its output, you would need to `nix-store --realise` the `.drv` to build the output on the remote. In the typical workflow for forge-metal (`nix build` locally → `nix copy --to ssh://host`), you copy only the built outputs, so `--realise` is not needed.

**`nix-store --realise` on a non-derivation path:** If the path is a pre-built output (not a `.drv`), it tries to substitute it from binary caches. If no substitute is available and it's not a derivation, realisation fails.

### `nix copy` vs legacy `nix-push` / `nix-pull`

`nix-push` and `nix-pull` were the old (pre-2016) mechanism for binary caches:
- `nix-push` created a manifest file and individual NAR files on a server
- `nix-pull` downloaded manifests and NARs

These are now fully replaced by:
- `nix copy --to file:///path` (creates a local binary cache)
- `nix copy --to s3://bucket` (creates an S3 binary cache)
- `nix copy --from https://cache.nixos.org/ path` (pull from HTTP binary cache)
- `nix copy --to ssh://host` (direct store-to-store copy)

The modern binary cache protocol uses `.narinfo` files (metadata + signature) and `.nar.xz` (compressed NAR) stored with content-addressed names. This is an HTTP protocol servable by `nix serve`, Cachix, or any static file server.

### `nix store sign` and why it's needed for binary caches

```bash
# Generate a signing key pair:
nix-store --generate-binary-cache-key mycache.example.com-1 \
  /etc/nix/signing-key.sec \
  /etc/nix/signing-key.pub

# Sign all paths in a local binary cache:
nix store sign --key-file /etc/nix/signing-key.sec \
  $(nix path-info --all)

# Or sign specific paths:
nix store sign --key-file /etc/nix/signing-key.sec \
  /nix/store/abc...-mypackage
```

**Why signing is required:** Nix verifies store path integrity using Ed25519 signatures on `.narinfo` files. When a store path is fetched from a binary cache, Nix checks that at least one signature matches a trusted public key listed in `trusted-public-keys` in `nix.conf`. Unsigned paths are rejected (unless you set `require-sigs = false`, which is a security risk).

**When `nix copy --to ssh://host` is used directly (not via HTTP binary cache):** The remote Nix daemon does not require signatures for direct store-to-store copies from trusted hosts. Signatures become mandatory when serving via HTTP binary cache (`nix serve` or Cachix).

**Trust model:** Paths built locally and copied via SSH inherit the local daemon's trust. Paths coming from HTTP binary caches are only trusted if signed by a key in `trusted-public-keys`. `nix-collect-garbage` and store validation (`nix store verify`) check signatures.

### `nix build --store ssh-ng://host` vs local build + `nix copy`

**`nix build --store ssh-ng://host`:**
- Evaluates the expression **locally** (parsing flake.nix, evaluating Nix language)
- Sends the resulting `.drv` files to the remote store
- Executes the **build** on the remote machine (the remote's CPU, RAM, and tools)
- The output stays on the remote machine
- Useful for cross-architecture builds or building on more powerful remote hardware

**Local build + `nix copy`:**
- Builds on the **local** machine
- Copies the resulting store paths to the remote
- Output is pushed (not pulled) to the remote
- Better for CI: build once, distribute to many nodes

**`ssh://` vs `ssh-ng://`:** The older `ssh://` protocol uses `nix-store --serve` on the remote and has limited capabilities (can query and add paths but not run builds). The newer `ssh-ng://` protocol (`--store ssh-ng://host`) uses the newer Nix daemon wire protocol over SSH and supports the full store API including running builds on the remote. For `nix build --store`, you need `ssh-ng://`.

**Practical distinction for forge-metal:** The pattern `nix build .#server-profile && nix copy --to ssh://host` is correct — build locally on the dev machine (or CI runner), push the pre-built closure to the bare metal server. The server only needs to run `nix-store --realise` or activate if it needs to switch profiles, which is handled by Ansible calling `nix-env -p /nix/var/nix/profiles/system --set /nix/store/...-nixos-system` or equivalent.

---

## Cross-cutting Notes for forge-metal

1. **sops-nix for this project:** The pattern of `sops.age.sshKeyPaths = [ "/etc/ssh/ssh_host_ed25519_key" ]` is the right approach for bare metal — no extra key management, the host's SSH key is the decryption key. Encrypted secrets live in the repo (Nix store-safe), decrypted secrets appear at `/run/secrets/` after `nixos-rebuild switch`.

2. **Source filtering for Go builds:** Use `lib.fileset.gitTracked ./.` or `lib.cleanSourceWith` with a filter that includes only `**/*.go`, `go.mod`, `go.sum` for `buildGoModule`. This prevents rebuilds on every doc change.

3. **Closure analysis for the server profile:** Run `nix path-info -rsSh .#server-profile | sort -nk2` before pushing to identify large unexpected deps. Use `nix why-depends .#server-profile .#some-large-dep` to trace the reference chain.

4. **Channel choice:** Pin `nixpkgs-unstable` in `flake.lock` for the server profile. Use `nix flake update` deliberately. The `nixpkgs-unstable` channel (not `nixos-unstable`) advances faster and is sufficient for building packages that will be activated via Ansible (not `nixos-rebuild`).

5. **Bootstrap flow on new Latitude.sh server:** Ansible `nix_deploy` role runs `curl | sh` for the Nix installer, then `nix copy --substitute-on-destination --to ssh://server /nix/store/...-server-profile` to push the closure. The `-s` flag lets the remote pull common packages from cache.nixos.org instead of uploading everything over the Latitude.sh → dev machine link.
