# Firecracker Metrics and Observability for CI

> What Firecracker measures, how to consume it, and how to map it to ClickHouse wide events.
>
> Repo: [firecracker-microvm/firecracker](https://github.com/firecracker-microvm/firecracker)
> Researched 2026-03-29.

## Metrics endpoint

Firecracker exposes metrics through a **file-based mechanism**, not HTTP. Configuration:

```bash
# CLI
./firecracker --metrics-path metrics.fifo

# API
curl --unix-socket /tmp/fc.sock -X PUT "http://localhost/metrics" \
  -d '{"metrics_path": "metrics.fifo"}'
```

The destination can be a named pipe (fifo) or regular file. Firecracker creates it if
it does not exist.

**Flushing:** Automatic every 60 seconds, or on demand:
```bash
curl --unix-socket /tmp/fc.sock -X PUT "http://localhost/actions" \
  -d '{"action_type": "FlushMetrics"}'
```

Source: [`docs/metrics.md`](https://github.com/firecracker-microvm/firecracker/blob/main/docs/metrics.md)

## Output format

**JSON Lines (NDJSON).** Each flush writes one complete JSON object + newline. Every
object starts with `utc_timestamp_ms` (milliseconds since epoch, `CLOCK_REALTIME`).

**Metric types:**

| Type | Behavior | Serialization |
|------|----------|---------------|
| `SharedIncMetric` | Counter, resets on flush | Delta since last flush |
| `SharedStoreMetric` | Gauge, persists | Current absolute value |
| `LatencyAggregateMetrics` | Composite | `{min_us, max_us, sum_us}` |

**Units convention** (in field names): `_bytes` = bytes, `_ms` = milliseconds,
`_us` = microseconds, otherwise = count.

Source: [`src/vmm/src/logger/metrics.rs`](https://github.com/firecracker-microvm/firecracker/blob/main/src/vmm/src/logger/metrics.rs)

## Metric categories

All categories are emitted regardless of whether the device is attached (unused devices
report zeros).

### `api_server` (SharedStoreMetric -- absolute values)

- `process_startup_time_us` -- wall-clock process startup time
- `process_startup_time_cpu_us` -- CPU time for startup

### `block` (aggregate) + `block_{drive_id}` (per-drive)

- `read_count`, `write_count` -- number of operations
- `read_bytes`, `write_bytes` -- bytes read/written (SharedIncMetric, delta per flush)
- `read_agg`, `write_agg` -- `{min_us, max_us, sum_us}` latency
- `flush_count`, `queue_event_count`
- `rate_limiter_throttled_events`
- `io_engine_throttled_events` -- io_uring submission queue full
- `activate_fails`, `cfg_fails`, `execute_fails`, `invalid_reqs_count`

### `net` (aggregate) + `net_{iface_id}` (per-interface)

- `rx_bytes_count`, `tx_bytes_count` -- bytes received/transmitted
- `rx_packets_count`, `tx_packets_count`
- `rx_count`, `tx_count` -- successful read/write operations
- `rx_fails`, `tx_fails`
- `rx_rate_limiter_throttled`, `tx_rate_limiter_throttled`
- `tap_read_fails`, `tap_write_fails`
- `tap_write_agg` -- `{min_us, max_us, sum_us}` TAP write latency
- `tx_spoofed_mac_count`, `tx_malformed_frames`

### `vcpu`

- `exit_io_in`, `exit_io_out` -- KVM exit counts for IO
- `exit_mmio_read`, `exit_mmio_write` -- KVM exit counts for MMIO
- `exit_io_in_agg`, `exit_io_out_agg`, `exit_mmio_read_agg`, `exit_mmio_write_agg` --
  each `{min_us, max_us, sum_us}`
- `failures`, `kvmclock_ctrl_fails`

### `latencies_us` (SharedStoreMetric -- absolute values)

- `full_create_snapshot`, `diff_create_snapshot`, `load_snapshot`
- `pause_vm`, `resume_vm`
- `vmm_full_create_snapshot`, `vmm_diff_create_snapshot`, `vmm_load_snapshot`
- `vmm_pause_vm`, `vmm_resume_vm`

### `vsock`

- `rx_bytes_count`, `tx_bytes_count`
- `rx_packets_count`, `tx_packets_count`
- `conns_added`, `conns_killed`, `conns_removed`
- `tx_flush_fails`, `tx_write_fails`, `rx_read_fails`

### `balloon`

- `inflate_count`, `deflate_count`
- `free_page_report_count`, `free_page_report_freed`, `free_page_report_fails`
- `free_page_hint_count`, `free_page_hint_freed`, `free_page_hint_fails`
- `stats_updates_count`, `stats_update_fails`

### `entropy` (virtio-rng)

- `entropy_bytes` -- bytes provided to guest
- `entropy_event_count`
- `host_rng_fails`, `entropy_rate_limiter_throttled`

### `seccomp`

- `num_faults` -- seccomp filter violations (SharedStoreMetric, 0 or 1)

### `signals`

- `sigbus`, `sigsegv`, `sigxfsz`, `sigxcpu`, `sighup`, `sigill` (0 or 1)
- `sigpipe` (SharedIncMetric)

### `mmds`

- `rx_accepted`, `rx_accepted_err`, `rx_count`
- `tx_bytes`, `tx_count`, `tx_errors`
- `connections_created`, `connections_destroyed`

### `vmm`

- `panic_count` -- whether the VMM panicked (0 or 1)

Source: Block metrics from [`src/vmm/src/devices/virtio/block/virtio/metrics.rs`](https://github.com/firecracker-microvm/firecracker/blob/main/src/vmm/src/devices/virtio/block/virtio/metrics.rs),
Net from [`net/metrics.rs`](https://github.com/firecracker-microvm/firecracker/blob/main/src/vmm/src/devices/virtio/net/metrics.rs)

## Mapping to ClickHouse `ci_events` wide event columns

| CI Wide Event Column | Firecracker Metric | Notes |
|---|---|---|
| `block_read_bytes` | `block_{drive_id}.read_bytes` | Per-flush delta |
| `block_write_bytes` | `block_{drive_id}.write_bytes` | Per-flush delta |
| `block_read_count` | `block_{drive_id}.read_count` | |
| `block_write_count` | `block_{drive_id}.write_count` | |
| `block_read_latency_us` | `block_{drive_id}.read_agg.sum_us` | Divide by count for avg |
| `block_write_latency_us` | `block_{drive_id}.write_agg.sum_us` | |
| `net_rx_bytes` | `net_{iface_id}.rx_bytes_count` | |
| `net_tx_bytes` | `net_{iface_id}.tx_bytes_count` | |
| `net_rx_packets` | `net_{iface_id}.rx_packets_count` | |
| `net_tx_packets` | `net_{iface_id}.tx_packets_count` | |
| `vm_boot_time_us` | `api_server.process_startup_time_us` | Absolute |
| `snapshot_load_us` | `latencies_us.load_snapshot` | If using snapshots |
| `vcpu_exit_count` | Sum of `vcpu.exit_io_*` + `vcpu.exit_mmio_*` | IO pressure indicator |
| `seccomp_violations` | `seccomp.num_faults` | Security signal |
| `vmm_panic` | `vmm.panic_count` | |

**Memory high water mark** is NOT in the metrics system. Use balloon statistics API
or host-side cgroup monitoring.

## Integration pattern for ClickHouse

Firecracker has no native Prometheus, OTel, or ClickHouse integration. The orchestrator
must bridge:

```
Firecracker --metrics-path /run/fc/{job_id}/metrics.fifo
    |
    v  (named pipe reader goroutine)
Host Orchestrator
    |
    v  (parse JSON, flatten, merge with job metadata)
ClickHouse INSERT into ci_events
```

The orchestrator should:
1. Call `FlushMetrics` immediately before VM exit to capture final counters
2. Read the last JSON line from the metrics pipe
3. Parse and flatten nested JSON into wide event columns
4. Merge with orchestrator-level data (job ID, clone time, `zfs get written`, wall-clock duration)
5. INSERT into ClickHouse

## Logging

Configuration (one-time, cannot be reconfigured):

```bash
./firecracker --log-path logs.fifo --level Info --show-level --show-log-origin
```

Log levels: `Error`, `Warning`, `Info`, `Debug`, `Trace`, `Off`. The `module` field
filters to a specific Rust module path (e.g., `api_server::request`).

**Warning:** Guest can influence log volume. Use bounded storage (ring buffers, journald)
for production.

Source: [`docs/logger.md`](https://github.com/firecracker-microvm/firecracker/blob/main/docs/logger.md)

## Serial console capture for CI logs

Two mechanisms for capturing guest serial output:

**`serial_out_path` API (v1.14+):**
```bash
curl --unix-socket /tmp/fc.sock -X PUT "http://localhost/serial" \
  -d '{"serial_out_path": "/run/fc/job-abc/serial.log"}'
```

**stdout redirect:**
```bash
./firecracker --api-sock /tmp/fc.sock > /run/fc/job-abc/serial.log 2>&1
```

Guest kernel must have `console=ttyS0` in boot args. To disable (recommended for
performance): `8250.nr_uarts=0`.

**Caveat:** Serial output is unbounded and guest-controlled. Use bounded storage.

Source: [`docs/prod-host-setup.md`](https://github.com/firecracker-microvm/firecracker/blob/main/docs/prod-host-setup.md)

## Guest-to-host communication patterns

### vsock (recommended for bidirectional streaming)

Guest AF_VSOCK sockets map 1:1 to host AF_UNIX sockets. The guest connects to CID 2
(host) on a port; the host listens on `{uds_path}_{port}`.

For CI: guest streams build logs, exit code, and custom metrics over vsock. Host agent
parses and forwards to ClickHouse/OTel.

vsock metrics tracked: `rx_bytes_count`, `tx_bytes_count`, connection lifecycle counts.

### MMDS (host-to-guest config injection)

Guest queries HTTP at link-local IP (default `169.254.169.254`). Use for injecting
job metadata (job ID, repo URL, commit SHA). Data store deliberately cleared on
snapshot restore -- each clone gets fresh config.

### No shared filesystem

Firecracker does not support virtio-fs or 9p. For shared data, use a second block
device (zvol) or the network.

Source: [`docs/vsock.md`](https://github.com/firecracker-microvm/firecracker/blob/main/docs/vsock.md),
[`docs/mmds/mmds-user-guide.md`](https://github.com/firecracker-microvm/firecracker/blob/main/docs/mmds/mmds-user-guide.md)

## Balloon statistics (guest memory visibility)

The balloon device with `stats_polling_interval_s > 0` provides guest memory stats
via `GET /balloon/statistics`:

- `free_memory`, `total_memory`, `available_memory` (bytes)
- `swap_in`, `swap_out` (bytes)
- `major_faults`, `minor_faults`
- `disk_caches` (bytes)
- `oom_kill` -- OOM killer invocations (kernel >= 6.12)

**This is the closest to a memory high water mark.** Poll periodically, track the
minimum of `available_memory` to derive peak usage per job.

**Caveat:** Statistics come from the guest driver. A compromised guest could report
false values.

Source: [`docs/ballooning.md`](https://github.com/firecracker-microvm/firecracker/blob/main/docs/ballooning.md)

## Recommended observability stack for forge-metal CI

```
MMDS (host -> guest):     Job metadata (job_id, repo_url, commit_sha)
vsock (guest -> host):    Real-time build logs, exit code, custom metrics
Metrics pipe (host):      Firecracker-level I/O, network, vCPU metrics
Serial console (host):    Kernel logs, early boot output
ZFS written (host):       Bytes dirtied by the job (zfs get written)
Balloon stats (host API): Guest memory usage, OOM events
Cgroup metrics (host):    CPU time, memory high water mark, I/O bytes
```

All of these feed into a single denormalized `ci_events` row in ClickHouse per job.

## Key paper

[Firecracker: Lightweight Virtualization for Serverless Applications (NSDI '20)](https://www.usenix.org/system/files/nsdi20-paper-agache.pdf)
-- Agache et al., AWS. Reports boot time <125ms, memory overhead <5 MiB, up to 150
microVM creations/second, network throughput up to 25 Gbps, storage throughput up to
1 GiB/s.

Reproducibility data: [firecracker-microvm/nsdi2020-data](https://github.com/firecracker-microvm/nsdi2020-data)

## Sources

- [metrics.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/metrics.md)
- [logger.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/logger.md)
- [vsock.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/vsock.md)
- [ballooning.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/ballooning.md)
- [snapshot-editor.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/snapshotting/snapshot-editor.md)
- [mmds-user-guide.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/mmds/mmds-user-guide.md)
- [prod-host-setup.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/prod-host-setup.md)
- [SPECIFICATION.md](https://github.com/firecracker-microvm/firecracker/blob/main/SPECIFICATION.md)
- [NSDI '20 Paper](https://www.usenix.org/system/files/nsdi20-paper-agache.pdf)
- [metrics.rs source](https://github.com/firecracker-microvm/firecracker/blob/main/src/vmm/src/logger/metrics.rs)
