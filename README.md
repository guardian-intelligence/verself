
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

### 4. Seed and assume rehearsal personas

After deploy, seed the platform tenant, Acme tenant, billing state, mailboxes,
and auth fixtures:

```bash
make seed-system
```

The `assume-*` targets are extremely useful utility scripts for operators and
agents. They mint short-lived, project-scoped Zitadel tokens from the deployed
credential store and write a `0600` env file under `artifacts/personas/`.

```bash
make assume-platform-admin
make assume-acme-admin
make assume-acme-member
make assume-persona PERSONA=platform-admin OUTPUT=/tmp/platform-admin.env
```

`platform-admin` is for dogfooding internal platform operations. It has
sandbox-rental, webmail/mailbox-service, Letters, and Forgejo OIDC project
access, plus operator command hints for ClickHouse and provider-native Forgejo
automation. `acme-admin` and `acme-member` rehearse the customer org roles used
by rent-a-sandbox.

These scripts do not export the Zitadel admin PAT, ClickHouse password,
Stalwart direct protocol passwords, or Forgejo provider API token. Those remain
behind the existing operator wrappers and remote credstore files.

### 5. Log in

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
2. clone the base zvol with ZFS for each VM job
3. run the submitted command inside an isolated Firecracker microVM
4. stream guest telemetry and structured guest events
5. emit wide events to ClickHouse for inspection in HyperDX


The optimization stack is, at a high level:

- keep a single base guest zvol with the runtime agent installed
- use ZFS for instant copy-on-write VM root disks
- expose selected host services through the host-service plane
- stream VM telemetry over vsock for billing and operations
- keep repo import and CI policy outside the privileged host daemon

Each VM job runs in a Firecracker microVM whose root disk is a ZFS zvol clone of the base guest image. The host orchestrator manages ZFS at the kernel level; guests have no awareness of ZFS.

```
Host (vm-orchestrator — bare metal, root, ZFS kernel module)
│
├── ZFS Pool (NVMe-backed)
│   ├── golden-zvol@ready                 ← base guest image: ext4 inside zvol
│   ├── workloads/job-abc                 ← zvol clone (~1.7ms COW, metadata-only)
│   ├── workloads/job-def                 ← zvol clone
│   └── workloads/job-ghi                 ← zvol clone
│
├── Firecracker VM (job-abc)
│   └── /dev/vda ← /dev/zvol/pool/workloads/job-abc
│       └── ext4 (from base image, COW diverges on write)
│
├── Firecracker VM (job-def)
│   └── /dev/vda ← /dev/zvol/pool/workloads/job-def
│
└── Firecracker VM (job-ghi)
    └── /dev/vda ← /dev/zvol/pool/workloads/job-ghi
```

### Why this layering

| Layer | Provides | Latency |
|-------|----------|---------|
| ZFS zvol clone | Instant COW rootfs from golden image | ~1.7ms kernel, ~5.7ms end-to-end |
| Firecracker microVM | Process/memory/kernel isolation, deterministic execution | ~125ms from snapshot, ~3s cold boot |
| gVisor (inside VM) | Syscall-level sandboxing for untrusted build scripts | Negligible on top of Firecracker |

### Key distinctions

- **zvol, not dataset.** Firecracker takes block devices (`/dev/zvol/...`), not mounted filesystems. A zvol is a ZFS block device — clone/destroy/written all work identically to datasets.
- **Golden image is a zvol with ext4 inside.** Built from the generic guest image produced by the Firecracker rootfs playbook and reused as the base snapshot for direct VM jobs.
- **Guest is unaware of ZFS.** It sees `/dev/vda` with ext4. No ZFS tooling needed in the VM image.
- **Orchestrator owns all ZFS operations.** Allocate (clone), monitor (`written` bytes), teardown (destroy). Implemented in the `vm-orchestrator` host daemon.

### Orchestrator flow per job

```
1. zfs clone pool/golden-zvol@ready pool/workloads/job-abc  # ~1.7ms
2. firecracker --drive path=/dev/zvol/pool/workloads/job-abc # boot VM
3. submitted command runs inside VM
4. VM exits
5. zfs get written pool/workloads/job-abc                    # bytes dirtied -> ClickHouse wide event
6. zfs destroy pool/workloads/job-abc                        # cleanup
```

### What this does NOT use

- **CRIU** — process checkpointing is fragile with Node.js/V8 (timer FDs, JIT pages, epoll). Not worth the complexity when ZFS clone already eliminates the expensive part (warm caches, pre-installed deps). Process startup of `node` is ~50ms.
- **libzfs** — shells out to `zfs` CLI like every production ZFS project (OpenZFS, Incus, DBLab, OBuilder).
- **Nested ZFS in guest** — the guest runs ext4 on a raw block device. ZFS stays on the host where it belongs.

## ZFS

This project makes heavy use of ZFS. Research notes are in `docs/research/`.

## Runtime Notes

- The runtime API accepts direct job commands; repo import/scan metadata is owned by sandbox-rental-service.
- Toolchain detection and repo-owned CI manifest parsing are not part of the current runtime contract.
- The host sends structured guest phases instead of generating `bash -lc` scripts. Shell is still allowed, but only when the workload explicitly uses it in the submitted command.
- Per-job guest config is delivered over the host-initiated vsock control stream. MMDS is not part of the steady-state runtime path.

--- A note on the future ---

We will want long-running VMs with developer tools installed for agents to work within, with full unbounded permissions and access to project source. If they do something destructive to their sandbox we want to restore from a snapshot. If they attempt to exfiltrate secrets, we tightly control egress and only provide encrypted secrets that must go through a layer for decryption. If they attempt to perform a destructive action on production systems, we have a policy layer to prevent it.

## Licensing

This project is open-source. Most bundled server components (ClickHouse, TigerBeetle, Forgejo, PostgreSQL) use permissive or weak-copyleft licenses with no network-interaction obligations.

**Stalwart Mail Server** is licensed under AGPL-3.0. If you run Stalwart unmodified (deployed as a pinned binary from `server-tools.json`), your obligation is to provide users with a link to the upstream source at `github.com/stalwartlabs/stalwart`. Your own application code that communicates with Stalwart over JMAP/SMTP/IMAP is a separate work and is not covered by AGPL. If you modify Stalwart's source and serve it over a network, you must make your modifications available to users who interact with it. Consult a lawyer if you are offering hosted email as a closed-source commercial product built on this stack.
