ALTER TABLE runner_jobs
    ADD COLUMN IF NOT EXISTS provider_run_attempt BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS github_workflow_invocations (
    provider_installation_id BIGINT    NOT NULL DEFAULT 0,
    provider_repository_id BIGINT      NOT NULL,
    provider_run_id        BIGINT      NOT NULL,
    provider_run_attempt   BIGINT      NOT NULL DEFAULT 0,
    repository_full_name   TEXT        NOT NULL,
    event_name             TEXT        NOT NULL DEFAULT '',
    head_sha               TEXT        NOT NULL DEFAULT '',
    head_branch            TEXT        NOT NULL DEFAULT '',
    head_repository_full_name TEXT     NOT NULL DEFAULT '',
    base_sha               TEXT        NOT NULL DEFAULT '',
    base_branch            TEXT        NOT NULL DEFAULT '',
    pull_request_number    BIGINT      NOT NULL DEFAULT 0,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider_installation_id, provider_repository_id, provider_run_id, provider_run_attempt)
);

CREATE INDEX IF NOT EXISTS idx_github_workflow_invocations_run
    ON github_workflow_invocations (provider_repository_id, provider_run_id, provider_run_attempt);
