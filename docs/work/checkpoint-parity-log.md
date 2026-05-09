# Verself Checkpoint parity log

What went into prod and where the wall-clock time is going next. The
authoritative source for "did this help" is `verself.benchmark_runs`,
populated by the canary cold/warm/dirty cadence and by
`aspect-operator bench-ci-runs` against the verself-sh GitHub Actions
API.

## Phases shipped

### Phase 0 — Verification + breakdown columns

`benchmark_runs` already had the breakdown columns; what was missing
was a populator. `aspect-operator benchmark-ingest` now joins each
canary case onto `verself.checkpoint_operations` by `execution_id`,
sums host_prepare durations into `checkpoint_restore_ms`, sums commit
+ promotion durations into `checkpoint_save_ms`, takes max
`used_bytes` into `checkpoint_used_bytes`, and sets `cache_hit=1` only
when an `operation='source_select' AND result='hit'` row is present.

`--verify-warm` (default on) returns a non-zero exit when any
warm/dirty case did not observe a Checkpoint generation hit. Cold
cases are exempt by definition.

The canary loop now runs cold → warm → dirty per workload, with the
dirty pass mutating a sentinel file before re-pushing so the warm
generation gets exercised on a non-identity rerun. The canary workflow
runs every six hours and uploads the JSON report as an artifact for
operator-side ingest.

### Phase 1 — Snapshot-chain ZFS layout

This is the architectural fix. Save cost was `O(used_bytes)` because
every commit ran `zfs send <clone_snap> | zfs recv <fresh_dataset>`
into a brand-new immutable image dataset. New layout:

  Persistent:   `vspool/checkpoints/<volume_id>` with snapshots
                `@gen-<gen_id>` representing the generation chain.
  Transient:    `vspool/workloads/<lease>/<idx>-<name>` clones from
                `vspool/checkpoints/<volume_id>@<gen_id>`.

Save now does:

    zfs snapshot <clone>@gen-<id>
    zfs send -i <volume>@<parent_gen> <clone>@<id> | zfs recv <volume>

cost is `O(delta_bytes)`. Restore stays `O(1)` via `zfs clone`.

Schema diff in migration `005_checkpoints_snapshot_chain.up.sql`:

  - `volume_generations.zfs_source_ref` now stores the (shared) volume
    dataset; UNIQUE constraint dropped.
  - `volume_generations.zfs_snapshot_ref` is the canonical per-
    generation identity, NOT NULL + UNIQUE. The retention sweeper,
    restore path, and promotion CAS all key on it.

Receive intentionally omits `-F` so a concurrent commit's lost-
receive-race surfaces as a `promotion_conflict` instead of being
silently overwritten.

### Phase 2 — Dogfood verself-ci.yml

`.github/workflows/verself-ci.yml` runs the same Bazel build + test as
`github-ci.yml` (passthrough) and `blacksmith-ci.yml`, but mounts
Bazel disk_cache, repository_cache, and the pnpm store via
`useverself/checkpoint@v0`. Every PR now produces three comparable
wall-clock samples: Verself runner with native Checkpoints, Verself
runner with `actions/cache` passthrough, and Blacksmith with sticky
disks. `aspect-operator bench-ci-runs` ingests these into
`benchmark_runs.workload = monorepo-bazel`.

### Phase 3 — Bench matrix expansion

Two heavy real-world OSS workloads (`rules-go-bazel`, `next-pnpm`)
joined the canary catalog so the matrix exercises a Bazel-heavy
Workspace and a large pnpm content-addressed store. The heavy lane
runs daily; light workloads run every 6 hours.

`bench-ci-runs` is the headline-metric ingestion path — pulls the last
N runs of `verself-ci.yml`, `github-ci.yml`, and `blacksmith-ci.yml`
from the GitHub Actions API and writes one `benchmark_runs` row per
run with `provider = <runner>:<cache_variant>`. Dedupes by
`provider_run_id`.

