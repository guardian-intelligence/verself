CREATE TYPE subscription_status AS ENUM ('active', 'past_due', 'suspended', 'cancelled', 'trialing');

CREATE TYPE billing_cadence AS ENUM ('monthly', 'annual');

CREATE TYPE task_status AS ENUM ('pending', 'claimed', 'completed', 'retrying', 'dead');

CREATE TABLE products (
    product_id    TEXT PRIMARY KEY,
    display_name  TEXT NOT NULL,
    meter_unit    TEXT NOT NULL,
    billing_model TEXT NOT NULL CHECK (billing_model IN ('metered', 'licensed', 'one_time')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE plans (
    plan_id                 TEXT PRIMARY KEY,
    product_id              TEXT NOT NULL REFERENCES products(product_id),
    display_name            TEXT NOT NULL,
    stripe_monthly_price_id TEXT,
    stripe_annual_price_id  TEXT,
    monthly_price_cents     INTEGER,
    annual_price_cents      INTEGER,
    included_credits        BIGINT NOT NULL DEFAULT 0,
    unit_rates              JSONB NOT NULL DEFAULT '{}',
    overage_unit_rates      JSONB NOT NULL DEFAULT '{}',
    quotas                  JSONB NOT NULL DEFAULT '{}',
    cancellation_policy     JSONB NOT NULL DEFAULT '{"annual_refund_mode":"credit_note","void_remaining_credits":false}',
    is_default              BOOLEAN NOT NULL DEFAULT false,
    sort_order              INTEGER NOT NULL DEFAULT 0,
    active                  BOOLEAN NOT NULL DEFAULT true,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_default_plan_per_product
    ON plans (product_id)
    WHERE is_default;

CREATE TABLE orgs (
    org_id             TEXT PRIMARY KEY,
    display_name       TEXT NOT NULL,
    stripe_customer_id TEXT UNIQUE,
    billing_email      TEXT,
    trust_tier         TEXT NOT NULL DEFAULT 'new' CHECK (trust_tier IN ('new', 'established', 'enterprise')),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE subscriptions (
    subscription_id        BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    org_id                 TEXT NOT NULL REFERENCES orgs(org_id),
    plan_id                TEXT NOT NULL REFERENCES plans(plan_id),
    product_id             TEXT NOT NULL REFERENCES products(product_id),
    stripe_subscription_id TEXT UNIQUE,
    stripe_item_id         TEXT,
    cadence                billing_cadence NOT NULL DEFAULT 'monthly',
    billing_anchor_day     SMALLINT NOT NULL DEFAULT 1,
    current_period_start   TIMESTAMPTZ,
    current_period_end     TIMESTAMPTZ,
    status                 subscription_status NOT NULL DEFAULT 'active',
    status_changed_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    past_due_since         TIMESTAMPTZ,
    overage_cap_units      BIGINT CHECK (overage_cap_units >= 0),
    cancel_at_period_end   BOOLEAN NOT NULL DEFAULT false,
    cancelled_at           TIMESTAMPTZ,
    cancellation_reason    TEXT,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_one_active_sub_per_product
    ON subscriptions (org_id, product_id)
    WHERE status IN ('active', 'past_due', 'trialing');

CREATE TABLE credit_grants (
    grant_id            TEXT PRIMARY KEY,  -- ULID, application-generated
    org_id              TEXT NOT NULL REFERENCES orgs(org_id),
    product_id          TEXT NOT NULL REFERENCES products(product_id),
    amount              BIGINT NOT NULL CHECK (amount > 0),
    source              TEXT NOT NULL,
    stripe_reference_id TEXT,
    subscription_id     BIGINT REFERENCES subscriptions(subscription_id),
    period_start        TIMESTAMPTZ,
    period_end          TIMESTAMPTZ,
    expires_at          TIMESTAMPTZ,
    closed_at           TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_credit_grants_active
    ON credit_grants (org_id, product_id, expires_at)
    WHERE closed_at IS NULL;

CREATE UNIQUE INDEX idx_credit_grants_subscription_period
    ON credit_grants (subscription_id, period_start)
    WHERE subscription_id IS NOT NULL;

CREATE TABLE org_pricing_overrides (
    org_id      TEXT NOT NULL REFERENCES orgs(org_id),
    plan_id     TEXT NOT NULL REFERENCES plans(plan_id),
    unit_rates  JSONB NOT NULL,
    quotas      JSONB,
    notes       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, plan_id)
);

CREATE TABLE tasks (
    task_id         BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    task_type       TEXT NOT NULL,
    payload         JSONB NOT NULL DEFAULT '{}',
    status          task_status NOT NULL DEFAULT 'pending',
    idempotency_key TEXT UNIQUE,
    attempts        INTEGER NOT NULL DEFAULT 0,
    max_attempts    INTEGER NOT NULL DEFAULT 5,
    last_error      TEXT,
    next_retry_at   TIMESTAMPTZ,
    scheduled_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    dead_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_tasks_claimable
    ON tasks (scheduled_at, next_retry_at)
    WHERE status IN ('pending', 'retrying');

CREATE INDEX idx_tasks_dead
    ON tasks (dead_at)
    WHERE status = 'dead';

CREATE TABLE billing_events (
    event_id        BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    org_id          TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    subscription_id BIGINT,
    grant_id        TEXT,
    task_id         BIGINT,
    payload         JSONB NOT NULL DEFAULT '{}',
    stripe_event_id TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_billing_events_stripe
    ON billing_events (stripe_event_id)
    WHERE stripe_event_id IS NOT NULL;

CREATE UNIQUE INDEX idx_billing_events_credits_deposited_grant
    ON billing_events (grant_id)
    WHERE event_type = 'credits_deposited' AND grant_id IS NOT NULL;

CREATE INDEX idx_billing_events_org
    ON billing_events (org_id, created_at);

CREATE TABLE billing_cursors (
    cursor_name   TEXT PRIMARY KEY,
    cursor_ts     TIMESTAMPTZ,
    cursor_bigint BIGINT,
    cursor_json   JSONB NOT NULL DEFAULT '{}',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
