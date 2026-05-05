CREATE TABLE IF NOT EXISTS verself.deploy_unit_events
(
    `event_at`            DateTime64(9)                CODEC(Delta(8), ZSTD(3)),
    `deploy_run_key`      String                       CODEC(ZSTD(3)),
    `site`                LowCardinality(String)       CODEC(ZSTD(3)),
    `executor`            LowCardinality(String)       CODEC(ZSTD(3)),
    `unit_id`             LowCardinality(String)       CODEC(ZSTD(3)),
    `event_kind`          LowCardinality(String)       CODEC(ZSTD(3)),
    `desired_digest`      String         DEFAULT ''    CODEC(ZSTD(3)),
    `observed_digest`     String         DEFAULT ''    CODEC(ZSTD(3)),
    `no_op`               UInt8          DEFAULT 0     CODEC(T64, ZSTD(3)),
    `dependency_unit_ids` Array(String)  DEFAULT []    CODEC(ZSTD(3)),
    `payload_kind`        LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    `duration_ms`         UInt32         DEFAULT 0     CODEC(T64, ZSTD(3)),
    `error_message`       String         DEFAULT ''    CODEC(ZSTD(3)),
    `trace_id`            String         DEFAULT ''    CODEC(ZSTD(3)),
    `span_id`             String         DEFAULT ''    CODEC(ZSTD(3))
)
ENGINE = MergeTree
ORDER BY (site, executor, unit_id, event_kind, event_at, deploy_run_key)
SETTINGS index_granularity = 8192;