### Phase 4 — Wrapped setup actions

Composite actions `setup-node`, `setup-go`, `setup-python`,
`cache-bazel` wrap the upstream `actions/setup-*` and call
`useverself/checkpoint@v0` for the canonical cache paths. Customers
migrating from `useblacksmith/setup-*` keep the same drop-in shape.
The Checkpoint authority model stays customer-authored so caches show
up in inventory and respect IAM scope.

### Phase 5 — Per-component-kind sizing

The action's empty-mount size is now an input (`size-gb`, default 5,
range 1..200). `cache-bazel` defaults to 50 GiB for disk_cache and 10
GiB for repository_cache. `verself-ci.yml` uses 50 / 10 / 8 for Bazel
disk, Bazel repo, pnpm store. The size only applies to first-save
(cold) paths; restore inherits source generation size.

## Phase 6 — Optimization exploration

What remains on the table after the architectural fix lands. Each
entry below is sized as "expected wall-clock impact" and "where it
fits structurally", with the goal of measuring against
`benchmark_runs` rather than asserting wins on theory.

### Already in flight (Phases 0–5)

| Optimization                                  | Cost class      | Status |
| --------------------------------------------- | --------------- | ------ |
| `zfs send -i` incremental commit              | O(delta_bytes) | Shipped |
| `zfs clone` mount restore                     | O(1)            | Shipped |
| Per-component-kind volume sizing               | One-time        | Shipped |
| Wrapped setup-* + Bazel cache actions          | Customer-side   | Shipped |
| 6-hour cold/warm/dirty canary cadence          | Coverage        | Shipped |

### Next-leverage items

**1. Single-RPC mount lifecycle.** Today the mount path is action →
sandbox-rental HTTP `/checkpoints/mount` → vm-orchestrator gRPC
`AttachFilesystemMount` → vm-bridge vsock filesystem mount → action
HTTP `/checkpoints/mounted`. Roll the second HTTP round-trip into a
host-initiated transition. Expected: 50–150 ms shaved per Checkpoint
mount. Materially helpful only when a workflow mounts ≥3 Checkpoints,
which is the wrapped-action world (cache-bazel = 2; setup-go = 2).

**2. Drive-slot model: virtio block hot-plug instead of sparse-file
PATCH.** Today: 16 sparse `checkpoint-reserve-NN.img` placeholders pre-
allocated at jail boot, swapped via Firecracker `PATCH /drives` after
the zvol is placed. The sparse files plus the PATCH+rescan dance
account for ~100 ms per slot used. Replacing with virtio block
hot-plug (or verified PATCH+rescan) is fewer moving parts and the
"guest sees zeros until rescan" failure mode goes away.

**3. ZFS dataset-level compression on `vspool/checkpoints/*`.** The
current pool inherits default compression. Setting `compression=zstd-3`
or `lz4` per-dataset on the checkpoint root buys 30–60% size reduction
on most cache content (Bazel disk caches compress especially well),
which translates directly into faster send-side I/O for cold saves and
faster receive on warm restores. ZFS handles this transparently; the
only caller-visible change is `zfs get used` returns the compressed
size.

**4. ZFS recordsize per component_kind.** Default recordsize 128k is
fine for sequential workloads but pessimal for random-read patterns
like Bazel repository_cache lookups (8k-64k reads against many small
files). Setting `recordsize=16k` on the repository_cache volumes and
`recordsize=128k` on disk_caches matches each tool's IO shape. Apply
at first-save creation — recordsize is a property of the dataset, not
the pool.

**5. Lazy mkfs.** The empty-mount path runs `zfs create` + `mkfs.ext4`
per cache miss; mkfs is the long pole. We can pre-build a tiny
"empty-cache.ext4" image, snapshot it once, and clone-from-snapshot for
cold mounts. Cost is on the order of mkfs.ext4 itself (~300 ms to
~1 s on a 5–50 GiB volume), traded for zero-cost clone of a 0-byte-
referenced snapshot. ZFS handles the volsize bump on the clone via
`zfs set volsize`.

