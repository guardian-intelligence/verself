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

CREATE TABLE credit_buckets (
    bucket_id     TEXT        PRIMARY KEY,
    display_name  TEXT        NOT NULL,
    sort_order    INTEGER     NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE skus (
    sku_id         TEXT        PRIMARY KEY,
    product_id     TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    bucket_id      TEXT        NOT NULL REFERENCES credit_buckets(bucket_id),
    display_name   TEXT        NOT NULL,
    quantity_unit  TEXT        NOT NULL,
    active         BOOLEAN     NOT NULL DEFAULT true,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_skus_product_active
    ON skus (product_id, active, bucket_id, sku_id);

CREATE TABLE plans (
    plan_id                  TEXT        PRIMARY KEY,
    product_id               TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    display_name             TEXT        NOT NULL,
    billing_mode             TEXT        NOT NULL CHECK (billing_mode IN ('prepaid', 'postpaid')),
    quotas                   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    is_default               BOOLEAN     NOT NULL DEFAULT false,
    tier                     TEXT        NOT NULL DEFAULT 'default',
    active                   BOOLEAN     NOT NULL DEFAULT true,
    stripe_price_id_monthly  TEXT        NOT NULL DEFAULT '',
    stripe_price_id_annual   TEXT        NOT NULL DEFAULT '',
    monthly_amount_cents     BIGINT      NOT NULL DEFAULT 0 CHECK (monthly_amount_cents >= 0),
    annual_amount_cents      BIGINT      NOT NULL DEFAULT 0 CHECK (annual_amount_cents >= 0),
    currency                 TEXT        NOT NULL DEFAULT 'usd',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_plans_default_per_product
    ON plans (product_id)
    WHERE is_default AND active;

CREATE TABLE plan_sku_rates (
    plan_id       TEXT        NOT NULL REFERENCES plans(plan_id) ON DELETE CASCADE,
    sku_id        TEXT        NOT NULL REFERENCES skus(sku_id),
    unit_rate     BIGINT      NOT NULL CHECK (unit_rate >= 0),
    active        BOOLEAN     NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (plan_id, sku_id)
);

CREATE INDEX idx_plan_sku_rates_active
    ON plan_sku_rates (plan_id, active, sku_id);

CREATE TABLE entitlement_policies (
    policy_id         TEXT        PRIMARY KEY,
    source            TEXT        NOT NULL CHECK (source IN ('free_tier', 'subscription', 'purchase', 'promo', 'refund')),
    product_id        TEXT        NOT NULL DEFAULT '',
    scope_type        TEXT        NOT NULL CHECK (scope_type IN ('sku', 'bucket', 'product', 'account')),
    scope_product_id  TEXT        NOT NULL DEFAULT '',
    scope_bucket_id   TEXT        NOT NULL DEFAULT '',
    scope_sku_id      TEXT        NOT NULL DEFAULT '',
    amount_units      BIGINT      NOT NULL CHECK (amount_units >= 0),
    cadence           TEXT        NOT NULL CHECK (cadence IN ('monthly', 'annual')),
    anchor_kind       TEXT        NOT NULL CHECK (anchor_kind IN ('calendar_month', 'subscription_period')),
    proration_mode    TEXT        NOT NULL CHECK (proration_mode IN ('none', 'prorate_by_time_left')),
    policy_version    TEXT        NOT NULL DEFAULT 'v1',
    active_from       TIMESTAMPTZ NOT NULL DEFAULT now(),
    active_until      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (scope_type = 'sku' AND scope_product_id <> '' AND scope_bucket_id <> '' AND scope_sku_id <> '')
        OR (scope_type = 'bucket' AND scope_product_id <> '' AND scope_bucket_id <> '' AND scope_sku_id = '')
        OR (scope_type = 'product' AND scope_product_id <> '' AND scope_bucket_id = '' AND scope_sku_id = '')
        OR (scope_type = 'account' AND scope_product_id = '' AND scope_bucket_id = '' AND scope_sku_id = '')
    )
);

CREATE INDEX idx_entitlement_policies_active
    ON entitlement_policies (source, product_id, active_from, active_until, policy_id);

CREATE TABLE plan_entitlements (
    plan_id     TEXT        NOT NULL REFERENCES plans(plan_id) ON DELETE CASCADE,
    policy_id   TEXT        NOT NULL REFERENCES entitlement_policies(policy_id) ON DELETE RESTRICT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (plan_id, policy_id)
);

CREATE TABLE subscription_contracts (
    subscription_id             BIGSERIAL    PRIMARY KEY,
    contract_id                 TEXT         NOT NULL DEFAULT '',
    org_id                      TEXT         NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id                  TEXT         NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    plan_id                     TEXT         NOT NULL REFERENCES plans(plan_id),
    cadence                     TEXT         NOT NULL DEFAULT 'monthly' CHECK (cadence IN ('monthly', 'annual')),
    status                      TEXT         NOT NULL DEFAULT 'active',
    payment_state               TEXT         NOT NULL DEFAULT 'pending' CHECK (payment_state IN ('not_required', 'pending', 'paid', 'failed', 'uncollectible', 'refunded')),
    entitlement_state           TEXT         NOT NULL DEFAULT 'grace' CHECK (entitlement_state IN ('scheduled', 'active', 'grace', 'closed', 'voided')),
    billing_anchor              TIMESTAMPTZ,
    grace_until                 TIMESTAMPTZ,
    current_period_start        TIMESTAMPTZ,
    current_period_end          TIMESTAMPTZ,
    stripe_subscription_id      TEXT         NOT NULL DEFAULT '',
    stripe_checkout_session_id  TEXT         NOT NULL DEFAULT '',
    created_at                  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_subscription_contracts_contract
    ON subscription_contracts (contract_id)
    WHERE contract_id <> '';

CREATE INDEX idx_subscription_contracts_org_product
    ON subscription_contracts (org_id, product_id, status, current_period_end DESC);

CREATE UNIQUE INDEX idx_subscription_contracts_stripe_subscription
    ON subscription_contracts (stripe_subscription_id)
    WHERE stripe_subscription_id <> '';

CREATE UNIQUE INDEX idx_subscription_contracts_stripe_checkout_session
    ON subscription_contracts (stripe_checkout_session_id)
    WHERE stripe_checkout_session_id <> '';

CREATE TABLE entitlement_periods (
    period_id            TEXT        PRIMARY KEY,
    org_id               TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id           TEXT        NOT NULL DEFAULT '',
    source               TEXT        NOT NULL CHECK (source IN ('free_tier', 'subscription', 'purchase', 'promo', 'refund')),
    policy_id            TEXT        NOT NULL DEFAULT '',
    contract_id          TEXT        NOT NULL DEFAULT '',
    scope_type           TEXT        NOT NULL CHECK (scope_type IN ('sku', 'bucket', 'product', 'account')),
    scope_product_id     TEXT        NOT NULL DEFAULT '',
    scope_bucket_id      TEXT        NOT NULL DEFAULT '',
    scope_sku_id         TEXT        NOT NULL DEFAULT '',
    amount_units         BIGINT      NOT NULL CHECK (amount_units >= 0),
    period_start         TIMESTAMPTZ NOT NULL,
    period_end           TIMESTAMPTZ NOT NULL,
    policy_version       TEXT        NOT NULL DEFAULT 'v1',
    payment_state        TEXT        NOT NULL DEFAULT 'not_required' CHECK (payment_state IN ('not_required', 'pending', 'paid', 'failed', 'uncollectible', 'refunded')),
    entitlement_state    TEXT        NOT NULL DEFAULT 'active' CHECK (entitlement_state IN ('scheduled', 'active', 'grace', 'closed', 'voided')),
    source_reference_id  TEXT        NOT NULL,
    created_reason       TEXT        NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (period_end > period_start),
    CHECK (
        (scope_type = 'sku' AND scope_product_id <> '' AND scope_bucket_id <> '' AND scope_sku_id <> '')
        OR (scope_type = 'bucket' AND scope_product_id <> '' AND scope_bucket_id <> '' AND scope_sku_id = '')
        OR (scope_type = 'product' AND scope_product_id <> '' AND scope_bucket_id = '' AND scope_sku_id = '')
        OR (scope_type = 'account' AND scope_product_id = '' AND scope_bucket_id = '' AND scope_sku_id = '')
    )
);

CREATE UNIQUE INDEX idx_entitlement_periods_source_reference
    ON entitlement_periods (org_id, source, source_reference_id);

CREATE INDEX idx_entitlement_periods_active
    ON entitlement_periods (org_id, product_id, source, period_start, period_end, entitlement_state);

CREATE TABLE credit_grants (
    grant_id                TEXT        PRIMARY KEY,
    org_id                  TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    scope_type              TEXT        NOT NULL CHECK (scope_type IN ('sku', 'bucket', 'product', 'account')),
    scope_product_id        TEXT        NOT NULL DEFAULT '',
    scope_bucket_id         TEXT        NOT NULL DEFAULT '',
    scope_sku_id            TEXT        NOT NULL DEFAULT '',
    amount                  BIGINT      NOT NULL CHECK (amount >= 0),
    source                  TEXT        NOT NULL CHECK (source IN ('free_tier', 'subscription', 'purchase', 'promo', 'refund')),
    source_reference_id     TEXT        NOT NULL DEFAULT '',
    entitlement_period_id   TEXT        NOT NULL DEFAULT '',
    policy_version          TEXT        NOT NULL DEFAULT '',
    starts_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    period_start            TIMESTAMPTZ,
    period_end              TIMESTAMPTZ,
    expires_at              TIMESTAMPTZ,
    closed_at               TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (scope_type = 'sku' AND scope_product_id <> '' AND scope_bucket_id <> '' AND scope_sku_id <> '')
        OR (scope_type = 'bucket' AND scope_product_id <> '' AND scope_bucket_id <> '' AND scope_sku_id = '')
        OR (scope_type = 'product' AND scope_product_id <> '' AND scope_bucket_id = '' AND scope_sku_id = '')
        OR (scope_type = 'account' AND scope_product_id = '' AND scope_bucket_id = '' AND scope_sku_id = '')
    ),
    CHECK (expires_at IS NULL OR expires_at > starts_at),
    CHECK (period_start IS NULL OR period_end IS NOT NULL),
    CHECK (period_end IS NULL OR period_start IS NOT NULL),
    CHECK (period_start IS NULL OR period_end > period_start)
);

CREATE INDEX idx_credit_grants_open
    ON credit_grants (org_id, source, scope_type, scope_product_id, scope_bucket_id, scope_sku_id, starts_at, expires_at, grant_id)
    WHERE closed_at IS NULL;

CREATE UNIQUE INDEX idx_credit_grants_source_reference
    ON credit_grants (org_id, source, scope_type, scope_product_id, scope_bucket_id, scope_sku_id, source_reference_id)
    WHERE source_reference_id <> '';

CREATE TABLE billing_outbox_events (
    event_id        TEXT        PRIMARY KEY,
    event_type      TEXT        NOT NULL,
    aggregate_type  TEXT        NOT NULL,
    aggregate_id    TEXT        NOT NULL,
    org_id          TEXT        NOT NULL,
    product_id      TEXT        NOT NULL DEFAULT '',
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    payload         JSONB       NOT NULL,
    delivered_at    TIMESTAMPTZ,
    delivery_error  TEXT        NOT NULL DEFAULT '',
    attempts        INTEGER     NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_billing_outbox_pending
    ON billing_outbox_events (occurred_at, event_id)
    WHERE delivered_at IS NULL;

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
