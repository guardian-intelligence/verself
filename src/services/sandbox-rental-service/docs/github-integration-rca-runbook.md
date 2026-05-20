# GitHub Integration RCA Runbook

The GitHub runner integration has three durable identities:

- GitHub installation identity: the GitHub App installation on a GitHub organization.
- Repository execution identity: `runner_provider_repositories(provider='github', provider_repository_id)`.
- Workload identity: `executions.execution_id` and `execution_attempts.attempt_id`.

Performance work for this path is tracked in
[`VM Acquisition KPIs`](../../../../docs/architecture/vm-acquisition-kpis.md).
That document defines the timestamp boundaries, cache hit dimensions, and
substeps that must remain visible when optimizing runner acquisition latency.

Provider IDs are correlation data until they are joined to a Verself org through `runner_provider_repositories`. A GitHub installation can be connected to an org without any repository being eligible for runner execution. A repository can receive webhooks before its repository sync has created the execution ownership row. RCA starts by locating which identity is missing or inconsistent.

## State Diagram

```text
Verself user/admin
  |
  | POST /api/v1/github/installations/connect
  | span: github.installation.begin
  v
github_installation_states
  state, org_id, actor_id, expires_at
  |
  | GitHub installation callback
  | GET /github/installations/callback?installation_id=...&state=...&code=...
  | GitHub API: OAuth token exchange, user installation verification, installation fetch
  | span: github.installation.complete
  v
github_accounts
github_installations
github_installation_connections
  installation_id -> org_id, active/inactive
  |
  | POST /api/v1/github/installations/{installation_id}/repositories/sync
  | GitHub API: /installation/repositories
  | span: github.installation.repositories.sync
  v
runner_provider_repositories
  provider='github', provider_repository_id -> org_id
  |
  | GitHub workflow_job webhook: queued
  | POST /webhooks/github/actions
  | span: github.webhook.workflow_job
  v
runner_jobs
github_workflow_invocations
  provider_job_id, run_id, labels, status, delivery id
  |
  | River: runner.capacity.reconcile
  | span: river.work/runner.capacity.reconcile + github.capacity.reconcile
  v
runner_allocations
  pending, deadlines, requested_for_provider_job_id
  |
  | River: runner.allocate
  | GitHub API: installation token, runner group policy, generate-jitconfig
  | span: river.work/runner.allocate + github.runner.allocate + github.api.request
  v
executions
execution_attempts
runner_bootstrap_configs
execution_filesystem_mounts
runner_allocations.state = vm_submitted
  |
  | River: execution.advance
  | billing reserve, vm-orchestrator lease, StartExec
  | span: river.work/execution.advance + sandbox-rental.execution.run
  v
VM boots GitHub runner
  |
  | GET /internal/sandbox/v1/github-runner-jit
  | span: runner.bootstrap.consume
  v
runner_allocations.state = runner_config_fetched
  |
  | GitHub assigns job to runner
  | workflow_job webhook: in_progress
  | River: runner.job.bind
  | span: github.job.bind
  v
runner_job_bindings
runner_allocations.state = assigned
  |
  | runner exits, GitHub job reaches completed
  | GitHub API: /actions/runs/{run_id}/attempts/{attempt}/jobs
  v
execution_attempts.state = succeeded|failed|canceled|lost
execution_logs
verself.job_events
verself.job_logs
runner_allocations.state = vm_exited
  |
  | River: runner.cleanup
  | GitHub API: DELETE org runner
  | span: github.runner.cleanup
  v
runner_allocations.state = cleaned

Completed workflow_job webhook
  |
  | River: golden.run.promote
  | spans: durable.workflow_run.promote, durable.promote
  v
durable_generation/current pointer promotion
```

## How To Read The Diagram

The installation flow only proves that a GitHub organization has installed the app and that a Verself org is allowed to operate that installation. It writes `github_accounts`, `github_installations`, and `github_installation_connections`.

The repository sync is the tenant boundary for execution. It lists repositories visible to the installation and writes `runner_provider_repositories`. Capacity reconciliation will ignore a queued GitHub job if this row is missing, inactive, or owned by a different Verself org.

