ALTER TABLE iam_browser_sessions
    ADD COLUMN IF NOT EXISTS session_handle TEXT,
    ADD COLUMN IF NOT EXISTS created_client_ip TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS created_client_ip_trusted BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS created_user_agent TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS last_seen_client_ip TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS last_seen_client_ip_trusted BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS last_seen_user_agent TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now();

UPDATE iam_browser_sessions
SET session_handle = 'bs_' || md5(session_hash)
WHERE session_handle IS NULL OR btrim(session_handle) = '';

ALTER TABLE iam_browser_sessions
    ALTER COLUMN session_handle SET NOT NULL;

DROP INDEX IF EXISTS iam_browser_sessions_subject_idx;

CREATE UNIQUE INDEX IF NOT EXISTS iam_browser_sessions_session_handle_idx
    ON iam_browser_sessions (session_handle);

CREATE INDEX IF NOT EXISTS iam_browser_sessions_subject_idx
    ON iam_browser_sessions (subject, last_seen_at DESC);
