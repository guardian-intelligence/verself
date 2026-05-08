CREATE TABLE IF NOT EXISTS verself.checkpoint_operations
(
    operation_id             UUID,
    execution_id             UUID,
    attempt_id               UUID,
    mount_id                 UUID,
    volume_id                UUID,
    source_generation_id     String DEFAULT ''       CODEC(ZSTD(3)),
    committed_generation_id  String DEFAULT ''       CODEC(ZSTD(3)),
    org_id                   UInt64                  CODEC(T64, ZSTD(3)),
    provider                 LowCardinality(String) DEFAULT '',
    provider_installation_id UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    provider_repository_id   UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    provider_run_id          UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    provider_job_id          UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    repository_full_name     LowCardinality(String) DEFAULT '',
    runner_class             LowCardinality(String) DEFAULT '',
    scope_kind               LowCardinality(String) DEFAULT '',
    scope_ref                String DEFAULT ''       CODEC(ZSTD(3)),
    key_hash                 String DEFAULT ''       CODEC(ZSTD(3)),
    mount_path_hash          String DEFAULT ''       CODEC(ZSTD(3)),
    event_name               LowCardinality(String) DEFAULT '',
    operation                LowCardinality(String) DEFAULT '',
    result                   LowCardinality(String) DEFAULT '',
    mount_state              LowCardinality(String) DEFAULT '',
    save_state               LowCardinality(String) DEFAULT '',
    trust_class              LowCardinality(String) DEFAULT '',
    used_bytes               UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    written_bytes            UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    duration_ms              UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    error_class              LowCardinality(String) DEFAULT '',
    observed_at              DateTime64(6, 'UTC')   CODEC(DoubleDelta, ZSTD(3)),
    trace_id                 String DEFAULT ''       CODEC(ZSTD(3)),
    span_id                  String DEFAULT ''       CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (org_id, operation, result, repository_full_name, observed_at, execution_id)
TTL toDateTime(observed_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;
