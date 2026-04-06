-- Sandbox job log chunks for observability dashboards and search.
-- Companion to sandbox_rental.job_logs in PostgreSQL (which feeds ElectricSQL live sync).

CREATE TABLE IF NOT EXISTS forge_metal.sandbox_job_logs
(
    job_id     UUID,
    seq        UInt32,
    stream     LowCardinality(String),
    chunk      String                 CODEC(ZSTD(3)),
    created_at DateTime64(6, 'UTC')   CODEC(DoubleDelta, ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (job_id, seq)
TTL toDateTime(created_at) + INTERVAL 90 DAY
SETTINGS index_granularity = 8192;

-- Sandbox job telemetry (wide event per job, denormalized).

CREATE TABLE IF NOT EXISTS forge_metal.sandbox_job_events
(
    job_id       UUID,
    org_id       UInt64,
    user_id      LowCardinality(String),
    repo_url     String                 CODEC(ZSTD(3)),
    run_command  String                 CODEC(ZSTD(3)),
    status       LowCardinality(String),
    exit_code    Int32                  CODEC(ZSTD(3)),
    duration_ms  Int64                  CODEC(Delta(8), ZSTD(3)),
    zfs_written  UInt64                 CODEC(T64, ZSTD(3)),
    stdout_bytes UInt64                 CODEC(T64, ZSTD(3)),
    stderr_bytes UInt64                 CODEC(T64, ZSTD(3)),
    started_at   DateTime64(6, 'UTC')   CODEC(DoubleDelta, ZSTD(3)),
    completed_at DateTime64(6, 'UTC')   CODEC(DoubleDelta, ZSTD(3)),
    created_at   DateTime64(6, 'UTC')   CODEC(DoubleDelta, ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (org_id, created_at, job_id)
TTL toDateTime(created_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;
