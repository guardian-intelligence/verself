-- Sandbox rental service schema.
-- Database: sandbox_rental (one database per service).

CREATE TABLE jobs (
    id                     UUID        PRIMARY KEY,
    org_id                 BIGINT      NOT NULL,
    user_id                TEXT        NOT NULL,
    repo_url               TEXT        NOT NULL,
    run_command             TEXT,
    status                 TEXT        NOT NULL DEFAULT 'pending',
    exit_code              INTEGER,
    duration_ms            BIGINT,
    zfs_written            BIGINT,
    billing_reservation_id TEXT,
    started_at             TIMESTAMPTZ,
    completed_at           TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_jobs_org_id ON jobs (org_id);
CREATE INDEX idx_jobs_status ON jobs (status);

CREATE TABLE job_logs (
    id         BIGSERIAL   PRIMARY KEY,
    job_id     UUID        NOT NULL REFERENCES jobs(id),
    seq        INTEGER     NOT NULL,
    stream     TEXT        NOT NULL,
    chunk      BYTEA       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_job_logs_job_id_seq ON job_logs (job_id, seq);
