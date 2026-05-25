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
    client_hash TEXT NOT NULL,
    nonce TEXT NOT NULL,
    code_verifier TEXT NOT NULL,
    redirect_to TEXT NOT NULL,
    purpose TEXT NOT NULL DEFAULT 'login',
    login_hint TEXT,
    required_subject TEXT,
    required_email TEXT,
    required_org_id TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (length(btrim(state_hash)) > 0),
    CHECK (length(btrim(client_hash)) > 0),
    CHECK (length(btrim(nonce)) > 0),
    CHECK (length(btrim(code_verifier)) > 0),
    CHECK (length(btrim(redirect_to)) > 0),
    CHECK (length(btrim(purpose)) > 0),
    CHECK (expires_at > created_at)
);

CREATE INDEX IF NOT EXISTS iam_browser_login_transactions_expires_at_idx
    ON iam_browser_login_transactions (expires_at);

CREATE TABLE IF NOT EXISTS iam_browser_clients (
    client_hash TEXT PRIMARY KEY,
    client_handle TEXT NOT NULL,
    active_account_handle TEXT,
    client_cache_partition TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_client_ip TEXT NOT NULL,
    created_client_ip_trusted BOOLEAN NOT NULL,
    created_client_ip_source TEXT NOT NULL DEFAULT '',
    created_edge_peer_ip TEXT NOT NULL DEFAULT '',
    created_user_agent TEXT NOT NULL,
    created_device_label TEXT NOT NULL DEFAULT '',
    created_device_kind TEXT NOT NULL DEFAULT '',
    created_browser_name TEXT NOT NULL DEFAULT '',
    created_os_name TEXT NOT NULL DEFAULT '',
    created_geo_country_code TEXT NOT NULL DEFAULT '',
    created_geo_region TEXT NOT NULL DEFAULT '',
    created_geo_city TEXT NOT NULL DEFAULT '',
    last_seen_client_ip TEXT NOT NULL,
    last_seen_client_ip_trusted BOOLEAN NOT NULL,
    last_seen_client_ip_source TEXT NOT NULL DEFAULT '',
    last_seen_edge_peer_ip TEXT NOT NULL DEFAULT '',
    last_seen_user_agent TEXT NOT NULL,
    last_seen_device_label TEXT NOT NULL DEFAULT '',
    last_seen_device_kind TEXT NOT NULL DEFAULT '',
    last_seen_browser_name TEXT NOT NULL DEFAULT '',
    last_seen_os_name TEXT NOT NULL DEFAULT '',
    last_seen_geo_country_code TEXT NOT NULL DEFAULT '',
    last_seen_geo_region TEXT NOT NULL DEFAULT '',
    last_seen_geo_city TEXT NOT NULL DEFAULT '',
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (length(btrim(client_hash)) > 0),
    CHECK (length(btrim(client_handle)) > 0),
    CHECK (length(btrim(client_cache_partition)) > 0),
    CHECK (expires_at > created_at)
);

CREATE UNIQUE INDEX IF NOT EXISTS iam_browser_clients_client_handle_idx
    ON iam_browser_clients (client_handle);

CREATE INDEX IF NOT EXISTS iam_browser_clients_expires_at_idx
    ON iam_browser_clients (expires_at);

