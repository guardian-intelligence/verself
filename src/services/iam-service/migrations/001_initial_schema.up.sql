CREATE TABLE IF NOT EXISTS iam_organizations (
    org_id TEXT PRIMARY KEY,
    identity_provider_org_id TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    state TEXT NOT NULL DEFAULT 'active',
    version INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT NOT NULL,
    CHECK (org_id ~ '^org_[0-9A-HJKMNP-TV-Z]{26}$'),
    CHECK (identity_provider_org_id ~ '^[0-9]+$'),
    CHECK (length(btrim(display_name)) > 0),
    CHECK (slug ~ '^[a-z0-9]([a-z0-9-]{0,78}[a-z0-9])?$'),
    CHECK (state IN ('active')),
    CHECK (version > 0),
    CHECK (length(btrim(created_by)) > 0),
    CHECK (length(btrim(updated_by)) > 0)
);

CREATE TABLE IF NOT EXISTS iam_organization_slug_redirects (
    slug TEXT PRIMARY KEY,
    org_id TEXT NOT NULL REFERENCES iam_organizations (org_id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by TEXT NOT NULL,
    CHECK (slug ~ '^[a-z0-9]([a-z0-9-]{0,78}[a-z0-9])?$'),
    CHECK (length(btrim(created_by)) > 0)
);

CREATE INDEX IF NOT EXISTS iam_organization_slug_redirects_org_idx
    ON iam_organization_slug_redirects (org_id, created_at DESC);

CREATE TABLE IF NOT EXISTS iam_service_accounts (
    service_account_id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    client_id TEXT NOT NULL,
    display_name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at TIMESTAMPTZ,
    disabled_by TEXT,
    last_used_at TIMESTAMPTZ,
    CHECK (length(btrim(service_account_id)) > 0),
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(btrim(subject_id)) > 0),
    CHECK (length(btrim(client_id)) > 0),
    CHECK (length(btrim(display_name)) > 0),
    CHECK (status IN ('active', 'disabled')),
    CHECK (length(btrim(created_by)) > 0),
    CHECK (
        (status = 'active' AND disabled_at IS NULL AND disabled_by IS NULL)
        OR
        (status = 'disabled' AND disabled_at IS NOT NULL AND length(btrim(disabled_by)) > 0)
    )
);

CREATE INDEX IF NOT EXISTS iam_service_accounts_org_status_idx
    ON iam_service_accounts (org_id, status, created_at DESC, service_account_id DESC);

CREATE UNIQUE INDEX IF NOT EXISTS iam_service_accounts_subject_idx
    ON iam_service_accounts (subject_id);

CREATE TABLE IF NOT EXISTS iam_api_credentials (
    credential_id TEXT PRIMARY KEY,
    service_account_id TEXT NOT NULL REFERENCES iam_service_accounts (service_account_id) ON DELETE CASCADE,
    org_id TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    client_id TEXT NOT NULL,
    display_name TEXT NOT NULL,
    auth_method TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    revoked_by TEXT,
    last_used_at TIMESTAMPTZ,
    CHECK (length(btrim(credential_id)) > 0),
    CHECK (length(btrim(service_account_id)) > 0),
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(btrim(subject_id)) > 0),
    CHECK (length(btrim(client_id)) > 0),
    CHECK (length(btrim(display_name)) > 0),
    CHECK (auth_method IN ('private_key_jwt', 'client_secret')),
    CHECK (length(btrim(created_by)) > 0),
    CHECK (status IN ('active', 'revoked')),
    CHECK (expires_at IS NULL OR expires_at > created_at),
    CHECK (
        (status = 'active' AND revoked_at IS NULL AND revoked_by IS NULL)
        OR
        (status = 'revoked' AND revoked_at IS NOT NULL AND length(btrim(revoked_by)) > 0)
    )
);

CREATE INDEX IF NOT EXISTS iam_api_credentials_org_subject_idx
    ON iam_api_credentials (org_id, subject_id, status);

CREATE UNIQUE INDEX IF NOT EXISTS iam_api_credentials_active_subject_idx
    ON iam_api_credentials (subject_id)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS iam_api_credentials_service_account_idx
    ON iam_api_credentials (service_account_id, status, created_at DESC, credential_id DESC);

