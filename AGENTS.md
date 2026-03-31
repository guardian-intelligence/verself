# forge-metal

Repo is for a turnkey "company in a box": fully self-hosted bare-metal platform with Forgejo, Fast CI via ZFS deep optimizations, ClickStack observability. This is free open-source software, not a PaaS.

Performance-first CI on bare metal. ZFS golden image clones (~28ms), gVisor sandboxing, ClickHouse wide events, and HyperDX for real-time observability. Designed for 1000+ globally distributed nodes.

The goal is for turnkey bootstrap from 0 -> bare metal instance -> forgejo + click stack + 2 deployed frontend apps reading/writing off the same DB. 

Hard requirement: everything must be self-hosted.

Exceptions:

Optional - Backblaze B2, Cloudflare R2, AWS S3 for backups (done through `zfs send`, not LINSTOR + DRBD)
Required - Domain Registar (Cloudflare only for now)
Required - Compute Provider (Latitude.sh only for now)
Required (not implemented) - Email Delivery (Resend only in the future)

## CI Architecture (Target)

Each CI job runs in a Firecracker microVM whose root disk is a ZFS zvol clone of a golden image. The host orchestrator manages ZFS at the kernel level; guests have no awareness of ZFS.

```
Host (CI Orchestrator — bare metal, root, ZFS kernel module)
│
├── ZFS Pool (NVMe-backed)
│   ├── golden-zvol@ready                 ← golden image: ext4 inside zvol, warm caches, node_modules
│   ├── ci/job-abc                        ← zvol clone (~1.7ms COW, metadata-only)
│   ├── ci/job-def                        ← zvol clone
│   └── ci/job-ghi                        ← zvol clone
│
├── Firecracker VM (job-abc)
│   └── /dev/vda ← /dev/zvol/pool/ci/job-abc
│       └── ext4 (from golden image, COW diverges on write)
│
├── Firecracker VM (job-def)
│   └── /dev/vda ← /dev/zvol/pool/ci/job-def
│
└── Firecracker VM (job-ghi)
    └── /dev/vda ← /dev/zvol/pool/ci/job-ghi
```

### Why this layering

| Layer | Provides | Latency |
|-------|----------|---------|
| ZFS zvol clone | Instant COW rootfs from golden image | ~1.7ms kernel, ~5.7ms end-to-end |
| Firecracker microVM | Process/memory/kernel isolation, deterministic execution | ~125ms from snapshot, ~3s cold boot |
| gVisor (inside VM) | Syscall-level sandboxing for untrusted build scripts | Negligible on top of Firecracker |

### Key distinctions

- **zvol, not dataset.** Firecracker takes block devices (`/dev/zvol/...`), not mounted filesystems. A zvol is a ZFS block device — clone/destroy/written all work identically to datasets.
- **Golden image is a zvol with ext4 inside.** Built by the orchestrator: `zfs create -V <size> pool/golden-zvol`, `mkfs.ext4`, mount, populate (Nix closure, node_modules, warm caches), unmount, `zfs snapshot pool/golden-zvol@ready`.
- **Guest is unaware of ZFS.** It sees `/dev/vda` with ext4. No ZFS tooling needed in the VM image.
- **Orchestrator owns all ZFS operations.** Allocate (clone), monitor (`written` bytes), teardown (destroy). Maps directly to the `Harness.Allocate` → `Clone.Release` lifecycle in `internal/zfsharness/`.

### Orchestrator flow per job

```
1. zfs clone pool/golden-zvol@ready pool/ci/job-abc         # ~1.7ms
2. firecracker --drive path=/dev/zvol/pool/ci/job-abc        # boot VM
3. (job runs inside VM: git clone, npm install, npm test)
4. VM exits
5. zfs get written pool/ci/job-abc                           # bytes dirtied → ClickHouse wide event
6. zfs destroy pool/ci/job-abc                               # cleanup
```

### What this does NOT use

- **CRIU** — process checkpointing is fragile with Node.js/V8 (timer FDs, JIT pages, epoll). Not worth the complexity when ZFS clone already eliminates the expensive part (warm caches, pre-installed deps). Process startup of `node` is ~50ms.
- **libzfs** — shells out to `zfs` CLI like every production ZFS project (OpenZFS, Incus, DBLab, OBuilder). See `internal/zfsharness/doc.go` for rationale.
- **Nested ZFS in guest** — the guest runs ext4 on a raw block device. ZFS stays on the host where it belongs.

## ZFS

This project makes heavy use of ZFS. We have compiled some research in docs/research. @docs/research/README.md


## Quick Start

### 1. Clone and enter dev shell

```bash
nix develop  # gives you Go, Terraform, Ansible, protoc, clickhouse-client, etc.
```

### 2. Provision bare metal

```bash
# Set your Latitude.sh API token
export LATITUDESH_AUTH_TOKEN="your-token-here"

# Create your tfvars (one-time)
cp terraform/terraform.tfvars.example.json terraform/terraform.tfvars.json
# Edit terraform/terraform.tfvars.json — set project_id to your Latitude.sh project

# Provision server + generate Ansible inventory
make provision
```

This provisions a bare metal server via OpenTofu and auto-generates `ansible/inventory/hosts.ini` from the outputs.

### 3. Deploy

```bash
make deploy  # idempotent, no wipe — this is the normal workflow
```

This builds the Nix server profile, pushes it over SSH, and configures services via Ansible. Safe to run repeatedly.

> **`make e2e` — destructive, rarely needed.** This wipes ALL data (ClickHouse, MongoDB, Forgejo repos, credentials) and reprovisions from scratch. Only run this when bootstrapping a brand new server or when you need to verify the full zero-to-healthy pipeline. Once your Forgejo has repos, runners registered, and CI history, `make e2e` destroys all of that. Use `make deploy` for normal iteration.

