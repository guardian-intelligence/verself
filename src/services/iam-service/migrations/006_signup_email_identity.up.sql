DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'iam_signup_intents'
          AND column_name IN ('email', 'email_hash')
    ) THEN
        TRUNCATE TABLE iam_signup_intents;
    END IF;
END $$;

ALTER TABLE IF EXISTS iam_signup_intents
    ADD COLUMN IF NOT EXISTS email_delivery TEXT,
    ADD COLUMN IF NOT EXISTS email_identity_hash BYTEA,
    ADD COLUMN IF NOT EXISTS email_identity_hash_key_id TEXT;

DROP INDEX IF EXISTS iam_signup_intents_email_hash_idx;

ALTER TABLE iam_signup_intents
    DROP COLUMN IF EXISTS email,
    DROP COLUMN IF EXISTS email_hash;

ALTER TABLE iam_signup_intents
    ALTER COLUMN email_delivery SET NOT NULL,
    ALTER COLUMN email_identity_hash SET NOT NULL,
    ALTER COLUMN email_identity_hash_key_id SET NOT NULL;

ALTER TABLE iam_signup_intents
    DROP CONSTRAINT IF EXISTS iam_signup_intents_email_delivery_nonempty,
    DROP CONSTRAINT IF EXISTS iam_signup_intents_email_identity_hash_len,
    DROP CONSTRAINT IF EXISTS iam_signup_intents_email_identity_hash_key_nonempty;

ALTER TABLE iam_signup_intents
    ADD CONSTRAINT iam_signup_intents_email_delivery_nonempty CHECK (email_delivery <> ''),
    ADD CONSTRAINT iam_signup_intents_email_identity_hash_len CHECK (octet_length(email_identity_hash) = 32),
    ADD CONSTRAINT iam_signup_intents_email_identity_hash_key_nonempty CHECK (email_identity_hash_key_id <> '');

CREATE UNIQUE INDEX IF NOT EXISTS iam_signup_intents_email_identity_hash_idx
    ON iam_signup_intents (email_identity_hash);
