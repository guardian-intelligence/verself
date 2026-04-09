-- Sandbox rental service control-plane schema.
-- Database: sandbox_rental (one database per service).

DROP TABLE IF EXISTS execution_logs;
DROP TABLE IF EXISTS execution_billing_windows;
DROP TABLE IF EXISTS execution_attempts;
DROP TABLE IF EXISTS executions;
DROP TABLE IF EXISTS golden_generations CASCADE;
DROP TABLE IF EXISTS repos CASCADE;
DROP TABLE IF EXISTS job_logs;
DROP TABLE IF EXISTS jobs;
DROP SEQUENCE IF EXISTS execution_billing_job_id_seq;
DROP SEQUENCE IF EXISTS job_billing_id_seq;

CREATE SEQUENCE execution_billing_job_id_seq AS BIGINT;

CREATE TABLE repos (
    repo_id                     UUID        PRIMARY KEY,
    org_id                      BIGINT      NOT NULL,
    provider                    TEXT        NOT NULL,
    provider_repo_id            TEXT        NOT NULL,
    owner                       TEXT        NOT NULL,
    name                        TEXT        NOT NULL,
    full_name                   TEXT        NOT NULL,
    clone_url                   TEXT        NOT NULL,
    default_branch              TEXT        NOT NULL DEFAULT 'main',
    runner_profile_slug         TEXT        NOT NULL DEFAULT 'forge-metal',
    state                       TEXT        NOT NULL CHECK (
        state IN (
            'importing',
            'action_required',
            'waiting_for_bootstrap',
            'preparing',
            'ready',
            'degraded',
            'failed',
            'archived'
        )
    ),
    compatibility_status        TEXT        NOT NULL DEFAULT '',
    compatibility_summary       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    last_scanned_sha            TEXT        NOT NULL DEFAULT '',
    active_golden_generation_id UUID,
    last_ready_sha              TEXT        NOT NULL DEFAULT '',
    last_error                  TEXT        NOT NULL DEFAULT '',
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at                 TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_repos_provider_repo_id
    ON repos (provider, provider_repo_id);
CREATE UNIQUE INDEX idx_repos_org_provider_full_name
    ON repos (org_id, provider, full_name);
CREATE INDEX idx_repos_org_state_updated_at
    ON repos (org_id, state, updated_at DESC);

CREATE TABLE executions (
    execution_id       UUID        PRIMARY KEY,
    org_id             BIGINT      NOT NULL,
    actor_id           TEXT        NOT NULL,
    kind               TEXT        NOT NULL,
    provider           TEXT        NOT NULL DEFAULT '',
    product_id         TEXT        NOT NULL DEFAULT 'sandbox',
    status             TEXT        NOT NULL,
    correlation_id     TEXT        NOT NULL DEFAULT '',
    idempotency_key    TEXT,
    repo_id            UUID        REFERENCES repos(repo_id) ON DELETE SET NULL,
    golden_generation_id UUID,
    repo               TEXT        NOT NULL DEFAULT '',
    repo_url           TEXT        NOT NULL DEFAULT '',
    ref                TEXT        NOT NULL DEFAULT '',
    default_branch     TEXT        NOT NULL DEFAULT '',
    run_command        TEXT        NOT NULL DEFAULT '',
    commit_sha         TEXT        NOT NULL DEFAULT '',
    workflow_path      TEXT        NOT NULL DEFAULT '',
    workflow_job_name  TEXT        NOT NULL DEFAULT '',
    provider_run_id    TEXT        NOT NULL DEFAULT '',
    provider_job_id    TEXT        NOT NULL DEFAULT '',
    latest_attempt_id  UUID        NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_executions_org_idempotency_key
    ON executions (org_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';

CREATE INDEX idx_executions_org_updated_at ON executions (org_id, updated_at DESC);
CREATE INDEX idx_executions_status ON executions (status);
CREATE INDEX idx_executions_correlation_id ON executions (correlation_id) WHERE correlation_id <> '';
CREATE INDEX idx_executions_repo_id ON executions (repo_id) WHERE repo_id IS NOT NULL;
CREATE INDEX idx_executions_golden_generation_id ON executions (golden_generation_id) WHERE golden_generation_id IS NOT NULL;

CREATE TABLE execution_attempts (
    attempt_id            UUID        PRIMARY KEY,
    execution_id          UUID        NOT NULL REFERENCES executions(execution_id) ON DELETE CASCADE,
    attempt_seq           INTEGER     NOT NULL,
    state                 TEXT        NOT NULL,
    orchestrator_job_id   TEXT        NOT NULL DEFAULT '',
    billing_job_id        BIGINT,
    runner_name           TEXT        NOT NULL DEFAULT '',
    golden_snapshot       TEXT        NOT NULL DEFAULT '',
    failure_reason        TEXT        NOT NULL DEFAULT '',
    exit_code             INTEGER,
    duration_ms           BIGINT,
    zfs_written           BIGINT,
    stdout_bytes          BIGINT,
    stderr_bytes          BIGINT,
    trace_id              TEXT        NOT NULL DEFAULT '',
    started_at            TIMESTAMPTZ,
    completed_at          TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (execution_id, attempt_seq)
);

CREATE INDEX idx_execution_attempts_execution_id ON execution_attempts (execution_id, attempt_seq DESC);
CREATE INDEX idx_execution_attempts_state ON execution_attempts (state);

CREATE TABLE execution_billing_windows (
    attempt_id       UUID        NOT NULL REFERENCES execution_attempts(attempt_id) ON DELETE CASCADE,
    window_seq       INTEGER     NOT NULL,
    reservation      JSONB       NOT NULL,
    window_seconds   INTEGER     NOT NULL,
    actual_seconds   INTEGER,
    pricing_phase    TEXT        NOT NULL DEFAULT '',
    state            TEXT        NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    settled_at       TIMESTAMPTZ,
    PRIMARY KEY (attempt_id, window_seq)
);

CREATE TABLE execution_logs (
    attempt_id       UUID        NOT NULL REFERENCES execution_attempts(attempt_id) ON DELETE CASCADE,
    seq              INTEGER     NOT NULL,
    stream           TEXT        NOT NULL,
    -- Execution and runner logs are line-oriented UTF-8 text. Keep them as
    -- TEXT so Electric can shape them directly into the browser.
    chunk            TEXT        NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (attempt_id, seq)
);

CREATE INDEX idx_execution_logs_attempt_id_seq ON execution_logs (attempt_id, seq);

CREATE TABLE golden_generations (
    golden_generation_id UUID        PRIMARY KEY,
    repo_id               UUID        NOT NULL REFERENCES repos(repo_id) ON DELETE CASCADE,
    runner_profile_slug   TEXT        NOT NULL DEFAULT 'forge-metal',
    source_ref            TEXT        NOT NULL,
    source_sha            TEXT        NOT NULL,
    state                 TEXT        NOT NULL CHECK (
        state IN ('queued', 'building', 'sanitizing', 'ready', 'failed', 'superseded')
    ),
    trigger_reason        TEXT        NOT NULL DEFAULT '',
    execution_id          UUID        REFERENCES executions(execution_id) ON DELETE SET NULL,
    attempt_id            UUID        REFERENCES execution_attempts(attempt_id) ON DELETE SET NULL,
    orchestrator_job_id   TEXT        NOT NULL DEFAULT '',
    snapshot_ref          TEXT        NOT NULL DEFAULT '',
    activated_at          TIMESTAMPTZ,
    superseded_at         TIMESTAMPTZ,
    failure_reason        TEXT        NOT NULL DEFAULT '',
    failure_detail        TEXT        NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_golden_generations_repo_created_at
    ON golden_generations (repo_id, runner_profile_slug, created_at DESC);
CREATE INDEX idx_golden_generations_execution_id
    ON golden_generations (execution_id)
    WHERE execution_id IS NOT NULL;
CREATE UNIQUE INDEX idx_golden_generations_active
    ON golden_generations (repo_id, runner_profile_slug)
    WHERE activated_at IS NOT NULL AND superseded_at IS NULL;

ALTER TABLE repos
    ADD CONSTRAINT repos_active_golden_generation_id_fkey
    FOREIGN KEY (active_golden_generation_id)
    REFERENCES golden_generations(golden_generation_id)
    ON DELETE SET NULL;

ALTER TABLE executions
    ADD CONSTRAINT executions_golden_generation_id_fkey
    FOREIGN KEY (golden_generation_id)
    REFERENCES golden_generations(golden_generation_id)
    ON DELETE SET NULL;
