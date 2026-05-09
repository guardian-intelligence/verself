# Verself Checkpoint parity log

What went into prod and where the wall-clock time is going next.

The authoritative source for "did this help" is
`verself.checkpoint_operations`, written by sandbox-rental on every
mount/save phase. It already carries `repository_full_name`,
`runner_class`, `provider`, `provider_run_id`, `provider_job_id`,
`scope_ref`, `key_hash`, and per-phase `duration_ms`, so wall-clock
breakdown by repo, runner class, provider, and operation is one
GROUP BY away. There is no separate benchmark fact table.

## Phases shipped

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
    generation identity, NOT NULL + UNIQUE.

Receive intentionally omits `-F` so a concurrent commit's lost-
receive-race surfaces as a `promotion_conflict` instead of being
silently overwritten.

### Phase 2 — Dogfood verself-ci.yml

`.github/workflows/verself-ci.yml` runs the same Bazel build + test as
`github-ci.yml` (passthrough) and `blacksmith-ci.yml`, but mounts
Bazel disk_cache, repository_cache, and the pnpm store via
`useverself/checkpoint@v0`. Every PR/push produces three comparable
samples; the comparison query lives directly on
`checkpoint_operations` filtered by `repository_full_name`.

### Phase 4 — Wrapped setup actions

Composite actions `setup-node`, `setup-go`, `setup-python`,
`cache-bazel` wrap the upstream `actions/setup-*` and call
`useverself/checkpoint@v0` for the canonical cache paths. Customers
migrating from `useblacksmith/setup-*` keep the same drop-in shape.

### Phase 5 — Per-component-kind sizing

The action's empty-mount size is now an input (`size-gb`, default 5,
range 1..200). `cache-bazel` defaults to 50 GiB for disk_cache and
10 GiB for repository_cache. `verself-ci.yml` uses 50 / 10 / 8 for
Bazel disk, Bazel repo, pnpm store. The size only applies to first-
save (cold) paths; restore inherits source generation size.

## Querying the data

Repository-scoped wall-clock breakdown:

```sql
SELECT
    repository_full_name,
    runner_class,
    operation,
    result,
    count(*)                       AS n,
    quantile(0.5)(duration_ms)     AS p50_ms,
    quantile(0.95)(duration_ms)    AS p95_ms
FROM verself.checkpoint_operations
WHERE observed_at > now() - INTERVAL 7 DAY
GROUP BY repository_full_name, runner_class, operation, result
ORDER BY repository_full_name, runner_class, operation;
```

Cache hit ratio per repo:

```sql
SELECT
    repository_full_name,
    countIf(operation = 'source_select' AND result = 'hit')  AS hits,
    countIf(operation = 'source_select' AND result = 'miss') AS misses,
    hits / (hits + misses)                                    AS hit_ratio
FROM verself.checkpoint_operations
WHERE observed_at > now() - INTERVAL 7 DAY
  AND operation = 'source_select'
GROUP BY repository_full_name;
```

Pre/post comparison of save cost (Phase 1 was deployed 2026-05-09):

```sql
SELECT
    if(observed_at < '2026-05-09 10:00:00', 'pre', 'post') AS phase,
    repository_full_name,
    quantile(0.5)(duration_ms)  AS p50_save_ms,
    quantile(0.95)(duration_ms) AS p95_save_ms,
    avg(used_bytes)             AS avg_used_bytes
FROM verself.checkpoint_operations
WHERE operation = 'commit' AND result = 'succeeded'
GROUP BY phase, repository_full_name;
```

Workflow-level latency (uses `provider_run_id` to align with the
GitHub Actions run):

```sql
SELECT
    repository_full_name,
    provider_run_id,
    sum(if(operation = 'host_prepare',
           duration_ms, 0))      AS restore_ms,
    sum(if(operation IN ('commit', 'promotion'),
           duration_ms, 0))      AS save_ms
FROM verself.checkpoint_operations
WHERE result = 'succeeded'
  AND repository_full_name = 'guardian-intelligence/verself'
  AND observed_at > now() - INTERVAL 7 DAY
GROUP BY repository_full_name, provider_run_id
ORDER BY provider_run_id DESC
LIMIT 50;
```

## Phase 6 — Optimization queue

What remains on the table after the architectural fix lands. Each
entry below is sized as "expected wall-clock impact" and "where it
fits structurally", with the goal of measuring against
`checkpoint_operations` rather than asserting wins on theory.

**1. Single-RPC mount lifecycle.** Today the mount path is action →
sandbox-rental HTTP `/checkpoints/mount` → vm-orchestrator gRPC
`AttachFilesystemMount` → vm-bridge vsock filesystem mount → action
HTTP `/checkpoints/mounted`. Roll the second HTTP round-trip into a
host-initiated transition. Expected: 50–150 ms shaved per Checkpoint
mount. Materially helpful only when a workflow mounts ≥3 Checkpoints,
which is the wrapped-action world (cache-bazel = 2; setup-go = 2).

