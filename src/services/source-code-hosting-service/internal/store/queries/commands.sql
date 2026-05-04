-- name: Ping :one
SELECT 1::int AS one;

-- name: InsertRepository :exec
INSERT INTO source_repositories (
    repo_id, org_id, project_id, created_by, name, slug, description, default_branch,
    visibility, state, version, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12);

-- name: InsertRepositoryBackend :exec
INSERT INTO source_repository_backends (
    backend_id, repo_id, backend, backend_owner, backend_repo, backend_repo_id, state, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8);

-- name: InsertGitCredential :exec
INSERT INTO source_git_credentials (
    credential_id, org_id, actor_id, label, username, token_prefix,
    scopes, state, expires_at, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10);

-- name: LockActiveGitCredentialForUse :one
SELECT credential_id, org_id, actor_id, username, scopes
FROM source_git_credentials
WHERE credential_id = $1 AND state = 'active' AND expires_at > $2
FOR UPDATE;

-- name: MarkGitCredentialUsed :exec
UPDATE source_git_credentials
SET last_used_at = $2
WHERE credential_id = $1;

-- name: DeleteRefsForRepository :exec
DELETE FROM source_ref_heads
WHERE repo_id = $1;

-- name: InsertRefHead :exec
INSERT INTO source_ref_heads (repo_id, org_id, ref_name, commit_sha, is_default, pushed_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$6);

-- name: MarkRepositoryRefsPushed :exec
UPDATE source_repositories
SET last_pushed_at = $2, updated_at = $2, version = version + 1
WHERE repo_id = $1;

-- name: InsertCheckoutGrant :exec
INSERT INTO source_checkout_grants (
    grant_id, repo_id, org_id, actor_id, ref, path_prefix, token_hash, expires_at, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9);

-- name: LockCheckoutGrantForConsume :one
SELECT
    g.grant_id,
    g.repo_id AS grant_repo_id,
    g.org_id AS grant_org_id,
    g.actor_id AS grant_actor_id,
    g.ref,
    g.path_prefix,
    g.expires_at AS grant_expires_at,
    g.created_at AS grant_created_at,
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
FROM source_checkout_grants g
JOIN source_repositories r ON r.repo_id = g.repo_id
JOIN source_repository_backends b ON b.repo_id = r.repo_id AND b.backend = $4 AND b.state = 'active'
WHERE g.grant_id = $1 AND g.token_hash = $2 AND g.consumed_at IS NULL AND g.expires_at > $3 AND r.state = 'active'
FOR UPDATE OF g;

-- name: ConsumeCheckoutGrant :exec
UPDATE source_checkout_grants
SET consumed_at = $2
WHERE grant_id = $1;

-- name: InsertWorkflowRun :one
INSERT INTO source_workflow_runs (
    workflow_run_id, org_id, project_id, repo_id, actor_id, idempotency_key, backend,
    workflow_path, ref, inputs_json, state, trace_id, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13)
ON CONFLICT (org_id, idempotency_key) DO NOTHING
RETURNING workflow_run_id;

-- name: MarkWorkflowRunDispatched :one
UPDATE source_workflow_runs
SET state = 'dispatched', backend_dispatch_id = $2, dispatched_at = $3, updated_at = $3
WHERE workflow_run_id = $1
RETURNING workflow_run_id, org_id, project_id, repo_id, actor_id, idempotency_key, backend, workflow_path, ref,
    inputs_json, state, backend_dispatch_id, failure_reason, trace_id, dispatched_at, created_at, updated_at;

-- name: MarkWorkflowRunFailed :exec
UPDATE source_workflow_runs
SET state = 'failed', failure_reason = $2, updated_at = $3
WHERE workflow_run_id = $1;

-- name: InsertStorageEvent :exec
INSERT INTO source_storage_events (
    storage_event_id, org_id, repo_id, project_id, backend, storage_object_kind, event_type, byte_count, trace_id, details, measured_at, created_at
) VALUES (
    $1,
    $2,
    sqlc.narg(repo_id),
    (SELECT project_id FROM source_repositories WHERE repo_id = sqlc.narg(repo_id)),
    $3,
    $4,
    $5,
    $6,
    $7,
    $8,
    $9,
    $9
);

-- name: UpsertWebhookDelivery :exec
INSERT INTO source_webhook_deliveries (
    webhook_delivery_id, backend, delivery_id, event_type, signature_valid, result,
    resolved_org_id, resolved_project_id, resolved_repo_id, trace_id, details, created_at
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    sqlc.narg(resolved_org_id),
    sqlc.narg(resolved_project_id),
    sqlc.narg(resolved_repo_id),
    $7,
    $8,
    $9
)
ON CONFLICT (backend, delivery_id) DO UPDATE SET
    event_type = EXCLUDED.event_type,
    signature_valid = EXCLUDED.signature_valid,
    result = EXCLUDED.result,
    resolved_org_id = EXCLUDED.resolved_org_id,
    resolved_project_id = EXCLUDED.resolved_project_id,
    resolved_repo_id = EXCLUDED.resolved_repo_id,
    trace_id = EXCLUDED.trace_id,
    details = EXCLUDED.details;

-- name: InsertSourceEvent :exec
INSERT INTO source_events (event_id, org_id, actor_id, repo_id, project_id, event_type, result, trace_id, details, created_at)
VALUES (
    $1,
    $2,
    $3,
    sqlc.narg(repo_id),
    (SELECT project_id FROM source_repositories WHERE repo_id = sqlc.narg(repo_id)),
    $4,
    $5,
    $6,
    $7,
    $8
);
