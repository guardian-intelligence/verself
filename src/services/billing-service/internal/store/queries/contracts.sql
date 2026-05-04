-- name: GetContract :one
SELECT c.contract_id,
       c.product_id,
       c.state,
       c.payment_state,
       c.entitlement_state,
       c.starts_at,
       c.ends_at
FROM contracts c
WHERE c.contract_id = sqlc.arg(contract_id)
  AND c.org_id = sqlc.arg(org_id);

-- name: GetCurrentContractPhase :one
SELECT phase_id,
       COALESCE(plan_id, '') AS plan_id,
       effective_start,
       effective_end
FROM contract_phases
WHERE contract_id = sqlc.arg(contract_id)
  AND state IN ('active','grace','pending_payment','scheduled')
  AND effective_start <= sqlc.arg(now)
  AND (effective_end IS NULL OR effective_end > sqlc.arg(now))
ORDER BY CASE state WHEN 'active' THEN 1 WHEN 'grace' THEN 2 ELSE 3 END,
         effective_start DESC,
         phase_id DESC
LIMIT 1;

-- name: GetPendingScheduledContractChange :one
SELECT change_id,
       change_type,
       COALESCE(target_plan_id, '') AS target_plan_id,
       requested_effective_at
FROM contract_changes
WHERE contract_id = sqlc.arg(contract_id)
  AND org_id = sqlc.arg(org_id)
  AND product_id = sqlc.arg(product_id)
  AND state = 'scheduled'
  AND timing = 'period_end'
  AND change_type IN ('downgrade', 'cancel')
ORDER BY requested_effective_at,
         change_id
LIMIT 1;

-- name: ScheduleContractCancellation :exec
UPDATE contracts
SET state = 'cancel_scheduled',
    cancel_at = sqlc.arg(cancel_at),
    ends_at = sqlc.arg(cancel_at)
WHERE contract_id = sqlc.arg(contract_id)
  AND org_id = sqlc.arg(org_id)
  AND state IN ('active','past_due');

-- name: ScheduleContractPhaseCancellation :exec
UPDATE contract_phases
SET effective_end = COALESCE(effective_end, sqlc.arg(cancel_at))
WHERE contract_id = sqlc.arg(contract_id)
  AND org_id = sqlc.arg(org_id)
  AND state IN ('active','grace','scheduled');

-- name: InsertCancellationContractChange :exec
INSERT INTO contract_changes (
    change_id, contract_id, org_id, product_id, change_type, timing, requested_effective_at,
    target_plan_id, state, idempotency_key, requested_at
) VALUES (
    sqlc.arg(change_id), sqlc.arg(contract_id), sqlc.arg(org_id), sqlc.arg(product_id),
    'cancel', 'period_end', sqlc.arg(requested_effective_at), NULL, 'scheduled',
    sqlc.arg(change_id), sqlc.arg(requested_at)
)
ON CONFLICT (contract_id, idempotency_key) DO NOTHING;

-- name: ListScheduledContractChangesForResume :many
SELECT change_id,
       change_type,
       COALESCE(target_plan_id, '') AS target_plan_id,
       requested_effective_at
FROM contract_changes
WHERE contract_id = sqlc.arg(contract_id)
  AND org_id = sqlc.arg(org_id)
  AND product_id = sqlc.arg(product_id)
  AND state = 'scheduled'
  AND timing = 'period_end'
  AND change_type IN ('downgrade', 'cancel')
ORDER BY requested_effective_at,
         change_id
FOR UPDATE;

-- name: CancelScheduledContractChangesForResume :exec
UPDATE contract_changes
SET state = 'canceled',
    updated_at = now()
WHERE contract_id = sqlc.arg(contract_id)
  AND org_id = sqlc.arg(org_id)
  AND product_id = sqlc.arg(product_id)
  AND state = 'scheduled'
  AND timing = 'period_end'
  AND change_type IN ('downgrade', 'cancel');

