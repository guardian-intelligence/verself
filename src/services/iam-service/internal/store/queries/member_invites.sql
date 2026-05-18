-- name: InsertMemberInviteAcceptanceToken :exec
INSERT INTO iam_member_invite_acceptance_tokens (
    token_hash,
    org_id,
    user_id,
    email,
    email_verification_code,
    password_reset_code,
    created_at,
    expires_at
) VALUES (
    sqlc.arg(token_hash),
    sqlc.arg(org_id),
    sqlc.arg(user_id),
    sqlc.arg(email),
    sqlc.arg(email_verification_code),
    sqlc.arg(password_reset_code),
    sqlc.arg(created_at),
    sqlc.arg(expires_at)
);

-- name: GetActiveMemberInviteAcceptanceToken :one
SELECT token_hash, org_id, user_id, email, email_verification_code, password_reset_code, created_at, expires_at, accepted_at
FROM iam_member_invite_acceptance_tokens
WHERE token_hash = sqlc.arg(token_hash)
  AND accepted_at IS NULL
  AND expires_at > sqlc.arg(now);

-- name: AcceptMemberInviteAcceptanceToken :execrows
UPDATE iam_member_invite_acceptance_tokens
SET accepted_at = sqlc.arg(accepted_at)
WHERE token_hash = sqlc.arg(token_hash)
  AND accepted_at IS NULL
  AND expires_at > sqlc.arg(accepted_at);
