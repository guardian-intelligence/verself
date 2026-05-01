# Volume Lifecycle

Per-customer durable storage for sticky disks, workspace generations, long-lived
VM data volumes, and later service-data volumes. The product abstraction is
**volume**; the current implementation uses OpenZFS zvol-backed datasets and
snapshots. Customer APIs, browser UI, billing descriptions, IAM policies, and
support language must not expose zvols, datasets, pool names, host paths,
device paths, or Firecracker/jailer arguments.

Lifecycle ownership is split:

- sandbox-rental-service owns customer-visible volume records, generation
  records, IAM, billing policy, idempotency, customer APIs, and ClickHouse
  billing/observability rows.
- vm-orchestrator owns privileged host execution: ZFS datasets, zvol devices,
  snapshots, clones, channel programs, TAP networking, Firecracker, jailer
  lifecycle, and guest telemetry.

This document describes the target architecture. Not every path exists yet.

## What we deliberately did not build

- No custom time/count GC daemon. zrepl in `snap` job mode owns the first prune
  loop for `short_7d`, `medium_14d`, and `long_30d`.
- No `lru_capped` first cut. Byte-cap LRU depends on `last_used_at` and
  tenant-level accounting, which zrepl pruning rules do not encode. Add it
  later as a sandbox-rental retention reconciler if product policy needs it.
- No retention DSL. Named retention policies are code-and-migration owned.
  Customer editing can choose among exposed policy names; new policy semantics
  require a migration.
- No CAS primitive on ZFS user properties. Postgres serializable transactions
  are the control-plane fence.
- No refcount graph. `last_used_at`, generation state, and named retention are
  sufficient for the single-node product.
- No per-dataset encryption yet. Defer until `zfs send -w` to off-node backup
  is on the critical path.

## Product Boundary

The customer-facing concepts are:

- `volume`: stable product object owned by one Verself organization.
- `volume_generation`: immutable point-in-time generation produced by a commit.
- `current_generation`: trust-class-scoped pointer used to choose the default
  readable generation.
- `retention_policy`: named policy controlling how long non-current generations
  remain available.
- `storage_usage`: measured durable storage at rest, derived from authoritative
  ZFS properties.

There is no current customer-facing durable-volume API. The only live product
path today is the internal sticky-disk lifecycle used by GitHub runner
executions. If a long-lived VM data-volume product returns later, sandbox-rental
should expose it as a product-owned API without leaking storage node IDs, pool
IDs, dataset refs, snapshot refs, zvol device paths, host mount paths, or
orchestrator implementation names.

## Privilege Boundary

vm-orchestrator is the only runtime process allowed to hold host privileges for
ZFS and Firecracker. The Unix socket group (`vm-clients`) is therefore a
privileged capability boundary, not a general integration group. Membership must
be audited by Ansible and kept to the smallest set of internal control-plane
callers; for the current single-node product that set is `sandbox_rental`.

Customers, browser frontends, workers, webhook handlers, and guest code carry
service-authorized refs only. sandbox-rental resolves tenant policy,
idempotency, trust class, and generation selection in Postgres, then calls
vm-orchestrator with typed lifecycle intents. vm-orchestrator resolves those
intents to contained dataset paths under configured roots and performs the
privileged host mutations.

## ZFS Orchestration Package

ZFS lifecycle logic should be a separate Go package inside vm-orchestrator, not
a separate privileged daemon in the first cut. A second root process would add
another socket capability boundary without reducing the current trust surface.

Target package shape:

```
src/vm-orchestrator/zfs/
  lifecycle.go          typed VolumeLifecycle facade
  paths.go              dataset/snapshot constructors and containment checks
  stats.go              batched zfs get parsing
  channel_program.go    zfs program invocation and limits
  channel-programs/
    rotate_generation.lua
    snapshot_live.lua
```

The package owns:

