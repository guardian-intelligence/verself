DROP TABLE IF EXISTS iam_browser_resource_tokens;
DROP TABLE IF EXISTS iam_browser_account_observations;
DROP TABLE IF EXISTS iam_browser_session_observations;
DROP TABLE IF EXISTS iam_browser_accounts;
DROP TABLE IF EXISTS iam_browser_clients;
DROP TABLE IF EXISTS iam_browser_sessions;
DROP TABLE IF EXISTS iam_browser_login_transactions;

CREATE TABLE iam_browser_login_transactions (
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

CREATE INDEX iam_browser_login_transactions_expires_at_idx
    ON iam_browser_login_transactions (expires_at);

CREATE TABLE iam_browser_clients (
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

CREATE UNIQUE INDEX iam_browser_clients_client_handle_idx
    ON iam_browser_clients (client_handle);

CREATE INDEX iam_browser_clients_expires_at_idx
    ON iam_browser_clients (expires_at);

CREATE TABLE iam_browser_accounts (
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

CREATE INDEX iam_browser_accounts_client_idx
    ON iam_browser_accounts (client_hash, last_seen_at DESC);

CREATE UNIQUE INDEX iam_browser_accounts_client_subject_idx
    ON iam_browser_accounts (client_hash, subject);

CREATE INDEX iam_browser_accounts_subject_idx
    ON iam_browser_accounts (subject, last_seen_at DESC);

CREATE INDEX iam_browser_accounts_expires_at_idx
    ON iam_browser_accounts (expires_at);

CREATE TABLE iam_browser_account_observations (
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

CREATE INDEX idx_iam_browser_account_observations_subject_time
    ON iam_browser_account_observations (subject, observed_at DESC);

CREATE INDEX idx_iam_browser_account_observations_account_time
    ON iam_browser_account_observations (account_handle, observed_at DESC);

CREATE TABLE iam_browser_resource_tokens (
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

CREATE INDEX iam_browser_resource_tokens_expires_at_idx
    ON iam_browser_resource_tokens (expires_at);

ALTER TABLE iam_browser_login_transactions OWNER TO iam_service;
ALTER TABLE iam_browser_clients OWNER TO iam_service;
ALTER TABLE iam_browser_accounts OWNER TO iam_service;
ALTER TABLE iam_browser_account_observations OWNER TO iam_service;
ALTER TABLE iam_browser_resource_tokens OWNER TO iam_service;
ALTER SEQUENCE iam_browser_account_observations_observation_id_seq OWNER TO iam_service;

GRANT ALL PRIVILEGES ON TABLE iam_browser_login_transactions TO iam_service;
GRANT ALL PRIVILEGES ON TABLE iam_browser_clients TO iam_service;
GRANT ALL PRIVILEGES ON TABLE iam_browser_accounts TO iam_service;
GRANT ALL PRIVILEGES ON TABLE iam_browser_account_observations TO iam_service;
GRANT ALL PRIVILEGES ON TABLE iam_browser_resource_tokens TO iam_service;
GRANT USAGE, SELECT ON SEQUENCE iam_browser_account_observations_observation_id_seq TO iam_service;
