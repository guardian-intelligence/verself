CREATE TABLE IF NOT EXISTS iam_web_auth_clients (
    client_handle TEXT PRIMARY KEY,
    auth_state TEXT NOT NULL DEFAULT 'client',
    active_account_handle TEXT NOT NULL DEFAULT '',
    subject TEXT NOT NULL DEFAULT '',
    email TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    preferred_username TEXT NOT NULL DEFAULT '',
    org_id TEXT NOT NULL DEFAULT '',
    home_org_id TEXT NOT NULL DEFAULT '',
    selected_org_id TEXT NOT NULL DEFAULT '',
    available_org_contexts JSONB NOT NULL DEFAULT '[]'::jsonb,
    client_cache_partition TEXT NOT NULL,
    auth_revision BIGINT NOT NULL DEFAULT 0,
    last_auth_event TEXT NOT NULL DEFAULT '',
    client_expires_at TIMESTAMPTZ NOT NULL,
    account_expires_at TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01T00:00:00Z'::timestamptz,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (length(btrim(client_handle)) > 0),
    CHECK (auth_state IN ('client', 'account')),
    CHECK (length(btrim(client_cache_partition)) > 0),
    CHECK (auth_revision >= 0),
    CHECK (jsonb_typeof(available_org_contexts) = 'array')
);

CREATE TABLE IF NOT EXISTS iam_web_auth_accounts (
    client_handle TEXT NOT NULL REFERENCES iam_web_auth_clients (client_handle) ON DELETE CASCADE,
    account_handle TEXT NOT NULL,
    is_current BOOLEAN NOT NULL DEFAULT false,
    subject TEXT NOT NULL,
    email TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    preferred_username TEXT NOT NULL DEFAULT '',
    home_org_id TEXT NOT NULL DEFAULT '',
    selected_org_id TEXT NOT NULL DEFAULT '',
    available_org_contexts JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (client_handle, account_handle),
    CHECK (length(btrim(client_handle)) > 0),
    CHECK (length(btrim(account_handle)) > 0),
    CHECK (length(btrim(subject)) > 0),
    CHECK (jsonb_typeof(available_org_contexts) = 'array')
);

CREATE INDEX IF NOT EXISTS iam_web_auth_accounts_client_seen_idx
    ON iam_web_auth_accounts (client_handle, last_seen_at DESC);

CREATE TABLE IF NOT EXISTS iam_web_auth_account_sessions (
    client_handle TEXT NOT NULL REFERENCES iam_web_auth_clients (client_handle) ON DELETE CASCADE,
    account_handle TEXT NOT NULL,
    session_handle TEXT NOT NULL,
    is_current BOOLEAN NOT NULL DEFAULT false,
    device_label TEXT NOT NULL DEFAULT '',
    device_kind TEXT NOT NULL DEFAULT '',
    browser_name TEXT NOT NULL DEFAULT '',
    os_name TEXT NOT NULL DEFAULT '',
    location_country_code TEXT NOT NULL DEFAULT '',
    location_region TEXT NOT NULL DEFAULT '',
    location_city TEXT NOT NULL DEFAULT '',
    client_ip TEXT NOT NULL DEFAULT '',
    client_ip_source TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (client_handle, session_handle),
    CHECK (length(btrim(client_handle)) > 0),
    CHECK (length(btrim(account_handle)) > 0),
    CHECK (length(btrim(session_handle)) > 0)
);

CREATE INDEX IF NOT EXISTS iam_web_auth_account_sessions_client_seen_idx
    ON iam_web_auth_account_sessions (client_handle, last_seen_at DESC);

