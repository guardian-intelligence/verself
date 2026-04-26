# sandbox-rental-service

Public `/api/*` Huma routes must use the secured-operation registration pattern in `internal/api`: keep the method/path/OpenAPI declaration and `operationPolicy` together in `RegisterRoutes` so IAM, rate-limit, idempotency, audit, and generated-client contracts cannot drift.

SPIFFE-only service APIs belong on the internal Huma API, committed
`openapi/internal-openapi-3.0.yaml`, and generated `internalclient` package.
Callers must inject `workloadauth.MTLSClientForService` into that generated
client; do not add handwritten service HTTP calls or repo-owned Zitadel bearer
tokens to reach sandbox-rental-service.

## Host privilege boundary

sandbox-rental-service owns tenant policy, execution state, billing coordination, and customer APIs. It must never shell out to `zfs`, Firecracker, jailer, TAP, or host device operations, and it must never receive `zfs allow`, `/dev/zvol`, `/dev/kvm`, or Linux capabilities. All privileged VM and volume lifecycle work goes through vm-orchestrator.

Its membership in `vm-clients` is a root-equivalent control-plane capability. Do not share that group with frontend servers, webhook-only services, guest code, runner workloads, or plugins. Calls to vm-orchestrator must use refs authorized from sandbox-rental's own Postgres state; never pass through tenant-supplied host paths or dataset names.

## Runner Auth And Billing Flow

Runner integrations have two different auth boundaries. Keep them separate:

- Zitadel authenticates the Verself user or service account configuring an
  integration.
- The source provider authenticates webhook, installation, and repository facts
  with provider-native mechanisms. Webhooks are not Zitadel-authenticated
  requests.

The durable tenant binding is provider-specific:

- GitHub uses an org-scoped GitHub installation connection for onboarding and a
  `runner_provider_repositories` row for execution ownership.
- Forgejo repositories imported through source-code-hosting-service register a
  `runner_provider_repositories` row through the SPIFFE-only sandbox internal
  API.

GitHub flow:

```text
Zitadel JWT
  sub = user/service account
  org_id = Verself org
      |
      | POST /api/v1/github/installations/connect
      | internal/api/routes.go: beginGitHubInstallation
      v
github_installation_states
  state -> org_id, actor_id, expires_at
      |
      | GitHub callback /github/installations/callback
      | carries installation_id + state
      v
github_accounts + github_installations + github_installation_connections
  installation_id -> GitHub account facts
  installation_id + org_id -> active Verself org connection
      |
      | GitHub workflow_job webhook, HMAC verified
      | carries installation.id, repository, workflow job id
      v
runner_jobs(provider=github)
  provider_job_id, provider_installation_id, provider_repository_id, labels, runner identity
      |
      | capacity reconcile joins runner_provider_repositories by repository_id
      v
runner_allocations(provider=github)
  allocation_id, installation_id, requested_for_provider_job_id
      |
      | AllocateRunner calls Service.Submit with allocation.OrgID
      v
executions
  org_id from imported runner repository, actor_id = github-app:<installation_id>,
  source_kind = github_actions, workload_kind = runner,
  external_provider = github, external_task_id = <provider_job_id>
      |
      | execution.advance reserve/activate/settle
      v
billing_windows
  org_id, actor_id, source_type = github_actions,
  source_ref = <execution_id>, billing_job_id, usage_summary
```

Forgejo flow:

```text
source-code-hosting-service
  repo create / first git push
      |
      | SPIFFE internal POST /internal/v1/runner/repositories
      v
runner_provider_repositories(provider=forgejo)
  source_repository_id, org_id, provider_repository_id
      |
      | Forgejo action webhook -> repository job sync
      v
runner_jobs(provider=forgejo)
  provider_job_id, provider_repository_id, runs_on labels
      |
      | capacity reconcile uses runner_classes
      v
runner_allocations(provider=forgejo)
  allocation_id, requested_for_provider_job_id
      |
      | one-job bootstrap fetches attempt-scoped config
      v
executions
  org_id from runner_provider_repositories,
  source_kind = forgejo_actions, workload_kind = runner,
  external_provider = forgejo, external_task_id = <provider_job_id>
```

Rules that are easy to get wrong:

- Never infer tenant ownership from a GitHub job ID, runner name, repository
  name, installation ID, or webhook delivery ID. GitHub onboarding is bound by
  `github_installation_connections(installation_id, org_id)`, while GitHub
  execution ownership is bound by
  `runner_provider_repositories(provider, provider_repository_id) -> org_id`.
  The same GitHub installation may be connected to multiple Verself orgs;
  repository import ownership is the exclusive execution boundary.
