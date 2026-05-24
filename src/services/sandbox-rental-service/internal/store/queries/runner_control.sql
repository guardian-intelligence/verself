-- name: MarkRunnerExecutionExited :many
UPDATE runner_allocations
SET state = CASE WHEN state = 'cleaned' THEN state ELSE 'vm_exited' END,
    vm_exit_by = sqlc.arg(updated_at),
    updated_at = sqlc.arg(updated_at)
WHERE execution_id = sqlc.arg(execution_id)
RETURNING allocation_id;

-- name: ListTerminalRunnerExecutionsWithLiveAllocations :many
SELECT DISTINCT e.execution_id
FROM executions e
JOIN execution_attempts a ON a.execution_id = e.execution_id
JOIN runner_allocations ra ON ra.execution_id = e.execution_id
WHERE e.workload_kind = 'runner'
  AND a.state IN ('succeeded', 'failed', 'canceled', 'lost')
  AND ra.state NOT IN ('cleaned', 'failed', 'job_completed', 'vm_exited')
ORDER BY e.execution_id
LIMIT 50;

-- name: LockRunnerAllocationProvider :one
SELECT provider
FROM runner_allocations
WHERE allocation_id = sqlc.arg(allocation_id)
FOR UPDATE;

-- name: GetRunnerAllocationProvider :one
SELECT provider
FROM runner_allocations
WHERE allocation_id = sqlc.arg(allocation_id);

-- name: UpsertProviderRunnerRepository :execrows
INSERT INTO runner_provider_repositories (
    provider, provider_repository_id, org_id, project_id, source_repository_id,
    provider_owner, provider_repo, repository_full_name, active, created_at, updated_at
) VALUES (
    sqlc.arg(provider), sqlc.arg(provider_repository_id), sqlc.arg(org_id),
    sqlc.narg(project_id), sqlc.narg(source_repository_id),
    sqlc.arg(provider_owner), sqlc.arg(provider_repo), sqlc.arg(repository_full_name),
    true, sqlc.arg(updated_at), sqlc.arg(updated_at)
)
ON CONFLICT (provider, provider_repository_id) DO UPDATE SET
    org_id = EXCLUDED.org_id,
    project_id = EXCLUDED.project_id,
    source_repository_id = EXCLUDED.source_repository_id,
    provider_owner = EXCLUDED.provider_owner,
    provider_repo = EXCLUDED.provider_repo,
    repository_full_name = EXCLUDED.repository_full_name,
    active = true,
    updated_at = EXCLUDED.updated_at
WHERE runner_provider_repositories.org_id = EXCLUDED.org_id;

-- name: UpsertProviderRunnerJob :exec
INSERT INTO runner_jobs (
    provider, provider_job_id, provider_installation_id, provider_repository_id, repository_full_name,
    provider_run_id, provider_run_attempt, job_name, head_sha, head_branch, workflow_name,
    status, conclusion, labels_json, runner_id, runner_name, started_at, completed_at,
    last_webhook_delivery, updated_at
) VALUES (
    sqlc.arg(provider), sqlc.arg(provider_job_id), sqlc.arg(provider_installation_id),
    sqlc.arg(provider_repository_id), sqlc.arg(repository_full_name),
    sqlc.arg(provider_run_id), sqlc.arg(provider_run_attempt), sqlc.arg(job_name),
    sqlc.arg(head_sha), sqlc.arg(head_branch), sqlc.arg(workflow_name),
    sqlc.arg(status), sqlc.arg(conclusion), sqlc.arg(labels_json)::jsonb,
    sqlc.arg(runner_id), sqlc.arg(runner_name), sqlc.narg(started_at),
    sqlc.narg(completed_at), sqlc.arg(last_webhook_delivery), sqlc.arg(updated_at)
)
ON CONFLICT (provider, provider_job_id) DO UPDATE SET
    provider_installation_id = CASE WHEN EXCLUDED.provider_installation_id <> 0 THEN EXCLUDED.provider_installation_id ELSE runner_jobs.provider_installation_id END,
    provider_repository_id = CASE WHEN EXCLUDED.provider_repository_id <> 0 THEN EXCLUDED.provider_repository_id ELSE runner_jobs.provider_repository_id END,
    repository_full_name = COALESCE(NULLIF(EXCLUDED.repository_full_name, ''), runner_jobs.repository_full_name),
    provider_run_id = CASE WHEN EXCLUDED.provider_run_id <> 0 THEN EXCLUDED.provider_run_id ELSE runner_jobs.provider_run_id END,
    provider_run_attempt = CASE WHEN EXCLUDED.provider_run_attempt <> 0 THEN EXCLUDED.provider_run_attempt ELSE runner_jobs.provider_run_attempt END,
    job_name = COALESCE(NULLIF(EXCLUDED.job_name, ''), runner_jobs.job_name),
    head_sha = COALESCE(NULLIF(EXCLUDED.head_sha, ''), runner_jobs.head_sha),
    head_branch = COALESCE(NULLIF(EXCLUDED.head_branch, ''), runner_jobs.head_branch),
    workflow_name = COALESCE(NULLIF(EXCLUDED.workflow_name, ''), runner_jobs.workflow_name),
    status = EXCLUDED.status,
    conclusion = EXCLUDED.conclusion,
    labels_json = EXCLUDED.labels_json,
    runner_id = EXCLUDED.runner_id,
    runner_name = EXCLUDED.runner_name,
    started_at = COALESCE(EXCLUDED.started_at, runner_jobs.started_at),
    completed_at = COALESCE(EXCLUDED.completed_at, runner_jobs.completed_at),
    last_webhook_delivery = COALESCE(NULLIF(EXCLUDED.last_webhook_delivery, ''), runner_jobs.last_webhook_delivery),
    updated_at = EXCLUDED.updated_at;

