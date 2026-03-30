# Hydra Self-Hosted CI and NixOS System Configuration Patterns

Covers: Hydra CI architecture for flakes, `hydraJobs` output type, `lib.recurseIntoAttrs`, `nix-eval-jobs`, binary cache integration, `services.hydra` NixOS module, `system.activationScripts` DAG, `systemd.services` hardening, `systemd.tmpfiles.rules`, `networking.nftables`, `virtualisation.oci-containers`, `nixpkgs.overlays` in modules, `hardware.enableRedistributableFirmware`, the impermanence pattern, and `system.etc.overlay`.

---

## Part A: Hydra — Self-Hosted Nix CI

### Overview

Hydra is the official Nix continuous integration system. It is what powers `cache.nixos.org` and the official NixOS channel evaluation. It runs exclusively on NixOS and stores all state in PostgreSQL. Conceptually it organizes work as **projects → jobsets → jobs**, where each job is a derivation Hydra will build.

Unlike generic CI systems, Hydra is tightly integrated with the Nix store: it can serve as a binary cache, sign NARs on-the-fly, upload to S3, and gate channel advancement on multi-platform build success.

Source: https://github.com/NixOS/hydra

---

### `hydraJobs` Output Type

`hydraJobs` is the **only flake output that Hydra natively understands**. Hydra ignores `packages`, `checks`, and all other standard flake outputs unless you explicitly re-expose them under `hydraJobs`.

```nix
# flake.nix
{
  outputs = { self, nixpkgs }: {
    # Standard outputs ignored by Hydra:
    packages.x86_64-linux.myapp = ...;

    # What Hydra actually reads:
    hydraJobs = {
      inherit (self) packages;  # re-expose packages under hydraJobs
    };
  };
}
```

Hydra walks the `hydraJobs` attrset depth-first, treating every derivation it finds as a job to build. Non-derivation values (attrsets, strings, etc.) are either recursed into (if tagged) or skipped.

The conventional layout mirrors `packages` nesting:

```nix
hydraJobs = {
  x86_64-linux = {
    myapp = pkgs.myapp;
    mylib = pkgs.mylib;
  };
  aarch64-linux = {
    myapp = pkgsCross.myapp;
  };
};
```

**Fallback behavior**: If a flake has no `hydraJobs` output, Hydra falls back to evaluating `flake.checks`. This fallback is a convenience for simple projects; production setups should define `hydraJobs` explicitly.

---

### `lib.recurseIntoAttrs`

By default, Hydra **does not recurse into nested attrsets** unless they carry a special marker. `lib.recurseIntoAttrs` adds `recurseForDerivations = true` to an attrset, which signals Hydra (and `nix flake check`) to descend into it:

```nix
# Implementation (from nixpkgs lib):
recurseIntoAttrs = attrs: attrs // { recurseForDerivations = true; };
```

**Critical behavior**: `recurseIntoAttrs` applies only to the **immediate** attrset, not transitively. Nested attrsets inside a `recurseIntoAttrs`-marked set still need their own annotation if they contain further nesting.

```nix
hydraJobs = {
  # Hydra recurses into this because recurseForDerivations = true
  myPackages = pkgs.lib.recurseIntoAttrs {
    hello = pkgs.hello;
    # This inner attrset would NOT be recursed without its own annotation:
    tools = pkgs.lib.recurseIntoAttrs {
      curl = pkgs.curl;
    };
  };
};
```

**Silent skip hazard**: If you forget `recurseIntoAttrs` on a nested attrset, Hydra silently skips all jobs inside it. No error is emitted. This is a common source of "why isn't Hydra building X" confusion.

`pkgs.lib.dontRecurseIntoAttrs` is the inverse — it removes the marker. Useful when inheriting a top-level attrset that is already tagged and you want to exclude a sub-attrset.

---

### Flake Jobset Configuration

When Hydra evaluates a flake-based jobset, it does **not** use `import ./flake.nix`. Instead it calls `builtins.getFlake "<flake-ref>"`, which respects the lockfile and fetches all inputs from the Nix store (not from the network at eval time).

**Jobset settings in the Hydra web UI or JSON API**:

| Field | Value |
|-------|-------|
| Type | `flake` |
| Flake URI | `git+https://git.example.com/org/repo.git` or `github:org/repo` |

Hydra evaluates flakes in **restricted mode** (`--restrict-eval`). This blocks access to paths outside the Nix store, including flake inputs fetched from arbitrary URLs. You must whitelist prefixes in `nix.settings.allowed-uris`:

```nix
nix.settings.allowed-uris = [
  "github:"
  "git+https://github.com/"
  "git+ssh://github.com/"
  "https://releases.example.com/"
];
```

Without these entries, Hydra evaluation fails with a cryptic "access to URI denied" error even if the flake lock file already pins the input.

