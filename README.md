
## Quick Start

### 1. Install dev tools

```bash
cd src/platform/ansible && ansible-playbook playbooks/setup-dev.yml
```

### 2. Provision bare metal

```bash
# Create your tfvars (one-time)
cp src/platform/terraform/terraform.tfvars.example.json src/platform/terraform/terraform.tfvars.json
# Edit terraform.tfvars.json — set project_id to your Latitude.sh project

# Provision server + generate Ansible inventory
cd src/platform/ansible && ansible-playbook playbooks/provision.yml
```

This provisions a bare metal server via OpenTofu and auto-generates the gitignored `src/platform/ansible/inventory/hosts.ini` from the outputs. The Latitude.sh auth token is read from SOPS-encrypted secrets.

### 3. Deploy

```bash
cd src/platform/ansible && ansible-playbook playbooks/dev-single-node.yml
```

Idempotent, no wipe. Safe to run repeatedly. Deploy a single role with `--tags`:

```bash
cd src/platform/ansible && ansible-playbook playbooks/dev-single-node.yml --tags caddy
```

### 4. Log in

```bash
# HyperDX admin credentials are in the SOPS-encrypted secrets file
sops -d --extract '["hyperdx_admin_email_slug"]' src/platform/ansible/group_vars/all/secrets.sops.yml
sops -d --extract '["hyperdx_admin_password_base"]' src/platform/ansible/group_vars/all/secrets.sops.yml
# Email: admin+{slug}@forge-metal.local, Password: {base}#@F1
```

Open `https://<ip>` in your browser (self-signed cert for IP addresses, auto Let's Encrypt for domains).

## CI

The CI path is built around repo-specific golden images:

1. start from a generic guest image
2. cold-bootstrap a repo's `main` branch inside Firecracker
3. snapshot that warmed state as the repo golden
4. clone the golden with ZFS for each PR job
5. run CI inside an isolated Firecracker microVM
6. emit wide events to ClickHouse for inspection in HyperDX


The optimization stack is, at a high level:

- keep a repo-specific golden image of `main` with warmed caches
- zfs for instant copy of the warmed golden image
- local Forgejo fetches and mirrors for deterministic refs
- warm default-branch database state when a fixture requests Postgres
- turbo cache locally when the repo uses it

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
- **Golden image is a zvol with ext4 inside.** Built from the generic guest image produced by `scripts/build-guest-rootfs.sh`, then refreshed by the `forge-metal ci warm` path into repo-specific goldens.
- **Guest is unaware of ZFS.** It sees `/dev/vda` with ext4. No ZFS tooling needed in the VM image.
- **Orchestrator owns all ZFS operations.** Allocate (clone), monitor (`written` bytes), teardown (destroy). Implemented in the `forge-metal` Go binary.

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
- **libzfs** — shells out to `zfs` CLI like every production ZFS project (OpenZFS, Incus, DBLab, OBuilder).
- **Nested ZFS in guest** — the guest runs ext4 on a raw block device. ZFS stays on the host where it belongs.

## ZFS

This project makes heavy use of ZFS. Research notes are in `docs/research/`.

## Canonical Workload Contract

The repo-owned workload contract is:

```toml
version = 1

workdir = "."
run = ["bash", "-lc", "npm test"]

prepare = ["bash", "-lc", "npm install"]
services = ["postgres"]
env = ["DATABASE_URL"]
profile = "auto"
```

Meaning:

- `run`: required CI command executed for the job
- `workdir`: optional working directory relative to the repo root
- `prepare`: optional command used when warming the repo golden; defaults to `run`
- `services`: optional local services required inside the VM; currently only `postgres` is supported
- `env`: optional environment variable names expected by the workload; values are copied from the runner environment and missing names fail fast
- `profile`: optional execution-profile override; currently `auto` and `node` are supported, and `auto` resolves to the current Node runtime path

## What The Platform Should Derive

These should not live in repo-owned workload config:

- repo name and description
- default branch
- package manager and version
- runtime version
- lockfile path and cache identity
- base guest selection
- telemetry IDs and run grouping
- generated Forgejo workflow contents

## What Does Not Belong In Workload Config

Fixture-only test metadata should be kept out of the runtime contract:

- PR branch names
- PR titles and commit messages
- find/replace rules used to trigger a fixture PR
- any Forgejo-specific E2E mutation details

Those are fixture orchestration concerns, not workload execution concerns.

## Runtime Notes

- The runtime manifest is read from the checked-out ref, not from the warmed default-branch copy.
- Fixture metadata for Forgejo E2E lives in the internal fixture layer, not in `.forge-metal/ci.toml`.
- Toolchain detection is derived behavior behind the current Node profile; it is not part of the repo-owned config surface.
- The host now sends structured guest phases instead of generating `bash -lc` scripts. Shell is still allowed, but only when the workload explicitly uses it in `run` or `prepare`.
- Per-job guest config is delivered over the host-initiated vsock control stream. MMDS is not part of the steady-state runtime path.

--- A note on the future ---

We will  want long-running VMs with developer tools installed for agents to work within with full unbounded permissions and access to  If they do something destructive to their sandbox we want to restore from a snapshots. If they attempt to exfiltrate secrets, we tightly controll egress and only provide encrypted secrets that must go through a layer for decryption (unless you can think of something better) If they attempt to perform a destructive action on production systems, we have a policy layer to prevent it.
