-- name: InsertExportJob :exec
INSERT INTO governance_export_jobs (
    export_id, org_id, requested_by, idempotency_key_hash, scopes, include_logs,
    format, state, expires_at
) VALUES (
    sqlc.arg(export_id), sqlc.arg(org_id), sqlc.arg(requested_by),
    sqlc.arg(idempotency_key_hash), sqlc.arg(scopes), sqlc.arg(include_logs),
    'tar.gz', 'running', sqlc.arg(expires_at)
);

-- name: MarkExportFailed :exec
UPDATE governance_export_jobs
SET state = 'failed',
    error_code = 'export-build-failed',
    error_message = sqlc.arg(error_message),
    updated_at = now()
WHERE export_id = sqlc.arg(export_id);

-- name: CompleteExportJob :exec
UPDATE governance_export_jobs
SET state = 'completed',
    artifact_path = sqlc.arg(artifact_path),
    artifact_sha256 = sqlc.arg(artifact_sha256),
    artifact_bytes = sqlc.arg(artifact_bytes),
    manifest = sqlc.arg(manifest),
    updated_at = now(),
    completed_at = now()
WHERE export_id = sqlc.arg(export_id);

-- name: DeleteExportFiles :exec
DELETE FROM governance_export_files
WHERE export_id = sqlc.arg(export_id);

-- name: InsertExportFile :exec
INSERT INTO governance_export_files (export_id, path, content_type, row_count, bytes, sha256)
VALUES (
    sqlc.arg(export_id), sqlc.arg(path), sqlc.arg(content_type),
    sqlc.arg(row_count), sqlc.arg(bytes), sqlc.arg(sha256)
);

-- name: ListExportsForOrg :many
SELECT export_id, org_id, requested_by, scopes, include_logs, format, state,
       artifact_path, artifact_sha256, artifact_bytes, manifest, error_code, error_message,
       created_at, updated_at, completed_at, expires_at
FROM governance_export_jobs
WHERE org_id = sqlc.arg(org_id)
ORDER BY created_at DESC, export_id DESC
LIMIT 25;

-- name: GetExportByIDAndOrg :one
SELECT export_id, org_id, requested_by, scopes, include_logs, format, state,
       artifact_path, artifact_sha256, artifact_bytes, manifest, error_code, error_message,
       created_at, updated_at, completed_at, expires_at
FROM governance_export_jobs
WHERE export_id = sqlc.arg(export_id)
  AND org_id = sqlc.arg(org_id);

-- name: GetExportByIdempotency :one
SELECT export_id, org_id, requested_by, scopes, include_logs, format, state,
       artifact_path, artifact_sha256, artifact_bytes, manifest, error_code, error_message,
       created_at, updated_at, completed_at, expires_at
FROM governance_export_jobs
WHERE org_id = sqlc.arg(org_id)
  AND idempotency_key_hash = sqlc.arg(idempotency_key_hash);

-- name: MarkExportDownloaded :exec
UPDATE governance_export_jobs
SET downloaded_at = now(), updated_at = now()
WHERE export_id = sqlc.arg(export_id)
  AND org_id = sqlc.arg(org_id);

-- name: ListExportFiles :many
SELECT path, content_type, row_count, bytes, sha256
FROM governance_export_files
WHERE export_id = sqlc.arg(export_id)
ORDER BY path;
