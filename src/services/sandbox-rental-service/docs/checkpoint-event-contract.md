# Checkpoint Event Contract

Verself Checkpoints v0 are action-driven mounted filesystems. The GitHub
runner evaluates workflow expressions, then `useverself/checkpoint@v0` sends a
concrete `key` and `path` to sandbox-rental from inside the runner attempt.
sandbox-rental derives tenant, repository, provider, run, job, branch, pull
request, and trust context from persisted runner allocation state. Guest
requests never carry tenant authority, repository authority, ZFS refs, host
paths, dataset names, device paths, or generation-promotion authority.

The first benchmark target is `expressjs/express` as a modest npm workload.
The benchmark pins a repository ref and Node version, mounts `~/.npm`, runs
`npm ci`, and runs the repository test command. The first run should create and
promote generation `1`; the second run should mount that generation and show an
npm package-cache hit from local filesystem bytes.

The repeatable canary is `aspect-operator checkpoint-canary`. It prepares the
same workload commit, pushes it to a source-code-hosting-service repository and
a private GitHub repository, dispatches `.github/workflows/checkpoint-canary.yml`
on each provider, and waits for sandbox-rental runs matching repository,
workflow name, branch, and head SHA. GitHub canaries also sync the installed
GitHub App repositories into `runner_provider_repositories` before dispatching,
so runner ownership remains a product API operation instead of an operator-side
database edit.

Example workflow shape:

```yaml
jobs:
  express:
    runs-on: verself-4vcpu-ubuntu-2404
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-node@v5
        with:
          node-version: 22
      - uses: useverself/checkpoint@v0
        with:
          key: npm-${{ github.repository }}-${{ runner.os }}-${{ hashFiles('package-lock.json') }}
          path: ~/.npm
      - run: npm ci
      - run: npm test
```

## Durable State

Postgres is lifecycle truth. ClickHouse is evidence. OpenTelemetry is
sequencing detail.

- `volumes` identifies the stable Checkpoint resource for an organization,
  repository, scope, component kind, and key hash.
- `execution_volume_mounts` is the attempt-scoped authority row for one action
  invocation. It stores the concrete mount path, selected source generation,
  mount state, save state, and commit result.
- `volume_generations` records immutable committed filesystem generations.
- `volume_current_generation` stores the readable current pointer per trust
  class and scope.
- `checkpoint_lifecycle_events` is the append-only Postgres transition ledger.
  Every state transition appends one row in the same transaction that updates
  the current state projection.
- `checkpoint_operations` is the ClickHouse fact table projected from
  lifecycle transitions for benchmarks, dashboards, and diagnosis.

`checkpoint_lifecycle_events` should use this envelope:

| Field | Purpose |
| --- | --- |
| `event_id` | Globally unique event identity. |
| `event_name` | Stable lifecycle event name. |
| `observed_at` | sandbox-rental observation time. |
| `execution_id`, `attempt_id` | Attempt authority and trace join keys. |
| `mount_id`, `volume_id` | Checkpoint resource identities. |
| `source_generation_id`, `committed_generation_id` | Generation lineage. Empty when absent. |
| `provider`, `provider_installation_id`, `provider_repository_id` | Provider identity recovered from runner state. |
| `provider_run_id`, `provider_job_id` | Provider run/job correlation. |
| `repository_full_name`, `runner_class` | Customer-facing run context. |
| `scope_kind`, `scope_ref`, `trust_class` | Branch/trust policy context. |
| `key_hash`, `mount_path_hash` | Non-secret correlation handles. |
| `from_mount_state`, `to_mount_state` | Mount-state transition. |
| `from_save_state`, `to_save_state` | Save-state transition. |
| `result`, `reason_code` | Stable outcome and machine-readable reason. |
| `used_bytes`, `written_bytes`, `duration_ms` | Storage and latency evidence when known. |
| `trace_id`, `span_id` | OpenTelemetry join keys. |

Full keys, raw mount paths, bearer tokens, provider runtime tokens, storage
credentials, host paths, zvol device paths, and ZFS dataset names stay out of
ClickHouse and traces. A restricted copy of the key and the raw customer mount
path may live in Postgres product state for inventory and support.

## State Vocabulary

`execution_volume_mounts.mount_state`:

