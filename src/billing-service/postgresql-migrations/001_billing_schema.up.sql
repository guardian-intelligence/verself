CREATE TABLE orgs (
    org_id              TEXT        PRIMARY KEY,
    display_name        TEXT        NOT NULL,
    billing_email       TEXT        NOT NULL DEFAULT '',
    stripe_customer_id  TEXT        NOT NULL DEFAULT '',
    trust_tier          TEXT        NOT NULL CHECK (trust_tier IN ('new', 'established', 'enterprise', 'platform')),
    overage_policy      TEXT        NOT NULL DEFAULT 'hard_cap' CHECK (overage_policy IN ('hard_cap', 'bill_overages')),
    overage_consent_at  TIMESTAMPTZ,
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
    source            TEXT        NOT NULL CHECK (source IN ('free_tier', 'contract', 'purchase', 'promo', 'refund')),
    product_id        TEXT        NOT NULL DEFAULT '',
    scope_type        TEXT        NOT NULL CHECK (scope_type IN ('sku', 'bucket', 'product', 'account')),
    scope_product_id  TEXT        NOT NULL DEFAULT '',
    scope_bucket_id   TEXT        NOT NULL DEFAULT '',
    scope_sku_id      TEXT        NOT NULL DEFAULT '',
    amount_units      BIGINT      NOT NULL CHECK (amount_units >= 0),
    cadence           TEXT        NOT NULL CHECK (cadence IN ('monthly', 'annual')),
    anchor_kind       TEXT        NOT NULL CHECK (anchor_kind IN ('calendar_month', 'contract_phase', 'billing_cycle')),
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

CREATE TABLE contracts (
    contract_id         TEXT        PRIMARY KEY,
    org_id              TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id          TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    display_name        TEXT        NOT NULL DEFAULT '',
    contract_kind       TEXT        NOT NULL CHECK (contract_kind IN ('self_serve', 'enterprise', 'internal')),
    status              TEXT        NOT NULL CHECK (status IN ('draft', 'pending_activation', 'active', 'past_due', 'suspended', 'cancel_scheduled', 'ended', 'voided')),
    payment_state       TEXT        NOT NULL DEFAULT 'pending' CHECK (payment_state IN ('not_required', 'pending', 'paid', 'failed', 'uncollectible', 'refunded')),
    entitlement_state   TEXT        NOT NULL DEFAULT 'scheduled' CHECK (entitlement_state IN ('scheduled', 'active', 'grace', 'closed', 'voided')),
    cadence_kind        TEXT        NOT NULL DEFAULT 'anniversary_monthly' CHECK (cadence_kind IN ('anniversary_monthly', 'calendar_monthly', 'annual', 'manual')),
    billing_anchor_at   TIMESTAMPTZ,
    starts_at           TIMESTAMPTZ NOT NULL,
    ends_at             TIMESTAMPTZ,
    grace_until         TIMESTAMPTZ,
    overage_policy      TEXT        NOT NULL DEFAULT 'hard_cap' CHECK (overage_policy IN ('hard_cap', 'bill_overages')),
    overage_consent_at  TIMESTAMPTZ,
    metadata            JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (ends_at IS NULL OR ends_at > starts_at)
);

CREATE INDEX idx_contracts_org_product_status
    ON contracts (org_id, product_id, status, starts_at DESC, contract_id);

CREATE TABLE provider_bindings (
    binding_id            TEXT        PRIMARY KEY,
    provider              TEXT        NOT NULL CHECK (provider IN ('stripe', 'manual')),
    aggregate_type        TEXT        NOT NULL CHECK (aggregate_type IN ('contract', 'payment_method', 'invoice', 'payment_intent', 'customer')),
    aggregate_id          TEXT        NOT NULL,
    provider_object_type  TEXT        NOT NULL,
    provider_object_id    TEXT        NOT NULL,
    provider_account_id   TEXT        NOT NULL DEFAULT '',
    metadata              JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_provider_bindings_provider_object
    ON provider_bindings (provider, provider_object_type, provider_object_id)
    WHERE provider_object_id <> '';

CREATE INDEX idx_provider_bindings_aggregate
    ON provider_bindings (aggregate_type, aggregate_id);

CREATE TABLE payment_methods (
    payment_method_id           TEXT        PRIMARY KEY,
    org_id                      TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    provider                    TEXT        NOT NULL CHECK (provider IN ('stripe', 'manual')),
    provider_payment_method_id  TEXT        NOT NULL DEFAULT '',
    provider_customer_id        TEXT        NOT NULL DEFAULT '',
    status                      TEXT        NOT NULL CHECK (status IN ('pending', 'active', 'detached', 'failed')),
    is_default                  BOOLEAN     NOT NULL DEFAULT false,
    card_brand                  TEXT        NOT NULL DEFAULT '',
    card_last4                  TEXT        NOT NULL DEFAULT '',
    card_exp_month              INTEGER,
    card_exp_year               INTEGER,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_payment_methods_default_per_org
    ON payment_methods (org_id)
    WHERE is_default AND status = 'active';

CREATE UNIQUE INDEX idx_payment_methods_provider_object
    ON payment_methods (provider, provider_payment_method_id)
    WHERE provider_payment_method_id <> '';

CREATE TABLE billing_provider_events (
    event_id                  TEXT        PRIMARY KEY,
    provider                  TEXT        NOT NULL CHECK (provider IN ('stripe', 'manual')),
    provider_event_id         TEXT        NOT NULL,
    event_type                TEXT        NOT NULL,
    state                     TEXT        NOT NULL CHECK (state IN ('received', 'queued', 'applying', 'applied', 'ignored', 'failed', 'dead_letter')),
    livemode                  BOOLEAN     NOT NULL DEFAULT false,
    provider_created_at       TIMESTAMPTZ,
    received_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    applied_at                TIMESTAMPTZ,
    attempts                  INTEGER     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at           TIMESTAMPTZ,
    last_error                TEXT        NOT NULL DEFAULT '',
    org_id                    TEXT        NOT NULL DEFAULT '',
    product_id                TEXT        NOT NULL DEFAULT '',
    contract_id               TEXT        NOT NULL DEFAULT '',
    invoice_id                TEXT        NOT NULL DEFAULT '',
    payment_method_id         TEXT        NOT NULL DEFAULT '',
    provider_customer_id      TEXT        NOT NULL DEFAULT '',
    provider_invoice_id       TEXT        NOT NULL DEFAULT '',
    provider_payment_intent_id TEXT       NOT NULL DEFAULT '',
    provider_object_type      TEXT        NOT NULL DEFAULT '',
    provider_object_id        TEXT        NOT NULL DEFAULT '',
    payload                   JSONB       NOT NULL,
    normalized_payload        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_event_id)
);

CREATE INDEX idx_billing_provider_events_state_due
    ON billing_provider_events (state, next_attempt_at, received_at, event_id)
    WHERE state IN ('received', 'queued', 'failed');

CREATE TABLE contract_changes (
    change_id             TEXT        PRIMARY KEY,
    contract_id           TEXT        REFERENCES contracts(contract_id) ON DELETE CASCADE,
    org_id                TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id            TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    change_type           TEXT        NOT NULL CHECK (change_type IN ('activation', 'upgrade', 'downgrade', 'cancellation', 'enterprise_amendment', 'cadence_change')),
    state                 TEXT        NOT NULL CHECK (state IN ('requested', 'provider_pending', 'awaiting_payment', 'scheduled', 'applying', 'applied', 'failed', 'canceled')),
    timing                TEXT        NOT NULL CHECK (timing IN ('immediate', 'period_end', 'specific_time')),
    requested_plan_id     TEXT        NOT NULL DEFAULT '',
    target_plan_id        TEXT        NOT NULL DEFAULT '',
    from_phase_id         TEXT        NOT NULL DEFAULT '',
    to_phase_id           TEXT        NOT NULL DEFAULT '',
    provider_invoice_id   TEXT        NOT NULL DEFAULT '',
    invoice_id            TEXT        NOT NULL DEFAULT '',
    requested_effective_at TIMESTAMPTZ,
    actual_effective_at   TIMESTAMPTZ,
    failure_reason        TEXT        NOT NULL DEFAULT '',
    payload               JSONB       NOT NULL DEFAULT '{}'::jsonb,
    requested_by          TEXT        NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_contract_changes_due
    ON contract_changes (state, requested_effective_at, change_id)
    WHERE state IN ('requested', 'scheduled', 'provider_pending', 'awaiting_payment', 'failed');

CREATE TABLE contract_phases (
    phase_id             TEXT        PRIMARY KEY,
    contract_id          TEXT        NOT NULL REFERENCES contracts(contract_id) ON DELETE CASCADE,
    org_id               TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id           TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    plan_id              TEXT        NOT NULL DEFAULT '',
    phase_kind           TEXT        NOT NULL CHECK (phase_kind IN ('catalog_plan', 'bespoke', 'internal')),
    state                TEXT        NOT NULL CHECK (state IN ('scheduled', 'pending_payment', 'active', 'grace', 'superseded', 'closed', 'voided')),
    payment_state        TEXT        NOT NULL DEFAULT 'pending' CHECK (payment_state IN ('not_required', 'pending', 'paid', 'failed', 'uncollectible', 'refunded')),
    entitlement_state    TEXT        NOT NULL DEFAULT 'scheduled' CHECK (entitlement_state IN ('scheduled', 'active', 'grace', 'closed', 'voided')),
    cadence_kind         TEXT        NOT NULL DEFAULT 'anniversary_monthly' CHECK (cadence_kind IN ('anniversary_monthly', 'calendar_monthly', 'annual', 'manual')),
    effective_start      TIMESTAMPTZ NOT NULL,
    effective_end        TIMESTAMPTZ,
    recurrence_anchor_at TIMESTAMPTZ NOT NULL,
    grace_until          TIMESTAMPTZ,
    overage_policy       TEXT        NOT NULL DEFAULT 'hard_cap' CHECK (overage_policy IN ('hard_cap', 'bill_overages')),
    rate_context         JSONB       NOT NULL DEFAULT '{}'::jsonb,
    metadata             JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (effective_end IS NULL OR effective_end > effective_start)
);

CREATE INDEX idx_contract_phases_active_lookup
    ON contract_phases (org_id, product_id, state, effective_start, effective_end, phase_id);

CREATE TABLE contract_entitlement_lines (
    line_id          TEXT        PRIMARY KEY,
    contract_id      TEXT        NOT NULL REFERENCES contracts(contract_id) ON DELETE CASCADE,
    phase_id         TEXT        NOT NULL REFERENCES contract_phases(phase_id) ON DELETE CASCADE,
    product_id       TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    policy_id        TEXT        NOT NULL DEFAULT '',
    policy_version   TEXT        NOT NULL DEFAULT 'v1',
    scope_type       TEXT        NOT NULL CHECK (scope_type IN ('sku', 'bucket', 'product', 'account')),
    scope_product_id TEXT        NOT NULL DEFAULT '',
    scope_bucket_id  TEXT        NOT NULL DEFAULT '',
    scope_sku_id     TEXT        NOT NULL DEFAULT '',
    amount_units     BIGINT      NOT NULL CHECK (amount_units >= 0),
    cadence          TEXT        NOT NULL CHECK (cadence IN ('monthly', 'annual')),
    proration_mode   TEXT        NOT NULL CHECK (proration_mode IN ('none', 'prorate_by_time_left')),
    active           BOOLEAN     NOT NULL DEFAULT true,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (scope_type = 'sku' AND scope_product_id <> '' AND scope_bucket_id <> '' AND scope_sku_id <> '')
        OR (scope_type = 'bucket' AND scope_product_id <> '' AND scope_bucket_id <> '' AND scope_sku_id = '')
        OR (scope_type = 'product' AND scope_product_id <> '' AND scope_bucket_id = '' AND scope_sku_id = '')
        OR (scope_type = 'account' AND scope_product_id = '' AND scope_bucket_id = '' AND scope_sku_id = '')
    )
);

CREATE INDEX idx_contract_entitlement_lines_phase
    ON contract_entitlement_lines (phase_id, active, line_id);

CREATE TABLE billing_cycles (
    cycle_id             TEXT        PRIMARY KEY,
    org_id               TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id           TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    predecessor_cycle_id TEXT        REFERENCES billing_cycles(cycle_id),
    cadence_kind         TEXT        NOT NULL CHECK (cadence_kind IN ('anniversary_monthly', 'calendar_monthly', 'annual', 'manual')),
    anchor_at            TIMESTAMPTZ NOT NULL,
    cycle_seq            BIGINT      NOT NULL CHECK (cycle_seq >= 0),
    starts_at            TIMESTAMPTZ NOT NULL,
    ends_at              TIMESTAMPTZ NOT NULL,
    status               TEXT        NOT NULL CHECK (status IN ('open', 'closing', 'closed_for_usage', 'invoice_finalizing', 'invoiced', 'blocked', 'voided')),
    finalization_due_at  TIMESTAMPTZ NOT NULL,
    closed_for_usage_at  TIMESTAMPTZ,
    invoice_id           TEXT        NOT NULL DEFAULT '',
    block_reason         TEXT        NOT NULL DEFAULT '',
    metadata             JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (ends_at > starts_at)
);

CREATE UNIQUE INDEX idx_billing_cycles_open_per_org_product
    ON billing_cycles (org_id, product_id)
    WHERE status = 'open';

CREATE UNIQUE INDEX idx_billing_cycles_chain
    ON billing_cycles (org_id, product_id, anchor_at, cycle_seq);

CREATE INDEX idx_billing_cycles_due
    ON billing_cycles (status, ends_at, finalization_due_at, cycle_id)
    WHERE status IN ('open', 'closing', 'closed_for_usage', 'blocked');

CREATE TABLE entitlement_periods (
    period_id            TEXT        PRIMARY KEY,
    cycle_id             TEXT        NOT NULL DEFAULT '',
    org_id               TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id           TEXT        NOT NULL DEFAULT '',
    source               TEXT        NOT NULL CHECK (source IN ('free_tier', 'contract', 'purchase', 'promo', 'refund')),
    policy_id            TEXT        NOT NULL DEFAULT '',
    contract_id          TEXT        NOT NULL DEFAULT '',
    phase_id             TEXT        NOT NULL DEFAULT '',
    line_id              TEXT        NOT NULL DEFAULT '',
    scope_type           TEXT        NOT NULL CHECK (scope_type IN ('sku', 'bucket', 'product', 'account')),
    scope_product_id     TEXT        NOT NULL DEFAULT '',
    scope_bucket_id      TEXT        NOT NULL DEFAULT '',
    scope_sku_id         TEXT        NOT NULL DEFAULT '',
    amount_units         BIGINT      NOT NULL CHECK (amount_units >= 0),
    period_start         TIMESTAMPTZ NOT NULL,
    period_end           TIMESTAMPTZ NOT NULL,
    policy_version       TEXT        NOT NULL DEFAULT 'v1',
    payment_state        TEXT        NOT NULL DEFAULT 'not_required' CHECK (payment_state IN ('not_required', 'pending', 'paid', 'failed', 'uncollectible', 'refunded')),
    entitlement_state    TEXT        NOT NULL DEFAULT 'scheduled' CHECK (entitlement_state IN ('scheduled', 'active', 'grace', 'closed', 'voided')),
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
    ),
    CHECK (
        (source = 'contract' AND contract_id <> '' AND phase_id <> '' AND line_id <> '')
        OR (source <> 'contract' AND contract_id = '' AND phase_id = '' AND line_id = '')
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
    source                  TEXT        NOT NULL CHECK (source IN ('free_tier', 'contract', 'purchase', 'promo', 'refund', 'receivable')),
    source_reference_id     TEXT        NOT NULL DEFAULT '',
    entitlement_period_id   TEXT        NOT NULL DEFAULT '',
    policy_version          TEXT        NOT NULL DEFAULT '',
    deposit_transfer_id     TEXT        NOT NULL DEFAULT '',
    ledger_state            TEXT        NOT NULL DEFAULT 'posted' CHECK (ledger_state IN ('pending', 'posted', 'failed')),
    starts_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    period_start            TIMESTAMPTZ,
    period_end              TIMESTAMPTZ,
    expires_at              TIMESTAMPTZ,
    closed_at               TIMESTAMPTZ,
    closed_reason           TEXT        NOT NULL DEFAULT '',
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

CREATE TABLE billing_windows (
    window_id               TEXT        PRIMARY KEY,
    cycle_id                TEXT        NOT NULL DEFAULT '',
    org_id                  TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    actor_id                TEXT        NOT NULL,
    product_id              TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    pricing_contract_id     TEXT        NOT NULL DEFAULT '',
    pricing_phase_id        TEXT        NOT NULL DEFAULT '',
    pricing_plan_id         TEXT        NOT NULL DEFAULT '',
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
    writeoff_reason         TEXT        NOT NULL DEFAULT '',
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

CREATE INDEX idx_billing_windows_cycle
    ON billing_windows (cycle_id, state, window_start, window_id);

CREATE TABLE billing_invoices (
    invoice_id                  TEXT        PRIMARY KEY,
    cycle_id                    TEXT        NOT NULL REFERENCES billing_cycles(cycle_id),
    org_id                      TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id                  TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    invoice_number              TEXT        NOT NULL,
    invoice_kind                TEXT        NOT NULL CHECK (invoice_kind IN ('cycle', 'activation', 'adjustment', 'credit_note')),
    status                      TEXT        NOT NULL CHECK (status IN ('draft', 'finalizing', 'issued', 'paid', 'payment_failed', 'voided', 'blocked')),
    payment_status              TEXT        NOT NULL DEFAULT 'n_a' CHECK (payment_status IN ('n_a', 'pending', 'paid', 'failed', 'uncollectible', 'refunded')),
    period_start                TIMESTAMPTZ NOT NULL,
    period_end                  TIMESTAMPTZ NOT NULL,
    issued_at                   TIMESTAMPTZ,
    due_at                      TIMESTAMPTZ,
    currency                    TEXT        NOT NULL DEFAULT 'usd',
    total_due_units             BIGINT      NOT NULL DEFAULT 0 CHECK (total_due_units >= 0),
    subtotal_units              BIGINT      NOT NULL DEFAULT 0 CHECK (subtotal_units >= 0),
    tax_units                   BIGINT      NOT NULL DEFAULT 0 CHECK (tax_units >= 0),
    adjustment_units            BIGINT      NOT NULL DEFAULT 0 CHECK (adjustment_units >= 0),
    recipient_email             TEXT        NOT NULL DEFAULT '',
    recipient_name              TEXT        NOT NULL DEFAULT '',
    invoice_snapshot_json       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    rendered_html               TEXT        NOT NULL DEFAULT '',
    content_hash                TEXT        NOT NULL DEFAULT '',
    stripe_invoice_id           TEXT        NOT NULL DEFAULT '',
    stripe_hosted_invoice_url   TEXT        NOT NULL DEFAULT '',
    stripe_invoice_pdf_url      TEXT        NOT NULL DEFAULT '',
    stripe_payment_intent_id    TEXT        NOT NULL DEFAULT '',
    voided_by_invoice_id        TEXT        NOT NULL DEFAULT '',
    block_reason                TEXT        NOT NULL DEFAULT '',
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (invoice_number),
    CHECK (period_end > period_start)
);

CREATE UNIQUE INDEX idx_billing_invoices_cycle_kind
    ON billing_invoices (cycle_id, invoice_kind)
    WHERE status <> 'voided';

CREATE TABLE invoice_line_items (
    line_item_id       TEXT        PRIMARY KEY,
    invoice_id         TEXT        NOT NULL REFERENCES billing_invoices(invoice_id) ON DELETE CASCADE,
    line_type          TEXT        NOT NULL CHECK (line_type IN ('usage', 'recurring_charge', 'adjustment', 'tax', 'rounding')),
    product_id         TEXT        NOT NULL DEFAULT '',
    bucket_id          TEXT        NOT NULL DEFAULT '',
    sku_id             TEXT        NOT NULL DEFAULT '',
    description        TEXT        NOT NULL,
    quantity           NUMERIC     NOT NULL DEFAULT 0,
    quantity_unit      TEXT        NOT NULL DEFAULT '',
    unit_rate          BIGINT      NOT NULL DEFAULT 0 CHECK (unit_rate >= 0),
    charge_units       BIGINT      NOT NULL DEFAULT 0 CHECK (charge_units >= 0),
    free_tier_units    BIGINT      NOT NULL DEFAULT 0 CHECK (free_tier_units >= 0),
    contract_units     BIGINT      NOT NULL DEFAULT 0 CHECK (contract_units >= 0),
    purchase_units     BIGINT      NOT NULL DEFAULT 0 CHECK (purchase_units >= 0),
    promo_units        BIGINT      NOT NULL DEFAULT 0 CHECK (promo_units >= 0),
    refund_units       BIGINT      NOT NULL DEFAULT 0 CHECK (refund_units >= 0),
    receivable_units   BIGINT      NOT NULL DEFAULT 0 CHECK (receivable_units >= 0),
    adjustment_units   BIGINT      NOT NULL DEFAULT 0 CHECK (adjustment_units >= 0),
    metadata           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_invoice_line_items_invoice
    ON invoice_line_items (invoice_id, line_type, line_item_id);

CREATE TABLE invoice_adjustments (
    adjustment_id              TEXT        PRIMARY KEY,
    invoice_id                 TEXT        NOT NULL REFERENCES billing_invoices(invoice_id) ON DELETE CASCADE,
    invoice_finalization_id    TEXT        NOT NULL,
    org_id                     TEXT        NOT NULL REFERENCES orgs(org_id) ON DELETE CASCADE,
    product_id                 TEXT        NOT NULL REFERENCES products(product_id) ON DELETE CASCADE,
    window_id                  TEXT        NOT NULL DEFAULT '',
    sku_id                     TEXT        NOT NULL DEFAULT '',
    amount_units               BIGINT      NOT NULL CHECK (amount_units >= 0),
    adjustment_type            TEXT        NOT NULL CHECK (adjustment_type IN ('credit', 'debit')),
    adjustment_source          TEXT        NOT NULL CHECK (adjustment_source IN ('system_policy', 'manual_admin', 'sla', 'campaign')),
    reason_code                TEXT        NOT NULL,
    policy_version             TEXT        NOT NULL DEFAULT 'v1',
    customer_visible           BOOLEAN     NOT NULL DEFAULT false,
    recoverable                BOOLEAN     NOT NULL DEFAULT false,
    affects_customer_balance   BOOLEAN     NOT NULL DEFAULT false,
    metadata                   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_invoice_adjustments_deterministic_system
    ON invoice_adjustments (invoice_finalization_id, org_id, product_id, window_id, sku_id, reason_code, policy_version)
    WHERE adjustment_source = 'system_policy';

CREATE TABLE invoice_number_allocators (
    issuer_id    TEXT        NOT NULL,
    invoice_year INTEGER     NOT NULL CHECK (invoice_year >= 2000),
    next_number  BIGINT      NOT NULL CHECK (next_number > 0),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issuer_id, invoice_year)
);

CREATE TABLE billing_account_registry (
    account_kind             TEXT        PRIMARY KEY,
    tigerbeetle_account_id   TEXT        NOT NULL,
    description              TEXT        NOT NULL DEFAULT '',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE billing_events (
    event_id             TEXT        PRIMARY KEY CHECK (event_id <> ''),
    event_type           TEXT        NOT NULL CHECK (event_type <> ''),
    event_version        INTEGER     NOT NULL DEFAULT 1 CHECK (event_version > 0),
    aggregate_type       TEXT        NOT NULL CHECK (aggregate_type <> ''),
    aggregate_id         TEXT        NOT NULL CHECK (aggregate_id <> ''),
    org_id               TEXT        NOT NULL,
    product_id           TEXT        NOT NULL DEFAULT '',
    occurred_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    payload              JSONB       NOT NULL,
    payload_hash         TEXT        NOT NULL CHECK (payload_hash <> ''),
    correlation_id       TEXT        NOT NULL DEFAULT '',
    causation_event_id   TEXT        NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_billing_events_occurred
    ON billing_events (occurred_at, event_id);

CREATE INDEX idx_billing_events_aggregate
    ON billing_events (aggregate_type, aggregate_id, occurred_at, event_id);

CREATE INDEX idx_billing_events_org_product
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
    PRIMARY KEY (event_id, sink)
);

CREATE INDEX idx_billing_event_delivery_due
    ON billing_event_delivery_queue (sink, state, next_attempt_at, event_id)
    WHERE state IN ('pending', 'retryable_failed');

CREATE INDEX idx_billing_event_delivery_leased_expired
    ON billing_event_delivery_queue (sink, lease_expires_at, event_id)
    WHERE state = 'in_progress';

CREATE INDEX idx_billing_event_delivery_dead_letter
    ON billing_event_delivery_queue (sink, dead_lettered_at, event_id)
    WHERE state = 'dead_letter';
