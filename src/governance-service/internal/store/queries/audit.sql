-- name: InitializeAuditChainState :exec
INSERT INTO governance_audit_chain_state (org_id, sequence, row_hmac)
VALUES (sqlc.arg(org_id), 0, sqlc.arg(row_hmac))
ON CONFLICT (org_id) DO NOTHING;

-- name: LockAuditChainState :one
SELECT sequence, row_hmac
FROM governance_audit_chain_state
WHERE org_id = sqlc.arg(org_id)
FOR UPDATE;

-- name: AdvanceAuditChainState :exec
UPDATE governance_audit_chain_state
SET sequence = sqlc.arg(sequence), row_hmac = sqlc.arg(row_hmac), updated_at = now()
WHERE org_id = sqlc.arg(org_id);

-- name: InsertAuditEvent :exec
INSERT INTO governance_audit_events (
    org_id, sequence, event_id, recorded_at, event_date, ingested_at,
    schema_version, payload_json, row_json, prev_hmac, row_hmac, hmac_key_id
) VALUES (
    sqlc.arg(org_id), sqlc.arg(sequence), sqlc.arg(event_id), sqlc.arg(recorded_at),
    sqlc.arg(event_date), sqlc.arg(ingested_at), sqlc.arg(schema_version),
    sqlc.arg(payload_json), sqlc.arg(row_json), sqlc.arg(prev_hmac),
    sqlc.arg(row_hmac), sqlc.arg(hmac_key_id)
);

-- name: ClaimPendingAuditEventRows :many
SELECT row_json
FROM governance_audit_events
WHERE projected_at IS NULL
ORDER BY recorded_at ASC, sequence ASC, event_id ASC
LIMIT sqlc.arg(limit_count)
FOR UPDATE SKIP LOCKED;

-- name: MarkAuditEventProjected :execrows
UPDATE governance_audit_events
SET projected_at = COALESCE(projected_at, now())
WHERE org_id = sqlc.arg(org_id)
  AND sequence = sqlc.arg(sequence);
