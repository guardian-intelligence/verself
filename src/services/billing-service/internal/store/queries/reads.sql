-- name: ListContractProductIDs :many
SELECT DISTINCT product_id
FROM contracts
WHERE org_id = sqlc.arg(org_id)
ORDER BY product_id;

-- name: ListContractsForOrg :many
WITH contract_now AS (
    SELECT c.contract_id,
           COALESCE(product_clock.business_now, org_clock.business_now, global_clock.business_now, transaction_timestamp()) AS effective_at
    FROM contracts c
    LEFT JOIN billing_clock_overrides product_clock
      ON product_clock.scope_kind = 'org_product'
     AND product_clock.scope_id = c.org_id || ':' || c.product_id
    LEFT JOIN billing_clock_overrides org_clock
      ON org_clock.scope_kind = 'org'
     AND org_clock.scope_id = c.org_id
    LEFT JOIN billing_clock_overrides global_clock
      ON global_clock.scope_kind = 'global'
     AND global_clock.scope_id = ''
    WHERE c.org_id = sqlc.arg(org_id)
),
current_phase AS (
    SELECT DISTINCT ON (p.contract_id)
           p.contract_id, p.phase_id, COALESCE(p.plan_id, '') AS plan_id, p.effective_start, p.effective_end
    FROM contract_phases p
    JOIN contract_now cn ON cn.contract_id = p.contract_id
    WHERE p.org_id = sqlc.arg(org_id)
      AND p.state IN ('active', 'grace', 'pending_payment', 'scheduled')
      AND p.effective_start <= cn.effective_at
      AND (p.effective_end IS NULL OR p.effective_end > cn.effective_at)
    ORDER BY p.contract_id, p.effective_start DESC, p.phase_id DESC
)
SELECT c.contract_id,
       c.product_id,
       COALESCE(cp.plan_id, '') AS plan_id,
       COALESCE(cp.phase_id, '') AS phase_id,
       c.state,
       c.payment_state,
       c.entitlement_state,
       COALESCE(pending_change.change_id, '') AS pending_change_id,
       COALESCE(pending_change.change_type, '') AS pending_change_type,
       COALESCE(pending_change.target_plan_id, '') AS pending_change_target_plan_id,
       pending_change.requested_effective_at AS pending_change_effective_at,
       c.starts_at,
       c.ends_at,
       cp.effective_start AS phase_start,
       cp.effective_end AS phase_end
FROM contracts c
LEFT JOIN current_phase cp ON cp.contract_id = c.contract_id
LEFT JOIN LATERAL (
    SELECT cc.change_id, cc.change_type, COALESCE(cc.target_plan_id, '') AS target_plan_id, cc.requested_effective_at
    FROM contract_changes cc
    WHERE cc.contract_id = c.contract_id
      AND cc.org_id = c.org_id
      AND cc.product_id = c.product_id
      AND cc.state = 'scheduled'
      AND cc.timing = 'period_end'
      AND cc.change_type IN ('downgrade', 'cancel')
    ORDER BY cc.requested_effective_at, cc.change_id
    LIMIT 1
) pending_change ON true
WHERE c.org_id = sqlc.arg(org_id)
ORDER BY c.starts_at DESC, c.contract_id DESC;

-- name: ListGrantBalanceRows :many
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
LEFT JOIN entitlement_periods p ON p.period_id = g.entitlement_period_id
LEFT JOIN contract_phases cp ON cp.phase_id = p.phase_id
LEFT JOIN plans pl ON pl.plan_id = cp.plan_id
WHERE g.org_id = sqlc.arg(org_id)
  AND g.closed_at IS NULL
  AND (sqlc.arg(product_id)::text = '' OR COALESCE(g.scope_product_id, sqlc.arg(product_id)::text) = sqlc.arg(product_id)::text OR g.scope_type = 'account')
  AND (g.expires_at IS NULL OR g.expires_at > sqlc.arg(now))
ORDER BY CASE g.source WHEN 'free_tier' THEN 1 WHEN 'contract' THEN 2 WHEN 'promo' THEN 3 WHEN 'refund' THEN 4 WHEN 'purchase' THEN 5 ELSE 6 END,
         g.starts_at,
         g.grant_id;

-- name: ListAuthorizedGrantUsage :many
SELECT l.grant_id,
       SUM(CASE WHEN w.state = 'settling' THEN l.amount_posted ELSE l.amount_reserved END)::bigint AS amount
FROM billing_windows w
JOIN billing_window_ledger_legs l ON l.window_id = w.window_id
WHERE w.org_id = sqlc.arg(org_id)
  AND w.state IN ('reserved', 'active', 'settling')
  AND l.grant_id IS NOT NULL
GROUP BY l.grant_id
ORDER BY l.grant_id;

-- name: ListBillingDocuments :many
SELECT document_id,
       COALESCE(document_number, '') AS document_number,
       document_kind,
       COALESCE(finalization_id, '') AS finalization_id,
       product_id,
       COALESCE(cycle_id, '') AS cycle_id,
       status,
       payment_status,
       period_start,
       period_end,
       issued_at,
       currency,
       subtotal_units,
       adjustment_units,
       tax_units,
       total_due_units,
       COALESCE(stripe_hosted_invoice_url, '') AS stripe_hosted_invoice_url,
       COALESCE(stripe_invoice_pdf_url, '') AS stripe_invoice_pdf_url,
       COALESCE(stripe_payment_intent_id, '') AS stripe_payment_intent_id
FROM billing_documents
WHERE org_id = sqlc.arg(org_id)
  AND (sqlc.arg(product_id)::text = '' OR product_id = sqlc.arg(product_id)::text)
  AND status <> 'voided'
ORDER BY period_start DESC, issued_at DESC NULLS LAST, document_id DESC;

-- name: ListStatementWindowIDs :many
SELECT window_id
FROM billing_windows
WHERE cycle_id = sqlc.arg(cycle_id)
  AND org_id = sqlc.arg(org_id)
  AND product_id = sqlc.arg(product_id)
  AND state IN ('reserved','active','settling','settled')
ORDER BY window_start, window_seq, window_id;

-- name: ListEntitlementCatalogRows :many
SELECT p.product_id,
       p.display_name AS product_display_name,
       b.bucket_id,
       b.display_name AS bucket_display_name,
       b.sort_order,
       s.sku_id,
       s.display_name AS sku_display_name
FROM products p
LEFT JOIN skus s ON s.product_id = p.product_id AND s.active
LEFT JOIN credit_buckets b ON b.bucket_id = s.bucket_id
WHERE p.active
ORDER BY p.display_name, p.product_id, b.sort_order NULLS LAST, b.bucket_id, s.display_name, s.sku_id;
