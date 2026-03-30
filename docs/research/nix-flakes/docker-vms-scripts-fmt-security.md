# Nix: Docker Images, VM Tests, Shebang Scripts, treefmt, Store Security

Covers: `pkgs.dockerTools` OCI image building, NixOS VM tests as flake `checks`, `nix-shell` shebang scripts, `treefmt`/`treefmt-nix` formatting integration, and Nix store security properties.

---

## Topic 1: `pkgs.dockerTools` — Building OCI/Docker Images Without Docker

**Source:** https://ryantm.github.io/nixpkgs/builders/images/dockertools/ | https://github.com/NixOS/nixpkgs/blob/master/pkgs/build-support/docker/default.nix | https://github.com/NixOS/nixpkgs/blob/master/doc/build-helpers/images/dockertools.section.md

### `buildImage` vs `buildLayeredImage`

`buildImage` produces a **single-layer** Docker image. Internally it unpacks the parent image (if any), computes new files via diff using `comm`, and repacks. It uses a Linux VM (via `vmTools.runInLinuxVM`) when `runAsRoot` is specified — this is why `buildImage` with `runAsRoot` requires the `kvm` system feature.

`buildLayeredImage` is a thin wrapper over `streamLayeredImage` that saves the streamed tarball to the Nix store with compression. `streamLayeredImage` is the real engine: it produces a shell script that streams an uncompressed OCI tarball to stdout at runtime. This means `streamLayeredImage`'s output is a **script**, not a tarball — the tarball only exists when you run it:

```bash
# streamLayeredImage output is a script, run it to get the tarball
$(nix-build -A docker-image) | docker load
# Or pipe to skopeo without materializing:
$(nix-build -A docker-image) | gzip --fast | skopeo copy docker-archive:/dev/stdin docker://registry/image:tag
```

### Layer Deduplication for Binary Cache Sharing

This is the key reason to prefer `buildLayeredImage` over `buildImage`. The algorithm — called a "popularity contest" — works as follows:

1. Compute the full closure of all `contents` and `config`-referenced paths.
2. Rank each store path by how many other paths in the closure depend on it (popularity = number of reverse dependencies).
3. The `maxLayers - 2` most popular paths each get their own layer.
4. All remaining less-popular paths share one combined layer.
5. The final layer is the image config.

The default `maxLayers` is 100. Modern Docker supports 128 layers; older versions support as few as 42. The OCI spec does not impose a limit.

**Why this matters for binary caches:** Layer content is content-addressed. If you build two images that both include `glibc`, `bash`, and `openssl`, those layers are identical across builds and across images. A registry pull will skip layers already present locally. With `buildImage`'s single layer, the entire image must be re-pulled even if only one package changes.

The layering pipeline is configurable via `layeringPipeline` (experimental interface). When null, `dockerAutoLayer` (the popularity contest) runs. Custom pipelines use `dockerMakeLayers`.

### `contents` vs `config` in `buildLayeredImage`

```nix
buildLayeredImage {
  name = "myapp";

  # contents: filesystem artifacts — paths/derivations to include
  # Each path listed directly gets a symlink at the image root.
  # E.g., listing pkgs.hello creates /bin/hello -> /nix/store/.../bin/hello
  contents = [ pkgs.bash pkgs.coreutils ];

  # config: Docker Image Spec v1.3.1 runtime metadata
  # The closure of config is AUTOMATICALLY included in the image closure.
  # You do NOT need to list packages in both contents and config.
  config = {
    Cmd = [ "${pkgs.hello}/bin/hello" ];
    Entrypoint = [ "/bin/sh" "-c" ];
    Env = [ "PATH=/usr/bin:/bin" "HOME=/root" ];
    WorkingDir = "/app";
    ExposedPorts = { "8080/tcp" = {}; };
    User = "nobody";
    Labels = { "org.opencontainers.image.source" = "https://github.com/..."; };
    Volumes = { "/data" = {}; };
  };
}
```

Critical non-obvious behavior: **the closure of `config` is automatically walked**. If `config.Cmd` references a store path, that store path and all its dependencies are included in the image even without listing them in `contents`. This means the minimal viable image is just:

```nix
buildLayeredImage {
  name = "hello";
  config.Cmd = [ "${pkgs.hello}/bin/hello" ];
}
# No contents needed — pkgs.hello's closure is pulled in automatically.
```

### `dockerTools.pullImage`

Fetches a pre-built image from a registry using `skopeo`. Requires a pinned digest for reproducibility — tag names are mutable and non-reproducible.

```nix
pullImage {
  imageName = "nixos/nix";
  imageDigest = "sha256:abc123...";
  sha256 = "sha256-...";      # hash of the fetched archive
  finalImageName = "nixos/nix";
  finalImageTag = "2.9.1";
  # os = "linux";             # default
  # arch = defaultArchitecture;  # from go.GOARCH mapping
  # tlsVerify = true;         # default; set false for HTTP registries
}
```

Obtain digest and sha256 via: `nix-prefetch-docker --image-name mysql --image-tag 5 --arch x86_64 --os linux`

### `dockerTools.streamLayeredImage` — Full Signature

```nix
streamLayeredImage {
  name,
  tag ? null,               # defaults to derivation output hash
  fromImage ? null,         # base image tarball; null = FROM scratch
  contents ? [],            # derivations to include
  config ? {},              # Docker runtime config
  architecture ? defaultArchitecture,
  created ? "1970-01-01T00:00:01Z",
  mtime ? "1970-01-01T00:00:01Z",
  uid ? 0,
  gid ? 0,
  uname ? "root",
  gname ? "root",
  maxLayers ? 100,
  extraCommands ? "",       # shell commands in layer finalization (no root)
  fakeRootCommands ? "",    # shell commands with fakeroot privileges
  enableFakechroot ? false, # run fakeRootCommands under proot fakechroot
  includeStorePaths ? true, # include Nix store paths as layers
  includeNixDB ? false,     # populate Nix store database
  passthru ? {},
  meta ? {},
  layeringPipeline ? null,  # custom layer strategy (experimental)
  debug ? false,
}
```