-- name: UpsertProviderWorkflowRun :exec
INSERT INTO provider_workflow_runs (
    provider, provider_installation_id, provider_repository_id, provider_run_id,
    provider_run_attempt, repository_full_name, event_name, head_sha,
    head_branch, head_repository_full_name, base_sha, base_branch,
    pull_request_number, workflow_path, commit_count, updated_at
) VALUES (
    sqlc.arg(provider), sqlc.arg(provider_installation_id), sqlc.arg(provider_repository_id),
    sqlc.arg(provider_run_id), sqlc.arg(provider_run_attempt),
    sqlc.arg(repository_full_name), sqlc.arg(event_name), sqlc.arg(head_sha),
    sqlc.arg(head_branch), sqlc.arg(head_repository_full_name), sqlc.arg(base_sha),
    sqlc.arg(base_branch), sqlc.arg(pull_request_number), sqlc.arg(workflow_path),
    NULLIF(sqlc.arg(commit_count)::bigint, 0), sqlc.arg(updated_at)
)
ON CONFLICT (
    provider, provider_installation_id, provider_repository_id, provider_run_id,
    provider_run_attempt
) DO UPDATE SET
    repository_full_name = COALESCE(NULLIF(EXCLUDED.repository_full_name, ''), provider_workflow_runs.repository_full_name),
    event_name = COALESCE(NULLIF(EXCLUDED.event_name, ''), provider_workflow_runs.event_name),
    head_sha = COALESCE(NULLIF(EXCLUDED.head_sha, ''), provider_workflow_runs.head_sha),
    head_branch = COALESCE(NULLIF(EXCLUDED.head_branch, ''), provider_workflow_runs.head_branch),
    head_repository_full_name = COALESCE(NULLIF(EXCLUDED.head_repository_full_name, ''), provider_workflow_runs.head_repository_full_name),
    base_sha = COALESCE(NULLIF(EXCLUDED.base_sha, ''), provider_workflow_runs.base_sha),
    base_branch = COALESCE(NULLIF(EXCLUDED.base_branch, ''), provider_workflow_runs.base_branch),
    pull_request_number = CASE WHEN EXCLUDED.pull_request_number <> 0 THEN EXCLUDED.pull_request_number ELSE provider_workflow_runs.pull_request_number END,
    workflow_path = COALESCE(NULLIF(EXCLUDED.workflow_path, ''), provider_workflow_runs.workflow_path),
    commit_count = COALESCE(EXCLUDED.commit_count, provider_workflow_runs.commit_count),
    updated_at = EXCLUDED.updated_at;

