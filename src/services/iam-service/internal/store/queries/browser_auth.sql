-- name: InsertBrowserLoginTransaction :exec
INSERT INTO iam_browser_login_transactions (
  state_hash,
  client_hash,
  nonce,
  code_verifier,
  redirect_to,
  purpose,
  login_hint,
  required_subject,
  required_email,
  required_org_id,
  provider_session_id,
  provider_session_token_ciphertext,
  expires_at
) VALUES (
  sqlc.arg(state_hash),
  sqlc.arg(client_hash),
  sqlc.arg(nonce),
  sqlc.arg(code_verifier),
  sqlc.arg(redirect_to),
  sqlc.arg(purpose),
  sqlc.narg(login_hint),
  sqlc.narg(required_subject),
  sqlc.narg(required_email),
  sqlc.narg(required_org_id),
  sqlc.arg(provider_session_id),
  sqlc.arg(provider_session_token_ciphertext),
  sqlc.arg(expires_at)
);

-- name: DeleteBrowserLoginTransaction :one
DELETE FROM iam_browser_login_transactions
WHERE state_hash = sqlc.arg(state_hash)
  AND expires_at > now()
RETURNING state_hash, client_hash, nonce, code_verifier, redirect_to, purpose, login_hint, required_subject, required_email, required_org_id, provider_session_id, provider_session_token_ciphertext, expires_at, created_at;

-- name: DeleteExpiredBrowserLoginTransactions :exec
DELETE FROM iam_browser_login_transactions
WHERE expires_at <= now();

-- name: UpsertBrowserClient :exec
INSERT INTO iam_browser_clients (
  client_hash,
  client_handle,
  active_account_handle,
  client_cache_partition,
  expires_at,
  created_client_ip,
  created_client_ip_trusted,
  created_client_ip_source,
  created_edge_peer_ip,
  created_user_agent,
  created_device_label,
  created_device_kind,
  created_browser_name,
  created_os_name,
  created_geo_country_code,
  created_geo_region,
  created_geo_city,
  last_seen_client_ip,
  last_seen_client_ip_trusted,
  last_seen_client_ip_source,
  last_seen_edge_peer_ip,
  last_seen_user_agent,
  last_seen_device_label,
  last_seen_device_kind,
  last_seen_browser_name,
  last_seen_os_name,
  last_seen_geo_country_code,
  last_seen_geo_region,
  last_seen_geo_city,
  last_seen_at
) VALUES (
  sqlc.arg(client_hash),
  sqlc.arg(client_handle),
  sqlc.narg(active_account_handle),
  sqlc.arg(client_cache_partition),
  sqlc.arg(expires_at),
  sqlc.arg(client_ip),
  sqlc.arg(client_ip_trusted),
  sqlc.arg(client_ip_source),
  sqlc.arg(edge_peer_ip),
  sqlc.arg(user_agent),
  sqlc.arg(device_label),
  sqlc.arg(device_kind),
  sqlc.arg(browser_name),
  sqlc.arg(os_name),
  sqlc.arg(geo_country_code),
  sqlc.arg(geo_region),
  sqlc.arg(geo_city),
  sqlc.arg(client_ip),
  sqlc.arg(client_ip_trusted),
  sqlc.arg(client_ip_source),
  sqlc.arg(edge_peer_ip),
  sqlc.arg(user_agent),
  sqlc.arg(device_label),
  sqlc.arg(device_kind),
  sqlc.arg(browser_name),
  sqlc.arg(os_name),
  sqlc.arg(geo_country_code),
  sqlc.arg(geo_region),
  sqlc.arg(geo_city),
  now()
)
ON CONFLICT (client_hash) DO UPDATE SET
  client_cache_partition = EXCLUDED.client_cache_partition,
  expires_at = EXCLUDED.expires_at,
  last_seen_client_ip = EXCLUDED.last_seen_client_ip,
  last_seen_client_ip_trusted = EXCLUDED.last_seen_client_ip_trusted,
  last_seen_client_ip_source = EXCLUDED.last_seen_client_ip_source,
  last_seen_edge_peer_ip = EXCLUDED.last_seen_edge_peer_ip,
  last_seen_user_agent = EXCLUDED.last_seen_user_agent,
  last_seen_device_label = EXCLUDED.last_seen_device_label,
  last_seen_device_kind = EXCLUDED.last_seen_device_kind,
  last_seen_browser_name = EXCLUDED.last_seen_browser_name,
  last_seen_os_name = EXCLUDED.last_seen_os_name,
  last_seen_geo_country_code = EXCLUDED.last_seen_geo_country_code,
  last_seen_geo_region = EXCLUDED.last_seen_geo_region,
  last_seen_geo_city = EXCLUDED.last_seen_geo_city,
  last_seen_at = EXCLUDED.last_seen_at,
  updated_at = now();