### `dockerTools.buildImageWithNixDb`

Wraps `buildImage` (or `buildLayeredImage`) with `includeNixDB = true`. This populates the Nix store database inside the image at `/nix/var/nix/db/db.sqlite`, loading all store path registrations via `nix-store --load-db`.

**Why it's needed:** Nix commands inside a container (`nix-build`, `nix-env`, etc.) query the SQLite database to know which paths are valid. Without the database, the store paths are present in the filesystem but Nix treats them as unregistered garbage. The registration times are reset to `SOURCE_DATE_EPOCH` for reproducibility.

A warning is emitted: "only the database of the deepest Nix layer is loaded" — if you compose multiple Nix-aware layers, only the innermost database survives. This is a fundamental limitation of the layered architecture.

Source: `mkDbExtraCommand` in `pkgs/build-support/docker/default.nix`:
```nix
${lib.getExe' buildPackages.nix "nix-store"} --load-db < ${
  closureInfo { rootPaths = contentsList; }
}/registration
${lib.getExe buildPackages.sqlite} nix/var/nix/db/db.sqlite \
  "UPDATE ValidPaths SET registrationTime = ''${SOURCE_DATE_EPOCH}"
```

### `fakeRootCommands` — Setting File Permissions

Nix builds run as unprivileged users. `fakeRootCommands` is a string of shell commands executed inside a `fakeroot` environment during the customisation layer build. This allows `chown`, `chmod`, `mknod`, and similar operations that would fail as a regular user — `fakeroot` intercepts system calls and lies about the UID/GID, then records the intended ownership in the tarball metadata.

```nix
buildLayeredImage {
  name = "myapp";
  fakeRootCommands = ''
    chown -R 1000:1000 /app
    chmod 4755 /usr/local/bin/ping   # setuid bit
    mkdir -p /var/log/myapp
    chown nobody:nobody /var/log/myapp
  '';
  enableFakechroot = false;  # set true to also use proot for path isolation
}
```

`enableFakechroot = true` adds `proot` on top of `fakeroot` for scripts that need to see a chrooted filesystem view.

### Determinism — What Is Stripped

Nix-built Docker images are **bit-for-bit reproducible by default**. The sources of non-determinism that are eliminated:

1. **Timestamps:** All tar entries use `--mtime="@$SOURCE_DATE_EPOCH"` and layer `created` defaults to `"1970-01-01T00:00:01Z"` (one second past epoch — zero would be technically invalid per Docker spec).
2. **File ordering:** Tar entries use `--sort=name` for deterministic ordering.
3. **Layer hashes:** Computed from content, not time of build.
4. **Image config hash:** Includes layer digests, so changes propagate correctly.

To get a human-readable creation date (breaks reproducibility):
```nix
buildLayeredImage {
  name = "myapp";
  created = "now";  # uses current date — image hash differs each build
}
```

### The `/etc/passwd` Problem

**Non-obvious:** A Nix-built Docker image has NO `/etc/passwd`, `/etc/group`, or `/etc/nsswitch.conf` by default. The Nix store contains only what you explicitly include — there is no implicit base layer. Many programs (nginx, OpenSSH, PostgreSQL) call `getpwuid()`/`getgrnam()` via NSS at startup and fail or crash without these files.

Two solutions:

**Option 1: `fakeNss`** (recommended for most cases)
```nix
buildLayeredImage {
  name = "nginx";
  contents = [ pkgs.fakeNss pkgs.nginx ];
  # fakeNss provides:
  # /etc/passwd  — root:x:0:0::/root:/bin/sh and nobody:x:65534:65534::/:/sbin/nologin
  # /etc/group   — root:x:0: and nobody:x:65534:
  # /etc/nsswitch.conf — files-only resolution (no LDAP/NIS)
}
```

`fakeNss` also supports extending via override:
```nix
pkgs.fakeNss.override {
  extraPasswdLines = [ "myuser:x:1000:1000::/home/myuser:/bin/bash" ];
  extraGroupLines = [ "myuser:x:1000:" ];
}
```

**Option 2: `shadowSetup`** (for images that need to create users at build time)

`shadowSetup` is a string of shell commands for use inside `runAsRoot`. It creates the full shadow-utils infrastructure and then lets you call `useradd`/`groupadd`:

```nix
buildImage {
  name = "myapp";
  runAsRoot = ''
    ${pkgs.dockerTools.shadowSetup}
    groupadd -r myapp
    useradd -r -g myapp myapp
  '';
}
```

**Do not use both `fakeNss` and `shadowSetup`** — they conflict on `/etc/passwd`.

Source: `pkgs/build-support/docker/default.nix` `shadowSetup` string:
```bash
mkdir -p /etc/pam.d
if [[ ! -f /etc/passwd ]]; then
  echo "root:x:0:0::/root:${runtimeShell}" > /etc/passwd
  echo "root:!x:::::::" > /etc/shadow
fi
if [[ ! -f /etc/group ]]; then
  echo "root:x:0:" > /etc/group
  echo "root:x::" > /etc/gshadow
fi
```

### Minimal Viable Image Pattern (Scratch + Binary)

```nix
# Absolute minimum — no shell, no coreutils, just the binary and its closure
pkgs.dockerTools.buildLayeredImage {
  name = "myapp";
  tag = "latest";
  config = {
    Cmd = [ "${myBinary}/bin/myapp" ];
    Env = [ "SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt" ];
    ExposedPorts = { "8080/tcp" = {}; };
  };
}

# With common helpers for debugging
pkgs.dockerTools.buildLayeredImage {
  name = "myapp-debug";
  contents = [
    pkgs.dockerTools.binSh        # /bin/sh -> bashInteractive
    pkgs.dockerTools.usrBinEnv    # /usr/bin/env -> coreutils env
    pkgs.dockerTools.caCertificates  # /etc/ssl/certs/ca-certificates.crt
    pkgs.fakeNss                  # /etc/passwd, /etc/group
  ];
  config.Cmd = [ "${myBinary}/bin/myapp" ];
}
```

