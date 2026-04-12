# Firecracker VM Networking

Firecracker workload networking uses a host-managed allocator over a dedicated guest pool:

- one configurable IPv4 guest pool, default `172.16.0.0/16`
- one `/30` network slot per VM
- one TAP per slot
- one persistent host-owned NAT and forwarding policy for the entire guest pool
- one durable SQLite WAL ledger at `/var/lib/forge-metal/vm-orchestrator/state.db`
- one host-only service address, default `10.255.0.1/32` on `fm-host0`
- no DHCP, no CNI, no Linux bridge management, no network namespaces in this phase

This document is about run networking, not submission orchestration. A run gets a slot,
TAP device, and host-service address; the VM bridge then drives the run through
its control protocol and telemetry stream.

## Topology

```text
Bare-Metal Host
│
├── uplink: eth0
│   └── nftables
│       ├── MASQUERADE 172.16.0.0/16 -> eth0
│       ├── allow guest egress
│       └── drop guest -> guest east-west traffic
│
├── host service plane
│   ├── fm-host0: 10.255.0.1/32
│   ├── Caddy: 10.255.0.1:18080 -> Forgejo 127.0.0.1:3000
│   └── Verdaccio: 10.255.0.1:4873
│
├── host runtime ledger: /var/lib/forge-metal/vm-orchestrator/state.db (WAL)
│   ├── network_slots[0] -> 172.16.0.0/30
│   ├── network_slots[1] -> 172.16.0.4/30
│   └── ...
│
├── TAP devices
│   ├── fc-tap-0  -> 172.16.0.1/30
│   ├── fc-tap-1  -> 172.16.0.5/30
│   └── ...
│
├── Firecracker VM A
│   └── eth0
│       ├── guest IP: 172.16.0.2
│       └── gateway:  172.16.0.1
│
└── Firecracker VM B
    └── eth0
        ├── guest IP: 172.16.0.6
        └── gateway:  172.16.0.5
```

## Operational Notes

- The allocator is the source of truth for slot/IP uniqueness. Slot state is persisted in SQLite WAL so concurrent allocator calls and daemon restarts recover deterministically.
- Per-run runtime only creates and deletes TAP devices. It does not mutate host-wide firewall state.
- Recovery is ledger-first and host-probe-second. On startup, allocated slots are reconciled against live TAP devices plus `(pid,start_ticks)` metadata to avoid PID reuse ambiguity.
- Guest networking requests are never host authority signals; the host allocator and host firewall policy define the effective network state.
- Guests still use static kernel boot args, so the guest image stays simple and unaware of host network orchestration.
- Guests do not reach host loopback through DNAT. Host-local platform services are exposed through `fm-host0`, and host firewall rules match `fc-tap-*` plus destination `10.255.0.1`.

## Bridge And Telemetry Proof

Networking proofs are not just IP reachability checks. The maintained proof targets
exercise the full run path:

- `make vm-orchestrator-proof` confirms the healthy baseline run path and records ClickHouse traces/logs for the successful run lifecycle.
- `make vm-orchestrator-proof-gap` injects a telemetry sequence gap and expects a `gap` diagnostic in the host event stream plus matching ClickHouse log evidence.
- `make vm-orchestrator-proof-regression` injects a telemetry regression and expects the orchestrator to drop the regression from the emitted run stream while recording `regression` diagnostics and logs.
- `make vm-orchestrator-proof-bridge-fault` injects a vm-bridge protocol fault and expects deterministic protocol-violation handling from the explicit control states, with trace/log evidence in ClickHouse.

For all four proofs, the required evidence is the same shape:

- ClickHouse traces for the run lifecycle and bridge/telemetry handling
- ClickHouse logs for diagnostics, protocol violations, and cleanup breadcrumbs
- host state that can be correlated back to the run ID and the affected slot/TAP allocation

## Why not CNI or `tc-redirect-tap` yet

Those approaches solve more general problems than we need right now. The current goal is bounded concurrent Firecracker workloads on a single worker with deterministic cleanup and low operational complexity. A slot allocator with host-managed TAP devices gets us there without introducing another control plane.
