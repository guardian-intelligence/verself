# Capacity Planning and Operations

> ZFS ARC memory contention, MMIO vs PCI throughput, concurrent VM lifecycle management.
>
> Researched 2026-03-29.

## ZFS ARC vs Firecracker memory contention

### The problem

ZFS uses RAM for ARC (Adaptive Replacement Cache). Firecracker VMs also need RAM.
On a 64GB host running 100 VMs at 512MB each, that's 50GB for VMs alone -- leaving
nothing for ARC, which means every zvol read goes to NVMe.

### zfs_arc_max tuning

**Defaults:**
- OpenZFS historic: 50% of RAM
- OpenZFS 2.3+: `max(RAM - 1GB, 5/8 * RAM)` -- on 64GB host = ~59GB (way too aggressive)
- Proxmox VE 8.1+: changed to 10% of RAM, capped at 16 GiB (acknowledging VM contention)

**Setting it:**

```bash
# Runtime (temporary)
echo "$[8 * 1024*1024*1024]" >/sys/module/zfs/parameters/zfs_arc_max

# Persistent
echo 'options zfs zfs_arc_max=8589934592' > /etc/modprobe.d/zfs.conf
update-initramfs -u -k all
```

**Related parameters:**
- `zfs_arc_min` -- floor for ARC (default: RAM/32). Set lower than `zfs_arc_max`.
- `zfs_arc_sys_free` -- free memory below which ARC shrinks (default: `max(512KB, RAM/64)`).
  On 64GB, this is only 1GB -- **raise it to 4GB** so ARC shrinks before VMs starve.

