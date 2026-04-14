-- Billing-service reset schema.
--
-- This is intentionally a full cutover schema, not a compatibility migration.
-- PostgreSQL owns billing truth; River owns execution handles; ClickHouse is a
-- projection sink fed from billing_events via billing_event_delivery_queue.

CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE OR REPLACE FUNCTION billing_set_updated_at()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;

CREATE TABLE orgs (
    org_id              TEXT        PRIMARY KEY CHECK (org_id <> ''),
    display_name        TEXT        NOT NULL,
    billing_email       TEXT        NOT NULL DEFAULT '',
    state               TEXT        NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'suspended', 'closed')),
    trust_tier          TEXT        NOT NULL DEFAULT 'new' CHECK (trust_tier IN ('new', 'established', 'enterprise', 'platform')),
    overage_policy      TEXT        NOT NULL DEFAULT 'block' CHECK (overage_policy IN ('block', 'bill_published_rate', 'block_after_balance')),
    overage_consent_at  TIMESTAMPTZ,
    metadata            JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((overage_policy = 'bill_published_rate') = (overage_consent_at IS NOT NULL))
);

CREATE TABLE products (
    product_id      TEXT        PRIMARY KEY CHECK (product_id <> ''),
    display_name    TEXT        NOT NULL,
    meter_unit      TEXT        NOT NULL,
    billing_model   TEXT        NOT NULL CHECK (billing_model IN ('metered', 'licensed', 'one_time')),
    reserve_policy  JSONB       NOT NULL DEFAULT '{}'::jsonb,
    active          BOOLEAN     NOT NULL DEFAULT true,
    metadata        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE credit_buckets (
    bucket_id     TEXT        PRIMARY KEY CHECK (bucket_id <> ''),
    display_name  TEXT        NOT NULL,
    sort_order    INTEGER     NOT NULL DEFAULT 0,
    metadata      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE skus (
    sku_id         TEXT        PRIMARY KEY CHECK (sku_id <> ''),
    product_id     TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    bucket_id      TEXT        NOT NULL REFERENCES credit_buckets(bucket_id) ON DELETE RESTRICT,
    display_name   TEXT        NOT NULL,
    quantity_unit  TEXT        NOT NULL,
    active         BOOLEAN     NOT NULL DEFAULT true,
    metadata       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX skus_product_active_idx
    ON skus (product_id, active, bucket_id, sku_id);

CREATE TABLE plans (
    plan_id                  TEXT        PRIMARY KEY CHECK (plan_id <> ''),
    product_id               TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    display_name             TEXT        NOT NULL,
    tier                     TEXT        NOT NULL,
    billing_mode             TEXT        NOT NULL CHECK (billing_mode IN ('prepaid', 'postpaid', 'invoice_only')),
    monthly_amount_cents     BIGINT      NOT NULL DEFAULT 0 CHECK (monthly_amount_cents >= 0),
    annual_amount_cents      BIGINT      NOT NULL DEFAULT 0 CHECK (annual_amount_cents >= 0),
    currency                 TEXT        NOT NULL DEFAULT 'usd' CHECK (currency <> ''),
    stripe_price_id_monthly  TEXT,
    stripe_price_id_annual   TEXT,
    active                   BOOLEAN     NOT NULL DEFAULT true,
    is_default               BOOLEAN     NOT NULL DEFAULT false,
    metadata                 JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX plans_default_per_product_idx
    ON plans (product_id)
    WHERE is_default AND active;

CREATE INDEX plans_product_active_idx
    ON plans (product_id, active, tier, plan_id);

CREATE TABLE plan_sku_rates (
    rate_id       TEXT        PRIMARY KEY CHECK (rate_id <> ''),
    plan_id       TEXT        NOT NULL REFERENCES plans(plan_id) ON DELETE CASCADE,
    sku_id        TEXT        NOT NULL REFERENCES skus(sku_id) ON DELETE RESTRICT,
    unit_rate     BIGINT      NOT NULL CHECK (unit_rate >= 0),
    currency      TEXT        NOT NULL DEFAULT 'usd' CHECK (currency <> ''),
    active        BOOLEAN     NOT NULL DEFAULT true,
    active_from   TIMESTAMPTZ NOT NULL,
    active_until  TIMESTAMPTZ,
    metadata      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (active_until IS NULL OR active_until > active_from)
);

CREATE INDEX plan_sku_rates_lookup_idx
    ON plan_sku_rates (plan_id, sku_id, currency, active, active_from DESC);

ALTER TABLE plan_sku_rates
    ADD CONSTRAINT plan_sku_rates_no_active_overlap
    EXCLUDE USING gist (
        plan_id WITH =,
        sku_id WITH =,
        currency WITH =,
        tstzrange(active_from, COALESCE(active_until, 'infinity'::timestamptz), '[)') WITH &&
    )
    WHERE (active);

CREATE TABLE entitlement_policies (
    policy_id         TEXT        PRIMARY KEY CHECK (policy_id <> ''),
    product_id        TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    source            TEXT        NOT NULL CHECK (source IN ('free_tier', 'contract', 'purchase', 'promo', 'refund')),
    scope_type        TEXT        NOT NULL CHECK (scope_type IN ('sku', 'bucket', 'product', 'account')),
    scope_product_id  TEXT        REFERENCES products(product_id) ON DELETE RESTRICT,
    scope_bucket_id   TEXT        REFERENCES credit_buckets(bucket_id) ON DELETE RESTRICT,
    scope_sku_id      TEXT        REFERENCES skus(sku_id) ON DELETE RESTRICT,
    amount_units      BIGINT      NOT NULL CHECK (amount_units >= 0),
    cadence           TEXT        NOT NULL CHECK (cadence IN ('monthly', 'annual', 'one_time')),
    anchor_kind       TEXT        NOT NULL CHECK (anchor_kind IN ('billing_cycle', 'calendar_month', 'anniversary', 'calendar_month_day')),
    proration_mode    TEXT        NOT NULL CHECK (proration_mode IN ('none', 'prorate_by_time_left')),
    policy_version    TEXT        NOT NULL DEFAULT 'v1' CHECK (policy_version <> ''),
    active_from       TIMESTAMPTZ NOT NULL,
    active_until      TIMESTAMPTZ,
    metadata          JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (active_until IS NULL OR active_until > active_from),
    CHECK (
        (scope_type = 'sku' AND scope_product_id IS NOT NULL AND scope_bucket_id IS NOT NULL AND scope_sku_id IS NOT NULL)
        OR (scope_type = 'bucket' AND scope_product_id IS NOT NULL AND scope_bucket_id IS NOT NULL AND scope_sku_id IS NULL)
        OR (scope_type = 'product' AND scope_product_id IS NOT NULL AND scope_bucket_id IS NULL AND scope_sku_id IS NULL)
        OR (scope_type = 'account' AND scope_product_id IS NULL AND scope_bucket_id IS NULL AND scope_sku_id IS NULL)
    )
);

CREATE INDEX entitlement_policies_active_idx
    ON entitlement_policies (source, product_id, active_from, active_until, policy_id);

CREATE TABLE plan_entitlements (
    plan_id     TEXT        NOT NULL REFERENCES plans(plan_id) ON DELETE CASCADE,
    policy_id   TEXT        NOT NULL REFERENCES entitlement_policies(policy_id) ON DELETE RESTRICT,
    sort_order  INTEGER     NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (plan_id, policy_id)
);

CREATE TABLE contracts (
    contract_id        TEXT        PRIMARY KEY CHECK (contract_id <> ''),
    org_id             TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id         TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    display_name       TEXT        NOT NULL DEFAULT '',
    contract_kind      TEXT        NOT NULL CHECK (contract_kind IN ('self_serve', 'enterprise', 'internal')),
    state              TEXT        NOT NULL CHECK (state IN ('draft', 'pending_activation', 'active', 'past_due', 'suspended', 'cancel_scheduled', 'ended', 'voided')),
    payment_state      TEXT        NOT NULL DEFAULT 'pending' CHECK (payment_state IN ('not_required', 'pending', 'paid', 'failed', 'uncollectible', 'refunded')),
    entitlement_state  TEXT        NOT NULL DEFAULT 'scheduled' CHECK (entitlement_state IN ('scheduled', 'active', 'grace', 'closed', 'voided')),
    currency           TEXT        NOT NULL DEFAULT 'usd' CHECK (currency <> ''),
    overage_policy     TEXT        NOT NULL DEFAULT 'block' CHECK (overage_policy IN ('block', 'bill_published_rate', 'block_after_balance')),
    starts_at          TIMESTAMPTZ NOT NULL,
    ends_at            TIMESTAMPTZ,
    grace_until        TIMESTAMPTZ,
    cancel_at          TIMESTAMPTZ,
    closed_at          TIMESTAMPTZ,
    state_version      BIGINT      NOT NULL DEFAULT 1 CHECK (state_version > 0),
    metadata           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (ends_at IS NULL OR ends_at > starts_at),
    CHECK (closed_at IS NULL OR state IN ('ended', 'voided')),
    CHECK (cancel_at IS NULL OR state IN ('cancel_scheduled', 'ended', 'voided'))
);

CREATE UNIQUE INDEX contracts_one_live_per_org_product_idx
    ON contracts (org_id, product_id)
    WHERE state IN ('pending_activation', 'active', 'past_due', 'suspended', 'cancel_scheduled');

CREATE INDEX contracts_org_product_state_idx
    ON contracts (org_id, product_id, state, starts_at DESC, contract_id);

CREATE TABLE provider_bindings (
    binding_id            TEXT        PRIMARY KEY CHECK (binding_id <> ''),
    aggregate_type        TEXT        NOT NULL CHECK (aggregate_type IN ('contract', 'payment_method', 'invoice', 'payment_intent', 'customer')),
    aggregate_id          TEXT        NOT NULL CHECK (aggregate_id <> ''),
    contract_id           TEXT        REFERENCES contracts(contract_id) ON DELETE CASCADE,
    provider              TEXT        NOT NULL CHECK (provider IN ('stripe', 'manual')),
    provider_object_type  TEXT        NOT NULL CHECK (provider_object_type <> ''),
    provider_object_id    TEXT        NOT NULL CHECK (provider_object_id <> ''),
    provider_customer_id  TEXT,
    sync_state            TEXT        NOT NULL DEFAULT 'none' CHECK (sync_state IN ('none', 'pending', 'synced', 'error')),
    metadata              JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX provider_bindings_provider_object_idx
    ON provider_bindings (provider, provider_object_type, provider_object_id);

CREATE INDEX provider_bindings_aggregate_idx
    ON provider_bindings (aggregate_type, aggregate_id);

CREATE INDEX provider_bindings_contract_idx
    ON provider_bindings (contract_id, provider, provider_object_type);

CREATE TABLE payment_methods (
    payment_method_id            TEXT        PRIMARY KEY CHECK (payment_method_id <> ''),
    org_id                       TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    provider                     TEXT        NOT NULL CHECK (provider IN ('stripe')),
    provider_customer_id         TEXT        NOT NULL CHECK (provider_customer_id <> ''),
    provider_payment_method_id   TEXT        NOT NULL CHECK (provider_payment_method_id <> ''),
    setup_intent_id              TEXT,
    status                       TEXT        NOT NULL CHECK (status IN ('pending', 'active', 'detached', 'failed')),
    is_default                   BOOLEAN     NOT NULL DEFAULT false,
    card_brand                   TEXT        NOT NULL DEFAULT '',
    card_last4                   TEXT        NOT NULL DEFAULT '',
    expires_month                INTEGER     CHECK (expires_month IS NULL OR expires_month BETWEEN 1 AND 12),
    expires_year                 INTEGER     CHECK (expires_year IS NULL OR expires_year >= 2000),
    off_session_authorized_at    TIMESTAMPTZ,
    metadata                     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at                   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX payment_methods_default_per_org_idx
    ON payment_methods (org_id)
    WHERE is_default AND status = 'active';

CREATE UNIQUE INDEX payment_methods_provider_object_idx
    ON payment_methods (provider, provider_payment_method_id);

CREATE TABLE billing_provider_events (
    event_id                    TEXT        PRIMARY KEY CHECK (event_id <> ''),
    provider_event_id           TEXT        NOT NULL CHECK (provider_event_id <> ''),
    provider                    TEXT        NOT NULL CHECK (provider IN ('stripe', 'manual')),
    event_type                  TEXT        NOT NULL CHECK (event_type <> ''),
    provider_object_type        TEXT,
    provider_object_id          TEXT,
    provider_customer_id        TEXT,
    provider_invoice_id         TEXT,
    provider_payment_intent_id  TEXT,
    contract_id                 TEXT,
    change_id                   TEXT,
    invoice_id                  TEXT,
    binding_id                  TEXT        REFERENCES provider_bindings(binding_id) ON DELETE SET NULL,
    org_id                      TEXT        REFERENCES orgs(org_id) ON DELETE SET NULL,
    product_id                  TEXT        REFERENCES products(product_id) ON DELETE SET NULL,
    received_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    provider_created_at         TIMESTAMPTZ,
    api_version                 TEXT,
    livemode                    BOOLEAN     NOT NULL DEFAULT false,
    payload                     JSONB       NOT NULL,
    state                       TEXT        NOT NULL CHECK (state IN ('received', 'queued', 'applying', 'applied', 'ignored', 'failed', 'dead_letter')),
    attempts                    INTEGER     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at             TIMESTAMPTZ,
    applied_at                  TIMESTAMPTZ,
    last_error                  TEXT        NOT NULL DEFAULT '',
    idempotency_key             TEXT        NOT NULL CHECK (idempotency_key <> ''),
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_event_id)
);

CREATE INDEX billing_provider_events_due_idx
    ON billing_provider_events (state, next_attempt_at, received_at, event_id)
    WHERE state IN ('received', 'queued', 'failed');

CREATE INDEX billing_provider_events_object_idx
    ON billing_provider_events (provider, provider_object_type, provider_object_id, received_at DESC)
    WHERE provider_object_type IS NOT NULL AND provider_object_id IS NOT NULL;

CREATE TABLE contract_changes (
    change_id                   TEXT        PRIMARY KEY CHECK (change_id <> ''),
    contract_id                 TEXT        NOT NULL REFERENCES contracts(contract_id) ON DELETE CASCADE,
    org_id                      TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id                  TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    change_type                 TEXT        NOT NULL CHECK (change_type IN ('create', 'upgrade', 'downgrade', 'cancel', 'renew', 'amend')),
    timing                      TEXT        NOT NULL CHECK (timing IN ('immediate', 'period_end', 'specific_time')),
    requested_effective_at      TIMESTAMPTZ NOT NULL,
    actual_effective_at         TIMESTAMPTZ,
    from_phase_id               TEXT,
    to_phase_id                 TEXT,
    target_plan_id              TEXT        REFERENCES plans(plan_id) ON DELETE RESTRICT,
    state                       TEXT        NOT NULL CHECK (state IN ('requested', 'provider_pending', 'awaiting_payment', 'scheduled', 'applying', 'applied', 'failed', 'canceled')),
    provider                    TEXT        CHECK (provider IS NULL OR provider IN ('stripe', 'manual')),
    provider_request_id         TEXT,
    provider_invoice_id         TEXT,
    idempotency_key             TEXT        NOT NULL CHECK (idempotency_key <> ''),
    failure_reason              TEXT        NOT NULL DEFAULT '',
    requested_by                TEXT        NOT NULL DEFAULT '',
    requested_at                TIMESTAMPTZ NOT NULL,
    next_attempt_at             TIMESTAMPTZ,
    attempts                    INTEGER     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    state_version               BIGINT      NOT NULL DEFAULT 1 CHECK (state_version > 0),
    proration_basis_cycle_id    TEXT,
    price_delta_units           BIGINT      NOT NULL DEFAULT 0 CHECK (price_delta_units >= 0),
    entitlement_delta_mode      TEXT        NOT NULL DEFAULT 'none' CHECK (entitlement_delta_mode IN ('none', 'positive_delta')),
    proration_numerator         BIGINT      NOT NULL DEFAULT 0 CHECK (proration_numerator >= 0),
    proration_denominator       BIGINT      NOT NULL DEFAULT 0 CHECK (proration_denominator >= 0),
    payload                     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (actual_effective_at IS NULL OR actual_effective_at >= requested_effective_at),
    CHECK ((proration_denominator = 0 AND proration_numerator = 0) OR proration_denominator > 0),
    CHECK (proration_numerator <= proration_denominator),
    UNIQUE (contract_id, idempotency_key)
);

CREATE INDEX contract_changes_due_idx
    ON contract_changes (state, requested_effective_at, next_attempt_at, change_id)
    WHERE state IN ('requested', 'provider_pending', 'awaiting_payment', 'scheduled', 'failed');

CREATE INDEX contract_changes_contract_idx
    ON contract_changes (contract_id, requested_effective_at DESC, change_id);

CREATE UNIQUE INDEX contract_changes_one_scheduled_period_end_idx
    ON contract_changes (contract_id)
    WHERE state = 'scheduled'
      AND timing = 'period_end'
      AND change_type IN ('downgrade', 'cancel');

CREATE UNIQUE INDEX contract_changes_provider_request_idx
    ON contract_changes (provider, provider_request_id)
    WHERE provider IS NOT NULL AND provider_request_id IS NOT NULL;

CREATE TABLE contract_phases (
    phase_id                  TEXT        PRIMARY KEY CHECK (phase_id <> ''),
    contract_id               TEXT        NOT NULL REFERENCES contracts(contract_id) ON DELETE CASCADE,
    org_id                    TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id                TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    plan_id                   TEXT        REFERENCES plans(plan_id) ON DELETE RESTRICT,
    provider_price_id         TEXT,
    phase_kind                TEXT        NOT NULL CHECK (phase_kind IN ('catalog_plan', 'bespoke', 'internal')),
    state                     TEXT        NOT NULL CHECK (state IN ('scheduled', 'pending_payment', 'active', 'grace', 'superseded', 'closed', 'voided')),
    payment_state             TEXT        NOT NULL DEFAULT 'pending' CHECK (payment_state IN ('not_required', 'pending', 'paid', 'failed', 'uncollectible', 'refunded')),
    entitlement_state         TEXT        NOT NULL DEFAULT 'scheduled' CHECK (entitlement_state IN ('scheduled', 'active', 'grace', 'closed', 'voided')),
    currency                  TEXT        NOT NULL DEFAULT 'usd' CHECK (currency <> ''),
    recurring_amount_units    BIGINT      NOT NULL DEFAULT 0 CHECK (recurring_amount_units >= 0),
    recurring_interval        TEXT        NOT NULL DEFAULT 'month' CHECK (recurring_interval IN ('month', 'year', 'manual', 'none')),
    effective_start           TIMESTAMPTZ NOT NULL,
    effective_end             TIMESTAMPTZ,
    activated_at              TIMESTAMPTZ,
    closed_at                 TIMESTAMPTZ,
    superseded_by_phase_id    TEXT        REFERENCES contract_phases(phase_id) ON DELETE SET NULL,
    created_reason            TEXT        NOT NULL DEFAULT '',
    state_version             BIGINT      NOT NULL DEFAULT 1 CHECK (state_version > 0),
    rate_context              JSONB       NOT NULL DEFAULT '{}'::jsonb,
    metadata                  JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (effective_end IS NULL OR effective_end > effective_start),
    CHECK ((phase_kind = 'catalog_plan') = (plan_id IS NOT NULL)),
    CHECK (closed_at IS NULL OR state IN ('superseded', 'closed', 'voided'))
);

ALTER TABLE contract_changes
    ADD CONSTRAINT contract_changes_from_phase_fk
    FOREIGN KEY (from_phase_id) REFERENCES contract_phases(phase_id) ON DELETE SET NULL;

CREATE INDEX contract_phases_contract_idx
    ON contract_phases (contract_id, effective_start DESC, phase_id);

CREATE INDEX contract_phases_active_lookup_idx
    ON contract_phases (org_id, product_id, state, effective_start, effective_end, phase_id)
    WHERE state IN ('active', 'grace');

ALTER TABLE contract_phases
    ADD CONSTRAINT contract_phases_no_active_overlap
    EXCLUDE USING gist (
        org_id WITH =,
        product_id WITH =,
        tstzrange(effective_start, COALESCE(effective_end, 'infinity'::timestamptz), '[)') WITH &&
    )
    WHERE (state IN ('active', 'grace'));

CREATE TABLE contract_entitlement_lines (
    line_id                         TEXT        PRIMARY KEY CHECK (line_id <> ''),
    phase_id                        TEXT        NOT NULL REFERENCES contract_phases(phase_id) ON DELETE CASCADE,
    contract_id                     TEXT        NOT NULL REFERENCES contracts(contract_id) ON DELETE CASCADE,
    org_id                          TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id                      TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    source                          TEXT        NOT NULL DEFAULT 'contract' CHECK (source = 'contract'),
    policy_id                       TEXT        REFERENCES entitlement_policies(policy_id) ON DELETE RESTRICT,
    scope_type                      TEXT        NOT NULL CHECK (scope_type IN ('sku', 'bucket', 'product', 'account')),
    scope_product_id                TEXT        REFERENCES products(product_id) ON DELETE RESTRICT,
    scope_bucket_id                 TEXT        REFERENCES credit_buckets(bucket_id) ON DELETE RESTRICT,
    scope_sku_id                    TEXT        REFERENCES skus(sku_id) ON DELETE RESTRICT,
    amount_units                    BIGINT      NOT NULL CHECK (amount_units >= 0),
    recurrence_interval             TEXT        NOT NULL CHECK (recurrence_interval IN ('month', 'year')),
    recurrence_anchor_kind          TEXT        NOT NULL CHECK (recurrence_anchor_kind IN ('billing_cycle', 'anniversary', 'calendar_month_day')),
    recurrence_anchor_day           INTEGER     CHECK (recurrence_anchor_day IS NULL OR recurrence_anchor_day BETWEEN 1 AND 31),
    recurrence_timezone             TEXT        NOT NULL DEFAULT 'UTC',
    charge_timing                   TEXT        NOT NULL DEFAULT 'cycle_start' CHECK (charge_timing IN ('cycle_start', 'cycle_end', 'none')),
    proration_mode                  TEXT        NOT NULL CHECK (proration_mode IN ('none', 'prorate_by_time_left')),
    policy_version                  TEXT        NOT NULL DEFAULT 'v1' CHECK (policy_version <> ''),
    active_from                     TIMESTAMPTZ NOT NULL,
    active_until                    TIMESTAMPTZ,
    last_materialized_period_start  TIMESTAMPTZ,
    next_materialize_at             TIMESTAMPTZ,
    metadata                        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at                      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (active_until IS NULL OR active_until > active_from),
    CHECK (
        (scope_type = 'sku' AND scope_product_id IS NOT NULL AND scope_bucket_id IS NOT NULL AND scope_sku_id IS NOT NULL)
        OR (scope_type = 'bucket' AND scope_product_id IS NOT NULL AND scope_bucket_id IS NOT NULL AND scope_sku_id IS NULL)
        OR (scope_type = 'product' AND scope_product_id IS NOT NULL AND scope_bucket_id IS NULL AND scope_sku_id IS NULL)
        OR (scope_type = 'account' AND scope_product_id IS NULL AND scope_bucket_id IS NULL AND scope_sku_id IS NULL)
    ),
    CHECK ((recurrence_anchor_kind = 'calendar_month_day') = (recurrence_anchor_day IS NOT NULL))
);

CREATE INDEX contract_entitlement_lines_phase_idx
    ON contract_entitlement_lines (phase_id, next_materialize_at, line_id);

CREATE INDEX contract_entitlement_lines_due_idx
    ON contract_entitlement_lines (next_materialize_at, line_id)
    WHERE active_until IS NULL OR next_materialize_at < active_until;

CREATE TABLE billing_cycles (
    cycle_id              TEXT        PRIMARY KEY CHECK (cycle_id <> ''),
    org_id                TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id            TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    currency              TEXT        NOT NULL DEFAULT 'usd' CHECK (currency <> ''),
    predecessor_cycle_id  TEXT        REFERENCES billing_cycles(cycle_id) ON DELETE SET NULL,
    anchor_at             TIMESTAMPTZ NOT NULL,
    cycle_seq             BIGINT      NOT NULL CHECK (cycle_seq >= 0),
    cadence_kind          TEXT        NOT NULL CHECK (cadence_kind IN ('anniversary_monthly', 'calendar_monthly', 'annual', 'manual')),
    starts_at             TIMESTAMPTZ NOT NULL,
    ends_at               TIMESTAMPTZ NOT NULL,
    status                TEXT        NOT NULL CHECK (status IN ('open', 'closing', 'closed_for_usage', 'invoice_finalizing', 'invoiced', 'blocked', 'voided')),
    finalization_due_at   TIMESTAMPTZ NOT NULL,
    invoice_id            TEXT,
    blocked_reason        TEXT        NOT NULL DEFAULT '',
    closed_for_usage_at   TIMESTAMPTZ,
    finalized_at          TIMESTAMPTZ,
    metadata              JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (ends_at > starts_at),
    UNIQUE (org_id, product_id, anchor_at, cycle_seq)
);

CREATE UNIQUE INDEX billing_cycles_open_per_org_product_idx
    ON billing_cycles (org_id, product_id)
    WHERE status IN ('open', 'closing');

CREATE INDEX billing_cycles_due_idx
    ON billing_cycles (status, ends_at, finalization_due_at, cycle_id)
    WHERE status IN ('open', 'closing', 'closed_for_usage', 'blocked');

ALTER TABLE billing_cycles
    ADD CONSTRAINT billing_cycles_no_overlap
    EXCLUDE USING gist (
        org_id WITH =,
        product_id WITH =,
        tstzrange(starts_at, ends_at, '[)') WITH &&
    )
    WHERE (status <> 'voided');

ALTER TABLE contract_changes
    ADD CONSTRAINT contract_changes_proration_cycle_fk
    FOREIGN KEY (proration_basis_cycle_id) REFERENCES billing_cycles(cycle_id) ON DELETE SET NULL;

CREATE TABLE entitlement_periods (
    period_id             TEXT        PRIMARY KEY CHECK (period_id <> ''),
    org_id                TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id            TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    cycle_id              TEXT        REFERENCES billing_cycles(cycle_id) ON DELETE SET NULL,
    source                TEXT        NOT NULL CHECK (source IN ('free_tier', 'contract', 'purchase', 'promo', 'refund')),
    policy_id             TEXT        REFERENCES entitlement_policies(policy_id) ON DELETE RESTRICT,
    contract_id           TEXT        REFERENCES contracts(contract_id) ON DELETE CASCADE,
    phase_id              TEXT        REFERENCES contract_phases(phase_id) ON DELETE CASCADE,
    line_id               TEXT        REFERENCES contract_entitlement_lines(line_id) ON DELETE CASCADE,
    scope_type            TEXT        NOT NULL CHECK (scope_type IN ('sku', 'bucket', 'product', 'account')),
    scope_product_id      TEXT        REFERENCES products(product_id) ON DELETE RESTRICT,
    scope_bucket_id       TEXT        REFERENCES credit_buckets(bucket_id) ON DELETE RESTRICT,
    scope_sku_id          TEXT        REFERENCES skus(sku_id) ON DELETE RESTRICT,
    amount_units          BIGINT      NOT NULL CHECK (amount_units >= 0),
    period_start          TIMESTAMPTZ NOT NULL,
    period_end            TIMESTAMPTZ NOT NULL,
    policy_version        TEXT        NOT NULL DEFAULT 'v1' CHECK (policy_version <> ''),
    payment_state         TEXT        NOT NULL DEFAULT 'not_required' CHECK (payment_state IN ('not_required', 'pending', 'paid', 'failed', 'uncollectible', 'refunded')),
    entitlement_state     TEXT        NOT NULL DEFAULT 'scheduled' CHECK (entitlement_state IN ('scheduled', 'active', 'grace', 'closed', 'voided')),
    provider_invoice_id   TEXT,
    provider_event_id     TEXT,
    change_id             TEXT        REFERENCES contract_changes(change_id) ON DELETE SET NULL,
    calculation_kind      TEXT        NOT NULL CHECK (calculation_kind IN ('recurrence', 'activation', 'upgrade_delta', 'manual_adjustment')),
    source_reference_id   TEXT        NOT NULL CHECK (source_reference_id <> ''),
    created_reason        TEXT        NOT NULL DEFAULT '',
    metadata              JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (period_end > period_start),
    CHECK (
        (scope_type = 'sku' AND scope_product_id IS NOT NULL AND scope_bucket_id IS NOT NULL AND scope_sku_id IS NOT NULL)
        OR (scope_type = 'bucket' AND scope_product_id IS NOT NULL AND scope_bucket_id IS NOT NULL AND scope_sku_id IS NULL)
        OR (scope_type = 'product' AND scope_product_id IS NOT NULL AND scope_bucket_id IS NULL AND scope_sku_id IS NULL)
        OR (scope_type = 'account' AND scope_product_id IS NULL AND scope_bucket_id IS NULL AND scope_sku_id IS NULL)
    ),
    CHECK (
        (source = 'contract' AND contract_id IS NOT NULL AND phase_id IS NOT NULL AND line_id IS NOT NULL)
        OR (source <> 'contract' AND contract_id IS NULL AND phase_id IS NULL AND line_id IS NULL)
    )
);

CREATE UNIQUE INDEX entitlement_periods_source_reference_idx
    ON entitlement_periods (org_id, source, source_reference_id);

CREATE INDEX entitlement_periods_active_idx
    ON entitlement_periods (org_id, product_id, source, period_start, period_end, entitlement_state)
    WHERE entitlement_state IN ('active', 'grace');

CREATE INDEX entitlement_periods_cycle_idx
    ON entitlement_periods (cycle_id, source, period_start, period_id);

CREATE TABLE credit_grants (
    grant_id               TEXT        PRIMARY KEY CHECK (grant_id <> ''),
    org_id                 TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    scope_type             TEXT        NOT NULL CHECK (scope_type IN ('sku', 'bucket', 'product', 'account')),
    scope_product_id       TEXT        REFERENCES products(product_id) ON DELETE RESTRICT,
    scope_bucket_id        TEXT        REFERENCES credit_buckets(bucket_id) ON DELETE RESTRICT,
    scope_sku_id           TEXT        REFERENCES skus(sku_id) ON DELETE RESTRICT,
    amount                 BIGINT      NOT NULL CHECK (amount >= 0),
    source                 TEXT        NOT NULL CHECK (source IN ('free_tier', 'contract', 'purchase', 'promo', 'refund')),
    source_reference_id    TEXT        NOT NULL CHECK (source_reference_id <> ''),
    entitlement_period_id  TEXT        REFERENCES entitlement_periods(period_id) ON DELETE SET NULL,
    policy_version         TEXT        NOT NULL DEFAULT 'v1' CHECK (policy_version <> ''),
    starts_at              TIMESTAMPTZ NOT NULL,
    period_start           TIMESTAMPTZ,
    period_end             TIMESTAMPTZ,
    expires_at             TIMESTAMPTZ,
    closed_at              TIMESTAMPTZ,
    closed_reason          TEXT        NOT NULL DEFAULT '',
    tigerbeetle_account_id TEXT,
    deposit_transfer_id    TEXT,
    ledger_state           TEXT        NOT NULL DEFAULT 'posted' CHECK (ledger_state IN ('pending', 'posted', 'failed')),
    metadata               JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (scope_type = 'sku' AND scope_product_id IS NOT NULL AND scope_bucket_id IS NOT NULL AND scope_sku_id IS NOT NULL)
        OR (scope_type = 'bucket' AND scope_product_id IS NOT NULL AND scope_bucket_id IS NOT NULL AND scope_sku_id IS NULL)
        OR (scope_type = 'product' AND scope_product_id IS NOT NULL AND scope_bucket_id IS NULL AND scope_sku_id IS NULL)
        OR (scope_type = 'account' AND scope_product_id IS NULL AND scope_bucket_id IS NULL AND scope_sku_id IS NULL)
    ),
    CHECK (expires_at IS NULL OR expires_at > starts_at),
    CHECK ((period_start IS NULL AND period_end IS NULL) OR (period_start IS NOT NULL AND period_end IS NOT NULL AND period_end > period_start))
);

CREATE UNIQUE INDEX credit_grants_source_reference_idx
    ON credit_grants (org_id, source, scope_type, scope_product_id, scope_bucket_id, scope_sku_id, source_reference_id)
    NULLS NOT DISTINCT;

CREATE INDEX credit_grants_open_idx
    ON credit_grants (org_id, scope_type, scope_product_id, scope_bucket_id, scope_sku_id, source, starts_at, expires_at, grant_id)
    WHERE closed_at IS NULL;

CREATE TABLE billing_windows (
    window_id               TEXT        PRIMARY KEY CHECK (window_id <> ''),
    cycle_id                TEXT        NOT NULL REFERENCES billing_cycles(cycle_id) ON DELETE RESTRICT,
    org_id                  TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id              TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    actor_id                TEXT        NOT NULL DEFAULT '',
    source_type             TEXT        NOT NULL CHECK (source_type <> ''),
    source_ref              TEXT        NOT NULL CHECK (source_ref <> ''),
    billing_job_id          TEXT,
    window_seq              BIGINT      NOT NULL CHECK (window_seq >= 0),
    state                   TEXT        NOT NULL CHECK (state IN ('reserving', 'reserved', 'active', 'settled', 'voided', 'denied', 'failed')),
    reservation_shape       TEXT        NOT NULL CHECK (reservation_shape IN ('time', 'units')),
    reserved_quantity       BIGINT      NOT NULL CHECK (reserved_quantity >= 0),
    actual_quantity         BIGINT      NOT NULL DEFAULT 0 CHECK (actual_quantity >= 0),
    billable_quantity       BIGINT      NOT NULL DEFAULT 0 CHECK (billable_quantity >= 0),
    writeoff_quantity       BIGINT      NOT NULL DEFAULT 0 CHECK (writeoff_quantity >= 0),
    reserved_charge_units   BIGINT      NOT NULL CHECK (reserved_charge_units >= 0),
    billed_charge_units     BIGINT      NOT NULL DEFAULT 0 CHECK (billed_charge_units >= 0),
    writeoff_charge_units   BIGINT      NOT NULL DEFAULT 0 CHECK (writeoff_charge_units >= 0),
    writeoff_reason         TEXT        NOT NULL DEFAULT '',
    pricing_contract_id     TEXT        REFERENCES contracts(contract_id) ON DELETE SET NULL,
    pricing_phase_id        TEXT        REFERENCES contract_phases(phase_id) ON DELETE SET NULL,
    pricing_plan_id         TEXT        REFERENCES plans(plan_id) ON DELETE SET NULL,
    pricing_phase           TEXT        NOT NULL DEFAULT '',
    allocation              JSONB       NOT NULL DEFAULT '{}'::jsonb,
    rate_context            JSONB       NOT NULL DEFAULT '{}'::jsonb,
    usage_summary           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    funding_legs            JSONB       NOT NULL DEFAULT '[]'::jsonb,
    window_start            TIMESTAMPTZ NOT NULL,
    activated_at            TIMESTAMPTZ,
    expires_at              TIMESTAMPTZ NOT NULL,
    renew_by                TIMESTAMPTZ,
    settled_at              TIMESTAMPTZ,
    metering_projected_at   TIMESTAMPTZ,
    last_projection_error   TEXT        NOT NULL DEFAULT '',
    metadata                JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (expires_at > window_start),
    CHECK (settled_at IS NULL OR state = 'settled'),
    CHECK (activated_at IS NULL OR state IN ('active', 'settled')),
    UNIQUE (product_id, source_type, source_ref, window_seq)
);

CREATE UNIQUE INDEX billing_windows_billing_job_seq_idx
    ON billing_windows (billing_job_id, window_seq)
    WHERE billing_job_id IS NOT NULL;

CREATE INDEX billing_windows_cycle_idx
    ON billing_windows (cycle_id, state, window_start, window_id);

CREATE INDEX billing_windows_projection_pending_idx
    ON billing_windows (state, metering_projected_at, created_at, window_id)
    WHERE state = 'settled' AND metering_projected_at IS NULL;

CREATE TABLE invoice_finalizations (
    invoice_finalization_id             TEXT        PRIMARY KEY CHECK (invoice_finalization_id <> ''),
    cycle_id                            TEXT        NOT NULL REFERENCES billing_cycles(cycle_id) ON DELETE RESTRICT,
    org_id                              TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id                          TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    invoice_id                          TEXT,
    state                               TEXT        NOT NULL CHECK (state IN ('started', 'blocked', 'issued', 'failed', 'canceled')),
    started_at                          TIMESTAMPTZ NOT NULL,
    completed_at                        TIMESTAMPTZ,
    blocked_reason                      TEXT        NOT NULL DEFAULT '',
    automatic_adjustment_cap_units      BIGINT      NOT NULL DEFAULT 9900000 CHECK (automatic_adjustment_cap_units >= 0),
    automatic_adjustment_units          BIGINT      NOT NULL DEFAULT 0 CHECK (automatic_adjustment_units >= 0),
    attempts                            INTEGER     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    idempotency_key                     TEXT        NOT NULL CHECK (idempotency_key <> ''),
    snapshot_hash                       TEXT,
    last_error                          TEXT        NOT NULL DEFAULT '',
    metadata                            JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at                          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (completed_at IS NULL OR completed_at >= started_at),
    UNIQUE (cycle_id, idempotency_key)
);

CREATE INDEX invoice_finalizations_cycle_idx
    ON invoice_finalizations (cycle_id, state, started_at DESC, invoice_finalization_id);

CREATE TABLE billing_invoices (
    invoice_id                 TEXT        PRIMARY KEY CHECK (invoice_id <> ''),
    invoice_number             TEXT,
    org_id                     TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id                 TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    cycle_id                   TEXT        NOT NULL REFERENCES billing_cycles(cycle_id) ON DELETE RESTRICT,
    invoice_finalization_id    TEXT        REFERENCES invoice_finalizations(invoice_finalization_id) ON DELETE SET NULL,
    change_id                  TEXT        REFERENCES contract_changes(change_id) ON DELETE SET NULL,
    invoice_kind               TEXT        NOT NULL CHECK (invoice_kind IN ('cycle', 'activation', 'contract_change', 'adjustment', 'credit_note')),
    status                     TEXT        NOT NULL CHECK (status IN ('draft', 'finalizing', 'issued', 'paid', 'payment_failed', 'blocked', 'voided')),
    payment_status             TEXT        NOT NULL DEFAULT 'n_a' CHECK (payment_status IN ('n_a', 'pending', 'paid', 'failed', 'uncollectible')),
    period_start               TIMESTAMPTZ NOT NULL,
    period_end                 TIMESTAMPTZ NOT NULL,
    issued_at                  TIMESTAMPTZ,
    currency                   TEXT        NOT NULL DEFAULT 'usd' CHECK (currency <> ''),
    subtotal_units             BIGINT      NOT NULL DEFAULT 0,
    adjustment_units           BIGINT      NOT NULL DEFAULT 0,
    tax_units                  BIGINT      NOT NULL DEFAULT 0,
    total_due_units            BIGINT      NOT NULL DEFAULT 0,
    recipient_email            TEXT        NOT NULL DEFAULT '',
    recipient_name             TEXT        NOT NULL DEFAULT '',
    invoice_snapshot_json      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    rendered_html              TEXT        NOT NULL DEFAULT '',
    content_hash               TEXT        NOT NULL DEFAULT '',
    stripe_invoice_id          TEXT,
    stripe_hosted_invoice_url  TEXT,
    stripe_invoice_pdf_url     TEXT,
    stripe_payment_intent_id   TEXT,
    resend_message_id          TEXT,
    voided_by_invoice_id       TEXT        REFERENCES billing_invoices(invoice_id) ON DELETE SET NULL,
    blocked_reason             TEXT        NOT NULL DEFAULT '',
    metadata                   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (period_end > period_start),
    CHECK (issued_at IS NULL OR status IN ('issued', 'paid', 'payment_failed', 'voided')),
    CHECK (invoice_number IS NOT NULL OR status IN ('draft', 'finalizing', 'blocked'))
);

CREATE UNIQUE INDEX billing_invoices_number_idx
    ON billing_invoices (invoice_number)
    WHERE invoice_number IS NOT NULL;

CREATE UNIQUE INDEX billing_invoices_cycle_kind_idx
    ON billing_invoices (cycle_id, invoice_kind)
    WHERE status <> 'voided' AND invoice_kind = 'cycle';

CREATE UNIQUE INDEX billing_invoices_finalization_idx
    ON billing_invoices (invoice_finalization_id)
    WHERE invoice_finalization_id IS NOT NULL AND status <> 'voided';

ALTER TABLE billing_cycles
    ADD CONSTRAINT billing_cycles_invoice_fk
    FOREIGN KEY (invoice_id) REFERENCES billing_invoices(invoice_id) ON DELETE SET NULL;

ALTER TABLE invoice_finalizations
    ADD CONSTRAINT invoice_finalizations_invoice_fk
    FOREIGN KEY (invoice_id) REFERENCES billing_invoices(invoice_id) ON DELETE SET NULL;

ALTER TABLE billing_provider_events
    ADD CONSTRAINT billing_provider_events_invoice_fk
    FOREIGN KEY (invoice_id) REFERENCES billing_invoices(invoice_id) ON DELETE SET NULL;

CREATE TABLE invoice_line_items (
    line_item_id                   TEXT        PRIMARY KEY CHECK (line_item_id <> ''),
    invoice_id                     TEXT        NOT NULL REFERENCES billing_invoices(invoice_id) ON DELETE CASCADE,
    line_type                      TEXT        NOT NULL CHECK (line_type IN ('usage', 'recurring_charge', 'adjustment', 'tax', 'rounding')),
    product_id                     TEXT        REFERENCES products(product_id) ON DELETE RESTRICT,
    bucket_id                      TEXT        REFERENCES credit_buckets(bucket_id) ON DELETE RESTRICT,
    sku_id                         TEXT        REFERENCES skus(sku_id) ON DELETE RESTRICT,
    description                    TEXT        NOT NULL,
    quantity                       NUMERIC     NOT NULL DEFAULT 0,
    quantity_unit                  TEXT        NOT NULL DEFAULT '',
    unit_rate_units                BIGINT      NOT NULL DEFAULT 0,
    charge_units                   BIGINT      NOT NULL DEFAULT 0,
    free_tier_units                BIGINT      NOT NULL DEFAULT 0 CHECK (free_tier_units >= 0),
    contract_units                 BIGINT      NOT NULL DEFAULT 0 CHECK (contract_units >= 0),
    purchase_units                 BIGINT      NOT NULL DEFAULT 0 CHECK (purchase_units >= 0),
    promo_units                    BIGINT      NOT NULL DEFAULT 0 CHECK (promo_units >= 0),
    refund_units                   BIGINT      NOT NULL DEFAULT 0 CHECK (refund_units >= 0),
    receivable_units               BIGINT      NOT NULL DEFAULT 0 CHECK (receivable_units >= 0),
    adjustment_units               BIGINT      NOT NULL DEFAULT 0,
    source_window_id               TEXT        REFERENCES billing_windows(window_id) ON DELETE SET NULL,
    source_phase_id                TEXT        REFERENCES contract_phases(phase_id) ON DELETE SET NULL,
    source_entitlement_period_id   TEXT        REFERENCES entitlement_periods(period_id) ON DELETE SET NULL,
    metadata                       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at                     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX invoice_line_items_invoice_idx
    ON invoice_line_items (invoice_id, line_type, line_item_id);

CREATE INDEX invoice_line_items_sources_idx
    ON invoice_line_items (source_window_id, source_phase_id, source_entitlement_period_id);

CREATE TABLE invoice_adjustments (
    adjustment_id              TEXT        PRIMARY KEY CHECK (adjustment_id <> ''),
    invoice_id                 TEXT        NOT NULL REFERENCES billing_invoices(invoice_id) ON DELETE CASCADE,
    invoice_finalization_id    TEXT        NOT NULL REFERENCES invoice_finalizations(invoice_finalization_id) ON DELETE RESTRICT,
    org_id                     TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id                 TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    window_id                  TEXT        REFERENCES billing_windows(window_id) ON DELETE SET NULL,
    bucket_id                  TEXT        REFERENCES credit_buckets(bucket_id) ON DELETE SET NULL,
    sku_id                     TEXT        REFERENCES skus(sku_id) ON DELETE SET NULL,
    adjustment_type            TEXT        NOT NULL CHECK (adjustment_type IN ('credit', 'debit')),
    adjustment_source          TEXT        NOT NULL CHECK (adjustment_source IN ('system_policy', 'manual_admin', 'sla', 'campaign')),
    reason_code                TEXT        NOT NULL CHECK (reason_code IN ('free_tier_overage_absorbed', 'paid_hard_cap_overage_absorbed', 'operator_goodwill', 'policy_migration', 'rounding_residual')),
    amount_units               BIGINT      NOT NULL CHECK (amount_units >= 0),
    published_charge_units     BIGINT      NOT NULL DEFAULT 0 CHECK (published_charge_units >= 0),
    estimated_cost_units       BIGINT      NOT NULL DEFAULT 0 CHECK (estimated_cost_units >= 0),
    customer_visible           BOOLEAN     NOT NULL DEFAULT false,
    recoverable                BOOLEAN     NOT NULL DEFAULT false,
    affects_customer_balance   BOOLEAN     NOT NULL DEFAULT false,
    cost_center                TEXT        NOT NULL DEFAULT '',
    expense_category           TEXT        NOT NULL DEFAULT '',
    policy_version             TEXT        NOT NULL DEFAULT 'v1' CHECK (policy_version <> ''),
    metadata                   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX invoice_adjustments_deterministic_system_idx
    ON invoice_adjustments (
        invoice_finalization_id,
        org_id,
        product_id,
        COALESCE(window_id, ''),
        COALESCE(sku_id, ''),
        reason_code,
        policy_version
    )
    WHERE adjustment_source = 'system_policy';

CREATE TABLE invoice_number_allocators (
    issuer_id     TEXT        NOT NULL CHECK (issuer_id <> ''),
    invoice_year  INTEGER     NOT NULL CHECK (invoice_year >= 2000),
    prefix        TEXT        NOT NULL DEFAULT 'FM' CHECK (prefix <> ''),
    next_number   BIGINT      NOT NULL CHECK (next_number > 0),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issuer_id, invoice_year)
);

CREATE TABLE billing_account_registry (
    account_kind             TEXT        PRIMARY KEY CHECK (account_kind <> ''),
    tigerbeetle_account_id   TEXT        NOT NULL CHECK (tigerbeetle_account_id <> ''),
    description              TEXT        NOT NULL DEFAULT '',
    metadata                 JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE billing_events (
    event_id            TEXT        PRIMARY KEY CHECK (event_id <> ''),
    event_type          TEXT        NOT NULL CHECK (event_type <> ''),
    event_version       INTEGER     NOT NULL DEFAULT 1 CHECK (event_version > 0),
    aggregate_type      TEXT        NOT NULL CHECK (aggregate_type <> ''),
    aggregate_id        TEXT        NOT NULL CHECK (aggregate_id <> ''),
    org_id              TEXT        NOT NULL DEFAULT '',
    product_id          TEXT        NOT NULL DEFAULT '',
    occurred_at         TIMESTAMPTZ NOT NULL,
    payload             JSONB       NOT NULL,
    payload_hash        TEXT        NOT NULL CHECK (payload_hash <> ''),
    correlation_id      TEXT        NOT NULL DEFAULT '',
    causation_event_id  TEXT        REFERENCES billing_events(event_id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX billing_events_occurred_idx
    ON billing_events (occurred_at, event_id);

CREATE INDEX billing_events_aggregate_idx
    ON billing_events (aggregate_type, aggregate_id, occurred_at, event_id);

CREATE INDEX billing_events_org_product_idx
    ON billing_events (org_id, product_id, occurred_at, event_id);

CREATE TABLE billing_event_delivery_queue (
    event_id             TEXT        NOT NULL REFERENCES billing_events(event_id) ON DELETE RESTRICT,
    sink                 TEXT        NOT NULL CHECK (sink <> ''),
    generation           INTEGER     NOT NULL DEFAULT 1 CHECK (generation > 0),
    state                TEXT        NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'in_progress', 'retryable_failed', 'dead_letter')),
    attempts             INTEGER     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_attempt_at      TIMESTAMPTZ,
    lease_expires_at     TIMESTAMPTZ,
    leased_by            TEXT        NOT NULL DEFAULT '',
    last_attempt_id      TEXT        NOT NULL DEFAULT '',
    delivery_error       TEXT        NOT NULL DEFAULT '',
    dead_lettered_at     TIMESTAMPTZ,
    dead_letter_reason   TEXT        NOT NULL DEFAULT '',
    operator_note        TEXT        NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, sink, generation)
);

CREATE UNIQUE INDEX billing_event_delivery_active_idx
    ON billing_event_delivery_queue (event_id, sink)
    WHERE state <> 'dead_letter';

CREATE INDEX billing_event_delivery_due_idx
    ON billing_event_delivery_queue (sink, state, next_attempt_at, event_id, generation)
    WHERE state IN ('pending', 'retryable_failed');

CREATE INDEX billing_event_delivery_leased_expired_idx
    ON billing_event_delivery_queue (sink, lease_expires_at, event_id, generation)
    WHERE state = 'in_progress';

CREATE INDEX billing_event_delivery_dead_letter_idx
    ON billing_event_delivery_queue (sink, dead_lettered_at, event_id, generation)
    WHERE state = 'dead_letter';

CREATE TABLE billing_clock_overrides (
    scope_kind    TEXT        NOT NULL CHECK (scope_kind IN ('global', 'org', 'org_product')),
    scope_id      TEXT        NOT NULL DEFAULT '',
    business_now  TIMESTAMPTZ NOT NULL,
    generation    BIGINT      NOT NULL DEFAULT 1 CHECK (generation > 0),
    reason        TEXT        NOT NULL DEFAULT '',
    updated_by    TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (scope_kind, scope_id),
    CHECK ((scope_kind = 'global' AND scope_id = '') OR (scope_kind <> 'global' AND scope_id <> ''))
);

CREATE TRIGGER orgs_set_updated_at BEFORE UPDATE ON orgs FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER products_set_updated_at BEFORE UPDATE ON products FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER credit_buckets_set_updated_at BEFORE UPDATE ON credit_buckets FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER skus_set_updated_at BEFORE UPDATE ON skus FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER plans_set_updated_at BEFORE UPDATE ON plans FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER plan_sku_rates_set_updated_at BEFORE UPDATE ON plan_sku_rates FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER entitlement_policies_set_updated_at BEFORE UPDATE ON entitlement_policies FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER contracts_set_updated_at BEFORE UPDATE ON contracts FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER provider_bindings_set_updated_at BEFORE UPDATE ON provider_bindings FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER payment_methods_set_updated_at BEFORE UPDATE ON payment_methods FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER billing_provider_events_set_updated_at BEFORE UPDATE ON billing_provider_events FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER contract_changes_set_updated_at BEFORE UPDATE ON contract_changes FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER contract_phases_set_updated_at BEFORE UPDATE ON contract_phases FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER contract_entitlement_lines_set_updated_at BEFORE UPDATE ON contract_entitlement_lines FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER billing_cycles_set_updated_at BEFORE UPDATE ON billing_cycles FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER entitlement_periods_set_updated_at BEFORE UPDATE ON entitlement_periods FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER credit_grants_set_updated_at BEFORE UPDATE ON credit_grants FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER billing_windows_set_updated_at BEFORE UPDATE ON billing_windows FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER invoice_finalizations_set_updated_at BEFORE UPDATE ON invoice_finalizations FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER billing_invoices_set_updated_at BEFORE UPDATE ON billing_invoices FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER billing_account_registry_set_updated_at BEFORE UPDATE ON billing_account_registry FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER billing_event_delivery_queue_set_updated_at BEFORE UPDATE ON billing_event_delivery_queue FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
CREATE TRIGGER billing_clock_overrides_set_updated_at BEFORE UPDATE ON billing_clock_overrides FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();

-- River v0.34.0 runtime schema. Keep this section in lockstep with the River
-- module pin; billing domain rows remain the source of truth for business due
-- times and state machines.

CREATE TABLE river_migration(
    line        TEXT        NOT NULL,
    version     BIGINT      NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT river_migration_line_length CHECK (char_length(line) > 0 AND char_length(line) < 128),
    CONSTRAINT river_migration_version_gte_1 CHECK (version >= 1),
    PRIMARY KEY (line, version)
);

CREATE TYPE river_job_state AS ENUM(
    'available',
    'cancelled',
    'completed',
    'discarded',
    'pending',
    'retryable',
    'running',
    'scheduled'
);

CREATE TABLE river_job(
    id             BIGSERIAL PRIMARY KEY,
    state          river_job_state NOT NULL DEFAULT 'available',
    attempt        SMALLINT        NOT NULL DEFAULT 0,
    max_attempts   SMALLINT        NOT NULL,
    attempted_at   TIMESTAMPTZ,
    created_at     TIMESTAMPTZ     NOT NULL DEFAULT now(),
    finalized_at   TIMESTAMPTZ,
    scheduled_at   TIMESTAMPTZ     NOT NULL DEFAULT now(),
    priority       SMALLINT        NOT NULL DEFAULT 1,
    args           JSONB           NOT NULL,
    attempted_by   TEXT[],
    errors         JSONB[],
    kind           TEXT            NOT NULL,
    metadata       JSONB           NOT NULL DEFAULT '{}'::jsonb,
    queue          TEXT            NOT NULL DEFAULT 'default',
    tags           VARCHAR(255)[]  NOT NULL DEFAULT '{}',
    unique_key     BYTEA,
    unique_states  BIT(8),
    CONSTRAINT river_job_finalized_or_finalized_at_null CHECK (
        (finalized_at IS NULL AND state NOT IN ('cancelled', 'completed', 'discarded'))
        OR (finalized_at IS NOT NULL AND state IN ('cancelled', 'completed', 'discarded'))
    ),
    CONSTRAINT river_job_max_attempts_is_positive CHECK (max_attempts > 0),
    CONSTRAINT river_job_priority_in_range CHECK (priority >= 1 AND priority <= 4),
    CONSTRAINT river_job_queue_length CHECK (char_length(queue) > 0 AND char_length(queue) < 128),
    CONSTRAINT river_job_kind_length CHECK (char_length(kind) > 0 AND char_length(kind) < 128)
);

CREATE INDEX river_job_kind ON river_job USING btree(kind);
CREATE INDEX river_job_state_and_finalized_at_index ON river_job USING btree(state, finalized_at) WHERE finalized_at IS NOT NULL;
CREATE INDEX river_job_prioritized_fetching_index ON river_job USING btree(state, queue, priority, scheduled_at, id);
CREATE INDEX river_job_args_index ON river_job USING gin(args);
CREATE INDEX river_job_metadata_index ON river_job USING gin(metadata);

CREATE TABLE river_queue (
    name        TEXT        PRIMARY KEY NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    metadata    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    paused_at   TIMESTAMPTZ,
    updated_at  TIMESTAMPTZ NOT NULL,
    CONSTRAINT river_queue_name_length CHECK (char_length(name) > 0 AND char_length(name) < 128)
);

CREATE UNLOGGED TABLE river_leader(
    elected_at  TIMESTAMPTZ NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    leader_id   TEXT        NOT NULL,
    name        TEXT        PRIMARY KEY DEFAULT 'default',
    CONSTRAINT river_leader_name_length CHECK (name = 'default'),
    CONSTRAINT river_leader_id_length CHECK (char_length(leader_id) > 0 AND char_length(leader_id) < 128)
);

CREATE UNLOGGED TABLE river_client (
    id          TEXT        PRIMARY KEY NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    metadata    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    paused_at   TIMESTAMPTZ,
    updated_at  TIMESTAMPTZ NOT NULL,
    CONSTRAINT river_client_name_length CHECK (char_length(id) > 0 AND char_length(id) < 128)
);

CREATE UNLOGGED TABLE river_client_queue (
    river_client_id     TEXT        NOT NULL REFERENCES river_client (id) ON DELETE CASCADE,
    name                TEXT        NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    max_workers         BIGINT      NOT NULL DEFAULT 0,
    metadata            JSONB       NOT NULL DEFAULT '{}'::jsonb,
    num_jobs_completed  BIGINT      NOT NULL DEFAULT 0,
    num_jobs_running    BIGINT      NOT NULL DEFAULT 0,
    updated_at          TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (river_client_id, name),
    CONSTRAINT river_client_queue_name_length CHECK (char_length(name) > 0 AND char_length(name) < 128),
    CONSTRAINT river_client_queue_num_jobs_completed_zero_or_positive CHECK (num_jobs_completed >= 0),
    CONSTRAINT river_client_queue_num_jobs_running_zero_or_positive CHECK (num_jobs_running >= 0)
);

CREATE OR REPLACE FUNCTION river_job_state_in_bitmask(bitmask BIT(8), state river_job_state)
RETURNS boolean
LANGUAGE SQL
IMMUTABLE
AS $$
    SELECT CASE state
        WHEN 'available' THEN get_bit(bitmask, 7)
        WHEN 'cancelled' THEN get_bit(bitmask, 6)
        WHEN 'completed' THEN get_bit(bitmask, 5)
        WHEN 'discarded' THEN get_bit(bitmask, 4)
        WHEN 'pending'   THEN get_bit(bitmask, 3)
        WHEN 'retryable' THEN get_bit(bitmask, 2)
        WHEN 'running'   THEN get_bit(bitmask, 1)
        WHEN 'scheduled' THEN get_bit(bitmask, 0)
        ELSE 0
    END = 1;
$$;

CREATE UNIQUE INDEX river_job_unique_idx ON river_job (unique_key)
    WHERE unique_key IS NOT NULL
      AND unique_states IS NOT NULL
      AND river_job_state_in_bitmask(unique_states, state);

INSERT INTO river_migration (line, version)
VALUES
    ('main', 1),
    ('main', 2),
    ('main', 3),
    ('main', 4),
    ('main', 5),
    ('main', 6)
ON CONFLICT (line, version) DO NOTHING;
