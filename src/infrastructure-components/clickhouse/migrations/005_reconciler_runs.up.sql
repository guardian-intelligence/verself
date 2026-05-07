CREATE TABLE IF NOT EXISTS verself.reconciler_runs
(
    `event_at`           DateTime64(9)                 CODEC(Delta(8), ZSTD(3)),
    `deploy_run_key`     String                        CODEC(ZSTD(3)),
    `site`               LowCardinality(String)        CODEC(ZSTD(3)),
    `reconciler`         LowCardinality(String)        CODEC(ZSTD(3)),
    `event_kind`         LowCardinality(String)        CODEC(ZSTD(3)),
    `targets_seen`       UInt32          DEFAULT 0     CODEC(T64, ZSTD(3)),
    `targets_diffed`     UInt32          DEFAULT 0     CODEC(T64, ZSTD(3)),
    `targets_applied`    UInt32          DEFAULT 0     CODEC(T64, ZSTD(3)),
    `duration_ms`        UInt32          DEFAULT 0     CODEC(T64, ZSTD(3)),
    `error_message`      String          DEFAULT ''    CODEC(ZSTD(3))
)
ENGINE = MergeTree
ORDER BY (site, reconciler, event_at, deploy_run_key)
SETTINGS index_granularity = 8192;
