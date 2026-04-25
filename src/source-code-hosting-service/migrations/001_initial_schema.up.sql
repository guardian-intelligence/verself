-- Source-code hosting service control-plane schema.
-- Database: source_code_hosting (one database per service).

CREATE TABLE source_provider_installations (
    installation_id          UUID        PRIMARY KEY,
    provider                 TEXT        NOT NULL CHECK (provider <> ''),
    provider_installation_id TEXT        NOT NULL DEFAULT '',
    org_id                   BIGINT      CHECK (org_id > 0),
    base_url                 TEXT        NOT NULL CHECK (base_url LIKE 'http%://%'),
    owner_username           TEXT        NOT NULL CHECK (owner_username <> ''),
    state                    TEXT        NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'disabled')),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_installation_id),
    UNIQUE (provider, base_url, owner_username)
);

CREATE TABLE source_repositories (
    repo_id          UUID        PRIMARY KEY,
    org_id           BIGINT      NOT NULL CHECK (org_id > 0),
    created_by       TEXT        NOT NULL CHECK (created_by <> ''),
    name             TEXT        NOT NULL CHECK (name <> ''),
    slug             TEXT        NOT NULL CHECK (slug <> ''),
    description      TEXT        NOT NULL DEFAULT '',
    default_branch   TEXT        NOT NULL DEFAULT 'main',
    visibility       TEXT        NOT NULL DEFAULT 'private' CHECK (visibility IN ('private')),
    provider         TEXT        NOT NULL CHECK (provider <> ''),
    provider_owner   TEXT        NOT NULL CHECK (provider_owner <> ''),
    provider_repo    TEXT        NOT NULL CHECK (provider_repo <> ''),
    provider_repo_id TEXT        NOT NULL DEFAULT '',
    state            TEXT        NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'deleted')),
    version          BIGINT      NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ,
    UNIQUE (org_id, slug),
    UNIQUE (provider, provider_owner, provider_repo)
);

CREATE INDEX idx_source_repositories_provider_id
    ON source_repositories (provider, provider_repo_id)
    WHERE provider_repo_id <> '';

CREATE INDEX idx_source_repositories_org_updated
    ON source_repositories (org_id, updated_at DESC, repo_id DESC)
    WHERE state = 'active';

CREATE TABLE source_checkout_grants (
    grant_id       UUID        PRIMARY KEY,
    repo_id        UUID        NOT NULL REFERENCES source_repositories(repo_id) ON DELETE CASCADE,
    org_id         BIGINT      NOT NULL CHECK (org_id > 0),
    actor_id       TEXT        NOT NULL CHECK (actor_id <> ''),
    ref            TEXT        NOT NULL CHECK (ref <> ''),
    path_prefix    TEXT        NOT NULL DEFAULT '',
    token_hash     TEXT        NOT NULL UNIQUE CHECK (token_hash <> ''),
    expires_at     TIMESTAMPTZ NOT NULL,
    consumed_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_source_checkout_grants_repo_created
    ON source_checkout_grants (repo_id, created_at DESC);

CREATE TABLE source_external_integrations (
    integration_id       UUID        PRIMARY KEY,
    org_id               BIGINT      NOT NULL CHECK (org_id > 0),
    created_by           TEXT        NOT NULL CHECK (created_by <> ''),
    provider             TEXT        NOT NULL CHECK (provider <> ''),
    external_repo        TEXT        NOT NULL CHECK (external_repo <> ''),
    credential_ref       TEXT        NOT NULL DEFAULT '',
    webhook_secret_hash  TEXT        NOT NULL DEFAULT '',
    state                TEXT        NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'disabled')),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_source_external_integrations_org_provider
    ON source_external_integrations (org_id, provider, updated_at DESC);

CREATE TABLE source_workflow_runs (
    workflow_run_id        UUID        PRIMARY KEY,
    org_id                 BIGINT      NOT NULL CHECK (org_id > 0),
    repo_id                UUID        NOT NULL REFERENCES source_repositories(repo_id) ON DELETE CASCADE,
    actor_id               TEXT        NOT NULL CHECK (actor_id <> ''),
    idempotency_key        TEXT        NOT NULL CHECK (idempotency_key <> ''),
    provider               TEXT        NOT NULL CHECK (provider <> ''),
    workflow_path          TEXT        NOT NULL CHECK (workflow_path <> ''),
    ref                    TEXT        NOT NULL CHECK (ref <> ''),
    inputs_json            JSONB       NOT NULL DEFAULT '{}'::jsonb,
    state                  TEXT        NOT NULL CHECK (state IN ('dispatching', 'dispatched', 'failed')),
    provider_dispatch_id   TEXT        NOT NULL DEFAULT '',
    failure_reason         TEXT        NOT NULL DEFAULT '',
    trace_id               TEXT        NOT NULL DEFAULT '',
    dispatched_at          TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, idempotency_key)
);

