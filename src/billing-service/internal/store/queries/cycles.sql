-- name: ListPendingDueBillingWorkTargets :many
SELECT DISTINCT org_id, product_id
FROM billing_cycles
WHERE status IN ('open', 'closing')
  AND ends_at <= transaction_timestamp()
ORDER BY org_id, product_id
LIMIT sqlc.arg(limit_count);

-- name: UpsertOpenBillingCycle :execrows
INSERT INTO billing_cycles (cycle_id, org_id, product_id, currency, anchor_at, cycle_seq, cadence_kind, starts_at, ends_at, status, finalization_due_at)
VALUES (
    sqlc.arg(cycle_id),
    sqlc.arg(org_id),
    sqlc.arg(product_id),
    sqlc.arg(currency),
    sqlc.arg(anchor_at),
    sqlc.arg(cycle_seq),
    sqlc.arg(cadence_kind),
    sqlc.arg(starts_at),
    sqlc.arg(ends_at),
    'open',
    sqlc.arg(ends_at)
)
ON CONFLICT (cycle_id) DO UPDATE
SET currency = EXCLUDED.currency,
    anchor_at = EXCLUDED.anchor_at,
    cycle_seq = EXCLUDED.cycle_seq,
    cadence_kind = EXCLUDED.cadence_kind,
    starts_at = EXCLUDED.starts_at,
    ends_at = EXCLUDED.ends_at,
    status = 'open',
    finalization_due_at = EXCLUDED.finalization_due_at,
    closed_reason = '',
    active_finalization_id = NULL,
    successor_cycle_id = NULL,
    closed_by_event_id = NULL,
    closed_for_usage_at = NULL,
    finalized_at = NULL,
    metadata = billing_cycles.metadata - 'voided_by'
WHERE billing_cycles.status = 'voided';

-- name: GetNextDueOpenCycleForUpdate :one
SELECT cycle_id,
       currency,
       COALESCE(predecessor_cycle_id, '') AS predecessor_cycle_id,
       anchor_at,
       cycle_seq,
       cadence_kind,
       starts_at,
       ends_at
FROM billing_cycles
WHERE org_id = sqlc.arg(org_id)
  AND product_id = sqlc.arg(product_id)
  AND status IN ('open', 'closing')
  AND ends_at <= sqlc.arg(now)
ORDER BY ends_at, cycle_id
LIMIT 1
FOR UPDATE;

-- name: GetOpenBillingCycleContaining :one
SELECT cycle_id,
       currency,
       COALESCE(predecessor_cycle_id, '') AS predecessor_cycle_id,
       anchor_at,
       cycle_seq,
       cadence_kind,
       starts_at,
       ends_at
FROM billing_cycles
WHERE org_id = sqlc.arg(org_id)
  AND product_id = sqlc.arg(product_id)
  AND status IN ('open', 'closing')
  AND starts_at <= sqlc.arg(now)
  AND ends_at > sqlc.arg(now)
ORDER BY starts_at DESC, cycle_id DESC
LIMIT 1;

-- name: GetAnyOpenBillingCycle :one
SELECT cycle_id,
       currency,
       COALESCE(predecessor_cycle_id, '') AS predecessor_cycle_id,
       anchor_at,
       cycle_seq,
       cadence_kind,
       starts_at,
       ends_at
FROM billing_cycles
WHERE org_id = sqlc.arg(org_id)
  AND product_id = sqlc.arg(product_id)
  AND status IN ('open', 'closing')
ORDER BY starts_at DESC, cycle_id DESC
LIMIT 1;

-- name: CloseCycleForUsage :execrows
UPDATE billing_cycles
SET status = 'closed_for_usage',
    closed_for_usage_at = sqlc.arg(closed_at)
WHERE cycle_id = sqlc.arg(cycle_id)
  AND status IN ('open', 'closing');

-- name: UpsertSuccessorBillingCycle :execrows
INSERT INTO billing_cycles (cycle_id, org_id, product_id, currency, predecessor_cycle_id, anchor_at, cycle_seq, cadence_kind, starts_at, ends_at, status, finalization_due_at)
VALUES (
    sqlc.arg(cycle_id),
    sqlc.arg(org_id),
    sqlc.arg(product_id),
    sqlc.arg(currency),
    sqlc.arg(predecessor_cycle_id),
    sqlc.arg(anchor_at),
    sqlc.arg(cycle_seq),
    sqlc.arg(cadence_kind),
    sqlc.arg(starts_at),
    sqlc.arg(ends_at),
    'open',
    sqlc.arg(ends_at)
)
ON CONFLICT (cycle_id) DO UPDATE
SET currency = EXCLUDED.currency,
    predecessor_cycle_id = EXCLUDED.predecessor_cycle_id,
    anchor_at = EXCLUDED.anchor_at,
    cycle_seq = EXCLUDED.cycle_seq,
    cadence_kind = EXCLUDED.cadence_kind,
    starts_at = EXCLUDED.starts_at,
    ends_at = EXCLUDED.ends_at,
    status = 'open',
    finalization_due_at = EXCLUDED.finalization_due_at,
    closed_reason = '',
    active_finalization_id = NULL,
    successor_cycle_id = NULL,
    closed_by_event_id = NULL,
    closed_for_usage_at = NULL,
    finalized_at = NULL,
    metadata = billing_cycles.metadata - 'voided_by'
WHERE billing_cycles.status = 'voided';

-- name: LinkSuccessorBillingCycle :exec
UPDATE billing_cycles
SET successor_cycle_id = sqlc.arg(successor_cycle_id)
WHERE cycle_id = sqlc.arg(cycle_id)
  AND successor_cycle_id IS NULL;

-- name: CloseFreeCycleAtActivation :exec
UPDATE billing_cycles
SET status = 'closed_for_usage',
    ends_at = sqlc.arg(effective_at),
    finalization_due_at = sqlc.arg(effective_at),
    closed_for_usage_at = sqlc.arg(effective_at),
    closed_reason = 'free_to_paid_activation'
WHERE cycle_id = sqlc.arg(cycle_id)
  AND status IN ('open', 'closing');

-- name: UpsertActivationBillingCycle :execrows
INSERT INTO billing_cycles (cycle_id, org_id, product_id, currency, predecessor_cycle_id, anchor_at, cycle_seq, cadence_kind, starts_at, ends_at, status, finalization_due_at)
VALUES (
    sqlc.arg(cycle_id),
    sqlc.arg(org_id),
    sqlc.arg(product_id),
    sqlc.arg(currency),
    NULLIF(sqlc.arg(predecessor_cycle_id), ''),
    sqlc.arg(anchor_at),
    0,
    sqlc.arg(cadence_kind),
    sqlc.arg(starts_at),
    sqlc.arg(ends_at),
    'open',
    sqlc.arg(ends_at)
)
ON CONFLICT (cycle_id) DO UPDATE
SET currency = EXCLUDED.currency,
    predecessor_cycle_id = EXCLUDED.predecessor_cycle_id,
    anchor_at = EXCLUDED.anchor_at,
    cycle_seq = EXCLUDED.cycle_seq,
    cadence_kind = EXCLUDED.cadence_kind,
    starts_at = EXCLUDED.starts_at,
    ends_at = EXCLUDED.ends_at,
    status = 'open',
    finalization_due_at = EXCLUDED.finalization_due_at,
    closed_reason = '',
    active_finalization_id = NULL,
    successor_cycle_id = NULL,
    closed_by_event_id = NULL,
    closed_for_usage_at = NULL,
    finalized_at = NULL,
    metadata = billing_cycles.metadata - 'voided_by'
WHERE billing_cycles.status = 'voided';
