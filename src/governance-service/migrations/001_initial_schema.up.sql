CREATE TABLE IF NOT EXISTS governance_audit_chain_state (
    org_id TEXT PRIMARY KEY,
    sequence BIGINT NOT NULL DEFAULT 0 CHECK (sequence >= 0),
    row_hmac TEXT NOT NULL DEFAULT repeat('0', 64),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(row_hmac) = 64)
);

CREATE TABLE IF NOT EXISTS governance_audit_events (
    org_id TEXT NOT NULL,
    sequence BIGINT NOT NULL CHECK (sequence >= 0),
    event_id UUID NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL,
    event_date DATE NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    schema_version TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    row_json TEXT NOT NULL,
    prev_hmac TEXT NOT NULL,
    row_hmac TEXT NOT NULL,
    hmac_key_id TEXT NOT NULL,
    projected_at TIMESTAMPTZ,
    PRIMARY KEY (org_id, sequence),
    UNIQUE (event_id),
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(btrim(schema_version)) > 0),
    CHECK (length(btrim(payload_json)) > 0),
    CHECK (length(btrim(row_json)) > 0),
    CHECK (length(prev_hmac) = 64),
    CHECK (length(row_hmac) = 64),
    CHECK (length(btrim(hmac_key_id)) > 0)
);

CREATE INDEX IF NOT EXISTS governance_audit_events_pending_idx
    ON governance_audit_events (projected_at, recorded_at, sequence, event_id)
    WHERE projected_at IS NULL;

CREATE TABLE IF NOT EXISTS governance_export_jobs (
    export_id UUID PRIMARY KEY,
    org_id TEXT NOT NULL,
    requested_by TEXT NOT NULL,
    idempotency_key_hash TEXT NOT NULL,
    scopes TEXT[] NOT NULL,
    include_logs BOOLEAN NOT NULL DEFAULT false,
    format TEXT NOT NULL DEFAULT 'tar.gz',
    state TEXT NOT NULL,
    artifact_path TEXT NOT NULL DEFAULT '',
    artifact_sha256 TEXT NOT NULL DEFAULT '',
    artifact_bytes BIGINT NOT NULL DEFAULT 0 CHECK (artifact_bytes >= 0),
    manifest JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    downloaded_at TIMESTAMPTZ,
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(btrim(requested_by)) > 0),
    CHECK (length(idempotency_key_hash) = 64),
    CHECK (state IN ('queued', 'running', 'completed', 'failed')),
    CHECK (format = 'tar.gz'),
    CHECK (completed_at IS NULL OR completed_at >= created_at)
);

CREATE UNIQUE INDEX IF NOT EXISTS governance_export_jobs_org_idempotency_idx
    ON governance_export_jobs (org_id, idempotency_key_hash);

CREATE INDEX IF NOT EXISTS governance_export_jobs_org_created_idx
    ON governance_export_jobs (org_id, created_at DESC, export_id);

CREATE TABLE IF NOT EXISTS governance_export_files (
    export_id UUID NOT NULL REFERENCES governance_export_jobs (export_id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    content_type TEXT NOT NULL,
    row_count BIGINT NOT NULL DEFAULT 0 CHECK (row_count >= 0),
    bytes BIGINT NOT NULL DEFAULT 0 CHECK (bytes >= 0),
    sha256 TEXT NOT NULL,
    PRIMARY KEY (export_id, path),
    CHECK (length(btrim(path)) > 0),
    CHECK (length(btrim(content_type)) > 0),
    CHECK (length(sha256) = 64)
);