CREATE INDEX idx_source_workflow_runs_repo_created
    ON source_workflow_runs (repo_id, created_at DESC, workflow_run_id DESC);

CREATE INDEX idx_source_workflow_runs_trace_id
    ON source_workflow_runs (trace_id)
    WHERE trace_id <> '';

CREATE TABLE source_runner_demands (
    demand_id           UUID        PRIMARY KEY,
    workflow_run_id     UUID        NOT NULL REFERENCES source_workflow_runs(workflow_run_id) ON DELETE CASCADE,
    org_id              BIGINT      NOT NULL CHECK (org_id > 0),
    repo_id             UUID        NOT NULL REFERENCES source_repositories(repo_id) ON DELETE CASCADE,
    provider            TEXT        NOT NULL CHECK (provider <> ''),
    provider_job_id     TEXT        NOT NULL DEFAULT '',
    job_name            TEXT        NOT NULL DEFAULT '',
    runner_class        TEXT        NOT NULL DEFAULT '',
    labels_json         JSONB       NOT NULL DEFAULT '[]'::jsonb,
    state               TEXT        NOT NULL CHECK (state IN ('pending', 'claimed', 'completed', 'canceled')),
    claimed_by          TEXT        NOT NULL DEFAULT '',
    trace_id            TEXT        NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_job_id)
);

CREATE INDEX idx_source_runner_demands_pending
    ON source_runner_demands (state, created_at, demand_id)
    WHERE state = 'pending';

CREATE TABLE source_storage_events (
    storage_event_id      UUID        PRIMARY KEY,
    org_id                BIGINT      NOT NULL CHECK (org_id > 0),
    repo_id               UUID        REFERENCES source_repositories(repo_id) ON DELETE SET NULL,
    provider              TEXT        NOT NULL CHECK (provider <> ''),
    storage_namespace     TEXT        NOT NULL DEFAULT '',
    storage_object_kind   TEXT        NOT NULL CHECK (storage_object_kind <> ''),
    event_type            TEXT        NOT NULL CHECK (event_type <> ''),
    byte_count            BIGINT      NOT NULL DEFAULT 0 CHECK (byte_count >= 0),
    trace_id              TEXT        NOT NULL DEFAULT '',
    details               JSONB       NOT NULL DEFAULT '{}'::jsonb,
    measured_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_source_storage_events_org_measured
    ON source_storage_events (org_id, measured_at DESC, storage_event_id DESC);

CREATE TABLE source_webhook_deliveries (
    webhook_delivery_id UUID        PRIMARY KEY,
    provider            TEXT        NOT NULL CHECK (provider <> ''),
    delivery_id         TEXT        NOT NULL CHECK (delivery_id <> ''),
    event_type          TEXT        NOT NULL CHECK (event_type <> ''),
    signature_valid     BOOLEAN     NOT NULL,
    result              TEXT        NOT NULL CHECK (result IN ('accepted', 'denied', 'unresolved', 'error')),
    resolved_org_id     BIGINT      CHECK (resolved_org_id > 0),
    resolved_repo_id    UUID        REFERENCES source_repositories(repo_id) ON DELETE SET NULL,
    trace_id            TEXT        NOT NULL DEFAULT '',
    details             JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, delivery_id)
);

CREATE INDEX idx_source_webhook_deliveries_created
    ON source_webhook_deliveries (created_at DESC, webhook_delivery_id DESC);

CREATE TABLE source_events (
    event_id       UUID        PRIMARY KEY,
    org_id         BIGINT      NOT NULL CHECK (org_id > 0),
    actor_id       TEXT        NOT NULL DEFAULT '',
    repo_id        UUID,
    event_type     TEXT        NOT NULL CHECK (event_type <> ''),
    result         TEXT        NOT NULL CHECK (result IN ('allowed', 'denied', 'error')),
    trace_id       TEXT        NOT NULL DEFAULT '',
    details        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_source_events_org_created
    ON source_events (org_id, created_at DESC, event_id DESC);

CREATE INDEX idx_source_events_trace_id
    ON source_events (trace_id)
    WHERE trace_id <> '';