CREATE TABLE IF NOT EXISTS iam_api_credential_secrets (
    secret_id TEXT PRIMARY KEY,
    credential_id TEXT NOT NULL REFERENCES iam_api_credentials (credential_id) ON DELETE CASCADE,
    auth_method TEXT NOT NULL,
    provider_key_id TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    secret_hash BYTEA NOT NULL UNIQUE,
    hash_algorithm TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by TEXT NOT NULL,
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    revoked_by TEXT,
    CHECK (length(btrim(secret_id)) > 0),
    CHECK (auth_method IN ('private_key_jwt', 'client_secret')),
    CHECK (length(btrim(provider_key_id)) > 0),
    CHECK (length(btrim(fingerprint)) > 0),
    CHECK (length(secret_hash) > 0),
    CHECK (length(btrim(hash_algorithm)) > 0),
    CHECK (length(btrim(created_by)) > 0),
    CHECK (expires_at IS NULL OR expires_at > created_at),
    CHECK (revoked_at IS NULL OR length(btrim(revoked_by)) > 0)
);

CREATE INDEX IF NOT EXISTS iam_api_credential_secrets_active_idx
    ON iam_api_credential_secrets (credential_id, expires_at)
    WHERE revoked_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS iam_api_credential_secrets_provider_key_idx
    ON iam_api_credential_secrets (auth_method, provider_key_id)
    WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS iam_browser_login_transactions (
    state_hash TEXT PRIMARY KEY,
    nonce TEXT NOT NULL,
    code_verifier TEXT NOT NULL,
    redirect_to TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (length(btrim(state_hash)) > 0),
    CHECK (length(btrim(nonce)) > 0),
    CHECK (length(btrim(code_verifier)) > 0),
    CHECK (length(btrim(redirect_to)) > 0),
    CHECK (expires_at > created_at)
);

CREATE INDEX IF NOT EXISTS iam_browser_login_transactions_expires_at_idx
    ON iam_browser_login_transactions (expires_at);

CREATE TABLE IF NOT EXISTS iam_browser_sessions (
    session_hash TEXT PRIMARY KEY,
    client_cache_partition TEXT NOT NULL,
    subject TEXT NOT NULL,
    email TEXT,
    display_name TEXT,
    preferred_username TEXT,
    org_id TEXT,
    home_org_id TEXT,
    selected_org_id TEXT,
    available_org_contexts JSONB NOT NULL DEFAULT '[]'::jsonb,
    user_claims JSONB NOT NULL DEFAULT '{}'::jsonb,
    id_token TEXT,
    access_token TEXT NOT NULL,
    refresh_token TEXT,
    token_scope TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (length(btrim(session_hash)) > 0),
    CHECK (length(btrim(client_cache_partition)) > 0),
    CHECK (length(btrim(subject)) > 0),
    CHECK (length(btrim(access_token)) > 0),
    CHECK (expires_at > created_at)
);

CREATE INDEX IF NOT EXISTS iam_browser_sessions_subject_idx
    ON iam_browser_sessions (subject);

CREATE INDEX IF NOT EXISTS iam_browser_sessions_expires_at_idx
    ON iam_browser_sessions (expires_at);

CREATE TABLE IF NOT EXISTS iam_browser_resource_tokens (
    session_hash TEXT NOT NULL REFERENCES iam_browser_sessions (session_hash) ON DELETE CASCADE,
    audience TEXT NOT NULL,
    selected_org_id TEXT NOT NULL,
    scope_hash TEXT NOT NULL,
    access_token TEXT NOT NULL,
    token_scope TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (session_hash, audience, selected_org_id, scope_hash),
    CHECK (length(btrim(session_hash)) > 0),
    CHECK (length(btrim(audience)) > 0),
    CHECK (length(btrim(selected_org_id)) > 0),
    CHECK (length(btrim(scope_hash)) > 0),
    CHECK (length(btrim(access_token)) > 0),
    CHECK (expires_at > created_at)
);

CREATE INDEX IF NOT EXISTS iam_browser_resource_tokens_expires_at_idx
    ON iam_browser_resource_tokens (expires_at);
