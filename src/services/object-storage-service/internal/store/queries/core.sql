-- name: CreateBucket :exec
INSERT INTO object_storage_buckets (
    bucket_id, org_id, bucket_name, provider, provider_bucket_id, quota_bytes, quota_objects,
    lifecycle_json, created_at, created_by, updated_at, updated_by
)
VALUES (
    sqlc.arg(bucket_id), sqlc.arg(org_id), sqlc.arg(bucket_name), sqlc.arg(provider), sqlc.arg(provider_bucket_id), sqlc.arg(quota_bytes), sqlc.arg(quota_objects),
    sqlc.arg(lifecycle_json)::jsonb, sqlc.arg(created_at), sqlc.arg(created_by), sqlc.arg(updated_at), sqlc.arg(updated_by)
);

-- name: UpdateBucket :one
UPDATE object_storage_buckets
SET quota_bytes = sqlc.arg(quota_bytes),
    quota_objects = sqlc.arg(quota_objects),
    lifecycle_json = sqlc.arg(lifecycle_json)::jsonb,
    updated_at = sqlc.arg(updated_at),
    updated_by = sqlc.arg(updated_by)
WHERE bucket_id = sqlc.arg(bucket_id)
RETURNING bucket_id;

-- name: BucketByID :one
SELECT bucket_id, org_id, bucket_name, provider, provider_bucket_id, quota_bytes, quota_objects,
       lifecycle_json, created_at, created_by, updated_at, updated_by
FROM object_storage_buckets
WHERE bucket_id = $1;

-- name: BucketByName :one
SELECT bucket_id, org_id, bucket_name, provider, provider_bucket_id, quota_bytes, quota_objects,
       lifecycle_json, created_at, created_by, updated_at, updated_by
FROM object_storage_buckets
WHERE bucket_name = $1;

-- name: ListBuckets :many
SELECT bucket_id, org_id, bucket_name, provider, provider_bucket_id, quota_bytes, quota_objects,
       lifecycle_json, created_at, created_by, updated_at, updated_by
FROM object_storage_buckets
ORDER BY created_at DESC, bucket_id DESC;

-- name: ListBucketsByOrg :many
SELECT bucket_id, org_id, bucket_name, provider, provider_bucket_id, quota_bytes, quota_objects,
       lifecycle_json, created_at, created_by, updated_at, updated_by
FROM object_storage_buckets
WHERE org_id = $1
ORDER BY created_at DESC, bucket_id DESC;

-- name: DeleteBucket :one
DELETE FROM object_storage_buckets
WHERE bucket_id = $1
RETURNING bucket_id;

-- name: CreateAlias :exec
INSERT INTO object_storage_bucket_aliases (alias, bucket_id, prefix, service_tag, created_at, created_by)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: DeleteAlias :one
DELETE FROM object_storage_bucket_aliases
WHERE alias = $1
RETURNING alias;

-- name: AliasesByBucket :many
SELECT alias, bucket_id, prefix, service_tag, created_at, created_by
FROM object_storage_bucket_aliases
WHERE bucket_id = $1
ORDER BY alias;

-- name: ResolveBucketAlias :one
SELECT b.bucket_id, b.org_id, b.bucket_name, b.provider, b.provider_bucket_id, b.quota_bytes, b.quota_objects,
       b.lifecycle_json, b.created_at, b.created_by, b.updated_at, b.updated_by,
       a.alias, a.bucket_id AS alias_bucket_id, a.prefix, a.service_tag, a.created_at AS alias_created_at, a.created_by AS alias_created_by
FROM object_storage_bucket_aliases a
JOIN object_storage_buckets b ON b.bucket_id = a.bucket_id
WHERE a.alias = $1;

-- name: CreateCredential :exec
INSERT INTO object_storage_credentials (
    credential_id, bucket_id, auth_mode, display_name, access_key_id, spiffe_subject,
    secret_hash, secret_fingerprint, secret_ciphertext, secret_nonce, status,
    expires_at, created_at, created_by, revoked_at, revoked_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16);

-- name: DeleteCredential :one
DELETE FROM object_storage_credentials
WHERE credential_id = $1
RETURNING credential_id;

-- name: RevokeCredentialByAccessKey :one
UPDATE object_storage_credentials
SET status = sqlc.arg(revoked_status), revoked_at = sqlc.arg(revoked_at), revoked_by = sqlc.arg(revoked_by)
WHERE access_key_id = sqlc.arg(access_key_id) AND auth_mode = sqlc.arg(auth_mode) AND status = sqlc.arg(active_status)
RETURNING credential_id;

