# sandbox-rental-service

Public `/api/*` Huma routes must use the secured-operation registration pattern in `internal/api`: keep the method/path/OpenAPI declaration and `operationPolicy` together in `RegisterRoutes` so IAM, rate-limit, idempotency, audit, and generated-client contracts cannot drift.



Use River OSS as the worker/queue runtime for sandbox-rental-service control-plane work, keep the execution state machine explicit in Postgres, and delete/rewrite the current jobs code instead of refactoring it in place. vm-orchestrator remains the VM execution boundary. Do not use River Pro features as a foundational dependency. If we need global concurrency beyond a single process, model it in
  our own PG capacity tables.

  What To Delete/Rewrite
  I would treat src/sandbox-rental-service/internal/jobs/jobs.go and src/sandbox-rental-service/internal/jobs/reconcile.go as top-down rewrites, not incremental refactors. The new shape should be smaller files by responsibility:

  - submit.go: validates/idempotently creates execution + enqueues River job in the same PG transaction.
  - worker.go: River worker entry point; no domain logic beyond calling transition functions.
  - transitions.go: only compare-and-swap state transitions and event append.
  - billing.go: reserve/activate/settle/void wrappers with deterministic IDs.
  - orchestrator.go: launch/status/cancel wrappers with deterministic orchestrator job IDs.
  - reconcile.go: first-class reconciliation jobs for every nonterminal state.
  - pagination.go: common cursor encode/decode helpers for list endpoints.

  Reference Architecture Shape
  The API handler should stop launching work. It should:

  1. Validate auth, rate limit, idempotency key.
  2. Insert executions + execution_attempts + initial execution_events.
  3. Enqueue execution.advance with River inside the same PG transaction.
  4. Return 202 Accepted or 201 Created with the execution ID.

  The River worker should advance one durable transition at a time:

  queued -> reserving -> reserved -> launching -> running -> finalizing -> succeeded|failed|canceled

  Every transition should write an append-only execution_events row and update the current projection in the same transaction. The worker can be retried at any point and should inspect state before doing work. External side effects must be deterministic:

  - TigerBeetle transfer/window IDs from attempt_id + window_seq.
  - VM orchestrator job ID from attempt_id.
  - Billing reserve failure 402 becomes terminal insufficient_balance, not an exception.
  - PG failure becomes retryable worker error, not a stranded row.

  Postgres gives us the locking primitive for this. FOR UPDATE SKIP LOCKED is explicitly suitable for queue-like multi-consumer access in Postgres docs, and LIMIT/OFFSET needs deterministic ordering to avoid inconsistent result sets: Postgres SELECT locking and LIMIT docs
  (https://www.postgresql.org/docs/current/sql-select.html). River should hide most queue-locking details, but our domain transitions should still use compare-and-swap updates like WHERE state = $expected.

  Backpressure
  Rate limit 120/min can stay. It protects the customer from accidental floods. Separate that from capacity:

  - HTTP rate limit: customer/API safety.
  - River queue depth cap: admission safety.
  - Worker concurrency: machine safety.
  - PG pool limits: database safety.
  - VM launch/running limits: host/orchestrator safety.
  - Billing reserve concurrency: TigerBeetle/billing safety.

  Having RAM means we can raise Postgres max_connections, but it does not mean we leave Go pools unbounded. Go’s database/sql defaults to unlimited open connections unless SetMaxOpenConns is called: Go database/sql docs (https://pkg.go.dev/database/sql#DB.SetMaxOpenConns). If we adopt River, I’d also move sandbox-rental’s
  PG access to pgxpool because River is built around pgx and it gives better pool instrumentation.

  Pagination
  We should add pagination as a service invariant, not endpoint-by-endpoint improvisation:

  - No unbounded list endpoints.
  - Cursor pagination only, no offset pagination for mutable tables.
  - limit with a hard max, probably 100 for executions and 500 for logs/events.
  - Cursor is opaque base64 JSON: version, direction, sort keys, filter hash.
  - Execution list order: (created_at DESC, execution_id DESC).
  - Execution events/logs order: (created_at ASC, event_seq ASC) or (created_at DESC, event_seq DESC).
  - ClickHouse invoice/usage views: (recorded_at DESC, event_id/source_ref DESC).
  - Fetch limit + 1 to compute has_more, return next_cursor.
  - total_count is not default; it is either omitted, approximate, or a separate expensive query.

  Observability
  OpenTelemetry is the standard; we should use it intentionally rather than sprinkling manual spans everywhere. OTel spans support attributes/events/status, and events are the right place for state-transition breadcrumbs: OpenTelemetry trace API (https://opentelemetry.io/docs/specs/otel/trace/api/). River also has OTel mid
  dleware for river.insert_many and river.work: River OpenTelemetry (https://riverqueue.com/docs/open-telemetry).

  The useful pattern is a single transition helper that always does:

  - span event: execution.transition
  - attributes: execution_id, attempt_id, from_state, to_state, reason, billing_window_id, orchestrator_job_id
  - PG execution_events append
  - ClickHouse wide event projection
  - error recording and status setting on failure

  Async worker traces should be linked back to the API submit trace, not necessarily parented under it. Store trace context in the execution row/job args and create a span link when River works the job.

  So the strong path is: River for queue mechanics, PG for execution truth, TigerBeetle for financial truth, ClickHouse for proof/read models, OTel for trace correlation. This avoids building a bespoke job system while keeping the core state machine and billing invariants explicit and reviewable.
