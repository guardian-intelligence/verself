-- name: ListActiveFreeTierPolicies :many
SELECT policy_id,
       scope_type,
       COALESCE(scope_product_id, '') AS scope_product_id,
       COALESCE(scope_bucket_id, '') AS scope_bucket_id,
       COALESCE(scope_sku_id, '') AS scope_sku_id,
       amount_units,
       policy_version
FROM entitlement_policies
WHERE source = 'free_tier'
  AND product_id = sqlc.arg(product_id)
  AND active_from <= sqlc.arg(now)
  AND (active_until IS NULL OR active_until > sqlc.arg(now))
ORDER BY policy_id;

-- name: ListActiveContractEntitlementLines :many
SELECT l.line_id,
       l.phase_id,
       l.contract_id,
       l.policy_id,
       l.scope_type,
       COALESCE(l.scope_product_id, '') AS scope_product_id,
       COALESCE(l.scope_bucket_id, '') AS scope_bucket_id,
       COALESCE(l.scope_sku_id, '') AS scope_sku_id,
       l.amount_units,
       l.policy_version,
       COALESCE(p.plan_id, '') AS plan_id
FROM contract_entitlement_lines l
JOIN contract_phases p ON p.phase_id = l.phase_id
JOIN contracts c ON c.contract_id = l.contract_id
WHERE l.org_id = sqlc.arg(org_id)
  AND l.product_id = sqlc.arg(product_id)
  AND p.state IN ('active', 'grace')
  AND p.effective_start <= sqlc.arg(now)
  AND (p.effective_end IS NULL OR p.effective_end > sqlc.arg(now))
  AND l.active_from <= sqlc.arg(now)
  AND (l.active_until IS NULL OR l.active_until > sqlc.arg(now))
  AND c.state IN ('active', 'past_due', 'cancel_scheduled')
ORDER BY l.line_id;

-- name: InsertEntitlementPeriod :exec
INSERT INTO entitlement_periods (
    period_id, org_id, product_id, cycle_id, source, policy_id, contract_id, phase_id, line_id,
    scope_type, scope_product_id, scope_bucket_id, scope_sku_id, amount_units, period_start, period_end,
    policy_version, payment_state, entitlement_state, calculation_kind, source_reference_id, created_reason
) VALUES (
    sqlc.arg(period_id),
    sqlc.arg(org_id),
    sqlc.arg(product_id),
    sqlc.arg(cycle_id),
    sqlc.arg(source),
    NULLIF(sqlc.arg(policy_id), ''),
    NULLIF(sqlc.arg(contract_id), ''),
    NULLIF(sqlc.arg(phase_id), ''),
    NULLIF(sqlc.arg(line_id), ''),
    sqlc.arg(scope_type),
    NULLIF(sqlc.arg(scope_product_id), ''),
    NULLIF(sqlc.arg(scope_bucket_id), ''),
    NULLIF(sqlc.arg(scope_sku_id), ''),
    sqlc.arg(amount_units),
    sqlc.arg(period_start),
    sqlc.arg(period_end),
    sqlc.arg(policy_version),
    sqlc.arg(payment_state),
    sqlc.arg(entitlement_state),
    sqlc.arg(calculation_kind),
    sqlc.arg(source_reference_id),
    'materialized'
)
ON CONFLICT (org_id, source, source_reference_id) DO NOTHING;

