CREATE TABLE IF NOT EXISTS github_installations (
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

CREATE TABLE IF NOT EXISTS github_repositories (
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

CREATE INDEX IF NOT EXISTS idx_github_repositories_installation
    ON github_repositories (provider_installation_id, state, updated_at DESC);

CREATE TABLE IF NOT EXISTS github_job_shapes (
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

CREATE INDEX IF NOT EXISTS idx_github_job_shapes_repository
    ON github_job_shapes (provider_repository_id, runner_class, updated_at DESC);

CREATE TABLE IF NOT EXISTS github_provider_demands (
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

CREATE INDEX IF NOT EXISTS idx_github_provider_demands_state
    ON github_provider_demands (state, updated_at);

CREATE TABLE IF NOT EXISTS github_provider_outbox (
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

CREATE INDEX IF NOT EXISTS idx_github_provider_outbox_ready
    ON github_provider_outbox (next_attempt_at NULLS FIRST, created_at)
    WHERE state IN ('pending', 'retryable');

ALTER TABLE github_runner_registrations
    ADD COLUMN IF NOT EXISTS demand_id UUID,
    ADD COLUMN IF NOT EXISTS failure_reason TEXT NOT NULL DEFAULT '';

ALTER TABLE github_provider_demands
    ADD COLUMN IF NOT EXISTS trust_class TEXT NOT NULL DEFAULT '';

ALTER TABLE github_terminal_job_evidence
    ADD COLUMN IF NOT EXISTS provider_installation_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS provider_repository_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS sandbox_allocation_id UUID,
    ADD COLUMN IF NOT EXISTS sandbox_execution_id UUID,
    ADD COLUMN IF NOT EXISTS sandbox_attempt_id UUID,
    ADD COLUMN IF NOT EXISTS runner_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS runner_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS job_shape_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS trust_class TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS github_golden_snapshot_barriers (
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
