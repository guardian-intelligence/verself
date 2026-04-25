-- Source-code hosting service control-plane schema.
-- Database: source_code_hosting (one database per service).

CREATE TABLE forgejo_installations (
    installation_id UUID        PRIMARY KEY,
    base_url        TEXT        NOT NULL CHECK (base_url LIKE 'http%://%'),
    owner_username  TEXT        NOT NULL CHECK (owner_username <> ''),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
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
    forgejo_owner    TEXT        NOT NULL CHECK (forgejo_owner <> ''),
    forgejo_repo     TEXT        NOT NULL CHECK (forgejo_repo <> ''),
    forgejo_repo_id  BIGINT      NOT NULL DEFAULT 0,
    state            TEXT        NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'deleted')),
    version          BIGINT      NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ,
    UNIQUE (org_id, slug),
    UNIQUE (forgejo_owner, forgejo_repo)
);

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
