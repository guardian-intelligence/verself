CREATE TABLE IF NOT EXISTS governance_api_activity_chain_state (
    org_id TEXT PRIMARY KEY,
    sequence BIGINT NOT NULL DEFAULT 0 CHECK (sequence >= 0),
    row_hmac TEXT NOT NULL DEFAULT repeat('0', 64),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(row_hmac) = 64)
);

CREATE TABLE IF NOT EXISTS governance_api_activities (
    org_id TEXT NOT NULL,
    sequence BIGINT NOT NULL CHECK (sequence >= 0),
    metadata_uid UUID NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    event_date DATE NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    ocsf_version TEXT NOT NULL,
    ocsf_json TEXT NOT NULL,
    ocsf_sha256 TEXT NOT NULL,
    row_json TEXT NOT NULL,
    resources_json TEXT NOT NULL,
    prev_hmac TEXT NOT NULL,
    row_hmac TEXT NOT NULL,
    hmac_key_id TEXT NOT NULL,
    projected_at TIMESTAMPTZ,
    PRIMARY KEY (org_id, sequence),
    UNIQUE (metadata_uid),
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(btrim(ocsf_version)) > 0),
    CHECK (length(btrim(ocsf_json)) > 0),
    CHECK (length(ocsf_sha256) = 64),
    CHECK (length(btrim(row_json)) > 0),
    CHECK (length(btrim(resources_json)) > 0),
    CHECK (length(prev_hmac) = 64),
    CHECK (length(row_hmac) = 64),
    CHECK (length(btrim(hmac_key_id)) > 0)
);

CREATE INDEX IF NOT EXISTS governance_api_activities_pending_idx
    ON governance_api_activities (projected_at, observed_at, sequence, metadata_uid)
    WHERE projected_at IS NULL;
