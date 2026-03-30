# Firecracker in Production Beyond AWS Lambda

> Companies using Firecracker (or migrating away from it) in production, with specific
> technical details on storage, networking, snapshots, and lessons learned.
>
> Companion to [../firecracker.md](../firecracker.md) which covers snapshot mechanics, ZFS comparison,
> and webapp.io/LayerCI.
>
> Researched 2026-03-29.

## Companies studied

| Company | Use case | VMM | Status |
|---------|----------|-----|--------|
| [Fly.io](#flyio) | App hosting platform | Firecracker | Active, thousands of VMs |
| [Koyeb](#koyeb) | Serverless platform | Firecracker -> Cloud Hypervisor | Migrated away |
| [Hocus.dev](#hocusdev) | Self-hosted dev environments | Firecracker -> QEMU | Migrated away |
| [E2B](#e2b) | AI agent sandboxes | Firecracker | Active, hundreds of millions of sandboxes |
| [Actuated](#actuated) | CI runners (GitHub Actions, GitLab) | Firecracker | Active |
| [Weaveworks Ignite](#weaveworks-ignite) | GitOps VM management | Firecracker | Archived (2023), succeeded by Flintlock |
| [Unikraft](#unikraft) | Unikernel runtime | Firecracker (as VMM) | Active, complementary technology |

---

## Fly.io

**What they run:** Global app hosting. Every user app runs in a Firecracker microVM on
dedicated bare-metal servers (8-32 cores, 32-256GB RAM). VMs are pinned to specific
physical hosts.

### Storage architecture

Three-layer approach:

1. **Rootfs (ephemeral):** OCI images pulled once during machine creation, stored on host.
   An `overlay-init` shell script creates an OverlayFS at VM boot so writes never touch
   the base rootfs layer. Root filesystems are ephemeral -- destroyed on redeploy.

2. **Volumes (persistent):** Linux LVM2 thin pools on NVMe, carved into thin-provisioned
   logical volumes. ext4 filesystem on each LV. Encrypted with XTS (random keys via
   HashiCorp Vault + Nomad integration). Before VM boot, the orchestrator recreates the
   block device node inside Firecracker's jail and sets up mount points.
   One volume per Machine, one Machine per volume -- no shared storage.

3. **Image storage:** OCI images in an S3-backed repository, downloaded once to host
   during `Machine.create()`. Stopped Machines incur only storage cost (~$0.15/month
   for 1GB image). This amortizes the slow image-pull across multiple start operations.

Source: [Persistent Storage and Fast Remote Builds](https://fly.io/blog/persistent-storage-and-fast-remote-builds/)

### Init system

Fly.io open-sourced their init: [`superfly/init-snapshot`](https://github.com/superfly/init-snapshot)
(299 stars). Written in Rust, compiled to x86_64-linux-musl. Key details:

- Init binary lives on a dedicated ext2 device mounted as `/dev/vda`
- Application rootfs attached separately as `/dev/vdb`
- Communication via vsock (virtio socket, no TCP/IP network stack exposure to host)
- Configuration injected via `/fly/run.json` on the root device
- Published version differs from actual production deployment

Source: [superfly/init-snapshot on GitHub](https://github.com/superfly/init-snapshot)

### Networking

- Fly Proxy: Rust-based reverse proxy handling connection routing and TLS termination
- WireGuard tunnels for inter-datacenter backhaul
- Anycast IP addresses for global traffic distribution
- TAP devices backing Firecracker's emulated network devices (standard pattern)

### Boot times

- **Machine create:** Slow (multiple seconds) -- involves API routing, database checks in
  Virginia, NATS messaging to regional hosts, resource reservation, image pull
- **Machine start (pre-created):** ~300ms total, of which 10-150ms is geographic latency
  to the host. OCI image already local, no pull needed.

Source: [Fly Machines: an API for fast-booting VMs](https://fly.io/blog/fly-machines/)

### Containerd integration

Fly.io uses the **devmapper snapshotter** for containerd to convert OCI images into block
devices for Firecracker. The devmapper snapshotter is a gRPC proxy plugin implementing
containerd's snapshotter API, using Linux device-mapper thin provisioning for deduplicated
storage between content layers. This is the same technology Docker used historically.

Source: [firecracker-containerd snapshotter docs](https://github.com/firecracker-microvm/firecracker-containerd/blob/main/docs/snapshotter.md)

### Lessons for forge-metal

- LVM thin pools for volumes are simpler than ZFS for single-attach block devices, but
  lack ZFS's clone/snapshot semantics needed for CI golden images.
- The overlay-init pattern (OverlayFS inside the VM, base image read-only) is universally
  adopted -- E2B and actuated use the same approach.
- Pinning VMs to physical hosts trades fault tolerance for simplicity and speed. Acceptable
  for CI where jobs are ephemeral.
- Devmapper snapshotter provides dedup between OCI layers but has known performance
  concerns for file read/write/COW operations.

---

## Koyeb

**What they run:** Serverless platform. Started with Kubernetes, migrated to
Nomad + Firecracker, then migrated Firecracker to Cloud Hypervisor.

### Why they chose Firecracker (and then left)

**Chose Firecracker because:**
- Purpose-built for multi-tenant serverless workloads
- gVisor on Kubernetes had disappointing performance
- Running Firecracker inside Kubernetes was experimental
- ~100MB hypervisor overhead vs Kubernetes's 10-25% RAM overhead per Kubelet

**Left Firecracker because:**
- No GPU passthrough support (Cloud Hypervisor has it)
- Needed snapshot/restore for scale-to-zero (Firecracker's snapshot support
  has known limitations; Cloud Hypervisor's was more mature for their use case)

Source: [The Koyeb Serverless Engine](https://www.koyeb.com/blog/the-koyeb-serverless-engine-from-kubernetes-to-nomad-firecracker-and-kuma)

### Architecture stack

```
Nomad (orchestrator)
  -> custom Nomad driver for VMM integration
    -> Kata Containers (abstraction layer, containerd-shim-kata-v2)
      -> Cloud Hypervisor (VMM, replaced Firecracker)
        -> microVM per workload
```

Kata Containers acts as the bridge: makes VMs look like regular OCI containers to
containerd and Nomad. This abstraction lets them swap VMMs without changing the
orchestration layer.

Source: [Scale-to-Zero: Wake VMs in 200ms](https://www.koyeb.com/blog/scale-to-zero-wake-vms-in-200-ms-with-light-sleep-ebpf-and-snapshots)

### Scale-to-zero implementation (200ms wake)

This is their most technically interesting contribution:

1. **eBPF idle detection:** Custom eBPF program monitors inbound packets at kernel level,
   incrementing counters per instance. Uses `sk_buff.mark` + iptables rules to tag and
   skip Consul health-check packets (prevents health checks from keeping services awake).
   A `scaletozero-agent` daemon watches counters and initiates sleep when no real traffic
   arrives for a configured period.

2. **Snapshot save:** Added `pause_with_snapshot` endpoint to Kata's shim. Saves full VM
   state (memory + CPU + devices) to disk.

3. **Snapshot restore:** Added `resume_from_snapshot` endpoint. Patched Kata's restore
   to run `ch-remote restore` as a subprocess. Network file descriptors passed via
   `ExtraFiles` mechanism (SCM_RIGHTS Unix socket).

4. **Wake on traffic:** eBPF detects real (non-health-check) packet, signals agent,
   agent calls `resume_from_snapshot`. First TCP packet fails (VM still restoring),
   TCP's automatic retry succeeds ~200ms later.

**Critical bug found:** virtio-fs (shared filesystem) couldn't reattach after snapshot
restoration. Solution: abandoned virtio-fs entirely, switched to block devices for all
storage.

### Networking

Kuma service mesh (built on Envoy) for service-to-service communication. Native mTLS.
Multi-region without per-region control planes.

### Lessons for forge-metal

- Kata Containers as an abstraction layer over VMMs is smart for future-proofing, but
  adds complexity forge-metal doesn't need (single VMM, single use case).
- The eBPF idle detection pattern is clever but irrelevant for CI (jobs are never idle).
- **virtio-fs breaks on snapshot restore** -- use block devices instead. This validates
  forge-metal's zvol approach (block device, not shared filesystem).
- Custom Nomad drivers are feasible and reportedly straightforward to build.

---

## Hocus.dev

**What they ran:** Self-hosted dev environments (alternative to GitHub Codespaces / Gitpod).
Open source. Started with Firecracker, replaced it with QEMU. Project appears dormant.

Source: [Why We Replaced Firecracker with QEMU](https://hocus.dev/blog/qemu-vs-firecracker/)

### Why Firecracker failed for dev environments

Firecracker was designed for AWS Lambda: workloads that run for seconds, not hours/days.
Three critical limitations emerged:

1. **Memory never returned to host.** Once a workload inside allocates RAM, Firecracker
   never reclaims it. An idle VM with 32GB allocated keeps 32GB of host RAM consumed.
   Memory ballooning exists but is impractical -- guest OS cannot reliably identify
   unused memory for general workloads.

2. **Disk space never reclaimed.** Deleting files inside the VM doesn't free backing
   host storage. A deleted 10GB file still occupies host disk. Only workaround:
   `virt-sparsify` on offline disks (operationally painful).

3. **virtio-blk MMIO throughput bottleneck.** Firecracker uses MMIO transport for
   virtio-blk, which creates throughput bottlenecks under intensive I/O. QEMU's PCI
   transport alternative offers better performance.

4. **No GPU support.** Required for some development workloads.

### QEMU was not free either

Switching to QEMU solved the above but required two months of experimentation:

- Free page reporting (returning unused pages to host)
- Linux DAMON subsystem tuning (Data Access Monitor for memory management)
- Transparent huge pages configuration
- Extensive source code analysis of QEMU internals

### Storage approach: Overlaybd

Hocus integrated with [Overlaybd](https://github.com/containerd/accelerated-container-image)
for lazy-pulling container images. For dev environments exceeding 100GB, this allows
booting with only the data needed to start, downloading the rest on demand in the background.

Source: [Virtualizing Development Environments in 2023](https://hocus.dev/blog/virtualizing-development-environments/)

### Lessons for forge-metal

- **Firecracker is wrong for long-running workloads.** Memory and disk reclamation don't
  exist. CI jobs (seconds to minutes) are the sweet spot. forge-metal's CI jobs are
  exactly the right workload type.
- **MMIO vs PCI matters for disk I/O.** Firecracker's virtio-blk MMIO transport is slower
  than PCI. For ZFS zvol-backed CI, this means the zvol's sequential throughput may be
  bottlenecked by MMIO if we ever layer Firecracker on top.
- **Overlaybd lazy pulling** is interesting for golden image distribution across nodes,
  but irrelevant when the golden image is a local ZFS zvol.

---

## E2B

**What they run:** Cloud sandbox runtime for AI agents. Firecracker-based. Claims
"hundreds of millions of sandboxes" served.

Source: [Scaling Firecracker: Using OverlayFS](https://e2b.dev/blog/scaling-firecracker-using-overlayfs-to-save-disk-space)

### Storage: OverlayFS with Squashfs base

The most documented rootfs approach in the Firecracker ecosystem:

```
Squashfs (read-only, compressed base image, shared by ALL instances)
  |
  +-- OverlayFS merge --+
  |                      |
  upperdir: ext4 sparse file (5GB, 0 actual bytes until written)
  workdir:  /overlay/work
```

Implementation via `overlay-init` script inside the VM:
1. Mount tmpfs or ext4 at `/overlay`
2. Configure OverlayFS: `lowerdir=/` (read-only rootfs), `upperdir=/overlay/root`
3. `pivot_root` to the merged view
4. Kernel boot param `overlay_root=vdb` tells init which device has the overlay partition

**Key benefit:** Instead of copying hundreds of MB per instance, only delta writes
consume storage. Sparse files show 5GB provisioned but 0 bytes actual until written.

### Security model

- Firecracker's jailer process: Linux cgroups + namespaces isolate the VMM process
  itself before dropping privileges
- Minimal device model: virtio-block, virtio-net, serial console, 1-button keyboard
- Each sandbox gets its own kernel instance

### Lessons for forge-metal

- **OverlayFS inside the VM is the universal pattern** for Firecracker rootfs when you
  don't have ZFS on the host. E2B, Fly.io, and actuated all converged on this.
- **ZFS zvol clones are strictly superior** for forge-metal's use case: they provide the
  same COW semantics (shared base, per-job deltas) but at the block level with kernel
  integration, no need for OverlayFS inside the guest, and `zfs get written` for
  monitoring delta size.
- E2B's Squashfs + OverlayFS approach is the right comparison for "what if we didn't
  have ZFS?" -- it works but adds guest-side complexity.

---

## Actuated

**What they run:** CI runners for GitHub Actions and GitLab CI on customer bare-metal
servers. Firecracker-based. Commercial product.

Source: [Blazing fast CI with MicroVMs](https://blog.alexellis.io/blazing-fast-ci-with-microvms/)

### Architecture

- Each CI job gets a dedicated Firecracker microVM
- Immutable base image built with automation, updated regularly
- Docker preinstalled and running at boot inside each VM
- systemd init inside the VM
- When the GitHub/GitLab runner process exits, the VM is forcibly destroyed
- No state persists between jobs (clean isolation)

### Boot times

- VM boot to job running: <1 second
- Scheduling + packing: multiple concurrent jobs per host based on available resources

### Rootfs construction

Uses the `firecracker-init-lab` approach (same author, Alex Ellis):
1. Start from `weaveworks/ignite-ubuntu` base image (includes udev and essential VM packages)
2. `docker create` + `docker export` to extract filesystem as tar
3. Allocate loopback file (5GB), `mkfs.ext4`, mount, extract tar, unmount
4. Custom Go init process mounts `/proc`, `/sys`, `/dev`, tmpfs, sets hostname

Source: [Grab your lab coat - building a microVM from a container](https://actuated.com/blog/firecracker-container-lab)

### Networking (TAP setup)

Standard pattern documented in the lab:
```bash
ip tuntap add tap0 mode tap
ip addr add 172.18.0.1/24 dev tap0
ip link set tap0 up
# Enable forwarding + NAT
sysctl -w net.ipv4.ip_forward=1
iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE
iptables -A FORWARD -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
iptables -A FORWARD -i tap0 -o eth0 -j ACCEPT
```

Guest kernel boot args: `ip=172.18.0.2::172.18.0.1:255.255.255.0::eth0:off`

### Performance example

ARM64 build for Parca project: 33m5s with QEMU emulation -> 1m26s on native hardware
with Firecracker microVMs. 22x speedup (though most of this is native vs emulated,
not Firecracker-specific).

Source: [Secure microVM CI for GitLab](https://actuated.com/blog/secure-microvm-ci-gitlab)

### Lessons for forge-metal

- Actuated validates the "ephemeral VM per CI job" model at scale.
- Their rootfs construction (docker export -> ext4 loopback) is simpler than devmapper
  but doesn't deduplicate. ZFS zvol clones solve this better.
- The TAP networking recipe is the canonical pattern for Firecracker networking.
- actuated is the closest commercial analog to what forge-metal's CI does, but without
  ZFS's instant COW cloning -- they start from full rootfs copies.

---

## Weaveworks Ignite

**What they ran:** GitOps-managed Firecracker VM fleet. Archived December 2023 when
Weaveworks shut down. Succeeded by [Flintlock](https://github.com/weaveworks-liquidmetal/flintlock).

Source: [weaveworks/ignite on GitHub](https://github.com/weaveworks/ignite)

### Architecture

Docker/OCI images used directly as VM rootfs -- no `.vmdk` or `.qcow2` needed:
- `ignite run <oci-image>` converts OCI image to Firecracker rootfs
- Separate kernel images (OCI containers containing `/boot/vmlinux`)
- `/sbin/init` as PID 1 (true VM, not containerized app)
- CNI networking (compatible with Weave Net and other CNI plugins)
- ~125ms boot time with default 4.19 kernel

### GitOps integration

The `ignited gitops` daemon:
- Monitors a Git repository for VM specification changes
- Applies changes automatically (create/destroy/update VMs)
- Commits local VM state changes back to the repository
- Declarative VM management: VMs defined as YAML in Git

### Why it matters

Ignite proved that Firecracker VMs can be managed with container-like UX (docker-like CLI)
and GitOps workflows. Key insight: VMs as cattle, not pets.

Demonstrated 4,000 microVMs on a single host while maintaining security boundaries.

### Lessons for forge-metal

- The OCI-to-rootfs conversion pattern is well-proven but unnecessary when golden images
  are ZFS zvols built by the orchestrator.
- GitOps VM management is elegant but over-engineered for CI where the scheduler owns
  the full lifecycle.
- **Flintlock** (the successor) uses containerd + devmapper for storage, worth watching
  if forge-metal ever needs a higher-level VM management layer.

---

## Unikraft

**What they are:** Micro-library OS (unikernel framework). Not a competitor to Firecracker
but a complementary technology -- Unikraft images run ON Firecracker.

Source: [Unikraft Performance](https://unikraft.org/docs/concepts/performance)

### Performance comparison with Firecracker

| Metric | Unikraft on Firecracker | Alpine Linux on Firecracker |
|--------|------------------------|---------------------------|
| Boot time | <1ms (kernel), ~3ms total with VMM | ~330ms |
| Image size | <2MB | Hundreds of MB |
| Memory | 2-6MB | Tens of MB minimum |
| Redis throughput | 30-80% faster than containers | Baseline |
| Nginx throughput | 70-170% faster than Linux VMs | Baseline |

Unikraft achieves 10-60% faster than *native Linux* on Redis by eliminating OS abstractions
the application doesn't use.

### Production deployment

Unikraft Cloud launched with $6M funding (Oct 2025). Claims from Prisma founder:
"We can run over 100,000 strongly isolated PostgreSQL instances on a single machine."

Source: [Unikraft launches with $6M](https://www.businesswire.com/news/home/20251009046776/en/)

### Lessons for forge-metal

- Unikraft is irrelevant for CI (need full Linux userspace for `npm`, `node`, build tools).
- The boot time numbers show what's possible when you strip the OS: <1ms kernel boot on
  Firecracker. Forge-metal's ZFS clone (~1.7ms) is in the same ballpark for the storage
  layer.
- Unikraft's approach to running databases (one unikernel per SQL query/connection) is
  architecturally similar to forge-metal's "one zvol clone per CI job."

---

## Cross-cutting patterns

### Storage approaches ranked by complexity

| Approach | Who uses it | Dedup? | Host-side COW? | Guest complexity |
|----------|-------------|--------|---------------|------------------|
| ZFS zvol clone | **forge-metal** | Yes (shared base) | Yes (kernel COW) | None (ext4 on /dev/vda) |
| OverlayFS + Squashfs | E2B, Fly.io | Shared base | No | overlay-init script |
| Devmapper thin provisioning | Fly.io (containerd), firecracker-containerd | Yes (layer dedup) | Yes | None |
| Loopback ext4 copy | Actuated | No | No | None |
| LVM thin pool | Fly.io (volumes) | No | Thin provisioned | None |

**ZFS zvol clones are the simplest approach that provides both dedup and COW.** Every
other approach either requires guest-side overlay setup (E2B/Fly), complex device-mapper
configuration (firecracker-containerd), or copies full images (actuated).

### Networking pattern (universal)

Every Firecracker deployment uses the same TAP device pattern:
```
Host: TAP device (172.18.0.1/24) <-> iptables NAT <-> physical NIC
Guest: eth0 (172.18.0.2/24), gateway 172.18.0.1
```

For multiple VMs: one TAP device per VM, bridge or individual NAT rules.
Rate limiting via Firecracker's built-in token bucket (dual: bytes/s + ops/s).

### Rate limiting (Firecracker built-in)

Firecracker implements rate limiting via token bucket algorithm for both network and
storage devices:
- Dual buckets: bandwidth (bytes/second) AND operations/second
- One-time burst: extra initial credit that doesn't replenish
- Over-consumption: allowed but forces proportional wait
- Timer-based replenishment via Linux `timerfd`
- Event-driven: implements `AsRawFd` for event loop integration

Source: [Firecracker Rate Limiting](https://codecatalog.org/articles/firecracker-rate-limiting/)

### Kernel configuration gotcha

Firecracker's default kernel configs lack container-support modules. Since Firecracker
prohibits loadable kernel modules, all required functionality must be compiled statically
(`=Y`, not `=M`). Required for running containers inside VMs:

- `IP6_NF_IPTABLES=Y`
- `NETFILTER_XT_MARK=Y`
- Crypto acceleration modules
- `NET_SCH_FQ_CODEL=Y`

Source: [Exploring Firecracker for Multi-Tenant Dagger CI/CD](https://www.felipecruz.es/exploring-firecracker-microvms-for-multi-tenant-dagger-ci-cd-pipelines/)

### Snapshot production readiness

As of Firecracker's current docs, snapshot restore has these known limitations:
- Network and vsock packet loss expected on resume
- High restoration latency with cgroups v1 (use v2)
- Snapshotting during early kernel boot can crash
- Diff snapshots in developer preview only
- Security: duplicate entropy, crypto keys, boot IDs across clones
- Docs still say "not recommended" for production snapshots

Despite this, AWS Lambda uses snapshots at massive scale internally, and E2B and others
use them in production. The gap is between upstream docs (conservative) and what
sophisticated operators achieve with mitigations.

Source: [Firecracker snapshot-support.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/snapshotting/snapshot-support.md)

---

## Key takeaway for forge-metal

The research confirms forge-metal's architecture is well-positioned:

1. **ZFS zvol clones beat every rootfs approach** used by production Firecracker deployments.
   E2B's OverlayFS, Fly's devmapper, actuated's full copies -- all are more complex or
   less efficient than `zfs clone`.

2. **Firecracker is right for short-lived workloads only.** Hocus and Koyeb both migrated
   away when they needed long-running VMs or GPU support. CI jobs are the sweet spot.

3. **forge-metal doesn't need Firecracker snapshots.** ZFS clone provides the "instant
   environment" without the complexity of memory snapshots, entropy remediation, and
   CPU-model portability constraints. The 2-5s delta (ZFS clone + cold process start vs
   snapshot restore with warm process) doesn't justify the added infrastructure.

4. **The TAP + NAT networking pattern is standard.** No need to innovate here.

5. **Rate limiting is built into Firecracker.** If forge-metal adds Firecracker VMs in the
   future, network and disk rate limiting per job comes for free via token bucket config.
