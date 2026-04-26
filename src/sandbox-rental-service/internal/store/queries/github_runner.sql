-- name: InsertGitHubInstallationState :exec
INSERT INTO github_installation_states (
    state, org_id, actor_id, expires_at, created_at
) VALUES (
    sqlc.arg(state), sqlc.arg(org_id), sqlc.arg(actor_id), sqlc.arg(expires_at), sqlc.arg(created_at)
)
ON CONFLICT (state) DO NOTHING;

-- name: ListGitHubInstallations :many
SELECT
    i.installation_id,
    c.org_id,
    a.account_login,
    a.account_type,
    (i.active AND c.state = 'active')::boolean AS active,
    c.created_at,
    GREATEST(a.updated_at, i.updated_at, c.updated_at)::timestamptz AS updated_at
FROM github_installation_connections c
JOIN github_installations i ON i.installation_id = c.installation_id
JOIN github_accounts a ON a.account_id = i.account_id
WHERE c.org_id = sqlc.arg(org_id)
ORDER BY c.updated_at DESC, i.installation_id DESC;

-- name: LockGitHubInstallationState :one
SELECT org_id, actor_id, expires_at
FROM github_installation_states
WHERE state = sqlc.arg(state)
FOR UPDATE;

-- name: UpsertGitHubAccount :exec
INSERT INTO github_accounts (
    account_id, account_login, account_type, created_at, updated_at
) VALUES (
    sqlc.arg(account_id), sqlc.arg(account_login), sqlc.arg(account_type), sqlc.arg(updated_at), sqlc.arg(updated_at)
)
ON CONFLICT (account_id) DO UPDATE SET
    account_login = EXCLUDED.account_login,
    account_type = EXCLUDED.account_type,
    updated_at = EXCLUDED.updated_at;

-- name: UpsertGitHubInstallation :exec
INSERT INTO github_installations (
    installation_id, account_id, active, repository_selection, permissions_json, created_at, updated_at
) VALUES (
    sqlc.arg(installation_id), sqlc.arg(account_id), true, sqlc.arg(repository_selection), sqlc.arg(permissions_json)::jsonb,
    sqlc.arg(updated_at), sqlc.arg(updated_at)
)
ON CONFLICT (installation_id) DO UPDATE SET
    account_id = EXCLUDED.account_id,
    active = true,
    repository_selection = EXCLUDED.repository_selection,
    permissions_json = EXCLUDED.permissions_json,
    updated_at = EXCLUDED.updated_at;

-- name: UpsertGitHubInstallationConnection :exec
INSERT INTO github_installation_connections (
    connection_id, installation_id, org_id, connected_by_actor_id, state, created_at, updated_at
) VALUES (
    sqlc.arg(connection_id), sqlc.arg(installation_id), sqlc.arg(org_id), sqlc.arg(connected_by_actor_id),
    'active', sqlc.arg(updated_at), sqlc.arg(updated_at)
)
ON CONFLICT (installation_id, org_id) DO UPDATE SET
    connected_by_actor_id = EXCLUDED.connected_by_actor_id,
    state = 'active',
    updated_at = EXCLUDED.updated_at;

-- name: GetGitHubInstallationForOrg :one
SELECT
    i.installation_id,
    c.org_id,
    a.account_login,
    a.account_type,
    (i.active AND c.state = 'active')::boolean AS active,
    c.created_at,
    GREATEST(a.updated_at, i.updated_at, c.updated_at)::timestamptz AS updated_at
FROM github_installation_connections c
JOIN github_installations i ON i.installation_id = c.installation_id
JOIN github_accounts a ON a.account_id = i.account_id
WHERE c.org_id = sqlc.arg(org_id)
  AND i.installation_id = sqlc.arg(installation_id);

-- name: UpsertGitHubRunnerRepository :execrows
INSERT INTO runner_provider_repositories (
    provider, provider_repository_id, org_id, project_id, source_repository_id,
    provider_owner, provider_repo, repository_full_name, active, created_at, updated_at
) VALUES (
    'github', sqlc.arg(provider_repository_id), sqlc.arg(org_id), NULL, NULL,
    sqlc.arg(provider_owner), sqlc.arg(provider_repo), sqlc.arg(repository_full_name),
    true, sqlc.arg(updated_at), sqlc.arg(updated_at)
)
ON CONFLICT (provider, provider_repository_id) DO UPDATE SET
    org_id = EXCLUDED.org_id,
    project_id = NULL,
    source_repository_id = NULL,
    provider_owner = EXCLUDED.provider_owner,
    provider_repo = EXCLUDED.provider_repo,
    repository_full_name = EXCLUDED.repository_full_name,
    active = true,
    updated_at = EXCLUDED.updated_at
