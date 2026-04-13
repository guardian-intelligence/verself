# Durable Execution Workflow: Phased Implementation Plan

Status: In progress
Owner: `sandbox-rental-service`  
Last Updated: 2026-04-13

## Scope

- Eliminate `500`s and stuck execution attempts under burst load.
- Replace request-scoped goroutine execution with a durable state machine.
- Add queue/backpressure controls independent of HTTP rate limiting.
- Add first-class reconciliation for every nonterminal execution state.
- Make pagination a service invariant for execution and evidence surfaces.
- Make River the shared durable queue/scheduler runtime inside
  `sandbox-rental-service` for every control-plane producer: direct API
  submissions, Forgejo/GitHub runner adapters, long-running VM sessions,
  recurring schedules, and infrastructure canaries.
- Define the scheduler/queue taxonomy before rewriting code so runner dispatch,
  VM lifecycle supervision, and cron materialization share one execution state
  machine instead of growing separate schedulers.
- Prove completion with deployed end-to-end rehearsals and ClickHouse evidence.

## Non-Scope

- JetStream/Kafka fanout and outbox CDC cutover.
- Temporal/DBOS orchestration adoption in this rewrite.
- River Pro features as foundational dependencies. In particular, do not depend
  on Pro global concurrency, Pro workflows, Pro sequences, or Pro durable
  periodic jobs for customer-visible correctness.
- Per-org runner pools, autoscaling across multiple hosts, cache/artifact
  protocols, and multi-tenant runner management UI. This plan includes the
  first single-runner Forgejo adapter and the future GitHub adapter attachment
  point because those decisions affect the queue/scheduler model.
- Changing the `execution_submit=120/min` per-API rate-limit policy.
- Treating billing `402` reserve denial as an exception instead of terminal business outcome.

## Grounding References

### In-Repo References

- VM execution boundary and ownership: [vm-execution-control-plane.md](./vm-execution-control-plane.md)
- Sandbox scheduler rewrite guidance: [AGENTS.md](../AGENTS.md)
- System deployment and runtime graph: [docs/architecture/service-architecture.md](../../../docs/architecture/service-architecture.md)
- Identity/authz boundary and secured Huma operation model: [identity-and-iam.md](../../platform/docs/identity-and-iam.md)
- Shared wire-language and OpenAPI contract rules: [wire-contracts.md](../../apiwire/docs/wire-contracts.md)
- Billing reserve/settle/void architecture: [billing-architecture.md](../../billing-service/docs/billing-architecture.md)
- Current sandbox schema: [001_sandbox_schema.up.sql](../migrations/001_sandbox_schema.up.sql)
- Current evidence tables: [007_sandbox_job_logs.up.sql](../../platform/migrations/007_sandbox_job_logs.up.sql), [002_otel_tables.up.sql](../../platform/migrations/002_otel_tables.up.sql)
- Live verification flow and evidence harvesting: [verify-sandbox-live.sh](../../platform/scripts/verify-sandbox-live.sh), [verify-scheduler-runtime.sh](../../platform/scripts/verify-scheduler-runtime.sh), [collect-sandbox-verification-evidence.sh](../../platform/scripts/collect-sandbox-verification-evidence.sh)
- Forgejo runner engine tracer bullet before River integration: [forgejo-runner-phase-0.md](./forgejo-runner-phase-0.md)

### Primary Sources

- River transactional enqueueing: https://riverqueue.com/docs/transactional-enqueueing
- River retries: https://riverqueue.com/docs/job-retries
- River database drivers: https://riverqueue.com/docs/database-drivers
- River multiple queues: https://riverqueue.com/docs/multiple-queues
- River scheduled jobs: https://riverqueue.com/docs/scheduled-jobs
- River periodic and cron jobs: https://riverqueue.com/docs/periodic-jobs
- River leader election: https://riverqueue.com/docs/leader-election
- River OpenTelemetry: https://riverqueue.com/docs/open-telemetry
- PostgreSQL `SELECT ... FOR UPDATE SKIP LOCKED`: https://www.postgresql.org/docs/current/sql-select.html
- PostgreSQL `LISTEN`: https://www.postgresql.org/docs/current/sql-listen.html
- PostgreSQL `NOTIFY`: https://www.postgresql.org/docs/current/sql-notify.html
- PostgreSQL connection settings: https://www.postgresql.org/docs/current/runtime-config-connection.html
- Go `database/sql` connection pool defaults and `SetMaxOpenConns`: https://pkg.go.dev/database/sql#DB.SetMaxOpenConns
- OpenTelemetry trace API (events/links/status): https://opentelemetry.io/docs/specs/otel/trace/api/
- TigerBeetle transfer model: https://docs.tigerbeetle.com/reference/transfer/
- Forgejo `act` fork source and README: https://code.forgejo.org/forgejo/act
- Forgejo runner source: https://code.forgejo.org/forgejo/runner
- Forgejo runner protocol package: https://pkg.go.dev/code.forgejo.org/forgejo/actions-proto/runner/v1
- Forgejo runner ConnectRPC package: https://pkg.go.dev/code.forgejo.org/forgejo/actions-proto/runner/v1/runnerv1connect
- GitHub self-hosted runners: https://docs.github.com/en/actions/reference/runners/self-hosted-runners
- GitHub self-hosted runner REST API: https://docs.github.com/en/rest/actions/self-hosted-runners
- GitHub `workflow_job` webhook: https://docs.github.com/en/webhooks/webhook-events-and-payloads#workflow_job

## Rollout Contract (Applies To Every Phase)

Each phase is a deployable vertical slice and must satisfy all of the following before merge:

1. Live deploy rehearsal with Ansible (single-node path):
   `cd src/platform/ansible && ansible-playbook -i inventory/hosts.ini playbooks/dev-single-node.yml --tags ...`
2. Full e2e rehearsal (`rent-a-sandbox` browser journey + API burst/fault drill for that phase).
3. Postgres assertions for expected row/state transitions.
4. ClickHouse assertions for expected trace/log/event sequence.
5. Evidence artifact persisted under `artifacts/sandbox-live/<run-id>/evidence/`.

Proof source of truth is ClickHouse traces/logs/events, not unit tests alone.

## Scheduler Runtime Requirements

River is the queue/scheduler runtime for sandbox-rental-service control-plane
work. It is not the VM execution substrate. vm-orchestrator remains the only
privileged VM/ZFS/Firecracker execution boundary; River workers call
vm-orchestrator when a durable control-plane transition needs VM work.

The durable truth remains in service-owned PostgreSQL tables:

- `executions`, `execution_attempts`, and `execution_events` own one-shot
  execution lifecycle state for direct commands, Forgejo/GitHub CI jobs,
  arbitrary workload invocations, canaries, and long-running VM lifecycle
  actions.
- Runner integration tables such as `forgejo_task_executions` map external CI
  task identity, log ack cursors, cancellation state, and finalization state to
  Forge Metal execution IDs.
- Schedule definition tables own recurring customer cron semantics: schedule
  expression, timezone, next fire time, misfire policy, overlap policy, pause
  state, and policy/billing owner. River only queues and runs the scanner,
  materializer, and follow-on control-plane workers.
- Capacity/lease tables own host/resource concurrency. River worker concurrency
  protects a single process; it is not the global source of truth for VM slots,
  org quotas, runner labels, or future multi-node pools.

Use `riverpgxv5` with a `pgxpool.Pool` for the production client. River's
`database/sql` driver exists, but it falls back to poll-only behavior because
`database/sql` cannot expose PostgreSQL `LISTEN`/`NOTIFY`. The rewrite should
therefore move the scheduler-owned transaction path to pgx instead of bolting
River onto the current `lib/pq` monolith.

River OSS periodic jobs are acceptable for internal scanner ticks only. Their
schedules are in-memory and can skip edge runtimes across leader changes. Durable
customer schedules must be modeled in Forge Metal tables and materialized by an
idempotent `schedule.fire` worker. River scheduled jobs are acceptable for
specific future `run_at` work items when the execution row already exists and the
domain schedule table remains authoritative.

## Queue And Job Taxonomy

Initial queues:

- `execution`: normal state-machine advancement.
- `orchestrator`: VM launch/cancel/status calls, bounded by host capacity.
- `runner`: Forgejo/GitHub fetch, task update, cancellation, and log-ack IO.
- `scheduler`: recurring schedule scanners and execution materializers.
- `reconcile`: low-priority repair of nonterminal attempts and orphaned side
  effects.
- `webhook`: current webhook delivery work, moved off the bespoke worker after
  the execution path is stable.

Initial job kinds:

- `execution.advance`: load an attempt by ID, inspect state, perform at most one
  durable transition, append `execution_events`, and enqueue itself or a
  follow-up job only when another transition is ready.
- `execution.cancel`: record cancellation intent and advance cleanup through the
  same transition machinery.
- `execution.reconcile`: re-evaluate one nonterminal attempt or a bounded stale
  state shard. Reconciliation never bypasses transition helpers.
- `runner.forgejo.fetch`: long-poll Forgejo with a capacity snapshot/lease,
  translate an accepted task into execution admission, and persist
  `forgejo_task_executions` before acknowledging task state.
- `runner.forgejo.update_log`: flush execution log chunks to Forgejo using the
  persisted ack cursor. Persist the acknowledged index, not merely the attempted
  flush index.
- `runner.forgejo.update_task`: finalize external task state after the execution
  reaches terminal state. Cancellation responses enqueue `execution.cancel`.
- `runner.github.fetch`: same queue contract as Forgejo, provider-specific
  protocol only. It must reuse execution admission and log/finalizer semantics.
- `schedule.scan`: scan due customer schedule definitions and enqueue bounded
  `schedule.fire` jobs.
- `schedule.fire`: claim one schedule occurrence, materialize an execution via
  the same admission path as API/runner work, and advance `next_fire_at`
  according to the stored misfire/overlap policy.
- `vm.session.renew`: renew billing/capacity leases and checkpoint policy for
  long-running VM sessions.
- `vm.session.expire`: terminalize sessions whose TTL, entitlement, or policy
  window expired.

The taxonomy intentionally makes external queues thin adapters. Forgejo and
GitHub own CI task availability; River owns durable local control-plane work and
recovery once we decide to accept a task. Customer-visible execution state is
always projected from Forge Metal execution tables.

## Capacity Model

Do not use River Pro global concurrency as an invariant. Model host and product
capacity in PostgreSQL with short TTL leases, deterministic lease IDs, and
compare-and-swap release/renewal:

- VM slot lease key: resource class + host/node + attempt ID.
- Runner task lease key: provider + runner label + external task ID.
- Schedule fire lease key: schedule ID + scheduled fire timestamp.
- Session lease key: session ID + lease sequence.

The `runner.forgejo.fetch`/`runner.github.fetch` workers must acquire or observe
capacity before long-polling so they do not claim CI tasks that cannot launch.
The external runner protocol remains the upstream queue; River is not a
pre-buffer for unbounded CI work.

## Recurring Schedule Model

Recurring workloads need product semantics that River OSS periodic jobs do not
own:

