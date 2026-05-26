-- name: GetAccountSubject :one
SELECT
  s.issuer,
  s.subject,
  s.account_id,
  s.state,
  s.email,
  s.email_verified,
  s.display_name,
  s.claims::text AS claims_json,
  s.created_at,
  s.last_seen_at,
  s.updated_at,
  a.primary_email,
  a.display_name AS account_display_name,
  a.created_at AS account_created_at
FROM iam_account_subjects s
JOIN iam_accounts a ON a.account_id = s.account_id
WHERE s.issuer = sqlc.arg(issuer)
  AND s.subject = sqlc.arg(subject)
  AND s.state = 'linked'
  AND a.state = 'active';

-- name: CountVerifiedAccountEmailsByIdentityKey :one
SELECT count(DISTINCT account_id)::bigint AS account_count
FROM iam_account_emails
WHERE email_identity_key = sqlc.arg(email_identity_key)
  AND verified = true;

-- name: GetAccountEmailByIdentityKeyForUpdate :one
SELECT
  e.account_id,
  e.email,
  e.email_identity_key,
  e.verified,
  e.source,
  e.first_seen_at,
  e.last_seen_at,
  a.state AS account_state,
  a.primary_email,
  a.display_name
FROM iam_account_emails e
JOIN iam_accounts a ON a.account_id = e.account_id
WHERE e.email_identity_key = sqlc.arg(email_identity_key)
FOR UPDATE OF e, a;

-- name: LockAccountEmailIdentityKey :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(email_identity_key), 0));

-- name: CreateSignupAccountReservation :execrows
INSERT INTO iam_accounts (
  account_id,
  state,
  primary_email,
  display_name
) VALUES (
  sqlc.arg(account_id),
  'provisioning',
  sqlc.arg(primary_email),
  sqlc.arg(display_name)
)
ON CONFLICT (account_id) DO UPDATE SET
  primary_email = CASE WHEN iam_accounts.primary_email = '' THEN EXCLUDED.primary_email ELSE iam_accounts.primary_email END,
  display_name = CASE WHEN iam_accounts.display_name = '' THEN EXCLUDED.display_name ELSE iam_accounts.display_name END,
  updated_at = now()
WHERE iam_accounts.state = 'provisioning';

-- name: InsertSignupAccountEmail :exec
INSERT INTO iam_account_emails (
  account_id,
  email,
  email_identity_key,
  verified,
  source,
  first_seen_at,
  last_seen_at
) VALUES (
  sqlc.arg(account_id),
  sqlc.arg(email),
  sqlc.arg(email_identity_key),
  true,
  'signup',
  now(),
  now()
);

-- name: ActivateSignupAccount :execrows
UPDATE iam_accounts
SET state = 'active',
    primary_email = sqlc.arg(primary_email),
    display_name = sqlc.arg(display_name),
    updated_at = now()
WHERE account_id = sqlc.arg(account_id)
  AND state IN ('provisioning', 'active');

-- name: CreateAccount :exec
INSERT INTO iam_accounts (
  account_id,
  primary_email,
  display_name
) VALUES (
  sqlc.arg(account_id),
  sqlc.arg(primary_email),
  sqlc.arg(display_name)
)
ON CONFLICT (account_id) DO UPDATE SET
  primary_email = CASE WHEN iam_accounts.primary_email = '' THEN EXCLUDED.primary_email ELSE iam_accounts.primary_email END,
  display_name = CASE WHEN iam_accounts.display_name = '' THEN EXCLUDED.display_name ELSE iam_accounts.display_name END,
  updated_at = now();

-- name: UpsertAccountSubject :execrows
INSERT INTO iam_account_subjects (
  issuer,
  subject,
  account_id,
  state,
  email,
  email_verified,
  display_name,
  claims,
  created_at,
  last_seen_at,
  updated_at
) VALUES (
  sqlc.arg(issuer),
  sqlc.arg(subject),
  sqlc.arg(account_id),
  'linked',
  sqlc.arg(email),
  sqlc.arg(email_verified),
  sqlc.arg(display_name),
  sqlc.arg(claims_json)::jsonb,
  now(),
  now(),
  now()
)
ON CONFLICT (issuer, subject) DO UPDATE SET
  state = 'linked',
  email = EXCLUDED.email,
  email_verified = EXCLUDED.email_verified,
  display_name = EXCLUDED.display_name,
  claims = EXCLUDED.claims,
  last_seen_at = now(),
  updated_at = now()