-- name: InsertCreditGrantForEntitlement :execrows
INSERT INTO credit_grants (
    grant_id, org_id, scope_type, scope_product_id, scope_bucket_id, scope_sku_id, amount, source,
    source_reference_id, entitlement_period_id, policy_version, starts_at, period_start, period_end, expires_at,
    account_id, deposit_transfer_id, ledger_posting_state
) VALUES (
    sqlc.arg(grant_id),
    sqlc.arg(org_id),
    sqlc.arg(scope_type),
    NULLIF(sqlc.arg(scope_product_id), ''),
    NULLIF(sqlc.arg(scope_bucket_id), ''),
    NULLIF(sqlc.arg(scope_sku_id), ''),
    sqlc.arg(amount),
    sqlc.arg(source),
    sqlc.arg(source_reference_id),
    sqlc.arg(entitlement_period_id),
    sqlc.arg(policy_version),
    sqlc.arg(starts_at),
    sqlc.arg(period_start),
    sqlc.arg(period_end),
    sqlc.arg(period_end),
    sqlc.arg(account_id)::bytea,
    sqlc.arg(deposit_transfer_id)::bytea,
    'pending'
)
ON CONFLICT (org_id, source, scope_type, scope_product_id, scope_bucket_id, scope_sku_id, source_reference_id) DO NOTHING;

-- name: ReopenMaterializedEntitlementPeriod :exec
UPDATE entitlement_periods
SET cycle_id = sqlc.arg(cycle_id),
    amount_units = sqlc.arg(amount_units),
    period_start = sqlc.arg(period_start),
    period_end = sqlc.arg(period_end),
    policy_version = sqlc.arg(policy_version),
    payment_state = sqlc.arg(payment_state),
    entitlement_state = sqlc.arg(entitlement_state),
    calculation_kind = sqlc.arg(calculation_kind),
    metadata = (metadata - 'voided_by') || jsonb_build_object('reopened_by', 'entitlement-materializer'),
    updated_at = now()
WHERE org_id = sqlc.arg(org_id)
  AND source = sqlc.arg(source)
  AND source_reference_id = sqlc.arg(source_reference_id)
  AND entitlement_state = 'voided';

-- name: ReopenMaterializedCreditGrant :exec
UPDATE credit_grants
SET entitlement_period_id = sqlc.arg(entitlement_period_id),
    policy_version = sqlc.arg(policy_version),
    starts_at = sqlc.arg(period_start),
    period_start = sqlc.arg(period_start),
    period_end = sqlc.arg(period_end),
    expires_at = sqlc.arg(period_end),
    closed_at = NULL,
    closed_reason = '',
    metadata = (metadata - 'closed_by') || jsonb_build_object('reopened_by', 'entitlement-materializer'),
    updated_at = now()
WHERE org_id = sqlc.arg(org_id)
  AND source = sqlc.arg(source)
  AND scope_type = sqlc.arg(scope_type)
  AND COALESCE(scope_product_id, '') = sqlc.arg(scope_product_id)
  AND COALESCE(scope_bucket_id, '') = sqlc.arg(scope_bucket_id)
  AND COALESCE(scope_sku_id, '') = sqlc.arg(scope_sku_id)
  AND source_reference_id = sqlc.arg(source_reference_id)
  AND closed_at IS NOT NULL;

-- name: InsertManualCreditGrant :exec
INSERT INTO credit_grants (
    grant_id, org_id, scope_type, scope_product_id, scope_bucket_id, scope_sku_id, amount, source,
    source_reference_id, entitlement_period_id, policy_version, starts_at, expires_at,
    account_id, deposit_transfer_id, ledger_posting_state
)
VALUES (
    sqlc.arg(grant_id),
    sqlc.arg(org_id),
    sqlc.arg(scope_type),
    NULLIF(sqlc.arg(scope_product_id), ''),
    NULLIF(sqlc.arg(scope_bucket_id), ''),
    NULLIF(sqlc.arg(scope_sku_id), ''),
    sqlc.arg(amount),
    sqlc.arg(source),
    sqlc.arg(source_reference_id),
    NULLIF(sqlc.arg(entitlement_period_id), ''),
    sqlc.arg(policy_version),
    sqlc.arg(starts_at),
    sqlc.narg(expires_at),
    sqlc.arg(account_id)::bytea,
    sqlc.arg(deposit_transfer_id)::bytea,
    'pending'
)
ON CONFLICT (org_id, source, scope_type, scope_product_id, scope_bucket_id, scope_sku_id, source_reference_id) DO NOTHING;