| State | Meaning |
| --- | --- |
| `requested` | The action request authenticated and an idempotent mount row exists. |
| `resolving` | sandbox-rental is resolving volume, scope, and source generation. |
| `preparing_host` | vm-orchestrator is creating or cloning the writable zvol. |
| `attaching` | The zvol is being bound to a reserved runner drive slot. |
| `mounted` | vm-bridge mounted the ext4 filesystem at the requested guest path. |
| `mount_failed` | The mount can no longer become usable in this attempt. |
| `finalizing` | The job has ended and saveback/cleanup is in progress. |
| `finalized` | Saveback decision and cleanup completed for this attempt. |

`execution_volume_mounts.save_state`:

| State | Meaning |
| --- | --- |
| `none` | The action has not requested saveback. |
| `requested` | The post step requested saveback for this mount. |
| `committing` | vm-orchestrator is sealing, flushing, and snapshotting the mount. |
| `committed` | A new immutable `volume_generations` row exists. |
| `promotion_succeeded` | The generation became current for its scope/trust pointer. |
| `promotion_conflict` | Generation creation succeeded but current pointer CAS lost. |
| `promotion_skipped` | Trust policy allows generation retention but not promotion. |
| `failed` | Saveback cannot complete for this attempt. |

`volume_generations.state`:

| State | Meaning |
| --- | --- |
| `committing` | Host commit has started but product metadata is not complete. |
| `committed` | Immutable generation metadata is durable. |
| `retained` | Generation is retained but is not current. |
| `current` | Generation is the current pointer for at least one scope/trust row. |
| `expired` | Retention selected the generation for deletion. |
| `destroyed` | Host storage is gone and product state is terminal. |

The future finite state machine should encode allowed transitions over these
state values. v0 transition helpers should still use compare-and-swap updates
so retry and reconciliation behavior matches the later FSM.

## Lifecycle Events

| Event | Required state transition | Emitter | ClickHouse operation/result |
| --- | --- | --- | --- |
| `checkpoint.mount.requested` | `NULL -> requested`, `none` save state | sandbox-rental guest API | `mount_request` / `accepted` |
| `checkpoint.volume.resolving` | `requested -> resolving` | sandbox-rental | `volume_resolve` / `started` |
| `checkpoint.volume.created` | no mount-state change | sandbox-rental | `volume_resolve` / `created` |
| `checkpoint.volume.found` | no mount-state change | sandbox-rental | `volume_resolve` / `found` |
| `checkpoint.source.missed` | no mount-state change; source generation empty | sandbox-rental | `source_select` / `miss` |
| `checkpoint.source.hit` | source generation recorded | sandbox-rental | `source_select` / `hit` |
| `checkpoint.host.prepare_started` | `resolving -> preparing_host` | sandbox-rental before vm-orchestrator call | `host_prepare` / `started` |
| `checkpoint.host.prepare_succeeded` | `preparing_host -> attaching` | sandbox-rental after vm-orchestrator response | `host_prepare` / `succeeded` |
| `checkpoint.host.prepare_failed` | `preparing_host -> mount_failed` | sandbox-rental | `host_prepare` / `failed` |
| `checkpoint.mount.ready` | `attaching -> mounted` | sandbox-rental after vm-bridge mount acknowledgement | `guest_mount` / `mounted` |
| `checkpoint.mount.failed` | `requested/resolving/preparing_host/attaching -> mount_failed` | sandbox-rental | operation-specific / `failed` |
| `checkpoint.save.requested` | `none -> requested` save state | action post endpoint | `save_request` / `accepted` |
| `checkpoint.finalization.started` | `mounted -> finalizing` | execution finalizer | `finalize` / `started` |
| `checkpoint.commit.started` | `requested -> committing` save state | execution finalizer | `commit` / `started` |
| `checkpoint.commit.succeeded` | `committing -> committed` save state; generation row inserted | sandbox-rental after vm-orchestrator commit | `commit` / `succeeded` |
| `checkpoint.commit.failed` | `committing -> failed` save state | sandbox-rental | `commit` / `failed` |
| `checkpoint.promotion.succeeded` | `committed -> promotion_succeeded`; generation may become `current` | sandbox-rental transaction | `promotion` / `succeeded` |
| `checkpoint.promotion.conflicted` | `committed -> promotion_conflict`; generation retained | sandbox-rental transaction | `promotion` / `conflicted` |
| `checkpoint.promotion.skipped` | `committed -> promotion_skipped`; generation retained | sandbox-rental transaction | `promotion` / `skipped` |
| `checkpoint.finalization.completed` | `finalizing -> finalized` | execution finalizer | `finalize` / `succeeded` |

