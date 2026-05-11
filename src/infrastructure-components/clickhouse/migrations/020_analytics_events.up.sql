CREATE TABLE IF NOT EXISTS verself.analytics_events
(
    `event_date`        Date                                      CODEC(Delta(2), ZSTD(3)),
    `observed_at`       DateTime64(6, 'UTC')                      CODEC(DoubleDelta, ZSTD(3)),
    `event_id`          String                                    CODEC(ZSTD(3)),
    `dataset`           LowCardinality(String)                    CODEC(ZSTD(3)),
    `event_name`        LowCardinality(String)                    CODEC(ZSTD(3)),
    `source_kind`       LowCardinality(String)                    CODEC(ZSTD(3)),
    `source_subject`    String                                    CODEC(ZSTD(3)),
    `tenant_id`         LowCardinality(String)                    CODEC(ZSTD(3)),
    `repository`        LowCardinality(String)                    CODEC(ZSTD(3)),
    `repository_owner`  LowCardinality(String)                    CODEC(ZSTD(3)),
    `git_ref`           String                                    CODEC(ZSTD(3)),
    `git_sha`           String                                    CODEC(ZSTD(3)),
    `provider_run_id`   String                                    CODEC(ZSTD(3)),
    `provider_run_attempt` UInt32                                  CODEC(T64, ZSTD(3)),
    `provider_workflow` LowCardinality(String)                    CODEC(ZSTD(3)),
    `provider_job`      LowCardinality(String)                    CODEC(ZSTD(3)),
    `service_name`      LowCardinality(String)                    CODEC(ZSTD(3)),
    `service_version`   LowCardinality(String)                    CODEC(ZSTD(3)),
    `trace_id`          String                                    CODEC(ZSTD(3)),
    `span_id`           String                                    CODEC(ZSTD(3)),
    `build_tool`        LowCardinality(String)                    CODEC(ZSTD(3)),
    `build_package`     LowCardinality(String)                    CODEC(ZSTD(3)),
    `build_command`     LowCardinality(String)                    CODEC(ZSTD(3)),
    `build_target`      String                                    CODEC(ZSTD(3)),
    `config_path`       String                                    CODEC(ZSTD(3)),
    `cache_source`      LowCardinality(String)                    CODEC(ZSTD(3)),
    `cache_result`      LowCardinality(String)                    CODEC(ZSTD(3)),
    `cache_reason`      String                                    CODEC(ZSTD(3)),
    `status`            LowCardinality(String)                    CODEC(ZSTD(3)),
    `duration_ms`       UInt64                                    CODEC(T64, ZSTD(3)),
    `string_attributes` Map(LowCardinality(String), String)       CODEC(ZSTD(3)),
    `int_attributes`    Map(LowCardinality(String), Int64)        CODEC(ZSTD(3)),
    `float_attributes`  Map(LowCardinality(String), Float64)      CODEC(ZSTD(3)),
    `bool_attributes`   Map(LowCardinality(String), UInt8)        CODEC(ZSTD(3)),
    INDEX idx_string_attr_key mapKeys(string_attributes) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_string_attr_value mapValues(string_attributes) TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (dataset, event_name, build_tool, repository, build_package, observed_at, event_id)
TTL toDateTime(observed_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;