-- name: GetBrowserClient :one
SELECT
  client_hash,
  client_handle,
  active_account_handle,
  client_cache_partition,
  expires_at,
  created_client_ip,
  created_client_ip_trusted,
  created_client_ip_source,
  created_edge_peer_ip,
  created_user_agent,
  created_device_label,
  created_device_kind,
  created_browser_name,
  created_os_name,
  created_geo_country_code,
  created_geo_region,
  created_geo_city,
  last_seen_client_ip,
  last_seen_client_ip_trusted,
  last_seen_client_ip_source,
  last_seen_edge_peer_ip,
  last_seen_user_agent,
  last_seen_device_label,
  last_seen_device_kind,
  last_seen_browser_name,
  last_seen_os_name,
  last_seen_geo_country_code,
  last_seen_geo_region,
  last_seen_geo_city,
  last_seen_at,
  created_at,
  updated_at
FROM iam_browser_clients
WHERE client_hash = sqlc.arg(client_hash)
  AND expires_at > now();

-- name: DeleteBrowserClient :exec
DELETE FROM iam_browser_clients
WHERE client_hash = sqlc.arg(client_hash);

-- name: SetBrowserClientActiveAccount :exec
UPDATE iam_browser_clients
SET active_account_handle = sqlc.narg(account_handle),
    client_cache_partition = sqlc.arg(client_cache_partition),
    updated_at = now()
WHERE client_hash = sqlc.arg(client_hash);

-- name: TouchBrowserClientSeen :exec
UPDATE iam_browser_clients
SET last_seen_client_ip = sqlc.arg(client_ip),
    last_seen_client_ip_trusted = sqlc.arg(client_ip_trusted),
    last_seen_client_ip_source = sqlc.arg(client_ip_source),
    last_seen_edge_peer_ip = sqlc.arg(edge_peer_ip),
    last_seen_user_agent = sqlc.arg(user_agent),
    last_seen_device_label = sqlc.arg(device_label),
    last_seen_device_kind = sqlc.arg(device_kind),
    last_seen_browser_name = sqlc.arg(browser_name),
    last_seen_os_name = sqlc.arg(os_name),
    last_seen_geo_country_code = sqlc.arg(geo_country_code),
    last_seen_geo_region = sqlc.arg(geo_region),
    last_seen_geo_city = sqlc.arg(geo_city),
    last_seen_at = now(),
    updated_at = now()
WHERE client_hash = sqlc.arg(client_hash);

