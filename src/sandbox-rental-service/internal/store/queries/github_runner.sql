-- name: InsertGitHubInstallationState :exec
INSERT INTO github_installation_states (
    state, org_id, actor_id, expires_at, created_at
) VALUES (
    sqlc.arg(state), sqlc.arg(org_id), sqlc.arg(actor_id), sqlc.arg(expires_at), sqlc.arg(created_at)
)
ON CONFLICT (state) DO NOTHING;

-- name: ListGitHubInstallations :many
SELECT installation_id, org_id, account_login, account_type, active, created_at, updated_at
FROM github_installations
WHERE org_id = sqlc.arg(org_id)
ORDER BY updated_at DESC;

-- name: LockGitHubInstallationState :one
SELECT org_id, actor_id, expires_at
FROM github_installation_states
WHERE state = sqlc.arg(state)
FOR UPDATE;

-- name: UpsertGitHubInstallation :one
INSERT INTO github_installations (
    installation_id, org_id, account_login, account_type, active, created_at, updated_at
) VALUES (
    sqlc.arg(installation_id), sqlc.arg(org_id), sqlc.arg(account_login), sqlc.arg(account_type), true, sqlc.arg(updated_at), sqlc.arg(updated_at)
)
ON CONFLICT (installation_id) DO UPDATE SET
    org_id = EXCLUDED.org_id,
    account_login = EXCLUDED.account_login,
    account_type = EXCLUDED.account_type,
    active = true,
    updated_at = EXCLUDED.updated_at
RETURNING installation_id, org_id, account_login, account_type, active, created_at, updated_at;

-- name: DeleteGitHubInstallationState :exec
DELETE FROM github_installation_states
WHERE state = sqlc.arg(state);

-- name: UpsertGitHubRunnerJob :exec
INSERT INTO runner_jobs (
    provider, provider_job_id, provider_installation_id, provider_repository_id, repository_full_name,
    provider_run_id, job_name, head_sha, head_branch, workflow_name,
    status, conclusion, labels_json, runner_id, runner_name, started_at, completed_at,
    last_webhook_delivery, updated_at
) VALUES (
    'github', sqlc.arg(provider_job_id), sqlc.arg(provider_installation_id), sqlc.arg(provider_repository_id), sqlc.arg(repository_full_name),
    sqlc.arg(provider_run_id), sqlc.arg(job_name), sqlc.arg(head_sha), sqlc.arg(head_branch), sqlc.arg(workflow_name),
    sqlc.arg(status), sqlc.arg(conclusion), sqlc.arg(labels_json)::jsonb, sqlc.arg(runner_id), sqlc.arg(runner_name),
    sqlc.narg(started_at), sqlc.narg(completed_at), sqlc.arg(last_webhook_delivery), sqlc.arg(updated_at)
)
ON CONFLICT (provider, provider_job_id) DO UPDATE SET
    job_name = EXCLUDED.job_name,
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
    last_webhook_delivery = EXCLUDED.last_webhook_delivery,
    updated_at = EXCLUDED.updated_at;

-- name: GetGitHubQueuedJob :one
SELECT
    j.provider_job_id,
    j.provider_installation_id,
    j.provider_repository_id,
    j.repository_full_name,
    j.provider_run_id,
    j.job_name,
    j.head_sha,
    j.head_branch,
    j.labels_json,
    i.org_id,
    i.account_login
FROM runner_jobs j
JOIN github_installations i ON i.installation_id = j.provider_installation_id AND i.active
WHERE j.provider = 'github'
  AND j.provider_job_id = sqlc.arg(provider_job_id)
  AND j.status = 'queued';

-- name: InsertGitHubRunnerAllocation :exec
INSERT INTO runner_allocations (
    allocation_id, provider, provider_installation_id, provider_repository_id, runner_class, runner_name, state,
    requested_for_provider_job_id, allocate_by, jit_by, vm_submitted_by, runner_listening_by,
    assignment_by, vm_exit_by, cleanup_by, created_at, updated_at
) VALUES (
    sqlc.arg(allocation_id), 'github', sqlc.arg(provider_installation_id), sqlc.arg(provider_repository_id),
    sqlc.arg(runner_class), sqlc.arg(runner_name), 'pending', sqlc.arg(requested_for_provider_job_id),
    sqlc.arg(allocate_by), sqlc.arg(jit_by), sqlc.arg(vm_submitted_by), sqlc.arg(runner_listening_by),
    sqlc.arg(assignment_by), sqlc.arg(vm_exit_by), sqlc.arg(cleanup_by), sqlc.arg(created_at), sqlc.arg(created_at)
);

-- name: UpdateGitHubAllocationJITCreated :exec
UPDATE runner_allocations
SET provider_runner_id = sqlc.arg(provider_runner_id),
    runner_name = sqlc.arg(runner_name),
    state = 'jit_created',
    updated_at = sqlc.arg(updated_at)
WHERE allocation_id = sqlc.arg(allocation_id);

-- name: UpdateRunnerExecutionExternalTaskID :exec
UPDATE executions e
SET external_task_id = sqlc.arg(external_task_id),
    updated_at = sqlc.arg(updated_at)
FROM runner_allocations a
WHERE a.allocation_id = sqlc.arg(allocation_id)
  AND a.execution_id = e.execution_id
  AND e.workload_kind = 'runner';

-- name: GetGitHubAllocationIDByExecution :one
SELECT allocation_id
FROM runner_allocations
WHERE provider = 'github'
  AND execution_id = sqlc.arg(execution_id);

-- name: GetGitHubAllocation :one
SELECT
    a.allocation_id,
    a.provider_installation_id,
    a.provider_repository_id,
    a.runner_class,
    a.runner_name,
    a.provider_runner_id,
    a.requested_for_provider_job_id,
    COALESCE(j.provider_run_id, 0)::bigint AS provider_run_id,
    COALESCE(j.job_name, '')::text AS job_name,
    COALESCE(j.head_sha, '')::text AS head_sha,
    COALESCE(j.head_branch, '')::text AS head_branch,
    COALESCE(a.execution_id, '00000000-0000-0000-0000-000000000000'::uuid) AS execution_id,
    COALESCE(a.attempt_id, '00000000-0000-0000-0000-000000000000'::uuid) AS attempt_id,
    a.state,
    i.org_id,
    i.account_login,
    COALESCE(j.repository_full_name, '')::text AS repository_full_name,
    c.product_id,
    c.vcpus,
    c.memory_mib,
    c.rootfs_gib
FROM runner_allocations a
JOIN github_installations i ON i.installation_id = a.provider_installation_id
JOIN runner_classes c ON c.runner_class = a.runner_class
LEFT JOIN runner_jobs j ON j.provider = a.provider AND j.provider_job_id = a.requested_for_provider_job_id
WHERE a.provider = 'github'
  AND a.allocation_id = sqlc.arg(allocation_id);