- tenant/org and actor/policy context;
- billing product and entitlement checks at fire time;
- timezone-aware cron parsing;
- overlap policy (`skip`, `queue_one`, `allow`);
- misfire policy (`skip_missed`, `fire_once`, `catch_up_bounded`);
- maximum catch-up window and per-schedule concurrency;
- immutable occurrence IDs for idempotency and evidence.

The durable table shape should be explicit, roughly:

```sql
workload_schedules(
  schedule_id uuid primary key,
  org_id text not null,
  actor_id text not null,
  workload_kind text not null,
  schedule_expr text not null,
  timezone text not null,
  next_fire_at timestamptz not null,
  paused_at timestamptz,
  overlap_policy text not null,
  misfire_policy text not null,
  created_at timestamptz not null,
  updated_at timestamptz not null
)

workload_schedule_occurrences(
  occurrence_id uuid primary key,
  schedule_id uuid not null references workload_schedules(schedule_id),
  scheduled_for timestamptz not null,
  execution_id uuid references executions(execution_id),
  state text not null,
  created_at timestamptz not null,
  unique(schedule_id, scheduled_for)
)
```

`schedule.fire` claims an occurrence, inserts the execution + attempt +
`execution_events` + River `execution.advance` job in one transaction, then
projects evidence into ClickHouse like any other admission source.

## Runner Adapter Contract

Forgejo and GitHub adapters must translate provider work into the same internal
execution contract:

- `source_kind`: `api`, `forgejo_actions`, `github_actions`, `cron`, `canary`,
  `vm_session`.
- `workload_kind`: `direct`, `forgejo_workflow`, `github_workflow`,
  `vm_session`, or future explicit variants.
- external identity fields: provider task/run/job IDs, repo, ref, SHA, runner
  label, request key/idempotency key, and log ack cursor.
- trace context from the fetch/registration path extracted before River's OTel
  middleware starts `execution.advance` worker spans, or explicitly linked if
  parent/child semantics become misleading.

The adapter may own provider-specific tables, but it may not own lifecycle
terminalization. Terminal execution state, billing settlement, log storage, and
orchestrator cleanup stay in the shared state machine. This is what makes the
eventual GitHub runner a protocol adapter instead of a second scheduler.

## Handoff Implementation Sequence

The first River code cut should be a queue-runtime cut, not a partial VM
execution rewrite:

1. Add `pgxpool`, `riverpgxv5`, and `otelriver` dependencies; keep the existing
   `database/sql` path alive only for code not yet owned by the scheduler cut.
2. Add service-owned migrations for River tables, `execution_events`, source and
   workload projection columns, capacity leases, and schedule occurrence tables.
3. Register the River queues and typed job args for the taxonomy above. The
   first cut activated `scheduler.probe` to prove schema, pgx-backed runtime
   startup, queue registration, and OTel spans. The second cut activates
   `execution.advance` as the first domain worker once `execution_events` and
   the transactional admission contract exist.
4. Replace `Submit` with a pgx transaction that writes execution + attempt +
   initial `execution_events` row + River `execution.advance` job. Stop launching
   `go s.execute(...)` from the request handler in the same cut.
5. Move billing reserve, orchestrator launch, wait/finalize, and cancellation
   into transition helpers one state at a time. Every helper must be retryable by
   inspecting stored state before attempting side effects.
   Billing reserve uses `(source_type, source_ref, window_seq)` as the
   idempotency key, with direct executions using `source_ref=attempt_id` and an
   explicit window sequence. Existing billing windows are replayable only while
   still reserved; terminal billing windows must fail the repeated reserve
   instead of being handed back to the execution worker.
6. Re-run the Forgejo `act` Phase 0 tracer through `execution.advance` before
   adding Forgejo `Register`/`Declare`/`FetchTask`. If the tracer cannot produce
   the same ClickHouse evidence when queued through River, runner integration
   stays blocked.

Do not use the River `database/sql` driver as a temporary production bridge. It
would make the first cut easier, but it bakes in polling behavior and leaves the
transactional enqueue path split from the pgx path we need for the final
scheduler.

## Phase Gates Up Front

| Phase | Vertical Slice | Must-Pass Gate Summary |
|---|---|---|
| 0 | Baseline proof and guardrails | Existing direct execution path survives burst/fault rehearsal without `500`, PG slot exhaustion, or stale nonterminal attempts |
| 1 | River queue runtime online | `riverpgxv5` client, River schema, queue taxonomy, and OTel middleware are deployed and proven by a real River probe job |
| 2 | Direct execution queued through River | `POST /api/v1/executions` transactionally writes execution + attempt + event + River job; worker calls vm-orchestrator and reaches terminal state with no request goroutine |
| 3 | Reconciliation and capacity leases | Crash/restart and injected dependency faults reconcile; VM/runner/schedule capacity is enforced by service-owned PG leases |
| 4 | Recurring schedules and VM sessions | Due cron occurrence and VM session renewal/expiry materialize through the same control-plane state machine |
| 5 | Forgejo workflow workload | Forgejo `act` tracer is queued through River and executed by vm-orchestrator/Firecracker with live ClickHouse proof, without Forgejo runner protocol yet |
| 6 | Forgejo runner adapter | Register/declare/fetch/log/finalize/cancel protocol attaches to the shared queue/scheduler runtime and drives a real Forgejo Actions job |
| 7 | GitHub runner adapter | GitHub workflow-job/JIT runner integration uses the same queue/scheduler model and does not introduce a second CI scheduler |
| 8 | Product read contract | Execution, event, log, schedule, runner, and session lists are cursor-paginated and observable |

## Phase 0: Baseline Proof And Guardrails

Primary anchors:

- In-repo: `vm-execution-control-plane.md`, `billing-architecture.md`,
  `verify-sandbox-live.sh`, `collect-sandbox-verification-evidence.sh`.
