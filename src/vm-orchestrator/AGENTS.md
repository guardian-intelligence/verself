# vm-orchestrator

Privileged Go daemon for run-centric Firecracker lifecycle management: ZFS clone/snapshot/destroy, jailer setup, TAP networking, vm-bridge control, guest telemetry aggregation, and host-side state reconciliation. The public surface is the run API over a Unix socket (`/run/vm-orchestrator/api.sock`); the daemon owns no customer submission queue and no mutable workload policy.

## Run Model

`RunSpec` is the unit of work and `RunResult` is the terminal product. A run is a host-authorized Firecracker VM execution with deterministic control messages and durable host state:

- host state is authoritative for run lifecycle, run events, checkpoint refs, and cleanup
- guest/control-plane messages are untrusted inputs and must be validated before host mutation
- guest event streams are host-derived phase, lifecycle, checkpoint, log, and telemetry signals
- guest checkpoint requests may name only a service-authorized ref; they must never carry ZFS paths, org IDs, or host dataset/version IDs

Do not reintroduce legacy API/state wording in this tree. If a new term is needed, use `run`, `run_id`, `run_state`, `run_event`, or `run_result` consistently.

## Guest Networking

Firecracker guests live on a TAP network (`172.16.0.0/16`, one `/30` per run). Three layers mediate guest-to-host connectivity:

1. **nftables FORWARD chain** (`forge_metal_firecracker`): allows guest egress to the internet via the uplink interface and blocks guest-to-guest lateral movement.

2. **Host-service plane** (`fm-host0`, default `10.255.0.1/32`): a dummy interface exposes selected platform endpoints to guests without routing packets to `127.0.0.1`. Caddy listens on `10.255.0.1:18080` and reverse-proxies Forgejo Git smart-HTTP clone/fetch endpoints to Forgejo's loopback listener. Verdaccio is reached directly at `10.255.0.1:4873`.

3. **nftables INPUT chain** (`forge_metal_host`): default-deny on the host. Guest traffic is accepted only from `fc-tap-*`, only from `nftables_firecracker_guest_cidr`, only to `nftables_firecracker_host_service_ip`, and only on `nftables_firecracker_guest_tcp_ports`.

Do not reintroduce DNAT to `127.0.0.1` or `net.ipv4.conf.*.route_localnet=1` for guest access. Guest scripts receive `FORGE_METAL_HOST_SERVICE_IP` and `FORGE_METAL_HOST_SERVICE_HTTP_ORIGIN`; use those rather than the TAP gateway or loopback addresses.

## vm-bridge Control

The host<->guest control protocol is deterministic and sequence-checked in both directions:

- outbound envelopes are monotonic and non-zero by default
- `seq == 0` is a protocol violation and is used only by explicit fault injection
- the guest must send `ack` only for the matching `result` frame
- `ack.for_type` must be `result`
- `ack.for_seq` must match the exact `result` sequence number
- protocol violations are reported from the explicit state labels `await_run_request`, `run_phase`, `await_result_ack`, and `await_shutdown`

Keep these checks strict. The point is to make bridge faults deterministic and reviewable, not recoverable by best-effort guessing.

## Telemetry Validation

Guest telemetry is validated as a stream, not as isolated frames:

- first frame must be `hello`
- subsequent sample sequence numbers must be monotonic
- forward gaps emit a `gap` diagnostic and keep the stream alive
- sequence regressions emit a `regression` diagnostic and are dropped from the run event stream

Persist both the telemetry frames and the diagnostic breadcrumbs in host state and ClickHouse. The proof target is not just that the VM booted; it is that the orchestrator can prove what happened from events and logs after a fault drill.

## Proof Targets

Use the maintained proof targets instead of ad hoc manual runs:

- `make vm-orchestrator-proof` - baseline deploy and telemetry proof against a healthy run
- `make vm-orchestrator-proof-gap` - inject a telemetry gap and verify the `gap` diagnostic path
- `make vm-orchestrator-proof-regression` - inject a telemetry regression and verify the `regression` diagnostic path
- `make vm-orchestrator-proof-bridge-fault` - inject a vm-bridge protocol fault and verify deterministic protocol-violation handling

Each proof is expected to leave ClickHouse evidence behind: traces for run lifecycle and bridge/telemetry handling, logs for the diagnostic or protocol-violation breadcrumbs, and enough state to correlate the run ID back to the host ledger.

## Shell Scripting Inside Guests

The guest rootfs is Ubuntu 24.04. Use normal Debian-family userland
assumptions and keep scripts explicit about paths when PID 1 is involved:

- `/bin/sh` is dash. Use `bash` explicitly for bash-only expansions.
- Use full paths for system utilities (`/sbin/ip`, not `ip`) when the PATH may not include `/sbin`.
- Avoid `sed` with dynamic substitution values that may contain shell metacharacters. Prefer structured inputs or purpose-built helper code.
- The `set -eu` flag is recommended for POSIX shell scripts.
