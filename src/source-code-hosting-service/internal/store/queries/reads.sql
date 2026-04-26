-- name: ListRepositories :many
SELECT
    r.repo_id,
    r.org_id,
    r.project_id,
    r.created_by,
    r.name,
    r.slug,
    r.description,
    r.default_branch,
    r.visibility,
    r.state,
    r.version,
    r.last_pushed_at,
    r.created_at,
    r.updated_at,
    b.backend_id,
    b.repo_id AS backend_source_repo_id,
    b.backend,
    b.backend_owner,
    b.backend_repo,
    b.backend_repo_id,
    b.state AS backend_state,
    b.created_at AS backend_created_at,
    b.updated_at AS backend_updated_at
FROM source_repositories r
JOIN source_repository_backends b ON b.repo_id = r.repo_id AND b.backend = $2 AND b.state = 'active'
WHERE r.org_id = $1 AND r.state = 'active'
ORDER BY r.updated_at DESC, r.repo_id DESC;

-- name: ListRepositoriesByProject :many
SELECT
    r.repo_id,
    r.org_id,
    r.project_id,
    r.created_by,
    r.name,
    r.slug,
    r.description,
    r.default_branch,
    r.visibility,
    r.state,
    r.version,
    r.last_pushed_at,
    r.created_at,
    r.updated_at,
    b.backend_id,
    b.repo_id AS backend_source_repo_id,
    b.backend,
    b.backend_owner,
    b.backend_repo,
    b.backend_repo_id,
    b.state AS backend_state,
    b.created_at AS backend_created_at,
    b.updated_at AS backend_updated_at
FROM source_repositories r
JOIN source_repository_backends b ON b.repo_id = r.repo_id AND b.backend = $3 AND b.state = 'active'
WHERE r.org_id = $1 AND r.project_id = $2 AND r.state = 'active'
ORDER BY r.updated_at DESC, r.repo_id DESC;

-- name: GetRepository :one
SELECT
    r.repo_id,
    r.org_id,
    r.project_id,
    r.created_by,
    r.name,
    r.slug,
    r.description,
    r.default_branch,
    r.visibility,
    r.state,
    r.version,
    r.last_pushed_at,
    r.created_at,
    r.updated_at,
    b.backend_id,
    b.repo_id AS backend_source_repo_id,
    b.backend,
    b.backend_owner,
    b.backend_repo,
    b.backend_repo_id,
    b.state AS backend_state,
    b.created_at AS backend_created_at,
    b.updated_at AS backend_updated_at
FROM source_repositories r
JOIN source_repository_backends b ON b.repo_id = r.repo_id AND b.backend = $3 AND b.state = 'active'
WHERE r.org_id = $1 AND r.repo_id = $2 AND r.state = 'active';

-- name: GetRepositoryByProject :one
SELECT
    r.repo_id,
    r.org_id,
    r.project_id,
    r.created_by,
    r.name,
    r.slug,
    r.description,
    r.default_branch,
    r.visibility,
    r.state,
    r.version,
    r.last_pushed_at,
    r.created_at,
    r.updated_at,
    b.backend_id,
    b.repo_id AS backend_source_repo_id,
    b.backend,
    b.backend_owner,
    b.backend_repo,
    b.backend_repo_id,
    b.state AS backend_state,
    b.created_at AS backend_created_at,
    b.updated_at AS backend_updated_at
FROM source_repositories r
JOIN source_repository_backends b ON b.repo_id = r.repo_id AND b.backend = $3 AND b.state = 'active'
WHERE r.org_id = $1 AND r.project_id = $2 AND r.state = 'active';

-- name: FindRepositoryByBackend :one
SELECT
    r.repo_id,
    r.org_id,
    r.project_id,
    r.created_by,
    r.name,
    r.slug,
    r.description,
    r.default_branch,
    r.visibility,
    r.state,
    r.version,
    r.last_pushed_at,
    r.created_at,
    r.updated_at,
    b.backend_id,
    b.repo_id AS backend_source_repo_id,
    b.backend,
    b.backend_owner,
    b.backend_repo,
    b.backend_repo_id,
    b.state AS backend_state,
    b.created_at AS backend_created_at,
    b.updated_at AS backend_updated_at
FROM source_repositories r
JOIN source_repository_backends b ON b.repo_id = r.repo_id
WHERE b.backend = $1
  AND b.state = 'active'
  AND r.state = 'active'
  AND (
    (sqlc.arg(backend_repo_id)::text <> '' AND b.backend_repo_id = sqlc.arg(backend_repo_id))
    OR (sqlc.arg(backend_repo_id)::text = '' AND b.backend_owner = sqlc.arg(backend_owner) AND b.backend_repo = sqlc.arg(backend_repo))
  )
LIMIT 1;

-- name: ListRefs :many
SELECT ref_name, commit_sha
FROM source_ref_heads
WHERE org_id = $1 AND repo_id = $2
ORDER BY is_default DESC, ref_name;

-- name: GetWorkflowRunByIdempotencyKey :one
SELECT workflow_run_id, org_id, project_id, repo_id, actor_id, idempotency_key, backend, workflow_path, ref,
    inputs_json, state, backend_dispatch_id, failure_reason, trace_id, dispatched_at, created_at, updated_at
FROM source_workflow_runs
WHERE org_id = $1 AND idempotency_key = $2;

-- name: GetWorkflowRun :one
SELECT workflow_run_id, org_id, project_id, repo_id, actor_id, idempotency_key, backend, workflow_path, ref,
    inputs_json, state, backend_dispatch_id, failure_reason, trace_id, dispatched_at, created_at, updated_at
FROM source_workflow_runs
WHERE org_id = $1 AND workflow_run_id = $2;

-- name: ListWorkflowRuns :many
SELECT workflow_run_id, org_id, project_id, repo_id, actor_id, idempotency_key, backend, workflow_path, ref,
    inputs_json, state, backend_dispatch_id, failure_reason, trace_id, dispatched_at, created_at, updated_at
FROM source_workflow_runs
WHERE org_id = $1 AND repo_id = $2
ORDER BY created_at DESC, workflow_run_id DESC
LIMIT 100;