- path construction from typed refs
- dataset/snapshot name validation
- tenant-root containment checks
- channel program invocation
- ZFS property collection
- conversion from ZFS command output to typed Go values

It does not own product policy, billing, tenant state, idempotency, or public
API DTOs.

## Dataset Hierarchy

```
pool/tenants/<org_id>/
  volumes/<component_kind>/<key_hash>/
    gen-<monotonic>           clone dataset for the generation
    gen-<monotonic>@live      snapshot that locks it as immutable
  fork-pr-scoped/<component_kind>/<key_hash>/<pr_id>/gen-N
```

- `refquota` on `pool/tenants/<org_id>` bounds live bytes per tenant.
- `quota` on `pool/tenants/<org_id>` bounds live + retained bytes so retention
  cannot pin unbounded storage.
- No service user receives `zfs allow`, `/dev/zvol`, `/dev/kvm`, TAP, jailer,
  or Firecracker privileges.
- Fork-PR writes land in a parallel subtree. The trust class is encoded in the
  path so accidental cross-class promotion is structurally impossible.

## Durable Model

sandbox-rental-service owns the product state:

- `volumes(volume_id, org_id, product_kind, component_kind, key_hash,
  display_name, retention_policy, state, created_at, updated_at, last_used_at)`
  is the stable customer-visible object.
- `volume_generations(volume_generation_id, volume_id, org_id,
  component_kind, key_hash, generation, parent_generation_id, kind,
  zfs_dataset, zfs_snapshot, trust_class, retention_policy, used_bytes,
  usedbysnapshots_bytes, written_bytes, state, created_by_attempt, created_at,
  last_used_at, expires_at)` is the immutable generation ledger.
- `volume_current_generation(org_id, volume_id, trust_class,
  current_generation, generation_id, updated_at)` stores a separate current
  pointer per trust class.
- `retention_policies(policy_name, ttl_from_last_use, keep_last_n)` seeds
  `short_7d`, `medium_14d`, and `long_30d`.

Sticky disks are the first consumer. `runner_sticky_disk_generations` and
`execution_sticky_disk_mounts.committed_generation` collapse into this model in
the same migration. There is no compatibility shim.

## Generation Semantics

Volumes are generation-first:

1. sandbox-rental chooses a readable base generation for the mount.
2. vm-orchestrator clones that generation into the lease as a writable zvol.
3. The guest writes to the mounted filesystem.
4. On commit, vm-orchestrator seals and flushes the guest filesystem, snapshots
   the lease mount, and creates a new immutable generation dataset/snapshot.
5. sandbox-rental records the generation and attempts current-pointer rotation
   inside a serializable transaction.

A commit produces an immutable generation even if it loses the current-pointer
race. The losing rotation emits `runtime.volume.rotation_lost` so cache thrash
is visible in ClickHouse instead of being hidden as a failed save.

## Trust Classes

Trust class is computed inside sandbox-rental from persisted, validated GitHub
installation/webhook/run context. vm-orchestrator receives the trust class as an
opaque label and enforces only the dataset-path containment implied by it.

If the GitHub event context is incomplete, the request must fail closed: deny a
writable protected mount or classify the write as `fork_pr` if a scoped fork
volume is explicitly allowed.

| Class | Reads | Writes | Rotates `current` |
|---|---|---|---|
| `protected` | protected + same_repo_pr + operator | new protected generation | yes, protected pointer |
| `same_repo_pr` | protected + operator | same_repo_pr generation | yes, same_repo_pr pointer |
| `fork_pr` | operator + this PR's fork-pr-scoped subtree | fork-pr-scoped generation | never |
| `operator` | everything | operator generation | yes, operator pointer |

This mirrors branch-write allowlists used by CI cache-volume systems: untrusted
fork code can consume safe bases but cannot poison protected caches.

## Atomic ZFS Rotation

