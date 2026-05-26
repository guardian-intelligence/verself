CREATE TABLE iam_accounts (
    account_id TEXT PRIMARY KEY,
    state TEXT NOT NULL DEFAULT 'active',
    primary_email TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (account_id ~ '^acct_[0-9A-HJKMNP-TV-Z]{26}$'),
    CHECK (state IN ('active', 'disabled')),
    CHECK (length(btrim(display_name)) <= 256)
);

CREATE INDEX iam_accounts_state_updated_idx
    ON iam_accounts (state, updated_at DESC, account_id);

CREATE TABLE iam_account_subjects (
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    account_id TEXT NOT NULL REFERENCES iam_accounts (account_id) ON DELETE RESTRICT,
    state TEXT NOT NULL DEFAULT 'linked',
    email TEXT NOT NULL DEFAULT '',
    email_verified BOOLEAN NOT NULL DEFAULT false,
    display_name TEXT NOT NULL DEFAULT '',
    claims JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issuer, subject),
    CHECK (length(btrim(issuer)) > 0),
    CHECK (length(btrim(subject)) > 0),
    CHECK (state IN ('linked', 'removed')),
    CHECK (jsonb_typeof(claims) = 'object')
);

CREATE INDEX iam_account_subjects_account_idx
    ON iam_account_subjects (account_id, state, last_seen_at DESC);

CREATE TABLE iam_account_emails (
    account_id TEXT NOT NULL REFERENCES iam_accounts (account_id) ON DELETE RESTRICT,
    email TEXT NOT NULL,
    email_identity_key TEXT NOT NULL,
    verified BOOLEAN NOT NULL DEFAULT false,
    source TEXT NOT NULL DEFAULT 'oidc',
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, email_identity_key),
    CHECK (length(btrim(email)) > 0),
    CHECK (length(btrim(email_identity_key)) > 0),
    CHECK (length(btrim(source)) > 0)
);

CREATE INDEX iam_account_emails_identity_idx
    ON iam_account_emails (email_identity_key, verified, account_id);

CREATE TABLE iam_account_connections (
    connection_id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES iam_accounts (account_id) ON DELETE RESTRICT,
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'linked',
    email TEXT NOT NULL DEFAULT '',
    email_verified BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (issuer, subject),
    CHECK (connection_id ~ '^conn_[0-9A-HJKMNP-TV-Z]{26}$'),
    CHECK (length(btrim(issuer)) > 0),
    CHECK (length(btrim(subject)) > 0),
    CHECK (state IN ('linked', 'removed'))
);

CREATE INDEX iam_account_connections_account_idx
    ON iam_account_connections (account_id, state, last_seen_at DESC);

CREATE TABLE iam_device_sessions (
    session_id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES iam_accounts (account_id) ON DELETE RESTRICT,
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    channel TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'active',
    device_label TEXT NOT NULL DEFAULT '',
    credential_fingerprint TEXT NOT NULL DEFAULT '',
    provider_session_id TEXT NOT NULL DEFAULT '',
    auth_time TIMESTAMPTZ NOT NULL DEFAULT 'epoch',
    auth_methods TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ NOT NULL DEFAULT 'epoch',
    revoked_reason TEXT NOT NULL DEFAULT '',
    created_client_ip TEXT NOT NULL DEFAULT '',
    created_client_ip_trusted BOOLEAN NOT NULL DEFAULT false,
    created_client_ip_source TEXT NOT NULL DEFAULT '',
    created_user_agent TEXT NOT NULL DEFAULT '',
    last_seen_client_ip TEXT NOT NULL DEFAULT '',
    last_seen_client_ip_trusted BOOLEAN NOT NULL DEFAULT false,
    last_seen_client_ip_source TEXT NOT NULL DEFAULT '',
    last_seen_user_agent TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (session_id ~ '^sess_[0-9A-HJKMNP-TV-Z]{26}$'),
    CHECK (length(btrim(account_id)) > 0),
    CHECK (length(btrim(issuer)) > 0),
    CHECK (length(btrim(subject)) > 0),
    CHECK (channel IN ('browser', 'cli', 'sdk', 'mobile', 'workload')),
    CHECK (state IN ('active', 'revoked', 'expired')),
    CHECK (expires_at > created_at)
);

CREATE INDEX iam_device_sessions_account_state_idx
    ON iam_device_sessions (account_id, state, last_seen_at DESC, session_id);

CREATE INDEX iam_device_sessions_subject_state_idx
    ON iam_device_sessions (issuer, subject, state, last_seen_at DESC);

ALTER TABLE iam_accounts OWNER TO iam_service;
ALTER TABLE iam_account_subjects OWNER TO iam_service;
ALTER TABLE iam_account_emails OWNER TO iam_service;
ALTER TABLE iam_account_connections OWNER TO iam_service;
ALTER TABLE iam_device_sessions OWNER TO iam_service;

GRANT ALL PRIVILEGES ON TABLE iam_accounts TO iam_service;
GRANT ALL PRIVILEGES ON TABLE iam_account_subjects TO iam_service;
GRANT ALL PRIVILEGES ON TABLE iam_account_emails TO iam_service;
GRANT ALL PRIVILEGES ON TABLE iam_account_connections TO iam_service;
GRANT ALL PRIVILEGES ON TABLE iam_device_sessions TO iam_service;
