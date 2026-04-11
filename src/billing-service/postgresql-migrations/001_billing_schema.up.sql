CREATE TABLE orgs (
    org_id              TEXT        PRIMARY KEY,
    display_name        TEXT        NOT NULL,
    stripe_customer_id  TEXT        NOT NULL DEFAULT '',
    billing_email       TEXT        NOT NULL DEFAULT '',
    trust_tier          TEXT        NOT NULL CHECK (trust_tier IN ('new', 'established', 'enterprise', 'platform')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE products (
    product_id       TEXT        PRIMARY KEY,
    display_name     TEXT        NOT NULL,
    meter_unit       TEXT        NOT NULL,
    billing_model    TEXT        NOT NULL CHECK (billing_model IN ('metered', 'licensed', 'one_time')),
    reserve_policy   JSONB       NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE plans (
    plan_id                  TEXT        PRIMARY KEY,
    product_id               TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    display_name             TEXT        NOT NULL,
    billing_mode             TEXT        NOT NULL CHECK (billing_mode IN ('prepaid', 'postpaid')),
    included_credits         BIGINT,
    unit_rates               JSONB       NOT NULL,
    overage_unit_rates       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    quotas                   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    is_default               BOOLEAN     NOT NULL DEFAULT false,
    tier                     TEXT        NOT NULL DEFAULT 'default',
    active                   BOOLEAN     NOT NULL DEFAULT true,
    stripe_price_id_monthly  TEXT        NOT NULL DEFAULT '',
    stripe_price_id_annual   TEXT        NOT NULL DEFAULT '',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_plans_default_per_product
    ON plans (product_id)
    WHERE is_default AND active;

CREATE TABLE subscriptions (
    subscription_id            BIGSERIAL    PRIMARY KEY,
    org_id                     TEXT         NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id                 TEXT         NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    plan_id                    TEXT         NOT NULL REFERENCES plans(plan_id),
    cadence                    TEXT         NOT NULL DEFAULT 'monthly' CHECK (cadence IN ('monthly', 'annual')),
    status                     TEXT         NOT NULL DEFAULT 'active',
    stripe_subscription_id     TEXT         NOT NULL DEFAULT '',
    stripe_checkout_session_id TEXT         NOT NULL DEFAULT '',
    current_period_start       TIMESTAMPTZ,
    current_period_end         TIMESTAMPTZ,
    created_at                 TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at                 TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_subscriptions_org_product
    ON subscriptions (org_id, product_id, status, current_period_end DESC);

CREATE TABLE credit_grants (
    grant_id             TEXT        PRIMARY KEY,
    org_id               TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id           TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    amount               BIGINT      NOT NULL CHECK (amount >= 0),
    source               TEXT        NOT NULL CHECK (source IN ('free_tier', 'subscription', 'purchase', 'promo', 'refund')),
    contract_id          TEXT        NOT NULL DEFAULT '',
    stripe_reference_id  TEXT        NOT NULL DEFAULT '',
    expires_at           TIMESTAMPTZ,
    closed_at            TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_credit_grants_open
    ON credit_grants (org_id, product_id, expires_at, grant_id)
    WHERE closed_at IS NULL;

CREATE UNIQUE INDEX idx_credit_grants_stripe_reference
    ON credit_grants (org_id, product_id, stripe_reference_id)
    WHERE stripe_reference_id <> '';

CREATE TABLE billing_windows (
    window_id               TEXT        PRIMARY KEY,
    org_id                  TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    actor_id                TEXT        NOT NULL,
    product_id              TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    plan_id                 TEXT        NOT NULL DEFAULT '',
    source_type             TEXT        NOT NULL,
    source_ref              TEXT        NOT NULL,
    window_seq              BIGINT      NOT NULL CHECK (window_seq >= 0),
    state                   TEXT        NOT NULL CHECK (state IN ('reserving', 'reserved', 'settled', 'voided', 'denied', 'failed')),
    reservation_shape       TEXT        NOT NULL CHECK (reservation_shape IN ('time', 'units')),
    reserved_quantity       BIGINT      NOT NULL CHECK (reserved_quantity >= 0),
    actual_quantity         BIGINT      NOT NULL DEFAULT 0 CHECK (actual_quantity >= 0),
    billable_quantity       BIGINT      NOT NULL DEFAULT 0 CHECK (billable_quantity >= 0),
    writeoff_quantity       BIGINT      NOT NULL DEFAULT 0 CHECK (writeoff_quantity >= 0),
    reserved_charge_units   BIGINT      NOT NULL CHECK (reserved_charge_units >= 0),
    billed_charge_units     BIGINT      NOT NULL DEFAULT 0 CHECK (billed_charge_units >= 0),
    writeoff_charge_units   BIGINT      NOT NULL DEFAULT 0 CHECK (writeoff_charge_units >= 0),
    pricing_phase           TEXT        NOT NULL,
    allocation              JSONB       NOT NULL,
    rate_context            JSONB       NOT NULL,
    usage_summary           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    funding_legs            JSONB       NOT NULL DEFAULT '[]'::jsonb,
    window_start            TIMESTAMPTZ NOT NULL,
    activated_at            TIMESTAMPTZ,
    expires_at              TIMESTAMPTZ NOT NULL,
    renew_by                TIMESTAMPTZ,
    settled_at              TIMESTAMPTZ,
    metering_projected_at   TIMESTAMPTZ,
    last_projection_error   TEXT        NOT NULL DEFAULT '',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_billing_windows_source_seq
    ON billing_windows (product_id, source_type, source_ref, window_seq);

CREATE INDEX idx_billing_windows_projection_pending
    ON billing_windows (state, metering_projected_at, created_at)
    WHERE state = 'settled' AND metering_projected_at IS NULL;