-- name: RestoreContractDuringResume :exec
UPDATE contracts
SET state = CASE WHEN state = 'cancel_scheduled' THEN 'active' ELSE state END,
    ends_at = NULL,
    cancel_at = NULL,
    closed_at = NULL,
    updated_at = now()
WHERE contract_id = sqlc.arg(contract_id)
  AND org_id = sqlc.arg(org_id)
  AND state IN ('active', 'past_due', 'suspended', 'cancel_scheduled');

-- name: RestorePhaseBoundaryDuringCancelResume :exec
UPDATE contract_phases
SET effective_end = NULL,
    updated_at = now()
WHERE contract_id = sqlc.arg(contract_id)
  AND org_id = sqlc.arg(org_id)
  AND product_id = sqlc.arg(product_id)
  AND state IN ('active', 'grace', 'scheduled')
  AND effective_end = sqlc.arg(requested_effective_at);

-- name: InsertScheduledDowngrade :exec
INSERT INTO contract_changes (
    change_id, contract_id, org_id, product_id, change_type, timing, requested_effective_at,
    from_phase_id, to_phase_id, target_plan_id, state, idempotency_key, requested_at
) VALUES (
    sqlc.arg(change_id), sqlc.arg(contract_id), sqlc.arg(org_id), sqlc.arg(product_id),
    'downgrade', 'period_end', sqlc.arg(requested_effective_at),
    NULLIF(sqlc.arg(from_phase_id), ''), sqlc.arg(to_phase_id), sqlc.arg(target_plan_id),
    'scheduled', sqlc.arg(change_id), sqlc.arg(requested_at)
)
ON CONFLICT (contract_id, idempotency_key) DO NOTHING;

-- name: InsertUpgradeContractChange :exec
INSERT INTO contract_changes (
    change_id, contract_id, org_id, product_id, change_type, timing, requested_effective_at,
    from_phase_id, to_phase_id, target_plan_id, state, provider, provider_request_id,
    idempotency_key, requested_at, proration_basis_cycle_id, price_delta_units,
    entitlement_delta_mode, proration_numerator, proration_denominator, payload
) VALUES (
    sqlc.arg(change_id), sqlc.arg(contract_id), sqlc.arg(org_id), sqlc.arg(product_id),
    'upgrade', 'immediate', sqlc.arg(requested_effective_at),
    NULLIF(sqlc.arg(from_phase_id), ''), sqlc.arg(to_phase_id), sqlc.arg(target_plan_id),
    'awaiting_payment', 'stripe', sqlc.arg(provider_request_id), sqlc.arg(change_id),
    sqlc.arg(requested_at), sqlc.arg(proration_basis_cycle_id), sqlc.arg(price_delta_units),
    'positive_delta', sqlc.arg(proration_numerator), sqlc.arg(proration_denominator), sqlc.arg(payload)
)
ON CONFLICT (contract_id, idempotency_key) DO UPDATE
SET provider_request_id = COALESCE(contract_changes.provider_request_id, EXCLUDED.provider_request_id),
    updated_at = now();

-- name: InsertUpgradeFinalization :exec
INSERT INTO billing_finalizations (
    finalization_id, subject_type, subject_id, cycle_id, contract_change_id, org_id, product_id,
    reason, document_kind, state, customer_visible, notification_policy, has_financial_activity,
    started_at, completed_at, idempotency_key, snapshot_hash, metadata
) VALUES (
    sqlc.arg(finalization_id), 'contract_change', sqlc.arg(change_id), sqlc.arg(cycle_id),
    sqlc.arg(change_id), sqlc.arg(org_id), sqlc.arg(product_id),
    'immediate_upgrade_delta', 'invoice', 'collection_pending', true, 'always', true,
    sqlc.arg(requested_at), sqlc.arg(requested_at), sqlc.arg(finalization_id),
    sqlc.arg(snapshot_hash), sqlc.arg(metadata)
)
ON CONFLICT (subject_type, subject_id, idempotency_key) DO NOTHING;