---

### Hydra Build Products

A derivation can publish **download links** in the Hydra UI by writing a `$out/nix-support/hydra-build-products` file during its build phase:

```bash
# In a derivation's buildPhase or installPhase:
mkdir -p $out/nix-support
echo "file binary $out/bin/myapp" >> $out/nix-support/hydra-build-products
echo "file tarball $out/dist/myapp-1.0.tar.gz" >> $out/nix-support/hydra-build-products
echo "doc html $out/share/doc/index.html" >> $out/nix-support/hydra-build-products
```

**Line format**: `<type> <subtype> <path> [<name>]`

| Type | Meaning |
|------|---------|
| `file` | Generic file download |
| `doc` | Documentation (linked specially in UI) |
| `report` | Test report |
| `nix-build` | A Nix build result |

The Hydra web UI renders these as clickable download links on the build page.

**Channel publishing**: To publish a derivation as a Nix channel entry, write:

```bash
echo "channel" > $out/nix-support/hydra-channel-name
```

This makes the build output available as a Nix channel via Hydra's channel endpoints at `https://<hydra-host>/channel/custom/<project>/<jobset>/<job>/latest`.

---

### Aggregate Jobs

`pkgs.releaseTools.aggregate` creates a meta-job that succeeds only when all its constituent jobs succeed. This is the standard Hydra pattern for **gating a release on multi-platform build success**:

```nix
hydraJobs = rec {
  x86_64-linux.myapp = pkgs.myapp;
  aarch64-linux.myapp = pkgsCross.myapp;

  # Gate: succeeds only when all constituents pass
  release = pkgs.releaseTools.aggregate {
    name = "myapp-release";
    constituents = [
      x86_64-linux.myapp
      aarch64-linux.myapp
    ];
    meta.description = "All platforms pass before release";
  };
};
```

**GC optimization**: Constituent jobs can also be specified as **strings** (derivation store paths) rather than derivation values. When Hydra schedules a string-constituent aggregate, Nix's GC can reclaim intermediate build products sooner because the evaluator holds no live references to those derivations. This matters at nixpkgs scale.

Hydra's UI displays aggregate jobs with a constituent status table, making it easy to see which platform failed.

---

### `nix-eval-jobs` — Hydra's Evaluator

Hydra does not call `nix-instantiate` directly. Instead it uses **`nix-eval-jobs`** (`github:NixOS/nix-eval-jobs`) to evaluate `hydraJobs` in parallel:

```bash
nix-eval-jobs \
  --gc-roots-dir /nix/var/nix/gcroots/hydra/eval-123 \
  --workers 4 \
  --max-memory-size 2048 \
  --flake 'github:NixOS/patchelf#hydraJobs'
```

**JSON Lines output schema** (one object per job per line):

```json
{
  "attr": "x86_64-linux.myapp",
  "attrPath": ["x86_64-linux", "myapp"],
  "drvPath": "/nix/store/abc123-myapp.drv",
  "outputs": { "out": "/nix/store/def456-myapp" },
  "system": "x86_64-linux",
  "isCached": false,
  "meta": { "description": "...", "license": "..." }
}
```

Key options:

| Flag | Effect |
|------|--------|
| `--workers N` | Parallel eval workers (default: 1) |
| `--max-memory-size MB` | Per-worker RSS limit (default: 4096) |
| `--gc-roots-dir` | Creates `.drv` GC roots preventing mid-eval GC races |
| `--flake URI` | Evaluate a flake instead of a legacy `.nix` file |
| `--meta` | Include `meta` attrset in JSON output |
| `--check-cache-status` | Add `isCached: "local" | "cached" | "notBuilt"` |

**GC root safety**: The `--gc-roots-dir` flag is essential in production. Without it, a concurrent `nix-collect-garbage` can delete `.drv` files that `nix-eval-jobs` just instantiated but hasn't yet scheduled for building, causing build failures with "path not found" errors.

The project lives at `github:NixOS/nix-eval-jobs` (canonically under the NixOS org); `github:nix-community/nix-eval-jobs` is a mirror/fork maintained by the same team (`@Mic92`, `@adisbladis`).

---

### Self-Hosting Hydra on NixOS

Minimum viable `services.hydra` configuration:

