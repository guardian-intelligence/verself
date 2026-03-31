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
| `make guest-rootfs` | Build Alpine guest rootfs on the server |
| `make deploy-ci-artifacts` | Deploy rootfs to /var/lib/ci/ on the server |

## Project Structure

```
forge-metal/
├── cmd/bmci/              # CLI entry point (doctor, setup-domain, benchmark)
├── cmd/forgevm-init/      # PID 1 inside Firecracker VMs (mounts, network, fork+exec)
├── internal/
│   ├── clickhouse/        # ClickHouse client, wide event struct
│   ├── cloudflare/        # Cloudflare API client (DNS, zone lookup)
│   ├── config/            # Layered TOML config
│   ├── doctor/            # Dev environment health checks
│   ├── domain/            # Domain setup wizard
│   ├── prompt/            # Shared Prompter interface + TTY implementation
│   ├── firecracker/       # Firecracker orchestrator (Go reference impl, used by bmci)
│   └── zfsharness/        # ZFS golden image clone allocation + recovery
├── ansible/
│   ├── playbooks/         # ci-e2e, dev-single-node, site, golden-refresh, security-patch
│   └── roles/
│       ├── nix_deploy/    # Install Nix + push server profile closure
│       ├── base/          # System config (ZFS, users, npm registry, sudoers)
│       ├── cloudflare_dns/ # Cloudflare DNS A record management
│       ├── clickstack/    # ClickHouse, HyperDX, OTel, Caddy, MongoDB (config only)
│       ├── zfs/           # Pool creation, golden/ci datasets
│       ├── firecracker/   # KVM, jailer user, golden zvol, CI dataset
│       ├── fc_ci_seed/    # Forgejo CI seeding: golden image bake, action repo, bench repo
│       ├── containerd/    # containerd + gVisor runsc (config only)
│       ├── verdaccio/     # Sealed npm registry mirror (config only)
│       ├── wireguard/     # Mesh networking (config only)
│       ├── golden_image/  # ZFS snapshot with warm caches (dataset-based, legacy)
│       ├── benchmark_seed/ # Seed benchmark repo in Forgejo
│       └── forgejo/       # Git server + CI runner (config only)
├── scripts/
│   ├── forge-vm-run.sh    # Shell-based Firecracker VM lifecycle (replaces Go orchestrator for CI)
│   └── build-guest-rootfs.sh # Alpine rootfs builder (Layer 1 of golden image)
├── ci/
│   ├── versions.json      # Pinned Alpine + Firecracker versions with SHA256
│   └── seed.sql           # PostgreSQL schema + data for next-learn demo app
├── terraform/             # Latitude.sh provisioning
├── migrations/            # ClickHouse schema (MergeTree + Replicated)
├── config/default.toml    # Embedded defaults
└── flake.nix              # Dev shell + server profile (Nix removed from VM builds)
```

## Firecracker CI: Tracer Bullet Status

Full CI proven: `tsc --noEmit && next build` runs in 22.3s inside a Firecracker VM with pre-seeded Postgres.

### Two-layer golden image

```
Layer 1 (scripts/build-guest-rootfs.sh):
  Alpine 3.21 + Node 22 + PostgreSQL 17 + git + forgevm-init + /ci-start.sh
  → ci/output/rootfs.ext4

Layer 2 (ansible/roles/fc_ci_seed/tasks/golden_image.yml):
  Mount zvol, copy app, npm install, patch SSL, seed DB, unmount, snapshot
  → benchpool/golden-zvol2@ready
```

### Known hacks (search for HACK: and LEARNING: in codebase)

| Hack | Location | Fix |
|------|----------|-----|
| `golden-zvol2` instead of `golden-zvol` | `forge-vm-run.sh`, `firecracker/defaults` | Reboot server, destroy old zvol |
| Entire golden_image.yml is next-learn specific | `golden_image.yml` | Generalize into workload config |
| Inline POSTGRES_URL/AUTH_SECRET in workflow | `fc_bench_repo.yml` | forge-ci-action env input |
| /ci-start.sh called explicitly | `fc_bench_repo.yml` | Auto-detect services from golden image |
| ci/seed.sql is next-learn specific | `ci/seed.sql` | Per-workload seed scripts |
| Static firecracker binaries manually deployed | `forge-vm-run.sh` | Ansible role or Makefile target |
| Sudoers file wiped on server | Server state | Run base role |
| golden_image.yml Postgres seed untested via Ansible | `golden_image.yml` | Add bind-mount tasks, test e2e |

### Key learnings

1. Nix + Firecracker don't mix (dynamic linking vs jailer chroot)
2. Alpine > Nix for guest rootfs (standard paths)
3. Kernel CONFIG_IP_PNP not enabled — forgevm-init configures network in userspace
4. Loopback (127.0.0.1) not auto-configured in Firecracker VMs
5. Alpine /sbin/init is a busybox symlink — must rm before cp
6. initdb needs /dev/null — bind-mount host /dev
7. uuid-ossp not in Alpine postgresql — use gen_random_uuid()
8. sed -i fails on zvols after repeated create/destroy
9. ZFS zvol "dataset is busy" — may need server reboot

### Next: generalize workload configuration

The platform vision: describe a workload in YAML (repo, runtime, services, setup, env, CI steps), and the platform builds the golden image and runs CI automatically. See `HACK:` comments for what needs to change.

## Assistant Contract

* When proposing solutions, think from the perspective of the user of the system. The user is a sole operator of a single-person software company.
* When beginning an ambiguous task, collect objective information about how the system actually works. There are a lot of technologies being stitched together so its important to understand how everything connects.
* You are expected to push back on poor technical decisions. Technical decisions are poor when they couple too much to a specific workflow (e.g. hardcoding Postgres in every Firecracker VM), attempt to use technology in ways its not meant to be used (e.g. using Nix inside of a firecracker VM)

## Tool Use Contract

* When executing long-running tasks, execute them in the background and check in every 30 - 60 seconds.

## Output Contract

* Act as a dispassionate CTO.

## License

MIT
