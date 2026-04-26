-- name: GetProjectByID :one
SELECT project_id, org_id, slug, display_name, description, state, version, created_by, updated_by, created_at, updated_at, archived_at
FROM projects
WHERE org_id = $1 AND project_id = $2;

-- name: GetProjectBySlug :one
SELECT project_id, org_id, slug, display_name, description, state, version, created_by, updated_by, created_at, updated_at, archived_at
FROM projects
WHERE org_id = $1 AND slug = $2;

-- name: GetProjectByRedirectSlug :one
SELECT p.project_id, p.org_id, p.slug, p.display_name, p.description, p.state, p.version, p.created_by, p.updated_by, p.created_at, p.updated_at, p.archived_at
FROM project_slug_redirects r
JOIN projects p ON p.project_id = r.project_id AND p.org_id = r.org_id
WHERE r.org_id = $1 AND r.slug = $2;

-- name: ProjectSlugUnavailable :one
SELECT EXISTS (
    SELECT 1 FROM projects p WHERE p.org_id = $1 AND p.slug = $2
) OR EXISTS (
    SELECT 1 FROM project_slug_redirects r WHERE r.org_id = $1 AND r.slug = $2
) AS unavailable;

-- name: ProjectSlugUnavailableForOtherProject :one
SELECT EXISTS (
    SELECT 1 FROM projects p WHERE p.org_id = $1 AND p.slug = $2 AND p.project_id <> $3
) OR EXISTS (
    SELECT 1 FROM project_slug_redirects r WHERE r.org_id = $1 AND r.slug = $2
) AS unavailable;

-- name: ListProjects :many
SELECT project_id, org_id, slug, display_name, description, state, version, created_by, updated_by, created_at, updated_at, archived_at
FROM projects
WHERE org_id = $1
ORDER BY created_at DESC, project_id DESC
LIMIT $2;

-- name: ListProjectsByState :many
SELECT project_id, org_id, slug, display_name, description, state, version, created_by, updated_by, created_at, updated_at, archived_at
FROM projects
WHERE org_id = $1 AND state = $2
ORDER BY created_at DESC, project_id DESC
LIMIT $3;

-- name: ListProjectsAfterCursor :many
SELECT project_id, org_id, slug, display_name, description, state, version, created_by, updated_by, created_at, updated_at, archived_at
FROM projects
WHERE org_id = $1 AND (created_at, project_id) < (sqlc.arg(created_before)::timestamptz, sqlc.arg(id_before)::uuid)
ORDER BY created_at DESC, project_id DESC
LIMIT sqlc.arg(limit_count);

-- name: ListProjectsByStateAfterCursor :many
SELECT project_id, org_id, slug, display_name, description, state, version, created_by, updated_by, created_at, updated_at, archived_at
FROM projects
WHERE org_id = $1 AND state = $2 AND (created_at, project_id) < (sqlc.arg(created_before)::timestamptz, sqlc.arg(id_before)::uuid)
ORDER BY created_at DESC, project_id DESC
LIMIT sqlc.arg(limit_count);

-- name: GetEnvironmentByID :one
SELECT environment_id, project_id, org_id, slug, display_name, kind, state, protection_policy, version, created_by, updated_by, created_at, updated_at, archived_at
FROM project_environments
WHERE org_id = $1 AND project_id = $2 AND environment_id = $3;

-- name: GetEnvironmentBySlug :one
SELECT environment_id, project_id, org_id, slug, display_name, kind, state, protection_policy, version, created_by, updated_by, created_at, updated_at, archived_at
FROM project_environments
WHERE org_id = $1 AND project_id = $2 AND slug = $3;

-- name: ListEnvironments :many
SELECT environment_id, project_id, org_id, slug, display_name, kind, state, protection_policy, version, created_by, updated_by, created_at, updated_at, archived_at
FROM project_environments
WHERE org_id = $1 AND project_id = $2
ORDER BY kind, slug;

-- name: GetProjectIdempotencyRecord :one
SELECT result_kind, request_hash, result_payload
FROM project_idempotency_records
WHERE org_id = $1 AND operation = $2 AND key_hash = $3;

-- name: ListEvents :many
SELECT event_id, org_id, project_id, environment_id, event_type, actor_id, payload, trace_id, traceparent, created_at
FROM project_events
WHERE org_id = $1
ORDER BY created_at DESC, event_id DESC
LIMIT $2;

-- name: ListEventsAfterCursor :many
SELECT event_id, org_id, project_id, environment_id, event_type, actor_id, payload, trace_id, traceparent, created_at
FROM project_events
WHERE org_id = $1 AND (created_at, event_id) < (sqlc.arg(created_before)::timestamptz, sqlc.arg(id_before)::uuid)
ORDER BY created_at DESC, event_id DESC
LIMIT sqlc.arg(limit_count);