```nix
# configuration.nix
{ config, pkgs, ... }:
{
  services.hydra = {
    enable = true;
    hydraURL = "https://ci.example.com";        # Public URL (used in email links)
    notificationSender = "hydra@example.com";   # From address for build notifications
    buildMachinesFiles = [];                     # Use localhost as builder (default)
    useSubstitutes = true;                       # Fetch pre-built paths from substituters
    minimumDiskFree = 5;                         # Pause queue runner if < 5 GiB free
    minimumDiskFreeEvaluator = 2;               # Pause evaluator if < 2 GiB free
  };

  # Required: Hydra needs a local PostgreSQL instance
  services.postgresql = {
    enable = true;
    identMap = "hydra-users hydra hydra";
  };

  # Required: Hydra runs as the 'hydra' user and builds as 'hydra-queue-runner'
  nix.settings.allowed-users = [ "hydra" "hydra-queue-runner" ];
  nix.settings.trusted-users = [ "hydra-queue-runner" ];
}
```

**Full `services.hydra` module options**:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `hydraURL` | string | — | Base URL for web UI; used in email notification links |
| `notificationSender` | string | — | From address for build notification emails |
| `minimumDiskFree` | int (GiB) | 0 | Queue runner halts when disk falls below this |
| `minimumDiskFreeEvaluator` | int (GiB) | 0 | Evaluator halts when disk falls below this |
| `extraConfig` | lines | "" | Raw text appended to `hydra.conf` |
| `useSubstitutes` | bool | false | Allow builders to use binary substituters |
| `buildMachinesFiles` | list of path | [] | Files listing remote build machines |
| `listenHost` | string | `"*"` | Bind address for web server |
| `port` | port | 3000 | HTTP port |
| `gcRootsDir` | path | `/nix/var/nix/gcroots/hydra` | GC roots for build outputs |
| `debugServer` | bool | false | Enable Catalyst debug mode |
| `extraEnv` | attrs | {} | Extra environment variables for Hydra processes |

---

### Binary Cache: Hydra as Substituter

Hydra serves the Nix binary cache protocol over HTTP. Clients configure it as a substituter:

```nix
# On machines that consume the cache:
nix.settings = {
  substituters = [ "https://ci.example.com" ];
  trusted-public-keys = [ "ci.example.com:AAAA...+w=" ];
};
```

**Signing and S3 upload** via `extraConfig`:

```nix
services.hydra.extraConfig = ''
  # Sign NARs on-the-fly and upload to S3:
  store_uri = s3://my-cache-bucket?compression=zstd&parallel-compression=true&write-nar-listing=1&log-compression=br&secret-key=/run/keys/hydra-cache-key

  # Or serve from local Nix store and sign in-place:
  binary_cache_secret_key_file = /run/keys/hydra-signing-key

  # Tune NAR compression threads:
  compress_num_threads = 4

  # Git input fetch timeout (seconds):
  <git-input>
    timeout = 3600
  </git-input>
'';
```

The `store_uri` parameter accepts any Nix store URI. The `s3://` form requires the Hydra process to have AWS credentials (via `extraEnv` or instance IAM role). `secret-key` is a query parameter of the store URI, not a separate config key.

Generate a signing keypair:

```bash
nix-store --generate-binary-cache-key ci.example.com \
  /etc/nix/hydra-secret-key \
  /etc/nix/hydra-public-key
```

---

## Part B: NixOS System Configuration Deep Patterns

### `system.activationScripts` — DAG-Ordered Activation

NixOS activation runs a generated shell script when you do `nixos-rebuild switch`. The script is built from named fragments via `system.activationScripts`:

```nix
system.activationScripts.mySetup = {
  text = ''
    mkdir -p /var/lib/myapp
    chown myapp:myapp /var/lib/myapp
  '';
  deps = [ "users" "groups" ];  # Run after 'users' and 'groups' scripts
};
```

**Structure**:

- Each script is either a bare string or `{ text = "..."; deps = [ "name1" "name2" ]; }`.
- The `deps` list names other activation scripts that must run first.
- Internally, `lib.textClosureList` performs a topological sort, producing a flat shell script with all fragments in dependency order.
- The final script is stored at `/nix/var/nix/profiles/system/activate`.

**Built-in activation scripts** (partial list, useful as dep targets):

| Name | Purpose |
|------|---------|
| `specialfs` | Mount `/proc`, `/sys`, `/dev` |
| `users` | Create/update user accounts |
| `groups` | Create/update groups |
| `etc` | Install `/etc` files (the Perl/overlay step) |
| `var` | Create `/var` directories |

**Dry activation**: Scripts can declare `supportsDryActivation = true` to be safe for `nixos-rebuild dry-activate`. A script with this flag should only read state, not modify it:

```nix
system.activationScripts.dryFriendly = {
  supportsDryActivation = true;
  text = "echo 'would configure X'";
  deps = [ "etc" ];
};
```

**`systemd.tmpfiles.rules` as a better alternative**: For creating directories, setting permissions, and writing symlinks, prefer `systemd.tmpfiles.rules` over activation scripts. tmpfiles rules are declarative, idempotent, and run at every boot, whereas activation scripts only run on `nixos-rebuild switch`.

---

### Specialisations

