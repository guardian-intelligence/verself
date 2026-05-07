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

INSERT INTO iam_service_accounts (
    service_account_id, org_id, subject_id, client_id, display_name, description,
    status, created_at, created_by, updated_at, disabled_at, disabled_by, last_used_at
)
SELECT
    credential_id, org_id, subject_id, client_id, display_name, '',
    CASE WHEN status = 'revoked' THEN 'disabled' ELSE 'active' END,
    created_at, created_by, updated_at, revoked_at, revoked_by, last_used_at
FROM iam_api_credentials
ON CONFLICT (service_account_id) DO NOTHING;

ALTER TABLE iam_api_credentials
    ADD COLUMN IF NOT EXISTS service_account_id TEXT;

UPDATE iam_api_credentials
SET service_account_id = credential_id
WHERE service_account_id IS NULL OR btrim(service_account_id) = '';

ALTER TABLE iam_api_credentials
    ALTER COLUMN service_account_id SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'iam_api_credentials_service_account_id_fkey'
    ) THEN
        ALTER TABLE iam_api_credentials
            ADD CONSTRAINT iam_api_credentials_service_account_id_fkey
            FOREIGN KEY (service_account_id)
            REFERENCES iam_service_accounts (service_account_id)
            ON DELETE CASCADE;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS iam_service_accounts_org_status_idx
    ON iam_service_accounts (org_id, status, created_at DESC, service_account_id DESC);

CREATE UNIQUE INDEX IF NOT EXISTS iam_service_accounts_subject_idx
    ON iam_service_accounts (subject_id);

CREATE INDEX IF NOT EXISTS iam_api_credentials_service_account_idx
    ON iam_api_credentials (service_account_id, status, created_at DESC, credential_id DESC);

CREATE UNIQUE INDEX IF NOT EXISTS iam_api_credentials_active_subject_idx
    ON iam_api_credentials (subject_id)
    WHERE status = 'active';
