-- name: InsertSignupIntent :one
INSERT INTO iam_signup_intents (
    signup_intent_id, idempotency_key, request_hash, email, email_hash,
    organization_display_name, requested_organization_slug, given_name, family_name,
    verification_token_hash, state, org_id, verification_expires_at
) VALUES (
    sqlc.arg(signup_intent_id), sqlc.arg(idempotency_key), sqlc.arg(request_hash), sqlc.arg(email), sqlc.arg(email_hash),
    sqlc.arg(organization_display_name), sqlc.arg(requested_organization_slug), sqlc.arg(given_name), sqlc.arg(family_name),
    sqlc.arg(verification_token_hash), 'pending_verification', sqlc.arg(org_id), sqlc.arg(verification_expires_at)
)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING signup_intent_id, idempotency_key, request_hash, email, email_hash,
          organization_display_name, requested_organization_slug, organization_slug, given_name, family_name,
          verification_token_hash, state, materialization_step, materialization_attempts,
          materialization_last_error, materialization_lease_expires_at, verify_idempotency_key, verify_request_hash,
          org_id, identity_provider_org_id, identity_provider_user_id,
          created_at, updated_at, verification_expires_at, verified_at, completed_at;

-- name: GetSignupIntentByIdempotencyKey :one
SELECT signup_intent_id, idempotency_key, request_hash, email, email_hash,
       organization_display_name, requested_organization_slug, organization_slug, given_name, family_name,
       verification_token_hash, state, materialization_step, materialization_attempts,
       materialization_last_error, materialization_lease_expires_at, verify_idempotency_key, verify_request_hash,
       org_id, identity_provider_org_id, identity_provider_user_id,
       created_at, updated_at, verification_expires_at, verified_at, completed_at
FROM iam_signup_intents
WHERE idempotency_key = sqlc.arg(idempotency_key);

-- name: GetSignupIntentForUpdate :one
SELECT signup_intent_id, idempotency_key, request_hash, email, email_hash,
       organization_display_name, requested_organization_slug, organization_slug, given_name, family_name,
       verification_token_hash, state, materialization_step, materialization_attempts,
       materialization_last_error, materialization_lease_expires_at, verify_idempotency_key, verify_request_hash,
       org_id, identity_provider_org_id, identity_provider_user_id,
       created_at, updated_at, verification_expires_at, verified_at, completed_at
FROM iam_signup_intents
WHERE signup_intent_id = sqlc.arg(signup_intent_id)
FOR UPDATE;

-- name: DeletePendingSignupIntent :execrows
DELETE FROM iam_signup_intents
WHERE signup_intent_id = sqlc.arg(signup_intent_id)
  AND state = 'pending_verification'
  AND identity_provider_org_id = ''
  AND identity_provider_user_id = '';

-- name: MarkSignupIntentMaterializing :exec
UPDATE iam_signup_intents
SET state = 'materializing',
    verify_idempotency_key = sqlc.arg(verify_idempotency_key),
    verify_request_hash = sqlc.arg(verify_request_hash),
    verified_at = COALESCE(verified_at, sqlc.arg(verified_at)),
    materialization_lease_expires_at = sqlc.arg(materialization_lease_expires_at),
    materialization_attempts = materialization_attempts + 1,
    materialization_last_error = '',
    updated_at = now()
WHERE signup_intent_id = sqlc.arg(signup_intent_id);

-- name: MarkSignupIntentExpired :exec
UPDATE iam_signup_intents
SET state = 'expired',
    updated_at = now()
WHERE signup_intent_id = sqlc.arg(signup_intent_id)
  AND state = 'pending_verification';

-- name: MarkSignupIntentStep :exec
UPDATE iam_signup_intents
SET materialization_step = sqlc.arg(materialization_step),
    materialization_lease_expires_at = sqlc.arg(materialization_lease_expires_at),
    updated_at = now()
WHERE signup_intent_id = sqlc.arg(signup_intent_id);

-- name: RecordSignupIntentProviderOrg :exec
UPDATE iam_signup_intents
SET identity_provider_org_id = sqlc.arg(identity_provider_org_id),
    materialization_step = 'zitadel_organization',
    updated_at = now()
WHERE signup_intent_id = sqlc.arg(signup_intent_id);

-- name: RecordSignupIntentProviderUser :exec
UPDATE iam_signup_intents
SET identity_provider_user_id = sqlc.arg(identity_provider_user_id),
    materialization_step = 'zitadel_user',
    updated_at = now()
WHERE signup_intent_id = sqlc.arg(signup_intent_id);

-- name: RecordSignupIntentOrganization :exec
UPDATE iam_signup_intents
SET org_id = sqlc.arg(org_id),
    organization_slug = sqlc.arg(organization_slug),
    materialization_step = 'iam_organization',
    updated_at = now()
WHERE signup_intent_id = sqlc.arg(signup_intent_id);

-- name: MarkSignupIntentFailed :exec
UPDATE iam_signup_intents
SET state = sqlc.arg(state),
    materialization_last_error = sqlc.arg(materialization_last_error),
    materialization_lease_expires_at = NULL,
    updated_at = now()
WHERE signup_intent_id = sqlc.arg(signup_intent_id);

-- name: MarkSignupIntentCompleted :one
UPDATE iam_signup_intents
SET state = 'completed',
    materialization_step = 'completed',
    completed_at = COALESCE(completed_at, sqlc.arg(completed_at)),
    materialization_lease_expires_at = NULL,
    updated_at = now()
WHERE signup_intent_id = sqlc.arg(signup_intent_id)
RETURNING signup_intent_id, idempotency_key, request_hash, email, email_hash,
          organization_display_name, requested_organization_slug, organization_slug, given_name, family_name,
          verification_token_hash, state, materialization_step, materialization_attempts,
          materialization_last_error, materialization_lease_expires_at, verify_idempotency_key, verify_request_hash,
          org_id, identity_provider_org_id, identity_provider_user_id,
          created_at, updated_at, verification_expires_at, verified_at, completed_at;
