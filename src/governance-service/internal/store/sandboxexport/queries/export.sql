-- name: ExportSandboxExecutionsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM executions WHERE org_id::text = sqlc.arg(org_id) ORDER BY created_at, execution_id) t;

-- name: ExportSandboxExecutionAttemptsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (
    SELECT a.*
    FROM execution_attempts a
    JOIN executions e ON e.execution_id = a.execution_id
    WHERE e.org_id::text = sqlc.arg(org_id)
    ORDER BY a.created_at,
             a.attempt_id
) t;

-- name: ExportSandboxExecutionEventsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (
    SELECT ev.*
    FROM execution_events ev
    JOIN executions e ON e.execution_id = ev.execution_id
    WHERE e.org_id::text = sqlc.arg(org_id)
    ORDER BY ev.created_at,
             ev.event_seq
) t;

-- name: ExportSandboxExecutionBillingWindowsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (
    SELECT bw.*
    FROM execution_billing_windows bw
    JOIN execution_attempts a ON a.attempt_id = bw.attempt_id
    JOIN executions e ON e.execution_id = a.execution_id
    WHERE e.org_id::text = sqlc.arg(org_id)
    ORDER BY bw.window_start,
             bw.billing_window_id
) t;

-- name: ExportSandboxGithubInstallationsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM github_installations WHERE org_id::text = sqlc.arg(org_id) ORDER BY created_at, installation_id) t;

-- name: ExportSandboxRunnerProviderRepositoriesJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM runner_provider_repositories WHERE org_id::text = sqlc.arg(org_id) ORDER BY created_at, provider, provider_repository_id) t;

-- name: ExportSandboxRunnerJobsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (
    SELECT j.*
    FROM runner_jobs j
    LEFT JOIN github_installations i ON j.provider = 'github' AND i.installation_id = j.provider_installation_id
    LEFT JOIN runner_provider_repositories r ON r.provider = j.provider AND r.provider_repository_id = j.provider_repository_id
    WHERE COALESCE(i.org_id, r.org_id)::text = sqlc.arg(org_id)
    ORDER BY j.created_at,
             j.provider,
             j.provider_job_id
) t;

-- name: ExportSandboxRunnerAllocationsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (
    SELECT a.*
    FROM runner_allocations a
    LEFT JOIN github_installations i ON a.provider = 'github' AND i.installation_id = a.provider_installation_id
    LEFT JOIN runner_provider_repositories r ON r.provider = a.provider AND r.provider_repository_id = a.provider_repository_id
    WHERE COALESCE(i.org_id, r.org_id)::text = sqlc.arg(org_id)
    ORDER BY a.created_at,
             a.allocation_id
) t;

-- name: ExportSandboxRunnerJobBindingsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (
    SELECT b.*
    FROM runner_job_bindings b
    JOIN runner_allocations a ON a.allocation_id = b.allocation_id
    LEFT JOIN github_installations i ON a.provider = 'github' AND i.installation_id = a.provider_installation_id
    LEFT JOIN runner_provider_repositories r ON r.provider = a.provider AND r.provider_repository_id = a.provider_repository_id
    WHERE COALESCE(i.org_id, r.org_id)::text = sqlc.arg(org_id)
    ORDER BY b.created_at,
             b.binding_id
) t;

-- name: ExportSandboxExecutionFilesystemMountsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (
    SELECT m.*
    FROM execution_filesystem_mounts m
    JOIN executions e ON e.execution_id = m.execution_id
    WHERE e.org_id::text = sqlc.arg(org_id)
    ORDER BY m.execution_id,
             m.sort_order,
             m.mount_name
) t;

-- name: ExportSandboxRunnerStickyDiskGenerationsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (
    SELECT g.*
    FROM runner_sticky_disk_generations g
    LEFT JOIN github_installations i ON g.provider = 'github' AND i.installation_id = g.provider_installation_id
    LEFT JOIN runner_provider_repositories r ON r.provider = g.provider AND r.provider_repository_id = g.provider_repository_id
    WHERE COALESCE(i.org_id, r.org_id)::text = sqlc.arg(org_id)
    ORDER BY g.updated_at,
             g.provider,
             g.provider_installation_id,
             g.provider_repository_id,
             g.key_hash
) t;

-- name: ExportSandboxExecutionStickyDiskMountsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (
    SELECT m.*
    FROM execution_sticky_disk_mounts m
    JOIN executions e ON e.execution_id = m.execution_id
    WHERE e.org_id::text = sqlc.arg(org_id)
    ORDER BY m.created_at,
             m.mount_id
) t;

-- name: ExportSandboxVMResourceBoundsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM vm_resource_bounds WHERE org_id::text = sqlc.arg(org_id) ORDER BY updated_at) t;

-- name: ExportSandboxExecutionLogsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (
    SELECT l.*
    FROM execution_logs l
    JOIN executions e ON e.execution_id = l.execution_id
    WHERE e.org_id::text = sqlc.arg(org_id)
    ORDER BY l.attempt_id,
             l.seq
) t;
