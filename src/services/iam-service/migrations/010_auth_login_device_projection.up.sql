ALTER TABLE iam_browser_login_transactions
    ADD COLUMN provider_session_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN provider_session_token_ciphertext TEXT NOT NULL DEFAULT '';

ALTER TABLE iam_browser_accounts
    ADD COLUMN provider_session_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN provider_session_token_ciphertext TEXT NOT NULL DEFAULT '';

CREATE TABLE iam_device_login_intents (
    device_login_handle TEXT PRIMARY KEY,
    user_code_hash BYTEA NOT NULL UNIQUE,
    user_code_suffix TEXT NOT NULL DEFAULT '',
    provider_device_authorization_id TEXT NOT NULL,
    client_id TEXT NOT NULL DEFAULT '',
    app_name TEXT NOT NULL DEFAULT '',
    project_name TEXT NOT NULL DEFAULT '',
    scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
    state TEXT NOT NULL DEFAULT 'pending',
    required_subject TEXT NOT NULL DEFAULT '',
    required_email TEXT NOT NULL DEFAULT '',
    required_org_id TEXT NOT NULL DEFAULT '',
    approved_by_account_handle TEXT NOT NULL DEFAULT '',
    approved_by_subject TEXT NOT NULL DEFAULT '',
    approved_by_email TEXT NOT NULL DEFAULT '',
    denied_by_account_handle TEXT NOT NULL DEFAULT '',
    denied_by_subject TEXT NOT NULL DEFAULT '',
    denied_by_email TEXT NOT NULL DEFAULT '',
    approved_at TIMESTAMPTZ NOT NULL DEFAULT 'epoch',
    denied_at TIMESTAMPTZ NOT NULL DEFAULT 'epoch',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (device_login_handle ~ '^dlr_[A-Za-z0-9_-]{24,64}$'),
    CHECK (octet_length(user_code_hash) = 32),
    CHECK (length(btrim(provider_device_authorization_id)) > 0),
    CHECK (jsonb_typeof(scopes) = 'array'),
    CHECK (state IN ('pending', 'approved', 'denied', 'expired')),
    CHECK (expires_at > created_at)
);

CREATE INDEX iam_device_login_intents_state_expires_idx
    ON iam_device_login_intents (state, expires_at, updated_at DESC);

CREATE TABLE iam_web_device_login_requests (
    device_login_handle TEXT PRIMARY KEY,
    user_code_suffix TEXT NOT NULL DEFAULT '',
    client_id TEXT NOT NULL DEFAULT '',
    app_name TEXT NOT NULL DEFAULT '',
    project_name TEXT NOT NULL DEFAULT '',
    scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
    state TEXT NOT NULL DEFAULT 'pending',
    approved_at TIMESTAMPTZ NOT NULL DEFAULT 'epoch',
    denied_at TIMESTAMPTZ NOT NULL DEFAULT 'epoch',
    expires_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (device_login_handle ~ '^dlr_[A-Za-z0-9_-]{24,64}$'),
    CHECK (jsonb_typeof(scopes) = 'array'),
    CHECK (state IN ('pending', 'approved', 'denied', 'expired'))
);

CREATE OR REPLACE FUNCTION iam_refresh_web_device_login_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        DELETE FROM iam_web_device_login_requests
        WHERE device_login_handle = OLD.device_login_handle;
        RETURN OLD;
    END IF;

    INSERT INTO iam_web_device_login_requests (
        device_login_handle,
        user_code_suffix,
        client_id,
        app_name,
        project_name,
        scopes,
        state,
        approved_at,
        denied_at,
        expires_at,
        updated_at
    ) VALUES (
        NEW.device_login_handle,
        NEW.user_code_suffix,
        NEW.client_id,
        NEW.app_name,
        NEW.project_name,
        NEW.scopes,
        CASE
            WHEN NEW.state = 'pending' AND NEW.expires_at <= now() THEN 'expired'
            ELSE NEW.state
        END,
        NEW.approved_at,
        NEW.denied_at,
        NEW.expires_at,
        NEW.updated_at
    )
    ON CONFLICT (device_login_handle) DO UPDATE SET
        user_code_suffix = EXCLUDED.user_code_suffix,
        client_id = EXCLUDED.client_id,
        app_name = EXCLUDED.app_name,
        project_name = EXCLUDED.project_name,
        scopes = EXCLUDED.scopes,
        state = EXCLUDED.state,
        approved_at = EXCLUDED.approved_at,
        denied_at = EXCLUDED.denied_at,
        expires_at = EXCLUDED.expires_at,
        updated_at = EXCLUDED.updated_at;

    RETURN NEW;
END;
$$;

CREATE TRIGGER iam_device_login_intents_projection_trg
AFTER INSERT OR UPDATE OR DELETE ON iam_device_login_intents
FOR EACH ROW EXECUTE FUNCTION iam_refresh_web_device_login_projection();

ALTER TABLE iam_device_login_intents OWNER TO iam_service;
ALTER TABLE iam_web_device_login_requests OWNER TO iam_service;
ALTER FUNCTION iam_refresh_web_device_login_projection() OWNER TO iam_service;

GRANT ALL PRIVILEGES ON TABLE iam_device_login_intents TO iam_service;
GRANT ALL PRIVILEGES ON TABLE iam_web_device_login_requests TO iam_service;
