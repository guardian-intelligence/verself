-- name: GetBusinessClockOverride :one
SELECT business_now, generation
FROM billing_clock_overrides
WHERE scope_kind = sqlc.arg(scope_kind) AND scope_id = sqlc.arg(scope_id);

-- name: LockBusinessClockOverride :one
SELECT business_now
FROM billing_clock_overrides
WHERE scope_kind = sqlc.arg(scope_kind) AND scope_id = sqlc.arg(scope_id)
FOR UPDATE;

-- name: UpsertBusinessClockOverride :one
INSERT INTO billing_clock_overrides (scope_kind, scope_id, business_now, reason, updated_by)
VALUES (sqlc.arg(scope_kind), sqlc.arg(scope_id), sqlc.arg(business_now), sqlc.arg(reason), 'billing-service')
ON CONFLICT (scope_kind, scope_id) DO UPDATE
SET business_now = EXCLUDED.business_now,
    reason = EXCLUDED.reason,
    updated_by = EXCLUDED.updated_by,
    generation = billing_clock_overrides.generation + 1
RETURNING generation;

-- name: DeleteBusinessClockOverride :exec
DELETE FROM billing_clock_overrides
WHERE scope_kind = sqlc.arg(scope_kind) AND scope_id = sqlc.arg(scope_id);

-- name: GetActivePaidPhaseForWallClockReset :one
SELECT c.contract_id, p.phase_id, COALESCE(p.plan_id, '') AS plan_id, c.starts_at
FROM contracts c
JOIN contract_phases p ON p.contract_id = c.contract_id
WHERE c.org_id = sqlc.arg(org_id)
  AND c.product_id = sqlc.arg(product_id)
  AND c.state IN ('active', 'past_due', 'cancel_scheduled')
  AND p.state IN ('active', 'grace')
ORDER BY CASE WHEN p.effective_start <= sqlc.arg(wall_now) AND (p.effective_end IS NULL OR p.effective_end > sqlc.arg(wall_now)) THEN 0 ELSE 1 END,
         p.effective_start DESC,
         p.phase_id DESC
LIMIT 1
FOR UPDATE OF c, p;

-- name: ShiftActiveContractToWallClock :exec
UPDATE contracts
SET starts_at = LEAST(starts_at, sqlc.arg(wall_now)),
    updated_at = now()
WHERE contract_id = sqlc.arg(contract_id);

-- name: ShiftActiveContractPhaseToWallClock :exec
UPDATE contract_phases
SET effective_start = LEAST(effective_start, sqlc.arg(wall_now)),
    effective_end = CASE WHEN effective_end IS NOT NULL AND effective_end <= sqlc.arg(wall_now) THEN NULL ELSE effective_end END,
    activated_at = COALESCE(activated_at, sqlc.arg(wall_now)),
    updated_at = now()
WHERE phase_id = sqlc.arg(phase_id);

-- name: VoidCyclesForWallClockReset :many
UPDATE billing_cycles
SET status = 'voided',
    finalized_at = COALESCE(finalized_at, sqlc.arg(wall_now)::timestamptz),
    closed_reason = CASE WHEN closed_reason = '' THEN 'wall_clock_reset' ELSE closed_reason END,
    metadata = metadata || jsonb_build_object('voided_by', 'billing-wall-clock', 'voided_at', sqlc.arg(wall_now)::timestamptz::text, 'reason', sqlc.arg(reason)::text),
    updated_at = now()
WHERE org_id = sqlc.arg(org_id)
  AND product_id = sqlc.arg(product_id)
  AND status <> 'voided'
  AND (
    status IN ('open', 'closing')
    OR tstzrange(starts_at, ends_at, '[)') && tstzrange(sqlc.arg(starts_at)::timestamptz, sqlc.arg(ends_at)::timestamptz, '[)')
  )
RETURNING cycle_id;

-- name: CloseCurrentEntitlementGrantsForWallClockReset :many
UPDATE credit_grants AS g
SET closed_at = sqlc.arg(wall_now)::timestamptz,
    closed_reason = 'wall_clock_reset',
    metadata = metadata || jsonb_build_object('closed_by', 'billing-wall-clock', 'closed_at', sqlc.arg(wall_now)::timestamptz::text),
    updated_at = now()
WHERE g.org_id = sqlc.arg(org_id)
  AND g.closed_at IS NULL
  AND g.source IN ('free_tier', 'contract')
  AND (
    g.scope_product_id = sqlc.arg(product_id)
    OR g.entitlement_period_id IN (SELECT period_id FROM entitlement_periods WHERE org_id = sqlc.arg(org_id) AND product_id = sqlc.arg(product_id))
  )
  AND NOT (g.period_start = sqlc.arg(starts_at)::timestamptz AND g.period_end = sqlc.arg(ends_at)::timestamptz)