Specialisations let you define named **variants** of a system configuration that share the same base config. Each variant gets its own entry in the bootloader menu and can be activated at runtime without a reboot.

```nix
# In your NixOS flake configuration:
specialisation = {
  with-nvidia.configuration = {
    hardware.nvidia.enable = true;
    services.xserver.videoDrivers = [ "nvidia" ];
  };
  debug-kernel.configuration = {
    boot.kernelPackages = pkgs.linuxPackages_latest;
    boot.kernelParams = [ "debug" ];
  };
};
```

**`inheritParentConfig`** defaults to `true` — the specialisation inherits and extends the parent config. Set it to `false` to start from scratch (rare; useful for minimal testing environments).

**Boot integration**: `nixos-rebuild boot` adds entries like `NixOS - generation 42 - with-nvidia` to GRUB/systemd-boot.

**Runtime switch** (no reboot required for non-kernel changes):

```bash
# Switch to a specialisation at runtime:
/run/current-system/specialisation/with-nvidia/bin/switch-to-configuration switch

# Or via nixos-rebuild:
nixos-rebuild switch --specialisation with-nvidia
```

The specialisation closure lives at `/run/current-system/specialisation/<name>/`. Changes that require a kernel reload (new kernel, changed kernel modules) cannot be fully activated without reboot.

**Detecting the active specialisation in config**:

```nix
# Apply X only when NOT in a specialisation:
services.someService.enable = lib.mkIf (config.specialisation == {}) true;
```

---

### `systemd.services.<name>` — Security Hardening Patterns

NixOS exposes all systemd service options as module-level attributes. The hardening options below are the most commonly relevant for server workloads.

#### DynamicUser

```nix
systemd.services.myapp.serviceConfig.DynamicUser = true;
```

Creates an ephemeral user and group at service start, tears them down at stop. The UID is allocated from a high range (62000+) and is not stable across service restarts. Pairs with `StateDirectory`:

```nix
serviceConfig = {
  DynamicUser = true;
  StateDirectory = "myapp";    # Creates /var/lib/myapp, owned by the ephemeral user
  CacheDirectory = "myapp";    # Creates /var/cache/myapp
  LogsDirectory = "myapp";     # Creates /var/log/myapp
};
```

**Caveat**: `DynamicUser = true` implicitly enables a private `/tmp` and several namespace-based restrictions. This can conflict with services that need stable UIDs, bind-mount host paths, or share a namespace with other services. In practice, services that write to `/var/lib` via `StateDirectory` work well; services with complex inter-process communication may not.

#### Directory Options

| Option | Creates path | Owned by service user |
|--------|-------------|----------------------|
| `StateDirectory = "name"` | `/var/lib/name` | Yes |
| `CacheDirectory = "name"` | `/var/cache/name` | Yes |
| `LogsDirectory = "name"` | `/var/log/name` | Yes |
| `RuntimeDirectory = "name"` | `/run/name` | Yes (deleted on stop) |
| `ConfigurationDirectory = "name"` | `/etc/name` | Read-only in unit |

Paths are **automatically created** by systemd before the service starts. No `mkdir` in activation scripts needed. Multiple names allowed: `StateDirectory = "myapp myapp/data"`.

#### Filesystem Namespacing

```nix
serviceConfig = {
  PrivateTmp = true;          # Private /tmp and /var/tmp (not shared with host)
  ProtectSystem = "strict";   # /usr, /boot, /etc read-only; "full" also makes /etc read-only
  ProtectHome = true;         # /home, /root, /run/user inaccessible
  ProtectKernelTunables = true;  # /proc/sys, /sys read-only
  ProtectKernelModules = true;   # Cannot load/unload kernel modules
  ProtectKernelLogs = true;      # /proc/kmsg inaccessible
  ProtectClock = true;           # Cannot change system clock
  ProtectControlGroups = true;   # Cgroup filesystem read-only

  # Explicit bind mounts when you need host paths under a strict namespace:
  BindPaths = [ "/data/uploads:/uploads" ];
  BindReadOnlyPaths = [ "/etc/ssl/certs:/etc/ssl/certs" ];

  # Grant access to specific /dev nodes (PrivateDevices removes /dev entirely):
  PrivateDevices = true;
  DeviceAllow = [ "/dev/net/tun rw" ];  # Override PrivateDevices for one device

  # Drop all capabilities except what's needed:
  CapabilityBoundingSet = [ "CAP_NET_BIND_SERVICE" ];
  AmbientCapabilities = [ "CAP_NET_BIND_SERVICE" ];
  NoNewPrivileges = true;
};
```

**`ProtectSystem` values**:
- `true` / `"yes"`: `/usr` and `/boot` read-only
- `"full"`: additionally makes `/etc` read-only
- `"strict"`: makes entire filesystem read-only except `StateDirectory`/`CacheDirectory`/`RuntimeDirectory` paths

