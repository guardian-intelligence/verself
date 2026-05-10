# ZFS Volume Lifecycle

vm-orchestrator owns privileged ZFS lifecycle for Firecracker leases. Product
services pass service-authorized refs over the Unix socket; guests never pass
ZFS dataset names, host paths, or mutation intents.

## Dataset Roots

Default roots under the configured pool:

| Root | Purpose |
| --- | --- |
| `images/` | Read-only composable toolchain images seeded by `SeedImage`. |
| `workloads/` | Ephemeral per-lease root disks and writable mount clones. |
| `goldens/` | Immutable golden environment generations committed after a successful job. |

`EnsureRoots` creates those roots at daemon startup. Ansible configures the host
and service unit; it does not issue runtime ZFS mutations for workloads.

## Lease Boot

1. sandbox-rental builds a `LeaseSpec` containing the substrate image and any
   boot-time filesystem mounts.
2. vm-orchestrator clones the substrate snapshot into `workloads/<lease>/root`.
3. For each filesystem mount, vm-orchestrator either clones the selected golden
   generation snapshot or creates a fresh ext4 zvol on a cache miss.
4. vm-orchestrator waits for every `/dev/zvol/<dataset>` node, jailer-binds the
   devices, starts Firecracker, and sends the filesystem manifest to vm-bridge.
5. vm-bridge mounts the declared filesystems before the runner process starts.
6. vm-bridge returns per-filesystem mount results. Required mount failures
   fail lease acquisition; optional cache mount failures are reported to the
   product service as degraded cache state.

There is no dynamic block-device attach path after the guest boots. This keeps
Firecracker device topology static and makes mount availability part of lease
acquisition rather than a guest-originated side effect.

## Commit

After the runner exits, sandbox-rental may ask vm-orchestrator to commit a named
writable filesystem mount. The commit path:

1. Seals the guest mount through vm-bridge.
2. Flushes the host block device.
3. Snapshots the lease mount clone.
4. Clones that snapshot to `goldens/<scope>/generations/<generation>`.
5. Promotes the clone so the immutable generation no longer depends on the
   ephemeral lease dataset.
6. Creates `@sealed` on the promoted generation and returns the full snapshot
   ref plus used/written byte counters.

Postgres records the operation and promotion decision. ZFS runs only in
vm-orchestrator; sandbox-rental records observed results and advances current
pointers with compare-and-swap.

## Retention

Committed generations are immutable. A generation that loses promotion CAS is
retained as an unreferenced candidate until a retention worker destroys its
`goldens/<scope>/generations/<generation>` dataset. Destroy operations remain
host-owned RPCs or daemon-local maintenance tasks; product services request
destruction by generation ref over the vm-orchestrator Unix socket and never
shell out to `zfs`.