The webhook writes demand. The `workflow_job` event is HMAC verified, decoded, and upserted into `runner_jobs`. For `queued`, the webhook enqueues `runner.capacity.reconcile`. For `in_progress` and `completed`, it enqueues `runner.job.bind`. For `completed`, it also enqueues `golden.run.promote`.

Capacity reconciliation converts demand into capacity. It loads the queued job, checks active installation and repository ownership, resolves a runner class from the job labels, inserts `runner_allocations`, and enqueues `runner.allocate`.

Runner allocation creates provider capacity and then submits an ordinary sandbox execution. It calls GitHub for a JIT runner config, stores the one-time bootstrap payload, creates `executions` and `execution_attempts`, attaches the allocation, and enqueues `execution.advance`.

Execution advance is the VM state machine. It reserves billing, acquires a lease from `vm-orchestrator`, starts the guest runner command, waits for the process, settles billing, persists logs and job events, and marks the runner allocation as exited.

GitHub assignment is learned from provider evidence. The `in_progress` or `completed` workflow job payload carries `runner_id` and `runner_name`. `runner.job.bind` joins those values back to the allocation and writes `runner_job_bindings`.

GitHub logs and Verself logs are different surfaces. GitHub Actions logs are produced by the GitHub runner protocol after GitHub assigns a job to a runner. If a job is stuck in `queued`, there is no GitHub job log because no runner has claimed it. Verself product logs currently persist as a terminal combined chunk after `WaitExec` returns with output. A control-plane interruption before terminal finalization can leave `execution_logs` and `verself.job_logs` empty even when GitHub has partial step logs.

## Instrumentation Contract

| Point | Durable State | Trace Spans | ClickHouse Evidence | RCA Expectation |
| --- | --- | --- | --- | --- |
| Start install | `github_installation_states` | `sandbox-rental-service` HTTP span, `github.installation.begin` | HTTP access logs | State row exists and expires in about 10 minutes. |
| Complete install | `github_accounts`, `github_installations`, `github_installation_connections` | `github.installation.complete`, `github.api.request` | HTTP access logs, GitHub API client spans | Installation is active and account type is `Organization`. |
| Sync repos | `runner_provider_repositories` | `github.installation.repositories.sync`, `github.api.request` | HTTP access logs | Repo row is active and owned by the expected org. |
| Webhook received | `runner_jobs`, `github_workflow_invocations` | `github.webhook.workflow_job` | `default.http_access_logs`, `default.otel_traces` | Delivery ID is correlation ID, job row updated, status matches GitHub. |
| Capacity reconcile | `runner_allocations` pending | `river.work/runner.capacity.reconcile`, `github.capacity.reconcile` | River spans | One active allocation per provider job. |
| Runner allocation | `runner_allocations` `jit_creating -> jit_created -> vm_submitted` | `github.runner.allocate`, `github.runner_group.reconcile`, `github.api.request`, `sandbox-rental.execution.submit` | River spans, GitHub API spans | Provider runner ID and execution/attempt IDs are attached. |
| Execution launch | `executions`, `execution_attempts`, `execution_billing_windows` | `sandbox-rental.execution.run`, billing client spans, vm-orchestrator spans | `verself.job_events` queued row, VM evidence after launch | Attempt reaches `running` with `lease_id` and `exec_id`. |
| Guest bootstrap | `runner_bootstrap_configs.consumed_at`, allocation `runner_config_fetched` | `runner.bootstrap.consume`, HTTP span for JIT endpoint | HTTP access logs | JIT config consumed once before assignment deadline. |
| Job binding | `runner_job_bindings`, allocation `assigned` or `job_completed` | `runner.job.bind`, `github.job.bind` | River spans | Runner identity from webhook matches allocation runner ID/name. |
| Terminal execution | attempt terminal, billing settled, allocation `vm_exited` | `sandbox-rental.execution.run`, `github.api.request` for workflow jobs | `verself.job_events`, `verself.vm_lease_evidence` | Terminal row explains user-visible outcome. |
| Logs | `execution_logs`, `verself.job_logs` | `sandbox-rental.execution.run` | `verself.job_logs` | Product logs appear only after terminal output persistence. |
| Cleanup | allocation `cleaned`, bootstrap config deleted | `runner.cleanup`, `github.runner.cleanup`, `github.api.request` | River spans | GitHub runner deleted or already gone. |
| Golden promotion | durable current pointers | `golden.run.promote`, `durable.workflow_run.promote`, `durable.promote` | `verself.durable_events` | Promotion happens only after every observed job is successful or skipped. |

