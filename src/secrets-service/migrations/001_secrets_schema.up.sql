CREATE TABLE IF NOT EXISTS secret_resources (
    secret_id UUID PRIMARY KEY,
    org_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    name TEXT NOT NULL,
    scope_level TEXT NOT NULL,
    source_id TEXT NOT NULL DEFAULT '',
    env_id TEXT NOT NULL DEFAULT '',
    branch_hash TEXT NOT NULL DEFAULT '',
    branch_display TEXT NOT NULL DEFAULT '',
    current_version BIGINT NOT NULL DEFAULT 0 CHECK (current_version >= 0),
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    CHECK (length(btrim(org_id)) > 0),
    CHECK (kind IN ('secret', 'variable')),
    CHECK (length(btrim(name)) > 0),
    CHECK (scope_level IN ('org', 'source', 'environment', 'branch')),
    CHECK ((scope_level = 'org' AND source_id = '' AND env_id = '' AND branch_hash = '')
        OR (scope_level = 'source' AND source_id <> '' AND env_id = '' AND branch_hash = '')
        OR (scope_level = 'environment' AND source_id <> '' AND env_id <> '' AND branch_hash = '')
        OR (scope_level = 'branch' AND source_id <> '' AND env_id <> '' AND branch_hash <> ''))
);

CREATE UNIQUE INDEX IF NOT EXISTS secret_resources_scope_name_idx
    ON secret_resources (org_id, kind, scope_level, source_id, env_id, branch_hash, name)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS secret_resources_org_updated_idx
    ON secret_resources (org_id, updated_at DESC, secret_id);

CREATE TABLE IF NOT EXISTS secret_versions (
    secret_id UUID NOT NULL REFERENCES secret_resources (secret_id) ON DELETE CASCADE,
    version BIGINT NOT NULL CHECK (version > 0),
    ciphertext BYTEA NOT NULL,
    nonce BYTEA NOT NULL,
    value_hash TEXT NOT NULL,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    destroyed_at TIMESTAMPTZ,
    PRIMARY KEY (secret_id, version),
    CHECK (length(value_hash) = 64)
);

CREATE TABLE IF NOT EXISTS transit_keys (
    key_id UUID PRIMARY KEY,
    org_id TEXT NOT NULL,
    name TEXT NOT NULL,
    current_version BIGINT NOT NULL DEFAULT 1 CHECK (current_version > 0),
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(btrim(name)) > 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS transit_keys_org_name_idx
    ON transit_keys (org_id, name)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS transit_key_versions (
    key_id UUID NOT NULL REFERENCES transit_keys (key_id) ON DELETE CASCADE,
    version BIGINT NOT NULL CHECK (version > 0),
    wrapped_key BYTEA NOT NULL,
    nonce BYTEA NOT NULL,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (key_id, version)
);