- Primary sources: Go `database/sql` pool controls and PostgreSQL connection
  settings.

### Verification Gate (Declared First)

Fail-first protocol:

1. Deploy current baseline on the single-node path.
2. Run a 1000-submit burst using a trivial `direct` command.
3. Run one crash/fault rehearsal that interrupts `sandbox-rental-service` after
   execution insertion but before terminalization.
4. Confirm baseline evidence includes at least one of:
   - HTTP `500` on submit,
   - PG slot exhaustion in `default.otel_logs`,
   - stale `queued`/`launching` attempts after TTL,
   - reserved billing window attached to an unreconcilable attempt.

Green protocol:

1. Re-run the burst and crash/fault rehearsals after the guardrails land.
2. Assert exactly zero submit `500`s and zero PG slot exhaustion messages.
3. Assert stale `queued`/`launching` attempts are reconciled or terminalized.
4. Assert billing windows end `settled` or `voided`.
5. Persist evidence under `artifacts/sandbox-live/<run-id>/evidence/`.

### Implementation Slice

- Set explicit PG pool limits for sandbox and billing service callers.
- Fix current `markLaunching`/reserve failure paths that can strand an attempt.
- Add stale `queued` and stale `launching` reconciliation to the current path.
- Add explicit OTel/log reason tags for reserve denial, reserve failure, and
  reconciler action.

### Expected Database Changes

- Minimal indices only:
  - `execution_attempts(state, updated_at)`
  - `execution_billing_windows(state, attempt_id)`
- No River schema yet.

### Required E2E + Evidence Assertions

PostgreSQL:

```sql
SELECT count(*) AS stale_nonterminal
FROM execution_attempts
WHERE state IN ('queued', 'launching')
  AND updated_at < (now() - interval '60 seconds');
```

```sql
SELECT count(*) AS invalid_reserved_windows
FROM execution_billing_windows w
JOIN execution_attempts a ON a.attempt_id = w.attempt_id
WHERE w.state = 'reserved'
  AND a.state NOT IN ('reserved', 'launching', 'running', 'finalizing');
```

ClickHouse:

```sql
SELECT
  toUInt16OrZero(SpanAttributes['http.status_code']) AS status,
  count() AS c
FROM default.otel_traces
WHERE Timestamp BETWEEN parseDateTime64BestEffort($1) AND parseDateTime64BestEffort($2)
  AND ServiceName = 'sandbox-rental-service'
  AND SpanAttributes['http.method'] = 'POST'
  AND SpanAttributes['http.target'] = '/api/v1/executions'
GROUP BY status
ORDER BY status;
```

Required trace order for one successful direct execution:

1. `sandbox-rental.execution.submit`
2. `sandbox-rental.execution.run`
3. `vm-orchestrator.EnsureRun`
4. `vm-orchestrator.WaitRun`
5. `sandbox-rental.execution.finalize`

## Phase 1: River Queue Runtime Online

Primary anchors:

- In-repo: `AGENTS.md` in `sandbox-rental-service`, Ansible
  `sandbox_rental_service` migration role, `wire-contracts.md`.
- Primary sources: River database drivers, transactional enqueueing, multiple
  queues, OpenTelemetry middleware, PostgreSQL `LISTEN`/`NOTIFY`.

### Verification Gate (Declared First)

Fail-first protocol:

1. Add a harness that tries to enqueue a `scheduler.probe` River job against
   baseline.
2. Confirm baseline fails because no River schema, client, queues, or worker
   spans exist.

Green protocol:

1. Deploy via `dev-single-node.yml --tags sandbox_rental_service`.
2. `make scheduler-proof` enqueues one `scheduler.probe` job through
   `sandbox-rental-service`.
3. Probe worker completes through `riverpgxv5`/`pgxpool`, not
   `riverdatabasesql`.
4. `default.otel_traces` shows River insert and work spans.
5. River queue tables show the job reached `completed`.

### Implementation Slice

- Add `pgxpool`, `riverpgxv5`, and `otelriver`.
- Apply River OSS migrations through `src/sandbox-rental-service/migrations/`.
- Add `make scheduler-proof` as the live deployed proof entrypoint.
- Start one River client with queues:
  - `execution`
  - `orchestrator`
  - `runner`
  - `scheduler`
  - `reconcile`
  - `webhook`
- Enable `otelriver` with `EnableWorkSpanJobKindSuffix`.
- Register `scheduler.probe` only; do not cut over execution behavior yet.

### Expected Database Changes

- River tables in the `sandbox_rental` database, including `river_job`,
  `river_queue`, and `river_migration`.
- No execution lifecycle mutation path changes.

### Required E2E + Evidence Assertions

PostgreSQL:

```sql
SELECT name, paused_at
FROM river_queue
WHERE name IN ('execution','orchestrator','runner','scheduler','reconcile','webhook')
ORDER BY name;
```

```sql
SELECT kind, queue, state, count(*) AS c
FROM river_job
WHERE kind = 'scheduler.probe'
GROUP BY kind, queue, state;
```

ClickHouse:

```sql
SELECT Timestamp, SpanName, SpanAttributes['messaging.destination.name'] AS queue
FROM default.otel_traces
WHERE ServiceName = 'sandbox-rental-service'
  AND Timestamp BETWEEN parseDateTime64BestEffort($1) AND parseDateTime64BestEffort($2)
  AND SpanName IN ('river.insert_many', 'river.work/scheduler.probe')
ORDER BY Timestamp;
```

Required trace order:

1. `sandbox-rental.scheduler.probe.submit`
2. `river.insert_many`
3. `river.work/scheduler.probe`
4. `sandbox-rental.scheduler.probe.complete`

