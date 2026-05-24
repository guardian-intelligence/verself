CREATE TABLE IF NOT EXISTS github_accounts (
    provider_account_id      BIGINT      PRIMARY KEY CHECK (provider_account_id > 0),
    login                    TEXT        NOT NULL CHECK (login <> ''),
    account_type             TEXT        NOT NULL CHECK (account_type <> ''),
    avatar_url               TEXT        NOT NULL DEFAULT '',
    html_url                 TEXT        NOT NULL DEFAULT '',
    state                    TEXT        NOT NULL CHECK (state <> ''),
    last_event_delivery_id   TEXT        NOT NULL DEFAULT '',
    observed_from_api_at     TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE github_installations ADD COLUMN IF NOT EXISTS provider_account_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE github_installations ADD COLUMN IF NOT EXISTS target_type TEXT NOT NULL DEFAULT '';
ALTER TABLE github_installations ADD COLUMN IF NOT EXISTS configuration_url TEXT NOT NULL DEFAULT '';
ALTER TABLE github_installations ADD COLUMN IF NOT EXISTS suspended_by TEXT NOT NULL DEFAULT '';
ALTER TABLE github_installations ADD COLUMN IF NOT EXISTS suspended_at TIMESTAMPTZ;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'github_installations'
          AND column_name = 'account_id'
    ) THEN
        EXECUTE $backfill$
            UPDATE github_installations
            SET provider_account_id = account_id
            WHERE provider_account_id = 0
              AND account_id > 0
        $backfill$;
    END IF;
END
$$;

DROP INDEX IF EXISTS idx_github_installations_account;
CREATE INDEX idx_github_installations_account
    ON github_installations (provider_account_id, state, updated_at DESC);

