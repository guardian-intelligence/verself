-- name: InsertBillingWindow :exec
INSERT INTO billing_windows (
    window_id, cycle_id, org_id, actor_id, product_id, pricing_contract_id, pricing_phase_id, pricing_plan_id,
    source_type, source_ref, source_fingerprint, billing_job_id, window_seq, state, reservation_shape, reserved_quantity,
    reserved_charge_units, pricing_phase, allocation, rate_context, funding_legs, ledger_correlation_id, window_start, expires_at, renew_by
) VALUES (
    sqlc.arg(window_id),
    sqlc.arg(cycle_id),
    sqlc.arg(org_id),
    sqlc.arg(actor_id),
    sqlc.arg(product_id),
    NULLIF(sqlc.arg(pricing_contract_id), ''),
    NULLIF(sqlc.arg(pricing_phase_id), ''),
    NULLIF(sqlc.arg(pricing_plan_id), ''),
    sqlc.arg(source_type),
    sqlc.arg(source_ref),
    sqlc.arg(source_fingerprint),
    NULLIF(sqlc.arg(billing_job_id), ''),
    sqlc.arg(window_seq),
    'reserved',
    sqlc.arg(reservation_shape),
    sqlc.arg(reserved_quantity),
    sqlc.arg(reserved_charge_units),
    sqlc.arg(pricing_phase),
    sqlc.arg(allocation),
    sqlc.arg(rate_context),
    sqlc.arg(funding_legs),
    sqlc.arg(ledger_correlation_id)::bytea,
    sqlc.arg(window_start),
    sqlc.arg(expires_at),
    sqlc.arg(renew_by)
);

-- name: ActivateBillingWindow :execrows
UPDATE billing_windows
SET state = 'active',
    window_start = sqlc.arg(activated_at),
    activated_at = sqlc.arg(activated_at),
    expires_at = sqlc.arg(expires_at),
    renew_by = sqlc.arg(renew_by)
WHERE window_id = sqlc.arg(window_id)
  AND state = 'reserved'
  AND activated_at IS NULL;

-- name: PrepareBillingWindowSettlement :execrows
UPDATE billing_windows
SET state = 'settling',
    actual_quantity = sqlc.arg(actual_quantity),
    billable_quantity = sqlc.arg(billable_quantity),
    writeoff_quantity = sqlc.arg(writeoff_quantity),
    billed_charge_units = sqlc.arg(billed_charge_units),
    writeoff_charge_units = sqlc.arg(writeoff_charge_units),
    writeoff_reason = sqlc.arg(writeoff_reason),
    usage_summary = sqlc.arg(usage_summary),
    funding_legs = sqlc.arg(funding_legs),
    settled_at = sqlc.arg(settled_at)
WHERE window_id = sqlc.arg(window_id)
  AND state IN ('reserved','active');

-- name: UpdateWindowProjectionError :exec
UPDATE billing_windows
SET last_projection_error = sqlc.arg(projection_error)
WHERE window_id = sqlc.arg(window_id);

-- name: ListPendingMeteringWindowIDs :many
SELECT window_id
FROM billing_windows
WHERE state = 'settled'
  AND metering_projected_at IS NULL
ORDER BY settled_at, window_id
LIMIT sqlc.arg(limit_count);

-- name: LockMeteringProjectionWindow :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(lock_key), 0));

-- name: GetMeteringProjectionStateForUpdate :one
SELECT state, metering_projected_at
FROM billing_windows
WHERE window_id = sqlc.arg(window_id)
FOR UPDATE;

-- name: MarkMeteringProjected :exec
UPDATE billing_windows
SET metering_projected_at = sqlc.arg(projected_at),
    last_projection_error = ''
WHERE window_id = sqlc.arg(window_id);

-- name: VoidBillingWindow :execrows
UPDATE billing_windows
SET state = 'voided'
WHERE window_id = sqlc.arg(window_id)
  AND state IN ('reserved','active');

-- name: VoidPendingWindowLedgerLegs :exec
UPDATE billing_window_ledger_legs
SET amount_posted = 0,
    amount_voided = amount_reserved,
    state = 'voided'
WHERE window_id = sqlc.arg(window_id)
  AND state = 'pending';

-- name: GetWindowLedgerCorrelationID :one
SELECT ledger_correlation_id::bytea AS ledger_correlation_id
FROM billing_windows
WHERE window_id = sqlc.arg(window_id);

