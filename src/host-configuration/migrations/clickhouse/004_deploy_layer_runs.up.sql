CREATE TABLE IF NOT EXISTS verself.deploy_layer_runs
(
    `event_at`           DateTime64(9)                 CODEC(Delta(8), ZSTD(3)),
    `deploy_run_key`     String                        CODEC(ZSTD(3)),
    `site`               LowCardinality(String)        CODEC(ZSTD(3)),
    `layer`              LowCardinality(String)        CODEC(ZSTD(3)),
    `input_hash`         FixedString(64)               CODEC(ZSTD(3)),
    `last_applied_hash`  FixedString(64) DEFAULT ''    CODEC(ZSTD(3)),
    `event_kind`         LowCardinality(String)        CODEC(ZSTD(3)),
    `skipped`            UInt8           DEFAULT 0     CODEC(T64, ZSTD(3)),
    `skip_reason`        LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    `duration_ms`        UInt32          DEFAULT 0     CODEC(T64, ZSTD(3)),
    `changed_count`      UInt32          DEFAULT 0     CODEC(T64, ZSTD(3)),
    `error_message`      String          DEFAULT ''    CODEC(ZSTD(3))
)
ENGINE = MergeTree
ORDER BY (site, layer, event_at, deploy_run_key)
SETTINGS index_granularity = 8192;
