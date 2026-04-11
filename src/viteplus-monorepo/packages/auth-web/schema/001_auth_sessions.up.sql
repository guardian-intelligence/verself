CREATE TABLE IF NOT EXISTS auth_sessions (
  session_id TEXT PRIMARY KEY,
  app_name TEXT NOT NULL,
  client_cache_partition TEXT NOT NULL,
  subject TEXT NOT NULL,
  email TEXT,
  display_name TEXT,
  preferred_username TEXT,
  org_id TEXT,
  roles JSONB NOT NULL DEFAULT '[]'::jsonb,
  user_claims JSONB NOT NULL DEFAULT '{}'::jsonb,
  id_token TEXT,
  access_token TEXT NOT NULL,
  refresh_token TEXT,
  token_scope TEXT,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS auth_sessions_app_subject_idx
  ON auth_sessions (app_name, subject);

ALTER TABLE auth_sessions
  ADD COLUMN IF NOT EXISTS client_cache_partition TEXT;

UPDATE auth_sessions
SET client_cache_partition = md5(session_id || ':' || app_name || ':client-cache-partition')
WHERE client_cache_partition IS NULL;

ALTER TABLE auth_sessions
  ALTER COLUMN client_cache_partition SET NOT NULL;

CREATE TABLE IF NOT EXISTS auth_resource_tokens (
  session_id TEXT NOT NULL REFERENCES auth_sessions (session_id) ON DELETE CASCADE,
  audience TEXT NOT NULL,
  access_token TEXT NOT NULL,
  token_scope TEXT,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (session_id, audience)
);

CREATE INDEX IF NOT EXISTS auth_resource_tokens_expiry_idx
  ON auth_resource_tokens (expires_at);
