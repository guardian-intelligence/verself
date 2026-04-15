-- Unified execution log chunks and wide execution events.

DROP TABLE IF EXISTS forge_metal.job_logs;
DROP TABLE IF EXISTS forge_metal.job_events;
DROP TABLE IF EXISTS forge_metal.sandbox_job_logs;
DROP TABLE IF EXISTS forge_metal.sandbox_job_events;

CREATE TABLE IF NOT EXISTS forge_metal.job_logs
(
    execution_id UUID,
    attempt_id   UUID,
    seq          UInt32,
    stream       LowCardinality(String),
    chunk        String               CODEC(ZSTD(3)),
    created_at   DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (attempt_id, seq)
TTL toDateTime(created_at) + INTERVAL 90 DAY
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS forge_metal.job_events
(
    execution_id      UUID,
    attempt_id        UUID,
    org_id            UInt64,
    actor_id          LowCardinality(String),
    kind              LowCardinality(String),
    source_kind       LowCardinality(String) DEFAULT '',
    workload_kind     LowCardinality(String) DEFAULT '',
    runner_class      LowCardinality(String) DEFAULT '',
    external_provider LowCardinality(String) DEFAULT '',
    external_task_id  String DEFAULT ''       CODEC(ZSTD(3)),
    provider          LowCardinality(String),
    product_id        LowCardinality(String),
    run_command       String                  CODEC(ZSTD(3)),
    status            LowCardinality(String),
    exit_code         Int32                   CODEC(ZSTD(3)),
    duration_ms       Int64                   CODEC(Delta(8), ZSTD(3)),
    zfs_written       UInt64                  CODEC(T64, ZSTD(3)),
    stdout_bytes      UInt64                  CODEC(T64, ZSTD(3)),
    stderr_bytes      UInt64                  CODEC(T64, ZSTD(3)),
    billing_job_id    Int64 DEFAULT 0         CODEC(ZSTD(3)),
    charge_units      UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    pricing_phase     LowCardinality(String) DEFAULT '',
    correlation_id    String DEFAULT ''       CODEC(ZSTD(3)),
    started_at        DateTime64(6, 'UTC')    CODEC(DoubleDelta, ZSTD(3)),
    completed_at      DateTime64(6, 'UTC')    CODEC(DoubleDelta, ZSTD(3)),
    created_at        DateTime64(6, 'UTC')    CODEC(DoubleDelta, ZSTD(3)),
    trace_id          String DEFAULT ''       CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (org_id, source_kind, workload_kind, runner_class, created_at, execution_id)
TTL toDateTime(created_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;
