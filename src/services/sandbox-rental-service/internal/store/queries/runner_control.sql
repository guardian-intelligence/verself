-- name: MarkRunnerExecutionExited :many
UPDATE runner_allocations
SET state = CASE WHEN state = 'cleaned' THEN state ELSE 'vm_exited' END,
    vm_exit_by = sqlc.arg(updated_at),
    updated_at = sqlc.arg(updated_at)
WHERE execution_id = sqlc.arg(execution_id)
RETURNING allocation_id;

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
    allocation_id, attempt_id, fetch_token_hash, bootstrap_kind, bootstrap_payload, expires_at, created_at
) VALUES (
    sqlc.arg(allocation_id), sqlc.arg(attempt_id), sqlc.arg(fetch_token_hash),
    sqlc.arg(bootstrap_kind), sqlc.arg(bootstrap_payload), sqlc.arg(expires_at), sqlc.arg(created_at)
)
ON CONFLICT (allocation_id) DO UPDATE SET
    attempt_id = EXCLUDED.attempt_id,
    fetch_token_hash = EXCLUDED.fetch_token_hash,
    bootstrap_kind = EXCLUDED.bootstrap_kind,
    bootstrap_payload = EXCLUDED.bootstrap_payload,
    expires_at = EXCLUDED.expires_at,
    consumed_at = NULL;

-- name: LockRunnerBootstrapConfigByTokenHash :one
SELECT allocation_id, bootstrap_kind, bootstrap_payload, expires_at, consumed_at
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

-- name: DeleteRunnerBootstrapConfig :exec
DELETE FROM runner_bootstrap_configs
WHERE allocation_id = sqlc.arg(allocation_id);

-- name: InsertRunnerJobBinding :exec
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
  AND state NOT IN ('failed', 'cleaned')
ORDER BY created_at DESC
LIMIT 1;

-- name: GetRunnerJobForBinding :one
SELECT runner_id, runner_name, status
FROM runner_jobs
WHERE provider = sqlc.arg(provider)
  AND provider_job_id = sqlc.arg(provider_job_id);

-- name: FindAllocationForRunner :one
SELECT allocation_id
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
