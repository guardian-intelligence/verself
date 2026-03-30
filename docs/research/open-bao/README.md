# OpenBao Research

Research notes on OpenBao (open-source Vault fork, MPL 2.0) for self-hosted secrets management
in forge-metal. Conducted 2026-03-29 as background research for integrating a turnkey key
management system on Latitude.sh bare metal.

## Goal

Replace flat-file credential storage (`/etc/clickstack/admin-credentials.txt`, mode 0600) and
SOPS-encrypted Ansible secrets with a proper secrets management server. The human operator is
the only entity with access to Forgejo admin credentials.

## Documents

| Document | Focus |
|----------|-------|
| [Source Architecture](source-architecture.md) | Repo layout, Core struct, server startup |
| [Static Key Seal](static-seal.md) | OpenBao-only auto-unseal via AES-256-GCM key file |
| [Raft Storage](raft-storage.md) | Integrated storage backend, on-disk format, snapshots |
| [Nix Integration](nix-integration.md) | nixpkgs package, NixOS module, systemd hardening |
| [CI Integration](ci-integration.md) | AppRole auth, Forgejo Actions, agent pattern for Firecracker |
| [Comparisons](comparisons.md) | OpenBao vs Vault vs SOPS vs Infisical |
| [Security Model](security-model.md) | Threat model for single-node static-seal deployment |
| [Production Deployments](production-deployments.md) | GitLab, ControlPlane, community guides |

## Cross-cutting findings

### Everyone shells out to `bao` CLI

Same pattern as ZFS ecosystem research (see `../README.md`). OpenBao's own Go client library
(`api/v2`) wraps HTTP, not the CLI, but every deployment tool (Ansible, Terraform, NixOS module)
shells out to the `bao` binary. The CLI is the stable interface.

### Static Key seal is the key enabler for self-hosted

HashiCorp Vault requires cloud KMS (AWS, GCP, Azure) or a physical HSM for auto-unseal.
OpenBao's static key seal (`seal "static"`) reads a 32-byte AES key from a local file and
auto-unseals on every reboot. No cloud dependency. This is the feature that makes self-hosted
OpenBao viable without manual intervention on every restart.

Source: `wrappers/static/static.go` in https://github.com/openbao/go-kms-wrapping

### Nix package is current and first-class

`openbao` v2.5.2 is in nixpkgs with a full NixOS module including systemd hardening
(`MemorySwapMax=0`, `LimitCORE=0`, `DynamicUser=true`). The module sets `restartIfChanged = false`
to prevent `nixos-rebuild switch` from sealing the instance. This slots directly into
forge-metal's Nix-based deployment model.

### SOPS and OpenBao are complementary, not competing

SOPS bootstraps OpenBao (unseal key, initial root token). OpenBao then becomes the runtime
secrets source. The SOPS-encrypted `secrets.sops.yml` shrinks to just the unseal key and
bootstrap token. Everything else moves into OpenBao's KV v2 engine.

### Forgejo lacks native external secrets support

Forgejo does not have a Vault/OpenBao integration for Actions (open issue: codeberg.org/forgejo/forgejo/issues/6038).
The workaround is OpenBao Agent in process supervisor mode on the runner, or in-workflow API calls.
GitLab's JWT-based integration is the gold standard but requires Forgejo to implement OIDC
token issuance for CI jobs.

## Applicability to forge-metal

| Pattern | Source | Applicability |
|---------|--------|---------------|
| Static key auto-unseal | OpenBao (unique) | Eliminates manual unseal on reboot. Deploy key via Ansible to `/etc/openbao/unseal.key` |
| Raft integrated storage | OpenBao | Single binary, no external DB. Data dir on ZFS for snapshot backup |
| AppRole + response wrapping | OpenBao | Per-Firecracker-VM scoped tokens. Orchestrator generates wrapped SecretID, injects into VM |
| NixOS module | nixpkgs | Add to `server-profile`, configure via Ansible role (thin config template) |
| SOPS for bootstrap | Current stack | Keep SOPS for unseal key + root token only. Migrate all other secrets to OpenBao |
| PKI engine | OpenBao | Defer until multi-node. Single-node uses localhost/unix sockets |