Other common missing packages in scratch images:
- `pkgs.iana-etc` — `/etc/protocols` and `/etc/services`; required for TCP/UDP socket names
- `pkgs.tzdata` — timezone data; required if program calls `localtime()`
- `pkgs.cacert` — CA bundle for TLS verification

### Using as `packages.<system>.docker-image` in a Flake

```nix
{
  outputs = { self, nixpkgs, ... }:
  let
    pkgs = nixpkgs.legacyPackages.x86_64-linux;
  in {
    packages.x86_64-linux.docker-image = pkgs.dockerTools.buildLayeredImage {
      name = "myapp";
      tag = self.shortRev or "dev";
      config.Cmd = [ "${self.packages.x86_64-linux.default}/bin/myapp" ];
    };

    # For streaming (no store materialization):
    packages.x86_64-linux.docker-image-stream = pkgs.dockerTools.streamLayeredImage {
      name = "myapp";
      config.Cmd = [ "${self.packages.x86_64-linux.default}/bin/myapp" ];
    };
  };
}
```

Build and push:
```bash
nix build .#packages.x86_64-linux.docker-image
docker load < result

# Or stream directly (more efficient for large images):
$(nix build --print-out-paths .#packages.x86_64-linux.docker-image-stream) | \
  docker load
```

---

## Topic 2: NixOS VM Tests as `checks` in Flakes

**Sources:** https://github.com/NixOS/nixpkgs/blob/master/nixos/doc/manual/development/writing-nixos-tests.section.md | https://nix.dev/tutorials/nixos/integration-testing-using-virtual-machines.html | https://nixcademy.com/posts/nixos-integration-tests/ | https://blakesmith.me/2024/03/02/running-nixos-tests-with-flakes.html | https://github.com/NixOS/nixpkgs/blob/master/nixos/tests/nginx.nix

### What `pkgs.testers.runNixOSTest` Is

`pkgs.testers.runNixOSTest` (also accessible as `pkgs.nixosTest`) is a function that takes a NixOS test module and returns a derivation. **Building that derivation runs the test.** The test result (pass/fail) is encoded as whether the derivation build succeeds or fails.

The test module accepts:
```nix
pkgs.testers.runNixOSTest {
  name = "my-test";

  # One or more QEMU VMs
  nodes = {
    server = { config, pkgs, ... }: {
      services.nginx.enable = true;
      networking.firewall.allowedTCPPorts = [ 80 ];
    };
    client = { config, pkgs, ... }: { /* ... */ };
  };

  # Python string (or function receiving nodes) — the test script
  testScript = ''
    start_all()
    server.wait_for_unit("nginx.service")
    client.succeed("curl http://server/")
  '';

  # Optional overrides
  defaults = { config, pkgs, ... }: { virtualisation.memorySize = 512; };
  extraPythonPackages = p: [ p.requests ];
  skipLint = false;          # disable Pyflakes + mypy lint
  skipTypeCheck = false;
  enableDebugHook = false;   # pause on failure, expose SSH backdoor
}
```

### QEMU and KVM Involvement

Each node in `nodes` becomes a QEMU virtual machine. The test driver starts these VMs via `qemu-kvm` and communicates with them through a serial console (virtioconsole). The VMs do **not** mount a disk image — they use the host Nix store directly (read-only bind mount into the VM), and overlay a writable union filesystem for store modifications if `virtualisation.writableStore = true`.

**KVM is required** for practical use. The framework marks itself as requiring the `kvm` system feature:
- On NixOS/Fedora: `/dev/kvm` is world-accessible (`crw-rw-rw-`), KVM works by default.
- On Ubuntu/Debian: `/dev/kvm` is restricted to the `kvm` group (`crw-rw----+`). Since Nix's build sandbox strips group memberships, the `nixbld` users cannot access `/dev/kvm` even if added to the group.
- **Without KVM:** QEMU falls back to TCG (software emulation). Tests still run but are "10-100x slower" — a test that takes 30s with KVM may take 5-10 minutes with TCG. The framework will emit "Could not access KVM kernel module: Permission denied" then "falling back to tcg". On CI hosts without KVM (many container-based CI systems), tests fail unless `system-features = kvm` is set in `nix.conf` or KVM is exposed to the container.

**Practical workaround on Ubuntu:** `sudo chmod o+rw /dev/kvm` or run Nix builds as a user in the `kvm` group with `--option sandbox false` (not recommended for production).

### Python Test Driver — Machine Methods

Each hostname in `nodes` becomes a Python variable. The test script is plain Python. Available methods on machine objects:

**VM lifecycle:**
```python
machine.start()           # Start VM asynchronously (does not wait for boot)
machine.shutdown()        # Graceful shutdown, waits for VM to exit
machine.crash()           # Simulate sudden power failure (QEMU process killed)
machine.block()           # Simulate unplugging network cable (VLAN isolation)
machine.unblock()         # Reconnect network cable
start_all()               # Start all VMs in parallel
```

**Command execution:**
```python
output = machine.succeed("cmd")        # Run cmd; raise exception if exit != 0; return stdout
machine.fail("cmd")                    # Run cmd; raise exception if exit == 0
(status, output) = machine.execute("cmd")  # Run cmd; return (exit_code, stdout) without asserting
machine.wait_until_succeeds("cmd")    # Retry every 1s until cmd exits 0
machine.wait_until_fails("cmd")       # Retry every 1s until cmd exits nonzero
```

**Wait/polling methods:**
```python
machine.wait_for_unit("nginx.service")          # Wait for systemd unit Active state
machine.wait_for_unit("ssh.service", "alice")   # Wait for user unit (second arg = username)
machine.wait_for_open_port(80)                  # Wait for TCP listener on localhost:80
machine.wait_for_closed_port(80)                # Wait for port to be unbound
machine.wait_for_file("/path/to/file")          # Wait for filesystem path to exist
machine.wait_for_x()                            # Wait for X11 display server
machine.wait_for_text("regex")                  # Wait for OCR-matched text on screen
machine.wait_for_window("regex")                # Wait for X11 window title match
```