-- name: InsertUpgradeDocument :exec
INSERT INTO billing_documents (
    document_id, document_number, document_kind, finalization_id, org_id, product_id, cycle_id,
    change_id, status, payment_status, period_start, period_end, issued_at, currency,
    subtotal_units, total_due_units, document_snapshot_json, content_hash
) VALUES (
    sqlc.arg(document_id), sqlc.arg(document_number), 'invoice', sqlc.arg(finalization_id),
    sqlc.arg(org_id), sqlc.arg(product_id), sqlc.arg(cycle_id), sqlc.arg(change_id),
    'issued', 'pending', sqlc.arg(period_start), sqlc.arg(period_end), sqlc.arg(issued_at),
    'usd', sqlc.arg(subtotal_units), sqlc.arg(total_due_units), sqlc.arg(document_snapshot_json),
    sqlc.arg(content_hash)
)
ON CONFLICT (document_id) DO NOTHING;

-- name: LinkUpgradeDocumentFinalization :exec
UPDATE billing_finalizations
SET document_id = sqlc.arg(document_id)
WHERE finalization_id = sqlc.arg(finalization_id)
  AND document_id IS NULL;

-- name: GetContractChangeStatus :one
SELECT state,
       change_type,
       COALESCE(provider_invoice_id, '') AS provider_invoice_id
FROM contract_changes
WHERE change_id = sqlc.arg(change_id);

-- name: GetContractChangeStateForUpdate :one
SELECT state
FROM contract_changes
WHERE change_id = sqlc.arg(change_id)
FOR UPDATE;

-- name: GetContractChangeProviderRequestID :one
SELECT COALESCE(provider_request_id, '') AS provider_request_id
FROM contract_changes
WHERE change_id = sqlc.arg(change_id);

-- name: SupersedeContractPhaseForUpgrade :one
UPDATE contract_phases
SET state = 'superseded',
    entitlement_state = CASE WHEN effective_start < sqlc.arg(effective_at) THEN 'closed' ELSE 'voided' END,
    effective_end = CASE WHEN effective_start < sqlc.arg(effective_at) THEN sqlc.arg(effective_at) ELSE NULL END,
    closed_at = sqlc.arg(effective_at)
WHERE phase_id = sqlc.arg(phase_id)
  AND contract_id = sqlc.arg(contract_id)
  AND state IN ('active','grace')
RETURNING effective_start, entitlement_state;

-- name: LinkSupersededContractPhase :exec
UPDATE contract_phases
SET superseded_by_phase_id = sqlc.arg(superseded_by_phase_id)
WHERE phase_id = sqlc.arg(phase_id)
  AND contract_id = sqlc.arg(contract_id)
  AND state = 'superseded';

-- name: UpdateUpgradedContract :exec
UPDATE contracts
SET state = 'active',
    payment_state = 'paid',
    entitlement_state = 'active',
    display_name = sqlc.arg(display_name),
    ends_at = NULL,
    cancel_at = NULL
WHERE contract_id = sqlc.arg(contract_id)
  AND org_id = sqlc.arg(org_id);

-- name: MarkUpgradeContractChangeApplied :exec
UPDATE contract_changes
SET state = 'applied',
    actual_effective_at = sqlc.arg(effective_at),
    provider_invoice_id = NULLIF(sqlc.arg(provider_invoice_id), ''),
    updated_at = now()
WHERE change_id = sqlc.arg(change_id);

-- name: MarkUpgradeDocumentPaid :exec
UPDATE billing_documents
SET status = 'paid',
    payment_status = 'paid',
    stripe_invoice_id = NULLIF(sqlc.arg(provider_invoice_id), ''),
    issued_at = COALESCE(issued_at, sqlc.arg(issued_at))
WHERE document_id = sqlc.arg(document_id);

