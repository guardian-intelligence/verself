# Firecracker Improvements Report

Audit of the current Firecracker integration across `internal/firecracker/`,
`internal/ci/`, `cmd/forgevm-init/`, `scripts/`, and `ansible/roles/firecracker/`.

Organized by impact. Each section states what we do today, what the improvement
is, and why it matters.

---

## 1. Boot Time: Snapshots Instead of Cold Boot

**Today:** Every VM cold-boots. The orchestrator starts the jailer, waits for
the API socket, makes 5 sequential PUT calls (metrics, boot-source, drive,
machine-config, network), then issues InstanceStart. This takes ~3s.

**Improvement:** Use Firecracker's snapshot/restore. Boot a template VM once,
pause it with `CreateSnapshot` (full memory + device state), then restore
per-job with `LoadSnapshot`. Restore is ~125ms — a 24x reduction in boot
latency.

**How it works:**

```
One-time (on golden image refresh):
  1. Boot VM from golden zvol with template networking
  2. Let forgevm-init mount VFS, configure loopback, reach idle
  3. Pause VM: PUT /snapshot/create { mem_file, snapshot_path }
  4. Store snapshot artifacts alongside golden zvol

Per-job:
  1. Clone golden zvol (1.7ms, unchanged)
  2. Start jailer
  3. PUT /snapshot/load { mem_file, snapshot_path, resume_vm: true }
  4. VM resumes mid-execution — init detects "restore" and reads job config
```

**Complications:**

