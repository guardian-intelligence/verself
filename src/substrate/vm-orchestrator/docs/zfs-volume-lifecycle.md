# ZFS Volume Lifecycle

vm-orchestrator owns privileged ZFS lifecycle for Firecracker leases. Product
services pass service-authorized refs over the Unix socket; guests never pass
ZFS dataset names, host paths, or mutation intents.

## Dataset Roots

Default roots under the configured pool:

| Root | Purpose |
| --- | --- |
| `images/` | Read-only composable toolchain images seeded by `SeedImage`. |
| `orgs/<org>/workloads/` | Ephemeral per-lease root disks and writable mount clones under the org quota. |
| `orgs/<org>/goldens/` | Immutable golden environment generations committed after a seal-eligible successful execution under the org quota. |

`EnsureRoots` creates global roots at daemon startup. Lease acquisition creates
the org namespace idempotently and applies the org dataset quota before any
workload or generation zvol is created. Ansible configures the host and service
unit; it does not issue runtime ZFS mutations for workloads.

## Customer Dataset Encryption

Every dataset that may contain customer bytes under `orgs/<org>/workloads/`
or `orgs/<org>/goldens/` is encrypted before guest I/O. The organization root
is the encryption boundary for workload roots, golden roots, lease clones, and
generation zvols. Customer zvol creation and clone preparation must fail if the
target namespace encryption key is unavailable or if the resulting dataset is
not encrypted.

vm-orchestrator is the only runtime process that loads customer ZFS keys. The
keys are host-only operational material and are never returned to product
services, guests, billing records, or public APIs. Image roots can remain
unencrypted when they contain only reproducible platform images.

## Backup Exclusion

Customer zvol snapshots and clones are lifecycle artifacts for lease boot,
seal, promotion, retention, pruning, and placement-affinity replication. They
are excluded from backup and recovery catalogs. A backup job must not run
`zfs send`, object-store upload, or provider-native backup over customer zvol
byte streams. CI golden loss is represented to the product as a cache miss and
rebuild; future non-rebuildable customer storage requires a service-owned
recovery design before release.

## Lease Boot

1. sandbox-rental builds a `LeaseSpec` containing the substrate image and any
   boot-time filesystem mounts.
2. vm-orchestrator clones the substrate snapshot into `orgs/<org>/workloads/<lease>/root`.
3. For each filesystem mount, vm-orchestrator either clones the selected golden
   generation snapshot or creates a fresh ext4 zvol on a cache miss.
4. vm-orchestrator waits for every `/dev/zvol/<dataset>` node, jailer-binds the
   devices, starts Firecracker, and sends the filesystem manifest to vm-bridge.
5. vm-bridge mounts the declared filesystems before the runner process starts.
   Cache filesystems mount once at `/verself/.mounts/<name>` and then bind
   into the declared customer paths. Missing bind-target directories created
   by vm-bridge are owned by the runner user; existing directories keep their
   image-provided ownership and must be empty.
6. vm-bridge returns per-filesystem mount results. Required mount failures
   fail lease acquisition; optional cache mount failures are reported to the
   product service as degraded cache state.

There is no dynamic block-device attach path after the guest boots. This keeps
Firecracker device topology static and makes mount availability part of lease
acquisition rather than a guest-originated side effect.

## Commit

After sandbox-rental has verified that the attempt-specific provider job result
is successful, it may ask vm-orchestrator to commit a named writable filesystem
mount. GitHub runner jobs are gated on the workflow-job terminal conclusion
after the local runner process exits. The commit path:

1. Seals the guest mount through vm-bridge.
2. Flushes the host block device.
3. Snapshots the lease mount clone.
4. Clones that snapshot to `orgs/<org>/goldens/<scope>/generations/<generation>`.
5. Promotes the clone so the immutable generation no longer depends on the
   ephemeral lease dataset.
6. Creates `@sealed` on the promoted generation and returns the full snapshot
   ref plus used/written byte counters.

Postgres records the operation, generation, and product promotion decision. ZFS
runs only in vm-orchestrator; sandbox-rental records observed results and
advances current pointers with compare-and-swap. Successful non-promotable
executions may produce retained generations, but provider-gated promotion of
protected branch pointers happens outside the host commit path.

## Retention

Committed generations are immutable. A generation that loses promotion CAS is
retained as an unreferenced candidate until a retention worker destroys its
`orgs/<org>/goldens/<scope>/generations/<generation>` dataset. Destroy
operations remain host-owned RPCs or daemon-local maintenance tasks; product
services request destruction by generation ref over the vm-orchestrator Unix
socket and never shell out to `zfs`.
