DROP TABLE IF EXISTS verself.durable_events;

CREATE TABLE verself.durable_events
(
    observed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    org_id LowCardinality(String) CODEC(ZSTD(3)),
    repository_id UInt64 CODEC(T64, ZSTD(3)),
    provider LowCardinality(String),
    provider_repository_id UInt64 CODEC(T64, ZSTD(3)),
    provider_run_id UInt64 CODEC(T64, ZSTD(3)),
    provider_run_attempt UInt64 CODEC(T64, ZSTD(3)),
    provider_job_id UInt64 CODEC(T64, ZSTD(3)),
    execution_id UUID,
    attempt_id UUID,
    operation_id UUID,
    durable_scope_id UUID,
    durable_generation_id UUID,
    cache_source LowCardinality(String),
    cache_name LowCardinality(String),
    event_name LowCardinality(String),
    result LowCardinality(String),
    reason String DEFAULT '' CODEC(ZSTD(3)),
    mount_name LowCardinality(String),
    source_generation_id UUID,
    candidate_generation_id UUID,
    current_generation_id UUID,
    zfs_snapshot_ref String DEFAULT '' CODEC(ZSTD(3)),
    used_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    written_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    pool_size_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    pool_allocated_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    pool_free_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    org_quota_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    org_used_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    org_available_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    trace_id String DEFAULT '' CODEC(ZSTD(3)),
    span_id String DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (event_name, provider, org_id, repository_id, cache_source, observed_at, operation_id);

GRANT SELECT, INSERT ON verself.durable_events TO sandbox_rental;