## RCA Inputs

Collect these before querying Verself:

- GitHub workflow run ID, from the URL `/actions/runs/{run_id}`.
- GitHub job ID, from `gh run view` or the job URL.
- Repository full name.
- Approximate time window.
- GitHub webhook delivery ID when available from the GitHub App delivery UI.

Commands:

```bash
gh run view <run_id> --repo <owner>/<repo> --json status,conclusion,jobs,updatedAt,url
gh run view <run_id> --repo <owner>/<repo> --log
```

`gh run view --log` only returns complete logs after GitHub considers the job log available. During an active run, use the GitHub UI for live logs.

## RCA Procedure

### 1. Locate Provider Demand

```bash
aspect db pg query --db=sandbox_rental --query='
SELECT
  provider, provider_job_id, provider_installation_id, provider_repository_id,
  repository_full_name, provider_run_id, provider_run_attempt, job_name,
  status, conclusion, labels_json, runner_id, runner_name,
  last_webhook_delivery, created_at, updated_at, started_at, completed_at
FROM runner_jobs
WHERE provider = ''github''
  AND (provider_run_id = <github_run_id> OR provider_job_id = <github_job_id>)
ORDER BY updated_at DESC;
'
```

Interpretation:

- No row means the webhook was not accepted or a later GitHub API refresh has not observed the job.
- `status='queued'` with no allocation means demand was recorded but capacity did not get created.
- `runner_id=0` and empty `runner_name` on an `in_progress` job means provider assignment evidence has not arrived.
- `last_webhook_delivery` is the best correlation key for webhook spans and River jobs.

### 2. Verify Installation And Repository Ownership

```bash
aspect db pg query --db=sandbox_rental --query='
SELECT
  c.org_id, c.state AS connection_state, i.installation_id, i.active AS installation_active,
  a.account_login, a.account_type, i.repository_selection, i.permissions_json,
  c.created_at, c.updated_at
FROM github_installation_connections c
JOIN github_installations i ON i.installation_id = c.installation_id
JOIN github_accounts a ON a.account_id = i.account_id
WHERE i.installation_id = <installation_id>
ORDER BY c.updated_at DESC;
'

aspect db pg query --db=sandbox_rental --query='
SELECT
  provider, provider_repository_id, org_id, provider_owner, provider_repo,
  repository_full_name, active, created_at, updated_at
FROM runner_provider_repositories
WHERE provider = ''github''
  AND provider_repository_id = <repository_id>;
'
```

Interpretation:

- Missing installation connection means onboarding never completed for the org.
- Missing repository row means repository sync did not run or the repository is outside the installation selection.
- Inactive repository row means it disappeared from the most recent repository sync.
- Different `org_id` means execution ownership is bound to a different Verself org.

### 3. Inspect River Work

```bash
aspect db pg query --db=sandbox_rental --query='
SELECT
  id, kind, queue, state, attempt, max_attempts, args,
  errors[array_length(errors, 1)] AS last_error,
  created_at, attempted_at, finalized_at, scheduled_at
FROM river_job
WHERE args @> jsonb_build_object(''provider_job_id'', <github_job_id>)
   OR args @> jsonb_build_object(''provider_run_id'', <github_run_id>)
ORDER BY id;
'
```

Expected jobs for a normal queued job:

- `runner.capacity.reconcile`
- `runner.allocate`
- `execution.advance`
- `runner.job.bind`
- `runner.cleanup`
- `golden.run.promote` after a completed workflow job webhook

Interpretation:

- `retryable` or `discarded` on `runner.capacity.reconcile` points before allocation.
- `retryable` or `discarded` on `runner.allocate` points at GitHub JIT, runner group policy, or execution submission.
- `retryable` or `discarded` on `execution.advance` points at billing, VM lease, exec start/wait, durable cache, or log finalization.
- `runner.cleanup` can be retryable while GitHub still considers a runner busy.

