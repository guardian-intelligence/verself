# vm-guest-telemetry

`vm-guest-telemetry` is the Zig guest agent for Firecracker VM health sampling.

The agent reads guest-local state from `/proc` and streams fixed-size 128-byte frames over vsock port `10790`. The Go guest PID 1 (`vm-init`) still owns the job-control channel on vsock port `10789`.

## Build

```bash
cd vm-guest-telemetry
zig build -Doptimize=ReleaseSafe
```

Artifacts land in `vm-guest-telemetry/zig-out/bin/`.

## Guest Artifact

The guest binary is a required part of the Firecracker rootfs. The `guest-rootfs` automation installs it at `/usr/local/bin/vm-guest-telemetry`, and `vm-init` starts it during boot.

## Cross-Language Conformance

`protocol/vectors.json` contains golden test vectors generated from the Zig reference encoder. Each vector pairs hex-encoded wire bytes with expected decoded field values. TypeScript or Go consumers validate their decoders against these vectors.

Regenerate after changing the binary protocol layout:

```bash
cd vm-guest-telemetry
zig build run-generate-vectors > protocol/vectors.json
zig build test  # staleness test verifies the checked-in file matches
```

See [docs/protocol.md](docs/protocol.md) for the vector file format and conformance testing model.

Read [docs/zig-coding/STYLE.md](docs/zig-coding/STYLE.md) for coding guidance.
