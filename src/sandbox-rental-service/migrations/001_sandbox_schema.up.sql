-- Sandbox rental service execution control plane schema.
-- Database: sandbox_rental (one database per service).

DROP TABLE IF EXISTS execution_logs;
DROP TABLE IF EXISTS execution_billing_windows;
DROP TABLE IF EXISTS execution_attempts;
DROP TABLE IF EXISTS executions;
DROP TABLE IF EXISTS job_logs;
DROP TABLE IF EXISTS jobs;
DROP SEQUENCE IF EXISTS execution_billing_job_id_seq;
DROP SEQUENCE IF EXISTS job_billing_id_seq;

CREATE SEQUENCE execution_billing_job_id_seq AS BIGINT;

CREATE TABLE executions (
    execution_id       UUID        PRIMARY KEY,
    org_id             BIGINT      NOT NULL,
    actor_id           TEXT        NOT NULL,
    kind               TEXT        NOT NULL,
    provider           TEXT        NOT NULL DEFAULT '',
    product_id         TEXT        NOT NULL DEFAULT 'sandbox',
    status             TEXT        NOT NULL,
    idempotency_key    TEXT,
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
    chunk            BYTEA       NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (attempt_id, seq)
);

CREATE INDEX idx_execution_logs_attempt_id_seq ON execution_logs (attempt_id, seq);