-- name: UpsertBrowserAccount :exec
INSERT INTO iam_browser_accounts (
  account_handle,
  client_hash,
  state,
  subject,
  email,
  display_name,
  preferred_username,
  org_id,
  home_org_id,
  selected_org_id,
  available_org_contexts,
  user_claims,
  id_token_ciphertext,
  access_token_ciphertext,
  refresh_token_ciphertext,
  provider_session_id,
  provider_session_token_ciphertext,
  token_scope,
  expires_at,
  created_client_ip,
  created_client_ip_trusted,
  created_client_ip_source,
  created_edge_peer_ip,
  created_user_agent,
  created_device_label,
  created_device_kind,
  created_browser_name,
  created_os_name,
  created_geo_country_code,
  created_geo_region,
  created_geo_city,
  last_seen_client_ip,
  last_seen_client_ip_trusted,
  last_seen_client_ip_source,
  last_seen_edge_peer_ip,
  last_seen_user_agent,
  last_seen_device_label,
  last_seen_device_kind,
  last_seen_browser_name,
  last_seen_os_name,
  last_seen_geo_country_code,
  last_seen_geo_region,
  last_seen_geo_city,
  last_seen_at
) VALUES (
  sqlc.arg(account_handle),
  sqlc.arg(client_hash),
  sqlc.arg(state),
  sqlc.arg(subject),
  sqlc.narg(email),
  sqlc.narg(display_name),
  sqlc.narg(preferred_username),
  sqlc.narg(org_id),
  sqlc.narg(home_org_id),
  sqlc.narg(selected_org_id),
  sqlc.arg(available_org_contexts_json)::jsonb,
  sqlc.arg(user_claims_json)::jsonb,
  sqlc.narg(id_token_ciphertext),
  sqlc.arg(access_token_ciphertext),
  sqlc.narg(refresh_token_ciphertext),
  sqlc.arg(provider_session_id),
  sqlc.arg(provider_session_token_ciphertext),
  sqlc.narg(token_scope),
  sqlc.arg(expires_at),
  sqlc.arg(client_ip),
  sqlc.arg(client_ip_trusted),
  sqlc.arg(client_ip_source),
  sqlc.arg(edge_peer_ip),
  sqlc.arg(user_agent),
  sqlc.arg(device_label),
  sqlc.arg(device_kind),
  sqlc.arg(browser_name),
  sqlc.arg(os_name),
  sqlc.arg(geo_country_code),
  sqlc.arg(geo_region),
  sqlc.arg(geo_city),
  sqlc.arg(client_ip),
  sqlc.arg(client_ip_trusted),
  sqlc.arg(client_ip_source),
  sqlc.arg(edge_peer_ip),
  sqlc.arg(user_agent),
  sqlc.arg(device_label),
  sqlc.arg(device_kind),
  sqlc.arg(browser_name),
  sqlc.arg(os_name),
  sqlc.arg(geo_country_code),
  sqlc.arg(geo_region),
  sqlc.arg(geo_city),
  now()
)
ON CONFLICT (client_hash, subject) DO UPDATE SET
  state = EXCLUDED.state,
  email = EXCLUDED.email,
  display_name = EXCLUDED.display_name,
  preferred_username = EXCLUDED.preferred_username,
  org_id = EXCLUDED.org_id,
  home_org_id = EXCLUDED.home_org_id,
  selected_org_id = EXCLUDED.selected_org_id,
  available_org_contexts = EXCLUDED.available_org_contexts,
  user_claims = EXCLUDED.user_claims,
  id_token_ciphertext = EXCLUDED.id_token_ciphertext,
  access_token_ciphertext = EXCLUDED.access_token_ciphertext,
  refresh_token_ciphertext = EXCLUDED.refresh_token_ciphertext,
  provider_session_id = EXCLUDED.provider_session_id,
  provider_session_token_ciphertext = EXCLUDED.provider_session_token_ciphertext,
  token_scope = EXCLUDED.token_scope,
  expires_at = EXCLUDED.expires_at,
  last_seen_client_ip = EXCLUDED.last_seen_client_ip,
  last_seen_client_ip_trusted = EXCLUDED.last_seen_client_ip_trusted,
  last_seen_client_ip_source = EXCLUDED.last_seen_client_ip_source,
  last_seen_edge_peer_ip = EXCLUDED.last_seen_edge_peer_ip,
  last_seen_user_agent = EXCLUDED.last_seen_user_agent,
  last_seen_device_label = EXCLUDED.last_seen_device_label,
  last_seen_device_kind = EXCLUDED.last_seen_device_kind,
  last_seen_browser_name = EXCLUDED.last_seen_browser_name,
  last_seen_os_name = EXCLUDED.last_seen_os_name,
  last_seen_geo_country_code = EXCLUDED.last_seen_geo_country_code,
  last_seen_geo_region = EXCLUDED.last_seen_geo_region,
  last_seen_geo_city = EXCLUDED.last_seen_geo_city,
  last_seen_at = EXCLUDED.last_seen_at,
  updated_at = now();

