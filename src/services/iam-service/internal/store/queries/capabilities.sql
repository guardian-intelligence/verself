-- name: GetMemberCapabilities :one
SELECT org_id, enabled_keys, version, updated_at, updated_by
FROM iam_member_capabilities
WHERE org_id = sqlc.arg(org_id);

-- name: InsertMemberCapabilities :one
INSERT INTO iam_member_capabilities (org_id, enabled_keys, version, updated_at, updated_by)
VALUES (sqlc.arg(org_id), sqlc.arg(enabled_keys), 1, now(), sqlc.arg(updated_by))
ON CONFLICT (org_id) DO NOTHING
RETURNING org_id, enabled_keys, version, updated_at, updated_by;

-- name: UpdateMemberCapabilities :one
UPDATE iam_member_capabilities
SET enabled_keys = sqlc.arg(enabled_keys),
    version = version + 1,
    updated_at = now(),
    updated_by = sqlc.arg(updated_by)
WHERE org_id = sqlc.arg(org_id) AND version = sqlc.arg(version)
RETURNING org_id, enabled_keys, version, updated_at, updated_by;