- forgevm-init needs a restore-aware path: after resume, re-read job config
  (wasn't written at snapshot time), reconfigure networking (new TAP/IP), and
  start job phases. This means the init can't just run linearly — it needs to
  checkpoint after basic setup and wait for a "go" signal.
- Snapshot files must be on the same filesystem accessible to the jailer chroot.
  The memory file can be large (equal to VM memory: 2 GiB default).
- Network interface gets a new TAP on restore, but the guest's IP stack was
  configured at snapshot time. Either: (a) snapshot before network config and
  let init configure on resume, or (b) use MMDS to signal new IP config.
- Clock skew: the guest's clock freezes at snapshot time. forgevm-init should
  call `clock_settime` on resume (or accept that CI jobs don't care about wall
  clock accuracy, which is probably true).

**When to do this:** After the current parallelism work is proven. Snapshots
are orthogonal to concurrent VMs — you need per-job networking either way.
Boot time is 12% of a 25s job. Worth doing, but not the next priority.

---

## 2. Job Config Injection: MMDS Instead of Mount/Unmount

**Now:** The orchestrator publishes per-job config through Firecracker MMDS
before `InstanceStart`, and `forgevm-init` fetches it during boot. The old
mount/write/unmount path has been removed from the live runtime.

**Former path:** The orchestrator mounted the zvol clone to a temp directory, wrote
`/etc/ci/job.json`, unmounted, then booted. This required:
- `mount` syscall (needs the device node to exist, polled with `waitForDevice`)
- Filesystem write
- `umount` syscall
- The mount point is exclusive — if the device is busy, it blocks

`orchestrator.go:193-206`:
```go
mountDir, mountErr := mountZvol(ctx, devPath)
writeJobConfig(mountDir, job)
unmount(ctx, mountDir)
```

**Current implementation:** Use Firecracker's MMDS (MicroVM Metadata Service).
The host configures MMDS and writes JSON to the MMDS data store via the API
socket *before* the VM boots. The guest fetches it by hitting the MMDS IP
(typically `169.254.169.254`) and then removes that route before running the
untrusted workload.

```
Host side (pre-boot):
  PUT /mmds/config { "version": "V2", "network_interfaces": ["eth0"] }
  PUT /mmds { "forge_metal": { "schema_version": 1, "job": {...} } }

Guest side (forgevm-init):
  PUT  /latest/api/token
  GET  /forge_metal with X-metadata-token
  ip route del 169.254.169.254/32 dev eth0
```

**Benefits:**
- Eliminates mount/unmount cycle entirely
- No filesystem write to the zvol before boot (the golden clone stays pristine
  until the job actually writes)
- Required for snapshot/restore (you can't mount the zvol and write config if
  restoring from a memory snapshot)
- MMDS traffic stays in-process (Firecracker's "Dumbo" TCP stack) — never
  touches the TAP or host network

**Current caveat:** There is still a temporary guest-side file fallback for
`/etc/ci/job.json`, but the host no longer writes that file in the live path.

---

## 3. Host-Guest Communication: vsock Instead of Serial Parsing

**Today:** The only communication channel from guest to host is the serial
console. The guest's stdout/stderr goes through `virtio-serial` → host reads
it via `jailerCmd.StdoutPipe()`. The exit code is extracted by parsing the
string `FORGEVM_EXIT_CODE=N` from the log buffer.

`orchestrator.go:487-498`:
```go
func parseGuestExitCode(logs string) int {
    const marker = "FORGEVM_EXIT_CODE="
    for _, line := range strings.Split(logs, "\n") {
        // ...
    }
    return -1
}
```

`forgevm-init/main.go:88`:
```go
fmt.Fprintf(os.Stdout, "FORGEVM_EXIT_CODE=%d\n", exitCode)
```

**Problems with this:**
- Fragile: if the job itself prints `FORGEVM_EXIT_CODE=0` in its output, we
  parse the wrong value. (Unlikely but possible.)
- 10MB log cap: `orchestrator.go:276` caps the log buffer. If a job produces
  more than 10MB of output, the exit code marker at the end gets dropped,
  and we return -1.
- No streaming: logs are only available after the VM exits. No live progress.
- No structured data: timing breakdowns (how long was prepare vs run?) are
  not reported to the host.

**Improvement:** Use `virtio-vsock` for a structured bidirectional channel.

```
Host: listen on AF_VSOCK, CID=guest, port=1024
Guest: connect to AF_VSOCK, CID=2 (host), port=1024
Protocol: newline-delimited JSON messages

Guest → Host:
  {"type":"phase_start","phase":"prepare","timestamp":1234}
  {"type":"phase_end","phase":"prepare","exit_code":0,"duration_ms":3200}
  {"type":"phase_start","phase":"run","timestamp":5678}
  {"type":"log","stream":"stdout","data":"PASS src/app.test.ts"}
  {"type":"phase_end","phase":"run","exit_code":0,"duration_ms":18700}
  {"type":"done","exit_code":0}
```

**Benefits:**
- Structured exit code delivery (no string parsing)
- Per-phase timing from the guest side
- Live log streaming (host can forward to ClickHouse in real-time)
- Host can send cancellation signals back to guest
- Serial console stays available for kernel panics and debugging

**Complications:**
- Firecracker must be configured with a vsock device: `PUT /vsock` with
  `guest_cid` and `uds_path` (Unix domain socket on host).
- The host orchestrator needs a goroutine to accept vsock connections
  (via the UDS that Firecracker exposes) and parse the protocol.
- forgevm-init binary grows: needs vsock client code. Still small since
  `AF_VSOCK` is just `syscall.Socket(AF_VSOCK, SOCK_STREAM, 0)` — no
  external dependencies.
- Backwards compatibility: keep serial console parsing as fallback for
  when vsock connection fails (kernel panic, guest crash).

**When to do this:** After MMDS. vsock is the highest-value guest communication
improvement but has the most moving parts. MMDS is simpler and unblocks
snapshots.

---

## 4. Resource Controls: CPU Pinning, IO Limits, Cgroups

**Today:** VMs get `vcpu_count` and `mem_size_mib` but no further resource
isolation. All VMs share the same physical cores via the kernel scheduler.
No I/O rate limiting. No cgroup constraints via the jailer.

**4a. CPU Pinning**

Firecracker supports `PUT /machine-config` with `cpu_template` for feature
masking, but actual core pinning is done via cgroups. The jailer accepts
`--cgroup cpuset.cpus=N-M` to pin vCPUs to specific physical cores.

Without pinning, 8 concurrent 2-vCPU VMs all compete for the same 32 cores.
The kernel scheduler handles this, but context-switching between VMs adds
latency variance. For CI, deterministic timing matters (flaky "timeout" tests
become less flaky with pinned cores).

```
Slot 0: cpuset.cpus=0-1
Slot 1: cpuset.cpus=2-3
...
Slot 15: cpuset.cpus=30-31
```

This pairs naturally with the network slot allocator — each slot gets a
network /30 AND a CPU set.

**4b. I/O Rate Limiting**

Firecracker supports per-drive and per-network rate limiters via the API:

```json
PUT /drives/rootfs {
  "rate_limiter": {
    "bandwidth": { "size": 104857600, "refill_time": 1000 },
    "ops":       { "size": 1000, "refill_time": 1000 }
  }
}
```

Without this, a single runaway `npm install` downloading hundreds of packages
can saturate the NVMe and starve other VMs. Rate limiting per-VM ensures
fair sharing.

**4c. Cgroup Integration**

The jailer supports `--cgroup` flags for memory, CPU, and I/O limits:
```
--cgroup memory.limit_in_bytes=2147483648
--cgroup cpuset.cpus=0-1
--cgroup cpuset.mems=0
```

We currently pass none. Adding memory limits via cgroups provides a hard
OOM boundary — if a VM exceeds its allocation, the cgroup kills it rather
than pressuring the host. This is especially important for parallel VMs
where one rogue process shouldn't affect others.

**When to do this:** CPU pinning should be added alongside the concurrency
work (it's a natural extension of the slot allocator). I/O rate limiting
and cgroup memory limits can follow.

---

## 5. PID Namespace Isolation

**Today:** The jailer runs without `--new-pid-ns`. Comment in
`orchestrator.go:411-415`:

```go
// No --new-pid-ns in the current runtime. The PID namespace adds a
// fork that makes jailerCmd.Wait() ambiguous about which process
// exited.
```

**Why this matters:** Without a PID namespace, the Firecracker process can
see host PIDs via `/proc` (though it's chrooted, so `/proc` isn't mounted
in the jail — this is defense in depth, not a live exploit). More importantly,
if the jailer process crashes without cleaning up, zombie Firecracker
processes retain visibility into the host PID space.

**The Wait() problem and fix:** With `--new-pid-ns`, the jailer forks. The
parent exits immediately, the child (PID 1 in the new namespace) execs
Firecracker. `jailerCmd.Wait()` returns when the *parent* exits, not when
Firecracker exits. Fix: after `Wait()` returns, read the child PID from the
jailer's stdout or use the API socket becoming un-connectable as the real
"VM exited" signal.

Alternative: use `--new-pid-ns` with the jailer's `--daemonize` flag, then
poll the API socket or the jailer PID file for termination.

**When to do this:** After vsock. The current approach works and the VM
already provides kernel-level PID isolation. This is hardening, not
functionality.

---

## 6. Guest Rootfs: Sizing, Compression, and Versioning

**Today:** The golden zvol is a fixed 4 GiB, created by `dd`-ing `rootfs.ext4`
onto it. The ext4 image is also 4 GiB (created by `mke2fs` in
`build-guest-rootfs.sh:156`). Actual content is ~1-2 GiB. The remaining
space is available for the job to write into.

**6a. Right-size the zvol**

ZFS zvols with `volblocksize=16K` and `compression=lz4` already compress
well, but the 4 GiB fixed size means every clone reserves 4 GiB of metadata
space in the ZFS pool even before COW writes. For 16 concurrent VMs, that's
64 GiB of zvol namespace, even though most jobs dirty <500 MiB.

ZFS zvol thin provisioning handles this: the clone only consumes space for
blocks actually written. The 4 GiB size is a *logical* maximum, not a
physical reservation. So this is not actually a problem in practice — ZFS
already does the right thing. No change needed.

**6b. Image versioning**

Currently tracked by rootfs SHA256 checksum in
`/var/lib/ci/golden-zvol.rootfs.sha256`. The Ansible role compares checksums
to decide whether to refresh. This works but has no history — you can't
roll back to a previous golden image.

Improvement: tag golden snapshots with a version derived from the build
inputs (Alpine version + Firecracker version + forgevm-init commit SHA).
Keep the last N snapshots for rollback:

```
benchpool/golden-zvol@v3.21.6-fc1.14.2-abc1234    ← current
benchpool/golden-zvol@v3.21.6-fc1.14.2-def5678    ← previous
```

**6c. SBOM tracking**

`build-guest-rootfs.sh` already generates an SBOM via `apk list --installed`.
This should be stored alongside the golden snapshot metadata (currently it's
only in `/tmp/ci/output/sbom.txt` on the build host).

**When to do this:** Low priority. The current approach works. Versioned
snapshots are a nice-to-have for operational confidence.

---

## 7. Observability Gaps

**Today:** We collect Firecracker metrics (boot time, block I/O, net I/O,
vCPU exit counts) from the metrics file, and emit a ClickHouse wide event
with ~30 fields per job.

**What's missing:**

**7a. Guest-side phase timing**

The host records total CI duration but not the breakdown. How long was
`npm install` vs `npm test`? This is logged to serial console
(`[init] prepare exited with code 0`) but not parsed into structured
telemetry.

Fix (without vsock): parse `[init] prepare:` and `[init] run:` lines from
the serial log after VM exit and extract timestamps. Fragile but cheap.

Fix (with vsock): guest reports phase start/end events with nanosecond
timestamps directly. Much better.

**7b. ZFS ARC pressure**

With parallel VMs, ARC (Adaptive Replacement Cache) pressure becomes a real
concern. ZFS uses ARC to cache frequently-read blocks. Multiple VMs reading
from overlapping COW blocks benefit from ARC; many VMs writing unique blocks
evict ARC entries.

We set `zfs_arc_max=8GiB` in Ansible but don't monitor ARC hit rate. A
ClickHouse metric for `arc_hit_ratio` per-job (via `/proc/spl/kstat/zfs/arcstats`)
would reveal when we're thrashing.

**7c. Host resource utilization during job**

We don't track host CPU, memory, or I/O during VM execution. A goroutine
that samples `/proc/stat`, `/proc/meminfo`, and `/proc/diskstats` at 1Hz
during the job window would give per-job resource utilization — useful for
right-sizing VM allocations and detecting noisy neighbors.

**When to do this:** 7a is high value and can be done incrementally (parse
serial output first, move to vsock later). 7b and 7c are medium value —
useful when running at scale.

---

## 8. Network Isolation: Guest-to-Guest and Egress Filtering

**Today:** Each VM gets its own /30 subnet (e.g., `172.16.0.4/30`). The
Ansible-managed firewall (`firecracker-network-setup.sh.j2`) sets up:

```bash
# NAT chain: guest → internet
iptables -t nat -A FORGE_METAL_FC_NAT -o ${UPLINK} -j MASQUERADE

# Forward chain: isolate guests
iptables -A FORGE_METAL_FC_FWD -d ${POOL_CIDR} -j DROP        # no guest→guest
iptables -A FORGE_METAL_FC_FWD -o ${UPLINK} -j ACCEPT          # guest→internet OK
iptables -A FORWARD -i ${UPLINK} -d ${POOL_CIDR} -m conntrack  # return traffic OK
```

**This is already good.** The `/30` topology means there's no L2 path between
guests — each TAP is a point-to-point link. The FORWARD rules drop packets
destined for the guest pool that aren't from established connections.

**Remaining gaps:**

**8a. Egress filtering**

Currently any guest can reach any internet host. For CI jobs that should
only talk to the local Verdaccio registry and the local Forgejo, wide-open
egress is unnecessary. A tighter policy:

```
ACCEPT  dst=172.16.0.0/16  (guest → host TAP, for registry/git)
ACCEPT  dst={forgejo_ip}    (guest → Forgejo)
DROP    everything else
```

This prevents exfiltration from a compromised CI job. Tradeoff: some
legitimate jobs need external access (downloading fonts, CDN assets, etc.).
Make this configurable per-manifest.

**8b. Rate limiting**

No per-guest bandwidth cap. A CI job running `curl` in a loop could saturate
the uplink. Firecracker's `tx_rate_limiter` on the network interface is the
right mechanism:

```json
PUT /network-interfaces/eth0 {
  "tx_rate_limiter": { "bandwidth": { "size": 52428800, "refill_time": 1000 } }
}
```

50 MB/s per VM is generous for CI and prevents single-VM saturation.

**When to do this:** Egress filtering is high value for security but needs
per-manifest configuration. Rate limiting is easy to add in the API client.

---

## 9. Shell Script Drift

**Today:** `scripts/forge-vm-run.sh` is a 181-line bash script that
duplicates the Go orchestrator's functionality. It uses:
- Hardcoded `golden-zvol2` (stale hack)
- Hardcoded single `172.16.0.2` subnet (no allocator)
- No lease management
- No structured metrics collection
- `python3 -c` for JSON escaping

The Go orchestrator (`internal/firecracker/`) already implements everything
the shell script does, plus lease-based networking, structured telemetry,
and the warm/exec paths.

**Recommendation:** Delete `forge-vm-run.sh`. It served its purpose as the
tracer bullet. Keeping it invites confusion about which path is canonical.
If you need a quick "run a command in a VM" tool, `forge-metal firecracker-test`
already does this via the Go orchestrator.

**When to do this:** Now. It's a one-line deletion that removes a maintenance
burden.

---

## 10. Binary Deployment

**Today:** Firecracker and jailer static binaries are manually `scp`'d to
`/usr/local/bin/` on the server. From `ci/versions.json`:

```json
"firecracker": {
  "version": "1.14.2",
  "note": "Static binaries at /usr/local/bin/ deployed separately"
}
```

From `build-guest-rootfs.sh:19`:
```bash
# LEARNING: Nix-packaged firecracker at /opt/forge-metal/profile/bin/ is
# dynamically linked against /nix/store/ paths — unusable inside the jailer's chroot.
```

**Problems:**
- No automated deployment path — if the server is reprovisioned, someone must
  remember to scp the binaries
- No version verification at runtime — the orchestrator trusts whatever binary
  is at the path
- No checksum verification

**Improvement:** Add a `firecracker_binaries` Ansible task that downloads
pinned releases from GitHub, verifies SHA256, and installs to `/usr/local/bin/`.
Pin the URL and checksum in `ci/versions.json` alongside the existing entries.

```yaml
# ansible/roles/firecracker/tasks/main.yml
- name: Install Firecracker static binaries
  get_url:
    url: "https://github.com/firecracker-microvm/firecracker/releases/download/v{{ fc_version }}/..."
    dest: /usr/local/bin/firecracker
    checksum: "sha256:{{ fc_sha256 }}"
    mode: '0755'
```

**When to do this:** Soon. It's a reliability and reproducibility issue.
Easy to implement.

---

## 11. Seccomp Hardening

**Today:** The jailer applies Firecracker's default seccomp filters, which
restrict the VMM process to a minimal syscall set. We don't pass custom
filters via `--seccomp-filter`.

**What default seccomp does:** Firecracker's built-in filter allows only the
syscalls needed for the VMM: `read`, `write`, `ioctl` (for KVM), `epoll_*`,
`timerfd_*`, `eventfd2`, etc. Anything else causes SIGSYS.

**What we could tighten:** The default filter is already restrictive.
Custom filters make sense when you've audited the exact syscall set your
workload triggers and want to further reduce the attack surface. For CI
workloads that change frequently, the default is appropriate.

**When to do this:** Not a priority. The default seccomp profile is
well-maintained by the Firecracker team and sufficient for CI isolation.

---

## 12. Memory Balloon for Overcommit

**Today:** Each VM gets a fixed `mem_size_mib` (default 2048). Firecracker
does not overcommit — the memory is allocated from the host at boot. With 16
concurrent VMs at 2 GiB each, that's 32 GiB committed to VMs.

**How balloon works:** Firecracker supports `virtio-balloon`. The host can
inflate the balloon (reclaim guest memory) or deflate it (give memory back).
The guest balloon driver cooperates by returning pages to the host.

**Why we probably don't want this:** Balloon is useful for long-running VMs
where memory usage varies over time. CI jobs are short-lived (20-30s) and
have predictable memory profiles. The complexity of monitoring host memory
pressure and dynamically adjusting balloons per-VM isn't justified.

**Better approach:** Right-size `mem_size_mib` per workload. The CI manifest
could specify memory requirements:

```toml
[resources]
memory_mib = 1024  # Next.js build needs ~800MB
```

Default to 2048, allow override down to 512. This is simpler than balloon
and achieves the same goal (fitting more VMs on the host).

**When to do this:** When you're resource-constrained. Not a priority at
current scale.

---

## 13. Warm Pool Consideration (and Why Not Yet)

**Today:** Every job gets a fresh VM: clone zvol, boot kernel, configure,
run, teardown. Total overhead is ~4s (1.7ms clone + ~3s boot + ~1s setup).

**Warm pool concept:** Keep N idle VMs running. When a job arrives, assign
it to an idle VM. When it finishes, reset the VM for the next job.

**Why this doesn't work with Firecracker today:**
- No rootfs hot-plug: can't swap the root drive after boot
- No memory reset: guest state from the previous job persists
- No filesystem reset: COW writes from the previous job persist on the zvol

You'd need to destroy and recreate the VM anyway, which is what we already
do. The only savings would be skipping the jailer setup and API configuration,
which is <1s.

**The actual "warm pool" for Firecracker is snapshots** (Section 1). Restore
a frozen VM image in ~125ms. That's the production answer — used by AWS
Lambda for exactly this reason.

**When to do this:** After snapshots are implemented, this question becomes
moot.

---

## Priority Order

| # | Improvement | Effort | Impact | Prereqs |
|---|------------|--------|--------|---------|
| 1 | Delete forge-vm-run.sh | Trivial | Removes confusion | None |
| 2 | Automate binary deployment | Small | Reproducibility | None |
| 3 | MMDS for job config | Medium | Enables snapshots, cleaner boot | None |
| 4 | CPU pinning via cgroup | Small | Deterministic perf | Slot allocator (done) |
| 5 | I/O rate limiting | Small | Fair sharing | None |
| 6 | Guest phase timing (serial parse) | Small | Observability | None |
| 7 | Network egress filtering | Medium | Security | Per-manifest config |
| 8 | Firecracker snapshots | Large | 24x boot speedup | MMDS |
| 9 | vsock communication | Large | Structured host-guest protocol | Snapshots benefit |
| 10 | PID namespace | Small | Hardening | Fix Wait() ambiguity |
| 11 | ARC/resource monitoring | Medium | Capacity planning | ClickHouse pipeline |
| 12 | Golden image versioning | Small | Operational confidence | None |
| 13 | Per-manifest memory sizing | Small | Density | Manifest schema change |
