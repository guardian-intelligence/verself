CREATE TABLE IF NOT EXISTS verself.ansible_task_events
(
    `event_at`        DateTime64(9)                CODEC(Delta(8), ZSTD(3)),
    `deploy_run_key`  String                       CODEC(ZSTD(3)),
    `site`            LowCardinality(String)       CODEC(ZSTD(3)),
    `layer`           LowCardinality(String)       CODEC(ZSTD(3)),
    `playbook`        LowCardinality(String)       CODEC(ZSTD(3)),
    `play`            String                       CODEC(ZSTD(3)),
    `task`            String                       CODEC(ZSTD(3)),
    `host`            LowCardinality(String)       CODEC(ZSTD(3)),
    -- Status set: ok | changed | skipped | failed | unreachable. The
    -- divergence canary's "changed task inside a skipped layer" query
    -- joins on (deploy_run_key, layer, status='changed').
    `status`          LowCardinality(String)       CODEC(ZSTD(3)),
    `item`            String         DEFAULT ''    CODEC(ZSTD(3)),
    `duration_ms`     UInt32         DEFAULT 0     CODEC(T64, ZSTD(3)),
    `message`         String         DEFAULT ''    CODEC(ZSTD(3))
)
ENGINE = MergeTree
ORDER BY (site, layer, event_at, deploy_run_key)
SETTINGS index_granularity = 8192;
