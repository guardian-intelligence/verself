# Firecracker MicroVM Snapshots — Full VM State Capture

> Firecracker snapshots capture memory + CPU + device state (not disk).
> Used by AWS Lambda, webapp.io (LayerCI), and others for instant VM resume.
>
> Repo: [firecracker-microvm/firecracker](https://github.com/firecracker-microvm/firecracker)
> Key source: webapp.io blog posts, Marc Brooker's blog, dev.to writeups

## What gets captured vs ZFS

| Layer | Firecracker | ZFS |
|-------|-------------|-----|
| CPU registers (instruction pointer, stack pointer) | Yes | No |
| Guest RAM (full contents) | Yes | No |
| Device state (virtio queues, serial) | Yes | No |
| Filesystem blocks | **No** — managed separately | Yes |
| Running processes | Effectively yes (resume from exact instruction) | No |

A Firecracker snapshot = two files:
- **Memory file**: flat dump of guest RAM, `mmap(MAP_PRIVATE)` on restore for COW
- **MicroVM state file**: serialized device models + KVM state, 64-bit CRC, versioned format

Disk is explicitly NOT included. User must manage disk images separately.

## The ~28ms restore breakdown

| Phase | Time |
|-------|------|
| Firecracker process startup | ~5ms |
| Memory-map snapshot file | ~8ms |
| Restore CPU and device state | ~10ms |
| vsock reconnection + ready signal | ~5ms |
| **Total** | **~28ms** |

Compare cold boot: ~1.1s (kernel load + init).

Source: [How I built sandboxes that boot in 28ms](https://dev.to/adwitiya/how-i-built-sandboxes-that-boot-in-28ms-using-firecracker-snapshots-i0k)

Speed comes from:
1. **No boot sequence** — kernel already booted, init already ran, services already started
2. **Lazy memory via mmap** — pages faulted in on demand, not bulk-loaded
3. **Minimal device model** — 4 devices (virtio-block/net, serial, keyboard) vs QEMU's hundreds

Marc Brooker (Firecracker team lead) states core restore can be **4ms**, believes
sub-millisecond is achievable.

Source: [Lambda SnapStart](https://brooker.co.za/blog/2022/11/29/snapstart.html)

## webapp.io: snapshot-per-step CI

Layerfiles (like Dockerfiles) execute each instruction in a VM. After each instruction,
snapshot. On future runs, if the files a step depends on haven't changed, restore snapshot
instead of re-executing. 10.5x speedup demonstrated.

The `CHECKPOINT` instruction gives explicit control over snapshot points.

Founder noted: "Firecracker is heading in the same direction we did with diff snapshots,
though they don't let you create a COW chain yet." Their custom hypervisor supports chaining
snapshots — upstream Firecracker does not.

Source: [10x faster with Firecracker](https://webapp.io/blog/github-actions-10x-faster-with-firecracker/)

## AWS Lambda's 3-tier snapshot cache

The most sophisticated implementation:
- **L1**: local in-memory cache on worker (67% hit rate)
- **L2**: per-AZ distributed cache (32% hit rate)
- **L3**: S3 authoritative storage (<0.1% of requests)

Data chunked into 512KiB blocks, hash-deduplicated (75% of container images contain <5%
unique bytes), erasure-coded for AZ-cache resilience, FUSE for lazy on-demand loading.
Only ~6.5% of data actually loaded on average.

Source: [Container Loading in AWS Lambda (USENIX ATC'23)](https://www.usenix.org/conference/atc23/presentation/brooker)

## Limitations vs ZFS for CI

| Dimension | Firecracker | ZFS (forge-metal) |
|-----------|-------------|-------------------|
| Snapshot size | O(guest RAM) — 512MB-8GB per snapshot | O(1) per clone, O(written) per job |
| 100 concurrent jobs | ~400GB memory snapshots | ~500MB shared + deltas |
| CPU portability | Locked to CPU model (Intel↛AMD) | Portable via `zfs send/recv` |
| Disk management | NOT included — needs device-mapper/overlay | Unified, disk IS the snapshot |
| Uniqueness | Duplicate entropy, crypto keys, boot IDs | No concern (stateless files) |
| Complexity | KVM + guest kernel + rootfs + jailer + device-mapper | `zfsutils-linux` |
| Production readiness | Full snapshots GA since v1.13 (Aug 2025); diff snapshots still dev preview | Battle-tested filesystem |

## The uniqueness footgun

Resuming the same snapshot twice = two VMs with identical:
- CSPRNG state
- Crypto keys
- `/proc/sys/kernel/random/boot_id`
- Systemd machine-id

Mitigations:
- Linux 5.18+ VMGenID — Firecracker writes new random ID on resume, forces kernel reseed
- `virtio-rng` device for host entropy injection
- Delete `/var/lib/systemd/random-seed` before snapshot
- Bind-mount unique file over `/proc/sys/kernel/random/boot_id` per clone
- Pre-5.18: `RNDADDENTROPY` + `RNDRESEEDCRNG` ioctls (requires `CAP_SYS_ADMIN`)

Source: [Restoring Uniqueness in MicroVM Snapshots](https://arxiv.org/abs/2102.12892)

## Disk sharing without ZFS

Since Firecracker doesn't snapshot disk, users need separate COW for block devices:

- **Device-mapper thin provisioning**: base linear device + `dmsetup create` overlay per VM
  Source: [Julia Evans — Day 47: device mapper for Firecracker](https://jvns.ca/blog/2021/01/27/day-47--using-device-mapper-to-manage-firecracker-images/)
- **Squashfs base + per-VM ext4 overlay** (firecracker-containerd's approach)
- **tmpfs overlay** for ephemeral workloads

## Honest comparison for forge-metal's Next.js CI workload

- ZFS clone (~2-6ms) + Firecracker cold boot (~125ms to init) + Node.js startup (~50ms)
  + npm module loading + V8 JIT warmup (~1-2s) = **~1.4-2.2s to job start**
- Firecracker snapshot restore (~28ms) with Node.js running, V8 warm, modules loaded
  = **~28ms to job start**
- Delta: ~1.4-2.2s of boot + process startup

The 1-2s saved doesn't justify the complexity cost (KVM, guest kernel, rootfs images,
device-mapper for disk, jailer, CPU-model portability, snapshot versioning, entropy
remediation). Especially since forge-metal already has gVisor for sandboxing.

See [firecracker-vm/](firecracker-vm/) for deep-dive research on the Go SDK, jailer
integration, guest kernel config, networking, metrics, and production deployments.

Firecracker wins for: CI jobs needing a running database with warm buffer pools and
established connections (the process state is expensive to reconstruct).

## Reading list

- [Seven Years of Firecracker](https://brooker.co.za/blog/2025/09/18/firecracker.html) — Brooker retrospective; Aurora DSQL uses one VM per SQL transaction
- [Fireworks (EuroSys'22)](https://multics69.github.io/pages/pubs/fireworks-shin-eurosys22.pdf) — research on faster snapshot restore
- [Launch HN: LayerCI](https://news.ycombinator.com/item?id=25979941) — founder discusses custom hypervisor