Source: [OpenZFS Module Parameters](https://openzfs.github.io/openzfs-docs/Performance%20and%20Tuning/Module%20Parameters.html)

### What happens when ARC is starved

- Every zvol block read that misses ARC goes to NVMe
- One benchmark showed ZFS at 6,452 MB/s vs raw device 12,724 MB/s when working set
  exceeds 2x available ARC -- ZFS I/O scheduling adds latency between `io_schedule` calls
- Direct I/O is **not implemented for zvols**, so all reads go through ARC
- Cold reads from zvol clones (first `npm install` in a fresh clone) pay full disk latency

Source: [OpenZFS issue #8381](https://github.com/openzfs/zfs/issues/8381)

### ARC shrink under memory pressure: the critical danger

1. ARC is "reclaimable" but the kernel's kswapd has become less aggressive in recent kernels
2. ARC shrinker limits reclaim to ~100ms per allocation attempt
3. If VMs allocate faster than ARC shrinks, the OOM killer fires
4. **Without swap, OOM killer targets Firecracker processes** before ZFS finishes shrinking
5. Every production ZFS guide says: always configure swap as a safety buffer

Source: [ZFS ate my RAM (2025)](https://blog.thalheim.io/2025/10/17/zfs-ate-my-ram-understanding-the-arc-cache/),
[ARC sizing: When Too Much Cache Slows Down Everything](https://cr0x.net/en/zfs-arc-sizing-too-much-cache/)

### Recommended memory layout for forge-metal (64GB host)

| Reservation | Size | Notes |
|-------------|------|-------|
| Host OS | 2 GiB | Kernel, systemd, orchestrator |
| ZFS ARC (`zfs_arc_max`) | 8-12 GiB | Fixed cap, Proxmox formula: 2 GiB + 1 GiB/TiB storage |
| Swap | 4 GiB | OOM safety net (required with ZFS) |
| Firecracker VMs | 46-50 GiB | ~90 x 512MB or ~50 x 1GB |
| `zfs_arc_sys_free` | 4 GiB | Raise from 1GB default |
| `zfs_arc_min` | 2 GiB | Allow ARC to shrink under pressure |

**Key insight from Proxmox operators:** Treat ARC as a fixed reservation, not dynamic.
Set `zfs_arc_max` conservatively and leave headroom.

Source: [Proxmox ZFS on Linux](https://pve.proxmox.com/wiki/ZFS_on_Linux),
[Klara Systems: ZFS in Virtualization](https://klarasystems.com/articles/zfs-virtualization-storage-backend-for-pros/)

### Balloon device vs right-sizing

For short-lived CI jobs (seconds to minutes), the balloon adds complexity with minimal
benefit. A Fly.io incident showed balloon inflating to 75-82% of guest RAM, causing
594x more major page faults. Instead:

- **Right-size VM memory** (512MB-1GB for npm/tsc workloads)
- **Rely on COW** to keep actual RSS low
- Use **cgroup memory limits** per VM as hard caps
- Consider **virtio-mem** (v1.14+) for dynamic right-sizing per job type

Source: [Firecracker issue #1570](https://github.com/firecracker-microvm/firecracker/issues/1570),
[Fly.io balloon thrashing](https://community.fly.io/t/virtio-balloon-overcommit-causing-severe-memory-thrashing-on-8-cpu-8gb-firecracker-vms/27124)

### KSM (Kernel Same-Page Merging): not worth it

KSM works with Firecracker (`MAP_PRIVATE | MAP_ANONYMOUS`), but:
- CPU overhead: ksmd scanning can consume substantial CPU
- Security risk: timing side channels between VMs
- Limited benefit: CI jobs diverge quickly (different repos, builds)
- Better alternative: **virtio-pmem** (v1.14+) with DAX for shared read-only rootfs

Source: [Linux KSM docs](https://docs.kernel.org/admin-guide/mm/ksm.html)

### Memory overcommit math

| Component | Per-VM | 80 VMs |
|-----------|--------|--------|
| VMM process overhead | <5 MiB | ~400 MiB |
| Guest memory (nominal 512MB) | 512 MiB | 40 GiB |
| Guest actual RSS (CI workload) | ~200-400 MiB | ~16-32 GiB |

Typical CI RSS: `npm install` peaks at 200-400MB, `tsc` at 400-800MB, `next build` at
1-2GB. With COW, actual host memory used is much less than nominal allocation.

**Practical limit on 64GB host:** ~50-80 concurrent 512MB VMs.

## MMIO vs PCI throughput

### The bottleneck

Firecracker historically used MMIO exclusively. Each MMIO read/write triggers a VM exit.
With the Sync I/O engine, block devices max out at **4-5K IOPS**.

### PCI transport benchmarks (v1.13+, `--enable-pci`)

Firecracker's own testing:

| Workload | PCI vs MMIO |
|----------|-------------|
| 1 vCPU sync reads | **+50%** |
| 1 vCPU sync writes | **+46%** |
| 2 vCPU sync reads | **+20%** |
| Network latency | **-27%** |

Guest kernel requires `CONFIG_PCI=y` (already in ACPI boot configs). Max 17 virtio
devices per VM on x86_64 with PCI.

Source: [Firecracker v1.13 release](https://github.com/firecracker-microvm/firecracker/releases),
[PR #5364](https://github.com/firecracker-microvm/firecracker/pull/5364)

### io_uring async engine

Developer preview (`WithIoEngine("Async")` in Go SDK):

| Workload | vs Sync |
|----------|---------|
| NVMe cold reads | **1.5-3x IOPS/CPU, up to 30x total** |
| NVMe writes | **+20-45% total IOPS** |

Caveats: ~110ms device creation latency, higher CPU per write, not production-ready.

Source: [Firecracker issue #1600](https://github.com/firecracker-microvm/firecracker/issues/1600)

### Is MMIO sufficient for CI?

**npm install I/O pattern:**
- Creates 15,000-41,000+ files and 1,800-5,240+ directories
- 50-77MB compressed -> 250-400MB on disk
- Write-heavy, many small files, random writes
- **4-5K IOPS sync limit is likely the bottleneck**

**next build I/O pattern:**
- Read-heavy initially (source + node_modules from page cache)
- Then write-heavy (.next directory, 166-251MB)
- **CPU/memory-bound, not disk-bound** (reads come from warm caches)

**Verdict:** Enable PCI transport (`--enable-pci`). It's a free 20-50% throughput
improvement with no API changes. The npm install bottleneck specifically benefits.

## Concurrent VM lifecycle management

### UID allocation per jail

Firecracker recommends unique UID/GID per jail. Strategies:

1. **Sequential pool (simplest):** Pre-create UIDs 10000-10999. Orchestrator maintains
   free pool, allocates/returns as VMs start/stop.
2. **Hash from job ID:** `uid = hash(jobID) % RANGE + BASE_UID`. No pool but risk of collisions.
3. **Process groups:** Use `process_group(0)` for cleanup (vm0's approach) -- enables
   `killpg()` to kill entire subprocess tree.

### VM state tracking

The Go SDK's `Machine.Wait()` is the integration point:

```go
go func(job Job) {
    ctx, cancel := context.WithTimeout(parentCtx, job.Timeout)
    defer cancel()
    m, _ := firecracker.NewMachine(ctx, cfg)
    m.Start(ctx)
    err := m.Wait(ctx)  // blocks until VM exits or timeout
    // SDK cleanup stack already ran (socket, FIFO, CNI)
    // Orchestrator cleans up: zvol destroy, jail dir, metrics collection
}(job)
```

`Wait()` is safe to call concurrently and delivers the same error to all callers.
Context timeout acts as hard deadline.

### Crash/hang detection: the overwatcher

From `prod-host-setup.md`: Firecracker's signal handlers are not async-signal-safe --
they acquire locks that can deadlock. An overwatcher process periodically checks for
unresponsive Firecracker processes and SIGKILLs them.

**vm0's pattern:**
- When stdout closes while state is `Running`, recognizes unexpected exit
- Atomically swaps state to `Stopped` via CAS
- Notifies waiting operations via channel
- All operations race against crash detection using `select` -- crash causes operations
  to fail with errors rather than hanging

**kvm-pit thread:** After guest start, the kernel creates a `kvm-pit/<pid>` thread in
the root cgroup. An external agent must move it into the VM's cgroup. Firecracker cannot
do this after dropping privileges.

### Timeout enforcement

**Go SDK pattern:** `context.WithTimeout` on the context passed to `Start()` and `Wait()`.
When context expires: SIGTERM -> grace period -> SIGKILL -> cleanup stack runs.

Each SDK API call has its own 500ms timeout (configurable via
`FIRECRACKER_GO_SDK_REQUEST_TIMEOUT_MILLISECONDS`).

### Cleanup on orchestrator restart

Resources that can leak:

| Resource | Detection | Cleanup |
|----------|-----------|---------|
| Firecracker processes | `pgrep firecracker` | SIGKILL |
| ZFS zvol clones | `zfs list -r pool/ci` | `zfs destroy` |
| Network namespaces | `ip netns list` | `ip netns del` |
| TAP devices | Destroyed with namespace | Automatic |
| Jail directories | `ls /srv/jailer/firecracker/` | `rm -rf` |
| Unix sockets | `ls /tmp/fc-*.sock` | `rm` |

**Recovery-on-restart sequence:**

```bash
# 1. Kill orphaned Firecracker processes
for pid in $(pgrep -f firecracker); do kill -9 $pid; done

# 2. Delete orphaned network namespaces
for ns in $(ip netns list | grep '^ci-'); do ip netns del $ns; done

# 3. Destroy leftover zvol clones
for clone in $(zfs list -H -o name -r pool/ci | tail -n +2); do
    zfs destroy -f $clone
done

# 4. Clean jail directories
rm -rf /srv/jailer/firecracker/*/

# 5. Resume normal operation
```

**Network namespace advantage:** Running each VM in its own namespace means deleting
the namespace destroys all contained TAP/veth devices automatically. Far cleaner than
tracking individual devices.

**CNI crash recovery:** The Go SDK's CNI flow "deletes pre-existing CNI network for
this container ID" before creating a new one. Idempotent cleanup-before-create handles
orphaned resources.

Source: [Firecracker prod-host-setup.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/prod-host-setup.md),
[firecracker-go-sdk](https://github.com/firecracker-microvm/firecracker-go-sdk),
[firecracker-containerd architecture](https://github.com/firecracker-microvm/firecracker-containerd/blob/main/docs/architecture.md)

## Sources

- [OpenZFS Module Parameters](https://openzfs.github.io/openzfs-docs/Performance%20and%20Tuning/Module%20Parameters.html)
- [Proxmox ZFS on Linux](https://pve.proxmox.com/wiki/ZFS_on_Linux)
- [ZFS ate my RAM (2025)](https://blog.thalheim.io/2025/10/17/zfs-ate-my-ram-understanding-the-arc-cache/)
- [ARC sizing pitfalls](https://cr0x.net/en/zfs-arc-sizing-too-much-cache/)
- [Klara Systems: ZFS in Virtualization](https://klarasystems.com/articles/zfs-virtualization-storage-backend-for-pros/)
- [Firecracker SPECIFICATION.md](https://github.com/firecracker-microvm/firecracker/blob/main/SPECIFICATION.md)
- [Firecracker issue #1570](https://github.com/firecracker-microvm/firecracker/issues/1570)
- [Firecracker issue #1600](https://github.com/firecracker-microvm/firecracker/issues/1600)
- [Firecracker prod-host-setup.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/prod-host-setup.md)
- [Hocus: Why We Replaced Firecracker with QEMU](https://hocus.dev/blog/qemu-vs-firecracker/)
- [npm install analysis](https://dev.to/pavel-zeman/demystifying-npm-package-installation-insights-analysis-and-optimization-tips-4nmj)