-- name: GetProviderQueuedJobContext :one
SELECT
    j.provider,
    j.provider_job_id,
    j.provider_installation_id,
    j.provider_repository_id,
    COALESCE(NULLIF(j.repository_full_name, ''), p.repository_full_name, '')::text AS repository_full_name,
    j.provider_run_id,
    j.provider_run_attempt,
    j.job_name,
    j.head_sha,
    j.head_branch,
    j.workflow_name,
    j.labels_json,
    p.org_id
FROM runner_jobs j
JOIN runner_provider_repositories p ON p.provider = j.provider
    AND p.provider_repository_id = j.provider_repository_id
    AND p.active
WHERE j.provider = sqlc.arg(provider)
  AND j.provider_job_id = sqlc.arg(provider_job_id)
  AND j.status = sqlc.arg(status);

-- name: UpsertRunnerJobCacheManifest :exec
INSERT INTO runner_job_cache_manifests (
    provider, provider_job_id, source_kind, source_path, source_sha,
    content_sha256, content_bytes, created_at, updated_at
) VALUES (
    sqlc.arg(provider), sqlc.arg(provider_job_id), sqlc.arg(source_kind),
    sqlc.arg(source_path), sqlc.arg(source_sha), sqlc.arg(content_sha256),
    sqlc.arg(content_bytes), sqlc.arg(updated_at), sqlc.arg(updated_at)
)
ON CONFLICT (provider, provider_job_id) DO UPDATE SET
    source_kind = EXCLUDED.source_kind,
    source_path = EXCLUDED.source_path,
    source_sha = EXCLUDED.source_sha,
    content_sha256 = EXCLUDED.content_sha256,
    content_bytes = EXCLUDED.content_bytes,
    updated_at = EXCLUDED.updated_at;

-- name: GetRunnerJobCacheManifest :one
SELECT source_kind, source_path, source_sha, content_bytes
FROM runner_job_cache_manifests
WHERE provider = sqlc.arg(provider)
  AND provider_job_id = sqlc.arg(provider_job_id);

-- name: InsertProviderRunnerAllocation :execrows
INSERT INTO runner_allocations (
    allocation_id, provider, provider_installation_id, provider_repository_id, runner_class, runner_name,
    provider_runner_id, state, origin_provider_job_id, allocate_by, jit_by, vm_submitted_by,
    runner_listening_by, assignment_by, vm_exit_by, cleanup_by, created_at, updated_at
) VALUES (
    sqlc.arg(allocation_id), sqlc.arg(provider), sqlc.arg(provider_installation_id),
    sqlc.arg(provider_repository_id), sqlc.arg(runner_class), sqlc.arg(runner_name),
    sqlc.arg(provider_runner_id), sqlc.arg(state), sqlc.arg(origin_provider_job_id),
    sqlc.arg(allocate_by), sqlc.arg(jit_by), sqlc.arg(vm_submitted_by),
    sqlc.arg(runner_listening_by), sqlc.arg(assignment_by), sqlc.arg(vm_exit_by),
    sqlc.arg(cleanup_by), sqlc.arg(created_at), sqlc.arg(created_at)
)
ON CONFLICT DO NOTHING;

-- name: GetRunnerAllocationByExecution :one
SELECT allocation_id, provider
FROM runner_allocations
WHERE execution_id = sqlc.arg(execution_id);

-- name: GetRunnerExecutionIdentity :one
SELECT
    a.allocation_id,
    a.provider,
    p.org_id,
    a.provider_installation_id,
    a.provider_repository_id,
    COALESCE(NULLIF(j.repository_full_name, ''), p.repository_full_name, '')::text AS repository_full_name,
    COALESCE(j.provider_run_id, 0)::bigint AS provider_run_id,
    COALESCE(j.provider_run_attempt, 0)::bigint AS provider_run_attempt,
    COALESCE(j.workflow_name, '')::text AS workflow_name,
    COALESCE(j.job_name, '')::text AS job_name,
    COALESCE(j.head_sha, '')::text AS head_sha,
    COALESCE(j.head_branch, '')::text AS head_branch,
    COALESCE(inv.event_name, '')::text AS run_event_name,
    COALESCE(inv.head_sha, '')::text AS run_head_sha,
    COALESCE(inv.head_branch, '')::text AS run_head_branch,
    COALESCE(inv.head_repository_full_name, '')::text AS run_head_repository_full_name,
    COALESCE(inv.base_sha, '')::text AS run_base_sha,
    COALESCE(inv.base_branch, '')::text AS run_base_branch,
    COALESCE(inv.workflow_path, '')::text AS workflow_path,
    COALESCE(inv.pull_request_number, 0)::bigint AS pull_request_number,
    COALESCE(b.provider_job_id, a.origin_provider_job_id)::bigint AS provider_job_id,
    e.runner_class,
    a.runner_name