CREATE TABLE IF NOT EXISTS github_setup_sessions (
    setup_session_id         UUID        PRIMARY KEY,
    org_id                   TEXT        NOT NULL CHECK (org_id <> ''),
    actor_id                 TEXT        NOT NULL CHECK (actor_id <> ''),
    state_hash               TEXT        NOT NULL UNIQUE CHECK (state_hash <> ''),
    session_state            TEXT        NOT NULL CHECK (session_state <> ''),
    installation_url         TEXT        NOT NULL DEFAULT '',
    callback_url             TEXT        NOT NULL DEFAULT '',
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    github_user_authorization_id UUID,
    installation_binding_id  UUID,
    failure_reason           TEXT        NOT NULL DEFAULT '',
    expires_at               TIMESTAMPTZ NOT NULL,
    completed_at             TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_github_setup_sessions_org_state
    ON github_setup_sessions (org_id, session_state, updated_at DESC);

CREATE TABLE IF NOT EXISTS github_oauth_sessions (
    oauth_session_id         UUID        PRIMARY KEY,
    org_id                   TEXT        NOT NULL CHECK (org_id <> ''),
    actor_id                 TEXT        NOT NULL CHECK (actor_id <> ''),
    state_hash               TEXT        NOT NULL UNIQUE CHECK (state_hash <> ''),
    session_state            TEXT        NOT NULL CHECK (session_state <> ''),
    authorization_url        TEXT        NOT NULL DEFAULT '',
    callback_url             TEXT        NOT NULL DEFAULT '',
    github_user_authorization_id UUID,
    failure_reason           TEXT        NOT NULL DEFAULT '',
    expires_at               TIMESTAMPTZ NOT NULL,
    completed_at             TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_github_oauth_sessions_org_state
    ON github_oauth_sessions (org_id, session_state, updated_at DESC);

CREATE TABLE IF NOT EXISTS github_idempotency_records (
    org_id                   TEXT        NOT NULL CHECK (org_id <> ''),
    actor_id                 TEXT        NOT NULL CHECK (actor_id <> ''),
    operation                TEXT        NOT NULL CHECK (operation <> ''),
    key_hash                 TEXT        NOT NULL CHECK (key_hash ~ '^[a-f0-9]{64}$'),
    request_hash             TEXT        NOT NULL CHECK (request_hash ~ '^[a-f0-9]{64}$'),
    result_payload           JSONB       NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, actor_id, operation, key_hash)
);

CREATE TABLE IF NOT EXISTS github_user_authorizations (
    github_user_authorization_id UUID    PRIMARY KEY,
    org_id                   TEXT        NOT NULL CHECK (org_id <> ''),
    actor_id                 TEXT        NOT NULL CHECK (actor_id <> ''),
    provider_user_id         BIGINT      NOT NULL CHECK (provider_user_id > 0),
    github_login             TEXT        NOT NULL CHECK (github_login <> ''),
    scopes_json              JSONB       NOT NULL DEFAULT '[]'::jsonb,
    credential_ref           TEXT        NOT NULL CHECK (credential_ref <> ''),
    state                    TEXT        NOT NULL CHECK (state <> ''),
    authorized_at            TIMESTAMPTZ NOT NULL,
    last_verified_at         TIMESTAMPTZ,
    revoked_at               TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_github_user_authorizations_active
    ON github_user_authorizations (org_id, actor_id, provider_user_id)
    WHERE state = 'active';

CREATE TABLE IF NOT EXISTS github_installation_bindings (
    installation_binding_id  UUID        PRIMARY KEY,
    org_id                   TEXT        NOT NULL CHECK (org_id <> ''),
    provider_installation_id BIGINT      NOT NULL REFERENCES github_installations(provider_installation_id) ON DELETE CASCADE,
    provider_account_id      BIGINT      NOT NULL DEFAULT 0,
    setup_session_id         UUID        REFERENCES github_setup_sessions(setup_session_id) ON DELETE SET NULL,
    connected_by_actor_id    TEXT        NOT NULL CHECK (connected_by_actor_id <> ''),
    disconnected_by_actor_id TEXT        NOT NULL DEFAULT '',
    state                    TEXT        NOT NULL CHECK (state <> ''),
    connected_at             TIMESTAMPTZ NOT NULL,
    disconnected_at          TIMESTAMPTZ,
    last_synced_at           TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_github_installation_bindings_active_installation
    ON github_installation_bindings (provider_installation_id)
    WHERE state = 'active';

CREATE INDEX IF NOT EXISTS idx_github_installation_bindings_org
    ON github_installation_bindings (org_id, state, updated_at DESC);

ALTER TABLE github_repositories ADD COLUMN IF NOT EXISTS provider_account_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE github_repositories ADD COLUMN IF NOT EXISTS archived BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE github_repositories ADD COLUMN IF NOT EXISTS visibility TEXT NOT NULL DEFAULT '';
ALTER TABLE github_repositories ADD COLUMN IF NOT EXISTS html_url TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS github_installation_repositories (
    provider_installation_id BIGINT      NOT NULL REFERENCES github_installations(provider_installation_id) ON DELETE CASCADE,
    provider_repository_id   BIGINT      NOT NULL REFERENCES github_repositories(provider_repository_id) ON DELETE CASCADE,
    state                    TEXT        NOT NULL CHECK (state <> ''),
    last_event_delivery_id   TEXT        NOT NULL DEFAULT '',
    observed_from_api_at     TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider_installation_id, provider_repository_id)
);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'github_repositories'
          AND column_name = 'provider_installation_id'
    ) THEN
        EXECUTE $backfill$
            INSERT INTO github_installation_repositories (
                provider_installation_id,
                provider_repository_id,
                state,
                last_event_delivery_id,
                observed_from_api_at,
                updated_at
            )
            SELECT provider_installation_id, provider_repository_id, 'selected',
                   last_event_delivery_id, observed_from_api_at, updated_at
            FROM github_repositories
            WHERE provider_installation_id > 0
            ON CONFLICT (provider_installation_id, provider_repository_id) DO NOTHING
        $backfill$;
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS idx_github_installation_repositories_installation
    ON github_installation_repositories (provider_installation_id, state, updated_at DESC);

CREATE TABLE IF NOT EXISTS github_repository_bindings (
    repository_binding_id    UUID        PRIMARY KEY,
    org_id                   TEXT        NOT NULL CHECK (org_id <> ''),
    installation_binding_id  UUID        NOT NULL REFERENCES github_installation_bindings(installation_binding_id) ON DELETE CASCADE,
    provider_installation_id BIGINT      NOT NULL,
    provider_repository_id   BIGINT      NOT NULL REFERENCES github_repositories(provider_repository_id) ON DELETE CASCADE,
    state                    TEXT        NOT NULL CHECK (state <> ''),
    enabled_by_actor_id      TEXT        NOT NULL DEFAULT '',
    disabled_by_actor_id     TEXT        NOT NULL DEFAULT '',
    enabled_at               TIMESTAMPTZ,
    disabled_at              TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_github_repository_bindings_enabled_repo
    ON github_repository_bindings (org_id, provider_repository_id)
    WHERE state = 'enabled';

CREATE UNIQUE INDEX IF NOT EXISTS idx_github_repository_bindings_installation_repo
    ON github_repository_bindings (installation_binding_id, provider_repository_id);

CREATE INDEX IF NOT EXISTS idx_github_repository_bindings_org
    ON github_repository_bindings (org_id, state, updated_at DESC);

CREATE TABLE IF NOT EXISTS github_provider_reconciliations (
    reconciliation_id        UUID        PRIMARY KEY,
    org_id                   TEXT        NOT NULL DEFAULT '',
    installation_binding_id  UUID,
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    reason                   TEXT        NOT NULL CHECK (reason <> ''),
    state                    TEXT        NOT NULL CHECK (state <> ''),
    attempt_count            INTEGER     NOT NULL DEFAULT 0,
    failure_reason           TEXT        NOT NULL DEFAULT '',
    started_at               TIMESTAMPTZ,
    completed_at             TIMESTAMPTZ,
    next_attempt_at          TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_github_provider_reconciliations_ready
    ON github_provider_reconciliations (next_attempt_at NULLS FIRST, created_at)
    WHERE state IN ('pending', 'retryable');

ALTER TABLE github_workflow_jobs ADD COLUMN IF NOT EXISTS org_id TEXT NOT NULL DEFAULT '';
ALTER TABLE github_workflow_jobs ADD COLUMN IF NOT EXISTS installation_binding_id UUID;
ALTER TABLE github_workflow_jobs ADD COLUMN IF NOT EXISTS repository_binding_id UUID;
ALTER TABLE github_job_shapes ADD COLUMN IF NOT EXISTS org_id TEXT NOT NULL DEFAULT '';
ALTER TABLE github_job_shapes ADD COLUMN IF NOT EXISTS installation_binding_id UUID;
ALTER TABLE github_job_shapes ADD COLUMN IF NOT EXISTS repository_binding_id UUID;
ALTER TABLE github_workflow_runs ADD COLUMN IF NOT EXISTS org_id TEXT NOT NULL DEFAULT '';
ALTER TABLE github_workflow_runs ADD COLUMN IF NOT EXISTS installation_binding_id UUID;
ALTER TABLE github_workflow_runs ADD COLUMN IF NOT EXISTS repository_binding_id UUID;
ALTER TABLE github_provider_demands ADD COLUMN IF NOT EXISTS org_id TEXT NOT NULL DEFAULT '';
ALTER TABLE github_provider_demands ADD COLUMN IF NOT EXISTS installation_binding_id UUID;
ALTER TABLE github_provider_demands ADD COLUMN IF NOT EXISTS repository_binding_id UUID;
ALTER TABLE github_provider_outbox ADD COLUMN IF NOT EXISTS org_id TEXT NOT NULL DEFAULT '';
ALTER TABLE github_provider_outbox ADD COLUMN IF NOT EXISTS installation_binding_id UUID;
ALTER TABLE github_provider_outbox ADD COLUMN IF NOT EXISTS repository_binding_id UUID;
ALTER TABLE github_provider_outbox ADD COLUMN IF NOT EXISTS repository_full_name TEXT NOT NULL DEFAULT '';
ALTER TABLE github_runner_instances ADD COLUMN IF NOT EXISTS org_id TEXT NOT NULL DEFAULT '';
ALTER TABLE github_runner_instances ADD COLUMN IF NOT EXISTS installation_binding_id UUID;
ALTER TABLE github_runner_instances ADD COLUMN IF NOT EXISTS repository_binding_id UUID;
ALTER TABLE github_terminal_job_evidence ADD COLUMN IF NOT EXISTS org_id TEXT NOT NULL DEFAULT '';
ALTER TABLE github_terminal_job_evidence ADD COLUMN IF NOT EXISTS installation_binding_id UUID;
ALTER TABLE github_terminal_job_evidence ADD COLUMN IF NOT EXISTS repository_binding_id UUID;
ALTER TABLE github_terminal_job_evidence ADD COLUMN IF NOT EXISTS repository_full_name TEXT NOT NULL DEFAULT '';
ALTER TABLE github_golden_snapshot_barriers ADD COLUMN IF NOT EXISTS org_id TEXT NOT NULL DEFAULT '';
ALTER TABLE github_golden_snapshot_barriers ADD COLUMN IF NOT EXISTS installation_binding_id UUID;
ALTER TABLE github_golden_snapshot_barriers ADD COLUMN IF NOT EXISTS repository_binding_id UUID;
ALTER TABLE github_golden_snapshot_barriers ADD COLUMN IF NOT EXISTS provider_installation_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE github_golden_snapshot_barriers ADD COLUMN IF NOT EXISTS provider_repository_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE github_golden_snapshot_barriers ADD COLUMN IF NOT EXISTS repository_full_name TEXT NOT NULL DEFAULT '';

DROP INDEX IF EXISTS idx_github_repositories_installation;
ALTER TABLE github_installations DROP COLUMN IF EXISTS org_id;
ALTER TABLE github_installations DROP COLUMN IF EXISTS account_id;
ALTER TABLE github_repositories DROP COLUMN IF EXISTS provider_installation_id;
ALTER TABLE github_repositories DROP COLUMN IF EXISTS org_id;

CREATE INDEX IF NOT EXISTS idx_github_repositories_owner
    ON github_repositories (owner_login, repository_name);
