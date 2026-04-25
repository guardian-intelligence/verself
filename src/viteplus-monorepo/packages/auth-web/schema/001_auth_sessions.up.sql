CREATE TABLE IF NOT EXISTS auth_sessions (
  session_id TEXT PRIMARY KEY,
  app_name TEXT NOT NULL,
  client_cache_partition TEXT NOT NULL,
  subject TEXT NOT NULL,
  email TEXT,
  display_name TEXT,
  preferred_username TEXT,
  org_id TEXT,
  home_org_id TEXT,
  selected_org_id TEXT,
  roles JSONB NOT NULL DEFAULT '[]'::jsonb,
  available_org_contexts JSONB NOT NULL DEFAULT '[]'::jsonb,
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

ALTER TABLE auth_sessions
  ADD COLUMN IF NOT EXISTS home_org_id TEXT,
  ADD COLUMN IF NOT EXISTS selected_org_id TEXT,
  ADD COLUMN IF NOT EXISTS available_org_contexts JSONB NOT NULL DEFAULT '[]'::jsonb;

UPDATE auth_sessions
SET client_cache_partition = md5(session_id || ':' || app_name || ':client-cache-partition')
WHERE client_cache_partition IS NULL;

UPDATE auth_sessions
SET home_org_id = COALESCE(home_org_id, org_id),
    selected_org_id = COALESCE(selected_org_id, org_id)
WHERE home_org_id IS NULL
   OR selected_org_id IS NULL;

ALTER TABLE auth_sessions
  ALTER COLUMN client_cache_partition SET NOT NULL;

CREATE TABLE IF NOT EXISTS auth_resource_tokens (
  session_id TEXT NOT NULL REFERENCES auth_sessions (session_id) ON DELETE CASCADE,
  audience TEXT NOT NULL,
  selected_org_id TEXT NOT NULL,
  scope_hash TEXT NOT NULL,
  access_token TEXT NOT NULL,
  token_scope TEXT,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (session_id, audience, selected_org_id, scope_hash)
);

ALTER TABLE auth_resource_tokens
  ADD COLUMN IF NOT EXISTS selected_org_id TEXT,
  ADD COLUMN IF NOT EXISTS scope_hash TEXT;

DELETE FROM auth_resource_tokens
WHERE selected_org_id IS NULL
   OR scope_hash IS NULL;

ALTER TABLE auth_resource_tokens
  ALTER COLUMN selected_org_id SET NOT NULL,
  ALTER COLUMN scope_hash SET NOT NULL,
  DROP CONSTRAINT IF EXISTS auth_resource_tokens_pkey,
  ADD PRIMARY KEY (session_id, audience, selected_org_id, scope_hash);

CREATE INDEX IF NOT EXISTS auth_resource_tokens_expiry_idx
  ON auth_resource_tokens (expires_at);
