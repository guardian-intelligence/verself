CREATE TABLE IF NOT EXISTS verself.substrate_convergence_events
(
    `event_at`           DateTime64(9)                 CODEC(Delta(8), ZSTD(3)),
    `deploy_run_key`     String                        CODEC(ZSTD(3)),
    `site`               LowCardinality(String)        CODEC(ZSTD(3)),
    `node`               LowCardinality(String)        CODEC(ZSTD(3)),
    `substrate_digest`   FixedString(64)               CODEC(ZSTD(3)),
    `mode`               LowCardinality(String)        CODEC(ZSTD(3)),
    `event_kind`         LowCardinality(String)        CODEC(ZSTD(3)),
    `changed_tasks`      UInt32         DEFAULT 0      CODEC(T64, ZSTD(3)),
    `error_message`      String         DEFAULT ''     CODEC(ZSTD(3))
)
ENGINE = MergeTree
ORDER BY (site, node, event_at, substrate_digest)
SETTINGS index_granularity = 8192;