FROM runner_allocations a
JOIN executions e ON e.execution_id = a.execution_id
JOIN runner_provider_repositories p ON p.provider = a.provider
    AND p.provider_repository_id = a.provider_repository_id
    AND p.active
LEFT JOIN runner_job_bindings b ON b.allocation_id = a.allocation_id
LEFT JOIN runner_jobs j ON j.provider = a.provider
    AND j.provider_job_id = COALESCE(b.provider_job_id, a.origin_provider_job_id)
LEFT JOIN LATERAL (
    SELECT run.event_name, run.head_sha, run.head_branch, run.head_repository_full_name,
           run.base_sha, run.base_branch, run.workflow_path, run.pull_request_number
    FROM provider_workflow_runs run
    WHERE run.provider = a.provider
      AND run.provider_installation_id = a.provider_installation_id
      AND run.provider_repository_id = a.provider_repository_id
      AND run.provider_run_id = COALESCE(j.provider_run_id, 0)
    ORDER BY
      CASE WHEN COALESCE(j.provider_run_attempt, 0) <> 0 AND run.provider_run_attempt = j.provider_run_attempt THEN 0 ELSE 1 END,
      CASE WHEN run.event_name <> '' THEN 0 ELSE 1 END,
      run.provider_run_attempt DESC
    LIMIT 1
) inv ON true
WHERE a.execution_id = sqlc.arg(execution_id)
  AND a.attempt_id = sqlc.arg(attempt_id);

-- name: GetProviderExecutionIdentity :one
SELECT
    a.allocation_id,
    p.org_id,
    a.provider_installation_id,
    a.provider_repository_id,
    COALESCE(NULLIF(j.repository_full_name, ''), p.repository_full_name, '')::text AS repository_full_name,
    COALESCE(j.provider_run_id, 0)::bigint AS provider_run_id,
    COALESCE(j.head_branch, '')::text AS head_branch,
    COALESCE(b.provider_job_id, a.origin_provider_job_id)::bigint AS provider_job_id,
    e.runner_class,
    a.runner_name
FROM runner_allocations a
JOIN executions e ON e.execution_id = a.execution_id
JOIN runner_provider_repositories p ON p.provider = a.provider
    AND p.provider_repository_id = a.provider_repository_id
    AND p.active
LEFT JOIN runner_job_bindings b ON b.allocation_id = a.allocation_id
LEFT JOIN runner_jobs j ON j.provider = a.provider
    AND j.provider_job_id = COALESCE(b.provider_job_id, a.origin_provider_job_id)
WHERE a.provider = sqlc.arg(provider)
  AND a.execution_id = sqlc.arg(execution_id)
  AND a.attempt_id = sqlc.arg(attempt_id);

-- name: AttachRunnerAllocationExecution :execrows
UPDATE runner_allocations
SET execution_id = sqlc.arg(execution_id),
    attempt_id = sqlc.arg(attempt_id),
    state = 'vm_submitted',
    vm_submitted_by = sqlc.arg(updated_at),
    runner_listening_by = sqlc.arg(runner_listening_by),
    updated_at = sqlc.arg(updated_at)
WHERE allocation_id = sqlc.arg(allocation_id)
  AND state IN ('jit_created', 'pending', 'jit_creating', 'bootstrap_created', 'bootstrap_creating');

-- name: UpsertRunnerBootstrapConfig :exec
INSERT INTO runner_bootstrap_configs (
    allocation_id, attempt_id, fetch_token_hash, bootstrap_kind, bootstrap_secret_name, expires_at, created_at
) VALUES (
    sqlc.arg(allocation_id), sqlc.arg(attempt_id), sqlc.arg(fetch_token_hash),
    sqlc.arg(bootstrap_kind), sqlc.arg(bootstrap_secret_name), sqlc.arg(expires_at), sqlc.arg(created_at)
)
ON CONFLICT (allocation_id) DO UPDATE SET
    attempt_id = EXCLUDED.attempt_id,
    fetch_token_hash = EXCLUDED.fetch_token_hash,
    bootstrap_kind = EXCLUDED.bootstrap_kind,
    bootstrap_secret_name = EXCLUDED.bootstrap_secret_name,
    expires_at = EXCLUDED.expires_at,
    consumed_at = NULL;