Idempotent replays append no duplicate transition event. The request handler
returns the existing mount or save state and writes a ClickHouse row with
`result = idempotent_replay` only if an external request was replayed.

## First-Run Lifecycle

1. GitHub sends a `workflow_job` webhook for the selected Express job.
2. sandbox-rental records runner demand and allocates a Verself runner attempt.
3. vm-orchestrator boots the runner with reserved Checkpoint drive slots.
4. `useverself/checkpoint@v0` receives concrete `key` and `path` values from the
   runner after GitHub expression evaluation.
5. The action sends a mount request with the attempt-scoped token.
6. sandbox-rental appends `checkpoint.mount.requested`.
7. sandbox-rental resolves or creates `volumes` and appends
   `checkpoint.volume.created` on first use.
8. No current generation exists, so sandbox-rental appends
   `checkpoint.source.missed`.
9. vm-orchestrator creates a fresh ext4 zvol, binds it to a reserved drive
   slot, and asks vm-bridge to mount it at `~/.npm` after path resolution.
10. sandbox-rental appends `checkpoint.mount.ready`.
11. `npm ci` downloads packages into the mounted npm cache.
12. The action post step sends a save request and sandbox-rental appends
   `checkpoint.save.requested`.
13. After the runner exits, finalization appends
   `checkpoint.finalization.started` and `checkpoint.commit.started`.
14. vm-orchestrator asks vm-bridge to seal the mount, flushes the zvol, and
   snapshots the writable filesystem.
15. sandbox-rental inserts `volume_generations(generation = 1)`, appends
   `checkpoint.commit.succeeded`, and attempts promotion.
16. A protected or same-repository branch run promotes by compare-and-swap and
   appends `checkpoint.promotion.succeeded`. A fork PR appends
   `checkpoint.promotion.skipped`.
17. sandbox-rental appends `checkpoint.finalization.completed`, writes
   `checkpoint_operations` rows, and releases the lease.

## Second-Run Lifecycle

1. The action sends the same normalized key and path.
2. sandbox-rental resolves the existing `volumes` row.
3. `volume_current_generation` selects generation `1`, and sandbox-rental
   appends `checkpoint.source.hit`.
4. vm-orchestrator clones the selected generation into a writable zvol and
   mounts it into the runner.
5. `npm ci` validates the dependency graph against the lockfile and reuses
   local npm cache bytes where valid.
6. Saveback creates generation `2`.
7. Promotion succeeds only if the current pointer still references generation
   `1`. Parallel writers that lose this compare-and-swap append
   `checkpoint.promotion.conflicted`.

## Failure And Reconciliation

- A mount failure is terminal for that action invocation. The workflow step
  should fail unless the action exposes an explicit future `continue-on-error`
  mode.
- A failed saveback must not hide the job result. It is recorded as a
  Checkpoint failure, and the workflow result remains the runner's result.
- If vm-orchestrator commits a host generation but sandbox-rental crashes
  before product metadata is written, reconciliation reads vm-orchestrator/ZFS
  facts, inserts or marks the missing generation, and appends a recovery event.
- If ClickHouse insert fails after Postgres commit, the transition remains
  durable. Projection reconciliation backfills `checkpoint_operations` from
  `checkpoint_lifecycle_events`.
- If the finalizer runs twice, compare-and-swap guards and idempotency keys
  return the already recorded terminal state.

## FSM Construction Rules

The later finite state machine should be generated from a transition table with
these columns:

```text
event_name
from_mount_state
to_mount_state
from_save_state
to_save_state
guard
side_effect
terminal
```

Guards are pure functions over Postgres state. Side effects are named calls to
vm-orchestrator, ClickHouse projection, or billing/storage metering. External
side effects use deterministic idempotency keys derived from `attempt_id`,
`mount_id`, and the transition name.