CREATE TABLE IF NOT EXISTS iam_browser_accounts (
    account_handle TEXT PRIMARY KEY,
    client_hash TEXT NOT NULL REFERENCES iam_browser_clients (client_hash) ON DELETE CASCADE,
    state TEXT NOT NULL DEFAULT 'active',
    subject TEXT NOT NULL,
    email TEXT,
    display_name TEXT,
    preferred_username TEXT,
    org_id TEXT,
    home_org_id TEXT,
    selected_org_id TEXT,
    available_org_contexts JSONB NOT NULL DEFAULT '[]'::jsonb,
    user_claims JSONB NOT NULL DEFAULT '{}'::jsonb,
    id_token_ciphertext TEXT,
    access_token_ciphertext TEXT NOT NULL,
    refresh_token_ciphertext TEXT,
    token_scope TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    created_client_ip TEXT NOT NULL,
    created_client_ip_trusted BOOLEAN NOT NULL,
    created_client_ip_source TEXT NOT NULL DEFAULT '',
    created_edge_peer_ip TEXT NOT NULL DEFAULT '',
    created_user_agent TEXT NOT NULL,
    created_device_label TEXT NOT NULL DEFAULT '',
    created_device_kind TEXT NOT NULL DEFAULT '',
    created_browser_name TEXT NOT NULL DEFAULT '',
    created_os_name TEXT NOT NULL DEFAULT '',
    created_geo_country_code TEXT NOT NULL DEFAULT '',
    created_geo_region TEXT NOT NULL DEFAULT '',
    created_geo_city TEXT NOT NULL DEFAULT '',
    last_seen_client_ip TEXT NOT NULL,
    last_seen_client_ip_trusted BOOLEAN NOT NULL,
    last_seen_client_ip_source TEXT NOT NULL DEFAULT '',
    last_seen_edge_peer_ip TEXT NOT NULL DEFAULT '',
    last_seen_user_agent TEXT NOT NULL,
    last_seen_device_label TEXT NOT NULL DEFAULT '',
    last_seen_device_kind TEXT NOT NULL DEFAULT '',
    last_seen_browser_name TEXT NOT NULL DEFAULT '',
    last_seen_os_name TEXT NOT NULL DEFAULT '',
    last_seen_geo_country_code TEXT NOT NULL DEFAULT '',
    last_seen_geo_region TEXT NOT NULL DEFAULT '',
    last_seen_geo_city TEXT NOT NULL DEFAULT '',
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (length(btrim(account_handle)) > 0),
    CHECK (length(btrim(client_hash)) > 0),
    CHECK (length(btrim(state)) > 0),
    CHECK (length(btrim(subject)) > 0),
    CHECK (length(btrim(access_token_ciphertext)) > 0),
    CHECK (expires_at > created_at)
);

