-- name: GetOrgACLState :one
SELECT org_id, version, updated_at, updated_by
FROM iam_org_acl_state
WHERE org_id = sqlc.arg(org_id);

-- name: EnsureOrgACLState :exec
INSERT INTO iam_org_acl_state (org_id, version, updated_at, updated_by)
VALUES (sqlc.arg(org_id), 1, sqlc.arg(updated_at), sqlc.arg(updated_by))
ON CONFLICT (org_id) DO NOTHING;

-- name: GetOrgACLStateForUpdate :one
SELECT org_id, version, updated_at, updated_by
FROM iam_org_acl_state
WHERE org_id = sqlc.arg(org_id)
FOR UPDATE;

-- name: UpdateOrgACLState :exec
UPDATE iam_org_acl_state
SET version = sqlc.arg(version), updated_at = sqlc.arg(updated_at), updated_by = sqlc.arg(updated_by)
WHERE org_id = sqlc.arg(org_id);

-- name: LookupCommandResultForUpdate :one
SELECT command_id, request_hash, result, reason, aggregate_version, target_user_id,
       requested_role_keys, expected_role_keys, actual_role_keys
FROM iam_command_results
WHERE command_id = sqlc.arg(command_id)
FOR UPDATE;

-- name: InsertCommandResult :exec
INSERT INTO iam_command_results (
    command_id, org_id, actor_id, operation_id, idempotency_key_hash,
    request_hash, result, reason, aggregate_kind, aggregate_id, aggregate_version,
    target_user_id, requested_role_keys, expected_role_keys, actual_role_keys, created_at
) VALUES (
    sqlc.arg(command_id), sqlc.arg(org_id), sqlc.arg(actor_id), sqlc.arg(operation_id), sqlc.arg(idempotency_key_hash),
    sqlc.arg(request_hash), sqlc.arg(result), sqlc.arg(reason), sqlc.arg(aggregate_kind), sqlc.arg(aggregate_id), sqlc.arg(aggregate_version),
    sqlc.arg(target_user_id),
    COALESCE(sqlc.arg(requested_role_keys)::text[], ARRAY[]::text[]),
    COALESCE(sqlc.arg(expected_role_keys)::text[], ARRAY[]::text[]),
    COALESCE(sqlc.arg(actual_role_keys)::text[], ARRAY[]::text[]),
    sqlc.arg(created_at)
);

-- name: InsertDomainEventOutbox :exec
INSERT INTO iam_domain_event_outbox (
    event_id, command_id, event_type, org_id, actor_id, operation_id, idempotency_key_hash,
    aggregate_kind, aggregate_id, aggregate_version, target_kind, target_id,
    result, reason, conflict_policy, expected_version, actual_version,
    expected_hash, actual_hash, requested_hash, changed_fields, payload,
    traceparent, occurred_at, next_attempt_at
) VALUES (
    sqlc.arg(event_id), sqlc.arg(command_id), sqlc.arg(event_type), sqlc.arg(org_id), sqlc.arg(actor_id), sqlc.arg(operation_id), sqlc.arg(idempotency_key_hash),
    sqlc.arg(aggregate_kind), sqlc.arg(aggregate_id), sqlc.arg(aggregate_version), sqlc.arg(target_kind), sqlc.arg(target_id),
    sqlc.arg(result), sqlc.arg(reason), sqlc.arg(conflict_policy), sqlc.arg(expected_version), sqlc.arg(actual_version),
    sqlc.arg(expected_hash), sqlc.arg(actual_hash), sqlc.arg(requested_hash),
    COALESCE(sqlc.arg(changed_fields)::text[], ARRAY[]::text[]),
    sqlc.arg(payload),
    sqlc.arg(traceparent), sqlc.arg(occurred_at), sqlc.arg(occurred_at)
);