## Phase 2: Direct Execution Queued Through River

Primary anchors:

- In-repo: `vm-execution-control-plane.md`, `billing-architecture.md`,
  `wire-contracts.md`, `007_sandbox_job_logs.up.sql`.
- Primary sources: River transactional enqueueing, River retries, OTel trace API,
  TigerBeetle transfer model.

### Verification Gate (Declared First)

Fail-first protocol:

1. Add an e2e test that submits the same idempotency key concurrently and then
   kills `sandbox-rental-service` after commit but before work starts.
2. Confirm baseline either duplicates side effects, launches from a request
   goroutine, or lacks ordered `execution_events` lineage.

Green protocol:

1. `POST /api/v1/executions` returns after a single pgx transaction writes
   execution + attempt + workload spec + initial event + `execution.advance`.
2. No request handler starts `go s.execute(...)`.
3. `execution.advance` calls vm-orchestrator as needed and reaches terminal
   state for a direct command.
4. Replayed idempotency key returns the same execution and attempt.
5. Worker spans are linked to the submit trace context.
6. The same admission helper accepts a synthetic `source_kind='forgejo_actions'`
   fixture without contacting Forgejo.

### Implementation Slice

- Add `execution_events`.
- Add source/workload projection:
  - `executions.source_kind`
  - `executions.workload_kind`
  - `executions.source_ref`
  - `execution_attempts.submit_trace_id`
  - `execution_attempts.submit_trace_context`
- Add `execution_workload_specs` for structured workload payloads and secret
  references.
- Replace `Submit` with pgx transactional enqueue.
- Implement `execution.advance` for the direct workload path; vm-orchestrator
  remains the VM executor.
- Keep side-effect IDs deterministic:
  - billing reserve identity from attempt/window sequence;
  - orchestrator run ID from attempt ID.
- Terminalize billing `402` as `insufficient_balance`, not a transport error.

### Expected Database Changes

- `execution_events(event_seq, execution_id, attempt_id, from_state, to_state,
  reason, trace_id, created_at)`.
- `execution_workload_specs(execution_id, workload_kind, spec_jsonb,
  secret_refs_jsonb, created_at)`.
- ClickHouse `forge_metal.job_events` gains empty-default columns:
  - `source_kind LowCardinality(String) DEFAULT ''`
  - `workload_kind LowCardinality(String) DEFAULT ''`
  - `external_provider LowCardinality(String) DEFAULT ''`
  - `external_task_id String DEFAULT ''`

### Required E2E + Evidence Assertions

PostgreSQL:

```sql
SELECT count(*) AS executions_for_key
FROM executions
WHERE org_id = $1 AND idempotency_key = $2;
```

```sql
SELECT event_seq, from_state, to_state, reason
FROM execution_events
WHERE attempt_id = $1
ORDER BY event_seq ASC;
```

Expected event sequence:

`NULL -> queued -> reserved -> launching -> running -> finalizing -> succeeded`

```sql
SELECT source_kind, workload_kind, count(*) AS c
FROM executions
WHERE execution_id IN ($1, $2)
GROUP BY source_kind, workload_kind
ORDER BY source_kind, workload_kind;
```

```sql
SELECT count(*) AS sandbox_windows
FROM execution_billing_windows
WHERE attempt_id = $1;

SELECT count(*) AS billing_windows
FROM billing_windows
WHERE source_type = 'execution_attempt'
  AND source_ref = $1;
```

The two counts must match; a mismatch means the scheduler handoff can duplicate
billing windows across a River retry or crash boundary.

ClickHouse:

```sql
SELECT Timestamp, SpanName, SpanAttributes['execution.state'] AS state
FROM default.otel_traces
WHERE ServiceName = 'sandbox-rental-service'
  AND Timestamp BETWEEN parseDateTime64BestEffort($1) AND parseDateTime64BestEffort($2)
  AND SpanName IN (
    'sandbox-rental.execution.submit',
    'river.insert_many',
    'river.work/execution.advance',
    'sandbox-rental.execution.transition',
    'sandbox-rental.execution.finalize'
  )
ORDER BY Timestamp;
```

Required trace order:

1. `sandbox-rental.execution.submit`
2. `river.insert_many`
3. `river.work/execution.advance`
4. `sandbox-rental.execution.transition` (`queued -> reserved`)
5. `sandbox-rental.execution.transition` (`reserved -> launching`)
6. `vm-orchestrator.EnsureRun`
7. `vm-orchestrator.WaitRun`
8. `sandbox-rental.execution.finalize`

## Phase 3: Reconciliation And Capacity Leases

Primary anchors:

- In-repo: `firecracker-vm-networking.md`, `vm-execution-control-plane.md`,
  `billing-architecture.md`.
- Primary sources: River retries, River leader election, PostgreSQL
  `SELECT ... FOR UPDATE SKIP LOCKED`, PostgreSQL `LISTEN`/`NOTIFY`.

### Verification Gate (Declared First)

Fail-first protocol:

1. Kill `sandbox-rental-service` after billing reserve but before launch.
2. Kill it after launch but before finalization.
3. Inject one transient orchestrator failure and one billing settlement failure.
4. Over-submit synthetic runner admissions beyond
   `vm-orchestrator.GetCapacity().total_slots`.
5. Confirm baseline can strand attempts or over-accept work.

Green protocol:

1. Every nonterminal state has an explicit `execution.reconcile` path.
2. No nonterminal state remains stale after reconcile SLA.
3. Capacity leases prevent over-accepting VM-backed work.
4. Queue saturation returns `503 Retry-After`; policy rate limit remains `429`.
5. Reserved billing windows are settled or voided.

