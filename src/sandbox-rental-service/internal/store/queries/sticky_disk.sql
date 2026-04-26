-- name: GetGitHubExecutionIdentity :one
SELECT
    a.allocation_id,
    p.org_id,
    a.provider_installation_id,
    a.provider_repository_id,
    COALESCE(NULLIF(j.repository_full_name, ''), p.repository_full_name, '')::text AS repository_full_name,
    COALESCE(b.provider_job_id, a.requested_for_provider_job_id)::bigint AS provider_job_id,
    a.runner_name
FROM runner_allocations a
JOIN github_installations i ON i.installation_id = a.provider_installation_id
JOIN runner_provider_repositories p ON p.provider = 'github' AND p.provider_repository_id = a.provider_repository_id
JOIN github_installation_connections c ON c.installation_id = i.installation_id AND c.org_id = p.org_id
LEFT JOIN runner_job_bindings b ON b.allocation_id = a.allocation_id
LEFT JOIN runner_jobs j ON j.provider = a.provider AND j.provider_job_id = COALESCE(b.provider_job_id, a.requested_for_provider_job_id)
WHERE a.provider = 'github'
  AND a.execution_id = sqlc.arg(execution_id)
  AND a.attempt_id = sqlc.arg(attempt_id);

-- name: RequestStickyDiskCommit :one
UPDATE execution_sticky_disk_mounts
SET save_requested = true,
    save_state = sqlc.arg(save_state),
    requested_at = sqlc.arg(requested_at),
    started_at = NULL,
    completed_at = NULL,
    failure_reason = '',
    updated_at = sqlc.arg(requested_at)
WHERE attempt_id = sqlc.arg(attempt_id)
  AND key_hash = sqlc.arg(key_hash)
  AND mount_path = sqlc.arg(mount_path)
RETURNING mount_id, requested_at;

-- name: ListPendingStickyDiskCommits :many
SELECT
    m.mount_id,
    m.mount_name,
    m.key,
    m.key_hash,
    m.mount_path,
    m.base_generation,
    m.target_source_ref,
    m.execution_id,
    m.attempt_id,
    m.allocation_id,
    p.org_id,
    a.provider_installation_id,
    a.provider_repository_id,
    COALESCE(NULLIF(j.repository_full_name, ''), p.repository_full_name, '')::text AS repository_full_name,
    COALESCE(b.provider_job_id, a.requested_for_provider_job_id)::bigint AS provider_job_id,
    a.runner_name
FROM execution_sticky_disk_mounts m
JOIN runner_allocations a ON a.allocation_id = m.allocation_id
JOIN github_installations i ON i.installation_id = a.provider_installation_id
JOIN runner_provider_repositories p ON p.provider = 'github' AND p.provider_repository_id = a.provider_repository_id
JOIN github_installation_connections c ON c.installation_id = i.installation_id AND c.org_id = p.org_id
LEFT JOIN runner_job_bindings b ON b.allocation_id = a.allocation_id
LEFT JOIN runner_jobs j ON j.provider = a.provider AND j.provider_job_id = COALESCE(b.provider_job_id, a.requested_for_provider_job_id)
WHERE m.attempt_id = sqlc.arg(attempt_id)
  AND m.save_requested
  AND m.save_state = sqlc.arg(save_state)
  AND a.provider = 'github'
ORDER BY m.requested_at, m.mount_name;

-- name: MarkStickyDiskCommitRunning :execrows
UPDATE execution_sticky_disk_mounts
SET save_state = sqlc.arg(to_state),
    started_at = sqlc.arg(started_at),
    updated_at = sqlc.arg(started_at)
WHERE mount_id = sqlc.arg(mount_id)
  AND save_state = sqlc.arg(from_state);

-- name: MarkStickyDiskCommitFinished :exec
UPDATE execution_sticky_disk_mounts
SET save_state = sqlc.arg(save_state),
    failure_reason = sqlc.arg(failure_reason),
    committed_generation = sqlc.arg(committed_generation),
    committed_snapshot = sqlc.arg(committed_snapshot),
    completed_at = sqlc.arg(completed_at),
    updated_at = sqlc.arg(completed_at)
WHERE mount_id = sqlc.arg(mount_id);

-- name: LockStickyDiskGeneration :one
SELECT current_generation
FROM runner_sticky_disk_generations
WHERE provider = 'github'
  AND provider_installation_id = sqlc.arg(provider_installation_id)
  AND provider_repository_id = sqlc.arg(provider_repository_id)
  AND key_hash = sqlc.arg(key_hash)
FOR UPDATE;

-- name: UpsertStickyDiskGeneration :exec
INSERT INTO runner_sticky_disk_generations (
    provider, provider_installation_id, provider_repository_id, key_hash, key, current_generation, current_source_ref, created_at, updated_at
) VALUES (
    'github', sqlc.arg(provider_installation_id), sqlc.arg(provider_repository_id),
    sqlc.arg(key_hash), sqlc.arg(key), sqlc.arg(current_generation), sqlc.arg(current_source_ref),
    sqlc.arg(updated_at), sqlc.arg(updated_at)
)
ON CONFLICT (provider, provider_installation_id, provider_repository_id, key_hash) DO UPDATE SET
    key = EXCLUDED.key,
    current_generation = EXCLUDED.current_generation,
    current_source_ref = EXCLUDED.current_source_ref,
    updated_at = EXCLUDED.updated_at;

-- name: GetCurrentStickyDiskGeneration :one
SELECT current_generation, current_source_ref
FROM runner_sticky_disk_generations
WHERE provider = 'github'
  AND provider_installation_id = sqlc.arg(provider_installation_id)
  AND provider_repository_id = sqlc.arg(provider_repository_id)
  AND key_hash = sqlc.arg(key_hash);
