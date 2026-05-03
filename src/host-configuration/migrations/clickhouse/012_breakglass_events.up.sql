CREATE TABLE IF NOT EXISTS verself.breakglass_events
(
    `event_at`           DateTime64(9)                 CODEC(Delta(8), ZSTD(3)),
    `deploy_run_key`     String                        CODEC(ZSTD(3)),
    `site`               LowCardinality(String)        CODEC(ZSTD(3)),
    `sha`                FixedString(40)               CODEC(ZSTD(3)),
    `actor`              LowCardinality(String)        CODEC(ZSTD(3)),
    `exception_id`       String                        CODEC(ZSTD(3)),
    `expires_at`         DateTime64(9)                 CODEC(Delta(8), ZSTD(3)),
    `reason`             String                        CODEC(ZSTD(3)),
    `allowed_results`    Array(LowCardinality(String)) CODEC(ZSTD(3)),
    `policy_rejected`    UInt32                        CODEC(T64, ZSTD(3)),
    `policy_provisional` UInt32                        CODEC(T64, ZSTD(3)),
    `trace_id`           String                        CODEC(ZSTD(3)),
    `span_id`            String                        CODEC(ZSTD(3)),
    `evidence`           String                        CODEC(ZSTD(3))
)
ENGINE = MergeTree
ORDER BY (site, event_at, deploy_run_key, exception_id)
SETTINGS index_granularity = 8192;