-- name: MarkUpgradeFinalizationPaid :exec
UPDATE billing_finalizations
SET state = 'paid',
    completed_at = COALESCE(completed_at, sqlc.arg(completed_at)),
    document_id = sqlc.arg(document_id),
    updated_at = now()
WHERE finalization_id = sqlc.arg(finalization_id);

-- name: ListDueContractChangesForUpdate :many
SELECT change_id,
       contract_id,
       change_type,
       COALESCE(from_phase_id, '') AS from_phase_id,
       COALESCE(to_phase_id, '') AS to_phase_id,
       COALESCE(target_plan_id, '') AS target_plan_id,
       requested_effective_at
FROM contract_changes
WHERE org_id = sqlc.arg(org_id)
  AND product_id = sqlc.arg(product_id)
  AND state = 'scheduled'
  AND requested_effective_at <= sqlc.arg(requested_effective_at)
ORDER BY requested_effective_at,
         change_id
FOR UPDATE;

-- name: ListCancelableContractPhasesForUpdate :many
SELECT phase_id,
       COALESCE(plan_id, '') AS plan_id
FROM contract_phases
WHERE contract_id = sqlc.arg(contract_id)
  AND org_id = sqlc.arg(org_id)
  AND product_id = sqlc.arg(product_id)
  AND state IN ('active', 'grace', 'scheduled')
  AND effective_start < sqlc.arg(effective_start_before)
ORDER BY effective_start,
         phase_id
FOR UPDATE;

-- name: CloseCanceledContract :exec
UPDATE contracts
SET state = 'ended',
    entitlement_state = 'closed',
    ends_at = COALESCE(ends_at, sqlc.arg(ended_at)),
    cancel_at = COALESCE(cancel_at, sqlc.arg(ended_at)),
    closed_at = sqlc.arg(ended_at)
WHERE contract_id = sqlc.arg(contract_id)
  AND org_id = sqlc.arg(org_id)
  AND state IN ('active', 'past_due', 'suspended', 'cancel_scheduled');

-- name: UpdateDowngradedContract :exec
UPDATE contracts
SET state = 'active',
    payment_state = 'paid',
    entitlement_state = 'active',
    display_name = sqlc.arg(display_name),
    ends_at = NULL,
    cancel_at = NULL,
    closed_at = NULL
WHERE contract_id = sqlc.arg(contract_id)
  AND org_id = sqlc.arg(org_id)
  AND state IN ('active', 'past_due', 'suspended', 'cancel_scheduled');

-- name: MarkContractChangeApplying :exec
UPDATE contract_changes
SET state = 'applying',
    attempts = attempts + 1
WHERE change_id = sqlc.arg(change_id)
  AND state = 'scheduled';

-- name: MarkContractChangeApplied :exec
UPDATE contract_changes
SET state = 'applied',
    actual_effective_at = sqlc.arg(effective_at)
WHERE change_id = sqlc.arg(change_id);

-- name: CloseContractPhase :execrows
UPDATE contract_phases
SET state = 'closed',
    entitlement_state = 'closed',
    effective_end = COALESCE(effective_end, sqlc.arg(effective_end)),
    closed_at = sqlc.arg(effective_end)
WHERE phase_id = sqlc.arg(phase_id)
  AND contract_id = sqlc.arg(contract_id)
  AND state IN ('active', 'grace', 'scheduled');

-- name: GetActivePhaseAtBoundary :one
SELECT phase_id
FROM contract_phases
WHERE contract_id = sqlc.arg(contract_id)
  AND org_id = sqlc.arg(org_id)
  AND product_id = sqlc.arg(product_id)
  AND state IN ('active', 'grace')
  AND effective_start < sqlc.arg(effective_at)
  AND (effective_end IS NULL OR effective_end > sqlc.arg(effective_at))
ORDER BY effective_start DESC,
         phase_id DESC
LIMIT 1
FOR UPDATE;