CREATE OR REPLACE FUNCTION iam_refresh_web_auth_projection(
    target_client_hash TEXT,
    target_client_handle TEXT,
    auth_event TEXT
) RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    projection_refreshed INTEGER;
BEGIN
    WITH source_client AS (
        SELECT
            c.client_hash,
            c.client_handle,
            COALESCE(c.active_account_handle, '') AS active_account_handle,
            c.client_cache_partition,
            c.expires_at AS client_expires_at
        FROM iam_browser_clients c
        WHERE c.client_hash = target_client_hash
          AND c.expires_at > now()
    ),
    source_active_account AS (
        SELECT a.*
        FROM source_client c
        JOIN iam_browser_accounts a
          ON a.client_hash = c.client_hash
         AND a.account_handle = c.active_account_handle
         AND a.state = 'active'
         AND a.expires_at > now()
    ),
    deleted_client AS (
        DELETE FROM iam_web_auth_clients p
        WHERE p.client_handle = target_client_handle
          AND NOT EXISTS (SELECT 1 FROM source_client)
        RETURNING p.client_handle
    ),
    upsert_client AS (
        INSERT INTO iam_web_auth_clients (
            client_handle,
            auth_state,
            active_account_handle,
            subject,
            email,
            display_name,
            preferred_username,
            org_id,
            home_org_id,
            selected_org_id,
            available_org_contexts,
            client_cache_partition,
            auth_revision,
            last_auth_event,
            client_expires_at,
            account_expires_at,
            updated_at
        )
        SELECT
            c.client_handle,
            CASE WHEN a.account_handle IS NULL THEN 'client' ELSE 'account' END,
            COALESCE(a.account_handle, ''),
            COALESCE(a.subject, ''),
            COALESCE(a.email, ''),
            COALESCE(a.display_name, ''),
            COALESCE(a.preferred_username, ''),
            COALESCE(a.org_id, ''),
            COALESCE(a.home_org_id, ''),
            COALESCE(a.selected_org_id, ''),
            COALESCE(a.available_org_contexts, '[]'::jsonb),
            c.client_cache_partition,
            txid_current(),
            COALESCE(NULLIF(auth_event, ''), 'iam.web_auth_projection.refreshed'),
            c.client_expires_at,
            COALESCE(a.expires_at, '1970-01-01T00:00:00Z'::timestamptz),
            now()
        FROM source_client c
        LEFT JOIN source_active_account a ON true
        ON CONFLICT (client_handle) DO UPDATE SET
            auth_state = EXCLUDED.auth_state,
            active_account_handle = EXCLUDED.active_account_handle,
            subject = EXCLUDED.subject,
            email = EXCLUDED.email,
            display_name = EXCLUDED.display_name,
            preferred_username = EXCLUDED.preferred_username,
            org_id = EXCLUDED.org_id,
            home_org_id = EXCLUDED.home_org_id,
            selected_org_id = EXCLUDED.selected_org_id,
            available_org_contexts = EXCLUDED.available_org_contexts,
            client_cache_partition = EXCLUDED.client_cache_partition,
            auth_revision = EXCLUDED.auth_revision,
            last_auth_event = EXCLUDED.last_auth_event,
            client_expires_at = EXCLUDED.client_expires_at,
            account_expires_at = EXCLUDED.account_expires_at,
            updated_at = EXCLUDED.updated_at
        RETURNING client_handle
    ),
    source_accounts AS (
        SELECT
            c.client_handle,
            c.active_account_handle,
            a.*
        FROM source_client c
        JOIN iam_browser_accounts a
          ON a.client_hash = c.client_hash
         AND a.state = 'active'
         AND a.expires_at > now()
    ),
    upsert_accounts AS (
        INSERT INTO iam_web_auth_accounts (
            client_handle,
            account_handle,
            is_current,
            subject,
            email,
            display_name,
            preferred_username,
            home_org_id,
            selected_org_id,
            available_org_contexts,
            created_at,
            last_seen_at,
            expires_at,
            updated_at
        )
        SELECT
            a.client_handle,
            a.account_handle,
            a.account_handle = a.active_account_handle,
            a.subject,
            COALESCE(a.email, ''),
            COALESCE(a.display_name, ''),
            COALESCE(a.preferred_username, ''),
            COALESCE(a.home_org_id, ''),
            COALESCE(a.selected_org_id, ''),
            a.available_org_contexts,
            a.created_at,
            a.last_seen_at,
            a.expires_at,
            now()
        FROM source_accounts a
        ON CONFLICT (client_handle, account_handle) DO UPDATE SET
            is_current = EXCLUDED.is_current,
            subject = EXCLUDED.subject,
            email = EXCLUDED.email,
            display_name = EXCLUDED.display_name,
            preferred_username = EXCLUDED.preferred_username,
            home_org_id = EXCLUDED.home_org_id,
            selected_org_id = EXCLUDED.selected_org_id,
            available_org_contexts = EXCLUDED.available_org_contexts,
            created_at = EXCLUDED.created_at,
            last_seen_at = EXCLUDED.last_seen_at,
            expires_at = EXCLUDED.expires_at,
            updated_at = EXCLUDED.updated_at
        RETURNING account_handle
    ),
    prune_accounts AS (
        DELETE FROM iam_web_auth_accounts p
        WHERE p.client_handle = target_client_handle
          AND EXISTS (SELECT 1 FROM source_client)
          AND NOT EXISTS (
              SELECT 1
              FROM source_accounts a
              WHERE a.account_handle = p.account_handle
          )
        RETURNING p.account_handle
    ),
    upsert_sessions AS (
        INSERT INTO iam_web_auth_account_sessions (
            client_handle,
            account_handle,
            session_handle,
            is_current,
            device_label,
            device_kind,
            browser_name,
            os_name,
            location_country_code,
            location_region,
            location_city,
            client_ip,
            client_ip_source,
            user_agent,
            created_at,
            last_seen_at,
            expires_at,
            updated_at
        )
        SELECT
            a.client_handle,
            a.account_handle,
            a.account_handle,
            a.account_handle = a.active_account_handle,
            a.last_seen_device_label,
            a.last_seen_device_kind,
            a.last_seen_browser_name,
            a.last_seen_os_name,
            a.last_seen_geo_country_code,
            a.last_seen_geo_region,
            a.last_seen_geo_city,
            a.last_seen_client_ip,
            a.last_seen_client_ip_source,
            a.last_seen_user_agent,
            a.created_at,
            a.last_seen_at,
            a.expires_at,
            now()
        FROM source_accounts a
        ON CONFLICT (client_handle, session_handle) DO UPDATE SET
            account_handle = EXCLUDED.account_handle,
            is_current = EXCLUDED.is_current,
            device_label = EXCLUDED.device_label,
            device_kind = EXCLUDED.device_kind,
            browser_name = EXCLUDED.browser_name,
            os_name = EXCLUDED.os_name,
            location_country_code = EXCLUDED.location_country_code,
            location_region = EXCLUDED.location_region,
            location_city = EXCLUDED.location_city,
            client_ip = EXCLUDED.client_ip,
            client_ip_source = EXCLUDED.client_ip_source,
            user_agent = EXCLUDED.user_agent,
            created_at = EXCLUDED.created_at,
            last_seen_at = EXCLUDED.last_seen_at,
            expires_at = EXCLUDED.expires_at,
            updated_at = EXCLUDED.updated_at
        RETURNING session_handle
    ),
    prune_sessions AS (
        DELETE FROM iam_web_auth_account_sessions p
        WHERE p.client_handle = target_client_handle
          AND EXISTS (SELECT 1 FROM source_client)
          AND NOT EXISTS (
              SELECT 1
              FROM source_accounts a
              WHERE a.account_handle = p.session_handle
          )
        RETURNING p.session_handle
    )
    SELECT 1 INTO projection_refreshed;