**Systemd control:**
```python
machine.systemctl("start nginx")               # Run systemctl as root
machine.systemctl("--user start myservice", "alice")  # Run as user alice
machine.get_unit_info("nginx.service")         # Return dict of unit properties
machine.start_job("nginx.service")             # Start systemd unit
machine.stop_job("nginx.service")              # Stop systemd unit
```

**Screenshots and OCR:**
```python
machine.screenshot("name")        # Save PNG to $out/screenshot-name.png; linked in HTML log
machine.get_screen_text()         # OCR the current screen; returns string
                                  # Requires enableOCR = true in test config
```

**File transfer:**
```python
machine.copy_file_from_host("/host/path", "/vm/path")  # Copy file host -> VM
# Note: host path must be accessible during Nix build (in the sandbox)
```

**Input:**
```python
machine.send_keys("ctrl-alt-delete")   # Send key combo to VM keyboard
machine.send_chars("hello world\n")    # Type character sequence
machine.send_monitor_command("info kvm")  # Send command to QEMU monitor (rare)
```

**Shell interaction (interactive mode only):**
```python
machine.shell_interact()   # Drop to interactive shell inside VM; blocks test
```

### `@polling_condition` — Early Failure Detection

```python
@polling_condition
def nginx_running():
    "check that nginx is still running during the test"
    machine.succeed("pgrep -x nginx")

with nginx_running:
    # If nginx dies during this block, the polling condition fails immediately
    client.succeed("curl http://server/")
    client.succeed("curl http://server/large-file")
```

### `specialisation` in Tests

NixOS specialisations let a single VM boot into an alternate configuration during the test. From the nginx test:

```nix
nodes.webserver = { ... }: {
  services.nginx.enable = true;
  specialisation = {
    etagSystem.configuration = {
      services.nginx.virtualHosts.localhost.root = "/different/path";
    };
    justReloadSystem.configuration = {
      services.nginx.virtualHosts."extra".listen = [{ addr = "0.0.0.0"; port = 8080; }];
    };
    reloadRestartSystem.configuration = {
      services.nginx.package = pkgs.nginxMainline;
    };
  };
};
```

In the test script, switching specialisations:
```python
webserver.succeed(
  "/run/current-system/specialisation/justReloadSystem/bin/switch-to-configuration test"
)
webserver.wait_for_open_port(8080)
```

The test checks journal output to distinguish reloads from restarts:
```python
webserver.succeed("journalctl -u nginx | grep -q 'reloaded'")
```

### Using as `checks.<system>.my-test` in a Flake

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system}.extend (final: prev: {
          # Make your packages available to NixOS modules in the test
          myApp = self.packages.${system}.myApp;
        });
      in {
        checks.integration-test = pkgs.testers.runNixOSTest {
          name = "myapp-integration";
          nodes.server = { config, pkgs, ... }: {
            imports = [ self.nixosModules.myApp ];
            services.myApp.enable = true;
          };
          testScript = ''
            server.wait_for_unit("myapp.service")
            server.succeed("curl http://localhost:8080/health | grep -q 'ok'")
          '';
        };
      }
    );
}
```

Run with: `nix build .#checks.x86_64-linux.integration-test`
Or as part of: `nix flake check` (runs all checks)

### Interactive Mode for Debugging

Build the driver interactive target:
```bash
# Old-style
nix-build nixos/tests/mytest.nix -A driver
./result/bin/nixos-test-driver
# In REPL:
>>> start_all()
>>> server.wait_for_unit("default.target")
>>> server.succeed("whoami")
>>> test_script()   # run the full test script, then return to REPL
>>> server.shell_interact()  # drop into an interactive shell in the VM

# New-style with flakes
nix run '.#checks.x86_64-linux.integration-test.driverInteractive'
```

VM state persists in `/tmp/vm-state-<machinename>` between REPL sessions.

### Typical Test Runtimes

Based on observed measurements (KVM-enabled build host):
- Simple single-VM service start + HTTP check: **~30 seconds**
- After a configuration change rebuild: **~40 seconds total** (~10s VM rebuild, ~30s test run)
- BitTorrent multi-VM test: **~75 seconds**
- Chromium sandbox test: **~100 seconds**

Tests rebuild "very quickly — typically in seconds" because the framework mounts the host's Nix store directly into the VM rather than building a full disk image. Only configuration-changed packages need rebuilding.

**Without KVM:** Multiply by 10-100x. Tests that take 30s become 5-50 minutes. Some CI platforms (GitHub Actions, many Docker-based CI) do not expose `/dev/kvm` — this is a hard operational constraint.

### VM vs Container Tests

The test framework also supports `systemd-nspawn` containers as an alternative to QEMU VMs:

```nix
containers.mycontainer = { config, pkgs, ... }: { /* NixOS config */ };
```

Containers are **significantly faster to start** (share host kernel, no boot) but **cannot test kernel-level features**. They run in CI environments that block KVM. For most service integration tests, containers are the better choice. Containers do not support specialisations, X11 tests, or setuid binary testing.

---

## Topic 3: `nix-shell` Shebang Scripts

**Sources:** https://wiki.nixos.org/wiki/Nix-shell_shebang | https://nix.dev/tutorials/first-steps/reproducible-scripts.html | https://github.com/BrianHicks/nix-script

### The Classic `nix-shell` Shebang Pattern

The kernel only supports one `#!` line. `nix-shell` implements multi-line shebang by reading subsequent `#! nix-shell` lines from the script file before executing:

```python
#!/usr/bin/env nix-shell
#! nix-shell -i python3
#! nix-shell -p python3 python3Packages.requests python3Packages.beautifulsoup4
#! nix-shell -I nixpkgs=https://github.com/NixOS/nixpkgs/archive/abc123.tar.gz

import requests
from bs4 import BeautifulSoup
# ...
```

