-- name: InitializeAPIActivityChainState :exec
INSERT INTO governance_api_activity_chain_state (org_id, sequence, row_hmac)
VALUES (sqlc.arg(org_id), 0, sqlc.arg(row_hmac))
ON CONFLICT (org_id) DO NOTHING;

-- name: LockAPIActivityChainState :one
SELECT sequence, row_hmac
FROM governance_api_activity_chain_state
WHERE org_id = sqlc.arg(org_id)
FOR UPDATE;

-- name: AdvanceAPIActivityChainState :exec
UPDATE governance_api_activity_chain_state
SET sequence = sqlc.arg(sequence), row_hmac = sqlc.arg(row_hmac), updated_at = now()
WHERE org_id = sqlc.arg(org_id);

-- name: InsertAPIActivity :exec
INSERT INTO governance_api_activities (
    org_id, sequence, metadata_uid, observed_at, event_date, ingested_at,
    ocsf_version, ocsf_json, ocsf_sha256, row_json, resources_json,
    prev_hmac, row_hmac, hmac_key_id
) VALUES (
    sqlc.arg(org_id), sqlc.arg(sequence), sqlc.arg(metadata_uid), sqlc.arg(observed_at),
    sqlc.arg(event_date), sqlc.arg(ingested_at), sqlc.arg(ocsf_version),
    sqlc.arg(ocsf_json), sqlc.arg(ocsf_sha256), sqlc.arg(row_json),
    sqlc.arg(resources_json), sqlc.arg(prev_hmac), sqlc.arg(row_hmac),
    sqlc.arg(hmac_key_id)
);

-- name: ClaimPendingAPIActivityRows :many
SELECT row_json, ocsf_json, resources_json
FROM governance_api_activities
WHERE projected_at IS NULL
ORDER BY observed_at ASC, sequence ASC, metadata_uid ASC
LIMIT sqlc.arg(limit_count)
FOR UPDATE SKIP LOCKED;

-- name: MarkAPIActivityProjected :execrows
UPDATE governance_api_activities
SET projected_at = COALESCE(projected_at, now())
WHERE org_id = sqlc.arg(org_id)
  AND sequence = sqlc.arg(sequence);