-- name: GetPhasePlanID :one
SELECT COALESCE(plan_id, '') AS plan_id
FROM contract_phases
WHERE phase_id = sqlc.arg(phase_id);

-- name: ContractPhaseAlreadyActive :one
SELECT EXISTS (
    SELECT 1
    FROM contract_phases
    WHERE phase_id = sqlc.arg(phase_id)
      AND contract_id = sqlc.arg(contract_id)
      AND state = 'active'
      AND payment_state = 'paid'
) AS already_active;

-- name: UpsertContract :exec
INSERT INTO contracts (
    contract_id, org_id, product_id, display_name, contract_kind, state,
    payment_state, entitlement_state, overage_policy, starts_at
) VALUES (
    sqlc.arg(contract_id), sqlc.arg(org_id), sqlc.arg(product_id), sqlc.arg(display_name),
    'self_serve', 'active', 'paid', 'active', 'bill_published_rate', sqlc.arg(starts_at)
)
ON CONFLICT (contract_id) DO UPDATE
SET state = 'active',
    payment_state = 'paid',
    entitlement_state = 'active',
    display_name = EXCLUDED.display_name,
    ends_at = NULL,
    cancel_at = NULL;

-- name: UpsertContractPhase :exec
INSERT INTO contract_phases (
    phase_id, contract_id, org_id, product_id, plan_id, phase_kind, state, payment_state,
    entitlement_state, currency, recurring_amount_units, recurring_interval, effective_start,
    activated_at, created_reason
) VALUES (
    sqlc.arg(phase_id), sqlc.arg(contract_id), sqlc.arg(org_id), sqlc.arg(product_id),
    sqlc.arg(plan_id), 'catalog_plan', sqlc.arg(state), sqlc.arg(payment_state),
    'active', sqlc.arg(currency), sqlc.arg(recurring_amount_units), 'month',
    sqlc.arg(effective_start), sqlc.arg(effective_start), 'catalog_contract'
)
ON CONFLICT (phase_id) DO UPDATE
SET state = EXCLUDED.state,
    payment_state = EXCLUDED.payment_state,
    entitlement_state = 'active',
    effective_end = NULL,
    activated_at = COALESCE(contract_phases.activated_at, EXCLUDED.activated_at);

-- name: UpsertContractEntitlementLine :exec
INSERT INTO contract_entitlement_lines (
    line_id, phase_id, contract_id, org_id, product_id, policy_id, scope_type,
    scope_product_id, scope_bucket_id, scope_sku_id, amount_units, recurrence_interval,
    recurrence_anchor_kind, recurrence_anchor_day, proration_mode, policy_version,
    active_from, next_materialize_at
) VALUES (
    sqlc.arg(line_id), sqlc.arg(phase_id), sqlc.arg(contract_id), sqlc.arg(org_id),
    sqlc.arg(product_id), sqlc.arg(policy_id), sqlc.arg(scope_type),
    NULLIF(sqlc.arg(scope_product_id), ''), NULLIF(sqlc.arg(scope_bucket_id), ''),
    NULLIF(sqlc.arg(scope_sku_id), ''), sqlc.arg(amount_units), 'month',
    sqlc.arg(recurrence_anchor_kind), NULL, sqlc.arg(proration_mode), sqlc.arg(policy_version),
    sqlc.arg(active_from), sqlc.arg(active_from)
)
ON CONFLICT (line_id) DO UPDATE
SET amount_units = EXCLUDED.amount_units,
    active_from = EXCLUDED.active_from,
    next_materialize_at = EXCLUDED.next_materialize_at,
    policy_version = EXCLUDED.policy_version;