-- name: ListPendingWindowLedgerLegsForUpdate :many
SELECT leg_seq,
       COALESCE(grant_id, '') AS grant_id,
       COALESCE(grant_account_id::bytea, decode('00000000000000000000000000000000','hex'))::bytea AS grant_account_id,
       COALESCE(settlement_transfer_id::bytea, decode('00000000000000000000000000000000','hex'))::bytea AS settlement_transfer_id,
       component_sku_id,
       component_bucket_id,
       source,
       scope_type,
       scope_product_id,
       scope_bucket_id,
       scope_sku_id,
       plan_id,
       amount_reserved
FROM billing_window_ledger_legs
WHERE window_id = sqlc.arg(window_id)
  AND state = 'pending'
ORDER BY leg_seq
FOR UPDATE;

-- name: StoreWindowSettlementAmounts :exec
UPDATE billing_window_ledger_legs
SET settlement_transfer_id = sqlc.arg(settlement_transfer_id)::bytea,
    amount_posted = sqlc.arg(amount_posted),
    amount_voided = sqlc.arg(amount_voided)
WHERE window_id = sqlc.arg(window_id)
  AND leg_seq = sqlc.arg(leg_seq);

-- name: InsertWindowLedgerLeg :exec
INSERT INTO billing_window_ledger_legs (
    window_id, leg_seq, grant_id, grant_account_id,
    component_sku_id, component_bucket_id, source, scope_type, scope_product_id, scope_bucket_id, scope_sku_id,
    plan_id, amount_reserved, state
) VALUES (
    sqlc.arg(window_id),
    sqlc.arg(leg_seq),
    NULLIF(sqlc.arg(grant_id), ''),
    sqlc.arg(grant_account_id)::bytea,
    sqlc.arg(component_sku_id),
    sqlc.arg(component_bucket_id),
    sqlc.arg(source),
    sqlc.arg(scope_type),
    sqlc.arg(scope_product_id),
    sqlc.arg(scope_bucket_id),
    sqlc.arg(scope_sku_id),
    sqlc.arg(plan_id),
    sqlc.arg(amount_reserved),
    'pending'
)
ON CONFLICT (window_id, leg_seq) DO NOTHING;

-- name: MarkWindowSettled :execrows
UPDATE billing_windows
SET state = 'settled'
WHERE window_id = sqlc.arg(window_id)
  AND state = 'settling';

-- name: MarkPendingWindowLedgerLegsPostedOrVoided :exec
UPDATE billing_window_ledger_legs
SET state = CASE WHEN amount_posted > 0 THEN 'posted' ELSE 'voided' END
WHERE window_id = sqlc.arg(window_id)
  AND state = 'pending';

-- name: GetWindowIDBySource :one
SELECT window_id
FROM billing_windows
WHERE org_id = sqlc.arg(org_id)
  AND product_id = sqlc.arg(product_id)
  AND source_type = sqlc.arg(source_type)
  AND source_ref = sqlc.arg(source_ref)
  AND window_seq = sqlc.arg(window_seq);

-- name: GetBillingWindow :one
SELECT window_id,
       cycle_id,
       org_id,
       actor_id,
       product_id,
       COALESCE(pricing_contract_id, '') AS pricing_contract_id,
       COALESCE(pricing_phase_id, '') AS pricing_phase_id,
       COALESCE(pricing_plan_id, '') AS pricing_plan_id,
       source_type,
       source_ref,
       source_fingerprint,
       window_seq,
       state,
       reservation_shape,
       reserved_quantity,
       actual_quantity,
       billable_quantity,
       writeoff_quantity,
       reserved_charge_units,
       billed_charge_units,
       writeoff_charge_units,
       pricing_phase,
       allocation,
       rate_context,
       usage_summary,
       funding_legs,
       window_start,
       activated_at,
       expires_at,
       renew_by,
       settled_at,
       created_at
FROM billing_windows
WHERE window_id = sqlc.arg(window_id);

-- name: LockWindowState :one
SELECT state
FROM billing_windows
WHERE window_id = sqlc.arg(window_id)
FOR UPDATE;

-- name: LockOrgProductBilling :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(lock_key), 0));

-- name: GetOrgBillingState :one
SELECT state, overage_policy
FROM orgs
WHERE org_id = sqlc.arg(org_id);