END;
$$;

CREATE OR REPLACE FUNCTION iam_web_auth_client_projection_trigger()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM iam_refresh_web_auth_projection(
            OLD.client_hash,
            OLD.client_handle,
            'iam.browser_client.deleted'
        );
        RETURN OLD;
    END IF;

    PERFORM iam_refresh_web_auth_projection(
        NEW.client_hash,
        NEW.client_handle,
        'iam.browser_client.' || lower(TG_OP)
    );
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION iam_web_auth_account_projection_trigger()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_hash TEXT;
    target_handle TEXT;
BEGIN
    target_hash := COALESCE(NEW.client_hash, OLD.client_hash);

    SELECT c.client_handle
      INTO target_handle
      FROM iam_browser_clients c
     WHERE c.client_hash = target_hash;

    IF target_handle = '' OR target_handle IS NULL THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    PERFORM iam_refresh_web_auth_projection(
        target_hash,
        target_handle,
        'iam.browser_account.' || lower(TG_OP)
    );
    RETURN COALESCE(NEW, OLD);
END;
$$;

DROP TRIGGER IF EXISTS iam_web_auth_clients_projection_refresh ON iam_browser_clients;
CREATE TRIGGER iam_web_auth_clients_projection_refresh
AFTER INSERT OR UPDATE OR DELETE ON iam_browser_clients
FOR EACH ROW
EXECUTE FUNCTION iam_web_auth_client_projection_trigger();

DROP TRIGGER IF EXISTS iam_web_auth_accounts_projection_refresh ON iam_browser_accounts;
CREATE TRIGGER iam_web_auth_accounts_projection_refresh
AFTER INSERT OR UPDATE OR DELETE ON iam_browser_accounts
FOR EACH ROW
EXECUTE FUNCTION iam_web_auth_account_projection_trigger();

SELECT iam_refresh_web_auth_projection(
    client_hash,
    client_handle,
    'iam.web_auth_projection.backfill'
)
FROM iam_browser_clients;

ALTER TABLE iam_web_auth_clients OWNER TO iam_service;
ALTER TABLE iam_web_auth_accounts OWNER TO iam_service;
ALTER TABLE iam_web_auth_account_sessions OWNER TO iam_service;
ALTER FUNCTION iam_refresh_web_auth_projection(TEXT, TEXT, TEXT) OWNER TO iam_service;
ALTER FUNCTION iam_web_auth_client_projection_trigger() OWNER TO iam_service;
ALTER FUNCTION iam_web_auth_account_projection_trigger() OWNER TO iam_service;

GRANT ALL PRIVILEGES ON TABLE iam_web_auth_clients TO iam_service;
GRANT ALL PRIVILEGES ON TABLE iam_web_auth_accounts TO iam_service;
GRANT ALL PRIVILEGES ON TABLE iam_web_auth_account_sessions TO iam_service;