-- name: LockRunnerBootstrapConfigByTokenHash :one
SELECT allocation_id, bootstrap_kind, bootstrap_secret_name, expires_at, consumed_at
FROM runner_bootstrap_configs
WHERE fetch_token_hash = sqlc.arg(fetch_token_hash)
FOR UPDATE;

-- name: MarkRunnerBootstrapConsumed :exec
UPDATE runner_bootstrap_configs
SET consumed_at = sqlc.arg(consumed_at)
WHERE allocation_id = sqlc.arg(allocation_id);

-- name: MarkRunnerAllocationConfigFetched :exec
UPDATE runner_allocations
SET state = CASE WHEN state = 'vm_submitted' THEN 'runner_config_fetched' ELSE state END,
    assignment_by = CASE WHEN state = 'vm_submitted' THEN sqlc.arg(assignment_by) ELSE assignment_by END,
    updated_at = sqlc.arg(updated_at)
WHERE allocation_id = sqlc.arg(allocation_id);

-- name: GetRunnerBootstrapSecretNameByAllocation :one
SELECT bootstrap_secret_name
FROM runner_bootstrap_configs
WHERE allocation_id = sqlc.arg(allocation_id);

-- name: DeleteRunnerBootstrapConfig :exec
DELETE FROM runner_bootstrap_configs
WHERE allocation_id = sqlc.arg(allocation_id);

-- name: InsertRunnerJobBinding :execrows
INSERT INTO runner_job_bindings (
    binding_id, allocation_id, provider, provider_job_id, provider_runner_id, runner_name, bound_at, created_at
) VALUES (
    sqlc.arg(binding_id), sqlc.arg(allocation_id), sqlc.arg(provider), sqlc.arg(provider_job_id),
    sqlc.arg(provider_runner_id), sqlc.arg(runner_name), sqlc.arg(bound_at), sqlc.arg(bound_at)
)
ON CONFLICT (provider, provider_job_id) DO NOTHING;

-- name: UpdateRunnerAllocationAssignment :exec
UPDATE runner_allocations
SET state = sqlc.arg(state),
    assignment_by = COALESCE(assignment_by, sqlc.arg(updated_at)),
    cleanup_by = sqlc.arg(cleanup_by),
    updated_at = sqlc.arg(updated_at)
WHERE provider = sqlc.arg(provider)
  AND allocation_id = sqlc.arg(allocation_id)
  AND state <> 'cleaned';

-- name: UpdateRunnerExecutionExternalTaskID :exec
UPDATE executions e
SET external_task_id = sqlc.arg(external_task_id),
    updated_at = sqlc.arg(updated_at)
FROM runner_allocations a
WHERE a.execution_id = e.execution_id
  AND a.allocation_id = sqlc.arg(allocation_id);

-- name: SetRunnerAllocationState :exec
UPDATE runner_allocations
SET state = sqlc.arg(state),
    failure_reason = sqlc.arg(failure_reason),
    updated_at = sqlc.arg(updated_at)
WHERE provider = sqlc.arg(provider)
  AND allocation_id = sqlc.arg(allocation_id);

-- name: MarkRunnerAllocationCleaned :exec
UPDATE runner_allocations
SET state = 'cleaned',
    cleanup_by = sqlc.arg(cleanup_by),
    updated_at = sqlc.arg(cleanup_by)
WHERE allocation_id = sqlc.arg(allocation_id);

-- name: GetActiveAllocationForRunnerJob :one
SELECT allocation_id
FROM runner_allocations
WHERE provider = sqlc.arg(provider)
  AND origin_provider_job_id = sqlc.arg(provider_job_id)
  AND state IN (
        'pending',
        'jit_creating',
        'jit_created',
        'bootstrap_creating',
        'bootstrap_created',
        'vm_submitted',
        'runner_config_fetched'
      )