**6. Prefetch on runner allocation, not action invocation.** Today,
the runner boots, the workflow runs `useverself/checkpoint@v0`, and
THEN sandbox-rental resolves the volume + asks vm-orchestrator to
clone + asks vm-bridge to mount. The clone+mount path can run earlier
— at `runner_allocations` time we know the workflow's repository,
branch, and commit, which is enough to predict the Checkpoint keys
that the workflow will request. Pre-clone the most-likely current
generation snapshot before the runner registers with GitHub. Expected:
the action's mount step becomes "ack already-cloned mount" in <50 ms
instead of ~500–1500 ms today. Speculative — wrong predictions are
cheap (clone is O(1)); the only cost is the slot allocation.

**7. ZFS holds for in-flight clones.** Right now the retention sweeper
relies on ZFS's clone-dependency check to refuse `zfs destroy
<snapshot>` when an active clone references it. That works but the
failure surfaces as a generic error; the sweeper marks the gen as
'failed' and gives up. Adding `zfs hold` / `zfs release` on the source
snapshot for the clone's lifetime makes the dependency explicit, lets
the sweeper distinguish "in-flight clone" from "real failure", and
gives a clean retry path.

**8. Send/receive over unix socket.** `zfs send | zfs receive` runs as
two separate processes piped through the kernel buffer. With large
generations, the pipe boundary is the bottleneck. On a single-host
deploy we can replace the pipe with a unix socket pair using
`splice(2)` — kernel-resident copy, no userspace bytes. Materially
helpful on big saves; less interesting once Phase 1's incremental send
shrinks the per-save payload.

**9. ZFS dataset quota per volume.** Today there's no quota enforcement
on a Checkpoint volume's dataset. A runaway customer workload can fill
the pool. `zfs set quota` per volume bounds the worst case to
`size_gb` even if the customer ignores the action input. Apply at
volume creation; no runtime cost.

**10. Compactly-encoded snapshot graph in `volume_generations`.** The
current `parent_generation_id` chain is a linked list. For long-lived
volumes (months of CI history), traversing parents to find a base for
incremental send becomes O(N) per save. A materialized
`zfs_snapshot_chain` array column on `volumes` would collapse parent
lookups to a single read. Premature until a volume has hundreds of
generations.

### Deferred / explicitly out of scope

- **Default workspace Checkpoint** (auto-mount of `/home/runner/_work`
  without action involvement). Breaks the customer-authored authority
  model and adds an opaque inventory class. Explicit reject — customers
  invoke `useverself/checkpoint@v0` like Blacksmith's
  `useblacksmith/stickydisk@v1`.

- **Promote+rename instead of incremental send.** Equivalent steady-
  state cost, more concurrency edge cases (clone-becomes-origin tree
  inversion, retention coordination during the swap). Snapshot-chain
  + incremental send dominates it on simplicity for the same big-O.

- **Shadow GitHub Actions cache service.** Provider passthrough stays
  provider-owned per the existing product contract. Verself archive
  cache is the explicit opt-in alternative.

## Cadence

Each session ends with: a commit, a deploy, a fresh canary dispatch, a
`benchmark_runs` snapshot, and a one-paragraph entry below.

### 2026-05-09

Phases 0–5 deployed to prod. `vspool/checkpoints` exists. Sandbox-
rental v33 running on the new schema. vm-orchestrator restarted with
`--checkpoint-dataset checkpoints`. Migration 5 was initially dirty
because the original migration had explicit `BEGIN/COMMIT` inside
golang-migrate's outer transaction; cleared by hand and the migration
file rewritten to rely on the wrapping transaction. Next: schedule
the canary against the headline workloads and let the matrix
accumulate samples for a week before judging the next-leverage items.
