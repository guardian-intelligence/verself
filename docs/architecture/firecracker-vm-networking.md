# Firecracker VM Networking

Firecracker CI networking uses a host-managed allocator over a dedicated guest pool:

- one configurable IPv4 guest pool, default `172.16.0.0/16`
- one `/30` lease per VM
- one TAP per lease
- one persistent host-owned NAT and forwarding policy for the entire guest pool
- no DHCP, no CNI, no Linux bridge management, no network namespaces in this phase

## Topology

```text
Bare-Metal Host
│
├── uplink: eth0
│   └── iptables
│       ├── MASQUERADE 172.16.0.0/16 -> eth0
│       ├── allow guest egress
│       └── drop guest -> guest east-west traffic
│
├── lease dir: /var/lib/ci/net/leases
│   ├── 000000.json  -> 172.16.0.0/30
│   ├── 000001.json  -> 172.16.0.4/30
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

- The allocator is the source of truth for IP uniqueness. Lease state is persisted on disk so multiple `forge-metal` processes can coordinate safely.
- Per-job runtime only creates and deletes TAP devices. It does not mutate host-wide firewall state.
- Recovery is best-effort. On startup, stale lease files are reconciled against live TAP devices and recorded PIDs.
- Guests still use static kernel boot args, so the guest image stays simple and unaware of host network orchestration.

## Why not CNI or `tc-redirect-tap` yet

Those approaches solve more general problems than we need right now. The current goal is bounded concurrent Firecracker CI on a single worker with deterministic cleanup and low operational complexity. A slot allocator with host-managed TAP devices gets us there without introducing another control plane.
