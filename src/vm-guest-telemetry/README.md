# vm-guest-telemetry

`vm-guest-telemetry` is the Zig guest agent for Firecracker VM health sampling. Written in Zig instead of Go because disk/RAM footprint matters.

The agent reads guest-local state from `/proc` and streams fixed-size 128-byte frames over vsock port `10790`. The Go guest PID 1 (`vm-bridge`) owns the run-control channel on vsock port `10789`.

The stream is host-validated, not just guest-emitted:

- the first frame must be `hello`
- `sample.seq` must increase monotonically for the stream
- sequence gaps and regressions are diagnosed by the host ingestion path
- the guest wire format does not change when the host injects telemetry faults

## Build

```bash
cd vm-guest-telemetry
zig build -Doptimize=ReleaseSafe
```

Artifacts land in `vm-guest-telemetry/zig-out/bin/`.

## Guest Artifact

The guest binary is a required part of the Firecracker rootfs. The `guest-rootfs` automation installs it at `/usr/local/bin/vm-guest-telemetry`, and `vm-bridge` starts it during boot.

## Cross-Language Conformance

`protocol/vectors.json` contains golden test vectors generated from the Zig canonical encoder. Each vector pairs hex-encoded wire bytes with expected decoded field values.

The Zig implementation is the wire-format authority. Go decoder tests should consume the checked-in vectors rather than duplicating frame layout assumptions in test code.

Regenerate after changing the binary protocol layout:

```bash
cd vm-guest-telemetry
zig build run-generate-vectors > protocol/vectors.json
zig build test  # staleness test verifies the checked-in file matches
```

See [docs/protocol.md](docs/protocol.md) for the vector file format and conformance testing model.

## Deterministic Host Faults

The telemetry proof harness uses deterministic host-side ingestion fault profiles to prove diagnostics:

- `gap_once@<seq>`
- `regression_once@<seq>`

These are host ingestion faults applied after frame decode. They are not guest wire-format changes and do not alter the checked-in protocol vectors.

The proof path should produce ClickHouse evidence for both the normal telemetry stream and the diagnostic path. Practical evidence is a host log row whose body contains `guest telemetry stream diagnostic` and whose structured attributes include `kind`, `expected_seq`, `observed_seq`, and `missing_samples`.

Read [docs/zig-coding/STYLE.md](docs/zig-coding/STYLE.md) for coding guidance.
