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
    COALESCE(b.provider_job_id, a.requested_for_provider_job_id)::bigint AS provider_job_id,
    e.runner_class,
    a.runner_name
FROM runner_allocations a
JOIN executions e ON e.execution_id = a.execution_id
JOIN runner_provider_repositories p ON p.provider = a.provider
    AND p.provider_repository_id = a.provider_repository_id
    AND p.active
LEFT JOIN runner_job_bindings b ON b.allocation_id = a.allocation_id
LEFT JOIN runner_jobs j ON j.provider = a.provider
    AND j.provider_job_id = COALESCE(b.provider_job_id, a.requested_for_provider_job_id)
LEFT JOIN LATERAL (
    SELECT gi.event_name, gi.head_sha, gi.head_branch, gi.head_repository_full_name,
           gi.base_sha, gi.base_branch, gi.workflow_path, gi.pull_request_number
    FROM github_workflow_invocations gi
    WHERE a.provider = 'github'
      AND gi.provider_installation_id = a.provider_installation_id
      AND gi.provider_repository_id = a.provider_repository_id
      AND gi.provider_run_id = COALESCE(j.provider_run_id, 0)
    ORDER BY
      CASE WHEN COALESCE(j.provider_run_attempt, 0) <> 0 AND gi.provider_run_attempt = j.provider_run_attempt THEN 0 ELSE 1 END,
      CASE WHEN gi.event_name <> '' THEN 0 ELSE 1 END,
      gi.provider_run_attempt DESC
    LIMIT 1
) inv ON true
WHERE a.execution_id = sqlc.arg(execution_id)
  AND a.attempt_id = sqlc.arg(attempt_id);

-- name: AttachRunnerAllocationExecution :execrows
UPDATE runner_allocations
SET execution_id = sqlc.arg(execution_id),
    attempt_id = sqlc.arg(attempt_id),
    state = 'vm_submitted',
    vm_submitted_by = sqlc.arg(updated_at),
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
  AND requested_for_provider_job_id = sqlc.arg(provider_job_id)
  AND state IN (
        'pending',
        'jit_creating',
        'jit_created',
        'bootstrap_creating',
        'bootstrap_created',
        'vm_submitted',
        'runner_config_fetched',
        'assigned'
      )
ORDER BY created_at DESC
LIMIT 1;

-- name: GetRunnerJobForBinding :one
SELECT runner_id, runner_name, status
FROM runner_jobs
WHERE provider = sqlc.arg(provider)
  AND provider_job_id = sqlc.arg(provider_job_id);

-- name: FindAllocationForRunner :one
SELECT allocation_id, requested_for_provider_job_id
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

-- name: ListQueuedRunnerJobsWithoutActiveAllocation :many
SELECT j.provider, j.provider_job_id
FROM runner_jobs j
WHERE (
        (j.provider = 'github' AND j.status = 'queued')
     OR (j.provider = 'forgejo' AND j.status IN ('waiting', 'queued'))
      )
  AND NOT EXISTS (
        SELECT 1
        FROM runner_allocations a
        WHERE a.provider = j.provider
          AND a.requested_for_provider_job_id = j.provider_job_id
          AND a.state IN (
                'pending',
                'jit_creating',
                'jit_created',
                'bootstrap_creating',
                'bootstrap_created',
                'vm_submitted',
                'runner_config_fetched',
                'assigned'
          )
      )
  AND NOT EXISTS (
        SELECT 1
        FROM runner_allocations a
        WHERE a.provider = j.provider
          AND a.requested_for_provider_job_id = j.provider_job_id
          AND a.created_at > now() - interval '60 seconds'
      )
ORDER BY j.updated_at ASC, j.provider, j.provider_job_id
LIMIT 50;
