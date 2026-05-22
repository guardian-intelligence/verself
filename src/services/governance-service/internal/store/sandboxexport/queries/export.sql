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

-- name: ExportSandboxRunnerProviderRepositoriesJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM runner_provider_repositories WHERE org_id::text = sqlc.arg(org_id) ORDER BY created_at, provider, provider_repository_id) t;

-- name: ExportSandboxRunnerJobsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (
    SELECT j.*
    FROM runner_jobs j
    LEFT JOIN runner_provider_repositories r ON r.provider = j.provider AND r.provider_repository_id = j.provider_repository_id
    WHERE r.org_id::text = sqlc.arg(org_id)
    ORDER BY j.created_at,
             j.provider,
	             j.provider_job_id
) t;

-- name: ExportSandboxRunnerJobCacheManifestsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (
    SELECT
        m.provider,
        m.provider_job_id,
        m.source_kind,
        m.source_path,
        m.source_sha,
        m.content_sha256,
        encode(m.content_bytes, 'base64') AS content_base64,
        m.created_at,
        m.updated_at
    FROM runner_job_cache_manifests m
    JOIN runner_jobs j ON j.provider = m.provider AND j.provider_job_id = m.provider_job_id
    JOIN runner_provider_repositories r ON r.provider = j.provider AND r.provider_repository_id = j.provider_repository_id
    WHERE r.org_id::text = sqlc.arg(org_id)
    ORDER BY m.updated_at, m.provider, m.provider_job_id
) t;

-- name: ExportSandboxRunnerAllocationsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (
    SELECT a.*
    FROM runner_allocations a
    LEFT JOIN runner_provider_repositories r ON r.provider = a.provider AND r.provider_repository_id = a.provider_repository_id
    WHERE r.org_id::text = sqlc.arg(org_id)
    ORDER BY a.created_at,
             a.allocation_id
) t;

-- name: ExportSandboxRunnerJobBindingsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (
    SELECT b.*
    FROM runner_job_bindings b
    JOIN runner_allocations a ON a.allocation_id = b.allocation_id
    LEFT JOIN runner_provider_repositories r ON r.provider = a.provider AND r.provider_repository_id = a.provider_repository_id
    WHERE r.org_id::text = sqlc.arg(org_id)
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
