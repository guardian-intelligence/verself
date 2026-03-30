# Firecracker VM Deep Dive

Deep technical research on Firecracker microVMs for forge-metal's CI isolation layer.
Builds on the initial comparison in [../firecracker.md](../firecracker.md).

Conducted 2026-03-29.

## Documents

| Document | Focus |
|----------|-------|
| [API & Internals](api-and-internals.md) | REST API, snapshot endpoints, memory backends, MMDS, balloon, entropy, recent releases (v1.12-v1.15) |
| [Jailer & Security](jailer-security.md) | Jailer step-by-step execution, seccomp filters (48 syscalls), threat model, ZFS zvol integration pattern |
| [Production Deployments](production-deployments.md) | Fly.io, Koyeb, Hocus, E2B, Actuated, Ignite, Unikraft — storage patterns, networking, migration stories |
| [Guest Kernel](guest-kernel.md) | Minimal kernel config for CI, container/gVisor support, PVH boot, init binary vs systemd, boot time optimization |
| [Networking](networking.md) | TAP at scale, namespace isolation, nftables vs iptables, CNI tc-redirect-tap, rate limiting, MAC generation |
| [Metrics & Observability](metrics-observability.md) | Metrics categories, ClickHouse wide event mapping, serial capture, vsock logs, balloon memory stats |
| [Go SDK](go-sdk.md) | Machine lifecycle, DrivesBuilder for zvols, jailer hard-link problem + custom ChrootStrategy, CNI networking, MMDS, vsock, snapshot load, handler system |

## Key findings

**Jailer + ZFS zvol integration** is straightforward: the orchestrator creates a `mknod`
block device node inside the jail root with the zvol's major/minor numbers. Firecracker
opens it as a regular block device. The jailer never touches ZFS directly.
See [jailer-security.md#jailer--zfs-zvol-how-to-pass-block-devices-into-the-jail](jailer-security.md#jailer--zfs-zvol-how-to-pass-block-devices-into-the-jail).

**ZFS zvol clones beat every production rootfs approach.** E2B uses OverlayFS + Squashfs,
Fly.io uses devmapper thin provisioning, actuated copies full ext4 images. ZFS clone is
simpler and provides both dedup and COW at the block level.

**Firecracker is right for short-lived workloads only.** Hocus and Koyeb both migrated
away when they needed long-running VMs or GPU support. CI jobs (seconds to minutes) are
the sweet spot — validates forge-metal's use case.

**Recommended phased approach:**
1. **Now:** ZFS zvol clones + Firecracker cold boot (~3s). Simple, no snapshot complexity.
2. **If boot time matters:** Snapshot per golden image, MMDS for config injection, `network_overrides` on restore, virtio-rng + VMGenID for entropy.
3. **If memory matters at scale:** UFFD backend for lazy loading, balloon with free_page_reporting, virtio-mem for right-sizing per job type.

## Operational warnings

- **`/proc/mounts` scaling:** jailer parses `/proc/mounts` for cgroup discovery. Many ZFS
  datasets = large `/proc/mounts` = slow jail creation. Use `canmount=off` on non-filesystem
  datasets.
- **Snapshot regeneration:** every Firecracker version bump likely requires new snapshots
  (bitcode format = no backward compatibility).
- **virtio-fs breaks on snapshot restore** (Koyeb found this). Use block devices only.
- **cgroups v2 required** for reasonable snapshot restore latency.