### Implementation Slice

- Add PG capacity leases with deterministic IDs and TTL:
  - VM slot lease,
  - runner task lease,
  - schedule occurrence lease,
  - VM session lease.
- Add `execution.cancel` and `execution.reconcile`.
- Add admission queue-depth and capacity checks distinct from HTTP rate limits.
- Ensure reconciler never bypasses transition helpers.

### Expected Database Changes

- `scheduler_capacity_leases(lease_id, lease_kind, resource_key, owner_ref,
  expires_at, released_at, created_at, updated_at)`.
- Indices for stale-state sweeps:
  - `execution_attempts(state, updated_at)`
  - `scheduler_capacity_leases(lease_kind, resource_key, expires_at)`

### Required E2E + Evidence Assertions

PostgreSQL:

```sql
SELECT count(*) AS stale_nonterminal
FROM execution_attempts
WHERE state IN ('queued','reserved','launching','running','finalizing')
  AND updated_at < (now() - interval '5 minutes');
```

```sql
SELECT lease_kind, resource_key, count(*) AS active
FROM scheduler_capacity_leases
WHERE released_at IS NULL AND expires_at > now()
GROUP BY lease_kind, resource_key
ORDER BY lease_kind, resource_key;
```

ClickHouse:

```sql
SELECT Timestamp, SpanName, SpanAttributes['capacity.lease_kind'] AS lease_kind,
       SpanAttributes['capacity.lease_result'] AS lease_result
FROM default.otel_traces
WHERE ServiceName = 'sandbox-rental-service'
  AND Timestamp BETWEEN parseDateTime64BestEffort($1) AND parseDateTime64BestEffort($2)
  AND SpanAttributes['capacity.lease_kind'] != ''
ORDER BY Timestamp;
```

Required trace order for a crash-recovered execution:

1. `river.work/execution.reconcile`
2. `sandbox-rental.execution.transition` (`reserved -> launching` or terminal cleanup)
3. `sandbox-rental.capacity.acquire` or `sandbox-rental.capacity.release`
4. `vm-orchestrator.EnsureRun` or `vm-orchestrator.WaitRun`
5. `sandbox-rental.execution.finalize`

## Phase 4: Recurring Schedules And VM Sessions

Primary anchors:

- In-repo: `identity-and-iam.md`, `billing-architecture.md`,
  `vm-execution-control-plane.md`.
- Primary sources: River periodic jobs, River scheduled jobs, River
  transactional enqueueing.

### Verification Gate (Declared First)

Fail-first protocol:

1. Add a private harness that creates one due schedule occurrence and one
   long-running VM session renewal target.
2. Confirm baseline has no durable customer schedule table, no idempotent
   occurrence materialization, and no session renewal job.

Green protocol:

1. `schedule.scan` finds a due schedule and enqueues one `schedule.fire`.
2. `schedule.fire` creates exactly one occurrence and one execution on retry.
3. `vm.session.renew` extends billing/capacity leases through the same lease
   model as one-shot executions.
4. `vm.session.expire` terminalizes a session through execution transitions.
5. All rows carry org/actor/billing context.

### Implementation Slice

- Add schedule definition and occurrence tables.
- Use River periodic jobs only for internal scanner ticks.
- Implement idempotent `schedule.scan` and `schedule.fire`.
- Add VM session renewal/expiry jobs behind private harnesses.
- Keep customer-facing schedule APIs out until the queue/runtime path is proven.

### Expected Database Changes

- `workload_schedules`.
- `workload_schedule_occurrences`.
- `vm_sessions` or session projection fields owned by the execution model.
- Capacity leases for schedule occurrence and VM session renewal.

### Required E2E + Evidence Assertions

PostgreSQL:

```sql
SELECT schedule_id, scheduled_for, count(*) AS occurrences, count(execution_id) AS executions
FROM workload_schedule_occurrences
WHERE schedule_id = $1 AND scheduled_for = $2
GROUP BY schedule_id, scheduled_for;
```

```sql
SELECT lease_kind, owner_ref, expires_at, released_at
FROM scheduler_capacity_leases
WHERE owner_ref IN ($1, $2)
ORDER BY created_at;
```

ClickHouse:

```sql
SELECT Timestamp, SpanName, SpanAttributes['schedule.id'] AS schedule_id,
       SpanAttributes['execution.source_kind'] AS source_kind
FROM default.otel_traces
WHERE ServiceName = 'sandbox-rental-service'
  AND Timestamp BETWEEN parseDateTime64BestEffort($1) AND parseDateTime64BestEffort($2)
  AND SpanName IN ('river.work/schedule.scan','river.work/schedule.fire','river.work/vm.session.renew','river.work/vm.session.expire')
ORDER BY Timestamp;
```

Required trace order for a scheduled execution:

1. `river.work/schedule.scan`
2. `river.insert_many`
3. `river.work/schedule.fire`
4. `sandbox-rental.execution.submit`
5. `river.work/execution.advance`
6. `sandbox-rental.execution.finalize`

## Phase 5: Forgejo Workflow Workload Queued Through River

Primary anchors:

- In-repo: `forgejo-runner-phase-0.md`, `vm-execution-control-plane.md`,
  `firecracker-vm-networking.md`, `wire-contracts.md`.
- Primary sources: Forgejo `act` fork, Forgejo runner source/protocol,
  River transactional enqueueing.

### Verification Gate (Declared First)

Fail-first protocol:

1. Submit a synthetic `forgejo_workflow` execution through the River-backed
   admission helper.
2. Confirm baseline fails because no Forgejo workflow workload kind, no
   Forgejo `act` integration, and no `vm-bridge.workflow_runner.*` spans exist.