-- name: ActiveCredentialByAccessKeyID :one
SELECT credential_id, bucket_id, auth_mode, display_name, access_key_id, spiffe_subject,
       secret_hash, secret_fingerprint, secret_ciphertext, secret_nonce, status, expires_at,
       created_at, created_by, revoked_at, revoked_by
FROM object_storage_credentials
WHERE access_key_id = $1 AND auth_mode = $2 AND status = $3;

-- name: CredentialByID :one
SELECT credential_id, bucket_id, auth_mode, display_name, access_key_id, spiffe_subject,
       secret_hash, secret_fingerprint, secret_ciphertext, secret_nonce, status, expires_at,
       created_at, created_by, revoked_at, revoked_by
FROM object_storage_credentials
WHERE credential_id = $1;

-- name: ActiveCredentialBySPIFFE :one
SELECT credential_id, bucket_id, auth_mode, display_name, access_key_id, spiffe_subject,
       secret_hash, secret_fingerprint, secret_ciphertext, secret_nonce, status, expires_at,
       created_at, created_by, revoked_at, revoked_by
FROM object_storage_credentials
WHERE spiffe_subject = $1 AND auth_mode = $2 AND status = $3;

-- name: CredentialsByBucket :many
SELECT credential_id, bucket_id, auth_mode, display_name, access_key_id, spiffe_subject,
       secret_hash, secret_fingerprint, secret_ciphertext, secret_nonce, status, expires_at,
       created_at, created_by, revoked_at, revoked_by
FROM object_storage_credentials
WHERE bucket_id = $1
ORDER BY created_at DESC, credential_id DESC;

-- name: RevokeCredentialByID :one
UPDATE object_storage_credentials
SET status = sqlc.arg(revoked_status), revoked_at = sqlc.arg(revoked_at), revoked_by = sqlc.arg(revoked_by)
WHERE credential_id = sqlc.arg(credential_id) AND status = sqlc.arg(active_status)
RETURNING credential_id;

-- name: CreateWriteSession :exec
INSERT INTO object_storage_write_sessions (
    session_id, site, capability, namespace, bucket_id, idempotency_key,
    status, expires_at, created_at, created_by, completed_at
)
VALUES (
    sqlc.arg(session_id), sqlc.arg(site), sqlc.arg(capability), sqlc.arg(namespace), sqlc.arg(bucket_id),
    sqlc.arg(idempotency_key), sqlc.arg(status), sqlc.arg(expires_at), sqlc.arg(created_at),
    sqlc.arg(created_by), sqlc.arg(completed_at)
);

-- name: WriteSessionByID :one
SELECT session_id, site, capability, namespace, bucket_id, idempotency_key,
       status, expires_at, created_at, created_by, completed_at
FROM object_storage_write_sessions
WHERE site = sqlc.arg(site) AND session_id = sqlc.arg(session_id);

-- name: WriteSessionByIdempotencyKey :one
SELECT session_id, site, capability, namespace, bucket_id, idempotency_key,
       status, expires_at, created_at, created_by, completed_at
FROM object_storage_write_sessions
WHERE site = sqlc.arg(site)
  AND capability = sqlc.arg(capability)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: CompleteWriteSession :one
UPDATE object_storage_write_sessions
SET status = sqlc.arg(completed_status), completed_at = sqlc.arg(completed_at)
WHERE site = sqlc.arg(site)
  AND session_id = sqlc.arg(session_id)
  AND status = sqlc.arg(open_status)
RETURNING session_id, site, capability, namespace, bucket_id, idempotency_key,
          status, expires_at, created_at, created_by, completed_at;

-- name: CreateWriteSessionObject :exec
INSERT INTO object_storage_write_session_objects (
    session_id, name, object_key, sha256, size_bytes, action, created_at
)
VALUES (
    sqlc.arg(session_id), sqlc.arg(name), sqlc.arg(object_key), sqlc.arg(sha256),
    sqlc.arg(size_bytes), sqlc.arg(action), sqlc.arg(created_at)
);

-- name: WriteSessionObjectsBySession :many
SELECT session_id, name, object_key, sha256, size_bytes, action, created_at
FROM object_storage_write_session_objects
WHERE session_id = sqlc.arg(session_id)
ORDER BY name;
