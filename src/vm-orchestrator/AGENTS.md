# vm-orchestrator

Privileged Go daemon for lease-scoped Firecracker lifecycle management: ZFS clone/snapshot/destroy, jailer setup, TAP networking, vm-bridge control, guest telemetry aggregation, and host-side state reconciliation. The public surface is the V1 lease/exec API over a Unix socket (`/run/vm-orchestrator/api.sock`); the daemon owns no product submission queue, payment policy, or mutable workload policy.


<guest_rootfs_split>

Firecracker guests boot from a slim **substrate** ext4 and compose
read-only **toolchain images** at lease boot. The catalog lives in
`src/substrate/ansible/group_vars/all/generated/ops.yml:firecracker_seed_images`; each
entry declares a `tier` of `substrate`, `platform_toolchain`, or
`customer_uploaded`, and the `vm-orchestrator-seed.service` oneshot
materialises every entry via `vm-orchestrator-cli seed-image` (one
SeedImage RPC per image, idempotent via `vs:source_digest` on
`@ready`).

- **Substrate** (`/var/lib/verself/guest-images/substrate.ext4`):
  kernel + minimal Ubuntu userland + vm-bridge as `/sbin/init` +
  vm-guest-telemetry + a few apt deps the runners need at runtime.
  No Go, no Node.js, no GitHub Actions runner, no Forgejo runner, no
  `runner` user. Refreshes only when the kernel, vm-bridge,
  vm-guest-telemetry, or Ubuntu base move. Built by
  `src/vm-orchestrator/guest-images/substrate/build-substrate.sh`,
  runs as root on the deploy host.
- **Toolchain images** (`/var/lib/verself/guest-images/toolchains/<ref>.ext4`):
  Bazel-built ext4 artefacts under
  `//src/vm-orchestrator/guest-images/<ref>:`. Today: `gh-actions-runner`
  (mounted at `/opt/actions-runner`) and `forgejo-runner` (`/opt/forgejo-runner`).
  Each carries `etc-overlay/` (vm-bridge copies into `/etc/` at lease
  boot — adds `runner@1000`, NOPASSWD sudo, profile.d hooks) and
  `.verself-writable-overlays` (vm-bridge tmpfs-mounts each listed path
  on top of the read-only base — e.g. `/opt/actions-runner/_work`).
- **Bazel macro** `toolchain_ext4_image` in
  `//src/vm-orchestrator/guest-images:guest_image.bzl` runs `mkfs.ext4 -d`
  hermetically with deterministic UUID + hash_seed +
  SOURCE_DATE_EPOCH=0, so byte-for-byte rebuilds reproduce the same
  sha256 and deploy-time re-seeds are no-ops when nothing changes.
- **Runner class → mount list** lives in
  `runner_class_filesystem_mounts` (sandbox-rental Postgres). Each
  runner_class names the toolchain images it wants; sandbox-rental
  resolves them into `LeaseSpec.FilesystemMounts` at acquire time.
  Sticky-disk mounts (caches, persistent workspace) are per-execution
  and arrive via `StartExecRequest`, not this table.
- **Customer-uploaded images** land later under
  `tier: customer_uploaded`. The seed path is uniform — whether the
  source artefact is a Bazel rule or a customer upload, the daemon's
  privilege boundary doesn't change.

</guest_rootfs_split>

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
