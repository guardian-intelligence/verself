# Durable Execution Workflow: Phased Implementation Plan

Status: Proposed  
Owner: `sandbox-rental-service`  
Last Updated: 2026-04-12

## Scope

- Eliminate `500`s and stuck execution attempts under burst load.
- Replace request-scoped goroutine execution with a durable state machine.
- Add queue/backpressure controls independent of HTTP rate limiting.
- Add first-class reconciliation for every nonterminal execution state.
- Make pagination a service invariant for execution and evidence surfaces.
- Prove completion with deployed end-to-end rehearsals and ClickHouse evidence.

## Non-Scope

- JetStream/Kafka fanout and outbox CDC cutover.
- Temporal/DBOS orchestration adoption in this rewrite.
- Changing the `execution_submit=120/min` per-API rate-limit policy.
- Treating billing `402` reserve denial as an exception instead of terminal business outcome.

## Grounding References

### In-Repo References

- VM execution boundary and ownership: [vm-execution-control-plane.md](./vm-execution-control-plane.md)
- System deployment and runtime graph: [docs/architecture/service-architecture.md](../../../docs/architecture/service-architecture.md)
- Identity/authz boundary and secured Huma operation model: [identity-and-iam.md](../../platform/docs/identity-and-iam.md)
- Shared wire-language and OpenAPI contract rules: [wire-contracts.md](../../apiwire/docs/wire-contracts.md)
- Billing reserve/settle/void architecture: [billing-architecture.md](../../billing-service/docs/billing-architecture.md)
- Current sandbox schema: [001_sandbox_schema.up.sql](../migrations/001_sandbox_schema.up.sql)
- Current evidence tables: [007_sandbox_job_logs.up.sql](../../platform/migrations/007_sandbox_job_logs.up.sql), [002_otel_tables.up.sql](../../platform/migrations/002_otel_tables.up.sql)
- Live verification flow and evidence harvesting: [verify-sandbox-live.sh](../../platform/scripts/verify-sandbox-live.sh), [collect-sandbox-verification-evidence.sh](../../platform/scripts/collect-sandbox-verification-evidence.sh)

### Primary Sources

- River transactional enqueueing: https://riverqueue.com/docs/transactional-enqueueing
- River retries: https://riverqueue.com/docs/job-retries
- River OpenTelemetry: https://riverqueue.com/docs/open-telemetry
- PostgreSQL `SELECT ... FOR UPDATE SKIP LOCKED`: https://www.postgresql.org/docs/current/sql-select.html
- PostgreSQL connection settings: https://www.postgresql.org/docs/current/runtime-config-connection.html
- Go `database/sql` connection pool defaults and `SetMaxOpenConns`: https://pkg.go.dev/database/sql#DB.SetMaxOpenConns
- OpenTelemetry trace API (events/links/status): https://opentelemetry.io/docs/specs/otel/trace/api/
- TigerBeetle transfer model: https://docs.tigerbeetle.com/reference/transfer/

## Rollout Contract (Applies To Every Phase)

Each phase is a deployable vertical slice and must satisfy all of the following before merge:

1. Live deploy rehearsal with Ansible (single-node path):
   `cd src/platform/ansible && ansible-playbook -i inventory/hosts.ini playbooks/dev-single-node.yml --tags ...`
2. Full e2e rehearsal (`rent-a-sandbox` browser journey + API burst/fault drill for that phase).
3. Postgres assertions for expected row/state transitions.
4. ClickHouse assertions for expected trace/log/event sequence.
5. Evidence artifact persisted under `artifacts/sandbox-live/<run-id>/evidence/`.

Proof source of truth is ClickHouse traces/logs/events, not unit tests alone.

## Phase Gates Up Front