-- name: InsertUpgradeDeltaPeriod :exec
INSERT INTO entitlement_periods (
    period_id, org_id, product_id, cycle_id, source, policy_id, contract_id, phase_id,
    line_id, scope_type, scope_product_id, scope_bucket_id, scope_sku_id, amount_units,
    period_start, period_end, policy_version, payment_state, entitlement_state,
    calculation_kind, source_reference_id, created_reason, change_id
) VALUES (
    sqlc.arg(period_id), sqlc.arg(org_id), sqlc.arg(product_id), sqlc.arg(cycle_id),
    'contract', sqlc.arg(policy_id), sqlc.arg(contract_id), sqlc.arg(phase_id),
    sqlc.arg(line_id), sqlc.arg(scope_type), NULLIF(sqlc.arg(scope_product_id), ''),
    NULLIF(sqlc.arg(scope_bucket_id), ''), NULLIF(sqlc.arg(scope_sku_id), ''),
    sqlc.arg(amount_units), sqlc.arg(period_start), sqlc.arg(period_end),
    sqlc.arg(policy_version), 'paid', 'active', 'upgrade_delta',
    sqlc.arg(source_reference_id), 'upgrade_delta', sqlc.arg(change_id)
)
ON CONFLICT (org_id, source, source_reference_id) DO NOTHING;

-- name: InsertUpgradeDeltaGrant :exec
INSERT INTO credit_grants (
    grant_id, org_id, scope_type, scope_product_id, scope_bucket_id, scope_sku_id,
    amount, source, source_reference_id, entitlement_period_id, policy_version,
    starts_at, period_start, period_end, expires_at, account_id, deposit_transfer_id,
    ledger_posting_state
) VALUES (
    sqlc.arg(grant_id), sqlc.arg(org_id), sqlc.arg(scope_type),
    NULLIF(sqlc.arg(scope_product_id), ''), NULLIF(sqlc.arg(scope_bucket_id), ''),
    NULLIF(sqlc.arg(scope_sku_id), ''), sqlc.arg(amount), 'contract',
    sqlc.arg(source_reference_id), sqlc.arg(entitlement_period_id), sqlc.arg(policy_version),
    sqlc.arg(starts_at), sqlc.arg(starts_at), sqlc.arg(period_end), sqlc.arg(period_end),
    sqlc.arg(account_id)::bytea, sqlc.arg(deposit_transfer_id)::bytea, 'pending'
)
ON CONFLICT (org_id, source, scope_type, scope_product_id, scope_bucket_id, scope_sku_id, source_reference_id) DO NOTHING;

-- name: ListPlanEntitlementPolicies :many
SELECT e.policy_id,
       e.product_id,
       e.scope_type,
       COALESCE(e.scope_product_id, '') AS scope_product_id,
       COALESCE(e.scope_bucket_id, '') AS scope_bucket_id,
       COALESCE(e.scope_sku_id, '') AS scope_sku_id,
       e.amount_units,
       e.cadence,
       e.anchor_kind,
       e.proration_mode,
       e.policy_version
FROM plan_entitlements pe
JOIN entitlement_policies e ON e.policy_id = pe.policy_id
WHERE pe.plan_id = sqlc.arg(plan_id)
ORDER BY pe.sort_order,
         e.policy_id;

-- name: GetActivePlan :one
SELECT plan_id,
       product_id,
       display_name,
       billing_mode,
       tier,
       currency,
       monthly_amount_cents,
       annual_amount_cents,
       active,
       is_default
FROM plans
WHERE plan_id = sqlc.arg(plan_id)
  AND active;

-- name: EnsureDocumentNumberAllocator :exec
INSERT INTO document_number_allocators (issuer_id, document_year, prefix, next_number)
VALUES ('verself', sqlc.arg(document_year), 'VS', 1)
ON CONFLICT (issuer_id, document_year) DO NOTHING;

-- name: LockDocumentNumberAllocator :one
SELECT next_number
FROM document_number_allocators
WHERE issuer_id = 'verself'
  AND document_year = sqlc.arg(document_year)
FOR UPDATE;

-- name: AdvanceDocumentNumberAllocator :exec
UPDATE document_number_allocators
SET next_number = next_number + 1
WHERE issuer_id = 'verself'
  AND document_year = sqlc.arg(document_year);
