-- name: UpsertForgejoRunnerRepository :exec
INSERT INTO runner_provider_repositories (
    provider, provider_repository_id, org_id, project_id, source_repository_id, provider_owner, provider_repo, repository_full_name, active, created_at, updated_at
) VALUES (
    'forgejo', sqlc.arg(provider_repository_id), sqlc.arg(org_id), sqlc.arg(project_id), sqlc.narg(source_repository_id),
    sqlc.arg(provider_owner), sqlc.arg(provider_repo), sqlc.arg(repository_full_name), true, sqlc.arg(updated_at), sqlc.arg(updated_at)
)
ON CONFLICT (provider, provider_repository_id) DO UPDATE SET
    org_id = EXCLUDED.org_id,
    project_id = EXCLUDED.project_id,
    source_repository_id = EXCLUDED.source_repository_id,
    provider_owner = EXCLUDED.provider_owner,
    provider_repo = EXCLUDED.provider_repo,
    repository_full_name = EXCLUDED.repository_full_name,
    active = true,
    updated_at = EXCLUDED.updated_at;

-- name: InsertForgejoRunnerAllocation :execrows
INSERT INTO runner_allocations (
    allocation_id, provider, provider_installation_id, provider_repository_id, runner_class, runner_name, state,
    requested_for_provider_job_id, allocate_by, jit_by, vm_submitted_by, runner_listening_by,
    assignment_by, vm_exit_by, cleanup_by, created_at, updated_at
) VALUES (
    sqlc.arg(allocation_id), 'forgejo', 0, sqlc.arg(provider_repository_id), sqlc.arg(runner_class), sqlc.arg(runner_name),
    'pending', sqlc.arg(requested_for_provider_job_id), sqlc.arg(allocate_by), sqlc.arg(jit_by),
    sqlc.arg(vm_submitted_by), sqlc.arg(runner_listening_by), sqlc.arg(assignment_by),
    sqlc.arg(vm_exit_by), sqlc.arg(cleanup_by), sqlc.arg(created_at), sqlc.arg(created_at)
)
ON CONFLICT DO NOTHING;

-- name: UpdateForgejoAllocationBootstrapCreated :exec
UPDATE runner_allocations
SET provider_runner_id = sqlc.arg(provider_runner_id),
    state = 'bootstrap_created',
    updated_at = sqlc.arg(updated_at)
WHERE provider = 'forgejo'
  AND allocation_id = sqlc.arg(allocation_id);

-- name: GetForgejoBindAllocation :one
SELECT a.allocation_id, j.status
FROM runner_allocations a
JOIN runner_jobs j ON j.provider = a.provider AND j.provider_job_id = a.requested_for_provider_job_id
WHERE a.provider = 'forgejo'
  AND j.provider_job_id = sqlc.arg(provider_job_id)
ORDER BY a.created_at DESC
LIMIT 1;

-- name: GetForgejoRepository :one
SELECT provider_repository_id, org_id, project_id, source_repository_id, provider_owner, provider_repo, repository_full_name
FROM runner_provider_repositories
WHERE provider = 'forgejo'
  AND provider_repository_id = sqlc.arg(provider_repository_id)
  AND active;

-- name: UpsertForgejoRunnerJob :exec
INSERT INTO runner_jobs (
    provider, provider_job_id, provider_installation_id, provider_repository_id, repository_full_name,
    provider_run_id, provider_task_id, provider_job_handle, job_name, status, labels_json, updated_at, created_at
) VALUES (
    'forgejo', sqlc.arg(provider_job_id), 0, sqlc.arg(provider_repository_id), sqlc.arg(repository_full_name),
    sqlc.arg(provider_run_id), sqlc.arg(provider_task_id), sqlc.arg(provider_job_handle), sqlc.arg(job_name),
    sqlc.arg(status), sqlc.arg(labels_json)::jsonb, sqlc.arg(updated_at), sqlc.arg(updated_at)
)
ON CONFLICT (provider, provider_job_id) DO UPDATE SET
    provider_repository_id = EXCLUDED.provider_repository_id,
    repository_full_name = EXCLUDED.repository_full_name,
    provider_task_id = EXCLUDED.provider_task_id,
    provider_job_handle = EXCLUDED.provider_job_handle,
    job_name = EXCLUDED.job_name,
    status = EXCLUDED.status,
    labels_json = EXCLUDED.labels_json,
    updated_at = EXCLUDED.updated_at;

-- name: GetForgejoQueuedJob :one
SELECT
    j.provider_job_id,
    j.provider_repository_id,
    j.provider_task_id,
    j.provider_job_handle,
    j.repository_full_name,
    j.job_name,
    j.head_sha,
    j.head_branch,
    j.labels_json,
    p.org_id
FROM runner_jobs j
JOIN runner_provider_repositories p ON p.provider = j.provider AND p.provider_repository_id = j.provider_repository_id AND p.active
WHERE j.provider = 'forgejo'
  AND j.provider_job_id = sqlc.arg(provider_job_id)
  AND j.status IN ('waiting', 'queued');

-- name: GetForgejoAllocation :one
SELECT
    a.allocation_id,
    a.provider_repository_id,
    a.runner_class,
    a.runner_name,
    a.provider_runner_id,
    a.requested_for_provider_job_id,
    COALESCE(j.provider_task_id, 0)::bigint AS provider_task_id,
    COALESCE(j.provider_job_handle, '')::text AS provider_job_handle,
    COALESCE(j.job_name, '')::text AS job_name,
    COALESCE(j.head_sha, '')::text AS head_sha,
    COALESCE(j.head_branch, '')::text AS head_branch,
    COALESCE(a.execution_id, '00000000-0000-0000-0000-000000000000'::uuid) AS execution_id,
    COALESCE(a.attempt_id, '00000000-0000-0000-0000-000000000000'::uuid) AS attempt_id,
    a.state,
    p.org_id,
    p.provider_owner,
    p.provider_repo,
    p.repository_full_name,
    COALESCE(j.labels_json, '[]'::jsonb) AS labels_json,
    c.product_id,
    c.vcpus,
    c.memory_mib,
    c.rootfs_gib
FROM runner_allocations a
JOIN runner_provider_repositories p ON p.provider = a.provider AND p.provider_repository_id = a.provider_repository_id
JOIN runner_classes c ON c.runner_class = a.runner_class
LEFT JOIN runner_jobs j ON j.provider = a.provider AND j.provider_job_id = a.requested_for_provider_job_id
WHERE a.provider = 'forgejo'
  AND a.allocation_id = sqlc.arg(allocation_id);