-- name: InsertDefaultOrg :exec
INSERT INTO orgs (org_id, display_name, trust_tier)
VALUES (sqlc.arg(org_id), sqlc.arg(display_name), 'new')
ON CONFLICT DO NOTHING;

-- name: LoadPricingContext :one
WITH active_phase AS (
    SELECT cp.contract_id, cp.phase_id, cp.plan_id, c.overage_policy
    FROM contract_phases cp
    JOIN contracts c ON c.contract_id = cp.contract_id
    WHERE cp.org_id = sqlc.arg(org_id)
      AND cp.product_id = sqlc.arg(product_id)
      AND cp.state IN ('active','grace')
      AND cp.effective_start <= sqlc.arg(now)
      AND (cp.effective_end IS NULL OR cp.effective_end > sqlc.arg(now))
      AND c.state IN ('active','past_due','cancel_scheduled')
    ORDER BY cp.effective_start DESC, cp.phase_id DESC
    LIMIT 1
),
chosen AS (
    SELECT COALESCE((SELECT plan_id FROM active_phase), (SELECT plan_id FROM plans WHERE product_id = sqlc.arg(product_id) AND active AND is_default ORDER BY plan_id LIMIT 1)) AS plan_id,
           COALESCE((SELECT contract_id FROM active_phase), '') AS contract_id,
           COALESCE((SELECT phase_id FROM active_phase), '') AS phase_id,
           COALESCE((SELECT overage_policy FROM active_phase), 'block') AS overage_policy
)
SELECT p.plan_id, p.billing_mode, chosen.contract_id::text AS contract_id, chosen.phase_id::text AS phase_id, chosen.overage_policy::text AS overage_policy, p.currency
FROM chosen
JOIN plans p ON p.plan_id = chosen.plan_id;

-- name: ListActivePlanSKURates :many
SELECT r.sku_id,
       r.unit_rate,
       s.bucket_id,
       s.display_name AS sku_display_name,
       s.quantity_unit,
       b.display_name AS bucket_display_name,
       b.sort_order
FROM plan_sku_rates r
JOIN skus s ON s.sku_id = r.sku_id
JOIN credit_buckets b ON b.bucket_id = s.bucket_id
WHERE r.plan_id = sqlc.arg(plan_id)
  AND r.active
  AND r.active_from <= sqlc.arg(now)
  AND (r.active_until IS NULL OR r.active_until > sqlc.arg(now))
ORDER BY r.sku_id;

-- name: ListGrantBalancesForReservation :many
SELECT g.grant_id,
       g.scope_type,
       COALESCE(g.scope_product_id, '') AS scope_product_id,
       COALESCE(g.scope_bucket_id, '') AS scope_bucket_id,
       COALESCE(g.scope_sku_id, '') AS scope_sku_id,
       g.amount,
       g.source,
       g.source_reference_id,
       COALESCE(g.entitlement_period_id, '') AS entitlement_period_id,
       g.policy_version,
       COALESCE(cp.plan_id, '') AS plan_id,
       COALESCE(pl.tier, '') AS plan_tier,
       COALESCE(pl.display_name, '') AS plan_display_name,
       g.starts_at,
       g.period_start,
       g.period_end,
       g.expires_at,
       g.account_id::bytea AS account_id
FROM credit_grants g
LEFT JOIN entitlement_periods ep ON ep.period_id = g.entitlement_period_id
LEFT JOIN contract_phases cp ON cp.phase_id = ep.phase_id
LEFT JOIN plans pl ON pl.plan_id = cp.plan_id
WHERE g.org_id = sqlc.arg(org_id)
  AND g.closed_at IS NULL
  AND (sqlc.arg(product_id)::text = '' OR COALESCE(g.scope_product_id, sqlc.arg(product_id)::text) = sqlc.arg(product_id)::text OR g.scope_type = 'account')
  AND g.starts_at <= sqlc.arg(now)
  AND (g.expires_at IS NULL OR g.expires_at > sqlc.arg(now))
ORDER BY CASE g.source WHEN 'free_tier' THEN 1 WHEN 'contract' THEN 2 WHEN 'promo' THEN 3 WHEN 'refund' THEN 4 WHEN 'purchase' THEN 5 ELSE 6 END,
         g.starts_at,
         g.grant_id
FOR UPDATE OF g;