WHERE runner_provider_repositories.org_id = EXCLUDED.org_id;

-- name: DeactivateMissingGitHubRunnerRepositories :exec
UPDATE runner_provider_repositories
SET active = false,
    updated_at = sqlc.arg(updated_at)
WHERE provider = 'github'
  AND org_id = sqlc.arg(org_id)
  AND provider_owner = sqlc.arg(provider_owner)
  AND active
  AND NOT (provider_repository_id = ANY(sqlc.arg(provider_repository_ids)::bigint[]));

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
    p.org_id,
    a.account_login
FROM runner_jobs j
JOIN github_installations i ON i.installation_id = j.provider_installation_id AND i.active
JOIN github_accounts a ON a.account_id = i.account_id
JOIN runner_provider_repositories p ON p.provider = 'github' AND p.provider_repository_id = j.provider_repository_id AND p.active
JOIN github_installation_connections c ON c.installation_id = i.installation_id AND c.org_id = p.org_id AND c.state = 'active'
WHERE j.provider = 'github'
  AND j.provider_job_id = sqlc.arg(provider_job_id)
  AND j.status = 'queued';

-- name: InsertGitHubRunnerAllocation :execrows
INSERT INTO runner_allocations (
    allocation_id, provider, provider_installation_id, provider_repository_id, runner_class, runner_name, state,
    requested_for_provider_job_id, allocate_by, jit_by, vm_submitted_by, runner_listening_by,
    assignment_by, vm_exit_by, cleanup_by, created_at, updated_at
) VALUES (
    sqlc.arg(allocation_id), 'github', sqlc.arg(provider_installation_id), sqlc.arg(provider_repository_id),
    sqlc.arg(runner_class), sqlc.arg(runner_name), 'pending', sqlc.arg(requested_for_provider_job_id),
    sqlc.arg(allocate_by), sqlc.arg(jit_by), sqlc.arg(vm_submitted_by), sqlc.arg(runner_listening_by),
    sqlc.arg(assignment_by), sqlc.arg(vm_exit_by), sqlc.arg(cleanup_by), sqlc.arg(created_at), sqlc.arg(created_at)
)
ON CONFLICT DO NOTHING;

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
    p.org_id,
    ga.account_login,
    COALESCE(NULLIF(j.repository_full_name, ''), p.repository_full_name, '')::text AS repository_full_name,
    c.product_id,
    c.vcpus,
    c.memory_mib,
    c.rootfs_gib
FROM runner_allocations a
JOIN github_installations i ON i.installation_id = a.provider_installation_id
JOIN github_accounts ga ON ga.account_id = i.account_id
JOIN runner_provider_repositories p ON p.provider = 'github' AND p.provider_repository_id = a.provider_repository_id AND p.active
JOIN github_installation_connections gc ON gc.installation_id = i.installation_id AND gc.org_id = p.org_id AND gc.state = 'active'
JOIN runner_classes c ON c.runner_class = a.runner_class
LEFT JOIN runner_jobs j ON j.provider = a.provider AND j.provider_job_id = a.requested_for_provider_job_id
WHERE a.provider = 'github'
  AND a.allocation_id = sqlc.arg(allocation_id);

-- name: GetGitHubAllocationForCleanup :one
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
    p.org_id,
    ga.account_login,
    COALESCE(NULLIF(j.repository_full_name, ''), p.repository_full_name, '')::text AS repository_full_name,
    c.product_id,
    c.vcpus,
    c.memory_mib,
    c.rootfs_gib
FROM runner_allocations a
JOIN github_installations i ON i.installation_id = a.provider_installation_id
JOIN github_accounts ga ON ga.account_id = i.account_id
JOIN runner_provider_repositories p ON p.provider = 'github' AND p.provider_repository_id = a.provider_repository_id
JOIN runner_classes c ON c.runner_class = a.runner_class
LEFT JOIN runner_jobs j ON j.provider = a.provider AND j.provider_job_id = a.requested_for_provider_job_id
WHERE a.provider = 'github'
  AND a.allocation_id = sqlc.arg(allocation_id);