| Phase | Vertical Slice | Must-Pass Gate Summary |
|---|---|---|
| 1 | Stabilize current path | 1000-submit burst produces `429`/`402` as expected, zero `500`, zero PG-slot exhaustion evidence, zero stale `queued`/`launching` beyond TTL |
| 2 | Durable admission + River worker | Submit API returns after durable enqueue, worker owns state progression to terminal, append-only `execution_events` present and ordered |
| 3 | Reconciliation + machine backpressure | Crash/restart and external dependency faults reconcile to terminal outcomes, no orphaned reserved windows, queue admission caps enforce `503 Retry-After` |
| 4 | Cursor pagination contract | All touched list endpoints are cursor-based with deterministic ordering and cursor/filter hash checks; no offset pagination on mutable execution tables |
| 5 | Reservation sizing + renewal | Short jobs release liability quickly, long jobs renew deterministically, billing windows reconcile to final usage with no stranded holds |

## Phase 1: Stabilize Current Runner Path

### Verification Gate (Declared First)

Fail-first protocol (must fail on current baseline before code changes):

1. Deploy baseline service builds.
2. Run a 1000-submit burst rehearsal as platform admin (`direct` trivial command).
3. Confirm known failures appear:
   - Non-zero HTTP `500` for `POST /api/v1/executions`.
   - Stale attempts in nonterminal states (`queued`/`launching`).
   - PG slot exhaustion evidence in `default.otel_logs`.

Green protocol (required to close phase):

1. Re-run the same burst rehearsal.
2. Assert:
   - `500` count is exactly zero.
   - `429` remains for rate-limit protection and `402` remains for insufficient balance.
   - No PG slot exhaustion messages in logs/traces.
   - No attempts older than TTL in `queued` or `launching`.
3. Run full browser lifecycle proof and evidence collection.

### Implementation Slice

- Set explicit PG pool budgets in both `sandbox-rental-service` and `billing-service` (`MaxOpenConns`, `MaxIdleConns`, `ConnMaxLifetime`, `ConnMaxIdleTime`).
- Fix `markLaunching` failure path to avoid stranded admitted executions.
- Add stale `queued` and stale `launching` reconciler handling in current job path.
- Emit explicit structured OTel/log reasons for billing reserve failures and reconciliation actions.

### Expected Database Changes

- Minimal schema/index changes only for reconciliation scan efficiency.
- No new orchestration subsystem yet.
- Existing execution tables remain source of truth:
  - `executions`
  - `execution_attempts`
  - `execution_billing_windows`
  - `execution_logs`

### Required E2E + Evidence Assertions

Postgres assertions:

```sql
-- No stale queued/launching attempts beyond phase TTL (example: 60s).
SELECT count(*) AS stale_nonterminal
FROM execution_attempts
WHERE state IN ('queued', 'launching')
  AND updated_at < (now() - interval '60 seconds');
```

```sql
-- No reserved billing window without an active attempt.
SELECT count(*) AS orphaned_reserved
FROM execution_billing_windows w
JOIN execution_attempts a ON a.attempt_id = w.attempt_id
WHERE w.state = 'reserved'
  AND a.state NOT IN ('launching', 'running', 'finalizing');
```

ClickHouse assertions:

```sql
-- No connection slot exhaustion signal in sandbox/billing logs during run window.
SELECT count(*) AS pg_slot_exhaustion_hits
FROM default.otel_logs
WHERE Timestamp BETWEEN parseDateTime64BestEffort($1) AND parseDateTime64BestEffort($2)
  AND ServiceName IN ('sandbox-rental-service', 'billing-service')
  AND (
    Body ILIKE '%remaining connection slots%'
    OR Body ILIKE '%too many clients%'
  );
```

```sql
-- Submit HTTP evidence: 500 must be zero in run window.
SELECT
  toUInt16OrZero(SpanAttributes['http.status_code']) AS status,
  count(*) AS c
FROM default.otel_traces
WHERE Timestamp BETWEEN parseDateTime64BestEffort($1) AND parseDateTime64BestEffort($2)
  AND ServiceName = 'sandbox-rental-service'
  AND SpanAttributes['http.method'] = 'POST'
  AND SpanAttributes['http.target'] = '/api/v1/executions'
GROUP BY status
ORDER BY status;
```