### 4. Log in

```bash
# Admin credentials (generated on first deploy)
ssh ubuntu@<ip> 'sudo cat /etc/clickstack/admin-credentials.txt'
```

Open `https://<ip>` in your browser (self-signed cert for IP addresses, auto Let's Encrypt for domains).

### TLS with a real domain (Cloudflare)

```bash
make setup-domain DOMAIN=anveio.com
make deploy
```

`setup-domain` walks you through everything: initializes secrets if needed, guides you through creating a Cloudflare API token, validates it, and writes the config. Then `deploy` creates DNS records and provisions TLS.

Services get subdomains automatically:

| Subdomain | Service |
|-----------|---------|
| `admin.<domain>` | ClickStack dashboard |
| `git.<domain>` | Forgejo (when enabled) |

### Deprovision

```bash
make deprovision  # destroys server + SSH key via OpenTofu, removes inventory
```

This runs `tofu destroy` and cleans up the generated Ansible inventory. DNS records (if any) must be removed separately via the Cloudflare dashboard or API.

## How It Works

### Nix Golden Image

All server software is packaged in a single Nix closure (`flake.nix` -> `server-profile`). This means:

- **Reproducible**: `flake.lock` pins every dependency transitively. Same lock = same binaries, always.
- **Fast deploys**: The closure is pushed once over SSH. No apt repos, no GitHub downloads, no `yarn install`.
- **Atomic updates**: `nix flake update` + `make deploy` upgrades everything. Rollback = `git revert flake.lock`.
- **No apt, no get_url, no build-from-source at deploy time** (except HyperDX, which builds from source using Nix-provided Node.js).

The only `apt install` that remains is `zfsutils-linux` (kernel-dependent, must match running kernel).

### Version Updates

```bash
nix flake update           # update all packages
make deploy --limit canary # deploy to one node, verify services come up
make deploy                # roll out fleet-wide
```

### Architecture

```
nix build .#server-profile --> content-addressed closure (~2GB)
nix copy --to ssh://host   --> push closure to bare metal
Ansible                    --> configure + enable services
```

| Component | Port | Purpose |
|-----------|------|---------|
| Caddy | 443, 80 | Reverse proxy with automatic TLS |
| Forgejo | 3000 | Git server + CI runner |
| Verdaccio | 4873 | Sealed npm registry mirror |
| ClickHouse | 9000, 8123 | Wide event storage with optimized codecs |
| HyperDX | 8080, 8000 | Observability UI + API |
| OTel Collector | 4317, 4318 | OTLP telemetry ingestion |
| MongoDB | 27017 | HyperDX app state |

### Wide Events

Every CI job produces one denormalized row in `ci_events` with ~50 columns. No JOINs needed.

Compression codecs per column type:
- Timestamps: `DoubleDelta + ZSTD(3)`
- Durations (Int64): `Delta(8) + ZSTD(3)`
- Byte counters: `T64 + ZSTD(3)`
- Low-cardinality strings: `LowCardinality + ZSTD(3)`
- Floats: `Gorilla + ZSTD(3)`

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make provision` | Provision bare metal via OpenTofu, generate Ansible inventory |
| `make deprovision` | Destroy all bare metal infrastructure |
| `make setup-domain` | Configure Cloudflare domain (interactive wizard) |
| `make server-profile` | Build Nix server profile (golden image closure) |
| `make deploy` | Deploy to all nodes (idempotent, no wipe) — **use this normally** |
| `make e2e` | **DESTRUCTIVE** full wipe + reprovision + test — rarely needed |
| `make build` | Build bmci Go binary locally |
| `make test` | Run Go tests |

## Project Structure

```
forge-metal/
├── cmd/bmci/              # CLI entry point (doctor, setup-domain, benchmark)
├── internal/
│   ├── clickhouse/        # ClickHouse client, wide event struct
│   ├── cloudflare/        # Cloudflare API client (DNS, zone lookup)
│   ├── config/            # Layered TOML config
│   ├── doctor/            # Dev environment health checks
│   ├── domain/            # Domain setup wizard
│   ├── prompt/            # Shared Prompter interface + TTY implementation
│   ├── benchmark/         # Pressure-test runner: weighted workloads, cgroup metrics, SIGHUP reload
│   └── zfsharness/        # ZFS golden image clone allocation + recovery
├── ansible/
│   ├── playbooks/         # ci-e2e, dev-single-node, site, golden-refresh, security-patch
│   └── roles/
│       ├── nix_deploy/    # Install Nix + push server profile closure
│       ├── base/          # System config (ZFS, users, npm registry)
│       ├── cloudflare_dns/ # Cloudflare DNS A record management
│       ├── clickstack/    # ClickHouse, HyperDX, OTel, Caddy, MongoDB (config only)
│       ├── zfs/           # Pool creation, golden/ci datasets
│       ├── containerd/    # containerd + gVisor runsc (config only)
│       ├── verdaccio/     # Sealed npm registry mirror (config only)
│       ├── wireguard/     # Mesh networking (config only)
│       ├── golden_image/  # ZFS snapshot with warm caches
│       └── forgejo/       # Git server + CI runner (config only)
├── terraform/             # Latitude.sh provisioning
├── migrations/            # ClickHouse schema (MergeTree + Replicated)
├── scripts/               # Security scripts, setup helpers
├── config/default.toml    # Embedded defaults
└── flake.nix              # Dev shell + server profile (golden image)
```

## Assistant Contract

* When proposing solutions, think from the perspective of the user of the system. The user is a sole operator of a single-person software company.
* When beginning an ambiguous task, collect information about t

## Tool Use Contract

* When executing long-running tasks, execute them in the background and check in every 30 - 60 seconds.

## Output Contract

* 
* 

## License

MIT
