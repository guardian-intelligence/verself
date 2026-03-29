# NixOS Impermanence — Ephemeral Root on ZFS

> ZFS rollback on every boot. Persistence is opt-in, not opt-out.
>
> Repo: [nix-community/impermanence](https://github.com/nix-community/impermanence)
> Commit: `7b1d382f`

## The core trick: one line in initrd

```nix
boot.initrd.postResumeCommands = lib.mkAfter ''
  zfs rollback -r rpool/local/root@blank
'';
```

Every boot, root is factory-fresh. Any file you created, any config you tweaked by hand — gone.
The ZFS snapshot pattern taken to its logical extreme.

- [`nixos.nix`](https://github.com/nix-community/impermanence/blob/7b1d382f/nixos.nix) — core module generating systemd mounts and activation scripts
- NixOS 24.11+ moved ZFS imports from `postDeviceCommands` to `postResumeCommands` — must use the new hook

## Persistence is declared, not accumulated

```nix
environment.persistence."/persistent" = {
  directories = [ "/var/log" "/var/lib/nixos" ];
  files = [ "/etc/machine-id" ];
};
```

Everything else is ephemeral. Inverts the mental model from "what should I clean up?" to
"what should I keep?"

- [`submodule-options.nix`](https://github.com/nix-community/impermanence/blob/7b1d382f/submodule-options.nix) — option definitions for directories, files, permissions

## Dataset naming convention signals backup policy

```
rpool/local/root  — ephemeral, rolled back every boot
rpool/local/nix   — persistent, NOT backed up (reconstructible from config)
rpool/safe/home   — persistent, backed up
rpool/safe/persist — persistent, backed up
```

`local` = expendable. `safe` = replicated. The convention encodes operational intent
in the ZFS namespace.

## Discovery: what needs persisting?

```bash
zfs diff rpool/local/root@blank
```

Shows every file that appeared since the clean snapshot. Run your system, see what it wrote,
decide what matters. Empirical, not guesswork.

## Dropped FUSE bindfs: 40-60% I/O penalty

Originally used FUSE `bindfs` for persistence mounts (allows ownership remapping).
Benchmarked and found 40-60% I/O overhead. Switched to native kernel bind mounts.

- Discussed in issue #248 and PR #272

Lesson for forge-metal: avoid FUSE in any hot path. Native mounts only.

## The /etc/machine-id saga

`systemd-machine-id-commit.service` detects `/etc/machine-id` isn't on tmpfs when it's a
bind mount from persistent storage, and fails. Fix: suppress the service after first boot
(`ConditionFirstBoot = true`), pre-create with "uninitialized" content.

- [`mount-file.bash`](https://github.com/nix-community/impermanence/blob/7b1d382f/mount-file.bash) — file bind-mount logic with machine-id special case
- Issues #229, #242

Systemd has deep assumptions about filesystem layout. Any non-standard mount scheme will
hit these edges.

## Boot sequence with impermanence

1. Kernel + initrd load
2. ZFS pool imported (`zfs-import-<pool>.service`)
3. **`postResumeCommands` runs** — `zfs rollback -r rpool/local/root@blank`
4. Root mounted at `/sysroot` — blank slate
5. Boot-critical bind mounts created in initrd (`neededForBoot` paths)
6. NixOS activation scripts run (users, groups, /etc population, persistence dirs)
7. switch-root to real root
8. Remaining systemd mounts fire for non-boot-critical paths
9. Normal service startup

## /var/lib/nixos must persist

Contains UID/GID maps. Without it, users/groups without hardcoded UIDs get reassigned on
every boot, breaking file ownership. The module explicitly warns if this isn't persisted.

- [`create-directories.bash`](https://github.com/nix-community/impermanence/blob/7b1d382f/create-directories.bash) — parent directory creation with ownership cloning

## ZFS datasets don't generate systemd device units

Can't use standard systemd mount dependencies. Workaround: manually add
`after = ["zfs-import-<pool>.service"]` to relevant initrd services.

## For CI: systemd isolation, not ZFS rollback per-job

The srvos project (production NixOS CI runners) uses:
- `DynamicUser = true` — ephemeral UID per runner
- `ProtectSystem = "strict"` — read-only system except own state dir
- `--ephemeral` flag — accept one job, de-register
- State directory wiping on restart

ZFS impermanence runs at the host level to prevent drift. Per-job isolation is systemd's job.
The two complement each other.

## Alternative: rollback-on-shutdown

The archived `chaotic-cx/nyx` project offered `zfs-impermanence-on-shutdown` — rollback when
shutting down instead of booting. Avoids initrd complexity but a hard crash leaves dirty state.