```bash
#!/usr/bin/env nix-shell
#! nix-shell -i bash --pure
#! nix-shell -p bash curl jq cacert
#! nix-shell -I nixpkgs=https://github.com/NixOS/nixpkgs/archive/abc123.tar.gz

curl -s "https://api.example.com/data" | jq '.results[]'
```

```ruby
#!/usr/bin/env nix-shell
#! nix-shell -i ruby
#! nix-shell -p ruby rubyPackages.faraday

require 'faraday'
conn = Faraday.new(url: 'http://example.com')
```

### `-i interpreter` Flag

`-i INTERPRETER` tells `nix-shell` which program to use to interpret the rest of the file after setting up the environment. The interpreter is looked up in the `PATH` of the constructed shell environment (the packages from `-p`). For Python scripts, `-i python3`; for Haskell, `-i runghc`; for Perl, `-i perl`.

The value is passed as `--interpreter` to nix-shell. Without `-i`, `nix-shell` drops to an interactive bash shell.

### `--pure` Flag

`--pure` unsets almost all environment variables except `HOME`, `USER`, `TERM`, `DISPLAY`, and a minimal `PATH`. This prevents the script from accidentally depending on system-installed tools. Recommended for reproducibility, required for scripts meant to work on any machine.

### `-I nixpkgs=...` for Version Pinning

Without `-I`, `nix-shell` uses the system `nixpkgs` channel — different users get different versions. Pinning to a specific commit hash guarantees reproducibility:

```bash
#! nix-shell -I nixpkgs=https://github.com/NixOS/nixpkgs/archive/2a601aafdc5605a5133a2ca506a34a3a73377247.tar.gz
```

### Caching Behavior

`nix-shell` builds and caches packages in the Nix store normally. The built packages are garbage-collected like any other Nix store path. There is no special cache for shebang scripts — each invocation re-evaluates the shebang lines and checks if packages are already in the store (cheap) or need building (expensive, first time only).

**The startup overhead** for cached packages is the `nix-shell` evaluation time (~0.5-2s for channel-based, ~0.1-0.5s for flake-based with eval cache). This is a known pain point.

**`cached-nix-shell`** (third-party) caches the resulting environment variables so the evaluation cost is paid only when inputs change, reducing startup to ~10ms.

### `--keep-env` / Environment Variables

`nix-shell` with `--pure` destroys almost all environment variables. To pass specific variables through:

```bash
#!/usr/bin/env nix-shell
#! nix-shell -i bash --pure --keep GITHUB_TOKEN --keep AWS_REGION
#! nix-shell -p awscli2 jq

aws s3 ls  # uses $AWS_REGION from host environment
```

For impure usage (not recommended but common), omit `--pure`:
```bash
#! nix-shell -i bash
#! nix-shell -p python3
# All host environment variables pass through
```

### The Flake-Native `nix shell` Shebang (Nix 2.19+)

As of Nix 2.19, the new `nix shell` command supports shebang usage. The `-S` flag to `env` splits arguments:

```bash
#!/usr/bin/env -S nix shell nixpkgs#bash nixpkgs#curl nixpkgs#jq --command bash

curl -s "https://api.example.com" | jq '.data'
```

This pulls packages from the flake registry (or `nixpkgs` flake directly), getting the version pinned in the registry. It does not support the multi-line `#!` syntax — all arguments must fit on one line.

**Known issues:** As of 2024, `nix shell` shebang has edge cases where it tries to use the script as a package installable rather than treating itself as the shebang interpreter. Tracked in NixOS/nix#10177.

### `nix-script` by BrianHicks

