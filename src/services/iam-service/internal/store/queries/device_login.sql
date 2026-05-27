-- name: UpsertDeviceLoginIntent :one
INSERT INTO iam_device_login_intents (
  device_login_handle,
  user_code_hash,
  user_code_suffix,
  provider_device_authorization_id,
  client_id,
  app_name,
  project_name,
  scopes,
  expires_at
) VALUES (
  sqlc.arg(device_login_handle),
  sqlc.arg(user_code_hash),
  sqlc.arg(user_code_suffix),
  sqlc.arg(provider_device_authorization_id),
  sqlc.arg(client_id),
  sqlc.arg(app_name),
  sqlc.arg(project_name),
  sqlc.arg(scopes_json)::jsonb,
  sqlc.arg(expires_at)
)
ON CONFLICT (device_login_handle) DO UPDATE SET
  provider_device_authorization_id = EXCLUDED.provider_device_authorization_id,
  client_id = EXCLUDED.client_id,
  app_name = EXCLUDED.app_name,
  project_name = EXCLUDED.project_name,
  scopes = EXCLUDED.scopes,
  state = CASE
    WHEN iam_device_login_intents.state IN ('approved', 'denied') THEN iam_device_login_intents.state
    WHEN iam_device_login_intents.expires_at <= now() THEN 'expired'
    ELSE EXCLUDED.state
  END,
  expires_at = EXCLUDED.expires_at,
  updated_at = now()
RETURNING
  device_login_handle,
  user_code_hash,
  user_code_suffix,
  provider_device_authorization_id,
  client_id,
  app_name,
  project_name,
  scopes::text AS scopes_json,
  state,
  required_subject,
  required_email,
  required_org_id,
  approved_by_account_handle,
  approved_by_subject,
  approved_by_email,
  denied_by_account_handle,
  denied_by_subject,
  denied_by_email,
  approved_at,
  denied_at,
  expires_at,
  created_at,
  updated_at;

-- name: GetDeviceLoginIntent :one
SELECT
  device_login_handle,
  user_code_hash,
  user_code_suffix,
  provider_device_authorization_id,
  client_id,
  app_name,
  project_name,
  scopes::text AS scopes_json,
  state,
  required_subject,
  required_email,
  required_org_id,
  approved_by_account_handle,
  approved_by_subject,
  approved_by_email,
  denied_by_account_handle,
  denied_by_subject,
  denied_by_email,
  approved_at,
  denied_at,
  expires_at,
  created_at,
  updated_at
FROM iam_device_login_intents
WHERE device_login_handle = sqlc.arg(device_login_handle);

-- name: GetDeviceLoginIntentByUserCodeHash :one
SELECT
  device_login_handle,
  user_code_hash,
  user_code_suffix,
  provider_device_authorization_id,
  client_id,
  app_name,
  project_name,
  scopes::text AS scopes_json,
  state,
  required_subject,
  required_email,
  required_org_id,
  approved_by_account_handle,
  approved_by_subject,
  approved_by_email,
  denied_by_account_handle,
  denied_by_subject,
  denied_by_email,
  approved_at,
  denied_at,
  expires_at,
  created_at,
  updated_at
FROM iam_device_login_intents
WHERE user_code_hash = sqlc.arg(user_code_hash);

-- name: MarkExpiredDeviceLoginIntent :one
UPDATE iam_device_login_intents
SET state = 'expired',
    updated_at = now()
WHERE device_login_handle = sqlc.arg(device_login_handle)
  AND state = 'pending'
  AND expires_at <= now()
RETURNING
  device_login_handle,
  user_code_hash,
  user_code_suffix,
  provider_device_authorization_id,
  client_id,
  app_name,
  project_name,
  scopes::text AS scopes_json,
  state,
  required_subject,
  required_email,
  required_org_id,
  approved_by_account_handle,
  approved_by_subject,
  approved_by_email,
  denied_by_account_handle,
  denied_by_subject,
  denied_by_email,
  approved_at,
  denied_at,
  expires_at,
  created_at,
  updated_at;

-- name: ApproveDeviceLoginIntent :one
UPDATE iam_device_login_intents
SET state = 'approved',
    approved_by_account_handle = sqlc.arg(approved_by_account_handle),
    approved_by_subject = sqlc.arg(approved_by_subject),
    approved_by_email = sqlc.arg(approved_by_email),
    approved_at = now(),
    updated_at = now()
WHERE device_login_handle = sqlc.arg(device_login_handle)
  AND state = 'pending'
  AND expires_at > now()
RETURNING
  device_login_handle,
  user_code_hash,
  user_code_suffix,
  provider_device_authorization_id,
  client_id,
  app_name,
  project_name,
  scopes::text AS scopes_json,
  state,
  required_subject,
  required_email,
  required_org_id,
  approved_by_account_handle,
  approved_by_subject,
  approved_by_email,
  denied_by_account_handle,
  denied_by_subject,
  denied_by_email,
  approved_at,
  denied_at,
  expires_at,
  created_at,
  updated_at;

-- name: DenyDeviceLoginIntent :one
UPDATE iam_device_login_intents
SET state = 'denied',
    denied_by_account_handle = sqlc.arg(denied_by_account_handle),
    denied_by_subject = sqlc.arg(denied_by_subject),
    denied_by_email = sqlc.arg(denied_by_email),
    denied_at = now(),
    updated_at = now()
WHERE device_login_handle = sqlc.arg(device_login_handle)
  AND state = 'pending'
  AND expires_at > now()
RETURNING
  device_login_handle,
  user_code_hash,
  user_code_suffix,
  provider_device_authorization_id,
  client_id,
  app_name,
  project_name,
  scopes::text AS scopes_json,
  state,
  required_subject,
  required_email,
  required_org_id,
  approved_by_account_handle,
  approved_by_subject,
  approved_by_email,
  denied_by_account_handle,
  denied_by_subject,
  denied_by_email,
  approved_at,
  denied_at,
  expires_at,
  created_at,
  updated_at;
