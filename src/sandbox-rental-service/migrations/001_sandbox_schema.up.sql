-- Sandbox rental service control-plane schema.
-- Database: sandbox_rental (one database per service).

DROP TABLE IF EXISTS execution_logs;
DROP TABLE IF EXISTS execution_billing_windows;
DROP TABLE IF EXISTS execution_attempts;
DROP TABLE IF EXISTS executions;
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS webhook_endpoint_secrets;
DROP TABLE IF EXISTS webhook_endpoints;
DROP TABLE IF EXISTS repos CASCADE;
DROP TABLE IF EXISTS git_integrations;
DROP TABLE IF EXISTS job_logs;
DROP TABLE IF EXISTS jobs;
DROP SEQUENCE IF EXISTS execution_billing_job_id_seq;
DROP SEQUENCE IF EXISTS job_billing_id_seq;

CREATE SEQUENCE execution_billing_job_id_seq AS BIGINT;

CREATE TABLE git_integrations (
    integration_id UUID        PRIMARY KEY,
    org_id         BIGINT      NOT NULL CHECK (org_id > 0),
    provider       TEXT        NOT NULL CHECK (provider IN ('forgejo', 'github', 'gitlab')),
    provider_host  TEXT        NOT NULL CHECK (provider_host <> ''),
    mode           TEXT        NOT NULL CHECK (mode IN ('manual_webhook')),
    label          TEXT        NOT NULL DEFAULT '',
    active         BOOLEAN     NOT NULL DEFAULT true,
    created_by     TEXT        NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_git_integrations_org_provider_host_mode_active
    ON git_integrations (org_id, provider, provider_host, mode)
    WHERE active;
CREATE UNIQUE INDEX idx_git_integrations_org_label_active
    ON git_integrations (org_id, label)
    WHERE active AND label <> '';

CREATE TABLE repos (
    repo_id                     UUID        PRIMARY KEY,
    org_id                      BIGINT      NOT NULL,
    integration_id              UUID        REFERENCES git_integrations(integration_id) ON DELETE SET NULL,
    provider                    TEXT        NOT NULL,
    provider_host               TEXT        NOT NULL DEFAULT '',
    provider_repo_id            TEXT        NOT NULL,
    owner                       TEXT        NOT NULL,
    name                        TEXT        NOT NULL,
    full_name                   TEXT        NOT NULL,
    clone_url                   TEXT        NOT NULL,
    default_branch              TEXT        NOT NULL DEFAULT 'main',
    state                       TEXT        NOT NULL CHECK (
        state IN (
            'importing',
            'action_required',
            'ready',
            'failed',
            'archived'
        )
    ),
    compatibility_status        TEXT        NOT NULL DEFAULT '',
    compatibility_summary       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    last_scanned_sha            TEXT        NOT NULL DEFAULT '',
    last_error                  TEXT        NOT NULL DEFAULT '',
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at                 TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_repos_org_provider_repo_id
    ON repos (org_id, provider, provider_host, provider_repo_id);
CREATE UNIQUE INDEX idx_repos_org_provider_full_name
    ON repos (org_id, provider, provider_host, full_name);
CREATE INDEX idx_repos_org_state_updated_at
    ON repos (org_id, state, updated_at DESC);
CREATE INDEX idx_repos_integration
    ON repos (integration_id)
    WHERE integration_id IS NOT NULL;

CREATE TABLE webhook_endpoints (
    endpoint_id      UUID        PRIMARY KEY,
    integration_id   UUID        NOT NULL REFERENCES git_integrations(integration_id) ON DELETE CASCADE,
    org_id           BIGINT      NOT NULL CHECK (org_id > 0),
    provider         TEXT        NOT NULL CHECK (provider IN ('forgejo', 'github', 'gitlab')),
    provider_host    TEXT        NOT NULL CHECK (provider_host <> ''),
    label            TEXT        NOT NULL DEFAULT '',
    active           BOOLEAN     NOT NULL DEFAULT true,
    created_by       TEXT        NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_delivery_at TIMESTAMPTZ,
    delivery_count   BIGINT      NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX idx_webhook_endpoints_org_label_active
    ON webhook_endpoints (org_id, label)
    WHERE active AND label <> '';
CREATE INDEX idx_webhook_endpoints_org
    ON webhook_endpoints (org_id, active, created_at DESC);
CREATE INDEX idx_webhook_endpoints_integration
    ON webhook_endpoints (integration_id);

CREATE TABLE webhook_endpoint_secrets (
    secret_id          UUID        PRIMARY KEY,
    endpoint_id        UUID        NOT NULL REFERENCES webhook_endpoints(endpoint_id) ON DELETE CASCADE,
    secret_ciphertext  TEXT        NOT NULL,
    secret_fingerprint TEXT        NOT NULL,
    active_from        TIMESTAMPTZ NOT NULL DEFAULT now(),
    retiring_at        TIMESTAMPTZ,
    revoked_at         TIMESTAMPTZ,
    created_by         TEXT        NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_webhook_endpoint_secrets_active
    ON webhook_endpoint_secrets (endpoint_id, active_from DESC)
    WHERE revoked_at IS NULL;

CREATE TABLE webhook_deliveries (
    delivery_id          UUID        PRIMARY KEY,
    endpoint_id          UUID        NOT NULL REFERENCES webhook_endpoints(endpoint_id) ON DELETE CASCADE,
    integration_id       UUID        NOT NULL REFERENCES git_integrations(integration_id) ON DELETE CASCADE,
    org_id               BIGINT      NOT NULL CHECK (org_id > 0),
    provider             TEXT        NOT NULL CHECK (provider IN ('forgejo', 'github', 'gitlab')),
    provider_host        TEXT        NOT NULL CHECK (provider_host <> ''),
    provider_delivery_id TEXT        NOT NULL DEFAULT '',
    event_type           TEXT        NOT NULL DEFAULT '',
    state                TEXT        NOT NULL CHECK (state IN ('queued', 'processing', 'processed', 'ignored', 'failed')),
    payload              JSONB       NOT NULL,
    payload_sha256       TEXT        NOT NULL,
    attempt_count        INTEGER     NOT NULL DEFAULT 0,
    last_error           TEXT        NOT NULL DEFAULT '',
    trace_id             TEXT        NOT NULL DEFAULT '',
    received_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at           TIMESTAMPTZ,
    processed_at         TIMESTAMPTZ,
    next_attempt_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_webhook_deliveries_endpoint_provider_delivery
    ON webhook_deliveries (endpoint_id, provider_delivery_id)
    WHERE provider_delivery_id <> '';
CREATE INDEX idx_webhook_deliveries_claim
    ON webhook_deliveries (state, next_attempt_at, received_at)
    WHERE state IN ('queued', 'failed');
CREATE INDEX idx_webhook_deliveries_org_received
    ON webhook_deliveries (org_id, received_at DESC);

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
    repo               TEXT        NOT NULL DEFAULT '',
    repo_url           TEXT        NOT NULL DEFAULT '',
    ref                TEXT        NOT NULL DEFAULT '',
    default_branch     TEXT        NOT NULL DEFAULT '',
    run_command        TEXT        NOT NULL DEFAULT '',
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

CREATE TABLE execution_attempts (
    attempt_id            UUID        PRIMARY KEY,
    execution_id          UUID        NOT NULL REFERENCES executions(execution_id) ON DELETE CASCADE,
    attempt_seq           INTEGER     NOT NULL,
    state                 TEXT        NOT NULL,
    orchestrator_run_id   TEXT        NOT NULL DEFAULT '',
    billing_job_id        BIGINT,
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
    attempt_id          UUID        NOT NULL REFERENCES execution_attempts(attempt_id) ON DELETE CASCADE,
    billing_window_id   TEXT        NOT NULL,
    window_seq          INTEGER     NOT NULL,
    reservation_shape   TEXT        NOT NULL DEFAULT 'time',
    reserved_quantity   INTEGER     NOT NULL,
    actual_quantity     INTEGER,
    pricing_phase       TEXT        NOT NULL DEFAULT '',
    state               TEXT        NOT NULL,
    window_start        TIMESTAMPTZ NOT NULL,
    activated_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    settled_at          TIMESTAMPTZ,
    PRIMARY KEY (attempt_id, window_seq),
    UNIQUE (billing_window_id)
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
