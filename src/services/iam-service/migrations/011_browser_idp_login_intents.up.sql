-- Pending state for an external identity provider ("Sign in with GitHub") login
-- intent. The browser is redirected to the provider before any OIDC auth request
-- exists, so this row only correlates the return trip: it binds the idp-login
-- state cookie to the originating browser client and carries the post-login
-- redirect target. The OIDC auth request and the full login transaction are
-- created on the idp callback (mirroring the password-login self-initiated
-- branch). Rows are single-use and reaped on expiry.
CREATE TABLE iam_browser_idp_login_intents (
    state_hash TEXT PRIMARY KEY,
    client_hash TEXT NOT NULL,
    redirect_to TEXT NOT NULL,
    purpose TEXT NOT NULL DEFAULT 'login',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (length(btrim(state_hash)) > 0),
    CHECK (length(btrim(client_hash)) > 0),
    CHECK (length(btrim(redirect_to)) > 0),
    CHECK (length(btrim(purpose)) > 0),
    CHECK (expires_at > created_at)
);

CREATE INDEX iam_browser_idp_login_intents_expires_at_idx
    ON iam_browser_idp_login_intents (expires_at);

ALTER TABLE iam_browser_idp_login_intents OWNER TO iam_service;

GRANT ALL PRIVILEGES ON TABLE iam_browser_idp_login_intents TO iam_service;