RETURNING grant_id;

-- name: VoidCurrentEntitlementPeriodsForWallClockReset :exec
UPDATE entitlement_periods
SET entitlement_state = 'voided',
    metadata = metadata || jsonb_build_object('voided_by', 'billing-wall-clock', 'voided_at', sqlc.arg(wall_now)::timestamptz::text, 'reason', sqlc.arg(reason)::text),
    updated_at = now()
WHERE org_id = sqlc.arg(org_id)
  AND product_id = sqlc.arg(product_id)
  AND source IN ('free_tier', 'contract')
  AND entitlement_state IN ('scheduled', 'active', 'grace')
  AND NOT (period_start = sqlc.arg(starts_at)::timestamptz AND period_end = sqlc.arg(ends_at)::timestamptz);

-- name: ReopenWallClockTargetEntitlementPeriods :exec
UPDATE entitlement_periods
SET entitlement_state = 'active',
    metadata = (metadata - 'voided_by' - 'voided_at' - 'reason') || jsonb_build_object('reopened_by', 'billing-wall-clock', 'reopened_at', sqlc.arg(wall_now)::timestamptz::text),
    updated_at = now()
WHERE org_id = sqlc.arg(org_id)
  AND product_id = sqlc.arg(product_id)
  AND source IN ('free_tier', 'contract')
  AND period_start = sqlc.arg(starts_at)::timestamptz
  AND period_end = sqlc.arg(ends_at)::timestamptz
  AND entitlement_state = 'voided'
  AND metadata->>'voided_by' = 'billing-wall-clock';

-- name: ReopenWallClockTargetCreditGrants :exec
UPDATE credit_grants AS g
SET closed_at = NULL,
    closed_reason = '',
    metadata = (metadata - 'closed_by' - 'closed_at') || jsonb_build_object('reopened_by', 'billing-wall-clock', 'reopened_at', sqlc.arg(wall_now)::timestamptz::text),
    updated_at = now()
WHERE g.org_id = sqlc.arg(org_id)
  AND g.source IN ('free_tier', 'contract')
  AND g.period_start = sqlc.arg(starts_at)::timestamptz
  AND g.period_end = sqlc.arg(ends_at)::timestamptz
  AND g.closed_reason = 'wall_clock_reset'
  AND (
    g.scope_product_id = sqlc.arg(product_id)
    OR g.entitlement_period_id IN (SELECT period_id FROM entitlement_periods WHERE org_id = sqlc.arg(org_id) AND product_id = sqlc.arg(product_id))
  );

-- name: UpsertWallClockResetCycle :execrows
INSERT INTO billing_cycles (cycle_id, org_id, product_id, currency, anchor_at, cycle_seq, cadence_kind, starts_at, ends_at, status, finalization_due_at, closed_reason, metadata)
VALUES (
    sqlc.arg(cycle_id),
    sqlc.arg(org_id),
    sqlc.arg(product_id),
    'usd',
    sqlc.arg(anchor_at),
    sqlc.arg(cycle_seq),
    sqlc.arg(cadence_kind),
    sqlc.arg(starts_at),
    sqlc.arg(ends_at),
    'open',
    sqlc.arg(ends_at),
    '',
    jsonb_build_object('opened_by', 'billing-wall-clock')
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
    metadata = (billing_cycles.metadata - 'voided_by' - 'voided_at' - 'reason') || jsonb_build_object('opened_by', 'billing-wall-clock'),
    updated_at = now()
WHERE billing_cycles.status = 'voided';

-- name: ReassignWallClockWindows :many
UPDATE billing_windows
SET cycle_id = sqlc.arg(cycle_id),
    metadata = metadata || jsonb_build_object(
      'cycle_reassigned_by', 'billing-wall-clock',
      'cycle_reassigned_at', sqlc.arg(wall_now)::timestamptz::text,
      'previous_cycle_id', cycle_id
    ),
    updated_at = now()
WHERE org_id = sqlc.arg(org_id)
  AND product_id = sqlc.arg(product_id)
  AND state IN ('reserved', 'active', 'settling', 'settled')
  AND window_start >= sqlc.arg(starts_at)::timestamptz
  AND window_start < sqlc.arg(ends_at)::timestamptz
  AND cycle_id <> sqlc.arg(cycle_id)
RETURNING window_id;
