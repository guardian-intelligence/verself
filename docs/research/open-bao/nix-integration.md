# Nix Integration

OpenBao has first-class support in nixpkgs: a package, a NixOS module, and integration tests.

## Package

File: `pkgs/by-name/op/openbao/package.nix` in nixpkgs
Version: 2.5.2 (current as of 2026-03-29)

Build details:
- Built with `buildGoModule`, `proxyVendor = true`
- `subPackages = [ "." ]` -- builds only the main binary
- Binary renamed from `openbao` to `bao` in `postInstall`
- Bash shell completion installed

Build flags:
- `withHsm` (default: true on Linux) -- enables PKCS#11 HSM support
- `withUi` (default: true) -- bundles the web UI (built as separate derivation via `passthru.ui`)

ldflags set version, git commit, and build date (pinned to epoch for reproducibility).

License: MPL-2.0
Maintainers: `brianmay`, `emilylange`

Source: https://github.com/NixOS/nixpkgs/blob/master/pkgs/by-name/op/openbao/package.nix

## NixOS Module

File: `nixos/modules/services/security/openbao.nix` in nixpkgs

### Configuration options

| Option | Type | Description |
|--------|------|-------------|
| `services.openbao.enable` | bool | Enable the OpenBao daemon |
| `services.openbao.package` | package | Override package (e.g., `pkgs.openbao.override { withUi = false; }`) |
| `services.openbao.settings` | freeform attrs | Direct mapping to OpenBao config as Nix attrsets |
| `services.openbao.settings.ui` | bool | Enable web UI |
| `services.openbao.settings.listener` | attrsOf submodule | Listener configs (type: `"tcp"` or `"unix"`) |
| `services.openbao.extraArgs` | listOf str | Additional CLI arguments to `bao server` |

### systemd hardening (notable)

The NixOS module applies aggressive hardening to the systemd unit:

| Setting | Value | Why |
|---------|-------|-----|
| `DynamicUser` | true | No static user needed, auto-created |
| `MemorySwapMax` | 0 | Prevents secrets from hitting swap |
| `MemoryZSwapMax` | 0 | Same for compressed swap |
| `LimitCORE` | 0 | No core dumps (would contain secrets) |
| `PrivateUsers` | true | User namespace isolation |
| `ProtectHome` | true | Cannot read user home directories |
| `ProtectKernelModules` | true | Cannot load kernel modules |
| `RestrictNamespaces` | true | Cannot create new namespaces |
| `SystemCallFilter` | `@system-service @resources ~@privileged` | Allowlist syscalls |
| `UMask` | 0077 | Files created are owner-only |

**Critical:** `restartIfChanged = false` -- prevents `nixos-rebuild switch` from restarting
OpenBao, which would seal the instance and disrupt all clients. Restarts must be explicit.

### Config format

Settings are serialized to JSON via `settingsFormat.generate "openbao.hcl.json"`. OpenBao
accepts JSON config files (HCL is a superset of JSON).

### Example NixOS config

```nix
services.openbao = {
    enable = true;
    settings = {
        ui = true;
        listener.default = {
            type = "tcp";
            address = "127.0.0.1:8200";
            tls_disable = true;  # Caddy handles TLS
        };
        storage.raft.path = "/var/lib/openbao";
        api_addr = "https://secrets.example.com";
    };
};
```

### NixOS test

`nixos/tests/openbao.nix` validates: service startup, web UI serving, initialization,
unsealing with Shamir keys, root token login, userpass auth, and cubbyhole secret operations.

Source: https://github.com/NixOS/nixpkgs/blob/master/nixos/tests/openbao.nix

## Applicability to forge-metal

The integration path mirrors the existing service model:

1. Add `pkgs.openbao` to `server-profile` in `flake.nix`
2. Create `ansible/roles/openbao/` with thin config template + systemd enablement
3. The NixOS module provides the systemd unit with hardening already applied
4. Config is a Nix attrset -> JSON, deployed by Ansible template

The `restartIfChanged = false` behavior is important -- it means `make deploy` won't
accidentally seal OpenBao. The operator must explicitly restart the service when needed
(e.g., after a seal key rotation or version upgrade).

Note: forge-metal doesn't use NixOS (it uses Ubuntu with Nix as a package manager), so the
NixOS module is a reference implementation rather than something to use directly. The Ansible
role should replicate the systemd hardening settings.
