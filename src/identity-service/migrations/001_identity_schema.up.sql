CREATE TABLE IF NOT EXISTS identity_policy_documents (
    org_id TEXT PRIMARY KEY,
    document JSONB NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT NOT NULL
);
