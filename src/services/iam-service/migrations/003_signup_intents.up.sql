CREATE TABLE IF NOT EXISTS iam_signup_intents (
    signup_intent_id                 TEXT        PRIMARY KEY CHECK (signup_intent_id ~ '^signup_[0-9A-HJKMNP-TV-Z]{26}$'),
    idempotency_key                  TEXT        NOT NULL CHECK (idempotency_key <> ''),
    request_hash                     BYTEA       NOT NULL CHECK (octet_length(request_hash) = 32),
    email                            TEXT        NOT NULL CHECK (email <> ''),
    email_hash                       BYTEA       NOT NULL CHECK (octet_length(email_hash) = 32),
    organization_display_name        TEXT        NOT NULL CHECK (organization_display_name <> ''),
    requested_organization_slug      TEXT        NOT NULL DEFAULT '',
    organization_slug                TEXT        NOT NULL DEFAULT '',
    given_name                       TEXT        NOT NULL DEFAULT '',
    family_name                      TEXT        NOT NULL DEFAULT '',
    verification_token_hash          BYTEA       NOT NULL CHECK (octet_length(verification_token_hash) = 32),
    state                            TEXT        NOT NULL CHECK (state IN ('pending_verification', 'materializing', 'completed', 'expired', 'failed_retryable', 'failed_terminal')),
    materialization_step             TEXT        NOT NULL DEFAULT '',
    materialization_attempts         INTEGER     NOT NULL DEFAULT 0 CHECK (materialization_attempts >= 0),
    materialization_last_error       TEXT        NOT NULL DEFAULT '',
    materialization_lease_expires_at TIMESTAMPTZ,
    verify_idempotency_key           TEXT        NOT NULL DEFAULT '',
    verify_request_hash              BYTEA,
    org_id                           TEXT        NOT NULL DEFAULT '',
    identity_provider_org_id         TEXT        NOT NULL DEFAULT '',
    identity_provider_user_id        TEXT        NOT NULL DEFAULT '',
    created_at                       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                       TIMESTAMPTZ NOT NULL DEFAULT now(),
    verification_expires_at          TIMESTAMPTZ NOT NULL,
    verified_at                      TIMESTAMPTZ,
    completed_at                     TIMESTAMPTZ,
    CHECK (verify_request_hash IS NULL OR octet_length(verify_request_hash) = 32),
    CHECK ((state = 'completed') = (completed_at IS NOT NULL)),
    CHECK (state <> 'completed' OR org_id <> ''),
    UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS iam_signup_intents_email_hash_idx
    ON iam_signup_intents (email_hash, created_at DESC);

CREATE INDEX IF NOT EXISTS iam_signup_intents_state_due_idx
    ON iam_signup_intents (state, verification_expires_at, materialization_lease_expires_at, created_at)
    WHERE state IN ('pending_verification', 'failed_retryable');

CREATE UNIQUE INDEX IF NOT EXISTS iam_signup_intents_org_id_idx
    ON iam_signup_intents (org_id)
    WHERE org_id <> '';

ALTER TABLE IF EXISTS iam_signup_intents OWNER TO iam_service;
GRANT ALL PRIVILEGES ON TABLE iam_signup_intents TO iam_service;