-- name: GetBrowserAccount :one
SELECT
  account_handle,
  client_hash,
  state,
  subject,
  email,
  display_name,
  preferred_username,
  org_id,
  home_org_id,
  selected_org_id,
  available_org_contexts::text AS available_org_contexts_json,
  user_claims::text AS user_claims_json,
  id_token_ciphertext,
  access_token_ciphertext,
  refresh_token_ciphertext,
  provider_session_id,
  provider_session_token_ciphertext,
  token_scope,
  expires_at,
  created_client_ip,
  created_client_ip_trusted,
  created_client_ip_source,
  created_edge_peer_ip,
  created_user_agent,
  created_device_label,
  created_device_kind,
  created_browser_name,
  created_os_name,
  created_geo_country_code,
  created_geo_region,
  created_geo_city,
  last_seen_client_ip,
  last_seen_client_ip_trusted,
  last_seen_client_ip_source,
  last_seen_edge_peer_ip,
  last_seen_user_agent,
  last_seen_device_label,
  last_seen_device_kind,
  last_seen_browser_name,
  last_seen_os_name,
  last_seen_geo_country_code,
  last_seen_geo_region,
  last_seen_geo_city,
  last_seen_at,
  created_at,
  updated_at
FROM iam_browser_accounts
WHERE account_handle = sqlc.arg(account_handle)
  AND client_hash = sqlc.arg(client_hash)
  AND state = 'active';

-- name: GetActiveBrowserAccount :one
SELECT
  c.client_handle,
  c.client_cache_partition,
  a.account_handle,
  a.client_hash,
  a.state,
  a.subject,
  a.email,
  a.display_name,
  a.preferred_username,
  a.org_id,
  a.home_org_id,
  a.selected_org_id,
  a.available_org_contexts::text AS available_org_contexts_json,
  a.user_claims::text AS user_claims_json,
  a.id_token_ciphertext,
  a.access_token_ciphertext,
  a.refresh_token_ciphertext,
  a.provider_session_id,
  a.provider_session_token_ciphertext,
  a.token_scope,
  a.expires_at,
  a.created_client_ip,
  a.created_client_ip_trusted,
  a.created_client_ip_source,
  a.created_edge_peer_ip,
  a.created_user_agent,
  a.created_device_label,
  a.created_device_kind,
  a.created_browser_name,
  a.created_os_name,
  a.created_geo_country_code,
  a.created_geo_region,
  a.created_geo_city,
  a.last_seen_client_ip,
  a.last_seen_client_ip_trusted,
  a.last_seen_client_ip_source,
  a.last_seen_edge_peer_ip,
  a.last_seen_user_agent,
  a.last_seen_device_label,
  a.last_seen_device_kind,
  a.last_seen_browser_name,
  a.last_seen_os_name,
  a.last_seen_geo_country_code,
  a.last_seen_geo_region,
  a.last_seen_geo_city,
  a.last_seen_at,
  a.created_at,
  a.updated_at
FROM iam_browser_clients c
JOIN iam_browser_accounts a
  ON a.client_hash = c.client_hash
 AND a.account_handle = c.active_account_handle
WHERE c.client_hash = sqlc.arg(client_hash)
  AND c.expires_at > now()
  AND a.state = 'active';

-- name: ListBrowserAccountsForClient :many
SELECT
  account_handle,
  client_hash,
  state,
  subject,
  email,
  display_name,
  preferred_username,
  org_id,
  home_org_id,
  selected_org_id,
  available_org_contexts::text AS available_org_contexts_json,
  expires_at,
  created_client_ip,
  created_client_ip_trusted,
  created_client_ip_source,
  created_edge_peer_ip,
  created_user_agent,
  created_device_label,
  created_device_kind,
  created_browser_name,
  created_os_name,
  created_geo_country_code,
  created_geo_region,
  created_geo_city,
  last_seen_client_ip,
  last_seen_client_ip_trusted,
  last_seen_client_ip_source,
  last_seen_edge_peer_ip,
  last_seen_user_agent,
  last_seen_device_label,
  last_seen_device_kind,
  last_seen_browser_name,
  last_seen_os_name,
  last_seen_geo_country_code,
  last_seen_geo_region,
  last_seen_geo_city,
  last_seen_at,
  created_at,
  updated_at
FROM iam_browser_accounts
WHERE client_hash = sqlc.arg(client_hash)
  AND state = 'active'
ORDER BY last_seen_at DESC, created_at DESC;

