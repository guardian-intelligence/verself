-- name: Ping :one
SELECT 1::int AS one;

-- name: LockIdempotencyKey :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(lock_key), 0::bigint));

-- name: InsertProject :exec
INSERT INTO projects (
    project_id, org_id, slug, display_name, description, state, version, created_by, updated_by, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: UpdateProject :execrows
UPDATE projects
SET slug = $3,
    display_name = $4,
    description = $5,
    version = version + 1,
    updated_by = $6,
    updated_at = $7
WHERE org_id = $1 AND project_id = $2 AND version = $8 AND state = 'active';

-- name: SetProjectState :execrows
UPDATE projects
SET state = $3,
    version = version + 1,
    updated_by = $4,
    updated_at = $5,
    archived_at = sqlc.narg(archived_at)
WHERE org_id = $1 AND project_id = $2 AND version = $6;

-- name: InsertProjectSlugRedirect :exec
INSERT INTO project_slug_redirects (org_id, slug, project_id, created_by, created_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT DO NOTHING;

-- name: InsertEnvironment :exec
INSERT INTO project_environments (
    environment_id, project_id, org_id, slug, display_name, kind, state, protection_policy, version, created_by, updated_by, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13);

-- name: UpdateEnvironment :execrows
UPDATE project_environments
SET display_name = $4,
    protection_policy = $5,
    version = version + 1,
    updated_by = $6,
    updated_at = $7
WHERE org_id = $1 AND project_id = $2 AND environment_id = $3 AND version = $8 AND state = 'active';

-- name: SetEnvironmentState :execrows
UPDATE project_environments
SET state = $4,
    version = version + 1,
    updated_by = $5,
    updated_at = $6,
    archived_at = sqlc.narg(archived_at)
WHERE org_id = $1 AND project_id = $2 AND environment_id = $3 AND version = $7;

-- name: InsertProjectEvent :exec
INSERT INTO project_events (
    event_id, org_id, project_id, environment_id, event_type, actor_id, payload, trace_id, traceparent, created_at
) VALUES ($1, $2, $3, sqlc.narg(environment_id), $4, $5, $6, $7, $8, $9);

-- name: InsertProjectIdempotencyRecord :exec
INSERT INTO project_idempotency_records (
    org_id, operation, key_hash, request_hash, result_kind, result_project_id, result_environment_id, result_payload, created_at
) VALUES ($1, $2, $3, $4, $5, $6, NULL, $7, $8);

-- name: InsertEnvironmentIdempotencyRecord :exec
INSERT INTO project_idempotency_records (
    org_id, operation, key_hash, request_hash, result_kind, result_project_id, result_environment_id, result_payload, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);