**`ProtectHome` values**:
- `true` / `"yes"`: `/home`, `/root`, `/run/user` inaccessible
- `"read-only"`: accessible but read-only
- `"tmpfs"`: visible as empty tmpfs

#### Syscall Filtering

```nix
serviceConfig.SystemCallFilter = [
  "@system-service"       # Allow all calls typically needed by services
  "~@privileged"          # Block privileged syscalls
  "~@resources"           # Block resource management syscalls
];
```

Predefined groups (`@system-service`, `@network-io`, `@io-event`, `@timer`, `@process`, `@signal`, `@ipc`, `@privileged`, etc.) reduce the syscall surface without manually listing hundreds of syscalls.

---

### `systemd.tmpfiles.rules`

`systemd.tmpfiles.rules` is a list of strings following the `tmpfiles.d(5)` format. Rules are applied at boot (via `systemd-tmpfiles-setup.service`) and can also be triggered manually with `systemd-tmpfiles --create`. This replaces manual `mkdir`/`chown` in activation scripts for persistent paths.

```nix
systemd.tmpfiles.rules = [
  # Type  Path               Mode  User   Group  Age  Argument
  "d     /var/lib/myapp      0750  myapp  myapp  -    -"
  "d     /var/lib/myapp/db   0700  myapp  myapp  -    -"
  "L     /opt/myapp/config   -     -      -      -    /etc/myapp/config"
  "z     /var/lib/myapp      0750  myapp  myapp  -    -"
  "Z     /var/lib/myapp      0750  myapp  myapp  -    -"
  "f     /var/lib/myapp/db/init.flag  0600  myapp  myapp  -  -"
];
```

**Key type characters**:

| Type | Action |
|------|--------|
| `d` | Create directory if absent; do NOT clear contents |
| `D` | Create directory if absent; clear contents on boot (for runtime dirs) |
| `e` | Adjust permissions/ownership on existing dir |
| `f` | Create file if absent |
| `F` | Create/truncate file |
| `L` | Create symlink; argument is the symlink target |
| `L+` | Force-create symlink (remove and recreate if exists) |
| `z` | Set permissions/ownership/label on path (non-recursive) |
| `Z` | Set permissions/ownership/label recursively |
| `r` | Remove file if exists |
| `R` | Recursively remove path |

**Cleanup age**: The `Age` field (`-` for never) controls when `systemd-tmpfiles --clean` removes files. Format: `10d`, `1h`, `30s`. Used primarily for runtime/cache directories.

**`systemd.tmpfiles.settings`**: NixOS 23.11+ adds a structured alternative to the raw string format:

```nix
systemd.tmpfiles.settings."myapp-dirs" = {
  "/var/lib/myapp" = {
    d = { mode = "0750"; user = "myapp"; group = "myapp"; };
  };
};
```

---

### `networking.nftables` vs `networking.firewall`

NixOS ships two firewall backends:

- **Default**: iptables-based, via `networking.firewall.enable = true` (the default)
- **Modern**: nftables-based, enabled with `networking.nftables.enable = true`

```nix
networking = {
  nftables.enable = true;  # Switch backend to nftables

  # These options work with both backends:
  firewall = {
    enable = true;
    allowedTCPPorts = [ 80 443 22 ];
    allowedUDPPorts = [ 51820 ];  # WireGuard
  };
};
```

**What `networking.nftables.enable = true` does**:

1. Creates a `nixos-fw` nftables table with chains: `input`, `forward`, `output`, and subchains `input-allow`, `input-deny`.
2. Rules from `networking.firewall.allowedTCPPorts` etc. are rendered as nft rules in the `input-allow` chain.
3. Rules are applied **atomically** (`nft -f <ruleset>`) — the entire ruleset is replaced at once, avoiding the incremental/non-atomic iptables behavior.

**Custom nftables tables**:

```nix
networking.nftables.tables.myCustomTable = {
  family = "inet";
  content = ''
    chain forward {
      type filter hook forward priority 0;
      ct state established,related accept
      drop
    }
  '';
};
```

**Why nftables is preferred for new deployments**:

- Atomic rule replacement prevents race conditions during rule updates.
- Single nft binary replaces `iptables`, `ip6tables`, `arptables`, `ebtables`.
- Better performance for large rulesets (single kernel pass vs. chained iptables traversal).
- Supports sets and maps natively, enabling compact rules for IP allowlists.
- Newer NixOS tooling (Tailscale, WireGuard NixOS modules) is tested against nftables.

**Compatibility warning**: Some older programs (docker < 25, some VPN clients) inject iptables rules at runtime, bypassing the declarative ruleset. When mixing container runtimes with nftables, verify the container runtime's nftables support.

