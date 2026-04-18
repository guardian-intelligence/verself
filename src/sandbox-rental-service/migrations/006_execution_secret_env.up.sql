CREATE TABLE IF NOT EXISTS execution_secret_env (
    execution_id UUID NOT NULL REFERENCES executions(execution_id) ON DELETE CASCADE,
    env_name TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'secret',
    secret_name TEXT NOT NULL,
    scope_level TEXT NOT NULL DEFAULT 'org',
    source_id TEXT NOT NULL DEFAULT '',
    env_id TEXT NOT NULL DEFAULT '',
    branch TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (execution_id, env_name),
    CHECK (length(btrim(env_name)) > 0),
    CHECK (length(btrim(secret_name)) > 0),
    CHECK (kind IN ('secret', 'variable')),
    CHECK (scope_level IN ('org', 'source', 'environment', 'branch')),
    CHECK ((scope_level = 'org' AND source_id = '' AND env_id = '' AND branch = '')
        OR (scope_level = 'source' AND source_id <> '' AND env_id = '' AND branch = '')
        OR (scope_level = 'environment' AND source_id <> '' AND env_id <> '' AND branch = '')
        OR (scope_level = 'branch' AND source_id <> '' AND env_id <> '' AND branch <> ''))
);

CREATE INDEX IF NOT EXISTS idx_execution_secret_env_execution_order
    ON execution_secret_env (execution_id, sort_order, env_name);