-- name: DeleteBrowserAccountByHandle :exec
UPDATE iam_browser_accounts
SET state = 'removed',
    updated_at = now()
WHERE client_hash = sqlc.arg(client_hash)
  AND account_handle = sqlc.arg(account_handle);

-- name: DeleteBrowserAccountsForClient :exec
UPDATE iam_browser_accounts
SET state = 'removed',
    updated_at = now()
WHERE client_hash = sqlc.arg(client_hash);

-- name: UpdateBrowserAccountSeen :exec
UPDATE iam_browser_accounts
SET last_seen_client_ip = sqlc.arg(client_ip),
    last_seen_client_ip_trusted = sqlc.arg(client_ip_trusted),
    last_seen_client_ip_source = sqlc.arg(client_ip_source),
    last_seen_edge_peer_ip = sqlc.arg(edge_peer_ip),
    last_seen_user_agent = sqlc.arg(user_agent),
    last_seen_device_label = sqlc.arg(device_label),
    last_seen_device_kind = sqlc.arg(device_kind),
    last_seen_browser_name = sqlc.arg(browser_name),
    last_seen_os_name = sqlc.arg(os_name),
    last_seen_geo_country_code = sqlc.arg(geo_country_code),
    last_seen_geo_region = sqlc.arg(geo_region),
    last_seen_geo_city = sqlc.arg(geo_city),
    last_seen_at = now(),
    updated_at = now()
WHERE account_handle = sqlc.arg(account_handle)
  AND client_hash = sqlc.arg(client_hash);

-- name: InsertBrowserAccountObservation :exec
INSERT INTO iam_browser_account_observations (
  client_hash,
  account_handle,
  subject,
  client_ip,
  client_ip_trusted,
  client_ip_source,
  edge_peer_ip,
  user_agent,
  device_label,
  device_kind,
  browser_name,
  os_name,
  geo_country_code,
  geo_region,
  geo_city
) VALUES (
  sqlc.arg(client_hash),
  sqlc.arg(account_handle),
  sqlc.arg(subject),
  sqlc.arg(client_ip),
  sqlc.arg(client_ip_trusted),
  sqlc.arg(client_ip_source),
  sqlc.arg(edge_peer_ip),
  sqlc.arg(user_agent),
  sqlc.arg(device_label),
  sqlc.arg(device_kind),
  sqlc.arg(browser_name),
  sqlc.arg(os_name),
  sqlc.arg(geo_country_code),
  sqlc.arg(geo_region),
  sqlc.arg(geo_city)
);

-- name: UpdateBrowserAccountOrganization :exec
UPDATE iam_browser_accounts
SET org_id = sqlc.arg(selected_org_id),
    selected_org_id = sqlc.arg(selected_org_id),
    updated_at = now()
WHERE account_handle = sqlc.arg(account_handle)
  AND client_hash = sqlc.arg(client_hash);

-- name: DeleteBrowserResourceTokens :exec
DELETE FROM iam_browser_resource_tokens
WHERE account_handle = sqlc.arg(account_handle);

-- name: GetBrowserResourceToken :one
SELECT access_token_ciphertext, token_scope, expires_at
FROM iam_browser_resource_tokens
WHERE account_handle = sqlc.arg(account_handle)
  AND audience = sqlc.arg(audience)
  AND selected_org_id = sqlc.arg(selected_org_id)
  AND scope_hash = sqlc.arg(scope_hash);

-- name: UpsertBrowserResourceToken :exec
INSERT INTO iam_browser_resource_tokens (
  account_handle,
  audience,
  selected_org_id,
  scope_hash,
  access_token_ciphertext,
  token_scope,
  expires_at
) VALUES (
  sqlc.arg(account_handle),
  sqlc.arg(audience),
  sqlc.arg(selected_org_id),
  sqlc.arg(scope_hash),
  sqlc.arg(access_token_ciphertext),
  sqlc.narg(token_scope),
  sqlc.arg(expires_at)
)
ON CONFLICT (account_handle, audience, selected_org_id, scope_hash) DO UPDATE SET
  access_token_ciphertext = EXCLUDED.access_token_ciphertext,
  token_scope = EXCLUDED.token_scope,
  expires_at = EXCLUDED.expires_at,
  updated_at = now();
