-- name: ListStickyDisks :many
SELECT
    g.provider_installation_id,
    g.provider_repository_id,
    p.repository_full_name,
    g.key_hash,
    g.key,
    g.current_generation,
    g.current_source_ref,
    last_mount.last_used_at,
    last_mount.completed_at,
    COALESCE(last_mount.save_state, '')::text AS save_state,
    COALESCE(last_mount.execution_id, '00000000-0000-0000-0000-000000000000'::uuid) AS execution_id,
    COALESCE(last_mount.attempt_id, '00000000-0000-0000-0000-000000000000'::uuid) AS attempt_id,
    COALESCE(last_mount.runner_class, '')::text AS runner_class,
    COALESCE(last_mount.workflow_name, '')::text AS workflow_name,
    COALESCE(last_mount.job_name, '')::text AS job_name,
    COALESCE(last_mount.mount_path, '')::text AS mount_path,
    g.updated_at
FROM runner_sticky_disk_generations g
JOIN github_installations i ON i.installation_id = g.provider_installation_id
JOIN runner_provider_repositories p ON p.provider = 'github' AND p.provider_repository_id = g.provider_repository_id AND p.active
JOIN github_installation_connections c ON c.installation_id = i.installation_id AND c.org_id = p.org_id AND c.state = 'active'
LEFT JOIN LATERAL (
    SELECT
        COALESCE(m.completed_at, m.requested_at, m.updated_at, m.created_at) AS last_used_at,
        m.completed_at,
        m.save_state,
        m.execution_id,
        m.attempt_id,
        a.runner_class,
        COALESCE(j.workflow_name, '') AS workflow_name,
        COALESCE(j.job_name, '') AS job_name,
        m.mount_path
    FROM execution_sticky_disk_mounts m
    JOIN runner_allocations a ON a.allocation_id = m.allocation_id
    LEFT JOIN runner_job_bindings b ON b.allocation_id = a.allocation_id
    LEFT JOIN runner_jobs j ON j.provider = a.provider AND j.provider_job_id = COALESCE(b.provider_job_id, a.requested_for_provider_job_id)
    WHERE a.provider = 'github'
      AND a.provider_installation_id = g.provider_installation_id
      AND a.provider_repository_id = g.provider_repository_id
      AND m.key_hash = g.key_hash
    ORDER BY COALESCE(m.completed_at, m.requested_at, m.updated_at, m.created_at) DESC, m.mount_id DESC
    LIMIT 1
) last_mount ON true
WHERE p.org_id = sqlc.arg(org_id)
  AND i.active
  AND (sqlc.arg(repository)::text = '' OR p.repository_full_name = sqlc.arg(repository))
  AND g.provider = 'github'
  AND (sqlc.arg(cursor_enabled)::boolean = false OR (g.updated_at, g.provider_installation_id, g.provider_repository_id, g.key_hash) < (sqlc.arg(cursor_updated_at)::timestamptz, sqlc.arg(cursor_installation_id)::bigint, sqlc.arg(cursor_repository_id)::bigint, sqlc.arg(cursor_key_hash)::text))
ORDER BY g.updated_at DESC, g.provider_installation_id DESC, g.provider_repository_id DESC, g.key_hash DESC
LIMIT sqlc.arg(limit_count);

-- name: DeleteStickyDiskGenerationForOrg :one
DELETE FROM runner_sticky_disk_generations g
USING github_installations i,
      runner_provider_repositories p,
      github_installation_connections c
WHERE g.provider = 'github'
  AND g.provider_installation_id = sqlc.arg(provider_installation_id)
  AND g.provider_repository_id = sqlc.arg(provider_repository_id)
  AND g.key_hash = sqlc.arg(key_hash)
  AND i.installation_id = g.provider_installation_id
  AND p.provider = 'github'
  AND p.provider_repository_id = g.provider_repository_id
  AND p.org_id = sqlc.arg(org_id)
  AND p.active
  AND c.installation_id = i.installation_id
  AND c.org_id = p.org_id
  AND c.state = 'active'
  AND i.active
RETURNING g.current_source_ref;
