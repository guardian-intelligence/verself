# vm-orchestrator

Privileged Go daemon for lease-scoped Firecracker lifecycle management: ZFS clone/snapshot/destroy, jailer setup, TAP networking, vm-bridge control, guest telemetry aggregation, and host-side state reconciliation. The public surface is the V1 lease/exec API over a Unix socket (`/run/vm-orchestrator/api.sock`); the daemon owns no product submission queue, payment policy, or mutable workload policy.

## Subdirectories

- `cmd/vm-orchestrator/` — daemon entry point.
- `cmd/vm-orchestrator-cli/` — privileged operator CLI (currently just `seed-image`).
- `cmd/vm-bridge/` — guest PID 1 + local control socket. Reads toolchain-image overlay manifests at lease boot.
- `zfs/` — typed refs, validation, channel programs, the `VolumeLifecycle` facade.
- `proto/v1/` — gRPC contracts for the lease/exec API.
- `vmproto/` — host↔guest control-plane wire types.
- `guest-images/` — `toolchain_ext4_image` Bazel macro + substrate builder + per-image build rules. Each image declares a `tier` consumed by `vm-orchestrator-seed.service`. See `<guest_rootfs_split>` in the repo-root `AGENTS.md`.

## Privilege Boundary

vm-orchestrator is the only runtime process allowed to hold host privileges for ZFS, Firecracker, TAP, jailer, `/dev/kvm`, or `/dev/zvol`. Product services never receive `zfs allow`, host device access, root-equivalent capabilities, or Firecracker/jailer arguments. They send service-authorized refs and lifecycle intents over the Unix socket; vm-orchestrator resolves those refs to contained host resources.

Treat membership in the socket group (`vm-clients`) as root-equivalent for this daemon's API. Keep it to explicitly approved internal callers, audit it in Ansible, and do not add browser frontends, webhook-only services, guest agents, or general platform services to that group.

PrivOps accepts host-level names because it is the privileged adapter. All callers above it must use typed refs or constructors that enforce pool/dataset containment before invoking ZFS, Firecracker, TAP, or jailer operations.

## Lease/Exec Model

`LeaseSpec` reserves a single VM with an absolute host-enforced deadline. `ExecSpec` is a unit of work attached to an existing lease. Host state is authoritative for lease lifecycle, exec lifecycle, lease events, checkpoint refs, and cleanup. Guest/control-plane messages are untrusted inputs and must be validated before host mutation.

Guest checkpoint requests may name only a service-authorized ref; they must never carry ZFS paths or host dataset/version IDs.

## Guest Networking

Firecracker guests live on a TAP network (`172.16.0.0/16`, one `/30` per lease). Three layers mediate guest-to-host connectivity:

1. **nftables FORWARD chain** (`verself_firecracker`): allows guest egress to the internet via the uplink interface and blocks guest-to-guest lateral movement.

2. **Host-service plane** (`verself-host0`, default `10.255.0.1/32`): a dummy interface exposes selected platform endpoints to guests without routing packets to `127.0.0.1`. Caddy listens on `10.255.0.1:18080`.

3. **nftables INPUT chain** (`verself_host`): default-deny on the host. Guest traffic is accepted only from `fc-tap-*`, only from `nftables_firecracker_guest_cidr`, only to `nftables_firecracker_host_service_ip`, and only on `nftables_firecracker_guest_tcp_ports`.

Do not reintroduce DNAT to `127.0.0.1` or `net.ipv4.conf.*.route_localnet=1` for guest access. Guest scripts receive `VERSELF_HOST_SERVICE_IP` and `VERSELF_HOST_SERVICE_HTTP_ORIGIN`; use those rather than the TAP gateway or loopback addresses.

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

Persist both the telemetry frames and the diagnostic breadcrumbs in host state and ClickHouse. The target is not just that the VM booted; it is that the orchestrator can reconstruct what happened from events and logs after a fault drill.
