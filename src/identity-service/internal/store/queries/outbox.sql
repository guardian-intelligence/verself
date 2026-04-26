-- name: ClaimPendingDomainLedgerEvents :many
SELECT event_id, occurred_at, event_type, org_id, actor_id, operation_id, command_id,
       idempotency_key_hash, aggregate_kind, aggregate_id, aggregate_version,
       target_kind, target_id, result, reason, conflict_policy, expected_version,
       actual_version, expected_hash, actual_hash, requested_hash, changed_fields,
       payload::text AS payload_json, traceparent
FROM identity_domain_event_outbox
WHERE projected_at IS NULL AND next_attempt_at <= now()
ORDER BY occurred_at, event_id
LIMIT sqlc.arg(limit_count)
FOR UPDATE SKIP LOCKED;

-- name: MarkDomainLedgerProjected :exec
UPDATE identity_domain_event_outbox
SET projected_at = COALESCE(projected_at, now()),
    attempts = attempts + 1,
    last_error = ''
WHERE event_id = sqlc.arg(event_id);

-- name: MarkDomainLedgerProjectionFailed :exec
UPDATE identity_domain_event_outbox
SET attempts = attempts + 1,
    last_error = sqlc.arg(last_error),
    next_attempt_at = now() + interval '1 second'
WHERE event_id = sqlc.arg(event_id);
