ALTER TABLE iam_accounts
    DROP CONSTRAINT IF EXISTS iam_accounts_state_check;

ALTER TABLE iam_accounts
    ADD CONSTRAINT iam_accounts_state_check CHECK (state IN ('provisioning', 'active', 'disabled'));

ALTER TABLE iam_signup_intents
    ADD COLUMN IF NOT EXISTS account_id TEXT NOT NULL DEFAULT '';

ALTER TABLE iam_signup_intents
    DROP CONSTRAINT IF EXISTS iam_signup_intents_account_id_format;

ALTER TABLE iam_signup_intents
    ADD CONSTRAINT iam_signup_intents_account_id_format
    CHECK (account_id = '' OR account_id ~ '^acct_[0-9A-HJKMNP-TV-Z]{26}$');

DROP INDEX IF EXISTS iam_account_emails_identity_idx;

CREATE TEMP TABLE completed_signup_account_projection ON COMMIT DROP AS
SELECT
    signup_intent_id,
    'acct_' || upper(substr(md5('signup-account:' || signup_intent_id) || md5('signup-account-2:' || signup_intent_id), 1, 26)) AS account_id,
    'conn_' || upper(substr(md5('signup-connection:' || signup_intent_id) || md5('signup-connection-2:' || signup_intent_id), 1, 26)) AS connection_id,
    'https://verself.sh' AS issuer,
    identity_provider_user_id AS subject,
    email_delivery AS email,
    split_part(lower(split_part(email_delivery, '@', 1)), '+', 1) || '@' || lower(split_part(email_delivery, '@', 2)) AS email_identity_key,
    organization_display_name AS display_name,
    created_at,
    COALESCE(completed_at, updated_at, created_at) AS completed_at,
    row_number() OVER (
        PARTITION BY split_part(lower(split_part(email_delivery, '@', 1)), '+', 1) || '@' || lower(split_part(email_delivery, '@', 2))
        ORDER BY COALESCE(completed_at, updated_at, created_at) DESC, created_at DESC, signup_intent_id DESC
    ) AS email_rank
FROM iam_signup_intents
WHERE state = 'completed'
  AND identity_provider_user_id <> ''
  AND email_delivery <> '';

INSERT INTO iam_accounts (
    account_id,
    state,
    primary_email,
    display_name,
    created_at,
    updated_at
)
SELECT
    account_id,
    'active',
    email,
    display_name,
    created_at,
    completed_at
FROM completed_signup_account_projection
WHERE email_rank = 1
ON CONFLICT (account_id) DO UPDATE SET
    state = 'active',
    primary_email = CASE WHEN iam_accounts.primary_email = '' THEN EXCLUDED.primary_email ELSE iam_accounts.primary_email END,
    display_name = CASE WHEN iam_accounts.display_name = '' THEN EXCLUDED.display_name ELSE iam_accounts.display_name END,
    updated_at = now();

INSERT INTO iam_account_emails (
    account_id,
    email,
    email_identity_key,
    verified,
    source,
    first_seen_at,
    last_seen_at
)
SELECT
    account_id,
    email,
    email_identity_key,
    true,
    'signup',
    created_at,
    completed_at
FROM completed_signup_account_projection
WHERE email_rank = 1
ON CONFLICT (account_id, email_identity_key) DO UPDATE SET
    email = EXCLUDED.email,
    verified = true,
    source = 'signup',
    last_seen_at = EXCLUDED.last_seen_at;

INSERT INTO iam_account_subjects (
    issuer,
    subject,
    account_id,
    state,
    email,
    email_verified,
    display_name,
    claims,
    created_at,
    last_seen_at,
    updated_at
)
SELECT
    issuer,
    subject,
    account_id,
    'linked',
    email,
    true,
    display_name,
    '{}'::jsonb,
    created_at,
    completed_at,
    completed_at
FROM completed_signup_account_projection
WHERE email_rank = 1
ON CONFLICT (issuer, subject) DO UPDATE SET
    state = 'linked',
    email = EXCLUDED.email,
    email_verified = true,
    display_name = EXCLUDED.display_name,
    last_seen_at = EXCLUDED.last_seen_at,
    updated_at = now()
WHERE iam_account_subjects.account_id = EXCLUDED.account_id;

INSERT INTO iam_account_connections (
    connection_id,
    account_id,
    issuer,
    subject,
    state,
    email,
    email_verified,
    created_at,
    last_seen_at,
    updated_at
)
SELECT
    connection_id,
    account_id,
    issuer,
    subject,
    'linked',
    email,
    true,
    created_at,
    completed_at,
    completed_at
FROM completed_signup_account_projection
WHERE email_rank = 1
ON CONFLICT (issuer, subject) DO UPDATE SET
    state = 'linked',
    email = EXCLUDED.email,
    email_verified = true,
    last_seen_at = EXCLUDED.last_seen_at,
    updated_at = now()
WHERE iam_account_connections.account_id = EXCLUDED.account_id;

UPDATE iam_signup_intents
SET account_id = completed_signup_accounts.account_id,
    updated_at = now()
FROM completed_signup_account_projection AS completed_signup_accounts
WHERE iam_signup_intents.signup_intent_id = completed_signup_accounts.signup_intent_id
  AND completed_signup_accounts.email_rank = 1
  AND iam_signup_intents.account_id = '';

CREATE UNIQUE INDEX IF NOT EXISTS iam_account_emails_identity_idx
    ON iam_account_emails (email_identity_key);

CREATE INDEX IF NOT EXISTS iam_account_emails_account_seen_idx
    ON iam_account_emails (account_id, last_seen_at DESC);