## Phase 2: Durable Admission + River Worker State Machine

### Verification Gate (Declared First)

Fail-first protocol:

1. Add phase-specific test that submits the same idempotency key concurrently.
2. Confirm baseline can duplicate side effects or lacks durable queue/event lineage.

Green protocol:

1. Submit returns `201/202` only after transactionally writing execution + attempt + initial event + River job.
2. Replayed idempotency key returns same execution identity and does not duplicate billing/orchestrator side effects.
3. Worker executes durable state transitions to terminal (`succeeded|failed|canceled`) with ordered append-only events.
4. API trace ends at durable enqueue, not at launch/finalize.

### Implementation Slice

- Replace request-spawned execution goroutine path with River OSS worker path.
- Add `execution_events` append-only table with deterministic sequence and transition metadata.
- Move state change logic into compare-and-swap transition functions.
- Keep external side effects deterministic:
  - billing IDs from `attempt_id` + `window_seq`
  - orchestrator run ID from `attempt_id`
- Ensure `402` reserve denial terminalizes as business outcome (`insufficient_balance`) instead of transport error.

### Expected Database Changes

- New table: `execution_events` (append-only transition/event log).
- River migration tables in `sandbox_rental` database (queue substrate).
- Optional attempt projection fields to link submit trace context and worker correlation.

### Required E2E + Evidence Assertions

Postgres assertions:

```sql
-- Exactly one execution per org+idempotency key.
SELECT count(*) AS executions_for_key
FROM executions
WHERE org_id = $1 AND idempotency_key = $2;
```

```sql
-- Transition sequence is complete and monotonic for terminal attempts.
SELECT event_seq, from_state, to_state, reason
FROM execution_events
WHERE attempt_id = $1
ORDER BY event_seq ASC;
```

Expected ordered state path for success:

`queued -> reserving -> reserved -> launching -> running -> finalizing -> succeeded`

ClickHouse assertions:

```sql
-- API submit span should be short and should not include launch/finalize child work.
SELECT
  intDiv(Duration, 1000000) AS duration_ms,
  SpanAttributes['http.status_code'] AS status
FROM default.otel_traces
WHERE ServiceName = 'sandbox-rental-service'
  AND SpanAttributes['http.method'] = 'POST'
  AND SpanAttributes['http.target'] = '/api/v1/executions'
  AND Timestamp BETWEEN parseDateTime64BestEffort($1) AND parseDateTime64BestEffort($2)
ORDER BY Timestamp DESC
LIMIT 50;
```

```sql
-- Worker transition span events exist and are ordered.
SELECT
  Timestamp,
  SpanName,
  Events.Name,
  Events.Attributes
FROM default.otel_traces
WHERE ServiceName = 'sandbox-rental-service'
  AND Timestamp BETWEEN parseDateTime64BestEffort($1) AND parseDateTime64BestEffort($2)
  AND has(Events.Name, 'execution.transition')
ORDER BY Timestamp;
```

## Phase 3: First-Class Reconciliation + Backpressure Controls

### Verification Gate (Declared First)

Fail-first protocol:

1. Inject process crash/restart mid-attempt (after reserve, before terminalization).
2. Inject orchestrator/billing transient failures.
3. Confirm baseline can strand states or reservations.

Green protocol:

1. Every nonterminal state has a reconciler path triggered via queue jobs.
2. Crash/restart leaves no indefinitely stuck attempt.
3. No reserved billing window remains unless execution is running/finalizing/reconciling.
4. Queue admission saturation returns `503` with `Retry-After` (distinct from per-tenant `429`).

### Implementation Slice

- Add reconciliation jobs for each nonterminal state:
  - `queued`, `reserving`, `reserved`, `launching`, `running`, `finalizing`.
- Add queue admission controls by org/trust tier and global depth.
- Bound worker concurrency by stage (`reserve`, `launch`, `run`, `finalize`).
- Add explicit cancellation precedence and deterministic cleanup semantics.

### Expected Database Changes

