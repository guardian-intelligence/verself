# vm-orchestrator

Privileged Go daemon for lease-scoped Firecracker lifecycle management: ZFS clone/snapshot/destroy, jailer setup, TAP networking, vm-bridge control, guest telemetry aggregation, and host-side state reconciliation. The public surface is the V1 lease/exec API over a Unix socket (`/run/vm-orchestrator/api.sock`); the daemon owns no product submission queue, payment policy, or mutable workload policy.

## Lease/Exec Model

`LeaseSpec` reserves a single VM with an absolute host-enforced deadline. `ExecSpec` is a unit of work attached to an existing lease. Host state is authoritative for lease lifecycle, exec lifecycle, lease events, checkpoint refs, and cleanup. Guest/control-plane messages are untrusted inputs and must be validated before host mutation.

Guest checkpoint requests may name only a service-authorized ref; they must never carry ZFS paths or host dataset/version IDs.

## Guest Networking

Firecracker guests live on a TAP network (`172.16.0.0/16`, one `/30` per lease). Three layers mediate guest-to-host connectivity:

1. **nftables FORWARD chain** (`forge_metal_firecracker`): allows guest egress to the internet via the uplink interface and blocks guest-to-guest lateral movement.

2. **Host-service plane** (`fm-host0`, default `10.255.0.1/32`): a dummy interface exposes selected platform endpoints to guests without routing packets to `127.0.0.1`. Caddy listens on `10.255.0.1:18080`.

3. **nftables INPUT chain** (`forge_metal_host`): default-deny on the host. Guest traffic is accepted only from `fc-tap-*`, only from `nftables_firecracker_guest_cidr`, only to `nftables_firecracker_host_service_ip`, and only on `nftables_firecracker_guest_tcp_ports`.

Do not reintroduce DNAT to `127.0.0.1` or `net.ipv4.conf.*.route_localnet=1` for guest access. Guest scripts receive `FORGE_METAL_HOST_SERVICE_IP` and `FORGE_METAL_HOST_SERVICE_HTTP_ORIGIN`; use those rather than the TAP gateway or loopback addresses.

## vm-bridge Control

The host<->guest control protocol is deterministic and sequence-checked in both directions:

- outbound envelopes are monotonic and non-zero by default
- `seq == 0` is a protocol violation and is used only by explicit fault injection
- the guest must send `ack` only for the matching exec result frame
- protocol violations are reported from explicit state labels such as `await_lease_init`, `exec_wait`, and `await_result_ack`

Keep these checks strict. The point is to make bridge faults deterministic and reviewable, not recoverable by best-effort guessing.

## Telemetry Validation

Guest telemetry is validated as a stream, not as isolated frames:

- first frame must be `hello`
- subsequent sample sequence numbers must be monotonic
- forward gaps emit a `gap` diagnostic and keep the stream alive
- sequence regressions emit a `regression` diagnostic and are dropped from the telemetry stream

Persist both the telemetry frames and the diagnostic breadcrumbs in host state and ClickHouse. The proof target is not just that the VM booted; it is that the orchestrator can prove what happened from events and logs after a fault drill.
