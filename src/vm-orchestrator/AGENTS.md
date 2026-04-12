# vm-orchestrator

Privileged Go daemon managing Firecracker VM lifecycle: ZFS clone/snapshot/destroy, jailer setup, TAP networking, guest agent protocol (vsock), and telemetry aggregation. Exposes a gRPC API over a Unix socket (`/run/vm-orchestrator/api.sock`).

## Guest Networking

Firecracker guests live on a TAP network (`172.16.0.0/16`, one `/30` per VM). Three layers mediate guest-to-host connectivity:

1. **nftables FORWARD chain** (`forge_metal_firecracker`): allows guest egress to internet via the uplink interface. Blocks guest-to-guest lateral movement.

2. **Host-service plane** (`fm-host0`, default `10.255.0.1/32`): a dummy interface exposes selected platform endpoints to guests without routing packets to `127.0.0.1`. Caddy listens on `10.255.0.1:18080` and reverse-proxies Forgejo Git smart-HTTP clone/fetch endpoints to Forgejo's loopback listener. Verdaccio is reached directly at `10.255.0.1:4873`.

3. **nftables INPUT chain** (`forge_metal_host`): default-deny on the host. Guest traffic is accepted only from `fc-tap-*`, only from `nftables_firecracker_guest_cidr`, only to `nftables_firecracker_host_service_ip`, and only on `nftables_firecracker_guest_tcp_ports`.

Do not reintroduce DNAT to `127.0.0.1` or `net.ipv4.conf.*.route_localnet=1` for guest access. Guest scripts receive `FORGE_METAL_HOST_SERVICE_IP` and `FORGE_METAL_HOST_SERVICE_HTTP_ORIGIN`; use those rather than the TAP gateway or loopback addresses.

## Workload Boundary

vm-orchestrator accepts direct VM job commands and host-authorized checkpoint save refs only. Repo import, repo scanning, CI policy, queueing, checkpoint ref policy, and billing semantics belong in the services that own those resources; this daemon stays focused on privileged VM lifecycle, safe ZFS operations, and telemetry aggregation.

Host runtime state is authoritative for VM lifecycle. Guest/control-plane inputs are untrusted requests that must be validated before touching host resources.

Guest event streams are host-derived phase/lifecycle/checkpoint signals; do not add workload-writable billing event channels. Guest checkpoint requests are untrusted input: they may name only a service-authorized ref and must never include ZFS paths, org IDs, or host dataset/version IDs.

## Proof Target

`make vm-orchestrator-proof` is the maintained live-proof entrypoint for this daemon. It runs the firecracker role deploy followed by `playbooks/vm-guest-telemetry-dev.yml`, which boots a Firecracker VM from a hot-swapped telemetry golden and verifies vm-orchestrator telemetry evidence in ClickHouse.

## Shell Scripting Inside Guests

The guest rootfs is Alpine with BusyBox. When constructing shell scripts to run inside VMs:

- `/bin/sh` is BusyBox ash. It supports `${var//pattern/replacement}` parameter expansion.
- Use full paths for system utilities (`/sbin/ip`, not `ip`) when the PATH may not include `/sbin`.
- BusyBox `awk` supports `/pattern/{action;exit}` which is the reliable way to extract a single field.
- Avoid `sed` with dynamic substitution values that may contain shell metacharacters. Prefer ash parameter expansion (`${var//old/new}`) over `sed` for in-script string replacement.
- The `set -eu` flag is recommended. BusyBox ash handles it correctly.
