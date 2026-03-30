# Firecracker Tracer Bullet

One bullet through every layer: ZFS zvol clone → Firecracker VM → command → ClickHouse wide event.

## Architecture

```
Host (orchestrator)
│
├── ZFS Pool
│   ├── benchpool/golden-zvol@ready     ← ext4 rootfs in zvol, Nix-built
│   └── benchpool/ci/job-<uuid>         ← COW clone (~1.7ms)
│
├── Jailer (chroot + PID namespace)
│   └── Firecracker VM
│       └── /dev/vda ← zvol clone (ext4, guest sees block device)
│           ├── /sbin/init              ← forgevm-init (PID 1)
│           ├── /etc/ci/job.json        ← job config (written by host)
│           └── /usr/bin/{node,git,...}  ← Nix store closure
│
└── ClickHouse
    └── ci_events (wide event with VM metrics)
```

## Prerequisites

- Bare metal host with KVM support (EPYC/Xeon with VT-x)
- ZFS pool (`benchpool`) with available space
- Nix installed on host
- forge-metal deployed (`make deploy`)

## Build

```bash
# Build all Firecracker guest components
nix build .#ci-guest-rootfs .#ci-kernel .#forgevm-init

# The rootfs image is at result/rootfs.ext4
# The kernel vmlinux is at result-1/vmlinux (from ci-kernel dev output)
```

## Deploy

```bash
# Push Firecracker role to host
make deploy  # includes firecracker role

# Or deploy just the Firecracker role:
ansible-playbook -i ansible/inventory/hosts.ini \
  ansible/playbooks/site.yml --tags firecracker
```

The Ansible role:
1. Loads `kvm_amd` module, verifies `/dev/kvm`
2. Creates `firecracker` user (UID 10000)
3. Creates `/srv/jailer` directory
4. Sets `zfs_arc_max` to 8 GiB
5. Creates golden zvol (`benchpool/golden-zvol`, 4G, volblocksize=16K)
6. Writes the Nix-built ext4 image to the zvol
7. Snapshots as `benchpool/golden-zvol@ready`
8. Deploys vmlinux to `/var/lib/ci/vmlinux`

## Run

```bash
# Simple test: run a node command
ssh host 'bmci firecracker-test --command "node -e console.log(42)"'

# With metadata
ssh host 'bmci firecracker-test \
  --repo https://github.com/example/repo \
  --commit abc123 \
  --command "node -e console.log(42)"'

# Custom resources
ssh host 'bmci firecracker-test \
  --vcpus 4 --memory 1024 \
  --command "bash -c \"echo hello && sleep 1\"" \
  --timeout 5m'
```

### Expected output

```
=== Firecracker Tracer Bullet Results ===
Job ID:         a1b2c3d4-...
Exit Code:      0
Total Duration: 1.234s
  Clone:        5ms
  Jail Setup:   15ms
  VM Boot:      130ms
  Cleanup:      20ms
ZFS Written:    12345 bytes
VM Boot (FC):   125000 us
Block R/W:      1234567 / 234567 bytes
Net RX/TX:      0 / 0 bytes
vCPU Exits:     5678

=== Serial Console Output ===
[init] mounts complete (3ms)
[init] job: node -e console.log(42)
[init] child pid=2 started (5ms since boot)
42
[init] child exited with code 0 (50ms total)
```

## Verify in ClickHouse

```sql
SELECT
    job_id,
    vm_boot_time_us,
    vm_exit_code,
    zfs_written_bytes,
    block_read_bytes,
    block_write_bytes,
    job_config_json
FROM ci_events
ORDER BY created_at DESC
LIMIT 1;
```

## Failure Modes

### KVM not available

**Symptom:** `FATAL: /dev/kvm does not exist`

**Fix:** Enable hardware virtualization in BIOS. Load module:
```bash
modprobe kvm_amd  # or kvm_intel
```

### Golden zvol doesn't exist

**Symptom:** `golden snapshot benchpool/golden-zvol@ready does not exist`

**Fix:** Run the Ansible firecracker role:
```bash
ansible-playbook -i ansible/inventory/hosts.ini \
  ansible/playbooks/site.yml --tags firecracker
```

### Command times out

**Symptom:** Job exceeds `--timeout` duration, context cancelled.

The LIFO cleanup stack ensures the zvol clone, jail directory, and TAP
device are all cleaned up even on timeout.

### zvol clone fails

**Symptom:** `zfs clone` returns error.

Common causes:
- Pool is full: `zpool list` to check
- Golden snapshot was destroyed: re-run Ansible role
- Permission denied: orchestrator must run as root (ZFS operations)

### Network unreachable from guest

**Symptom:** Guest can't reach external hosts.

Check:
```bash
# IP forwarding enabled?
sysctl net.ipv4.ip_forward

# NAT rule present?
iptables -t nat -L POSTROUTING -n

# TAP device up?
ip link show tap-<jobid>
```

### Jailer fails to start

**Symptom:** `jailer exec` error.

Common causes:
- Firecracker binary not found: check `--firecracker-bin` path
- UID/GID conflict: ensure firecracker user exists with correct IDs
- `/srv/jailer` doesn't exist: run Ansible role

## What this does NOT include

- **vsock** — job config via zvol file, logs via serial console
- **OpenBao** — secrets written as plain env vars for now
- **Forgejo runner** — orchestrator invoked directly via CLI
- **Snapshots** — cold boot only; ZFS clone provides the "instant" part
- **Multi-node** — single host, one VM at a time
- **CNI** — simple TAP + iptables NAT
- **Rate limiting / balloon** — phase 2

## Component summary

| Component | Path | Purpose |
|-----------|------|---------|
| Guest init | `cmd/forgevm-init/` | PID 1, mounts, runs command, reaps zombies |
| Orchestrator | `internal/firecracker/` | Full VM lifecycle, LIFO cleanup |
| API client | `internal/firecracker/api.go` | Thin HTTP over Unix socket |
| Network | `internal/firecracker/network.go` | TAP + iptables NAT |
| Metrics | `internal/firecracker/metrics.go` | Parse Firecracker NDJSON |
| ZFS zvol | `internal/firecracker/zvol.go` | Clone, mount, write, destroy |
| CLI | `cmd/bmci/firecracker.go` | `bmci firecracker-test` subcommand |
| Migration | `migrations/002_firecracker_columns.up.sql` | VM-specific ClickHouse columns |
| Ansible | `ansible/roles/firecracker/` | Host setup, golden zvol creation |
| Nix | `flake.nix` | Guest rootfs, kernel, firecracker packages |
