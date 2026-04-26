-- name: InsertCycleFinalization :execrows
INSERT INTO billing_finalizations (
    finalization_id, subject_type, subject_id, cycle_id, org_id, product_id, reason,
    document_kind, state, started_at, idempotency_key
) VALUES ($1, 'cycle', $2, $2, $3, $4, 'scheduled_period_end', 'statement', 'pending', $5, $1)
ON CONFLICT (subject_type, subject_id, idempotency_key) DO NOTHING;

-- name: LinkCycleFinalization :exec
UPDATE billing_cycles
SET active_finalization_id = $2
WHERE cycle_id = $1 AND active_finalization_id IS NULL;

-- name: ListPendingBillingFinalizations :many
SELECT finalization_id
FROM billing_finalizations
WHERE state IN ('pending', 'failed')
  AND subject_type = 'cycle'
ORDER BY started_at, finalization_id
LIMIT $1;

-- name: ClaimBillingFinalization :one
UPDATE billing_finalizations
SET state = 'collecting_facts',
    attempts = attempts + 1,
    last_error = '',
    updated_at = now()
WHERE finalization_id = $1
  AND state IN ('pending', 'failed')
RETURNING finalization_id, subject_type, subject_id, COALESCE(cycle_id, '') AS cycle_id, org_id, product_id, reason, document_kind, started_at;

-- name: FailBillingFinalization :exec
UPDATE billing_finalizations
SET state = CASE WHEN attempts >= 25 THEN 'blocked' ELSE 'failed' END,
    last_error = $2,
    blocked_reason = CASE WHEN attempts >= 25 THEN $2 ELSE blocked_reason END,
    updated_at = now()
WHERE finalization_id = $1
  AND state = 'collecting_facts';

-- name: GetBillingCycle :one
SELECT cycle_id, currency, COALESCE(predecessor_cycle_id, '') AS predecessor_cycle_id, anchor_at, cycle_seq, cadence_kind, starts_at, ends_at
FROM billing_cycles
WHERE cycle_id = $1;

-- name: CycleHasPaidContractOverlap :one
SELECT EXISTS (
    SELECT 1
    FROM contract_phases cp
    JOIN contracts c ON c.contract_id = cp.contract_id
    WHERE cp.org_id = $1
      AND cp.product_id = $2
      AND cp.payment_state = 'paid'
      AND c.payment_state = 'paid'
      AND cp.effective_start < $4
      AND COALESCE(cp.effective_end, $4) > $3
      AND cp.state IN ('active', 'grace', 'closed', 'superseded')
) AS has_overlap;

-- name: InsertBillingDocument :exec
INSERT INTO billing_documents (
    document_id, document_number, document_kind, finalization_id, org_id, product_id, cycle_id,
    status, payment_status, period_start, period_end, issued_at, currency, subtotal_units,
    total_due_units, document_snapshot_json, rendered_html, content_hash
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
ON CONFLICT (document_id) DO NOTHING;

-- name: UpdateBillingFinalizationIssued :exec
UPDATE billing_finalizations
SET state = $2,
    document_id = NULLIF($3, ''),
    document_kind = $4,
    customer_visible = $5,
    notification_policy = $6,
    has_usage = $7,
    has_financial_activity = $8,
    completed_at = $9,
    snapshot_hash = $10,
    updated_at = now()
WHERE finalization_id = $1;

-- name: MarkBillingCycleFinalized :exec
UPDATE billing_cycles
SET status = 'finalized',
    active_finalization_id = $2,
    finalized_at = $3,
    updated_at = now()
WHERE cycle_id = $1
  AND status IN ('closed_for_usage', 'finalized');

-- name: InsertBillingDocumentLineItem :exec
INSERT INTO billing_document_line_items (
    line_item_id, document_id, line_type, product_id, bucket_id, sku_id, description, quantity,
    quantity_unit, unit_rate_units, charge_units, free_tier_units, contract_units, purchase_units,
    promo_units, refund_units, receivable_units
) VALUES ($1, $2, 'usage', $3, NULLIF($4, ''), NULLIF($5, ''), $6, $7::double precision, $8, $9, $10, $11, $12, $13, $14, $15, $16)
ON CONFLICT (line_item_id) DO NOTHING;