---

### `virtualisation.oci-containers` — Declarative Container Management

`virtualisation.oci-containers` lets you run Docker/Podman containers declaratively in NixOS without writing systemd unit files by hand. The module generates a `<backend>-<name>.service` systemd unit for each container.

```nix
virtualisation.oci-containers = {
  backend = "podman";  # or "docker"; podman is default since NixOS 22.05

  containers.clickhouse = {
    image = "clickhouse/clickhouse-server:24.3";
    ports = [ "127.0.0.1:8123:8123" "127.0.0.1:9000:9000" ];
    volumes = [
      "/var/lib/clickhouse:/var/lib/clickhouse"
      "/etc/clickhouse-server:/etc/clickhouse-server:ro"
    ];
    environment = {
      CLICKHOUSE_DB = "mydb";
      CLICKHOUSE_USER = "default";
    };
    autoStart = true;
    extraOptions = [ "--ulimit" "nofile=262144:262144" ];
  };
};
```

**Full option set**:

| Option | Type | Description |
|--------|------|-------------|
| `image` | string | OCI image reference (required) |
| `imageFile` | path or null | Pre-built image from Nix store (skips pull) |
| `pull` | `"missing"` \| `"always"` \| `"never"` \| `"newer"` | Pull policy |
| `ports` | list of string | `"[host-ip:]host-port:container-port[/proto]"` |
| `volumes` | list of string | `"source:destination[:options]"` |
| `environment` | attrset | Env vars injected into container |
| `environmentFiles` | list of path | `.env` files (for secrets) |
| `networks` | list of string | Container networks to join |
| `dependsOn` | list of string | Other container names to start first |
| `extraOptions` | list of string | Raw flags appended to `podman run` |
| `cmd` | list of string | Override container CMD |
| `entrypoint` | string | Override ENTRYPOINT |
| `user` | string | User inside container |
| `autoStart` | bool | Start on boot (default: true) |
| `autoRemoveOnStop` | bool | Remove container on stop (default: true) |
| `log-driver` | string | Log driver (default: `"journald"`) |
| `serviceName` | string | Override generated systemd unit name |

**Systemd integration**: The generated unit uses `After=` and `Requires=` for `dependsOn` containers, integrating fully with the systemd dependency graph. Other NixOS services can depend on `podman-<name>.service`:

```nix
systemd.services.myapp = {
  after = [ "podman-clickhouse.service" ];
  requires = [ "podman-clickhouse.service" ];
};
```

**Podman-specific `sdnotify` modes**:

| Mode | Ready signal sent when |
|------|----------------------|
| `"conmon"` | Container process starts (default) |
| `"healthy"` | Container health check passes |
| `"container"` | Container sends `NOTIFY_SOCKET` notification itself |

**Rootless caveat**: By default, `virtualisation.oci-containers` runs containers as root. Rootless Podman requires additional configuration and has known issues with some features (volume permissions, user namespacing).

**Nix-store images**: `imageFile` lets you skip the registry entirely:

```nix
containers.myapp = {
  imageFile = pkgs.dockerTools.buildLayeredImage { ... };
  image = "myapp:latest";  # Must match the image tag in imageFile
};
```

---

### `nixpkgs.overlays` in NixOS Modules

When you set `nixpkgs.overlays` inside a NixOS module (including in `configuration.nix` or imported modules), the overlays are applied to the **system `pkgs` instance** — the same `pkgs` that all other NixOS modules receive.

```nix
# In configuration.nix or a NixOS module:
{ config, pkgs, lib, ... }:
{
  nixpkgs.overlays = [
    (final: prev: {
      myapp = prev.myapp.overrideAttrs (old: {
        version = "2.0";
        src = ...;
      });
    })
  ];
}
```

**Module-level vs. flake-level overlays**:

| Location | Scope |
|----------|-------|
| `nixpkgs.overlays` in NixOS config | Applied to system `pkgs`; affects all modules on this host |
| `overlays.default` in flake outputs | Exported for downstream consumers; not automatically applied anywhere |
| `nixpkgs.overlays` passed to `nixpkgs.lib.nixosSystem` | Same as module-level; these are merged |

**`nixpkgs.config` in NixOS modules**:

```nix
nixpkgs.config = {
  allowUnfree = true;         # Allow unfree packages in system config
  allowBroken = false;
  permittedInsecurePackages = [ "openssl-1.1.1w" ];
};
```

**Scope limitation**: `nixpkgs.config.allowUnfree = true` in NixOS configuration only affects `nixos-rebuild`-managed system packages. It does **not** affect `nix shell`, `nix run`, or user-level `nix profile` commands. Those require `~/.config/nix/nix.conf` setting `extra-trusted-users` or the `NIXPKGS_ALLOW_UNFREE=1` environment variable.

