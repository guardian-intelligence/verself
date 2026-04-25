-- Source-code hosting service control-plane schema.
-- Database: source_code_hosting (one database per service).

CREATE TABLE source_repositories (
    repo_id          UUID        PRIMARY KEY,
    org_id           BIGINT      NOT NULL CHECK (org_id > 0),
    org_path         TEXT        NOT NULL CHECK (org_path <> ''),
    created_by       TEXT        NOT NULL CHECK (created_by <> ''),
    name             TEXT        NOT NULL CHECK (name <> ''),
    slug             TEXT        NOT NULL CHECK (slug <> ''),
    description      TEXT        NOT NULL DEFAULT '',
    default_branch   TEXT        NOT NULL DEFAULT 'main',
    visibility       TEXT        NOT NULL DEFAULT 'private' CHECK (visibility IN ('private')),
    state            TEXT        NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'deleted')),
    version          BIGINT      NOT NULL DEFAULT 1 CHECK (version > 0),
    last_pushed_at   TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ,
    UNIQUE (org_id, slug),
    UNIQUE (org_path, slug)
);

CREATE INDEX idx_source_repositories_org_updated
    ON source_repositories (org_id, updated_at DESC, repo_id DESC)
    WHERE state = 'active';

CREATE TABLE source_repository_backends (
    backend_id         UUID        PRIMARY KEY,
    repo_id            UUID        NOT NULL REFERENCES source_repositories(repo_id) ON DELETE CASCADE,
    backend            TEXT        NOT NULL CHECK (backend <> ''),
    backend_owner      TEXT        NOT NULL CHECK (backend_owner <> ''),
    backend_repo       TEXT        NOT NULL CHECK (backend_repo <> ''),
    backend_repo_id    TEXT        NOT NULL DEFAULT '',
    state              TEXT        NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'disabled')),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repo_id, backend),
    UNIQUE (backend, backend_owner, backend_repo)
);

CREATE INDEX idx_source_repository_backends_backend_id
    ON source_repository_backends (backend, backend_repo_id)
    WHERE backend_repo_id <> '';

CREATE TABLE source_git_credentials (
    credential_id UUID        PRIMARY KEY,
    org_id        BIGINT      NOT NULL CHECK (org_id > 0),
    org_path      TEXT        NOT NULL CHECK (org_path <> ''),
    actor_id      TEXT        NOT NULL CHECK (actor_id <> ''),
    label         TEXT        NOT NULL CHECK (label <> ''),
    username      TEXT        NOT NULL CHECK (username <> ''),
    token_hash    TEXT        NOT NULL UNIQUE CHECK (token_hash <> ''),
    token_prefix  TEXT        NOT NULL CHECK (token_prefix <> ''),
    scopes        TEXT[]      NOT NULL DEFAULT ARRAY['repo:read','repo:write']::TEXT[],
    state         TEXT        NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'revoked')),
    expires_at    TIMESTAMPTZ NOT NULL,
    last_used_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at    TIMESTAMPTZ
);

CREATE INDEX idx_source_git_credentials_org_created
    ON source_git_credentials (org_id, created_at DESC, credential_id DESC);

CREATE TABLE source_ref_heads (
    repo_id      UUID        NOT NULL REFERENCES source_repositories(repo_id) ON DELETE CASCADE,
    org_id       BIGINT      NOT NULL CHECK (org_id > 0),
    ref_name     TEXT        NOT NULL CHECK (ref_name <> ''),
    commit_sha   TEXT        NOT NULL CHECK (commit_sha <> ''),
    is_default   BOOLEAN     NOT NULL DEFAULT false,
    pushed_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (repo_id, ref_name)
);

CREATE INDEX idx_source_ref_heads_org_updated
    ON source_ref_heads (org_id, updated_at DESC, repo_id, ref_name);

CREATE TABLE source_ci_runs (
    ci_run_id            UUID        PRIMARY KEY,
    org_id               BIGINT      NOT NULL CHECK (org_id > 0),
    repo_id              UUID        NOT NULL REFERENCES source_repositories(repo_id) ON DELETE CASCADE,
    ref_name             TEXT        NOT NULL CHECK (ref_name <> ''),
    commit_sha           TEXT        NOT NULL DEFAULT '',
    trigger_event        TEXT        NOT NULL CHECK (trigger_event <> ''),
    state                TEXT        NOT NULL CHECK (state IN ('queued', 'running', 'succeeded', 'failed', 'canceled', 'skipped')),
    sandbox_execution_id UUID,
    sandbox_attempt_id   UUID,
    failure_reason       TEXT        NOT NULL DEFAULT '',
    trace_id             TEXT        NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at           TIMESTAMPTZ,
    completed_at         TIMESTAMPTZ,
    UNIQUE (repo_id, ref_name, commit_sha, trigger_event)
);

CREATE INDEX idx_source_ci_runs_repo_created
    ON source_ci_runs (repo_id, created_at DESC, ci_run_id DESC);

CREATE INDEX idx_source_ci_runs_trace_id
    ON source_ci_runs (trace_id)
    WHERE trace_id <> '';

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

CREATE TABLE source_workflow_runs (
    workflow_run_id        UUID        PRIMARY KEY,
    org_id                 BIGINT      NOT NULL CHECK (org_id > 0),
    repo_id                UUID        NOT NULL REFERENCES source_repositories(repo_id) ON DELETE CASCADE,
    actor_id               TEXT        NOT NULL CHECK (actor_id <> ''),
    idempotency_key        TEXT        NOT NULL CHECK (idempotency_key <> ''),
    backend                TEXT        NOT NULL CHECK (backend <> ''),
    workflow_path          TEXT        NOT NULL CHECK (workflow_path <> ''),
    ref                    TEXT        NOT NULL CHECK (ref <> ''),
    inputs_json            JSONB       NOT NULL DEFAULT '{}'::jsonb,
    state                  TEXT        NOT NULL CHECK (state IN ('dispatching', 'dispatched', 'failed')),
    backend_dispatch_id    TEXT        NOT NULL DEFAULT '',
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

CREATE TABLE source_storage_events (
    storage_event_id      UUID        PRIMARY KEY,
    org_id                BIGINT      NOT NULL CHECK (org_id > 0),
    repo_id               UUID        REFERENCES source_repositories(repo_id) ON DELETE SET NULL,
    backend               TEXT        NOT NULL CHECK (backend <> ''),
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
    backend             TEXT        NOT NULL CHECK (backend <> ''),
    delivery_id         TEXT        NOT NULL CHECK (delivery_id <> ''),
    event_type          TEXT        NOT NULL CHECK (event_type <> ''),
    signature_valid     BOOLEAN     NOT NULL,
    result              TEXT        NOT NULL CHECK (result IN ('accepted', 'denied', 'unresolved', 'error')),
    resolved_org_id     BIGINT      CHECK (resolved_org_id > 0),
    resolved_repo_id    UUID        REFERENCES source_repositories(repo_id) ON DELETE SET NULL,
    trace_id            TEXT        NOT NULL DEFAULT '',
    details             JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (backend, delivery_id)
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