Green protocol:

1. Workflow spec is admitted as `source_kind='forgejo_actions'` and
   `workload_kind='forgejo_workflow'`.
2. Forgejo `act` library executes in Firecracker via the existing
   vm-orchestrator/vm-bridge protocol.
3. Workflow includes `actions/checkout@v4`, one Node-backed step, and marker
   `phase-0-forgejo-act`.
4. Billing settles and ClickHouse row/span proof is reproduced after
   `guest-rootfs.yml` and `dev-single-node.yml`.

### Implementation Slice

- Add `forgejo_workflow` workload spec validation.
- Add vmproto/vm-orchestrator fields needed to pass workflow spec into the VM.
- Add `vm-bridge` workflow runner using the Forgejo `act` library pin recorded
  in `forgejo-runner-phase-0.md`.
- Preserve the existing host service/vsock protocol; no new host service.

### Expected Database Changes

- `execution_workload_specs` stores workflow spec metadata and secret refs.
- `forge_metal.job_events` rows are populated with:
  - `source_kind='forgejo_actions'`
  - `workload_kind='forgejo_workflow'`
  - `exit_code=0`
- `forge_metal.job_logs` includes `phase-0-forgejo-act`.

### Required E2E + Evidence Assertions

PostgreSQL:

```sql
SELECT e.source_kind, e.workload_kind, a.state
FROM executions e
JOIN execution_attempts a ON a.attempt_id = e.latest_attempt_id
WHERE e.execution_id = $1;
```

ClickHouse:

```sql
SELECT count()
FROM forge_metal.job_events
WHERE execution_id = $1
  AND source_kind = 'forgejo_actions'
  AND workload_kind = 'forgejo_workflow'
  AND status = 'succeeded'
  AND exit_code = 0;
```

```sql
SELECT count()
FROM forge_metal.job_logs
WHERE attempt_id = $1
  AND positionCaseInsensitive(toString(chunk), 'phase-0-forgejo-act') > 0;
```

Required trace order:

1. `sandbox-rental.execution.submit`
2. `river.insert_many`
3. `river.work/execution.advance`
4. `vm-orchestrator.EnsureRun`
5. `vm-orchestrator.WaitRun`
6. `vm-bridge.run_phase`
7. `vm-bridge.workflow_runner.act_prepare`
8. `vm-bridge.workflow_runner.act_execute`
9. `sandbox-rental.execution.finalize`

## Phase 6: Forgejo Runner Adapter

Primary anchors:

- In-repo: `forgejo-runner-phase-0.md`, `wire-contracts.md`,
  `identity-and-iam.md`, Ansible `sandbox_rental_service` role.
- Primary sources: Forgejo Actions protocol package, Forgejo runner ConnectRPC
  client, River transactional enqueueing.

### Verification Gate (Declared First)

Fail-first protocol:

1. Enable Forgejo Actions in the local Forgejo deployment and push a workflow
   targeting `forge-metal-2vcpu-4gb`.
2. Confirm baseline has no runner registration/declare/fetch/log/finalize
   spans and Forgejo leaves the task queued.

Green protocol:

1. `sandbox-rental-service` registers once and persists runner UUID + secret.
2. `Declare` keeps the runner online across service restart.
3. `runner.forgejo.fetch` long-polls only when capacity is available.
4. Accepted task creates a normal execution through the Phase 2 admission path.
5. Live logs reach Forgejo through `runner.forgejo.update_log` with persisted ack
   cursor.
6. Final task state is posted through `runner.forgejo.update_task`.
7. Cancellation response enqueues `execution.cancel` and settles billing.

### Implementation Slice

- Vendor/pin Forgejo Actions protocol clients.
- Add registration/secret table and credstore integration.
- Add `forgejo_task_executions`.
- Add `runner.forgejo.fetch`, `runner.forgejo.update_log`,
  `runner.forgejo.update_task`.
- Add cancel watcher behavior from `UpdateTask` responses.

### Expected Database Changes

- `forgejo_runner_registrations`.
- `forgejo_task_executions(forgejo_task_id, execution_id, last_log_index,
  finalized_at, cancel_requested_at)`.
- `forge_metal.job_events.external_provider='forgejo'`.
- `forge_metal.job_events.external_task_id` equals the Forgejo task ID.

### Required E2E + Evidence Assertions

PostgreSQL:

```sql
SELECT runner_uuid, labels, last_declare_at
FROM forgejo_runner_registrations
WHERE tenant_id = 'platform';
```

```sql
SELECT forgejo_task_id, execution_id, last_log_index, finalized_at
FROM forgejo_task_executions
WHERE forgejo_task_id = $1;
```

ClickHouse:

```sql
SELECT external_task_id, exit_code, workload_kind
FROM forge_metal.job_events
WHERE external_provider = 'forgejo'
ORDER BY completed_at DESC
LIMIT 1;
```

Required trace order:

1. `sandbox-rental.forgejo_runner.register`
2. `sandbox-rental.forgejo_runner.declare`
3. `river.work/runner.forgejo.fetch`
4. `sandbox-rental.forgejo_runner.task_to_execution`
5. `river.work/execution.advance`
6. `vm-bridge.workflow_runner.act_execute`
7. `river.work/runner.forgejo.update_log`
8. `river.work/runner.forgejo.update_task`

## Phase 7: GitHub Runner Adapter

Primary anchors:

- In-repo: `identity-and-iam.md`, `wire-contracts.md`,
  `vm-execution-control-plane.md`.
- Primary sources: GitHub self-hosted runner docs, GitHub REST self-hosted runner
  APIs, GitHub `workflow_job` webhook docs.

### Verification Gate (Declared First)