**`legacyPackages` gotcha in overlays**: If you expose `legacyPackages` in a flake and a consumer does `nixpkgs.overlays = [ inputs.yourFlake.overlays.default ]`, the overlay is applied to their `pkgs` but the `legacyPackages` attrset in your flake is not recomputed — it was evaluated once at flake import time with the original `pkgs`. Always use `overlays` output (not `legacyPackages`) for distributing modifications.

---

### `hardware.enableRedistributableFirmware`

```nix
hardware.enableRedistributableFirmware = true;
```

This installs firmware packages with licenses permitting redistribution into the system closure. Firmware is loaded by the kernel from `/run/current-system/firmware` (a symlink chain into the Nix store).

**What it installs** (the `linux-firmware` package plus supplements):

- `linux-firmware` — Intel WiFi (iwlwifi), AMD GPU (amdgpu), Realtek, Broadcom, etc.
- `sof-firmware` — Sound Open Firmware (modern Intel/AMD audio)
- `alsa-firmware` — Legacy ALSA firmware
- `ipw2200-firmware`, `rt5677-firmware`, `rtl8192su-firmware`, `rtl8761b-firmware`, `zd1211fw` — specific wireless chipsets

**`hardware.enableAllFirmware`**: Superset of `enableRedistributableFirmware`. Adds proprietary-but-redistributable packages:
- `broadcom-bt-firmware`
- `b43Firmware_5_1_138`, `b43Firmware_6_30_163_46` (Broadcom 43xx WiFi)
- `xone-dongle-firmware` (Xbox One wireless adapter)

Requires `nixpkgs.config.allowUnfree = true`.

**Server relevance**: Bare metal servers frequently need redistributable firmware for:
- NIC firmware (Mellanox/NVIDIA ConnectX, Broadcom NIX)
- NVMe microcontroller firmware (some drives)
- BMC/IPMI firmware on supported boards

Servers generally do **not** need `enableAllFirmware` unless they have consumer-grade wireless cards.

**Firmware path**: NixOS creates `/run/current-system/firmware` pointing into the Nix store and appends it to `firmware_class.path` kernel parameter at boot, so `request_firmware()` in kernel drivers finds it.

---

### Impermanence Pattern

The impermanence module (`github:nix-community/impermanence`) inverts the traditional persistence model: the root filesystem (`/`) is ephemeral (tmpfs or btrfs subvolume rolled back on boot), and only **explicitly declared paths** survive reboots.

**Motivation**: Forces all persistent state to be intentional and auditable. Prevents configuration drift from manual edits to `/etc`. The system is always "one reboot away" from a clean state defined entirely by Nix.

#### NixOS Setup (tmpfs root)

```nix
# flake.nix — add impermanence as input:
inputs.impermanence.url = "github:nix-community/impermanence";

# configuration.nix:
{ config, lib, ... }:
{
  imports = [ inputs.impermanence.nixosModules.impermanence ];

  # Root is tmpfs; nothing persists without explicit declaration:
  fileSystems."/" = { device = "tmpfs"; fsType = "tmpfs"; options = [ "size=4G" ]; };
  fileSystems."/nix" = { device = "/dev/disk/by-label/nix"; fsType = "ext4"; };

  # Persistent storage mount (survives reboots):
  fileSystems."/persist" = { device = "/dev/disk/by-label/persist"; fsType = "ext4"; neededForBoot = true; };

  environment.persistence."/persist" = {
    hideMounts = true;   # Hide bind mounts from file managers

    directories = [
      "/var/lib/nixos"       # NixOS user/group state
      "/var/lib/systemd"     # systemd state (failed units, etc.)
      "/etc/NetworkManager"  # Network config
      { directory = "/var/log"; mode = "0755"; }
      { directory = "/var/lib/clickhouse"; user = "clickhouse"; group = "clickhouse"; mode = "0700"; }
    ];

    files = [
      "/etc/machine-id"
      "/etc/ssh/ssh_host_ed25519_key"
      "/etc/ssh/ssh_host_ed25519_key.pub"
    ];
  };
}
```

**How bind mounting works**: The module creates bind mounts during the activation phase (before any services start), linking `/var/lib/clickhouse` → `/persist/var/lib/clickhouse`, etc. If the persistent directory doesn't exist yet, it is created with the specified ownership/mode.

**`method` option for files**: Each file entry can specify `method = "symlink"` (default: `"auto"`, which uses bind mount if the file exists persistently, symlink otherwise).

#### Btrfs subvolume approach (more robust than tmpfs)

```nix
# On each boot, a startup script (in initrd) does:
# 1. Mount btrfs root
# 2. Move current @ subvolume to @old-<date>
# 3. Create fresh @ subvolume from @blank snapshot
# 4. Mount new @ as /
# 5. Continue boot
```

