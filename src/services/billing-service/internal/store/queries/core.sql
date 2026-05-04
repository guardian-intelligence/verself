-- name: BusinessNow :one
SELECT COALESCE(
    (SELECT business_now FROM billing_clock_overrides bco WHERE bco.scope_kind = 'org_product' AND bco.scope_id = sqlc.arg(p_org_product_scope)),
    (SELECT business_now FROM billing_clock_overrides bco WHERE bco.scope_kind = 'org' AND bco.scope_id = sqlc.arg(p_org_scope)),
    (SELECT business_now FROM billing_clock_overrides bco WHERE bco.scope_kind = 'global' AND bco.scope_id = ''),
    transaction_timestamp()
)::timestamptz AS business_now;

-- name: UpsertOrg :exec
INSERT INTO orgs (org_id, display_name, billing_email, trust_tier, overage_policy, overage_consent_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (org_id) DO UPDATE
SET display_name = EXCLUDED.display_name,
    billing_email = EXCLUDED.billing_email,
    trust_tier = EXCLUDED.trust_tier,
    overage_policy = EXCLUDED.overage_policy,
    overage_consent_at = EXCLUDED.overage_consent_at,
    updated_at = now();

-- name: ListProductIDs :many
SELECT product_id
FROM products
WHERE active
ORDER BY product_id;

-- name: ListActivePlans :many
SELECT plan_id, product_id, display_name, tier, billing_mode, monthly_amount_cents, annual_amount_cents, currency, active, is_default
FROM plans
WHERE product_id = $1 AND active AND NOT is_default
ORDER BY monthly_amount_cents, plan_id;

-- name: InsertBillingEvent :exec
INSERT INTO billing_events (
    event_id, event_type, event_version, aggregate_type, aggregate_id,
    org_id, product_id, occurred_at, payload, payload_hash, correlation_id, causation_event_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NULLIF($12, ''))
ON CONFLICT (event_id) DO NOTHING;

-- name: InsertBillingEventDelivery :exec
INSERT INTO billing_event_delivery_queue (event_id, sink, generation, state, next_attempt_at)
VALUES ($1, $2, 1, 'pending', now())
ON CONFLICT (event_id, sink) WHERE state <> 'dead_letter' DO NOTHING;

-- name: ClaimBillingEventDeliveries :many
UPDATE billing_event_delivery_queue q
SET state = 'in_progress',
    attempts = q.attempts + 1,
    last_attempt_at = now(),
    lease_expires_at = now() + sqlc.arg(lease_duration)::interval,
    leased_by = sqlc.arg(leased_by),
    last_attempt_id = sqlc.arg(attempt_id),
    updated_at = now()
WHERE (q.event_id, q.sink, q.generation) IN (
    SELECT q2.event_id, q2.sink, q2.generation
    FROM billing_event_delivery_queue q2
    WHERE q2.sink = sqlc.arg(p_sink)
      AND q2.state IN ('pending', 'retryable_failed')
      AND q2.next_attempt_at <= now()
    ORDER BY q2.next_attempt_at, q2.event_id, q2.generation
    LIMIT sqlc.arg(limit_count)
    FOR UPDATE SKIP LOCKED
)
RETURNING q.event_id, q.sink, q.generation;

-- name: MarkBillingEventDeliverySucceeded :exec
DELETE FROM billing_event_delivery_queue
WHERE event_id = $1 AND sink = $2 AND generation = $3;

-- name: MarkBillingEventDeliveryFailed :exec
UPDATE billing_event_delivery_queue
SET state = CASE WHEN attempts >= 25 THEN 'dead_letter' ELSE 'retryable_failed' END,
    next_attempt_at = CASE WHEN attempts >= 25 THEN next_attempt_at ELSE now() + sqlc.arg(retry_after)::interval END,
    lease_expires_at = NULL,
    leased_by = '',
    delivery_error = sqlc.arg(delivery_error),
    dead_lettered_at = CASE WHEN attempts >= 25 THEN now() ELSE NULL END,
    dead_letter_reason = CASE WHEN attempts >= 25 THEN sqlc.arg(delivery_error) ELSE '' END,
    updated_at = now()
WHERE event_id = sqlc.arg(event_id) AND sink = sqlc.arg(p_sink) AND generation = sqlc.arg(generation);

-- name: GetBillingEventForProjection :one
SELECT event_id, event_type, event_version, aggregate_type, aggregate_id, org_id, product_id, occurred_at, payload, payload_hash, correlation_id, COALESCE(causation_event_id, '') AS causation_event_id, created_at
FROM billing_events
WHERE event_id = $1;