**2. Drive-slot model: virtio block hot-plug instead of sparse-file
PATCH.** Today: 16 sparse `checkpoint-reserve-NN.img` placeholders
pre-allocated at jail boot, swapped via Firecracker `PATCH /drives`
after the zvol is placed. The sparse files plus the PATCH+rescan
dance account for ~100 ms per slot used. Replacing with virtio block
hot-plug (or verified PATCH+rescan) is fewer moving parts and the
"guest sees zeros until rescan" failure mode goes away.

**3. ZFS dataset-level compression on `vspool/checkpoints/*`.** The
current pool inherits default compression. Setting `compression=zstd-3`
or `lz4` per-dataset on the checkpoint root buys 30–60% size reduction
on most cache content (Bazel disk caches compress especially well),
which translates directly into faster send-side I/O for cold saves
and faster receive on warm restores. ZFS handles this transparently.

**4. ZFS recordsize per component_kind.** Default recordsize 128k is
fine for sequential workloads but pessimal for random-read patterns
like Bazel repository_cache lookups (8k-64k reads against many small
files). Setting `recordsize=16k` on the repository_cache volumes and
`recordsize=128k` on disk_caches matches each tool's IO shape. Apply
at first-save creation — recordsize is a property of the dataset.

**5. Lazy mkfs.** The empty-mount path runs `zfs create` + `mkfs.ext4`
per cache miss; mkfs is the long pole. We can pre-build a tiny
"empty-cache.ext4" image, snapshot it once, and clone-from-snapshot
for cold mounts. ~300 ms to ~1 s saved on cold starts, traded for
zero-cost clone of a 0-byte-referenced snapshot.

**6. Prefetch on runner allocation, not action invocation.** Today,
the runner boots, the workflow runs `useverself/checkpoint@v0`, and
THEN sandbox-rental resolves the volume + asks vm-orchestrator to
clone + asks vm-bridge to mount. The clone+mount path can run earlier
— at `runner_allocations` time we know the workflow's repository,
branch, and commit, which is enough to predict the Checkpoint keys
the workflow will request. Expected: the action's mount step becomes
"ack already-cloned mount" in <50 ms instead of ~500–1500 ms today.
Speculative — wrong predictions are cheap (clone is O(1)).

**7. ZFS holds for in-flight clones.** Right now the retention sweeper
relies on ZFS's clone-dependency check to refuse `zfs destroy
<snapshot>` when an active clone references it. That works but the
failure surfaces as a generic error; the sweeper marks the gen as
'failed' and gives up. Adding `zfs hold` / `zfs release` on the source
snapshot for the clone's lifetime makes the dependency explicit, lets
the sweeper distinguish "in-flight clone" from "real failure", and
gives a clean retry path.

**8. ZFS dataset quota per volume.** Today there's no quota
enforcement on a Checkpoint volume's dataset. A runaway customer
workload can fill the pool. `zfs set quota` per volume bounds the
worst case to `size_gb` even if the customer ignores the action
input. Apply at volume creation; no runtime cost.

### Deferred / explicitly out of scope

- **Default workspace Checkpoint** (auto-mount of `/home/runner/_work`
  without action involvement). Breaks the customer-authored authority
  model and adds an opaque inventory class. Customers invoke
  `useverself/checkpoint@v0` like Blacksmith's
  `useblacksmith/stickydisk@v1`.

- **Promote+rename instead of incremental send.** Equivalent steady-
  state cost, more concurrency edge cases. Snapshot-chain +
  incremental send dominates on simplicity for the same big-O.

- **Shadow GitHub Actions cache service.** Provider passthrough stays
  provider-owned. Verself archive cache is the explicit opt-in
  alternative.

- **Separate benchmark fact table.** `checkpoint_operations` already
  carries the dimensions needed for repo-scoped wall-clock queries.
  Building a parallel `benchmark_runs` table just smeared the same
  data across two stores and added a canary→ingest plumbing hop.

## Cadence

Each session ends with: a commit, a deploy, and a one-paragraph entry
below describing what changed. The data is queried directly from
`checkpoint_operations`; no separate ingestion job exists.

### 2026-05-09

Phases 1, 2, 4, 5 deployed to prod. `vspool/checkpoints` exists.
Sandbox-rental v33 running on the new schema. vm-orchestrator
restarted with `--checkpoint-dataset checkpoints`. Migration 5 was
initially dirty because the original migration had explicit
`BEGIN/COMMIT` inside golang-migrate's outer transaction; cleared by
hand and the migration file rewritten.

Tore down the parallel benchmark_runs table, the
`aspect-operator checkpoint-canary` / `benchmark-ingest` /
`bench-ci-runs` commands, and the `verself-checkpoint-canary*.yml`
workflows. The same questions are answered by
`SELECT … FROM verself.checkpoint_operations` filtered by
`repository_full_name`, with no additional ingest plumbing.
