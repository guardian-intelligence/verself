# `nixos-rebuild` Deep Mechanics, Remote Deploy Patterns, and Nix Version History

Covers: `nixos-rebuild` activation modes, the `switch-to-configuration` script internals, profile/generation management, remote deployment with `--target-host`, SSH gotchas, and a per-release changelog of Nix 2.4 through 2.30 focused on what breaks existing flakes and what new capabilities are added.

---

## Part A: `nixos-rebuild` Deep Mechanics

### What `nixos-rebuild` Actually Is

`nixos-rebuild` is a Bash script (rewritten in Python as `nixos-rebuild-ng` for NixOS 25.05+) that orchestrates three distinct phases:

1. **Build**: evaluates `nixosConfigurations.<hostname>` from a flake (or `/etc/nixos/configuration.nix`) to produce `config.system.build.toplevel` — a store path containing the entire system.
2. **Profile update**: adds the built toplevel to `/nix/var/nix/profiles/system`, creating a new generation symlink (`system-N-link`).
3. **Activation**: runs `$toplevel/bin/switch-to-configuration <action>` to apply changes to the live system.

The profile update (step 2) and activation (step 3) are **separate** from the `switch-to-configuration` script itself. The script receives only the action verb; it finds the system derivation via the environment.

---

### Activation Modes Compared

| Mode | Build | Profile Update | Activate Now | Bootloader Updated |
|------|-------|----------------|--------------|-------------------|
| `switch` | yes | yes | yes | yes |
| `boot` | yes | yes | no | yes |
| `test` | yes | no | yes | no |
| `dry-activate` | yes | no | dry-run only | no |
| `build` | yes | no | no | no |
| `dry-build` | show only | no | no | no |
| `build-vm` | yes (VM) | no | no | no |
| `build-vm-with-bootloader` | yes (VM+bootloader) | no | no | no |

**`switch`**: the normal deployment path. Activates immediately AND updates the bootloader. After success, `/run/current-system` → new generation, and GRUB/systemd-boot shows the new generation as default. On reboot, the new generation boots automatically.

**`boot`**: safe for kernel upgrades. Updates the bootloader so the new generation boots next time, but leaves the running system untouched. Combined with `reboot`, this ensures the old kernel remains active until the controlled reboot. Avoids the partial-activation hazard where new kernel modules exist but the kernel is still old.

**`test`**: the staging pattern. Activates immediately but does NOT touch the bootloader. If anything breaks (networking, sshd), a reboot returns to the last `switch` or `boot` generation. Ideal for: "deploy, verify manually, then `switch` to commit."

**`dry-activate`**: evaluates the configuration and reports which systemd units would be restarted/reloaded/stopped, but makes zero changes. Depends on the `supportsDryActivation` flag in `switch-to-configuration`. The list of planned changes is indicative, not guaranteed to be complete.

**`build`**: only produces a `./result` symlink; nothing is activated or registered. Useful for checking that the config evaluates and builds without touching the live system.

**`build-vm`**: builds a QEMU VM image of the configuration and creates `./result/bin/run-<hostname>-vm` — a script that boots it. Network is NAT; no real services exposed by default. Useful for smoke-testing NixOS module interactions.

**`build-vm-with-bootloader`**: like `build-vm` but includes a full GRUB or systemd-boot installation inside the VM image. Slower to build; useful for testing bootloader configuration.

---

### The Activation Script: Step-by-Step

When `switch-to-configuration switch` (or `test`) runs, it executes these steps (from the Perl/Python script and the `system.activationScripts` DAG):

1. **`/run/current-system` symlink**: atomically replaces the symlink to point at the new system path. `/run/booted-system` is set once at boot and never changed by `switch`.
2. **`system.activationScripts`**: runs the DAG of activation scripts in topological order. Common scripts: `users` (create/modify user accounts), `groups`, `etc` (populate `/etc` via symlink farm), `specialFiles` (set `/etc/passwd` etc.), `nix` (link the Nix daemon configuration), `tmpfiles` (invoke `systemd-tmpfiles --create`).
3. **`systemctl daemon-reload`**: makes systemd re-read all unit files from the new `/etc/systemd/system/` symlink farm.
4. **Unit diffing**: compares the unit file content between the old system (`/run/booted-system` or the previous current-system) and the new one. For each changed unit, the script checks NixOS-specific X-Restart-Triggers and X-Reload-Triggers directives to decide the action.
5. **Service actions**: units are stopped, restarted, or reloaded in the correct dependency order. The NixOS options `restartIfChanged` (default: true), `reloadIfChanged` (default: false), and `stopIfChanged` (default: true) control this per-service.
6. **Bootloader update** (only for `switch` and `boot`): invokes the bootloader activation script (e.g., `grub-install` or `bootctl install`). Controlled by `NIXOS_INSTALL_BOOTLOADER` env var.

