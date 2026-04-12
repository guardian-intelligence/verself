CREATE TABLE IF NOT EXISTS identity_policy_documents (
    org_id TEXT PRIMARY KEY,
    document JSONB NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS identity_api_credentials (
    credential_id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    client_id TEXT NOT NULL,
    display_name TEXT NOT NULL,
    auth_method TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    policy_version_at_issue INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    revoked_by TEXT,
    last_used_at TIMESTAMPTZ,
    CHECK (length(btrim(credential_id)) > 0),
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(btrim(subject_id)) > 0),
    CHECK (length(btrim(client_id)) > 0),
    CHECK (length(btrim(display_name)) > 0),
    CHECK (auth_method IN ('private_key_jwt', 'client_secret')),
    CHECK (length(btrim(created_by)) > 0),
    CHECK (policy_version_at_issue >= 0),
    CHECK (status IN ('active', 'revoked')),
    CHECK (expires_at IS NULL OR expires_at > created_at),
    CHECK (
        (status = 'active' AND revoked_at IS NULL AND revoked_by IS NULL)
        OR
        (status = 'revoked' AND revoked_at IS NOT NULL AND length(btrim(revoked_by)) > 0)
    )
);

CREATE INDEX IF NOT EXISTS identity_api_credentials_org_subject_idx
    ON identity_api_credentials (org_id, subject_id, status);

CREATE UNIQUE INDEX IF NOT EXISTS identity_api_credentials_active_subject_idx
    ON identity_api_credentials (subject_id)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS identity_api_credential_permissions (
    credential_id TEXT NOT NULL REFERENCES identity_api_credentials (credential_id) ON DELETE CASCADE,
    permission TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (credential_id, permission),
    CHECK (length(btrim(permission)) > 0)
);

CREATE TABLE IF NOT EXISTS identity_api_credential_secrets (
    secret_id TEXT PRIMARY KEY,
    credential_id TEXT NOT NULL REFERENCES identity_api_credentials (credential_id) ON DELETE CASCADE,
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

CREATE INDEX IF NOT EXISTS identity_api_credential_secrets_active_idx
    ON identity_api_credential_secrets (credential_id, expires_at)
    WHERE revoked_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS identity_api_credential_secrets_provider_key_idx
    ON identity_api_credential_secrets (auth_method, provider_key_id)
    WHERE revoked_at IS NULL;
