-- name: InsertBrowserLoginTransaction :exec
INSERT INTO iam_browser_login_transactions (
  state_hash,
  nonce,
  code_verifier,
  redirect_to,
  expires_at
) VALUES (
  sqlc.arg(state_hash),
  sqlc.arg(nonce),
  sqlc.arg(code_verifier),
  sqlc.arg(redirect_to),
  sqlc.arg(expires_at)
);

-- name: DeleteBrowserLoginTransaction :one
DELETE FROM iam_browser_login_transactions
WHERE state_hash = sqlc.arg(state_hash)
  AND expires_at > now()
RETURNING state_hash, nonce, code_verifier, redirect_to, expires_at, created_at;

-- name: DeleteExpiredBrowserLoginTransactions :exec
DELETE FROM iam_browser_login_transactions
WHERE expires_at <= now();

-- name: UpsertBrowserSession :exec
INSERT INTO iam_browser_sessions (
  session_hash,
  client_cache_partition,
  subject,
  email,
  display_name,
  preferred_username,
  org_id,
  home_org_id,
  selected_org_id,
  roles,
  available_org_contexts,
  user_claims,
  id_token,
  access_token,
  refresh_token,
  token_scope,
  expires_at
) VALUES (
  sqlc.arg(session_hash),
  sqlc.arg(client_cache_partition),
  sqlc.arg(subject),
  sqlc.narg(email),
  sqlc.narg(display_name),
  sqlc.narg(preferred_username),
  sqlc.narg(org_id),
  sqlc.narg(home_org_id),
  sqlc.narg(selected_org_id),
  sqlc.arg(roles),
  sqlc.arg(available_org_contexts_json)::jsonb,
  sqlc.arg(user_claims_json)::jsonb,
  sqlc.narg(id_token),
  sqlc.arg(access_token),
  sqlc.narg(refresh_token),
  sqlc.narg(token_scope),
  sqlc.arg(expires_at)
)
ON CONFLICT (session_hash) DO UPDATE SET
  client_cache_partition = EXCLUDED.client_cache_partition,
  subject = EXCLUDED.subject,
  email = EXCLUDED.email,
  display_name = EXCLUDED.display_name,
  preferred_username = EXCLUDED.preferred_username,
  org_id = EXCLUDED.org_id,
  home_org_id = EXCLUDED.home_org_id,
  selected_org_id = EXCLUDED.selected_org_id,
  roles = EXCLUDED.roles,
  available_org_contexts = EXCLUDED.available_org_contexts,
  user_claims = EXCLUDED.user_claims,
  id_token = EXCLUDED.id_token,
  access_token = EXCLUDED.access_token,
  refresh_token = EXCLUDED.refresh_token,
  token_scope = EXCLUDED.token_scope,
  expires_at = EXCLUDED.expires_at,
  updated_at = now();

-- name: GetBrowserSession :one
SELECT
  session_hash,
  client_cache_partition,
  subject,
  email,
  display_name,
  preferred_username,
  org_id,
  home_org_id,
  selected_org_id,
  roles,
  available_org_contexts::text AS available_org_contexts_json,
  user_claims::text AS user_claims_json,
  id_token,
  access_token,
  refresh_token,
  token_scope,
  expires_at,
  created_at,
  updated_at
FROM iam_browser_sessions
WHERE session_hash = sqlc.arg(session_hash);

-- name: DeleteBrowserSession :exec
DELETE FROM iam_browser_sessions
WHERE session_hash = sqlc.arg(session_hash);

-- name: UpdateBrowserSessionOrganization :exec
UPDATE iam_browser_sessions
SET org_id = sqlc.arg(selected_org_id),
    selected_org_id = sqlc.arg(selected_org_id),
    roles = sqlc.arg(roles),
    client_cache_partition = sqlc.arg(client_cache_partition),
    updated_at = now()
WHERE session_hash = sqlc.arg(session_hash);

-- name: DeleteBrowserResourceTokens :exec
DELETE FROM iam_browser_resource_tokens
WHERE session_hash = sqlc.arg(session_hash);

-- name: GetBrowserResourceToken :one
SELECT access_token, token_scope, expires_at
FROM iam_browser_resource_tokens
WHERE session_hash = sqlc.arg(session_hash)
  AND audience = sqlc.arg(audience)
  AND selected_org_id = sqlc.arg(selected_org_id)
  AND scope_hash = sqlc.arg(scope_hash);

-- name: UpsertBrowserResourceToken :exec
INSERT INTO iam_browser_resource_tokens (
  session_hash,
  audience,
  selected_org_id,
  scope_hash,
  access_token,
  token_scope,
  expires_at
) VALUES (
  sqlc.arg(session_hash),
  sqlc.arg(audience),
  sqlc.arg(selected_org_id),
  sqlc.arg(scope_hash),
  sqlc.arg(access_token),
  sqlc.narg(token_scope),
  sqlc.arg(expires_at)
)
ON CONFLICT (session_hash, audience, selected_org_id, scope_hash) DO UPDATE SET
  access_token = EXCLUDED.access_token,
  token_scope = EXCLUDED.token_scope,
  expires_at = EXCLUDED.expires_at,
  updated_at = now();
