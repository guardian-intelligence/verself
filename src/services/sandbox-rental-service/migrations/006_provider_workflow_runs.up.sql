CREATE TABLE IF NOT EXISTS provider_workflow_runs (
    provider               TEXT        NOT NULL DEFAULT 'github' CHECK (provider <> ''),
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
    workflow_path          TEXT        NOT NULL DEFAULT '',
    commit_count           BIGINT,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, provider_installation_id, provider_repository_id, provider_run_id, provider_run_attempt)
);

CREATE INDEX IF NOT EXISTS idx_provider_workflow_runs_run
    ON provider_workflow_runs (provider, provider_repository_id, provider_run_id, provider_run_attempt);

DO $$
BEGIN
    IF to_regclass('github_workflow_invocations') IS NOT NULL THEN
        EXECUTE $migrate$
            INSERT INTO provider_workflow_runs (
                provider, provider_installation_id, provider_repository_id,
                provider_run_id, provider_run_attempt, repository_full_name,
                event_name, head_sha, head_branch, head_repository_full_name,
                base_sha, base_branch, pull_request_number, workflow_path,
                commit_count, updated_at, created_at
            )
            SELECT
                'github', provider_installation_id, provider_repository_id,
                provider_run_id, provider_run_attempt, repository_full_name,
                event_name, head_sha, head_branch, head_repository_full_name,
                base_sha, base_branch, pull_request_number, workflow_path,
                commit_count, updated_at, created_at
            FROM github_workflow_invocations
            ON CONFLICT (
                provider, provider_installation_id, provider_repository_id,
                provider_run_id, provider_run_attempt
            ) DO UPDATE SET
                repository_full_name = COALESCE(NULLIF(EXCLUDED.repository_full_name, ''), provider_workflow_runs.repository_full_name),
                event_name = COALESCE(NULLIF(EXCLUDED.event_name, ''), provider_workflow_runs.event_name),
                head_sha = COALESCE(NULLIF(EXCLUDED.head_sha, ''), provider_workflow_runs.head_sha),
                head_branch = COALESCE(NULLIF(EXCLUDED.head_branch, ''), provider_workflow_runs.head_branch),
                head_repository_full_name = COALESCE(NULLIF(EXCLUDED.head_repository_full_name, ''), provider_workflow_runs.head_repository_full_name),
                base_sha = COALESCE(NULLIF(EXCLUDED.base_sha, ''), provider_workflow_runs.base_sha),
                base_branch = COALESCE(NULLIF(EXCLUDED.base_branch, ''), provider_workflow_runs.base_branch),
                pull_request_number = CASE WHEN EXCLUDED.pull_request_number <> 0 THEN EXCLUDED.pull_request_number ELSE provider_workflow_runs.pull_request_number END,
                workflow_path = COALESCE(NULLIF(EXCLUDED.workflow_path, ''), provider_workflow_runs.workflow_path),
                commit_count = COALESCE(EXCLUDED.commit_count, provider_workflow_runs.commit_count),
                updated_at = EXCLUDED.updated_at
        $migrate$;
        EXECUTE 'DROP TABLE github_workflow_invocations';
    END IF;
END $$;
