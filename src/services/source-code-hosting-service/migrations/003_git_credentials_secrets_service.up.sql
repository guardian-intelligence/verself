DROP TABLE source_git_credentials;

CREATE TABLE source_git_credentials (
    credential_id UUID        PRIMARY KEY,
    org_id        BIGINT      NOT NULL CHECK (org_id > 0),
    actor_id      TEXT        NOT NULL CHECK (actor_id <> ''),
    label         TEXT        NOT NULL CHECK (label <> ''),
    username      TEXT        NOT NULL CHECK (username <> ''),
    token_prefix  TEXT        NOT NULL CHECK (token_prefix <> ''),
    scopes        TEXT[]      NOT NULL DEFAULT ARRAY['repo:read','repo:write']::TEXT[],
    state         TEXT        NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'revoked')),
    expires_at    TIMESTAMPTZ NOT NULL,
    last_used_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at    TIMESTAMPTZ
);

CREATE INDEX idx_source_git_credentials_org_created
    ON source_git_credentials (org_id, created_at DESC, credential_id DESC);