CREATE INDEX IF NOT EXISTS iam_browser_accounts_client_idx
    ON iam_browser_accounts (client_hash, last_seen_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS iam_browser_accounts_client_subject_idx
    ON iam_browser_accounts (client_hash, subject);

CREATE INDEX IF NOT EXISTS iam_browser_accounts_subject_idx
    ON iam_browser_accounts (subject, last_seen_at DESC);

CREATE INDEX IF NOT EXISTS iam_browser_accounts_expires_at_idx
    ON iam_browser_accounts (expires_at);

CREATE TABLE IF NOT EXISTS iam_browser_account_observations (
    observation_id BIGSERIAL PRIMARY KEY,
    client_hash TEXT NOT NULL REFERENCES iam_browser_clients (client_hash) ON DELETE CASCADE,
    account_handle TEXT NOT NULL REFERENCES iam_browser_accounts (account_handle) ON DELETE CASCADE,
    subject TEXT NOT NULL,
    client_ip TEXT NOT NULL,
    client_ip_trusted BOOLEAN NOT NULL DEFAULT false,
    client_ip_source TEXT NOT NULL DEFAULT '',
    edge_peer_ip TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    device_label TEXT NOT NULL DEFAULT '',
    device_kind TEXT NOT NULL DEFAULT '',
    browser_name TEXT NOT NULL DEFAULT '',
    os_name TEXT NOT NULL DEFAULT '',
    geo_country_code TEXT NOT NULL DEFAULT '',
    geo_region TEXT NOT NULL DEFAULT '',
    geo_city TEXT NOT NULL DEFAULT '',
    observed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_iam_browser_account_observations_subject_time
    ON iam_browser_account_observations (subject, observed_at DESC);

CREATE INDEX IF NOT EXISTS idx_iam_browser_account_observations_account_time
    ON iam_browser_account_observations (account_handle, observed_at DESC);

CREATE TABLE IF NOT EXISTS iam_browser_resource_tokens (
    account_handle TEXT NOT NULL REFERENCES iam_browser_accounts (account_handle) ON DELETE CASCADE,
    audience TEXT NOT NULL,
    selected_org_id TEXT NOT NULL,
    scope_hash TEXT NOT NULL,
    access_token_ciphertext TEXT NOT NULL,
    token_scope TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_handle, audience, selected_org_id, scope_hash),
    CHECK (length(btrim(account_handle)) > 0),
    CHECK (length(btrim(audience)) > 0),
    CHECK (length(btrim(selected_org_id)) > 0),
    CHECK (length(btrim(scope_hash)) > 0),
    CHECK (length(btrim(access_token_ciphertext)) > 0),
    CHECK (expires_at > created_at)
);

CREATE INDEX IF NOT EXISTS iam_browser_resource_tokens_expires_at_idx
    ON iam_browser_resource_tokens (expires_at);

CREATE TABLE IF NOT EXISTS iam_member_invite_acceptance_tokens (
    token_hash TEXT PRIMARY KEY,
    org_id TEXT NOT NULL REFERENCES iam_organizations (org_id) ON DELETE CASCADE,
    user_id TEXT NOT NULL,
    email TEXT NOT NULL,
    email_verification_code TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    CHECK (token_hash ~ '^sha256:[0-9a-f]{64}$'),
    CHECK (length(btrim(user_id)) > 0),
    CHECK (length(btrim(email)) > 0),
    CHECK (length(btrim(email_verification_code)) > 0),
    CHECK (expires_at > created_at),
    CHECK (accepted_at IS NULL OR accepted_at >= created_at)
);

CREATE INDEX IF NOT EXISTS iam_member_invite_acceptance_tokens_user_idx
    ON iam_member_invite_acceptance_tokens (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS iam_member_invite_acceptance_tokens_expires_idx
    ON iam_member_invite_acceptance_tokens (expires_at)
    WHERE accepted_at IS NULL;

CREATE TABLE IF NOT EXISTS iam_signup_intents (
    signup_intent_id                 TEXT        PRIMARY KEY CHECK (signup_intent_id ~ '^signup_[0-9A-HJKMNP-TV-Z]{26}$'),
    idempotency_key                  TEXT        NOT NULL CHECK (idempotency_key <> ''),
    request_hash                     BYTEA       NOT NULL CHECK (octet_length(request_hash) = 32),
    email_delivery                   TEXT        NOT NULL CHECK (email_delivery <> ''),
    email_identity_hash              BYTEA       NOT NULL CHECK (octet_length(email_identity_hash) = 32),
    email_identity_hash_key_id       TEXT        NOT NULL CHECK (email_identity_hash_key_id <> ''),
    organization_display_name        TEXT        NOT NULL CHECK (organization_display_name <> ''),
    requested_organization_slug      TEXT        NOT NULL DEFAULT '',
    organization_slug                TEXT        NOT NULL DEFAULT '',
    given_name                       TEXT        NOT NULL DEFAULT '',
    family_name                      TEXT        NOT NULL DEFAULT '',
    verification_token_hash          BYTEA       NOT NULL CHECK (octet_length(verification_token_hash) = 32),
    state                            TEXT        NOT NULL CHECK (state IN ('pending_verification', 'materializing', 'completed', 'expired', 'failed_retryable', 'failed_terminal')),
    materialization_step             TEXT        NOT NULL DEFAULT '',
    materialization_attempts         INTEGER     NOT NULL DEFAULT 0 CHECK (materialization_attempts >= 0),
    materialization_last_error       TEXT        NOT NULL DEFAULT '',
    materialization_lease_expires_at TIMESTAMPTZ,
    verify_idempotency_key           TEXT        NOT NULL DEFAULT '',
    verify_request_hash              BYTEA,
    org_id                           TEXT        NOT NULL DEFAULT '',
    identity_provider_org_id         TEXT        NOT NULL DEFAULT '',
    identity_provider_user_id        TEXT        NOT NULL DEFAULT '',
    created_at                       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                       TIMESTAMPTZ NOT NULL DEFAULT now(),
    verification_expires_at          TIMESTAMPTZ NOT NULL,
    verified_at                      TIMESTAMPTZ,
    completed_at                     TIMESTAMPTZ,
    CHECK (verify_request_hash IS NULL OR octet_length(verify_request_hash) = 32),
    CHECK ((state = 'completed') = (completed_at IS NOT NULL)),
    CHECK (state <> 'completed' OR org_id <> ''),
    UNIQUE (idempotency_key)
);

CREATE UNIQUE INDEX IF NOT EXISTS iam_signup_intents_email_identity_hash_idx
    ON iam_signup_intents (email_identity_hash);

CREATE INDEX IF NOT EXISTS iam_signup_intents_state_due_idx
    ON iam_signup_intents (state, verification_expires_at, materialization_lease_expires_at, created_at)
    WHERE state IN ('pending_verification', 'failed_retryable');

CREATE UNIQUE INDEX IF NOT EXISTS iam_signup_intents_org_id_idx
    ON iam_signup_intents (org_id)
    WHERE org_id <> '';
