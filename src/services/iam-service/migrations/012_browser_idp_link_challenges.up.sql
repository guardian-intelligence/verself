-- Pending step-up challenge for linking an external identity provider ("Sign in
-- with GitHub") identity to an existing Verself account. The headless callback
-- resolves a verified-email match to an existing Zitadel human but never links on
-- the email alone: it captures the external identity here and redirects the
-- browser to a password proof. A successful password authentication carrying the
-- challenge cookie performs the link, so the password is the linking authority and
-- the email match only selects the candidate account. Rows are single-use, bound
-- to the originating browser client, and reaped on expiry.
CREATE TABLE iam_browser_idp_link_challenges (
    challenge_hash TEXT PRIMARY KEY,
    client_hash TEXT NOT NULL,
    zitadel_user_id TEXT NOT NULL,
    idp_id TEXT NOT NULL,
    external_user_id TEXT NOT NULL,
    external_user_name TEXT NOT NULL,
    email TEXT NOT NULL,
    redirect_to TEXT NOT NULL DEFAULT '',
    purpose TEXT NOT NULL DEFAULT 'login',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (length(btrim(challenge_hash)) > 0),
    CHECK (length(btrim(client_hash)) > 0),
    CHECK (length(btrim(zitadel_user_id)) > 0),
    CHECK (length(btrim(idp_id)) > 0),
    CHECK (length(btrim(external_user_id)) > 0),
    CHECK (length(btrim(email)) > 0),
    CHECK (length(btrim(purpose)) > 0),
    CHECK (expires_at > created_at)
);

CREATE INDEX iam_browser_idp_link_challenges_expires_at_idx
    ON iam_browser_idp_link_challenges (expires_at);

ALTER TABLE iam_browser_idp_link_challenges OWNER TO iam_service;

GRANT ALL PRIVILEGES ON TABLE iam_browser_idp_link_challenges TO iam_service;