ORDER BY created_at DESC
LIMIT 1;

-- name: GetRunnerAllocationSubmission :one
SELECT
    allocation_id,
    COALESCE(execution_id, '00000000-0000-0000-0000-000000000000'::uuid) AS execution_id,
    COALESCE(attempt_id, '00000000-0000-0000-0000-000000000000'::uuid) AS attempt_id,
    runner_name,
    provider_runner_id
FROM runner_allocations
WHERE allocation_id = sqlc.arg(allocation_id);

-- name: GetRunnerJobForBinding :one
SELECT runner_id, runner_name, status
FROM runner_jobs
WHERE provider = sqlc.arg(provider)
  AND provider_job_id = sqlc.arg(provider_job_id);

-- name: GetRunnerJobTerminalResult :one
SELECT status, conclusion
FROM runner_jobs
WHERE provider = sqlc.arg(provider)
  AND provider_job_id = sqlc.arg(provider_job_id);

-- name: ListWorkflowRunJobResults :many
SELECT provider_job_id, status, conclusion
FROM runner_jobs
WHERE provider = sqlc.arg(provider)
  AND provider_repository_id = sqlc.arg(provider_repository_id)
  AND provider_run_id = sqlc.arg(provider_run_id)
  AND provider_run_attempt = sqlc.arg(provider_run_attempt)
ORDER BY provider_job_id;

-- name: GetProviderWorkflowRun :one
SELECT
    provider,
    provider_installation_id,
    provider_repository_id,
    provider_run_id,
    provider_run_attempt,
    repository_full_name,
    event_name,
    head_sha,
    head_branch,
    head_repository_full_name,
    base_sha,
    base_branch,
    workflow_path,
    pull_request_number,
    COALESCE(commit_count, 0)::bigint AS commit_count
FROM provider_workflow_runs
WHERE provider = sqlc.arg(provider)
  AND provider_installation_id = sqlc.arg(provider_installation_id)
  AND provider_repository_id = sqlc.arg(provider_repository_id)
  AND provider_run_id = sqlc.arg(provider_run_id)
ORDER BY
  CASE WHEN sqlc.arg(provider_run_attempt)::bigint <> 0 AND provider_run_attempt = sqlc.arg(provider_run_attempt) THEN 0 ELSE 1 END,
  provider_run_attempt DESC
LIMIT 1;

-- name: FindAllocationForRunner :one
SELECT allocation_id, origin_provider_job_id
FROM runner_allocations
WHERE provider = sqlc.arg(provider)
  AND ((sqlc.arg(provider_runner_id)::bigint <> 0 AND provider_runner_id = sqlc.arg(provider_runner_id))
   OR (sqlc.arg(runner_name)::text <> '' AND runner_name = sqlc.arg(runner_name)))
ORDER BY created_at DESC
LIMIT 1;

-- name: ListActiveRunnerClasses :many
SELECT runner_class
FROM runner_classes
WHERE active
ORDER BY runner_class;

-- name: ListExpiredRunnerAllocations :many
-- Allocations whose current-state deadline column is in the past.
-- Each non-terminal state has exactly one "must-have-progressed-by" deadline
-- column; the CASE selects it. Terminals (cleaned, failed, job_completed) are
-- excluded so the reaper does not re-fail rows it already failed.
SELECT
    allocation_id,
    provider,
    state,
    execution_id,
    attempt_id,
    runner_name
FROM runner_allocations
WHERE state IN (
        'pending',
        'jit_creating',
        'jit_created',
        'bootstrap_creating',
        'bootstrap_created',
        'vm_submitted',
        'runner_config_fetched',
        'assigned',
        'vm_exited'
      )
  AND CASE state
        WHEN 'pending'              THEN allocate_by
        WHEN 'jit_creating'         THEN jit_by
        WHEN 'jit_created'          THEN vm_submitted_by
        WHEN 'bootstrap_creating'   THEN jit_by
        WHEN 'bootstrap_created'    THEN vm_submitted_by
        WHEN 'vm_submitted'         THEN runner_listening_by
        WHEN 'runner_config_fetched' THEN assignment_by
        WHEN 'assigned'             THEN vm_exit_by
        WHEN 'vm_exited'            THEN cleanup_by
      END < now()
ORDER BY allocation_id
LIMIT 50;