Fail-first protocol:

1. Install a GitHub App against a disposable test repo and subscribe to
   `workflow_job`.
2. Push a workflow targeting a Forge Metal label.
3. Confirm no GitHub adapter rows/spans exist and no Forge Metal execution is
   admitted.

Green protocol:

1. `workflow_job` queued webhook is accepted, verified, and mapped to a runner
   lease.
2. GitHub JIT/ephemeral runner registration is created through official GitHub
   APIs, or the phase is blocked with an explicit protocol finding if GitHub's
   current runner model cannot be implemented without their runner binary.
3. GitHub job work uses the same execution/capacity/billing/log model as
   Forgejo.
4. Completion/cancellation updates release the runner lease and settle billing.

### Implementation Slice

- Add GitHub provider tables and webhook verification.
- Add `runner.github.provision`, `runner.github.finalize`, and
  `runner.github.cleanup` workers.
- Reuse `execution.advance`, `execution.cancel`, capacity leases, and log
  forwarding.
- Do not model GitHub as a Forgejo-style `FetchTask`; GitHub's public surface is
  runner registration/JIT configuration plus webhooks, not an open task-fetch
  protocol.

### Expected Database Changes

- `github_runner_registrations`.
- `github_workflow_job_executions`.
- `forge_metal.job_events.external_provider='github'`.
- No second scheduler table outside the shared execution/capacity model.

### Required E2E + Evidence Assertions

PostgreSQL:

```sql
SELECT github_job_id, execution_id, state, finalized_at
FROM github_workflow_job_executions
WHERE github_job_id = $1;
```

ClickHouse:

```sql
SELECT Timestamp, SpanName, SpanAttributes['external.provider'] AS provider
FROM default.otel_traces
WHERE ServiceName = 'sandbox-rental-service'
  AND Timestamp BETWEEN parseDateTime64BestEffort($1) AND parseDateTime64BestEffort($2)
  AND SpanAttributes['external.provider'] = 'github'
ORDER BY Timestamp;
```

Required trace order:

1. `sandbox-rental.github_runner.workflow_job`
2. `river.work/runner.github.provision`
3. `sandbox-rental.github_runner.create_jit_config`
4. `river.work/execution.advance`
5. `river.work/runner.github.finalize`
6. `river.work/runner.github.cleanup`

## Phase 8: Product Read Contract

Primary anchors:

- In-repo: `wire-contracts.md`, `identity-and-iam.md`,
  generated OpenAPI specs, rent-a-sandbox frontend generated client.
- Primary sources: PostgreSQL `SELECT` ordering/locking documentation and OTel
  trace API.

### Verification Gate (Declared First)

Fail-first protocol:

1. Add tests that attempt offset pagination or unstable ordering on mutable
   execution/schedule/runner/session lists.
2. Confirm baseline accepts or emits unstable results.

Green protocol:

1. All touched list APIs use opaque cursor pagination.
2. Cursors include version, direction, sort keys, and filter hash.
3. Invalid/mismatched cursors return `400`.
4. Browser e2e proves lists remain stable during concurrent inserts.

### Implementation Slice

- Add shared cursor helpers.
- Apply cursor pagination to executions, attempts/events/logs, schedule
  occurrences, runner task mappings, and VM sessions.
- Add stable ordering indices.

### Expected Database Changes

- Stable ordering indices:
  - executions: `(org_id, created_at DESC, execution_id DESC)`
  - events: `(attempt_id, event_seq ASC)`
  - logs: `(attempt_id, created_at ASC, seq ASC)`
  - schedule occurrences: `(schedule_id, scheduled_for DESC, occurrence_id DESC)`
  - runner mappings: `(org_id, created_at DESC, execution_id DESC)`

### Required E2E + Evidence Assertions

PostgreSQL:

```sql
SELECT execution_id, created_at
FROM executions
WHERE org_id = $1
ORDER BY created_at DESC, execution_id DESC
LIMIT 101;
```

ClickHouse:

```sql
SELECT SpanName, SpanAttributes['pagination.limit'] AS limit,
       SpanAttributes['pagination.has_more'] AS has_more
FROM default.otel_traces
WHERE ServiceName = 'sandbox-rental-service'
  AND Timestamp BETWEEN parseDateTime64BestEffort($1) AND parseDateTime64BestEffort($2)
  AND SpanAttributes['pagination.limit'] != ''
ORDER BY Timestamp DESC
LIMIT 100;
```

## Test Harness Deliverables Across Phases

- Extend `verify-sandbox-live.sh` instead of creating disconnected proof flows.
- Add `make verify-scheduler` for Phases 0-4.
- Add `make verify-forgejo-runner` for Phases 5-6.
- Add `make verify-github-runner` only when Phase 7 becomes active.
- Keep evidence collection centralized in
  `collect-sandbox-verification-evidence.sh`.
- Each harness prints:
  - execution ID,
  - attempt ID,
  - River job IDs,
  - capacity lease IDs,
  - external provider task IDs when present,
  - ClickHouse trace ID,
  - one-line ordered span chain.

## Cutover Criteria

The scheduler cutover is complete only when:

1. Request handlers no longer advance workload lifecycle directly.
2. All control-plane producers use the shared admission transaction.
3. River workers are the only sandbox-rental-service lifecycle advancement path.
4. `execution_events` is complete for every attempt.
5. Capacity is enforced by PG leases, not in-memory counters or River Pro.
6. Recurring schedules and VM sessions use the same execution/capacity/billing
   control-plane state machine.
7. Forgejo and GitHub are protocol adapters, not separate schedulers.
8. Every phase gate above is green on the bare-metal single-node deploy with
   ClickHouse evidence attached.
