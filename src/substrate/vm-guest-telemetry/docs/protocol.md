# vm-guest-telemetry Protocol

`vm-guest-telemetry` emits one fixed-size binary stream over vsock port `10790`.
The stream contains:

- one `hello` frame at connection start
- a continuous sequence of `sample` frames at 60 Hz

All integers are little-endian. Every frame is exactly 128 bytes.

The host validates the stream as it ingests it:

- the first frame must be `hello`
- `sample.seq` must be monotonic
- a sequence jump forward is a gap diagnostic
- a sequence regression is a regression diagnostic

These diagnostics are host-side ingestion behavior. They do not change the guest wire format.

## Frame Header

| Offset | Size | Field | Type |
|--------|------|-------|------|
| 0 | 4 | `magic` | `u32` |
| 4 | 2 | `version` | `u16` |
| 6 | 2 | `kind` | `u16` |
| 8 | 4 | `seq` | `u32` |
| 12 | 4 | `flags` | `u32` |
| 16 | 8 | `mono_ns` | `u64` |
| 24 | 8 | `wall_ns` | `u64` |
| 32 | 96 | payload | bytes |

Constants:

- `magic = 0x46505600`
- `version = 1`
- `kind = 1` for `hello`
- `kind = 2` for `sample`

## Hello Payload

| Offset | Size | Field | Type |
|--------|------|-------|------|
| 32 | 16 | `boot_id` | raw UUID bytes |
| 48 | 8 | `mem_total_kb` | `u64` |

`hello.seq` is always `0`.

## Sample Payload

| Offset | Size | Field | Type |
|--------|------|-------|------|
| 32 | 8 | `cpu_user_ticks` | `u64` |
| 40 | 8 | `cpu_system_ticks` | `u64` |
| 48 | 8 | `cpu_idle_ticks` | `u64` |
| 56 | 4 | `load1_centis` | `u32` |
| 60 | 4 | `load5_centis` | `u32` |
| 64 | 4 | `load15_centis` | `u32` |
| 68 | 2 | `procs_running` | `u16` |
| 70 | 2 | `procs_blocked` | `u16` |
| 72 | 8 | `mem_available_kb` | `u64` |
| 80 | 8 | `io_read_bytes` | `u64` |
| 88 | 8 | `io_write_bytes` | `u64` |
| 96 | 8 | `net_rx_bytes` | `u64` |
| 104 | 8 | `net_tx_bytes` | `u64` |
| 112 | 2 | `psi_cpu_pct100` | `u16` |
| 114 | 2 | `psi_mem_pct100` | `u16` |
| 116 | 2 | `psi_io_pct100` | `u16` |

Samples begin at `seq = 1` and increment monotonically for the lifetime of the stream.
The host treats `hello.seq + 1` as the first expected sample sequence.

## Missing-Data Flags

`flags` is a guest-side bitset:

- bit 0: CPU PSI unavailable
- bit 1: memory PSI unavailable
- bit 2: IO PSI unavailable
- bit 3: root-disk counters unavailable
- bit 4: primary-interface counters unavailable

Unavailable fields encode as zero and must be interpreted alongside `flags`.

## Golden Vectors

`protocol/vectors.json` is the checked-in conformance file generated from the Zig canonical encoder. It contains:

- `guest_hello`
- `guest_sample`
- `guest_sample_max`

Each vector pairs hex-encoded wire bytes with decoded field values. Go decoder tests should validate against that file rather than re-specifying the layout independently.

Do not maintain separate Go-side protocol fixtures that drift from the Zig encoder output. The Zig encoder defines the canonical byte layout for the guest agent.

## Host Diagnostics

The current host ingestion path validates sequence continuity and emits diagnostics for non-monotonic streams:

- `hello-first` failures terminate ingestion immediately.
- a forward jump from `expected_seq` to a larger `observed_seq` emits a gap diagnostic with `missing_samples = observed_seq - expected_seq`.
- a backward jump emits a regression diagnostic and drops the regressed frame.

In ClickHouse, the practical evidence is the host log row:

- log body: `guest telemetry stream diagnostic`
- structured attributes: `run_id`, `kind`, `expected_seq`, `observed_seq`, `missing_samples`

For healthy streams, look for `guest telemetry hello received` and `guest telemetry sample received` log bodies with their corresponding `run_id` and `seq` attributes.

## Deterministic Fault Profiles

The smoke harness uses deterministic host-side ingestion fault profiles to exercise the diagnostics:

- `gap_once@<seq>`
- `regression_once@<seq>`

These profiles are injected on the host after frame decode. They are not guest wire-format changes, and they do not require regenerating `protocol/vectors.json`.

Regenerate:

```bash
cd src/substrate/vm-guest-telemetry
zig build run-generate-vectors > protocol/vectors.json
zig build test
```