WHERE iam_account_subjects.account_id = EXCLUDED.account_id;

-- name: UpsertAccountEmail :exec
INSERT INTO iam_account_emails (
  account_id,
  email,
  email_identity_key,
  verified,
  source,
  first_seen_at,
  last_seen_at
) VALUES (
  sqlc.arg(account_id),
  sqlc.arg(email),
  sqlc.arg(email_identity_key),
  sqlc.arg(verified),
  sqlc.arg(source),
  now(),
  now()
)
ON CONFLICT (account_id, email_identity_key) DO UPDATE SET
  email = EXCLUDED.email,
  verified = EXCLUDED.verified,
  source = EXCLUDED.source,
  last_seen_at = now();

-- name: UpsertAccountConnection :execrows
INSERT INTO iam_account_connections (
  connection_id,
  account_id,
  issuer,
  subject,
  state,
  email,
  email_verified,
  created_at,
  last_seen_at,
  updated_at
) VALUES (
  sqlc.arg(connection_id),
  sqlc.arg(account_id),
  sqlc.arg(issuer),
  sqlc.arg(subject),
  'linked',
  sqlc.arg(email),
  sqlc.arg(email_verified),
  now(),
  now(),
  now()
)
ON CONFLICT (issuer, subject) DO UPDATE SET
  state = 'linked',
  email = EXCLUDED.email,
  email_verified = EXCLUDED.email_verified,
  last_seen_at = now(),
  updated_at = now()
WHERE iam_account_connections.account_id = EXCLUDED.account_id;

-- name: GetAccountConnectionByProviderSubject :one
SELECT connection_id, account_id, issuer, subject, state, email, email_verified, created_at, last_seen_at, updated_at
FROM iam_account_connections
WHERE issuer = sqlc.arg(issuer)
  AND subject = sqlc.arg(subject);

-- name: ListAccountConnections :many
SELECT connection_id, account_id, issuer, subject, state, email, email_verified, created_at, last_seen_at, updated_at
FROM iam_account_connections
WHERE account_id = sqlc.arg(account_id)
  AND state = 'linked'
ORDER BY last_seen_at DESC, created_at DESC, connection_id;

-- name: RemoveAccountConnection :exec
UPDATE iam_account_connections
SET state = 'removed',
    updated_at = now()
WHERE account_id = sqlc.arg(account_id)
  AND connection_id = sqlc.arg(connection_id);

-- name: UpsertDeviceSession :one
INSERT INTO iam_device_sessions (
  session_id,
  account_id,
  issuer,
  subject,
  channel,
  state,
  device_label,
  credential_fingerprint,
  provider_session_id,
  auth_time,
  auth_methods,
  expires_at,
  created_client_ip,
  created_client_ip_trusted,
  created_client_ip_source,
  created_user_agent,
  last_seen_client_ip,
  last_seen_client_ip_trusted,
  last_seen_client_ip_source,
  last_seen_user_agent,
  created_at,
  last_seen_at,
  updated_at
) VALUES (
  sqlc.arg(session_id),
  sqlc.arg(account_id),
  sqlc.arg(issuer),
  sqlc.arg(subject),
  sqlc.arg(channel),
  'active',
  sqlc.arg(device_label),
  sqlc.arg(credential_fingerprint),
  sqlc.arg(provider_session_id),
  sqlc.arg(auth_time),
  sqlc.arg(auth_methods)::text[],
  sqlc.arg(expires_at),
  sqlc.arg(client_ip),
  sqlc.arg(client_ip_trusted),
  sqlc.arg(client_ip_source),
  sqlc.arg(user_agent),
  sqlc.arg(client_ip),
  sqlc.arg(client_ip_trusted),
  sqlc.arg(client_ip_source),
  sqlc.arg(user_agent),
  now(),
  now(),
  now()
)
ON CONFLICT (session_id) DO UPDATE SET
  state = 'active',
  device_label = EXCLUDED.device_label,
  credential_fingerprint = EXCLUDED.credential_fingerprint,
  provider_session_id = EXCLUDED.provider_session_id,
  auth_time = EXCLUDED.auth_time,
  auth_methods = EXCLUDED.auth_methods,
  expires_at = EXCLUDED.expires_at,
  last_seen_client_ip = EXCLUDED.last_seen_client_ip,
  last_seen_client_ip_trusted = EXCLUDED.last_seen_client_ip_trusted,
  last_seen_client_ip_source = EXCLUDED.last_seen_client_ip_source,
  last_seen_user_agent = EXCLUDED.last_seen_user_agent,
  last_seen_at = now(),
  updated_at = now()
