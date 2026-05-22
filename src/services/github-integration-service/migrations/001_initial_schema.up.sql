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

CREATE TABLE github_installations (
    provider_installation_id BIGINT      PRIMARY KEY CHECK (provider_installation_id > 0),
    org_id                   TEXT        NOT NULL DEFAULT '',
    account_id               BIGINT      NOT NULL DEFAULT 0,
    account_login            TEXT        NOT NULL DEFAULT '',
    account_type             TEXT        NOT NULL DEFAULT '',
    app_slug                 TEXT        NOT NULL DEFAULT '',
    repository_selection     TEXT        NOT NULL DEFAULT '',
    permissions_json         JSONB       NOT NULL DEFAULT '{}'::jsonb,
    state                    TEXT        NOT NULL CHECK (state <> ''),
    last_event_delivery_id   TEXT        NOT NULL DEFAULT '',
    observed_from_api_at     TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE github_repositories (
    provider_repository_id   BIGINT      PRIMARY KEY CHECK (provider_repository_id > 0),
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    org_id                   TEXT        NOT NULL DEFAULT '',
    owner_login              TEXT        NOT NULL DEFAULT '',
    repository_name          TEXT        NOT NULL DEFAULT '',
    repository_full_name     TEXT        NOT NULL CHECK (repository_full_name <> ''),
    default_branch           TEXT        NOT NULL DEFAULT '',
    private                  BOOLEAN     NOT NULL DEFAULT false,
    state                    TEXT        NOT NULL CHECK (state <> ''),
    last_event_delivery_id   TEXT        NOT NULL DEFAULT '',
    observed_from_api_at     TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_repositories_installation
    ON github_repositories (provider_installation_id, state, updated_at DESC);

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

CREATE TABLE github_job_shapes (
    job_shape_id             TEXT        PRIMARY KEY CHECK (job_shape_id <> ''),
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id   BIGINT      NOT NULL DEFAULT 0,
    repository_full_name     TEXT        NOT NULL DEFAULT '',
    workflow_path            TEXT        NOT NULL DEFAULT '',
    workflow_name            TEXT        NOT NULL DEFAULT '',
    job_name                 TEXT        NOT NULL DEFAULT '',
    matrix_key               TEXT        NOT NULL DEFAULT '',
    runner_class             TEXT        NOT NULL DEFAULT '',
    runner_labels_json       JSONB       NOT NULL DEFAULT '[]'::jsonb,
    cache_manifest_sha256    TEXT        NOT NULL DEFAULT '',
    trust_class              TEXT        NOT NULL DEFAULT '',
    canonical_json           JSONB       NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_job_shapes_repository
    ON github_job_shapes (provider_repository_id, runner_class, updated_at DESC);

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

CREATE TABLE github_provider_demands (
    demand_id                UUID        PRIMARY KEY,
    provider_job_id          BIGINT      NOT NULL UNIQUE REFERENCES github_workflow_jobs(provider_job_id) ON DELETE CASCADE,
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id   BIGINT      NOT NULL DEFAULT 0,
    repository_full_name     TEXT        NOT NULL DEFAULT '',
    provider_run_id          BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt     BIGINT      NOT NULL DEFAULT 0,
    job_shape_id             TEXT        NOT NULL DEFAULT '',
    trust_class              TEXT        NOT NULL DEFAULT '',
    runner_class             TEXT        NOT NULL DEFAULT '',
    runner_name              TEXT        NOT NULL CHECK (runner_name <> ''),
    runner_id                BIGINT      NOT NULL DEFAULT 0,
    state                    TEXT        NOT NULL CHECK (state <> ''),
    failure_reason           TEXT        NOT NULL DEFAULT '',
    jit_config_sha256        TEXT        NOT NULL DEFAULT '',
    sandbox_allocation_id    UUID,
    sandbox_execution_id     UUID,
    sandbox_attempt_id       UUID,
    last_delivery_id         TEXT        NOT NULL DEFAULT '',
    claimed_at               TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_provider_demands_state
    ON github_provider_demands (state, updated_at);

CREATE TABLE github_provider_outbox (
    outbox_id                UUID        PRIMARY KEY,
    command_kind             TEXT        NOT NULL CHECK (command_kind <> ''),
    provider_job_id          BIGINT      NOT NULL DEFAULT 0,
    provider_run_id          BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt     BIGINT      NOT NULL DEFAULT 0,
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id   BIGINT      NOT NULL DEFAULT 0,
    sandbox_execution_id     UUID,
    sandbox_attempt_id       UUID,
    state                    TEXT        NOT NULL CHECK (state <> ''),
    command_sha256           TEXT        NOT NULL CHECK (command_sha256 <> ''),
    payload_json             JSONB       NOT NULL,
    attempt_count            INTEGER     NOT NULL DEFAULT 0,
    next_attempt_at          TIMESTAMPTZ,
    processed_at             TIMESTAMPTZ,
    failure_reason           TEXT        NOT NULL DEFAULT '',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (command_kind, command_sha256)
);

CREATE INDEX idx_github_provider_outbox_ready
    ON github_provider_outbox (next_attempt_at NULLS FIRST, created_at)
    WHERE state IN ('pending', 'retryable');

CREATE TABLE github_runner_registrations (
    provider_job_id          BIGINT      PRIMARY KEY REFERENCES github_workflow_jobs(provider_job_id) ON DELETE CASCADE,
    demand_id                UUID        REFERENCES github_provider_demands(demand_id) ON DELETE SET NULL,
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
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id   BIGINT      NOT NULL DEFAULT 0,
    provider_run_id          BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt     BIGINT      NOT NULL DEFAULT 0,
    sandbox_allocation_id    UUID,
    sandbox_execution_id     UUID,
    sandbox_attempt_id       UUID,
    runner_id                BIGINT      NOT NULL DEFAULT 0,
    runner_name              TEXT        NOT NULL DEFAULT '',
    job_shape_id             TEXT        NOT NULL DEFAULT '',
    trust_class              TEXT        NOT NULL DEFAULT '',
    status                   TEXT        NOT NULL DEFAULT '',
    conclusion               TEXT        NOT NULL DEFAULT '',
    source                   TEXT        NOT NULL CHECK (source <> ''),
    delivery_id              TEXT        NOT NULL DEFAULT '',
    observed_at              TIMESTAMPTZ NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE github_golden_snapshot_barriers (
    barrier_id               UUID        PRIMARY KEY,
    terminal_evidence_id     UUID        NOT NULL REFERENCES github_terminal_job_evidence(terminal_evidence_id) ON DELETE CASCADE,
    provider_job_id          BIGINT      NOT NULL UNIQUE,
    provider_run_id          BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt     BIGINT      NOT NULL DEFAULT 0,
    sandbox_execution_id     UUID,
    sandbox_attempt_id       UUID,
    job_shape_id             TEXT        NOT NULL DEFAULT '',
    trust_class              TEXT        NOT NULL DEFAULT '',
    state                    TEXT        NOT NULL CHECK (state <> ''),
    failure_reason           TEXT        NOT NULL DEFAULT '',
    requested_at             TIMESTAMPTZ NOT NULL,
    completed_at             TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);
