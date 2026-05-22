CREATE TABLE IF NOT EXISTS runner_job_cache_manifests (
    provider          TEXT        NOT NULL CHECK (provider <> ''),
    provider_job_id   BIGINT      NOT NULL,
    source_kind       TEXT        NOT NULL CHECK (source_kind <> ''),
    source_path       TEXT        NOT NULL CHECK (source_path <> ''),
    source_sha        TEXT        NOT NULL CHECK (source_sha <> ''),
    content_sha256    TEXT        NOT NULL CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
    content_bytes     BYTEA       NOT NULL CHECK (octet_length(content_bytes) <= 65536),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, provider_job_id),
    FOREIGN KEY (provider, provider_job_id) REFERENCES runner_jobs(provider, provider_job_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_runner_job_cache_manifests_source
    ON runner_job_cache_manifests (provider, source_sha, updated_at DESC);
