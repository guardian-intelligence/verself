# Firecracker MicroVM -- REST API, Snapshots, and Internals

> Deep-dive research on Firecracker's REST API, snapshot system, memory management,
> and recent developments. All findings from primary sources (GitHub repo, Swagger spec,
> official docs, release notes).
>
> Repo: [firecracker-microvm/firecracker](https://github.com/firecracker-microvm/firecracker)
> Swagger spec: `src/firecracker/swagger/firecracker.yaml` (API version 1.16.0-dev on main)
> Conducted 2026-03-29 as background research for forge-metal's CI orchestrator.

## Table of Contents

- [REST API Overview](#rest-api-overview)
- [Snapshot API -- Exact Endpoints and Parameters](#snapshot-api----exact-endpoints-and-parameters)
- [Snapshot Versioning and Compatibility](#snapshot-versioning-and-compatibility)
- [Diff Snapshots (Incremental)](#diff-snapshots-incremental)
- [Memory Backend Options](#memory-backend-options)
- [Huge Pages](#huge-pages)
- [Recent Releases (v1.12 through v1.15)](#recent-releases-v112-through-v115)
- [MMDS (MicroVM Metadata Service)](#mmds-microvm-metadata-service)
- [Balloon Device](#balloon-device)
- [Entropy Device (virtio-rng)](#entropy-device-virtio-rng)
- [New in v1.14: virtio-pmem, virtio-mem, PCI](#new-in-v114-virtio-pmem-virtio-mem-pci)
- [Snapshot Security -- Entropy and Uniqueness](#snapshot-security----entropy-and-uniqueness)
- [Network Connectivity for Clones](#network-connectivity-for-clones)
- [CI-Relevant Analysis](#ci-relevant-analysis)

---

## REST API Overview

Firecracker exposes a RESTful API over a **Unix Domain Socket** (default `/run/firecracker.socket`).
All requests are JSON over HTTP. There is no TCP listener -- the UDS is the only transport.

### Key Endpoints (from Swagger spec, API version 1.16.0-dev)

| Method | Path | Timing | Description |
|--------|------|--------|-------------|
| `GET` | `/` | Any | Instance info (id, state, vmm_version, app_name) |
| `PUT` | `/boot-source` | Pre-boot | Kernel image path, boot args, initrd |
| `PUT` | `/drives/{drive_id}` | Pre-boot | Block device (path, read-only, rate limiter) |
| `PATCH` | `/drives/{drive_id}` | Post-boot | Hot-update drive path |
| `PUT/PATCH` | `/machine-config` | Pre-boot | vCPU count (1-32), mem_size_mib, SMT, huge_pages, track_dirty_pages |
| `PUT` | `/network-interfaces/{id}` | Pre-boot | TAP device, guest MAC, rate limiter |
| `PUT` | `/vsock` | Pre-boot | guest_cid, UDS path |
| `PUT` | `/balloon` | Pre-boot | amount_mib, deflate_on_oom, stats, free_page_reporting/hinting |
| `PATCH` | `/balloon` | Any | Update balloon target size |
| `PUT` | `/entropy` | Pre-boot | virtio-rng with optional rate limiter |
| `PUT` | `/pmem/{id}` | Pre-boot | virtio-pmem backed by host file |
| `PUT` | `/hotplug/memory` | Pre-boot | virtio-mem total_size_mib, block/slot sizes |
| `PATCH` | `/hotplug/memory` | Post-boot | Change requested_size_mib (plug/unplug) |
| `PUT` | `/mmds/config` | Pre-boot | MMDS version (V1/V2), network interfaces, IPv4 address |
| `PUT/PATCH/GET` | `/mmds` | Any | MMDS data store (JSON) |
| `PUT` | `/actions` | Post-boot | `InstanceStart`, `SendCtrlAltDel` |
| `PATCH` | `/vm` | Post-boot | Set state: `Paused` or `Resumed` |
| `PUT` | `/snapshot/create` | Post-boot | Create full or diff snapshot |
| `PUT` | `/snapshot/load` | Pre-boot | Load snapshot (fresh process only) |
| `GET` | `/vm/config` | Any | Full VM configuration dump |
| `GET` | `/version` | Any | Firecracker version string |
| `PUT` | `/cpu-config` | Pre-boot | Custom CPU template |
| `PUT` | `/logger` | Pre-boot | Log file path, level, show_level/origin |
| `PUT` | `/metrics` | Pre-boot | Metrics output file path |
| `PUT` | `/serial` | Pre-boot | Serial console output path (v1.14+) |

Source: `src/firecracker/swagger/firecracker.yaml` on `main` branch.

---

## Snapshot API -- Exact Endpoints and Parameters

### Step 1: Pause the VM

```bash
curl --unix-socket /tmp/firecracker.socket -i \
    -X PATCH 'http://localhost/vm' \
    -H 'Content-Type: application/json' \
    -d '{"state": "Paused"}'
```

Prerequisite: VM must be booted. Idempotent (successive calls keep VM paused).

### Step 2: Create Snapshot

**Endpoint:** `PUT /snapshot/create`

**SnapshotCreateParams:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `snapshot_path` | string | Yes | Path to write microVM state file |
| `mem_file_path` | string | Yes | Path to write guest memory file |
| `snapshot_type` | enum | No | `Full` (default) or `Diff` |

```bash
curl --unix-socket /tmp/firecracker.socket -i \
    -X PUT 'http://localhost/snapshot/create' \
    -H 'Content-Type: application/json' \
    -d '{
        "snapshot_type": "Full",
        "snapshot_path": "./snapshot_file",
        "mem_file_path": "./mem_file"
    }'
```

Prerequisite: VM must be `Paused`.

Effects on success:
- State file contains serialized device/KVM state (bitcode format, 64-bit CRC)
- Memory file contains full guest RAM (for Full) or dirty pages only (for Diff)
- If dirty page tracking enabled, dirtied page bitmap is reset
- Files immediately available; block device contents flushed to host FS cache (not necessarily to disk)
- Vsock device is reset (VIRTIO_VSOCK_EVENT_TRANSPORT_RESET sent to guest)

### Step 3: Resume (optional, if VM should continue running)

```bash
curl --unix-socket /tmp/firecracker.socket -i \
    -X PATCH 'http://localhost/vm' \
    -H 'Content-Type: application/json' \
    -d '{"state": "Resumed"}'
```

### Loading a Snapshot (in a fresh Firecracker process)

**Endpoint:** `PUT /snapshot/load`

**SnapshotLoadParams:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `snapshot_path` | string | Yes | Path to microVM state file |
| `mem_backend` | object | One of `mem_backend` or `mem_file_path` | `{backend_type, backend_path}` |
| `mem_file_path` | string | One of (deprecated) | Path to memory file (File backend only) |
| `track_dirty_pages` | boolean | No | Enable KVM dirty page tracking for future diff snapshots |
| `enable_diff_snapshots` | boolean | No | **Deprecated** in v1.13 -- use `track_dirty_pages` |
| `resume_vm` | boolean | No | Auto-resume VM after successful load |
| `network_overrides` | array | No | Override TAP device names: `[{iface_id, host_dev_name}]` |
| `vsock_override` | object | No | Override vsock UDS path: `{uds_path}` |

**MemoryBackend types:**

| `backend_type` | `backend_path` meaning | Behavior |
|----------------|----------------------|----------|
| `File` | Path to memory file | Kernel handles page faults via `mmap(MAP_PRIVATE)` of the file |
| `Uffd` | Path to Unix Domain Socket | Userspace process handles page faults via userfaultfd |

```bash
curl --unix-socket /tmp/firecracker.socket -i \
    -X PUT 'http://localhost/snapshot/load' \
    -H 'Content-Type: application/json' \
    -d '{
        "snapshot_path": "./snapshot_file",
        "mem_backend": {
            "backend_path": "./mem_file",
            "backend_type": "File"
        },
        "track_dirty_pages": false,
        "resume_vm": true,
        "network_overrides": [
            {"iface_id": "eth0", "host_dev_name": "vmtap01"}
        ]
    }'
```

Prerequisite: Fresh Firecracker process. Only Logger and Metrics may be configured before this call.

Critical constraint: The memory file **must remain immutable** for the lifetime of the restored VM.
It backs guest memory reads via the page cache. External modification corrupts guest memory.

The `track_dirty_pages` setting is NOT saved in snapshots -- must be re-specified on each load
if diff snapshots are desired from the restored VM.

Source: `docs/snapshotting/snapshot-support.md`, `src/firecracker/swagger/firecracker.yaml`

---

## Snapshot Versioning and Compatibility

### Format

The microVM state file uses a binary format with this layout:

| Field | Bits | Description |
|-------|------|-------------|
| `magic_id` | 64 | Architecture-specific magic (`0x0710_1984_8664_0000` for x86_64, `0x0710_1984_AAAA_0000` for aarch64) |
| `version` | Variable | Snapshot data format version as `MAJOR.MINOR.PATCH` |
| `state` | Variable | Bitcode-encoded microVM state blob |
| `crc` | 64 | Optional CRC64 checksum of magic + version + state |

### Encoding

Firecracker uses [Serde](https://serde.rs) with the [bitcode](https://github.com/SoftbearStudios/bitcode)
encoder. Benefits: minimal size overhead, minimal CPU overhead. Downside: **bitcode does not support
backwards-compatible changes** -- essentially every change to the microVM state struct bumps the
MAJOR version.

### Current Version

**Snapshot format version 9.0.0** (on main, for the upcoming release after v1.15).

Source: `src/vmm/src/persist.rs` -- `pub const SNAPSHOT_VERSION: Version = Version::new(9, 0, 0);`

### Compatibility Rules

1. **Same Firecracker version**: always compatible
2. **Cross-version**: a Firecracker binary supports one fixed snapshot format version. It checks
   compatibility on load and rejects mismatches. Because bitcode encoding prohibits backward-compatible
   changes, **snapshots are generally NOT portable across Firecracker major versions**.
3. **Cross-architecture**: never compatible (different magic IDs)
4. **Cross-CPU model**: not supported. Intel <-> AMD will fail. Even different Intel generations
   may fail if exposed CPU features differ. Use CPU templates to mask features for portability.
5. **Cross-kernel**: officially unstable. Limited testing shows 5.10 -> 6.1 works on same instance types.
6. **Regeneration required**: many new features require regenerating snapshots (Intel AMX, Xsave,
   vsock port reuse, VMClock, MMDS imds_compat, etc.)

Source: `docs/snapshotting/versioning.md`

---

## Diff Snapshots (Incremental)

### Status: Developer Preview

As of v1.15.0, diff snapshots remain in **developer preview**. Full snapshots are GA since v1.13.0
(August 2025).

> "Diff snapshots are still in developer preview while we are diving deep into how the feature
> can be combined with guest_memfd support in Firecracker."

### How They Work

Two modes for determining which pages to include:

**Mode 1: KVM Dirty Page Tracking** (explicit opt-in)
- Enable via `track_dirty_pages: true` in `/machine-config` (pre-boot) or `/snapshot/load`
- KVM logs every page write, diff snapshot contains exactly dirtied pages
- Runtime overhead: CPU cycles spent by KVM accounting for dirty pages
- Negates benefits of huge pages (KVM unconditionally uses 4K granularity for dirty tracking)

**Mode 2: mincore(2) Approximation** (v1.13+, no tracking needed)
- Added in [#5274](https://github.com/firecracker-microvm/firecracker/pull/5274)
- Uses `mincore(2)` syscall to overapproximate dirty pages (pages in core = pages included)
- **Only works if swap is disabled** (mincore doesn't account for swapped pages)
- Results in larger memory files than Mode 1, but avoids tracking overhead
- The `enable_diff_snapshots` parameter was deprecated in v1.13; replaced by `track_dirty_pages`

### Merging Diff Snapshots

Diff snapshots produce sparse memory files. They are **not directly resumable** (exception: diff
snapshots of freshly booted VMs before any previous snapshot). They must be merged onto a base:

```bash
snapshot-editor edit-memory rebase \
    --memory-path path/to/base \
    --diff-path path/to/layer
```

Layers must be merged in creation order. After merging, the base becomes a resumable full snapshot.
The `rebase-snap` tool is deprecated; `snapshot-editor` is the current tool.

State files are NOT merged -- use the state file from the same API call as the last merged layer.

### Bug Fix (v1.14.2 / v1.15.0)

[#5705](https://github.com/firecracker-microvm/firecracker/pull/5705): Fixed a bug that caused
Firecracker to **corrupt memory files of differential snapshots** for VMs with multiple memory
slots. This affected VMs using memory hot-plugging or any x86 VMs with memory > 3GiB.

Source: `docs/snapshotting/snapshot-support.md`

---

## Memory Backend Options

### File Backend (MAP_PRIVATE)

Default and simplest. On snapshot load, Firecracker creates a `MAP_PRIVATE` mapping of the memory
file. Page faults are handled by the kernel, loading pages on demand from the file. Copy-on-write:
writes go to anonymous memory. The memory file must remain accessible and immutable for the VM's
lifetime.

Characteristics:
- Fast restore (just mmap, no bulk copy)
- Runtime on-demand page loading (latency on first access per page)
- Anonymous memory grows as guest writes (COW divergence)
- Memory file must stay on disk

### UFFD Backend (Userfaultfd)

Advanced option for custom page fault handling. The orchestrator provides a userspace process
that handles page faults via `userfaultfd(2)`.

Flow:
1. Page fault handler process binds/listens on a UDS
2. `PUT /snapshot/load` with `backend_type: "Uffd"` and `backend_path: <UDS path>`
3. Firecracker creates userfault FD, registers guest memory regions
4. Firecracker sends UFFD FD + memory layout (region dimensions, page sizes) to handler via UDS
5. Handler privately mmaps the memory file
6. On page fault: handler receives event, issues `UFFDIO_COPY` to load pages

Use cases:
- Lazy loading from remote storage (S3, NFS)
- Prefetching based on access patterns
- Custom page prioritization
- Integration with the balloon device (`UFFD_EVENT_REMOVE` for madvise'd pages)

Caveat: if the handler crashes, Firecracker hangs forever waiting for pages. No built-in timeout.

UFFD creation differs by kernel:
- Kernel 5.10: `userfaultfd` syscall
- Kernel 6.1+: `/dev/userfaultfd` device (file permission managed)

Source: `docs/snapshotting/handling-page-faults-on-snapshot-resume.md`

### What About memfd?

Firecracker uses `memfd` for fresh (non-snapshot) VMs -- guest memory is anonymous/memfd-backed.
There is no explicit memfd option for snapshot restore. The `File` backend uses `mmap(MAP_PRIVATE)`
of a regular file. Firecracker does not support transparent huge pages (THP) because Linux does not
offer a way to dynamically enable THP for memfd regions, and UFFD does not integrate with THP.

---

## Huge Pages

Firecracker supports **2MiB hugetlbfs** pages for guest memory (v1.12+).

### Configuration

```bash
curl --unix-socket /tmp/firecracker.socket -i \
    -X PUT 'http://localhost/machine-config' \
    -H 'Content-Type: application/json' \
    -d '{
        "vcpu_count": 2,
        "mem_size_mib": 1024,
        "huge_pages": "2M"
    }'
```

Enum values: `None` (default) or `2M`.

### Benefits

- Less TLB contention
- Fewer KVM exits to rebuild extended page tables post-snapshot-restore
- Boot time improvement up to **50%** (measured by Firecracker's boot time tests)

### Requirements and Limitations

- Host must have pre-allocated 2M hugetlbfs pool (`/proc/sys/vm/nr_hugepages`)
- Firecracker uses `MAP_NORESERVE` -- pages claimed on demand, insufficient pool = SIGBUS
- `mem_size_mib` must be a multiple of 2
- Snapshots of hugepage VMs can **only** be restored via UFFD backend (not File)
- Dirty page tracking negates hugepage benefits (KVM forces 4K granularity)
- Traditional balloon cannot reclaim hugepage-backed RSS (can still restrict guest memory usage)

Source: `docs/hugepages.md`

---

## Recent Releases (v1.12 through v1.15)

### v1.15.0 (2026-03-09) -- Latest Stable

- **VMClock device** ([#5510](https://github.com/firecracker-microvm/firecracker/pull/5510)): New device
  for efficient application clock sync inside VMs. Supports `vm_generation_counter` that changes
  atomically on snapshot resume. Userspace can `mmap()` the vmclock_abi struct and call `poll()` to
  detect snapshot restores. Merged in Linux kernel v7.0 (backports available for 5.10, 6.1).
  Uses one extra GSI, reducing max VirtIO devices to 92 (aarch64) / 17 (x86).
- **Intel Granite Rapids** support ([#5574](https://github.com/firecracker-microvm/firecracker/pull/5574))
- **Fixed**: diff snapshot corruption for VMs with >3GiB memory or memory hotplug ([#5705](https://github.com/firecracker-microvm/firecracker/pull/5705))
- **Fixed**: vsock local port reuse across snapshot restore ([#5688](https://github.com/firecracker-microvm/firecracker/pull/5688)) -- requires snapshot regeneration

### v1.14.0 (2025-12-17)

- **virtio-pmem** ([#5463](https://github.com/firecracker-microvm/firecracker/pull/5463)): Persistent
  memory device backed by host file. Guest sees `/dev/pmem0`. Supports DAX (direct access, no page cache).
  Can be used as rootfs. Works with snapshots (config persisted, backing file must be at same path).
- **virtio-mem** ([#5534](https://github.com/firecracker-microvm/firecracker/pull/5534)): Memory
  hot-plugging. Define max hotpluggable pool at boot, plug/unplug at runtime via PATCH. Slot-based
  protection prevents malicious guest access.
- **Balloon free page reporting and hinting** ([#5491](https://github.com/firecracker-microvm/firecracker/pull/5491)):
  Reporting is GA, hinting is developer preview.
- **Serial endpoint** ([#5350](https://github.com/firecracker-microvm/firecracker/pull/5350)):
  `PUT /serial` to redirect serial output to file instead of stdout.
- **Balloon stats for kernel >= 6.12** ([#5516](https://github.com/firecracker-microvm/firecracker/pull/5516)):
  OOM kills, allocation stalls, memory scan/reclaim metrics.
- **Fixed**: KVM_KVMCLOCK_CTRL watchdog soft lockup on snapshot restore ([#5494](https://github.com/firecracker-microvm/firecracker/pull/5494))

### v1.13.0 (2025-08-28)

- **Snapshots promoted to GA** ([#5165](https://github.com/firecracker-microvm/firecracker/pull/5165)):
  Full snapshots are generally available. Diff snapshots remain developer preview.
- **mincore-based diff snapshots** ([#5274](https://github.com/firecracker-microvm/firecracker/pull/5274)):
  Take diff snapshots without enabling dirty page tracking (swap must be disabled).
- **PCI support** ([#5364](https://github.com/firecracker-microvm/firecracker/pull/5364)):
  Optional `--enable-pci` flag. When enabled, all VirtIO devices use PCI transport instead of MMIO.
- **MMDS IMDS compatibility** ([#5310](https://github.com/firecracker-microvm/firecracker/pull/5310),
  [#5290](https://github.com/firecracker-microvm/firecracker/pull/5290)): EC2-compatible session token
  headers, `imds_compat` flag, V1 session support for migration to V2.
- **PVTime on ARM** ([#5139](https://github.com/firecracker-microvm/firecracker/pull/5139)): Steal time support.
- `enable_diff_snapshots` deprecated in favor of `track_dirty_pages`.

### v1.12.0 (2025-05-07)

- **PVH boot mode** for x86 ([#5048](https://github.com/firecracker-microvm/firecracker/pull/5048)): Linux 5.0+ with CONFIG_PVH=y.
- **Intel AMX** support ([#5065](https://github.com/firecracker-microvm/firecracker/pull/5065)): Uses Xsave for snapshot/restore. Requires snapshot regeneration.
- **TAP device override on restore** ([#4731](https://github.com/firecracker-microvm/firecracker/pull/4731)):
  `network_overrides` parameter in `/snapshot/load`.
- **Intel Sapphire Rapids and ARM Graviton4** as supported platforms.

### Unreleased (main branch)

- **Vsock UDS path override on restore** ([#5323](https://github.com/firecracker-microvm/firecracker/pull/5323))
- **Fixed**: virtio-rng entropy request capped to 64 KiB ([#5762](https://github.com/firecracker-microvm/firecracker/pull/5762))
  -- previously guest could cause excessive host memory allocation
- **Fixed**: VMGenID HID alignment with upstream Linux ([#5760](https://github.com/firecracker-microvm/firecracker/pull/5760))
  -- driver wasn't binding correctly pre-Linux 6.10
- **Fixed**: virtio-mem plug/unplug KVM slot boundary bug ([#5793](https://github.com/firecracker-microvm/firecracker/pull/5793))

Source: GitHub Releases, `CHANGELOG.md`

---

## MMDS (MicroVM Metadata Service)

### What It Is

A mutable JSON data store for host-to-guest communication. The guest queries it like AWS EC2 IMDS
(HTTP GET to a link-local IP). The host populates/updates it via the Firecracker API socket.

### Architecture

- Not reachable by default -- must be explicitly bound to a network interface
- Implemented in Firecracker's network device model (no separate process)
- Guest sends HTTP to a configurable IPv4 address (default `169.254.169.254`, customizable)
- Packets intercepted by Firecracker before reaching the TAP device
- MMDS traffic never leaves the VM (no network namespace escape)

### Configuration

```bash
# 1. Attach network interface
curl --unix-socket $SOCK -X PUT 'http://localhost/network-interfaces/eth0' \
    -H 'Content-Type: application/json' \
    -d '{"iface_id": "eth0", "guest_mac": "AA:FC:00:00:00:01", "host_dev_name": "tap0"}'

# 2. Configure MMDS
curl --unix-socket $SOCK -X PUT 'http://localhost/mmds/config' \
    -H 'Content-Type: application/json' \
    -d '{
        "network_interfaces": ["eth0"],
        "version": "V2",
        "ipv4_address": "169.254.170.2",
        "imds_compat": true
    }'

# 3. Populate data store
curl --unix-socket $SOCK -X PUT 'http://localhost/mmds' \
    -H 'Content-Type: application/json' \
    -d '{"job_id": "abc123", "repo_url": "https://git.example.com/repo.git"}'
```

### Versions

- **V1** (deprecated, removed in next major): Simple GET requests, no authentication
- **V2**: Session-oriented, requires PUT to `/latest/api/token` with TTL header to get a token,
  then `X-metadata-token` header on subsequent GETs. Compatible with EC2 IMDSv2 headers.

### Guest-Side Usage

```bash
# In guest: add route to MMDS IP
ip route add 169.254.170.2 dev eth0

# V2: get token
TOKEN=$(curl -X PUT "http://169.254.170.2/latest/api/token" \
    -H "X-metadata-token-ttl-seconds: 21600")

# V2: query data
curl -s "http://169.254.170.2/job_id" -H "X-metadata-token: $TOKEN"
```

### Snapshot Behavior

- MMDS config (version, IP, network interfaces, imds_compat) IS persisted in snapshots
- MMDS **data store is NOT persisted** -- deliberately cleared to avoid leaking VM-specific info
- Must repopulate data store after snapshot restore

### Relevance for CI

MMDS is the ideal mechanism for injecting per-job configuration (repo URL, commit SHA, job ID,
secrets) into a VM restored from snapshot. The data store is intentionally cleared on restore,
so each clone gets fresh config without leaking previous job data.

Source: `docs/mmds/mmds-user-guide.md`

---

## Balloon Device

### Overview

A virtio balloon device for dynamic guest memory management. The host sets a target size (in MiB),
and the guest driver inflates (allocates guest pages, reports to host which calls `madvise(MADV_DONTNEED)`)
or deflates (returns pages to guest).

### API

```bash
# Install (pre-boot only)
curl --unix-socket $SOCK -X PUT 'http://localhost/balloon' \
    -H 'Content-Type: application/json' \
    -d '{
        "amount_mib": 0,
        "deflate_on_oom": true,
        "stats_polling_interval_s": 1,
        "free_page_reporting": true
    }'

# Resize (any time)
curl --unix-socket $SOCK -X PATCH 'http://localhost/balloon' \
    -H 'Content-Type: application/json' \
    -d '{"amount_mib": 256}'

# Get statistics
curl --unix-socket $SOCK -X GET 'http://localhost/balloon/statistics'
```

### Statistics (from guest driver)

Standard virtio balloon stats: swap_in/out, major/minor faults, free/total/available memory, disk caches,
hugetlb allocations/failures. Since Linux 6.12: OOM kills, allocation stalls, async/direct scan/reclaim.

### Free Page Reporting (v1.14+, GA)

Guest continuously reports unused pages to host. Host calls `madvise(MADV_DONTNEED)` to reduce RSS.
Enabled pre-boot with `free_page_reporting: true`. Requires `CONFIG_PAGE_REPORTING` in guest kernel.
Minimum page order configurable via guest kernel module parameter `page_reporting_order`.

### Free Page Hinting (v1.14+, Developer Preview)

Host-initiated memory reclaim. Three endpoints:
- `PATCH /balloon/hinting/start` -- start a hinting run (`acknowledge_on_stop: true` for auto-stop)
- `GET /balloon/hinting/status` -- poll progress
- `PATCH /balloon/hinting/stop` -- stop and release pages back to guest

Average time: ~200ms for a 1GB VM.

### Security Model

Balloon is paravirtualized -- requires guest driver cooperation. A compromised driver can:
- Report incorrect statistics
- Refuse to inflate
- Flood host with UFFD_EVENT_REMOVE (mitigate with cgroup limits)

The balloon **cannot** leak memory between Firecracker processes (MAP_PRIVATE + MAP_ANONYMOUS,
MADV_DONTNEED zeroes on re-access).

### Limitations with Huge Pages

Traditional balloon reports at 4K granularity -- cannot reclaim hugepage-backed RSS. Can still
restrict guest memory usage (guest sees less available memory).

Source: `docs/ballooning.md`

---

## Entropy Device (virtio-rng)

### Purpose

Provides high-quality randomness to the guest via the virtio-rng protocol. Guest kernel uses it
as an additional entropy source. Exposes `/dev/hwrng` character device in guest.

### Configuration

```bash
curl --unix-socket $SOCK -X PUT 'http://localhost/entropy' \
    -H 'Content-Type: application/json' \
    -d '{
        "rate_limiter": {
            "bandwidth": {
                "size": 10000,
                "one_time_burst": 0,
                "refill_time": 100
            }
        }
    }'
```

Only parameter: optional rate limiter (bandwidth in bytes). No rate limiter = unlimited.

### Implementation

Host side uses [`aws-lc-rs`](https://docs.rs/aws-lc-rs/latest/aws_lc_rs/) wrapping the
[AWS-LC cryptographic library](https://github.com/aws/aws-lc) for random byte generation.

### Guest Requirements

Kernel config: `CONFIG_HW_RANDOM_VIRTIO` (depends on `CONFIG_HW_RANDOM` and `CONFIG_VIRTIO`).

### Security Fix (unreleased)

[#5762](https://github.com/firecracker-microvm/firecracker/pull/5762): Caps per-request entropy
to 64 KiB. Previously, a guest could construct a descriptor chain causing Firecracker to allocate
excessive host memory.

### CI Relevance

For snapshot-based CI, virtio-rng is essential. Each restored clone starts with identical CSPRNG
state. The entropy device provides fresh randomness to reseed the guest kernel's entropy pool.
Combined with VMGenID (automatic CSPRNG reseed on restore), this ensures cryptographic uniqueness
across clones.

Source: `docs/entropy.md`

---

## New in v1.14: virtio-pmem, virtio-mem, PCI

### virtio-pmem

Host file mapped as persistent memory in guest. Guest sees `/dev/pmem0`, `/dev/pmem1`, etc.

Key properties:
- Direct access (DAX) -- guest reads host pages directly, no page cache duplication
- Can serve as root device (`root_device: true`)
- Snapshot-compatible (config persisted, backing file must be at same path)
- Guest memory page faults on first access (use huge pages for fewer faults)
- `MAP_SHARED` mapping -- host can page it out (use `vmtouch` to pin)

**CI relevance**: Could replace virtio-block for read-heavy rootfs. With DAX, saves ~24MB RSS
for a 128MB VM (96MB vs 120MB). The shared mapping means multiple VMs could potentially share
the same rootfs file's page cache (though Firecracker warns about side-channel risk).

### virtio-mem

Dynamic memory hotplugging with fine-grained control:
- Define pool at boot (`total_size_mib`, `block_size_mib` default 2, `slot_size_mib` default 128)
- Plug/unplug at runtime via `PATCH /hotplug/memory` with `requested_size_mib`
- Unplugged slots are KVM-protected (guest cannot access)
- Async operation -- guest driver plugs/unplugs blocks incrementally

**CI relevance**: Could allocate minimal boot memory, then hotplug based on job requirements.
Avoids over-provisioning memory for lightweight jobs while allowing heavy builds to scale up.

### PCI Support (v1.13+)

Optional `--enable-pci` flag at process launch. When enabled, all VirtIO devices use PCI transport
instead of MMIO. No API change -- same device configuration, different transport.

Source: `docs/pmem.md`, `docs/memory-hotplug.md`, release notes

---

## Snapshot Security -- Entropy and Uniqueness

### The Problem

Resuming the same snapshot in multiple VMs = identical:
- Kernel CSPRNG state (`/dev/urandom`, `getrandom()`)
- `/proc/sys/kernel/random/boot_id`
- Systemd random seed (`/var/lib/systemd/random-seed`)
- Any userspace PRNG state (OpenSSL pools, V8 Math.random seed, etc.)

### Mitigations (in order of preference)

**1. VMGenID (automatic, kernel >= 5.18 for ACPI, >= 6.10 for DeviceTree)**

Firecracker always enables VMGenID. On snapshot resume, writes a new 16-byte random identifier
and injects an interrupt. Linux reseeds its CSPRNG from this value.

Race window: between vCPU resume and kernel handling the VMGenID interrupt, `getrandom()` may
return stale values. For paranoid users, follow up with manual reseed.

**2. VMClock (v1.15+, kernel v7.0+)**

New device that exposes `vm_generation_counter` in a mmap'd struct. Userspace can:
- `mmap()` the vmclock_abi and check `vm_generation_counter` changes
- `poll()` on the VMClock device FD for event-driven notification

This is the recommended mechanism for userspace PRNG libraries to detect and handle restores.

**3. virtio-rng**

Provides ongoing entropy from host. Guest kernel requests random bytes at its own cadence.
Not immediate -- kernel decides when to request.

**4. Manual reseed (pre-5.18 kernels or maximum safety)**

```c
// In guest, after restore:
int fd = open("/dev/urandom", O_RDWR);
struct { int entropy_count; int buf_size; char buf[32]; } entropy;
entropy.entropy_count = 256;  // bits
entropy.buf_size = 32;
getrandom(entropy.buf, 32, GRND_RANDOM);  // from hwrng
ioctl(fd, RNDADDENTROPY, &entropy);
ioctl(fd, RNDRESEEDCRNG, NULL);  // force CSPRNG reseed
```

**5. Pre-snapshot cleanup**

- Delete `/var/lib/systemd/random-seed`
- Bind-mount unique file over `/proc/sys/kernel/random/boot_id`

### What VMGenID Does NOT Fix

Userspace cached state: OpenSSL entropy pools, V8 Math.random internal state, language runtime
PRNGs, cached UUIDs, session tokens. Applications must be designed to detect snapshot restore
(via VMClock poll) or avoid caching entropy across suspend points.

Source: `docs/snapshotting/random-for-clones.md`, `docs/snapshotting/snapshot-support.md`

---

## Network Connectivity for Clones

### The Two Problems

1. **TAP name collision**: all clones from the same snapshot expect the same TAP device name
2. **IP collision**: all clones resume with identical guest IP configuration

### Solutions

**Problem 1 -- TAP names:**
- Option A: Jailer's `--netns` parameter puts each clone in a separate network namespace.
  Multiple TAP devices with the same name are fine across namespaces.
- Option B (v1.12+): `network_overrides` in `/snapshot/load` to remap TAP device names
  without network namespaces.

**Problem 2 -- Guest IPs:**
- Use `iptables NAT` (MASQUERADE) per namespace to remap guest IPs
- Or reconfigure guest networking post-restore via vsock/MMDS command

### Network Override Example

```bash
curl --unix-socket $SOCK -X PUT 'http://localhost/snapshot/load' \
    -H 'Content-Type: application/json' \
    -d '{
        "snapshot_path": "./snapshot_file",
        "mem_backend": {"backend_path": "./mem_file", "backend_type": "File"},
        "network_overrides": [{"iface_id": "eth0", "host_dev_name": "vmtap42"}]
    }'
```

### Vsock Override (unreleased, on main)

```bash
"vsock_override": {"uds_path": "/tmp/vsock-job-42.sock"}
```

Source: `docs/snapshotting/network-for-clones.md`

---

## CI-Relevant Analysis

### Snapshot Lifecycle for CI (hundreds of VMs/hour)

For a CI system creating/destroying hundreds of VMs per hour, the key decision is whether to use
snapshots at all, or cold boot each VM.

**Snapshot approach:**
```
One-time: boot golden VM -> warm caches -> snapshot -> terminate
Per-job:  firecracker --no-api --config-file=<json> (or API: load snapshot -> resume)
          MMDS: inject {job_id, repo_url, commit_sha}
          Job runs -> VM exits
          Destroy firecracker process + cleanup TAP/cgroup
```

**Cold boot approach (forge-metal's current design):**
```
Per-job:  ZFS clone golden-zvol@ready -> /dev/zvol/pool/ci/job-abc   (~1.7ms)
          firecracker --drive /dev/zvol/pool/ci/job-abc              (~3s cold boot)
          Job runs -> VM exits
          zfs destroy pool/ci/job-abc
```

### Snapshot vs ZFS Clone -- Quantitative Comparison

| Metric | Snapshot Restore | ZFS Clone + Cold Boot |
|--------|-----------------|----------------------|
| VM ready time | ~28ms (mmap + KVM restore) | ~3s (kernel boot + init) |
| Memory overhead per VM | O(pages touched) via COW | O(pages touched) via COW |
| Disk overhead per snapshot | Full guest RAM on host disk | Zero (clone is metadata) |
| Disk COW | mmap MAP_PRIVATE (anonymous pages) | ZFS block-level COW |
| Per-VM disk state | Managed separately (device-mapper/overlay) | Unified in ZFS zvol |
| CPU model constraint | Locked to creation CPU model | None |
| Snapshot portability | Same CPU, same kernel (effectively) | `zfs send/recv` anywhere |
| Entropy handling | VMGenID + virtio-rng + manual | Not needed (no process state) |
| Network setup | TAP per namespace or override | TAP per namespace |
| Process state | Fully restored (Node.js running, V8 warm) | Cold start (~50ms for node) |

### What Snapshots Buy for Node.js CI

The 3-second cold boot delta breaks down as:
- ~1.1s kernel + init
- ~50ms Node.js process start
- ~1-2s npm module loading / V8 JIT warmup
- ~500ms application-specific warmup

With snapshots, all of this is skipped. The question is whether 3s matters when the CI job
itself takes 30-300s.

### Recommended Hybrid Approach

1. **Phase 1 (current)**: ZFS zvol clones + cold boot Firecracker. Simple, no snapshot complexity.
   3s boot overhead is negligible for most CI jobs.

2. **Phase 2 (if boot time matters)**: Create one snapshot per golden image update. Use `File`
   backend for restore (simplest). Use MMDS for per-job config injection. Use `network_overrides`
   to avoid namespace complexity. Use virtio-rng + VMGenID for entropy.

3. **Phase 3 (if memory matters at scale)**: UFFD backend for lazy loading from shared storage.
   Balloon with free_page_reporting for memory reclaim. virtio-mem for right-sizing per job type.

### Key Operational Considerations

- **cgroups v2 required**: snapshot restore has high latency on cgroups v1
  ([#2129](https://github.com/firecracker-microvm/firecracker/issues/2129))
- **Snapshot regeneration cadence**: every Firecracker version bump likely requires new snapshots
  (bitcode format changes = MAJOR version bump)
- **Memory file management**: each snapshot's memory file must persist on disk for the lifetime
  of all VMs using it. For 512MB guest RAM with 100 concurrent jobs, that's 512MB on disk (shared)
  plus COW anonymous pages per VM.
- **MMDS data store clears on restore**: by design. Repopulate per-job config after each restore.
- **Vsock resets on snapshot create**: existing connections close. Listen sockets survive.
- **Diff snapshots are developer preview**: don't rely on them for production CI.

### Features Most Useful for CI at Scale

| Feature | Version | Relevance |
|---------|---------|-----------|
| Full snapshots (GA) | v1.13+ | Core fast-boot mechanism |
| network_overrides on restore | v1.12+ | Avoids per-VM network namespace overhead |
| vsock_override on restore | main (unreleased) | Per-VM vsock socket path |
| MMDS V2 | v1.13+ | Secure per-job config injection |
| Free page reporting | v1.14+ | Memory reclaim without explicit balloon sizing |
| virtio-mem hotplug | v1.14+ | Right-size memory per job type |
| VMClock | v1.15+ | Userspace snapshot-restore detection |
| virtio-pmem | v1.14+ | DAX rootfs, shared page cache potential |
| Balloon stats (6.12+) | v1.14+ | OOM detection, memory pressure monitoring |

---

## Primary Sources

| Document | URL |
|----------|-----|
| Swagger/OpenAPI spec | `src/firecracker/swagger/firecracker.yaml` (API v1.16.0-dev) |
| Snapshot support doc | `docs/snapshotting/snapshot-support.md` |
| Snapshot versioning | `docs/snapshotting/versioning.md` |
| UFFD page fault handling | `docs/snapshotting/handling-page-faults-on-snapshot-resume.md` |
| Random for clones | `docs/snapshotting/random-for-clones.md` |
| Network for clones | `docs/snapshotting/network-for-clones.md` |
| MMDS user guide | `docs/mmds/mmds-user-guide.md` |
| Balloon device | `docs/ballooning.md` |
| Entropy device | `docs/entropy.md` |
| Huge pages | `docs/hugepages.md` |
| virtio-pmem | `docs/pmem.md` |
| Memory hotplug | `docs/memory-hotplug.md` |
| Changelog | `CHANGELOG.md` |
| GitHub Releases | https://github.com/firecracker-microvm/firecracker/releases |

All URLs are relative to `https://github.com/firecracker-microvm/firecracker/blob/main/` unless
otherwise specified. Research conducted 2026-03-29 against Firecracker main branch (API v1.16.0-dev),
with latest stable release being v1.15.0.