vm-orchestrator runs small ZFS multi-op rotations as Lua channel programs
(`zfs-program(8)`, OpenZFS >= 0.8). Channel programs are appropriate for
contained host mutations such as `snapshot + set userprop + promote/destroy`.
They are not a policy engine.

Rules:

- Keep channel programs short, deterministic, and idempotent.
- Never pass customer strings directly to a channel program. Pass only typed
  refs that have already been converted to contained dataset/snapshot paths.
- Do not run retention selection, billing, IAM, or customer state transitions in
  Lua.
- Treat Postgres as the control-plane ordering fence and channel programs as
  the on-disk ordering fence.

OpenZFS channel programs execute atomically relative to other administrative
operations, but fatal instruction or memory limits can leave partial execution.
The recovery path is therefore: re-read ZFS facts, compare them to Postgres
generation state, and reconcile loudly.

## Garbage Collection

zrepl runs as a sibling daemon under the platform Ansible roles. Per-tenant
subtrees get `snap` jobs whose prune rules are templated from named retention
policies.

zrepl is used for time/count retention only in the first cut:

- `short_7d`
- `medium_14d`
- `long_30d`

`lru_capped` is deferred. Implement it later only if the product needs
tenant-byte-cap LRU retention; that reconciler belongs in sandbox-rental because
it depends on product state (`last_used_at`, org policy, billing state), not
only on ZFS snapshot names.

When a generation is superseded but still needed as a `zfs send -i` cursor,
convert snapshot to bookmark (`zfs-bookmark(8)`) to reclaim
`usedbysnapshots` bytes without losing the incremental anchor. This is backup
plumbing, not part of the first customer API.

## Billing Model

Durable volume storage is billed as storage at rest. The internal accounting
unit is GiB-ms because billing-service windows already rate SKU allocation by
millisecond quantity. Product pricing and invoices may display GiB-second,
GiB-hour, GiB-day, or GiB-month rates. This mirrors cloud block-storage
pricing, where durable block capacity is charged over time and
higher-performance disks may add provisioned IOPS/throughput meters.

First-cut volume SKUs:

- `sandbox_durable_volume_live_storage_gib_ms`: top-line tenant storage minus
  retained snapshot bytes, clamped at zero.
- `sandbox_durable_volume_retained_snapshot_gib_ms`: retained generation/snapshot
  tail.

Execution root disks use the separate
`sandbox_execution_root_storage_premium_nvme_gib_ms` SKU and the
`execution_root_storage` bucket. Durable volume bytes use the
`durable_volume_storage` bucket. Do not collapse those buckets unless the
commercial entitlement policy intentionally treats ephemeral execution disks and
retained customer data as the same allowance.

Do not charge ingress/egress through the durable volume SKU. Network egress,
cross-node replication traffic, and backup transfer are separate future meters.
Guest block read/write IO and Firecracker network IO remain execution telemetry
until a specific product SKU needs performance-based charging.

For clone-backed, operator-owned bases, the authoritative ZFS properties are:

| Property | Role |
|---|---|
| `used` on tenant subtree root | Top-line physical tenant footprint; includes descendants and snapshot-retained bytes, excludes blocks shared with clone origins, and matches the quota signal. |
| `usedbysnapshots` | Retention-tail bytes. When billed as a separate line item, subtract this component from top-line `used` before billing live storage so the same bytes are not charged twice. |
| `written@<parent_snap>` | Per-generation incremental delta for evidence and debugging. |

`referenced` is wrong for clone-backed billing because it can include shared
base data. `logical*` is wrong unless the customer contract bills
pre-compression bytes; it does not.

For future customer-sized long-running VM data volumes, the contract may add a
provisioned-capacity meter (`volume_provisioned_gib_ms`) because that is
the industry-standard block-volume model. Sticky disks and managed workspace
volumes should start with measured at-rest bytes because they are product-owned,
thin, copy-on-write cache volumes rather than customer-sized raw disks.

