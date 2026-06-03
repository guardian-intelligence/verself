-- Object storage service schema.
-- Database: object_storage (one database per service).

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE object_storage_buckets (
    bucket_id        UUID        PRIMARY KEY,
    org_id           TEXT        NOT NULL,
    bucket_name      TEXT        NOT NULL UNIQUE,
    provider         TEXT        NOT NULL CHECK (provider IN ('garage', 'cloudflare_r2')),
    provider_bucket_id TEXT      NOT NULL,
    quota_bytes      BIGINT      NULL CHECK (quota_bytes IS NULL OR quota_bytes >= 0),
    quota_objects    BIGINT      NULL CHECK (quota_objects IS NULL OR quota_objects >= 0),
    lifecycle_json   JSONB       NOT NULL DEFAULT '[]'::jsonb,
    created_at       TIMESTAMPTZ NOT NULL,
    created_by       TEXT        NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL,
    updated_by       TEXT        NOT NULL
);

CREATE UNIQUE INDEX object_storage_buckets_provider_bucket_idx
    ON object_storage_buckets (provider, provider_bucket_id);

CREATE INDEX object_storage_buckets_org_created_idx
    ON object_storage_buckets (org_id, created_at DESC);

CREATE TABLE object_storage_bucket_aliases (
    alias       TEXT        PRIMARY KEY,
    bucket_id   UUID        NOT NULL REFERENCES object_storage_buckets(bucket_id) ON DELETE CASCADE,
    prefix      TEXT        NOT NULL DEFAULT '',
    service_tag TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL,
    created_by  TEXT        NOT NULL
);

CREATE INDEX object_storage_bucket_aliases_bucket_idx
    ON object_storage_bucket_aliases (bucket_id, alias);

-- access_key_id is intentionally non-unique as a column constraint. We store
-- '' for spiffe_mtls rows, so uniqueness is enforced only for non-empty values
-- via object_storage_credentials_access_key_nonempty_idx below.
CREATE TABLE object_storage_credentials (
    credential_id      UUID        PRIMARY KEY,
    bucket_id          UUID        NOT NULL REFERENCES object_storage_buckets(bucket_id) ON DELETE CASCADE,
    auth_mode          TEXT        NOT NULL CHECK (auth_mode IN ('sigv4_static', 'spiffe_mtls')),
    display_name       TEXT        NOT NULL,
    access_key_id      TEXT        NOT NULL DEFAULT '',
    spiffe_subject     TEXT        NOT NULL DEFAULT '',
    secret_hash        TEXT        NOT NULL DEFAULT '',
    secret_fingerprint TEXT        NOT NULL DEFAULT '',
    secret_ciphertext  BYTEA       NOT NULL DEFAULT '\\x',
    secret_nonce       BYTEA       NOT NULL DEFAULT '\\x',
    status             TEXT        NOT NULL CHECK (status IN ('active', 'revoked')),
    expires_at         TIMESTAMPTZ NULL,
    created_at         TIMESTAMPTZ NOT NULL,
    created_by         TEXT        NOT NULL,
    revoked_at         TIMESTAMPTZ NULL,
    revoked_by         TEXT        NOT NULL DEFAULT '',
    CHECK ((auth_mode = 'sigv4_static' AND access_key_id <> '' AND secret_hash <> '' AND secret_fingerprint <> '' AND octet_length(secret_ciphertext) > 0 AND octet_length(secret_nonce) > 0 AND spiffe_subject = '')
        OR (auth_mode = 'spiffe_mtls' AND spiffe_subject <> '' AND access_key_id = '' AND secret_hash = '' AND secret_fingerprint = '' AND octet_length(secret_ciphertext) = 0 AND octet_length(secret_nonce) = 0))
);

CREATE INDEX object_storage_credentials_bucket_status_idx
    ON object_storage_credentials (bucket_id, status, auth_mode, created_at DESC);

CREATE UNIQUE INDEX object_storage_credentials_spiffe_subject_active_idx
    ON object_storage_credentials (spiffe_subject)
    WHERE auth_mode = 'spiffe_mtls' AND status = 'active';

CREATE UNIQUE INDEX object_storage_credentials_access_key_active_idx
    ON object_storage_credentials (access_key_id)
    WHERE auth_mode = 'sigv4_static' AND status = 'active';

CREATE UNIQUE INDEX object_storage_credentials_access_key_nonempty_idx
    ON object_storage_credentials (access_key_id)
    WHERE access_key_id <> '';

CREATE TABLE object_storage_write_sessions (
    session_id      TEXT        PRIMARY KEY,
    site            TEXT        NOT NULL,
    capability      TEXT        NOT NULL CHECK (capability IN ('deployment_artifacts', 'product_objects', 'recovery')),
    namespace       TEXT        NOT NULL,
    bucket_id       UUID        NOT NULL REFERENCES object_storage_buckets(bucket_id) ON DELETE RESTRICT,
    idempotency_key TEXT        NOT NULL,
    status          TEXT        NOT NULL CHECK (status IN ('open', 'completed')),
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL,
    created_by      TEXT        NOT NULL,
    completed_at    TIMESTAMPTZ NULL
);

CREATE UNIQUE INDEX object_storage_write_sessions_idempotency_idx
    ON object_storage_write_sessions (site, capability, idempotency_key);

CREATE INDEX object_storage_write_sessions_site_created_idx
    ON object_storage_write_sessions (site, created_at DESC);

CREATE TABLE object_storage_write_session_objects (
    session_id TEXT        NOT NULL REFERENCES object_storage_write_sessions(session_id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    object_key TEXT        NOT NULL,
    sha256     TEXT        NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    size_bytes BIGINT      NOT NULL CHECK (size_bytes > 0),
    action     TEXT        NOT NULL CHECK (action IN ('present', 'put')),
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (session_id, name)
);

CREATE INDEX object_storage_write_session_objects_key_idx
    ON object_storage_write_session_objects (object_key);