### 4. Inspect Allocation State

```bash
aspect db pg query --db=sandbox_rental --query='
SELECT
  allocation_id, provider, provider_installation_id, provider_repository_id,
  runner_class, runner_name, provider_runner_id, execution_id, attempt_id,
  state, requested_for_provider_job_id, failure_reason,
  allocate_by, jit_by, vm_submitted_by, runner_listening_by,
  assignment_by, vm_exit_by, cleanup_by, created_at, updated_at
FROM runner_allocations
WHERE provider = ''github''
  AND requested_for_provider_job_id = <github_job_id>
ORDER BY created_at DESC;
'
```

Allocation states:

- `pending`: row exists, allocate worker has not started or failed before state transition.
- `jit_creating`: allocating GitHub runner capacity.
- `jit_created`: GitHub returned runner ID and encoded JIT config.
- `vm_submitted`: execution row exists and VM worker has been enqueued.
- `runner_config_fetched`: guest consumed one-time JIT config.
- `assigned`: GitHub assigned the job to this runner.
- `job_completed`: GitHub completed before or during binding.
- `vm_exited`: VM execution terminalized and cleanup has been enqueued.
- `cleaned`: GitHub runner was deleted and bootstrap config removed.
- `failed`: allocation failed before normal cleanup; inspect `failure_reason`.

Deadline columns show the progress expected by the reconciler. A state whose deadline is in the past should be failed by `Reconcile()` and queued for cleanup.

### 5. Inspect Execution State

```bash
aspect db pg query --db=sandbox_rental --query='
SELECT
  e.execution_id, e.org_id, e.actor_id, e.source_kind, e.workload_kind,
  e.source_ref, e.runner_class, e.external_provider, e.external_task_id,
  e.state AS execution_state, e.correlation_id,
  a.attempt_id, a.attempt_seq, a.state AS attempt_state,
  a.lease_id, a.exec_id, a.billing_job_id, a.failure_reason,
  a.exit_code, a.duration_ms, a.stdout_bytes, a.stderr_bytes,
  a.trace_id, a.started_at, a.completed_at, a.created_at, a.updated_at
FROM executions e
JOIN execution_attempts a ON a.execution_id = e.execution_id
WHERE e.execution_id = ''<execution_id>''
ORDER BY a.attempt_seq DESC;
'

aspect db pg query --db=sandbox_rental --query='
SELECT event_seq, execution_id, attempt_id, from_state, to_state, reason, trace_id, created_at
FROM execution_events
WHERE execution_id = ''<execution_id>''
ORDER BY event_seq;
'
```

Execution attempt states:

- `queued`: submitted and waiting for `execution.advance`.
- `reserved`: billing reservation path started.
- `launching`: billing reserved and VM lease path started.
- `running`: lease and exec IDs are durable.
- `finalizing`: exec returned and service is settling billing/logs/durable state.
- `succeeded`, `failed`, `canceled`, `lost`: terminal.

Interpretation:

- No execution for an allocation means `runner.allocate` failed before `Submit`.
- `queued` with no River job means submit and queue insertion did not commit atomically.
- `reserved` or `launching` past the reconciler timeout points at billing or lease launch.
- `running` with lease/exec IDs should be checked against `vm-orchestrator` evidence.
- Terminal failure reason should map to one failed side effect.

### 6. Inspect VM Evidence

```bash
aspect db ch query --query="
SELECT
  evidence_time, service_name, lease_id, exec_id, evidence_type,
  diagnostic_kind, reason_code, reason, trace_id, span_id
FROM verself.vm_lease_evidence
WHERE lease_id = '<lease_id>'
ORDER BY evidence_time;
"
```

Expected evidence:

- `lease_ready`
- `exec_started`
- `telemetry_hello`
- optional `telemetry_diagnostic`
- `lease_cleanup`

Interpretation:

- No `lease_ready` means the lease never became available.
- `lease_ready` without `exec_started` means StartExec or guest control failed.
- `exec_started` without terminal execution state means `WaitExec`, worker lifetime, or finalization failed.
- Telemetry diagnostics with missing samples indicate guest telemetry loss, not necessarily job failure.

### 7. Inspect Traces

```bash
aspect db ch query --query="
SELECT
  Timestamp, TraceId, SpanId, ParentSpanId, ServiceName, SpanName,
  StatusCode, StatusMessage, Duration / 1000000 AS duration_ms,
  SpanAttributes
FROM default.otel_traces
WHERE Timestamp > now() - toIntervalHour(6)
  AND ServiceName IN ('sandbox-rental-service', 'vm-orchestrator')
  AND (
       SpanAttributes['github.workflow_run.id'] = '<github_run_id>'
    OR SpanAttributes['github.workflow_run.attempt'] = '<github_run_attempt>'
    OR SpanAttributes['github.job_id'] = '<github_job_id>'
    OR SpanAttributes['runner.provider_job_id'] = '<github_job_id>'
    OR SpanAttributes['verself.correlation_id'] = '<delivery_id>'
    OR SpanAttributes['execution.id'] = '<execution_id>'
    OR SpanAttributes['attempt.id'] = '<attempt_id>'
    OR SpanAttributes['runner.allocation_id'] = '<allocation_id>'
  )
ORDER BY Timestamp;
"
```

High-signal spans:

- `github.installation.begin`
- `github.installation.complete`
- `github.installation.repositories.sync`
- `github.webhook.workflow_job`
- `river.work/runner.capacity.reconcile`
- `github.capacity.reconcile`
- `river.work/runner.allocate`
- `github.runner.allocate`
- `github.runner_group.reconcile`
- `github.api.request`
- `sandbox-rental.execution.submit`
- `river.work/execution.advance`
- `sandbox-rental.execution.run`
- `runner.bootstrap.consume`
- `river.work/runner.job.bind`
- `github.job.bind`
- `river.work/runner.cleanup`
- `github.runner.cleanup`
- `durable.workflow_run.promote`

### 8. Inspect Product Logs

```bash
aspect db pg query --db=sandbox_rental --query='
SELECT execution_id, org_id, attempt_id, seq, stream, length(chunk) AS bytes, created_at
FROM execution_logs
WHERE execution_id = ''<execution_id>''
ORDER BY seq;
'

aspect db ch query --query="
SELECT
  created_at, execution_id, attempt_id, seq, stream,
  length(chunk) AS bytes, left(chunk, 200) AS preview
FROM verself.job_logs
WHERE execution_id = toUUID('<execution_id>')
ORDER BY created_at, seq;
"
```

Interpretation:

- GitHub job logs can exist while Verself product logs are empty.
- Verself product logs are currently written as one combined terminal chunk.
- Empty Verself logs with a nonterminal attempt is expected.
- Empty Verself logs with a terminal attempt means final output persistence did not happen or the orchestrator returned no output.

### 9. Inspect GitHub Runner State

Use the organization runner API when the token has org runner permissions:

```bash
gh api /orgs/<org>/actions/runners --paginate \
  --jq '.runners[] | select(.name == "<runner_name>")'
```

Interpretation:

- Missing runner after allocation `jit_created` can mean GitHub accepted JIT config and then the runner was deleted by failure cleanup.
- Runner present and idle while job is queued points at runner group policy, labels, or GitHub scheduler behavior.
- Runner present and busy while cleanup is retryable means cleanup should wait until GitHub releases the job.

## Common Failure Signatures