If durable-storage billing returns, the meter sweep should reserve and settle an
exact billing-service time window for the sweep duration and emit product-owned
evidence without making ClickHouse the lifecycle source of truth. The product
layer owns the consequence of a failed storage billing window; billing-service
stays agnostic and reports the denied authorization.

Billing-service accepts caller-configurable `WindowMillis` on reservation. A
zero value preserves the default reservation duration; nonzero values choose the
millisecond quantity used for authorization, `reserved_quantity`, expiry, and
projection. Charge rounding happens after quantity:
`ceil(allocation * quantity * unit_rate)`. Autobuilder, Routines, long-running
VMs, sticky volumes, retained generations, and future storage products all reuse
the same billing window machinery but may need different reservation durations:

- VM executions can continue reserving multi-minute windows if that is the
  right credit-lock behavior for active workloads.
- Volume meter sweeps should reserve and settle the exact sweep interval, for
  example 60 seconds, to avoid over-holding customer credits.
- Routines may reserve per loop period or per active execution slice.
- Autobuilder can reserve per CI job attempt while durable cache volumes meter
  independently at rest.

## ClickHouse, Grafana, And Customer Usage API

ClickHouse is the evidence and analytics store, not the lifecycle source of
truth.

There is no live dedicated product projection table for durable-volume metering
today. If that product returns, the ClickHouse ingestion path must stay
idempotent by a deterministic meter-tick ID so replay cannot double count.
Partitioning should use a stable domain timestamp such as `window_start` or
`observed_at`, not an insertion timestamp.

Grafana dashboards and any future customer usage history should read a deduped
projection over those meter ticks, while inventory and lifecycle state continue
to come from sandbox-rental Postgres.

The Grafana dashboard should show:

- live bytes by tenant
- retained snapshot bytes by tenant
- total durable volume GiB-ms by tenant, with panel units converted for human
  display where useful
- top volumes by live bytes
- top volumes by retained bytes
- quota/refquota utilization
- generation commits and rotation-lost counts
- destroyed generation counts

## Code Pointers

- `src/vm-orchestrator/zfs/` - target package for channel programs and the
  `VolumeLifecycle` Go wrapper.
- `src/vm-orchestrator/orchestrator.go` - lease mount preparation currently
  resolves image refs to ZFS clones.
- `src/vm-orchestrator/filesystem_commit.go` - current commit RPC uses inline
  snapshot/send/receive; generation rotation replaces it.
- `src/sandbox-rental-service/migrations/` - current sticky-disk control-plane
  tables and any future volume-control-plane schema if a customer volume
  product returns.
- `src/substrate/ansible/roles/grafana/` - provisioned dashboard target.

## Primary References

- AWS EBS pricing: durable block volume storage is charged by provisioned GB per
  month, with optional provisioned IOPS/throughput for higher-performance
  volumes: https://aws.amazon.com/ebs/pricing/
- Google Compute Engine disk cost considerations: Local SSD, Persistent Disk,
  and Hyperdisk bill provisioned storage capacity from creation until deletion;
  Hyperdisk/Extreme may also bill provisioned performance:
  https://cloud.google.com/compute/docs/disks
- OpenZFS `zfsprops(7)` for `used`, `usedbysnapshots`, `referenced`, and
  `written`: https://openzfs.github.io/openzfs-docs/man/v2.2/7/zfsprops.7.html
- OpenZFS `zfs-program(8)` for channel program atomicity and resource-limit
  behavior: https://openzfs.github.io/openzfs-docs/man/master/8/zfs-program.8.html
- zrepl pruning rules: https://zrepl.github.io/configuration/prune.html
- containerd snapshotter metastore:
  `github.com/containerd/containerd/core/snapshots/storage/bolt.go`
- containerd/zfs reference snapshotter:
  `github.com/containerd/zfs/zfs.go`
- Namespace cache volumes:
  https://namespace.so/docs/architecture/storage/cache-volumes