- Reconciliation scheduling metadata (either explicit table or state fields).
- Admission/capacity state (if modeled in PG tables).
- Additional indices for stale-state sweeps and reconciliation claims.

### Required E2E + Evidence Assertions

Postgres assertions:

```sql
-- No stale nonterminal attempts past reconcile SLA (example 5m).
SELECT count(*) AS stale_nonterminal
FROM execution_attempts
WHERE state IN ('queued','reserving','reserved','launching','running','finalizing')
  AND updated_at < (now() - interval '5 minutes');
```

```sql
-- Invariant: reserved billing windows only with active/reconcilable attempts.
SELECT count(*) AS invalid_reserved_windows
FROM execution_billing_windows w
JOIN execution_attempts a ON a.attempt_id = w.attempt_id
WHERE w.state = 'reserved'
  AND a.state NOT IN ('launching','running','finalizing','reserving','reserved');
```

ClickHouse assertions:

```sql
-- Reconciliation transition evidence appears with reason tags.
SELECT
  Timestamp,
  ServiceName,
  Body,
  toString(LogAttributes) AS attrs
FROM default.otel_logs
WHERE Timestamp BETWEEN parseDateTime64BestEffort($1) AND parseDateTime64BestEffort($2)
  AND ServiceName = 'sandbox-rental-service'
  AND (Body ILIKE '%reconcile%' OR toString(LogAttributes) ILIKE '%reconcile%')
ORDER BY Timestamp;
```

```sql
-- Admission behavior separation: 429 (policy) vs 503 (capacity).
SELECT
  SpanAttributes['http.status_code'] AS status,
  count(*) AS c
FROM default.otel_traces
WHERE ServiceName = 'sandbox-rental-service'
  AND SpanAttributes['http.method'] = 'POST'
  AND SpanAttributes['http.target'] = '/api/v1/executions'
  AND Timestamp BETWEEN parseDateTime64BestEffort($1) AND parseDateTime64BestEffort($2)
GROUP BY status
ORDER BY status;
```

## Phase 4: Cursor Pagination As A Service Invariant

### Verification Gate (Declared First)

Fail-first protocol:

1. Add API tests that intentionally use offset/unstable ordering assumptions.
2. Confirm they fail against cursor-only contract changes.

Green protocol:

1. All touched execution list surfaces are cursor-based.
2. Bad/mismatched cursor returns `400`.
3. Ordering is deterministic with unique tie-breakers.
4. Browser e2e proves jobs list remains stable during concurrent inserts.

### Implementation Slice

- Add pagination helpers (`pagination.go`) with opaque base64url JSON cursor:
  - `version`
  - `direction`
  - `sort keys`
  - `filter hash`
- Add/upgrade list endpoints for executions, attempts/events/logs, billing windows, and usage history as needed.
- Use `limit + 1` fetch strategy and hard max limits.
- Remove offset pagination on mutable execution data.

### Expected Database Changes

- Indexes to support stable orders:
  - Executions: `(created_at DESC, execution_id DESC)`
  - Attempts: `(attempt_seq ASC, attempt_id ASC)`
  - Events: `(event_seq ASC)`
  - Logs: `(created_at ASC, seq ASC)`
  - Billing windows: `(window_seq ASC, billing_window_id ASC)`

### Required E2E + Evidence Assertions

Postgres assertions:

```sql
-- Example deterministic execution order check.
SELECT execution_id, created_at
FROM executions
WHERE org_id = $1
ORDER BY created_at DESC, execution_id DESC
LIMIT 101;
```

ClickHouse assertions:

```sql
-- Invalid cursor requests surface as 400 in traces.
SELECT count(*) AS bad_cursor_400s
FROM default.otel_traces
WHERE ServiceName = 'sandbox-rental-service'
  AND SpanAttributes['http.status_code'] = '400'
  AND SpanAttributes['http.target'] ILIKE '%cursor=%'
  AND Timestamp BETWEEN parseDateTime64BestEffort($1) AND parseDateTime64BestEffort($2);
```

