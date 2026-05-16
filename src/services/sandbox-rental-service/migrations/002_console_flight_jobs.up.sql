-- console_flight_jobs is the read-model behind the web console's CI
-- "flight tracker". It is a pure projection of GitHub job facts
-- (runner_jobs grain) denormalized with run-level identity
-- (github_workflow_invocations) and the owning Verself org
-- (runner_provider_repositories). The web app syncs the active subset over
-- Electric and never writes back: writes flow only through the workflow_job
-- webhook transaction in sandbox-rental-service.
--
-- Grain: one row per GitHub workflow job (provider, provider_job_id), matching
-- runner_jobs. The widget groups rows by provider_run_id into one card per
-- in-flight workflow run. predicted_baseline_ms is the p50 of the same
-- repo/workflow/job over the last 90d of succeeded runs, frozen at the job's
-- first sighting (NULL = no history / cold start).
CREATE TABLE console_flight_jobs (
    provider               TEXT        NOT NULL DEFAULT 'github',
    provider_job_id        BIGINT      NOT NULL,
    org_id                 TEXT        NOT NULL CHECK (length(btrim(org_id)) > 0),
    provider_run_id        BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt   BIGINT      NOT NULL DEFAULT 0,
    repository_full_name   TEXT        NOT NULL DEFAULT '',
    workflow_name          TEXT        NOT NULL DEFAULT '',
    job_name               TEXT        NOT NULL DEFAULT '',
    head_branch            TEXT        NOT NULL DEFAULT '',
    head_sha               TEXT        NOT NULL DEFAULT '',
    pr_number              BIGINT      NOT NULL DEFAULT 0,
    base_branch            TEXT        NOT NULL DEFAULT '',
    actor_login            TEXT        NOT NULL DEFAULT '',
    commit_count           BIGINT,
    status                 TEXT        NOT NULL,
    predicted_baseline_ms  BIGINT,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at             TIMESTAMPTZ,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, provider_job_id)
);

CREATE INDEX idx_console_flight_jobs_org_status
    ON console_flight_jobs (org_id, status);

-- Run-level PR commit count. Populated from the GitHub PR API on the
-- invocation refresh path; NULL for non-PR (push) runs and until first fetch.
ALTER TABLE github_workflow_invocations
    ADD COLUMN commit_count BIGINT;