`nix-script` (https://github.com/BrianHicks/nix-script) extends the shebang pattern to **compiled languages** with transparent caching:

```haskell
#!/usr/bin/env nix-script
#!build ghc -O $SRC -o $OUT
#!buildInputs haskellPackages.text haskellPackages.aeson
#!runtimeInputs coreutils

import Data.Text (pack)
main = putStrLn (show (pack "hello"))
```

Key directives:
- `#!build`: Command that reads `$SRC` and writes binary to `$OUT`
- `#!buildInputs`: Build-time Nix expressions (packages available to compiler)
- `#!runtimeInputs`: Runtime Nix expressions (packages on `PATH` when script runs)
- `#!runtimeFiles`: Auxiliary files accessible via `$RUNTIME_FILES_ROOT`
- `#!interpreter`: Override which binary from `runtimeInputs` is used as interpreter (for interpreted languages)

The compilation result is cached in the Nix store. `NIX_PATH` is included in cache keys, so changing the nixpkgs version forces a rebuild. Variant `nix-script-haskell` adds `#!haskellPackages` and `#!ghcFlags` directives, plus `--ghcid` mode for development feedback loops.

`nix-script` provides a `--shell` mode that drops into a development environment without re-compiling:
```bash
nix-script --shell myscript.hs  # nix-shell with all build and runtime deps
```

### Inline Flake Shebang Pattern

For more complex dependency graphs, an inline flake in a comment block:
```bash
#!/usr/bin/env nix-shell
#! nix-shell -i bash
#! nix-shell --arg pkgs "import (fetchTarball \"https://github.com/NixOS/nixpkgs/archive/abc.tar.gz\") {}"
#! nix-shell -E "(import <nixpkgs> {}).mkShell { buildInputs = [ ... ]; }"
```

Or using the `-E` expression form for full control:
```bash
#!/usr/bin/env nix-shell
#! nix-shell -i python3 -E "with import <nixpkgs> {}; python3.withPackages (ps: [ps.requests ps.numpy])"
```

---

## Topic 4: `treefmt` and `nix fmt` Integration

**Sources:** https://github.com/numtide/treefmt-nix | https://flake.parts/options/treefmt-nix.html | https://nixos.asia/en/treefmt

### What `treefmt` Is

`treefmt` is a single-command formatter that runs all configured language formatters over a project in one invocation, in parallel. Instead of `gofmt ./... && prettier --write . && alejandra .`, you run `treefmt`. It provides:

1. **Unified invocation:** One command for all formatters.
2. **Parallel execution:** Formatters run concurrently.
3. **mtime-based caching:** Files not modified since last run are skipped.
4. **Single config file:** `treefmt.toml` at project root.

### `treefmt.toml` Configuration Format

```toml
# treefmt.toml
[formatter.alejandra]
command = "alejandra"
includes = ["*.nix"]

[formatter.gofmt]
command = "gofmt"
options = ["-w"]
includes = ["*.go"]

[formatter.prettier]
command = "prettier"
options = ["--write"]
includes = ["*.ts", "*.tsx", "*.js", "*.json", "*.css", "*.md"]

[formatter.shfmt]
command = "shfmt"
options = ["-w", "-i", "2"]
includes = ["*.sh"]
```

### Common Formatter Choices

| Language | Recommended Formatter | Nix Package |
|----------|-----------------------|-------------|
| Nix | `alejandra` (opinionated) | `pkgs.alejandra` |
| Nix (RFC-style) | `nixfmt-rfc-style` | `pkgs.nixfmt-rfc-style` |
| Nix (legacy) | `nixpkgs-fmt` | `pkgs.nixpkgs-fmt` |
| Go | `gofumpt` (strict superset of gofmt) | `pkgs.gofumpt` |
| Go | `goimports` | `pkgs.gotools` |
| TypeScript/JS | `prettier` | `pkgs.nodePackages.prettier` |
| TypeScript/JS | `biome` (faster, no Node needed) | `pkgs.biome` |
| Shell | `shfmt` | `pkgs.shfmt` |
| Python | `ruff-format` | `pkgs.ruff` |
| Terraform/OpenTofu | `terraform fmt` | `pkgs.terraform` |
| Markdown | `prettier` | — |
| YAML | `prettier` | — |

### `treefmt-nix` Flake Module

`treefmt-nix` wraps treefmt configuration into Nix, eliminating the need for a separate `treefmt.toml`. The flake generates the config file and wraps the treefmt binary.

**Standalone (without flake-parts):**
```nix
# treefmt.nix
{ pkgs, ... }:
{
  projectRootFile = "flake.nix";  # marker file identifying project root

  programs.alejandra.enable = true;
  programs.gofumpt.enable = true;
  programs.prettier.enable = true;
  programs.shfmt.enable = true;
  programs.shfmt.indent_size = 2;

  settings.formatter.prettier.excludes = [ "*.min.js" ];
}
```

```nix
# flake.nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    treefmt-nix.url = "github:numtide/treefmt-nix";
  };
  outputs = { self, nixpkgs, treefmt-nix }:
  let
    pkgs = nixpkgs.legacyPackages.x86_64-linux;
    treefmtEval = treefmt-nix.lib.evalModule pkgs ./treefmt.nix;
  in {
    # nix fmt → runs treefmt
    formatter.x86_64-linux = treefmtEval.config.build.wrapper;
    # nix flake check → validates formatting
    checks.x86_64-linux.treefmt = treefmtEval.config.build.check self;
  };
}
```

**With `flake-parts` (recommended for larger projects):**
```nix
# flake.nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    treefmt-nix.url = "github:numtide/treefmt-nix";
    flake-parts.url = "github:hercules-ci/flake-parts";
  };
  outputs = inputs: inputs.flake-parts.lib.mkFlake { inherit inputs; } {
    imports = [ inputs.treefmt-nix.flakeModule ];
    systems = [ "x86_64-linux" "aarch64-linux" "aarch64-darwin" ];
    perSystem = { config, pkgs, ... }: {
      treefmt = {
        projectRootFile = "flake.nix";
        programs.alejandra.enable = true;
        programs.gofumpt.enable = true;
        programs.prettier.enable = true;
        # flakeFormatter = true  (default) → registers as formatter.<system>
        # flakeCheck = true      (default) → registers as checks.<system>.treefmt
      };
    };
  };
}
```

### `flake-parts` Module Options

Key `perSystem.treefmt` options:

| Option | Type | Default | Purpose |
|--------|------|---------|---------|
| `projectRootFile` | string | `"flake.nix"` | Marker file at project root |
| `flakeFormatter` | bool | `true` | Register as `formatter.<system>` for `nix fmt` |
| `flakeCheck` | bool | `true` | Register as `checks.<system>.treefmt` for `nix flake check` |
| `package` | package | `pkgs.treefmt` | Override treefmt binary |
| `enableDefaultExcludes` | bool | `true` | Skip `.git`, `node_modules`, etc. |
| `programs.<name>.enable` | bool | `false` | Activate a formatter |
| `programs.<name>.package` | package | default | Override formatter package version |
| `programs.<name>.includes` | list | formatter default | File globs to format |
| `programs.<name>.excludes` | list | `[]` | File globs to skip |
| `programs.<name>.priority` | int or null | null | Execution order |
| `settings.formatter.<name>.*` | attrs | — | Raw treefmt formatter config |

Generated outputs:
- `config.treefmt.build.wrapper` — the treefmt package with embedded config
- `config.treefmt.build.configFile` — path to generated `treefmt.toml`
- `config.treefmt.build.devShell` — devShell with treefmt + all formatter binaries
- `config.treefmt.build.check` — function `projectRoot -> derivation` for CI

### `--fail-on-change` for CI

The check derivation (`treefmt.config.build.check self`) internally runs treefmt with `--fail-on-change`, which exits nonzero if any file was reformatted. This makes `nix flake check` fail if code is not properly formatted — the check does not commit the changes, it just signals a failure.

```bash
# CI usage:
nix flake check  # fails if any file needs reformatting

# Local fix:
nix fmt          # reformats all files in-place
```

### mtime Caching — Interaction with Nix Builds

**Non-obvious critical detail:** `treefmt` uses file `mtime` to determine what has changed since the last run. It stores the last-run timestamp in a state file (`.treefmt.toml.state` or similar in the project root).

**The problem with Nix:** Nix normalises all file timestamps to `SOURCE_DATE_EPOCH` (January 1, 1970) for reproducibility. When `nix build` produces output files or when you `nix copy` a store path, all file timestamps are epoch zero. If your formatter runs inside a Nix derivation (as the `build.check` derivation does), all input files appear to have mtime = epoch, and treefmt sees every file as "possibly changed since last run = epoch" — it reformats everything, every time.

**This is why the `checks.treefmt` derivation works correctly:** It intentionally runs treefmt with `--fail-on-change` over all files (no mtime optimization), treating it as a full scan. The mtime caching only benefits **local interactive use** (`nix fmt` in a working directory), where files have real mtimes from git checkout or editing.

In a `nix develop` environment, `nix fmt` in your worktree uses real mtimes and caching works as expected. The CI check always does a full scan — this is intentional.

### Adding `treefmt` to a `devShell`

```nix
perSystem = { config, pkgs, ... }: {
  treefmt.programs.alejandra.enable = true;

  devShells.default = pkgs.mkShell {
    inputsFrom = [ config.treefmt.build.devShell ];
    # devShell includes treefmt binary + all enabled formatter binaries
    packages = [ pkgs.go pkgs.nodejs ];
  };
};
```

---

## Topic 5: Nix Store Security and Integrity

**Sources:** https://nix.dev/manual/nix/2.33/command-ref/new-cli/nix3-store-verify.html | https://github.com/NixOS/nix/security/advisories/GHSA-q82p-44mg-mgh5 | https://discourse.nixos.org/t/security-fix-nix-derivation-sandbox-escape/47778 | https://nix.dev/manual/nix/2.33/command-ref/conf-file.html | https://diogotc.com/blog/kalmarctf-writeup-reproducible-pwning/

### `nix store verify`

Verifies store path integrity. Checks two properties per path:

1. **Content integrity:** Recomputes the NAR hash of each path and compares against the value recorded in the Nix SQLite database at build time.
2. **Trust status:** A path is trusted if it has at least one trusted signing key signature, is content-addressed (hash-addressed, self-verifying), or was built locally ("ultimately trusted").

```bash
# Verify all store paths
nix store verify --all

# Verify specific path and its full closure
nix store verify --recursive /nix/store/abc123-mypackage

# Verify content only (skip trust check)
nix store verify --all --no-trust

# Require at least 2 signatures (for paranoid binary cache setups)
nix store verify --all --sigs-needed 2

# Skip content check (fast — only verifies trust/signatures)
nix store verify --all --no-contents
```

Exit codes are additive:
- `1` — corrupted paths (hash mismatch)
- `2` — untrusted paths (no valid signature)
- `4` — other failures (I/O errors)

### `nix store repair`

Rebuilds or re-downloads corrupted store paths:

```bash
nix store repair /nix/store/abc123-mypackage
```

**Privilege requirement:** Repair requires root or the `nix-daemon` connection. Regular users get "you are not privileged to repair paths". This is intentional — store repair modifies the Nix store, which is root-owned in multi-user installs.

For large stores, `nix-store --verify --check-contents` (old CLI) "is obviously quite slow" — it reads every byte of every file in the store to recompute SHA-256 hashes.

### `nix store dump-path`

Serialises a store path to NAR (Nix ARchive) format on stdout:

```bash
nix store dump-path /nix/store/abc123-hello > hello.nar
# Equivalent to:
nix-store --dump /nix/store/abc123-hello > hello.nar
```

NAR format is a deterministic, ordered archive format (similar to tar but without timestamps, permissions normalised). It is the canonical serialisation for store paths — the NAR hash is what gets stored in the database and what signatures cover.

### `nix store add-file` / `nix store add-path`

```bash
# Add a single file to the store
nix store add-file ./myfile.txt
# Output: /nix/store/abc123-myfile.txt

# Add a directory to the store
nix store add-path ./mydir/
# Output: /nix/store/xyz789-mydir
```

These create **content-addressed** store paths with no associated derivation. Useful for injecting files into the store for use by other derivations or for testing.

### Security Properties of the Nix Store

**Immutability:** Store paths under `/nix/store/` are owned by root with permissions `r-xr-xr-x` (directories) and `r--r--r--` (files). Regular users can read but not write. Once a path is built, its contents are fixed. However, root can always modify the store — immutability is enforced at the OS permission level, not cryptographically.

**World-readable:** All store paths are readable by all users on the system. This is intentional (binary cache sharing, user isolation without copy-on-write). It means secrets must never be placed in the Nix store — a build output containing a private key is readable by all local users.

**Content-addressed derivations (CA derivations):** An experimental feature where the output hash is computed from the actual build output content rather than the input derivation hash. CA derivations are self-verifying — if two different derivation inputs produce the same output, the output hash is the same, enabling deduplication. Regular (input-addressed) derivations hash the inputs, not the outputs.

**Multi-user trust model:** In multi-user Nix installations, `nix-daemon` runs as root and performs builds. Paths built locally via the daemon are "ultimately trusted." Paths downloaded from binary caches must be signed by a trusted key configured in `trusted-substituters` and `trusted-public-keys`. Regular users cannot build directly — they submit build requests to the daemon.

### `nix.conf` Sandbox Security Options

```
# sandbox = true (default on Linux, false elsewhere)
# Builds run in a Linux namespace sandbox, isolated from the rest of the filesystem.
# Only Nix store paths (dependencies), the build directory, /proc, /dev, /dev/shm are visible.
sandbox = true

# sandbox-paths — extra paths bind-mounted read-only into the sandbox
# Format: /target=source or just /path (mapped to same location)
# Optional suffix ? means: skip silently if source doesn't exist
sandbox-paths = /bin/sh=/nix/store/.../bash-interactive/bin/bash

# sandbox-fallback — if kernel doesn't support sandboxing, fall back to unsandboxed
# CRITICAL SECURITY SETTING: set to false to refuse builds on systems without sandboxing
# Default: true (fall back silently)
sandbox-fallback = false

# filter-syscalls — block dangerous syscalls in sandbox (mknod, setuid, setxattr, etc.)
# Default: true
# Disabling this is required for the CVE-2024-38531 attack to work
filter-syscalls = true

# build-dir — override where build directories are created
# Pre-CVE-2024-38531 fix: setting this to a root-only directory is the workaround
# build-dir = /root/nix-builds  # root-only, prevents world-accessible build dirs
```

**`sandbox-fallback = false`** is a critical security setting for production systems. Without it, if a build host is booted with a kernel that lacks namespace support (e.g., a container environment with restricted capabilities), Nix silently builds without any sandboxing. All build inputs become accessible to builds that declare no dependency on them, breaking reproducibility and potentially leaking secrets. Setting `sandbox-fallback = false` causes such builds to fail explicitly instead of succeeding insecurely.

### CVE-2024-38531 — Nix Sandbox Escape (Build Dir Privilege Escalation)

**CVE:** CVE-2024-38531 (GHSA-q82p-44mg-mgh5, reported by lheckemann and alois31)
**Severity:** CVSS 3.6 / Low (local-only, requires specific conditions)
**Affected:** Nix ≤ 2.23; patched in 2.23.1, 2.22.2, 2.21.3, 2.20.7, 2.19.5, 2.18.4

**Attack chain:**

1. The build process (running as `nixbld` user) executes inside the sandbox, with its working directory in a world-accessible location (default: `/tmp`).
2. The build can modify the **permissions** of its build directory — `fchmod()` is not blocked by `filter-syscalls`.
3. A malicious derivation creates a **setuid binary** (`chmod 4755 binary`) inside its build directory and places it in a world-accessible location.
4. A **local attacker** (different user on the same machine, not necessarily trusted by Nix) runs this setuid binary.
5. The setuid binary runs with the UID of the **`nixbld` worker** that built it (or the Nix daemon if running as root).
6. With daemon-level permissions, the attacker can "hijack all future builds" — modify build inputs, replace binaries being built, inject malicious outputs.

**Required conditions (all must be true):**
- Attacker has local system access (not remote)
- `filter-syscalls = false` OR sandbox is disabled (disabling seccomp allows setuid creation)
- A concurrent build is happening (to exploit the hijacked daemon worker)

**The fix (NixOS/nix PR):** The build process now executes in a subdirectory that is owned by and accessible only to the Nix daemon, not in a world-accessible directory. A malicious build can no longer create files in a location accessible to other local users.

**Workaround (pre-patch):** Set `build-dir` to a root-only path:
```
build-dir = /var/lib/nix/builds  # chmod 700, owned by root
```

### CVE-2024-51481 — macOS Nix Sandbox Escape

**CVE:** CVE-2024-51481 (macOS-specific)
**Affected:** Nix ≤ 2.23 on macOS; patched in 2.18.9, 2.19.7, 2.20.9, 2.21.5, 2.22.4, 2.23.4, 2.24.10

On macOS, Nix uses the macOS sandbox (`sandbox-exec`) rather than Linux namespaces. Built-in builders (the special builders like `fetchurl` that run inside Nix itself rather than as external processes) were **not executed within the macOS sandbox** — they had unrestricted read access to all world-readable paths and write access to world-writable paths on the system. This allowed built-in builder derivations to read sensitive files from the host filesystem.

### KalmarCTF 2024: `diff-hook` and `extra-sandbox-paths` Abuse

A 2024 CTF challenge demonstrated a more sophisticated attack vector using intentionally misconfigured Nix. Key attack vectors:

**Via `extra-sandbox-paths`:** If the Nix daemon socket is exposed inside the build sandbox via `extra-sandbox-paths = /tmp/daemon=/nix/var/nix/daemon-socket/socket`, builds can connect to the daemon. If `build-users-group = ""` (daemon runs builds as root), connecting to the daemon grants trusted-user privileges. A build can then pass `--option sandbox-paths /secret-data` to a nested `nix-build` call, exposing host filesystem paths inside an inner build's sandbox.

**Via `diff-hook`:** The `diff-hook` option runs a script when a non-deterministic build produces different outputs on two runs. This hook runs **outside the sandbox** as the build user. By creating an intentionally non-deterministic derivation (using `/dev/urandom`), the hook fires and executes arbitrary code with full host filesystem access.

**Store modification via trusted access:** A trusted user with `nix-daemon` socket access can directly `nix-store --add` arbitrary content or modify store paths, overwriting system binaries (e.g., replacing a setuid `su` binary). This demonstrates that Nix store "immutability" is only as strong as the daemon access controls.

**Lesson:** `extra-sandbox-paths` exposing the daemon socket, `build-users-group = ""`, and `diff-hook` are inherently dangerous combinations. The Nix security model assumes trusted users are trusted — if untrusted code gains daemon access, the model breaks.

### Store Security Summary

| Property | What Nix Provides | Caveat |
|----------|-------------------|--------|
| Immutability | Files are `r--r--r--`, dirs `r-xr-xr-x` | Root can always modify; daemon trusted users can too |
| Content integrity | SHA-256 NAR hash in SQLite database | Verified only by `nix store verify --check-contents` (not automatic) |
| World-readable | All store paths readable by all local users | Secrets placed in store are exposed to all users |
| Reproducibility | Source timestamps stripped, `SOURCE_DATE_EPOCH` applied | Only if builds are actually deterministic |
| Trust | Signing keys for binary cache paths | Trusted users can bypass; `--no-trust` skips verification |
| Sandbox | Linux namespaces + seccomp filter | Requires KVM-capable kernel; `sandbox-fallback = true` by default silently degrades |
| Build isolation | Each build gets own `/tmp`-based dir | Pre-CVE-2024-38531: dir was world-accessible |