**`dry-activate`** skips steps 2 through 6 entirely, printing what would happen instead. The `supportsDryActivation` attribute must be set on activation scripts to opt them into dry-run reporting; scripts without it are silently skipped in dry mode.

---

### Systemd Unit Diffing: Restart vs Reload vs Nothing

`switch-to-configuration` computes a diff between old and new unit files by:

1. Listing all units in the old system's `/etc/systemd/system/` and the new system's `/etc/systemd/system/`.
2. For each unit present in both: comparing file content. If the content changed, the unit is a candidate for action.
3. Checking `X-Restart-Triggers` in the `[Unit]` section — an arbitrary list of store paths; if any path changes, the unit is restarted even if the unit file itself is unchanged. This is how `restartTriggers` in `systemd.services.<name>` works: it writes paths into `X-Restart-Triggers`.
4. Similarly, `X-Reload-Triggers` (NixOS option: `reloadTriggers`) causes a reload (SIGHUP or `systemctl reload`) rather than a full restart.
5. For units with `reloadIfChanged = true`: if the unit file changed, send `systemctl reload` instead of `systemctl restart`. This is used for services like nginx that support graceful reload.
6. For units with `stopIfChanged = false`: do not stop/start; only reload if `reloadIfChanged` is also set. Use for services that must not be interrupted.

