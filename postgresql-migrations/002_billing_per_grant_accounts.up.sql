ALTER TABLE credit_grants DROP COLUMN remaining;

ALTER TABLE credit_grants DROP CONSTRAINT credit_grants_check;

ALTER TABLE credit_grants DROP COLUMN consumed;

ALTER TABLE credit_grants DROP COLUMN expired;

ALTER TABLE credit_grants DROP COLUMN account_type;

ALTER TABLE credit_grants ADD COLUMN closed_at TIMESTAMPTZ;

DROP INDEX IF EXISTS idx_credit_grants_active;

CREATE INDEX idx_credit_grants_active
    ON credit_grants (org_id, product_id, expires_at)
    WHERE closed_at IS NULL;

DROP INDEX IF EXISTS idx_credit_grants_subscription_period;

CREATE UNIQUE INDEX idx_credit_grants_subscription_period
    ON credit_grants (subscription_id, period_start)
    WHERE subscription_id IS NOT NULL;

DROP TYPE IF EXISTS grant_account;
