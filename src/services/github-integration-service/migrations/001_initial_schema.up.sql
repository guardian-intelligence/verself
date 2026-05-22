CREATE TABLE github_webhook_deliveries (
    delivery_id              TEXT        PRIMARY KEY CHECK (delivery_id <> ''),
    event_name               TEXT        NOT NULL CHECK (event_name <> ''),
    action                   TEXT        NOT NULL DEFAULT '',
    state                    TEXT        NOT NULL CHECK (state <> ''),
    failure_reason           TEXT        NOT NULL DEFAULT '',
    payload_sha256           TEXT        NOT NULL CHECK (payload_sha256 <> ''),
    payload_json             JSONB       NOT NULL,
    attempt_count            INTEGER     NOT NULL DEFAULT 0,
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id   BIGINT      NOT NULL DEFAULT 0,
    repository_full_name     TEXT        NOT NULL DEFAULT '',
    provider_run_id          BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt     BIGINT      NOT NULL DEFAULT 0,
    provider_job_id          BIGINT      NOT NULL DEFAULT 0,
    received_at              TIMESTAMPTZ NOT NULL,
    verified_at              TIMESTAMPTZ NOT NULL,
    next_attempt_at          TIMESTAMPTZ,
    processing_started_at    TIMESTAMPTZ,
    processed_at             TIMESTAMPTZ,
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_webhook_deliveries_pending
    ON github_webhook_deliveries (next_attempt_at NULLS FIRST, received_at)
    WHERE state IN ('verified', 'retryable');

CREATE TABLE github_workflow_jobs (
    provider_job_id          BIGINT      PRIMARY KEY,
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id   BIGINT      NOT NULL DEFAULT 0,
    repository_full_name     TEXT        NOT NULL DEFAULT '',
    provider_run_id          BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt     BIGINT      NOT NULL DEFAULT 0,
    job_name                 TEXT        NOT NULL DEFAULT '',
    head_sha                 TEXT        NOT NULL DEFAULT '',
    head_branch              TEXT        NOT NULL DEFAULT '',
    workflow_name            TEXT        NOT NULL DEFAULT '',
    status                   TEXT        NOT NULL DEFAULT '',
    conclusion               TEXT        NOT NULL DEFAULT '',
    labels_json              JSONB       NOT NULL DEFAULT '[]'::jsonb,
    runner_id                BIGINT      NOT NULL DEFAULT 0,
    runner_name              TEXT        NOT NULL DEFAULT '',
    started_at               TIMESTAMPTZ,
    completed_at             TIMESTAMPTZ,
    last_delivery_id         TEXT        NOT NULL DEFAULT '',
    observed_from_api_at     TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_workflow_jobs_run
    ON github_workflow_jobs (provider_repository_id, provider_run_id, provider_run_attempt, provider_job_id);

CREATE TABLE github_workflow_runs (
    provider_installation_id     BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id       BIGINT      NOT NULL,
    provider_run_id              BIGINT      NOT NULL,
    provider_run_attempt         BIGINT      NOT NULL DEFAULT 0,
    repository_full_name         TEXT        NOT NULL DEFAULT '',
    event_name                   TEXT        NOT NULL DEFAULT '',
    head_sha                     TEXT        NOT NULL DEFAULT '',
    head_branch                  TEXT        NOT NULL DEFAULT '',
    head_repository_full_name    TEXT        NOT NULL DEFAULT '',
    base_sha                     TEXT        NOT NULL DEFAULT '',
    base_branch                  TEXT        NOT NULL DEFAULT '',
    workflow_path                TEXT        NOT NULL DEFAULT '',
    pull_request_number          BIGINT      NOT NULL DEFAULT 0,
    commit_count                 BIGINT      NOT NULL DEFAULT 0,
    last_delivery_id             TEXT        NOT NULL DEFAULT '',
    observed_from_api_at         TIMESTAMPTZ,
    created_at                   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider_installation_id, provider_repository_id, provider_run_id, provider_run_attempt)
);

CREATE TABLE github_runner_registrations (
    provider_job_id          BIGINT      PRIMARY KEY REFERENCES github_workflow_jobs(provider_job_id) ON DELETE CASCADE,
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id   BIGINT      NOT NULL DEFAULT 0,
    runner_id                BIGINT      NOT NULL DEFAULT 0,
    runner_name              TEXT        NOT NULL CHECK (runner_name <> ''),
    runner_class             TEXT        NOT NULL DEFAULT '',
    jit_config_sha256        TEXT        NOT NULL DEFAULT '',
    sandbox_allocation_id    UUID,
    sandbox_execution_id     UUID,
    sandbox_attempt_id       UUID,
    state                    TEXT        NOT NULL CHECK (state <> ''),
    failure_reason           TEXT        NOT NULL DEFAULT '',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_github_runner_registrations_runner_name
    ON github_runner_registrations (runner_name);

CREATE TABLE github_terminal_job_evidence (
    terminal_evidence_id     UUID        PRIMARY KEY,
    provider_job_id          BIGINT      NOT NULL UNIQUE REFERENCES github_workflow_jobs(provider_job_id) ON DELETE CASCADE,
    provider_run_id          BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt     BIGINT      NOT NULL DEFAULT 0,
    status                   TEXT        NOT NULL DEFAULT '',
    conclusion               TEXT        NOT NULL DEFAULT '',
    source                   TEXT        NOT NULL CHECK (source <> ''),
    delivery_id              TEXT        NOT NULL DEFAULT '',
    observed_at              TIMESTAMPTZ NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);