This approach preserves the previous root for crash recovery (old roots are deleted after N days).

#### Home Manager integration

```nix
# home.nix:
home.persistence."/persist/home/alice" = {
  directories = [
    "Documents"
    ".config/nvim"
    { directory = ".ssh"; mode = "0700"; }
  ];
  files = [ ".bash_history" ];
};
```

**Gotcha for servers**: `/etc/machine-id` must be persisted or it changes on every boot, breaking systemd journal linking, DHCP client IDs, and other machine-ID-dependent features.

---

### `system.etc.overlay.enable`

```nix
system.etc.overlay.enable = true;  # Experimental; default false
```

An experimental alternative to NixOS's traditional `/etc` generation (which uses a Perl script to copy/symlink files into `/etc`). Instead, mounts `/etc` as an overlayfs combining:

- **Lower layer**: EROFS read-only image containing the system's etc files with correct ownership and permissions metadata
- **Upper layer** (when `mutable = true`, the default): writable tmpfs layer at `/.rw-etc/upper` that captures runtime edits to `/etc`

**Activation improvement**: The overlay approach allows atomic switch between system generations without running the Perl generation script, which iterates all `/etc` entries. On systems with large `/etc` (many files, complex config), this can noticeably speed up `nixos-rebuild switch`.

**Required kernel features**: `redirect_dir=on`, `metacopy=on` in the overlayfs kernel module. Standard in kernels ≥ 5.15.

**Mutable vs. immutable**:

```nix
system.etc.overlay = {
  enable = true;
  mutable = false;  # /etc is fully read-only; runtime edits to /etc fail
};
```

Setting `mutable = false` makes `/etc` entirely read-only. Useful for security-hardened or reproducibility-focused setups. Some services that write to `/etc` at runtime (e.g., legacy tools that update `/etc/resolv.conf` directly) will break.

**Known incompatibility**: `nixos-install` (for initial OS installation) does not work with `system.etc.overlay.enable = true`. It will fail during bootloader installation. Disable this option when using `nixos-install`, then re-enable after first boot.

**Status as of 2025**: Still marked experimental with the warning "only enable this option if you're confident that you can recover your system if it breaks." Used successfully in production by some teams on NixOS 24.05+.

---

## Patterns for forge-metal

### Hydra for forge-metal

The forge-metal CI architecture (Firecracker microVMs + ZFS clones) benefits from Hydra's binary cache capabilities:

1. **Expose jobs correctly**: Set `hydraJobs.x86_64-linux.bmci = self.packages.x86_64-linux.bmci` rather than relying on Hydra finding `packages` output.
2. **Binary cache for golden image closure**: Point Hydra's `store_uri` at the Backblaze B2 / Cloudflare R2 bucket to cache the `server-profile` closure. Operators on new machines can `nix copy` from there instead of rebuilding.
3. **Aggregate gate**: Use `releaseTools.aggregate` to gate a `release` job on successful builds across the `server-profile` and `bmci` derivations before tagging.
4. **`nix-eval-jobs` outside Hydra**: Run `nix-eval-jobs --flake .#hydraJobs --workers 2` locally to test jobset evaluation before pushing, catching `recurseIntoAttrs` omissions early.

### systemd hardening for ClickHouse and Forgejo

Both services benefit from:

```nix
systemd.services.clickhouse.serviceConfig = {
  ProtectSystem = "strict";
  ProtectHome = true;
  PrivateTmp = true;
  NoNewPrivileges = true;
  ProtectKernelTunables = true;
  ProtectKernelModules = true;
  ProtectKernelLogs = true;
  StateDirectory = "clickhouse";   # /var/lib/clickhouse, avoids manual mkdir
  LogsDirectory = "clickhouse";    # /var/log/clickhouse
};
```

### OCI containers for HyperDX/MongoDB sidecar pattern

Since HyperDX is built from source via Nix, running its MongoDB dependency via `virtualisation.oci-containers` is simpler than packaging MongoDB in Nix:

```nix
virtualisation.oci-containers.containers.mongodb = {
  image = "mongo:6.0";
  volumes = [ "/var/lib/mongodb:/data/db" ];
  environment.MONGO_INITDB_DATABASE = "hyperdx";
  extraOptions = [ "--network=host" ];
};
systemd.services.hyperdx.after = [ "podman-mongodb.service" ];
```

### nftables for port control on bare metal

On a Latitude.sh bare metal server with a public IP, switching to nftables gives atomic rule replacement during `nixos-rebuild switch`, which eliminates the brief window where all ports are open (iptables `-F; -A` pattern):

```nix
networking.nftables.enable = true;
networking.firewall.allowedTCPPorts = [ 22 80 443 ];
```