```sql
-- Pagination attributes are present for read spans.
SELECT
  SpanName,
  SpanAttributes['pagination.limit'] AS limit,
  SpanAttributes['pagination.direction'] AS direction,
  SpanAttributes['pagination.has_more'] AS has_more
FROM default.otel_traces
WHERE ServiceName = 'sandbox-rental-service'
  AND Timestamp BETWEEN parseDateTime64BestEffort($1) AND parseDateTime64BestEffort($2)
  AND SpanAttributes['pagination.limit'] != ''
ORDER BY Timestamp DESC
LIMIT 100;
```

## Phase 5: Reservation Sizing + Renewal

### Verification Gate (Declared First)

Fail-first protocol:

1. Run short-job and long-job rehearsal with current coarse reservation.
2. Confirm short jobs over-reserve liability horizon (coarse hold behavior).

Green protocol:

1. Short jobs settle/void quickly with minimal pending hold.
2. Long jobs renew reservations deterministically without billing gaps.
3. Final billed quantity equals observed usage quantity.
4. No pending reservation remains after terminal execution + reconciliation SLA.

### Implementation Slice

- Move from coarse fixed reservation to smaller initial reservation + renewal windows.
- Keep deterministic billing window IDs and window sequence semantics.
- Add renewal transition semantics in worker/reconciler (`reserved -> running -> renewed ... -> finalizing`).
- Ensure final settlement and remainder void are deterministic and idempotent.

### Expected Database Changes

- `execution_billing_windows` usage of multiple `window_seq` rows per attempt becomes normal.
- Optional schema additions for renewal bookkeeping (`expires_at`, `renew_by`) if needed for deterministic renewal cadence.

### Required E2E + Evidence Assertions

Postgres assertions:

```sql
-- Short job should usually have one settled/voided window quickly.
SELECT attempt_id, window_seq, state, reserved_quantity, actual_quantity, created_at, settled_at
FROM execution_billing_windows
WHERE attempt_id = $1
ORDER BY window_seq ASC;
```

```sql
-- Long job should show renewal windows (window_seq > 0) and terminal settlement.
SELECT count(*) FILTER (WHERE window_seq > 0) AS renewal_windows,
       count(*) FILTER (WHERE state IN ('settled','voided')) AS terminal_windows
FROM execution_billing_windows
WHERE attempt_id = $1;
```

ClickHouse assertions:

```sql
-- Metering rows reconcile with billing window sequence for the attempt.
SELECT source_ref, window_seq, pricing_phase, charge_units, usage_evidence
FROM forge_metal.metering
WHERE source_ref = $1
ORDER BY recorded_at ASC, event_id ASC;
```

```sql
-- Transition traces include billing window IDs and renewal reasons.
SELECT
  Timestamp,
  SpanName,
  Events.Name,
  Events.Attributes
FROM default.otel_traces
WHERE ServiceName = 'sandbox-rental-service'
  AND Timestamp BETWEEN parseDateTime64BestEffort($1) AND parseDateTime64BestEffort($2)
  AND has(Events.Name, 'execution.transition')
ORDER BY Timestamp;
```

## Test Harness Deliverables Across Phases

- Keep `verify-sandbox-live.sh` as the full-stack top contour rehearsal.
- Add phase-targeted harnesses under `src/platform/scripts/`:
  - burst reliability harness (`1000` concurrent submits),
  - crash/restart reconciliation harness,
  - pagination contract harness,
  - reservation-renewal harness.
- Keep evidence collection centralized in `collect-sandbox-verification-evidence.sh` and extend outputs rather than creating disconnected scripts.

## Cutover Criteria

The rewrite is complete only when:

1. Request handlers no longer launch/advance workload lifecycle directly.
2. Worker transitions are the sole mutation path for execution lifecycle state.
3. `execution_events` is append-only and complete for every attempt.
4. Reconciliation is explicit for every nonterminal state.
5. Phase gates above are green in deployed rehearsal with ClickHouse evidence attached.
