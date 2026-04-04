DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'grant_account') THEN
        CREATE TYPE grant_account AS ENUM ('free_tier', 'credit');
    END IF;
END $$;

ALTER TABLE credit_grants ADD COLUMN account_type grant_account;

UPDATE credit_grants
SET account_type = CASE
    WHEN source = 'free_tier' THEN 'free_tier'::grant_account
    ELSE 'credit'::grant_account
END;

ALTER TABLE credit_grants ALTER COLUMN account_type SET NOT NULL;

ALTER TABLE credit_grants ADD COLUMN consumed BIGINT NOT NULL DEFAULT 0 CHECK (consumed >= 0);

ALTER TABLE credit_grants ADD COLUMN expired BIGINT NOT NULL DEFAULT 0 CHECK (expired >= 0);

ALTER TABLE credit_grants ADD COLUMN remaining BIGINT GENERATED ALWAYS AS (amount - consumed - expired) STORED;

ALTER TABLE credit_grants ADD CONSTRAINT credit_grants_check CHECK (consumed + expired <= amount);

ALTER TABLE credit_grants DROP COLUMN closed_at;

DROP INDEX IF EXISTS idx_credit_grants_active;

CREATE INDEX idx_credit_grants_active
    ON credit_grants (org_id, product_id, expires_at)
    WHERE remaining > 0;

DROP INDEX IF EXISTS idx_credit_grants_subscription_period;

CREATE UNIQUE INDEX idx_credit_grants_subscription_period
    ON credit_grants (subscription_id, period_start, account_type)
    WHERE subscription_id IS NOT NULL;
