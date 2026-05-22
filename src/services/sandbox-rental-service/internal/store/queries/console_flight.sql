-- name: GetRunnerProviderRepositoryOrgID :one
SELECT org_id
FROM runner_provider_repositories
WHERE provider = 'github'
  AND provider_repository_id = sqlc.arg(provider_repository_id)
  AND active;

-- name: UpsertConsoleFlightJob :exec
-- Job-grained projection row. Job facts come from the webhook; pr_number /
-- base_branch / commit_count are denormalized from the run-level invocation
-- (LEFT JOIN so a job with no invocation row yet still projects). created_at
-- is left at its first-insert default so the elapsed clock anchors on first
-- sighting; ON CONFLICT preserves already-known run identity.
INSERT INTO console_flight_jobs (
    provider, provider_job_id, org_id, provider_run_id, provider_run_attempt,
    repository_full_name, workflow_name, job_name, head_branch, head_sha,
    pr_number, base_branch, actor_login, commit_count, status,
    started_at, updated_at
)
SELECT
    'github',
    sqlc.arg(provider_job_id),
    sqlc.arg(org_id),
    sqlc.arg(provider_run_id),
    sqlc.arg(provider_run_attempt),
    sqlc.arg(repository_full_name),
    sqlc.arg(workflow_name),
    sqlc.arg(job_name),
    sqlc.arg(head_branch),
    sqlc.arg(head_sha),
    COALESCE(inv.pull_request_number, 0),
    COALESCE(inv.base_branch, ''),
    sqlc.arg(actor_login),
    inv.commit_count,
    sqlc.arg(status),
    sqlc.narg(started_at),
    sqlc.arg(updated_at)
FROM (SELECT 1) AS _seed
LEFT JOIN github_workflow_invocations inv
    ON inv.provider_installation_id = sqlc.arg(provider_installation_id)
   AND inv.provider_repository_id   = sqlc.arg(provider_repository_id)
   AND inv.provider_run_id          = sqlc.arg(provider_run_id)
   AND inv.provider_run_attempt     = sqlc.arg(provider_run_attempt)
ON CONFLICT (provider, provider_job_id) DO UPDATE SET
    status        = EXCLUDED.status,
    started_at    = COALESCE(EXCLUDED.started_at, console_flight_jobs.started_at),
    workflow_name = COALESCE(NULLIF(EXCLUDED.workflow_name, ''), console_flight_jobs.workflow_name),
    job_name      = COALESCE(NULLIF(EXCLUDED.job_name, ''), console_flight_jobs.job_name),
    head_branch   = COALESCE(NULLIF(EXCLUDED.head_branch, ''), console_flight_jobs.head_branch),
    head_sha      = COALESCE(NULLIF(EXCLUDED.head_sha, ''), console_flight_jobs.head_sha),
    actor_login   = COALESCE(NULLIF(EXCLUDED.actor_login, ''), console_flight_jobs.actor_login),
    pr_number     = CASE WHEN EXCLUDED.pr_number <> 0 THEN EXCLUDED.pr_number ELSE console_flight_jobs.pr_number END,
    base_branch   = COALESCE(NULLIF(EXCLUDED.base_branch, ''), console_flight_jobs.base_branch),
    commit_count  = COALESCE(EXCLUDED.commit_count, console_flight_jobs.commit_count),
    updated_at    = EXCLUDED.updated_at;

-- name: SetConsoleFlightBaseline :exec
-- Frozen-at-departure: only the first observed p50 is recorded; later webhooks
-- never move the badge threshold mid-flight.
UPDATE console_flight_jobs
SET predicted_baseline_ms = sqlc.arg(predicted_baseline_ms)::bigint,
    updated_at = sqlc.arg(updated_at)
WHERE provider = 'github'
  AND provider_job_id = sqlc.arg(provider_job_id)
  AND predicted_baseline_ms IS NULL;
