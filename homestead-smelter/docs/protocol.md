# Protocol

`homestead-smelter` has two binary protocols:

- guest -> host telemetry over Firecracker's vsock bridge
- host -> local consumers over the host control socket

Normative language in this document follows the usual `MUST`, `SHOULD`, and `MAY` conventions.

## Guest Telemetry Frames

The guest emits a fixed `128`-byte frame every `1/60s`. All integers are little-endian. `frame_version`
remains `1`.

### Common Header

| Offset | Size | Name | Type | Units | Contract |
| --- | ---: | --- | --- | --- | --- |
| 0 | 4 | `magic` | `u32` | none | MUST equal `0x46505600`. |
| 4 | 2 | `version` | `u16` | none | MUST equal `1`. |
| 6 | 2 | `kind` | `u16` | none | MUST be `1` (`hello`) or `2` (`sample`). |
| 8 | 4 | `seq` | `u32` | frames | MUST increment monotonically within a stream. `hello` MUST use `0`. |
| 12 | 4 | `flags` | `u32` | bitset | Signals missing optional observations. Zero means all present. |
| 16 | 8 | `mono_ns` | `u64` | ns | Guest monotonic clock at emit time. |
| 24 | 8 | `wall_ns` | `u64` | ns | Guest realtime clock at emit time. |

### Hello Payload

`hello` is emitted once per guest connection.

| Offset | Size | Name | Type | Units | Contract |
| --- | ---: | --- | --- | --- | --- |
| 32 | 16 | `boot_id` | `[16]u8` | UUID bytes | MUST uniquely identify the guest boot. |
| 48 | 8 | `mem_total_kb` | `u64` | KiB | Total memory visible inside the guest. Boot-static. |
| 56 | 72 | `reserved` | bytes | none | MUST be zero on send and ignored on receive. |

### Sample Payload

Counter fields are monotonic within a boot. Gauge fields describe the guest state at the sample timestamp.

| Offset | Size | Name | Type | Units | Contract |
| --- | ---: | --- | --- | --- | --- |
| 32 | 8 | `cpu_user_ticks` | `u64` | ticks | User plus nice CPU time from `/proc/stat`. |
| 40 | 8 | `cpu_system_ticks` | `u64` | ticks | System plus irq plus softirq CPU time from `/proc/stat`. |
| 48 | 8 | `cpu_idle_ticks` | `u64` | ticks | Idle plus iowait CPU time from `/proc/stat`. |
| 56 | 4 | `load1_centis` | `u32` | x100 | 1-minute load average. |
| 60 | 4 | `load5_centis` | `u32` | x100 | 5-minute load average. |
| 64 | 4 | `load15_centis` | `u32` | x100 | 15-minute load average. |
| 68 | 2 | `procs_running` | `u16` | count | Runnable task count. |
| 70 | 2 | `procs_blocked` | `u16` | count | Blocked task count. |
| 72 | 8 | `mem_available_kb` | `u64` | KiB | Available guest memory. |
| 80 | 8 | `io_read_bytes` | `u64` | bytes | Root-device bytes read. |
| 88 | 8 | `io_write_bytes` | `u64` | bytes | Root-device bytes written. |
| 96 | 8 | `net_rx_bytes` | `u64` | bytes | Primary-interface bytes received. |
| 104 | 8 | `net_tx_bytes` | `u64` | bytes | Primary-interface bytes sent. |
| 112 | 2 | `psi_cpu_pct100` | `u16` | x100 | CPU pressure `avg10`. |
| 114 | 2 | `psi_mem_pct100` | `u16` | x100 | Memory pressure `avg10`. |
| 116 | 2 | `psi_io_pct100` | `u16` | x100 | IO pressure `avg10`. |
| 118 | 10 | `reserved` | bytes | none | MUST be zero on send and ignored on receive. |

### Sample Flags

| Bit | Meaning |
| --- | --- |
| 0 | CPU PSI unavailable |
| 1 | Memory PSI unavailable |
| 2 | IO PSI unavailable |
| 3 | Root-device stats unavailable |
| 4 | Primary-interface stats unavailable |

### Omitted Fields

The following values are intentionally absent because they are derivable from the deployment configuration or
would duplicate boot-static data:

- sample rate: fixed at `60Hz`
- guest telemetry port: fixed by the host launch path
- guest network interface name
- guest block device name

## Host Control Protocol

The host control socket is an `AF_UNIX` `SOCK_SEQPACKET` socket carrying fixed-size binary records. There
is no JSON path in the daemon.

### Request Record

Each request is exactly `32` bytes.

| Offset | Size | Name | Type | Units | Contract |
| --- | ---: | --- | --- | --- | --- |
| 0 | 4 | `magic` | `u32` | none | MUST equal `0x48534d00`. |
| 4 | 2 | `version` | `u16` | none | MUST equal `1`. |
| 6 | 2 | `kind` | `u16` | none | MUST be `1` (`attach`). |
| 8 | 24 | `reserved` | bytes | none | MUST be zero on send and ignored on receive. |

### Packet Record

Each host packet is exactly `176` bytes: a `48`-byte host envelope plus a raw `128`-byte guest payload.

| Offset | Size | Name | Type | Units | Contract |
| --- | ---: | --- | --- | --- | --- |
| 0 | 4 | `magic` | `u32` | none | MUST equal `0x48534d01`. |
| 4 | 2 | `version` | `u16` | none | MUST equal `1`. |
| 6 | 2 | `kind` | `u16` | none | `1`=`hello`, `2`=`sample`, `3`=`disconnect`, `4`=`vm_gone`, `5`=`snapshot_end`. |
| 8 | 8 | `host_seq` | `u64` | events | For event packets, MUST equal the underlying `host_core` event sequence. For `snapshot_end`, MUST equal the next resume sequence. |
| 16 | 8 | `observed_wall_ns` | `u64` | ns | Host realtime clock at packet emit time. |
| 24 | 16 | `job_id` | `[16]u8` | UUID bytes | CI job identity. Zero for `snapshot_end`. |
| 40 | 4 | `stream_generation` | `u32` | none | Host reconnect generation for the VM stream. Zero for `snapshot_end`. |
| 44 | 4 | `flags` | `u32` | bitset | Host packet flags. |
| 48 | 128 | `payload` | bytes | packet payload | Raw guest frame for `hello` and `sample`; disconnect reason enum in the first `u32` for `disconnect`; zero for `vm_gone` and `snapshot_end`. |

### Host Packet Flags

| Bit | Meaning |
| --- | --- |
| 0 | Packet was emitted as part of a snapshot replay rather than live tailing |

### Attach Semantics

- A consumer connects once and sends a single fixed-size `attach` request.
- The host replies with the current snapshot replay, then emits one `snapshot_end` packet.
- After `snapshot_end`, the same socket tails live event packets until the consumer disconnects or falls behind retention.