- Never infer tenant ownership from a Forgejo repository name, job handle,
  runner name, or webhook delivery ID. The tenant binding for Forgejo work is
  `runner_provider_repositories(provider, provider_repository_id) -> org_id`,
  established by source-code-hosting-service over the internal SPIFFE API.
- The GitHub callback is safe only because `state` was generated by
  `BeginInstallation` and persisted with `org_id`, `actor_id`, and an expiry.
  A callback without a live state row must not create or update an
  installation.
- Webhooks and job sync create demand facts. They do not directly create
  billable customer work. Capacity reconciliation may create a runner
  allocation; runner allocation then submits a normal sandbox execution.
- Billing is keyed to our execution system of record. `billing_windows.source_ref`
  is the Verself `execution_id`; provider job IDs are correlation metadata
  on `executions.external_task_id` and `runner_jobs`.
- The machine actor for runtime billing is `github-app:<installation_id>`.
  The human installer/updater is audit metadata and should be stored separately
  when that schema exists; do not overload runtime actor identity with the human
  actor.
- Sticky-disk restore/commit requests are guest-originated internal calls. They
  authenticate with attempt-scoped HMAC tokens tied to `execution_id` and
  `attempt_id`; they inherit the org through the execution/allocation lookup,
  not through a guest-supplied org field.

Concrete code anchors:

- Zitadel identity extraction: `internal/api/routes.go:requireIdentity` and
  `requireOrgID`.
- Installation connect/callback: `internal/jobs/github_runner.go:BeginInstallation`
  and `CompleteInstallation`.
- Webhook demand ingestion: `internal/jobs/github_runner.go:HandleWebhook` and
  `internal/jobs/forgejo_runner.go:HandleWebhook`.
- Org recovery for GitHub demand: `internal/jobs/github_runner.go:loadQueuedJob`
  joins `runner_jobs` to `runner_provider_repositories`, then verifies the
  active installation connection for the recovered org.
- Org recovery for Forgejo demand:
  `internal/jobs/forgejo_runner.go:loadQueuedJob` joins `runner_jobs` to
  `runner_provider_repositories`.
- Execution submission from runner allocation:
  `internal/jobs/github_runner.go:AllocateRunner` and
  `internal/jobs/forgejo_runner.go:AllocateRunner`.
- Billing reservation from execution org:
  `internal/jobs/jobs.go:reserveBilling`.

Queryable proof for a GitHub job should show one consistent `org_id` across
`github_installation_connections`, `runner_provider_repositories`,
`executions`, `billing_windows`, and `verself.metering`.

Use River OSS as the worker/queue runtime for sandbox-rental-service control-plane work, keep the execution state machine explicit in Postgres, and treat the current execution jobs code as a rewrite target during the execution cutover. vm-orchestrator remains the VM execution boundary. Do not use River Pro features as a foundational dependency. If we need global concurrency beyond a single process, model it in our own PG capacity tables.

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

  The River worker should advance durable transitions explicitly. The current
  first cut is:

  queued -> reserved -> launching -> running -> finalizing -> succeeded|failed|canceled

  Do not introduce a `reserving` state as local terminology without a migration
  and a verification gate; it would become part of the public execution event
  stream and reconciliation contract.

  Every transition should write an append-only execution_events row and update the current projection in the same transaction. The worker can be retried at any point and should inspect state before doing work. External side effects must be deterministic:

  - Billing reserve identity from attempt_id + window_seq; the billing service must return the existing window for a repeated reserve of the same source/window sequence only while that window is still reserved.
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

  Having RAM means we can raise Postgres max_connections, but it does not mean
  we leave Go pools unbounded. Sandbox-rental uses one bounded pgxpool for
  HTTP handlers, sqlc stores, River queue work, and execution workers. Keep its
  max/min pool settings in Ansible aligned with the node-wide Postgres
  connection budget and the separate recurring worker process pool.

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

  Async worker traces should be correlated back to the API submit trace. Store trace context in the execution row/job args and extract it before River's OpenTelemetry middleware starts `river.work/*`, or add an explicit span link if parent/child semantics become misleading.

  So the strong path is: River for queue mechanics, PG for execution truth, TigerBeetle for financial truth, ClickHouse for proof/read models, OTel for trace correlation. This avoids building a bespoke job system while keeping the core state machine and billing invariants explicit and reviewable.

Use sqlc in this service: https://docs.sqlc.dev/en/latest/index.html
