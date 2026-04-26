-- name: InsertAPICredential :exec
INSERT INTO identity_api_credentials (
    credential_id, org_id, subject_id, client_id, display_name, auth_method, status,
    policy_version_at_issue, created_at, created_by, updated_at, expires_at
)
VALUES (
    sqlc.arg(credential_id), sqlc.arg(org_id), sqlc.arg(subject_id), sqlc.arg(client_id), sqlc.arg(display_name),
    sqlc.arg(auth_method), sqlc.arg(status), sqlc.arg(policy_version_at_issue), sqlc.arg(created_at), sqlc.arg(created_by),
    sqlc.arg(created_at), sqlc.narg(expires_at)
);

-- name: InsertAPICredentialPermission :exec
INSERT INTO identity_api_credential_permissions (credential_id, permission, created_at)
VALUES (sqlc.arg(credential_id), sqlc.arg(permission), sqlc.arg(created_at));

-- name: ListAPICredentials :many
SELECT c.credential_id, c.org_id, c.subject_id, c.client_id, c.display_name, c.status,
       c.auth_method,
       COALESCE((
           SELECT s.fingerprint
           FROM identity_api_credential_secrets s
           WHERE s.credential_id = c.credential_id AND s.revoked_at IS NULL
           ORDER BY s.created_at DESC
           LIMIT 1
       ), ''::text)::text AS fingerprint,
       c.policy_version_at_issue, c.created_at, c.created_by, c.updated_at,
       c.expires_at, c.revoked_at, COALESCE(c.revoked_by, '') AS revoked_by, c.last_used_at
FROM identity_api_credentials c
WHERE c.org_id = sqlc.arg(org_id)
ORDER BY c.created_at DESC, c.credential_id DESC;

-- name: GetAPICredential :one
SELECT c.credential_id, c.org_id, c.subject_id, c.client_id, c.display_name, c.status,
       c.auth_method,
       COALESCE((
           SELECT s.fingerprint
           FROM identity_api_credential_secrets s
           WHERE s.credential_id = c.credential_id AND s.revoked_at IS NULL
           ORDER BY s.created_at DESC
           LIMIT 1
       ), ''::text)::text AS fingerprint,
       c.policy_version_at_issue, c.created_at, c.created_by, c.updated_at,
       c.expires_at, c.revoked_at, COALESCE(c.revoked_by, '') AS revoked_by, c.last_used_at
FROM identity_api_credentials c
WHERE c.org_id = sqlc.arg(org_id) AND c.credential_id = sqlc.arg(credential_id);

-- name: ListAPICredentialPermissions :many
SELECT permission
FROM identity_api_credential_permissions
WHERE credential_id = sqlc.arg(credential_id)
ORDER BY permission;

-- name: ListActiveAPICredentialSecrets :many
SELECT s.secret_id, s.credential_id, s.auth_method, s.provider_key_id, s.fingerprint,
       s.secret_hash, s.hash_algorithm, s.created_at, s.created_by, s.expires_at, s.revoked_at,
       COALESCE(s.revoked_by, '') AS revoked_by
FROM identity_api_credential_secrets s
JOIN identity_api_credentials c ON c.credential_id = s.credential_id
WHERE c.org_id = sqlc.arg(org_id) AND c.credential_id = sqlc.arg(credential_id) AND s.revoked_at IS NULL
ORDER BY s.created_at DESC;

-- name: InsertAPICredentialSecret :exec
INSERT INTO identity_api_credential_secrets (
    secret_id, credential_id, auth_method, provider_key_id, fingerprint, secret_hash,
    hash_algorithm, created_at, created_by, expires_at, revoked_at, revoked_by
)
VALUES (
    sqlc.arg(secret_id), sqlc.arg(credential_id), sqlc.arg(auth_method), sqlc.arg(provider_key_id), sqlc.arg(fingerprint),
    sqlc.arg(secret_hash), sqlc.arg(hash_algorithm), sqlc.arg(created_at), sqlc.arg(created_by),
    sqlc.narg(expires_at), sqlc.narg(revoked_at), NULLIF(sqlc.arg(revoked_by), '')
);

-- name: RevokeActiveAPICredentialSecrets :exec
UPDATE identity_api_credential_secrets s
SET revoked_at = sqlc.arg(revoked_at), revoked_by = COALESCE(NULLIF(s.revoked_by, ''), sqlc.arg(revoked_by))
FROM identity_api_credentials c
WHERE c.credential_id = s.credential_id
  AND c.org_id = sqlc.arg(org_id)
  AND c.credential_id = sqlc.arg(credential_id)
  AND s.revoked_at IS NULL;

-- name: UpdateAPICredentialAfterRoll :execrows
UPDATE identity_api_credentials
SET auth_method = sqlc.arg(auth_method), updated_at = sqlc.arg(updated_at)
WHERE org_id = sqlc.arg(org_id) AND credential_id = sqlc.arg(credential_id) AND status = 'active';

-- name: RevokeAPICredentialSecrets :exec
UPDATE identity_api_credential_secrets s
SET revoked_at = COALESCE(s.revoked_at, sqlc.arg(revoked_at)), revoked_by = COALESCE(NULLIF(s.revoked_by, ''), sqlc.arg(revoked_by))
FROM identity_api_credentials c
WHERE c.credential_id = s.credential_id
  AND c.org_id = sqlc.arg(org_id)
  AND c.credential_id = sqlc.arg(credential_id)
  AND s.revoked_at IS NULL;

-- name: RevokeAPICredential :execrows
UPDATE identity_api_credentials
SET status = 'revoked', revoked_at = sqlc.arg(revoked_at), revoked_by = sqlc.arg(revoked_by), updated_at = sqlc.arg(revoked_at)
WHERE org_id = sqlc.arg(org_id) AND credential_id = sqlc.arg(credential_id) AND status = 'active';

-- name: ResolveAPICredentialClaims :one
SELECT c.credential_id, c.org_id, c.display_name, c.auth_method,
       COALESCE((
           SELECT s.fingerprint
           FROM identity_api_credential_secrets s
           WHERE s.credential_id = c.credential_id AND s.revoked_at IS NULL
           ORDER BY s.created_at DESC
           LIMIT 1
       ), ''::text)::text AS fingerprint,
       c.created_by
FROM identity_api_credentials c
WHERE c.subject_id = sqlc.arg(subject_id)
  AND c.status = 'active'
  AND (c.expires_at IS NULL OR c.expires_at > sqlc.arg(used_at));

-- name: RecordAPICredentialUse :exec
UPDATE identity_api_credentials
SET last_used_at = sqlc.arg(used_at), updated_at = sqlc.arg(used_at)
WHERE credential_id = sqlc.arg(credential_id);
