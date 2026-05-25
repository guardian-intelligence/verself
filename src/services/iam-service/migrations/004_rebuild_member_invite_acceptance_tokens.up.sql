DROP TABLE IF EXISTS iam_member_invite_acceptance_tokens;

CREATE TABLE iam_member_invite_acceptance_tokens (
    token_hash TEXT PRIMARY KEY,
    org_id TEXT NOT NULL REFERENCES iam_organizations (org_id) ON DELETE CASCADE,
    user_id TEXT NOT NULL,
    email TEXT NOT NULL,
    email_verification_code TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    CHECK (token_hash ~ '^sha256:[0-9a-f]{64}$'),
    CHECK (length(btrim(user_id)) > 0),
    CHECK (length(btrim(email)) > 0),
    CHECK (length(btrim(email_verification_code)) > 0),
    CHECK (expires_at > created_at),
    CHECK (accepted_at IS NULL OR accepted_at >= created_at)
);

CREATE INDEX iam_member_invite_acceptance_tokens_user_idx
    ON iam_member_invite_acceptance_tokens (user_id, created_at DESC);

CREATE INDEX iam_member_invite_acceptance_tokens_expires_idx
    ON iam_member_invite_acceptance_tokens (expires_at)
    WHERE accepted_at IS NULL;

ALTER TABLE iam_member_invite_acceptance_tokens OWNER TO iam_service;
GRANT ALL PRIVILEGES ON TABLE iam_member_invite_acceptance_tokens TO iam_service;