**Key gotcha**: systemd socket units and their companion services are handled together. If a `.socket` unit is removed but the script tries to restart it, you get an error. This was a known bug (`nixos/switch-to-configuration: don't try restart deleted sockets`, fixed in PR #115927).

---

### Profile Management and Generations

```
/nix/var/nix/profiles/
├── system -> system-42-link          # current default boot generation
├── system-40-link -> /nix/store/...  # older generation (GC root)
├── system-41-link -> /nix/store/...  # GC root
└── system-42-link -> /nix/store/...  # latest
```

Every `nixos-rebuild switch` or `nixos-rebuild boot`:
1. Creates a new `system-N-link` symlink pointing to the built store path.
2. Advances `system` to point at `system-N-link`.
3. These symlinks are GC roots — the system derivation and its entire closure are protected from garbage collection as long as the symlink exists.

`nixos-rebuild --rollback` decrements N by one: it activates `system-(N-1)-link`. This is near-instantaneous because the old closure is already in the store.

`nixos-rebuild list-generations` shows all `system-N-link` entries with dates and NixOS version info.

To GC old generations: `nix-collect-garbage --delete-old` removes all but the current generation. `nix-collect-garbage -d` is an alias. Use `--delete-older-than 30d` to keep the last 30 days of generations.

---

### Specialisations

Specialisations are configuration variants built alongside the main config. They appear as subdirectories of the system toplevel:

```
/run/current-system/specialisation/
├── laptop/bin/switch-to-configuration
└── server/bin/switch-to-configuration
```

To switch to a specialisation at runtime:
```bash
/run/current-system/specialisation/laptop/bin/switch-to-configuration switch
# or via nixos-rebuild:
nixos-rebuild switch --specialisation laptop
```

Specialisations share the parent config with `inheritParentConfig = true` (the default). The bootloader lists them as separate boot entries. Each specialisation has its own `switch-to-configuration` script — when run, it activates the specialisation's closure from `/run/current-system/specialisation/<name>/`.

---

### `--flake` Option

```bash
nixos-rebuild switch --flake .#my-hostname
# or with default hostname:
nixos-rebuild switch --flake .
```

With `--flake path/to/dir#hostname`, nixos-rebuild:
1. Calls `nix build` with `.#nixosConfigurations.my-hostname.config.system.build.toplevel`
2. Uses the flake's eval to produce the system derivation

If `#hostname` is omitted, it defaults to the machine's `hostname` (output of `hostname` command).

**Git tracking requirement**: if the flake is in a Git repo, only tracked files are visible to the Nix evaluator. Untracked `configuration.nix` additions cause mysterious "file not found" errors. Always `git add` new files before rebuilding.

---

### `--no-build-nix` / `--fast`

By default, `nixos-rebuild` first rebuilds the Nix package from nixpkgs (to ensure the Nix daemon is up to date). `--no-build-nix` (aliased as `--fast`) skips this step. For config-only changes where the Nix version hasn't changed, `--fast` saves 5–30 seconds.

---

## Part B: Remote Deployment Patterns and Gotchas

### `--target-host`: Build Locally, Activate Remotely

```bash
nixos-rebuild switch \
  --flake .#remote-host \
  --target-host root@192.168.1.100 \
  --use-remote-sudo
```

The flow:
1. Builds the system derivation **locally**.
2. Copies the closure to the remote via `nix copy --to ssh-ng://root@host`.
3. SSHes into the remote and runs `switch-to-configuration switch` via `sudo`.

**Port requirements**: only port 22 (SSH). The Nix daemon socket on the remote is accessed through the SSH tunnel via the `ssh-ng://` protocol — no extra ports needed.

### `--build-host`: Compile on a Remote Builder

```bash
nixos-rebuild switch \
  --flake .#target \
  --build-host builder@compile-box \
  --target-host root@deploy-target
```

Useful when the local machine is underpowered (e.g., a laptop deploying to a beefy server). The build machine must be listed as a Nix remote builder in `nix.conf` or have trust established.

### `--use-remote-sudo` (also `--sudo`)

Prepends `sudo` to remote SSH commands. Requires either:
- Passwordless sudo (`security.sudo.wheelNeedsPassword = false` in NixOS config), OR
- Interactive TTY: `NIX_SSHOPTS="-o RequestTTY=force"` + the `--ask-sudo-password` flag in `nixos-rebuild-ng`

The `--use-remote-sudo` flag passes `NIXOS_INSTALL_BOOTLOADER` through `sudo`'s environment. Historically, plain `sudo` strips environment variables, so `--install-bootloader` combined with `--use-remote-sudo` had a bug where the bootloader was not actually reinstalled. Fixed in `nixos-rebuild-ng` but still present in the original Bash script — always verify bootloader reinstallation separately.

### SSH ControlMaster for Connection Reuse

`nixos-rebuild` makes multiple SSH connections: one for `nix copy`, one or more for running the activation script. Without SSH connection multiplexing, each connection incurs a full TLS/key-exchange handshake.

Configure `~/.ssh/config`:
```
Host my-server
  HostName 192.168.1.100
  User root
  ControlMaster auto
  ControlPath ~/.ssh/cm-%r@%h:%p
  ControlPersist 10m
```

**Known gotcha**: `nix copy --to ssh-ng://host` has been reported to hang when `ControlMaster` is active (issue #459273). The daemon-protocol tunneled through ControlMaster can deadlock in some Nix versions. Workaround: `NIX_SSHOPTS="-o ControlMaster=no"` for the `nix copy` phase, or disable ControlMaster for nix store operations:

```bash
NIX_SSHOPTS="-o ControlMaster=no" nixos-rebuild switch --target-host root@server
```

### `NIXOS_INSTALL_BOOTLOADER=1`

This environment variable forces the bootloader to be fully reinstalled during activation, even if the bootloader itself hasn't changed. Useful after:
- Disk replacement or repartitioning
- Moving from BIOS to UEFI
- Recovering from a corrupted boot partition

Usage (manual activation):
```bash
sudo NIXOS_INSTALL_BOOTLOADER=1 /nix/var/nix/profiles/system/bin/switch-to-configuration boot
```

Via `nixos-rebuild`:
```bash
sudo nixos-rebuild switch --install-bootloader
```

Note: `--install-bootloader` in the Bash `nixos-rebuild` exports `NIXOS_INSTALL_BOOTLOADER=1` into the environment before calling `switch-to-configuration`. The `sudo` binary may strip this variable depending on `/etc/sudoers` configuration (`env_reset` / `env_keep`).

### Zero-Downtime Deploy Pattern: `test` then `switch`

```bash
# Step 1: activate the new config, but don't commit to bootloader
nixos-rebuild test --flake .#server --target-host root@server

# Step 2: verify manually
ssh root@server systemctl status nginx
ssh root@server curl -sf https://localhost/health

# Step 3: if OK, commit to bootloader so it survives reboot
nixos-rebuild switch --flake .#server --target-host root@server
```

With `test`, a reboot at any point rolls back to the last `switch`-committed generation. This gives you a safe window for verification without risk.

### Kernel Upgrade Pattern: `boot` then `reboot`

When the new config changes the kernel, `nixos-rebuild switch` loads the new kernel modules but runs the old kernel binary. This can cause module version mismatches. The safe pattern:

```bash
# Build and update bootloader, but stay on old kernel
nixos-rebuild boot --flake .#server --target-host root@server

# Controlled reboot to new kernel
ssh root@server reboot
```

Avoids the race condition in `switch` where new kernel modules are loaded against a still-running old kernel. For remote servers where a botched kernel means an expensive recovery console, `boot` + `reboot` is always the safer choice.

### Dead Man's Switch Pattern

For unattended remote deployments where a broken config (e.g., bad firewall rule, sshd misconfiguration) could lock you out, implement automatic rollback:

```nix
# In your NixOS config:
systemd.services.rollback-timer = {
  description = "Auto-rollback if not cancelled within timeout";
  after = [ "network.target" ];
  wantedBy = [ "multi-user.target" ];
  serviceConfig = {
    Type = "oneshot";
    ExecStart = pkgs.writeScript "rollback" ''
      sleep 90
      /run/current-system/sw/bin/nixos-rebuild switch --rollback
    '';
  };
};
```

After deployment, SSH in and cancel: `systemctl stop rollback-timer`. If you can't connect within 90 seconds, the system rolls back automatically.

### UID Mismatch and Trusted Users

Files pushed via `nix copy --to ssh-ng://host` are owned by the remote Nix daemon user (`root` or the user running `nix-daemon`). The activation scripts must not assume specific ownership of store paths — store paths are always world-readable, and NixOS activation creates `/etc` entries via symlinks into the store, so ownership is not a concern for most configuration.

For the remote user (not root) case, the remote user must be in `trusted-users` in `/etc/nix/nix.conf`:
```
trusted-users = root mydeployuser
```
Without `trusted-users`, the Nix daemon rejects the incoming paths from `nix copy`.

### Checking Service Status After Switch

`nixos-rebuild` does not automatically show service status after activation (unlike some deployment tools). After `nixos-rebuild switch --target-host`:

```bash
ssh root@server systemctl --failed --no-pager
ssh root@server journalctl -p err -n 50 --no-pager
```

`nixos-rebuild-ng` (the Python rewrite) has better status reporting built in.

---

## Part C: `nixos-rebuild-ng` — The Python Rewrite

Starting with NixOS 25.05, `nixos-rebuild-ng` is available alongside the original Bash `nixos-rebuild`. It becomes the default in NixOS 25.11 via `system.rebuild.enableNg = true`.

Differences from the Bash version:
- Better `--ask-sudo-password` support for non-root remote deploys
- `--env-password` for SSH password via environment variable
- More accurate error messages and exit codes
- Structured output for programmatic consumption
- The `NIXOS_INSTALL_BOOTLOADER` environment variable is correctly propagated through `sudo`

To opt in early: `system.rebuild.enableNg = true;` in your NixOS configuration. To use side-by-side without making it default: add `pkgs.nixos-rebuild-ng` to `environment.systemPackages`.

---

## Part D: Nix Version History — 2.4 Through 2.30

This section documents the changes in each Nix release that materially affect flake workflows: what breaks, what's new, and what CLI changes occurred.

### Nix 2.4 (November 2021) — Flakes Debut

The first release with flakes and the unified `nix` CLI as experimental features.

**New capabilities:**
- `nix build`, `nix shell`, `nix develop`, `nix run` as new unified commands replacing `nix-build`, `nix-shell`, `nix-env --install`, `nix-env --upgrade`
- `nix flake` subcommands: `init`, `new`, `update`, `lock`, `check`, `show`, `clone`, `metadata`
- `nix profile` as a replacement for `nix-env` with provenance tracking
- `nix registry` for managing flake registries
- Eval cache v1: caches derivation output paths for flake outputs; avoids re-evaluating unchanged configs
- `builtins.fetchTree` as a unified fetcher replacing `fetchGit`/`fetchTarball`
- Content-addressed store paths (experimental): hash derived from content, not input closure

**Breaking changes for existing workflows:**
- None for `nix-*` legacy commands (fully preserved). Flakes require `--extra-experimental-features "nix-command flakes"`.

**What to know:**
- The flake output naming was not yet stabilized — `defaultPackage.<system>`, `devShell.<system>` (singular) were the correct names at this point; they were renamed in 2.7.
- Eval cache is per-flake-output; a cache miss forces full re-evaluation.

---

### Nix 2.5 (December 2021)

**New capabilities:**
- GC no longer blocks new builds (removed the "waiting for the big garbage collector lock" stall)
- `builtins.groupBy` added (faster than `lib.groupBy`)
- `nix develop --unpack` runs the `unpackPhase` in the dev shell
- Lists are now comparable with `<` (lexicographic)
- Binary cache `compression-level` setting

**Breaking changes:** None for flake workflows.

---

### Nix 2.6 (February 2022)

**New capabilities:**
- `nix` CLI now searches for `flake.nix` upward through Git repo root, not just the current directory. Enables running `nix build` from subdirectories of a monorepo.
- `commit-lockfile-summary` option for customizing lock file commit messages
- `builtins.zipAttrsWith` added
- `nix store copy-log` for transferring build logs between stores

**Breaking changes for flake workflows:**
- The upward search for `flake.nix` can cause surprising behavior if you have nested flakes: Nix may find an ancestor `flake.nix` instead of a local one.

---

### Nix 2.7 (March 2022) — Output Name Standardization

**New capabilities:**
- Flake output naming stabilized with new canonical names:
  - `defaultPackage.<system>` → `packages.<system>.default`
  - `devShell.<system>` → `devShells.<system>.default`
  - `defaultApp.<system>` → `apps.<system>.default`
  - `overlay` → `overlays.default`
  - `defaultTemplate` → `templates.default`
  - `nixosModule` → `nixosModules.default` (added in 2.8 to this list)
- Templates can now define `welcomeText` shown during `nix flake init`
- `nix flake {init,new}` prints which files were created
- `nix store ping` reports remote Nix daemon version
- Command-line typo suggestions

**Breaking changes for flake workflows:**
- `nix flake check` now warns on old-style output names (`devShell`, `defaultPackage`, etc.). Flakes using the old names continue to work but should be migrated.

---

### Nix 2.8 (April 2022)

**New capabilities:**
- `nix fmt` command added (experimental): runs the `formatter.<system>` flake output
- `builtins.fetchClosure`: experimental builtin for importing pre-built paths from binary caches at eval time; can rewrite to CA form
- Impure derivations (`__impure = true`): experimental; builds that may produce different results each run
- `nix store make-content-addressable` renamed to `nix store make-content-addressed`
- `nixosModule` output renamed to `nixosModules.default` (standardization continued from 2.7)
- `--file -` flag: most commands now read expressions from stdin

**Breaking changes:**
- `nix store make-content-addressable` command no longer exists (renamed). Scripts using it break.
- `nixosModule` output (singular) now triggers a deprecation warning from `nix flake check`.

---

### Nix 2.9 (May 2022)

**New capabilities:**
- `--debugger` flag: launches interactive REPL on exceptions or `builtins.break` calls
- `nix repl` gained `:bl` (build-and-link) command
- `builtins.fetchTree` now supports fetching individual files via HTTP(S)/file protocols (not just tarballs)
- `nix build --print-out-paths` flag
- Output selection syntax: `installable^output` (e.g., `nixpkgs#gcc^dev`)
- Paths from `builtins.toFile` usable under `restrict-eval`

**Breaking changes:** None for standard flake workflows.

---

### Nix 2.10 (July 2022)

**New capabilities:**
- `nix repl` accepts installables directly: `nix repl --file '<nixpkgs>'` (old `nix repl '<nixpkgs>'` no longer works)
- `nix search --exclude` for filtering results
- Non-root Linux users can use Nix without `/nix/` by falling back to `~/.local/share/nix/root`
- `builtins.traceVerbose` (enabled via `trace-verbose = true`)
- Flake registry source changed from GitHub to `channels.nixos.org`

**Breaking changes:**
- **`nix repl` syntax change**: `nix repl '<nixpkgs>'` now requires `nix repl --file '<nixpkgs>'`. Scripts invoking the old form break silently (they open a blank repl instead of loading nixpkgs).

---

### Nix 2.11 (August 2022)

**New capabilities:**
- `nix copy` parallelizes store path copies across multiple paths simultaneously. Exception: `daemon` and `ssh-ng` stores still batch everything to avoid latency spikes.

**Breaking changes:** None.

---

### Nix 2.12 (December 2022)

**New capabilities:**
- Builds can run in Linux user namespaces as root (UID 0) with 65536 UIDs available — enables container-in-build patterns
- Automatic UID allocation for build users; `nixbld*` accounts no longer required
- Experimental cgroup support for builds
- `nix build --json` reports per-derivation CPU statistics when cgroups are enabled

**Breaking changes:** None for flake workflows.

---

### Nix 2.13 (February 2023)

**New capabilities:**
- Flake references work in legacy CLI: `nix-build flake:nixpkgs -A hello`
- Much improved error trace formatting (precise source locations, summaries by default)
- Output selection from `.drv` files: `nix build /nix/store/...drv^dev`
- Disable global flake registry: `flake-registry = ""` in `nix.conf`
- `nix develop` configures personality for cross-compiled shells (e.g., `i686-linux` on `x86_64`)

**Breaking changes:**
- `--repeat` and `--enforce-determinism` options removed (were broken for a long time). Build scripts using them will fail with "unknown option."

---

### Nix 2.14 (February 2023)

**New capabilities:**
- `builtins.readFileType`: stat a path, returning `"regular"`, `"directory"`, `"symlink"`, or `"unknown"` — without reading the full directory like `builtins.readDir`
- Flake `.outPath` now always refers to the directory containing `flake.nix`, even for subdirectory flakes. Original source: `.sourceInfo.outPath`
- `unsafeDiscardReferences` for structured attributes: disable runtime dependency scanning for specific outputs (useful for self-contained FS images). Requires `discard-references` experimental feature.

**Breaking changes:**
- If your flake lives in a subdirectory of a repo and you relied on `.outPath` pointing to the repo root, it now points to the `flake.nix` directory. Rare but possible breakage.

---

### Nix 2.15 (April 2023)

**New capabilities:**
- `nix derivation add` and `nix derivation show` commands (the old `nix show-derivation` is kept as alias)
- Store settings documented in `nix help-stores`
- `nix store ping` and `nix doctor` report whether the remote store trusts the client
- `nix-hash` supports Base64 and SRI formats (`--base64`, `--sri`, `--to-base64`, `--to-sri`)

**Breaking changes:**
- The special handling of `.drv` installables (treating them as all their outputs) is removed. Must use `^` syntax: `myapp.drv^out,dev`. Old invocations with bare `.drv` paths fail differently than before.

---

### Nix 2.16 (May 2023)

**New capabilities:**
- Parallel downloads (`max-substitution-jobs`) decoupled from `--max-jobs`. Default parallel substitution jobs increased from 1 to 16. Significant speedup for binary cache downloads.
- `builtins.replaceStrings` now evaluates replacements lazily (only when the pattern matches)

**Breaking changes:** None for flake workflows.

---

### Nix 2.17 (July 2023)

**New capabilities:**
- `nix-channel --list-generations` subcommand
- `builtins.fetchClosure` can now fetch input-addressed paths in pure eval mode (previously required `--impure`)
- Tarball flakes can redirect to an "immutable" URL recorded in lock files: mutable URLs like `https://example.org/project/latest.tar.gz` are now usable as flake inputs
- Nested dynamic attributes merged correctly (previously silently discarded)
- Unprivileged `allowed-users` can now sign paths (previously only `trusted-users`)

**Breaking changes:** None.

---

### Nix 2.18 (September 2023) — Lix Fork Point

This is the last version before major architectural refactoring. Lix (the community fork) tracks this version as its upstream base.

**New capabilities:**
- `builtins.parseFlakeRef` and `builtins.flakeRefToString`: convert between flake references as attrsets and URL strings. Useful for flake-aware tooling.
- `discard-references` experimental feature stabilized (no longer experimental)
- `--valid-derivers` query option for `nix-store --query`
- `outputOf` builtin for dynamic derivations (experimental)

**Breaking changes:**
- `nix-build --json` output for non-derivation store paths changed: now output as strings, not objects. Scripts parsing JSON output from `nix-build` may break.

---

### Nix 2.19 (November 2023) — Lock File Overhaul

**New capabilities:**
- `nix flake lock` now only creates/adds missing inputs; it never updates existing ones
- `nix flake update` replaces all update operations:
  - No arguments: updates all inputs (same as before)
  - With input names: `nix flake update nixpkgs` updates only that input
  - For external flakes: requires `--flake path/to/flake`
- Shebang interpreter support for the `nix` command (experimental)
- `builtins.convertHash`: convert hash strings between formats
- `nix store add` command replaces `nix store add-file` and `nix store add-path`
- `nix path-info --json` output changed: from list to key-value map format

**Breaking changes:**
- `--recreate-lock-file` and `--update-input` removed from all commands operating on installables. Scripts using `nix build --update-input nixpkgs` must be migrated to `nix flake update nixpkgs && nix build`.
- `nix path-info --json` returns an object instead of an array — scripts parsing this output break.

---

### Nix 2.20 (January 2024) — CLI Reorganization

**New capabilities:**
- `nix show-config` renamed to `nix config show`; `nix doctor` renamed to `nix config check` (old names kept as aliases)
- `nix hash convert` replaces `nix hash to-*` family of commands
- Hash format `base32` renamed to `nix32` (Nix-specific character set)
- `nix profile` now uses human-readable package names instead of numeric indices
- `eval-system` setting: override `builtins.currentSystem` independently of build system
- `mounted-ssh-ng://` store type for full remote access through mounted Nix stores
- Ctrl-C now works reliably in Nix commands
- Function call depth limit prevents stack overflow segfaults

**Breaking changes:**
- Scripts using `nix hash to-base32` must use `nix hash convert --to nix32`
- `nix profile list` output format changed (names instead of indices); scripts parsing it must be updated

---

### Nix 2.21 (March 2024)

**New capabilities:**
- CVE-2024-27297 patched: sandbox escape via cooperating FODs
- `--arg-from-file` and `--arg-from-stdin` for passing file/stdin as string args
- `nix profile remove` and `nix profile upgrade` now match exact names by default; `--regex` for pattern matching
- `inherit (x)` evaluates the expression only once (performance fix)
- REPL: better pretty-printing, concise error messages

**Breaking changes:** None for standard flake workflows.

---

### Nix 2.22 (April 2024)

**New capabilities:**
- `repl-flake` experimental feature removed; `nix repl` redesigned to work consistently with the new CLI: `nix repl <path>` loads a flake (or errors if `flakes` feature not enabled)
- `nix eval` now displays derivations as `.drv` file paths (not attrsets), preventing infinite loops

**Breaking changes:**
- `nix eval nixpkgs#bash` now outputs `«derivation /nix/store/....drv»` instead of an attrset. Scripts that depended on `nix eval` returning an attrset for packages will break.

---

### Nix 2.23 (June 2024)

**New capabilities:**
- `builtins.warn` for configurable warnings (can trigger debugger or abort)
- Large path warnings when copying large paths to the store
- `nix env shell` as primary command; `nix shell` becomes an alias
- `fetchTree` now performs shallow Git clones by default (reduces network usage)
- JSON format for derivations updated: hash algorithm and CA fields separated

**Breaking changes:**
- `fetchTree` shallow-by-default: if you relied on full history in fetched Git repos (e.g., for `git log`), set `shallow = false` explicitly.
- Derivation JSON format changed (affects tooling parsing `nix derivation show` output).

---

### Nix 2.24 (August 2024) — Meson Migration

**New capabilities:**
- CVE-2024-38531 patched: hardened build directory against external interference
- `nix-shell` given a directory now searches for `shell.nix` instead of `default.nix`
- `nix repl :doc` displays function documentation comments (RFC 145)
- Unit prefixes in config: `--min-free 1G` instead of `--min-free 1073741824`
- `libnixflake` library extracted for better code modularity
- Pipe operators `|>` and `<|` added as experimental feature
- Eval cache persistence bug fixed
- **Build system**: migrated from autotools/make to Meson

**Breaking changes:**
- `nix-shell <directory>` behavior changed: now finds `shell.nix`, not `default.nix`. Projects relying on `nix-shell .` to load `default.nix` must rename to `shell.nix` or use `nix-shell default.nix` explicitly.

---

### Nix 2.25 (November 2024)

**New capabilities:**
- Fine-grained XDG directory overrides: `NIX_CACHE_HOME`, `NIX_CONFIG_HOME`, `NIX_DATA_HOME`, `NIX_STATE_HOME`
- Integer overflow is now an error (previously silently wrapped)
- `fsync-store-paths` setting for durable writes before store path registration
- `nix fmt` no longer passes a default `"."` argument; formatters control their own scope
- `<nix/fetchurl.nix>` now enforces TLS verification

**Breaking changes:**
- Integer overflow in Nix expressions now throws an error instead of wrapping. Obscure integer math in Nix code may break.
- `nix fmt` behavior change: formatters that expected `.` as a default argument now receive no argument.

---

### Nix 2.26 (January 2025) — Meson Default, Eval Cache for Dirty Trees

**New capabilities:**
- **Relative path flake inputs**: `inputs.foo.url = "path:./foo"` — references another flake in the same repo. Note: changes the lock file format; lock files with relative path inputs cannot be read by Nix < 2.26.
- **Local flake registries excluded from lock files**: local registry overrides no longer pollute committed lock files
- **Eval cache now works for dirty Git workdirs**: previously disabled for any dirty tree; now works when the tree is dirty but unchanged
- `nix copy --profile` and `nix copy --out-link` flags
- `nix-instantiate --eval --raw` for unquoted output
- NIX_SSHOPTS parsing improved for complex options (proxy commands, spaces)
- Meson is now the default build system for Nix itself

**Breaking changes:**
- Relative path flake inputs change the lock file format. Teams using mixed Nix versions cannot share lock files containing relative-path inputs.

---

### Nix 2.27 (March 2025)

**New capabilities:**
- `inputs.self.submodules = true` in flake.nix declares submodule requirements directly (callers no longer need to set it separately)
- Git LFS support: `lfs = true` fetcher attribute or `inputs.self.lfs = true` in flake
- BLAKE3 hash algorithm support (experimental, via `blake3-hashes` feature flag)
- `nix flake prefetch --out-link` option
- Chroot store provides union filesystem view of host and chroot `/nix/store`

**Breaking changes:** None for standard workflows.

---

### Nix 2.28 (April 2025)

An atypical release focused on C++ API restructuring for the NixOS 25.05 default.

**New capabilities:**
- C++ API headers now require `nix/` prefix: `#include "nix/store/derived-path.hh"`
- `nix_flake_init_global` C API function removed; replaced by `nix_flake_settings_add_to_eval_state_builder`
- Internal: lazy trees prototype further developed (behind flag; not default yet)

**Breaking changes:**
- External tools using the Nix C++ API (libstore, libexpr) must update include paths. The public API was always unstable; this formalizes the new layout.

---

### Nix 2.29 (May 2025)

**New capabilities:**
- `nix repl :reload` works with `:load-flake` and `:load` (previously didn't reload)
- `nix flake show` skips import-from-derivation outputs instead of failing
- Prettified JSON output on terminal (scripts/pipes unaffected)
- REPL continuation prompt changed from invisible spaces to `" > "`
- New C API functions for programmatic flake locking
- Flakes in the Nix store are no longer redundantly copied
- GitHub/GitLab flake refs preserve `host` attribute in lock files (fixes enterprise GitHub/GitLab URLs)
- S3 STS-based authentication support
- `nix formatter build` and `nix formatter run` commands added
- BLAKE3: multi-threaded and memory-mapped IO implementation

**Breaking changes:**
- `nix flake show` silently skips IFD outputs instead of erroring. If you relied on `nix flake check` to catch IFD-using outputs, the behavior has changed.

---

### Nix 2.30 (July 2025)

**New capabilities:**
- **Eval profiler**: `--eval-profiler flamegraph` outputs collapsed call stacks to `nix.profile` (or `--eval-profile-file`). Consumable by `flamegraph.pl` and Speedscope. Default sampling at 99 Hz, configurable via `--eval-profiler-frequency`. Includes function names when available.
- **Value size optimization**: values reduced from 24 to 16 bytes, ~20% reduction in peak heap, ~17% in total bytes during evaluation. Large nixpkgs evaluations are noticeably faster.
- `nix profile install` renamed to `nix profile add` (former kept as alias)
- Non-flake inputs gain `sourceInfo` attribute and support `?dir=subdir` parameters
- `nix repl` displays first 10 loaded variables (previously just a count)
- `nix flake archive --no-check-sigs` option
- `json-log-path` setting for JSON-formatted logs to file/socket
- `trace-import-from-derivation` setting warns on IFD usage
- `builtins.sort` switched to PeekSort for more reliable ordering

**Breaking changes:**
- `build-dir` no longer defaults to `$TMPDIR`; now uses `builds` inside `NIX_STATE_DIR`. CI scripts that set `TMPDIR` to control build location must switch to `build-dir` in `nix.conf`.
- `__json` attribute approach for structured attrs deprecated.
- `nix profile add` is the new canonical name (breaking for scripts that check exact command names in `--help` output).

---

## Quick Reference: What Breaks Between Versions

| Version | What Breaks |
|---------|-------------|
| 2.7 | `nix flake check` warns on `devShell`/`defaultPackage` (old naming) |
| 2.8 | `nix store make-content-addressable` renamed |
| 2.10 | `nix repl '<nixpkgs>'` syntax changed to `nix repl --file '<nixpkgs>'` |
| 2.13 | `--repeat`/`--enforce-determinism` removed |
| 2.15 | `.drv` installables no longer imply all outputs; `.drv^out` required |
| 2.19 | `--recreate-lock-file`/`--update-input` removed; `nix path-info --json` format changed |
| 2.20 | `nix hash to-base32` removed; `nix profile list` output format changed |
| 2.22 | `nix eval` on packages returns `.drv` path string, not attrset |
| 2.23 | `fetchTree` shallow by default; derivation JSON format changed |
| 2.24 | `nix-shell <dir>` finds `shell.nix` not `default.nix` |
| 2.25 | Integer overflow now throws; `nix fmt` doesn't pass `.` by default |
| 2.26 | Relative path inputs change lock file format (incompatible with < 2.26) |
| 2.30 | `build-dir` no longer defaults to `$TMPDIR`; `__json` deprecated |

---

## forge-metal Relevance

- **Remote deploy**: use `nixos-rebuild boot --target-host root@<latitude-ip>` after kernel changes, then `reboot`; use `test` before `switch` for configuration changes
- **SSH ControlMaster gotcha**: if deploys hang at "copying paths", add `NIX_SSHOPTS="-o ControlMaster=no"` temporarily
- **Flake eval cache**: Nix 2.26+ enables eval caching for dirty workdirs, which means `nix develop` from a dirty tree now caches — reduces re-evaluation overhead during active development
- **Relative path inputs**: if splitting `flake.nix` into sub-flakes (e.g., a separate CI flake), Nix ≥ 2.26 supports `path:./ci` as an input; requires all nodes to run Nix ≥ 2.26
- **Eval profiler**: use `nix build --eval-profiler flamegraph --eval-profile-file /tmp/nix-eval.json` to diagnose why `flake.nix` eval is slow (valuable when nixpkgs overlay grows large)