| Symptom | Likely Failed Point | Evidence |
| --- | --- | --- |
| GitHub run stuck queued, no `runner_jobs` row | Webhook delivery or HMAC | HTTP access logs, GitHub delivery UI, no `github.webhook.workflow_job` span. |
| `runner_jobs.status='queued'`, no allocation | Capacity reconcile | Missing/retryable `runner.capacity.reconcile`, no matching runner class, missing repository ownership row. |
| Allocation `failed`, `failure_reason='execution_submit_failed'` | Submit transaction | River error on `runner.allocate`, PostgreSQL error, no execution row. |
| Allocation `jit_created`, no execution | `runner.allocate` failed after GitHub JIT | GitHub runner ID exists, River error explains submit path. |
| Allocation `vm_submitted`, no `runner_config_fetched` | Guest never fetched JIT config | No `/internal/sandbox/v1/github-runner-jit` HTTP span, no `runner.bootstrap.consume`. |
| Allocation `runner_config_fetched`, GitHub still queued | GitHub scheduler did not assign runner | Runner labels/group policy, org runner visibility, GitHub API runner status. |
| GitHub job in progress, Verself attempt failed | Control-plane finalization bug or host failure | Attempt failure reason, `vm_lease_evidence`, `sandbox-rental.execution.run` error. |
| GitHub logs missing | Job not assigned or runner never uploaded logs | GitHub job status, runner assignment fields. |
| Verself logs missing | Terminal output persistence did not happen | `execution_logs`, `verself.job_logs`, `sandbox-rental.execution.run` finalization spans. |
| Cleanup retryable 422 | GitHub runner still busy | `runner.cleanup` error and GitHub job/runner state. |
| Golden promotion absent after success | Promotion gate deferred | `golden.run.promote`, `durable.workflow_run.promote`, job completion set. |

## Redrive

Prefer natural reconciliation first. `sandbox-rental-service` runs `Reconcile()` every 15 seconds. It repairs stale allocations, cleaned runner attempts, terminal runner allocations, and queued runner jobs without active allocations.

Manual redrive is for operator RCA only. Use it when the state rows are correct and River work is missing or exhausted.

```bash
aspect db pg query --db=sandbox_rental --query='
INSERT INTO river_job (kind, queue, args, max_attempts, tags)
VALUES (
  ''runner.capacity.reconcile'',
  ''runner'',
  jsonb_build_object(
    ''provider'', ''github'',
    ''provider_job_id'', <github_job_id>,
    ''correlation_id'', ''manual-rca-<github_run_id>'',
    ''submitted_at'', now()::text
  ),
  5,
  ARRAY[''runner'', ''capacity'']::varchar[]
)
RETURNING id, kind, queue, state, args;
'
```

After redrive, repeat the allocation, execution, River, and trace queries. A successful redrive should create a new allocation or show an existing active allocation.

## Instrumentation Gaps To Close

The current log persistence path is terminal and coarse. A production log service should stream output chunks during execution into ClickHouse with `(execution_id, attempt_id, seq, stream, source, created_at)`, durable truncation/drop metadata, and a pagination cursor that does not depend on terminal execution success.

Runner allocation and execution states should eventually emit a first-class append-only runner event table beside `execution_events`. Today, the current projection tables and spans are enough for RCA, but a runner event ledger would make deadline reconciliation and customer support timelines simpler.

GitHub webhook delivery redelivery should be part of the operator checklist. The durable delivery ID is already stored as `runner_jobs.last_webhook_delivery` and propagated as correlation ID for queued/bind work.

## Primary Sources

- Service ownership and tenant invariants: `src/services/sandbox-rental-service/AGENTS.md`.
- Setup and GitHub App settings: `src/services/sandbox-rental-service/docs/github-app-runner-setup.md`.
- Execution state machine: `src/services/sandbox-rental-service/docs/vm-execution-control-plane.md`.
- GitHub runner adapter: `src/services/sandbox-rental-service/internal/jobs/github_runner.go`.
- Runner workers and queues: `src/services/sandbox-rental-service/internal/jobs/scheduler_workers.go` and `src/services/sandbox-rental-service/internal/scheduler/runtime.go`.
- Postgres schema: `src/services/sandbox-rental-service/migrations/001_initial_schema.up.sql`.
- GitHub workflow job webhook: https://docs.github.com/en/webhooks/webhook-events-and-payloads#workflow_job
- GitHub self-hosted runners REST API: https://docs.github.com/en/rest/actions/self-hosted-runners
- GitHub workflow run jobs REST API: https://docs.github.com/en/rest/actions/workflow-jobs
- GitHub webhook redelivery: https://docs.github.com/en/webhooks/testing-and-troubleshooting-webhooks/redelivering-webhooks
