# vm-guest-telemetry

`vm-guest-telemetry` is the Zig guest agent for Firecracker VM health sampling. Written in Zig instead of Go because disk/RAM footprint matters.

The agent reads guest-local state from `/proc` and streams fixed-size 128-byte frames over vsock port `10790`. The Go guest PID 1 (`vm-bridge`) owns the run-control channel on vsock port `10789`.

The stream is host-validated, not just guest-emitted:

- the first frame must be `hello`
- `sample.seq` must increase monotonically for the stream
- sequence gaps and regressions are diagnosed by the host ingestion path
- the guest wire format does not change when the host injects telemetry faults

Bazel is the authoritative build system for this package — `rules_zig` orchestrates the Zig compiler directly. There is no `build.zig`.

## Build

```bash
bazelisk build //src/substrate/vm-guest-telemetry:vm-guest-telemetry
```

The deployable binary is pinned to `linux/x86_64/musl` via `zig_configure_binary` regardless of the requesting `-c` mode, and emitted under `bazel-bin/src/substrate/vm-guest-telemetry/`.

## Test

```bash
bazelisk test //src/substrate/vm-guest-telemetry/...
```

This runs:

- the in-source Zig unit tests in `root.zig` (encoder/decoder round-trips)
- the in-source Zig unit tests in `guest.zig` (`/proc` parsers)
- `write_vectors_test`, the staleness gate that fails when `protocol/vectors.json` diverges from the canonical encoder

## Guest Artifact

The guest binary is a required part of the Firecracker rootfs. The `guest-rootfs` automation builds `//src/substrate/vm-orchestrator/guest-images/substrate:substrate_inputs_bundle`, installs the bundled binary at `/usr/local/bin/vm-guest-telemetry`, and `vm-bridge` starts it during boot.

## Cross-Language Conformance

`protocol/vectors.json` contains golden test vectors generated from the Zig canonical encoder. Each vector pairs hex-encoded wire bytes with expected decoded field values.

The Zig implementation is the wire-format authority. The artifact is exported as a public Bazel label so decoders in other languages can consume the checked-in vectors through the action graph rather than via filesystem path:

```python
go_test(
    ...
    data = ["//src/substrate/vm-guest-telemetry:protocol/vectors.json"],
)
```

End-to-end ingestion is exercised by the telemetry smoke harness, which produces ClickHouse evidence — that path remains the primary correctness gate.

Regenerate after changing the binary protocol layout:

```bash
bazelisk run //src/substrate/vm-guest-telemetry:write_vectors
```

`bazel test` will fail until the regenerated file is committed. See [docs/protocol.md](docs/protocol.md) for the vector file format and conformance testing model.

## Bench

```bash
bazelisk run //src/substrate/vm-guest-telemetry:bench -- [args]
```

## Deterministic Host Faults

The telemetry smoke harness uses deterministic host-side ingestion fault profiles to verify diagnostics:

- `gap_once@<seq>`
- `regression_once@<seq>`

These are host ingestion faults applied after frame decode. They are not guest wire-format changes and do not alter the checked-in protocol vectors.

The smoke path should produce ClickHouse evidence for both the normal telemetry stream and the diagnostic path. Practical evidence is a host log row whose body contains `guest telemetry stream diagnostic` and whose structured attributes include `kind`, `expected_seq`, `observed_seq`, and `missing_samples`.

Read [docs/zig-coding/STYLE.md](docs/zig-coding/STYLE.md) for coding guidance.
