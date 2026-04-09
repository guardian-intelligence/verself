CREATE TABLE IF NOT EXISTS auth_sessions (
  session_id TEXT PRIMARY KEY,
  app_name TEXT NOT NULL,
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