RETURNING session_id, account_id, issuer, subject, channel, state, device_label, credential_fingerprint, provider_session_id, auth_time, auth_methods, expires_at, revoked_at, revoked_reason, created_client_ip, created_client_ip_trusted, created_client_ip_source, created_user_agent, last_seen_client_ip, last_seen_client_ip_trusted, last_seen_client_ip_source, last_seen_user_agent, created_at, last_seen_at, updated_at;

-- name: GetDeviceSession :one
SELECT session_id, account_id, issuer, subject, channel, state, device_label, credential_fingerprint, provider_session_id, auth_time, auth_methods, expires_at, revoked_at, revoked_reason, created_client_ip, created_client_ip_trusted, created_client_ip_source, created_user_agent, last_seen_client_ip, last_seen_client_ip_trusted, last_seen_client_ip_source, last_seen_user_agent, created_at, last_seen_at, updated_at
FROM iam_device_sessions
WHERE session_id = sqlc.arg(session_id);

-- name: TouchDeviceSession :one
UPDATE iam_device_sessions
SET last_seen_client_ip = sqlc.arg(client_ip),
    last_seen_client_ip_trusted = sqlc.arg(client_ip_trusted),
    last_seen_client_ip_source = sqlc.arg(client_ip_source),
    last_seen_user_agent = sqlc.arg(user_agent),
    last_seen_at = now(),
    updated_at = now()
WHERE session_id = sqlc.arg(session_id)
  AND account_id = sqlc.arg(account_id)
  AND state = 'active'
  AND expires_at > now()
RETURNING session_id, account_id, issuer, subject, channel, state, device_label, credential_fingerprint, provider_session_id, auth_time, auth_methods, expires_at, revoked_at, revoked_reason, created_client_ip, created_client_ip_trusted, created_client_ip_source, created_user_agent, last_seen_client_ip, last_seen_client_ip_trusted, last_seen_client_ip_source, last_seen_user_agent, created_at, last_seen_at, updated_at;

-- name: ListDeviceSessionsForAccount :many
SELECT session_id, account_id, issuer, subject, channel, state, device_label, credential_fingerprint, provider_session_id, auth_time, auth_methods, expires_at, revoked_at, revoked_reason, created_client_ip, created_client_ip_trusted, created_client_ip_source, created_user_agent, last_seen_client_ip, last_seen_client_ip_trusted, last_seen_client_ip_source, last_seen_user_agent, created_at, last_seen_at, updated_at
FROM iam_device_sessions
WHERE account_id = sqlc.arg(account_id)
ORDER BY
  CASE state WHEN 'active' THEN 0 WHEN 'revoked' THEN 1 ELSE 2 END,
  last_seen_at DESC,
  session_id;

-- name: RevokeDeviceSession :exec
UPDATE iam_device_sessions
SET state = 'revoked',
    revoked_at = now(),
    revoked_reason = sqlc.arg(revoked_reason),
    updated_at = now()
WHERE account_id = sqlc.arg(account_id)
  AND session_id = sqlc.arg(session_id)
  AND state <> 'revoked';
