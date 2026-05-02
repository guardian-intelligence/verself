CREATE TABLE IF NOT EXISTS verself.operator_command_runs
(
    `event_at`      DateTime64(3)           CODEC(Delta(8), ZSTD(3)),
    `run_id`        String                  CODEC(ZSTD(3)),
    `site`          LowCardinality(String)  CODEC(ZSTD(3)),
    `command`       LowCardinality(String)  CODEC(ZSTD(3)),
    `actor_device`  LowCardinality(String)  CODEC(ZSTD(3)),
    `target_host`   String                  CODEC(ZSTD(3)),
    `target_user`   LowCardinality(String)  CODEC(ZSTD(3)),
    `status`        LowCardinality(String)  CODEC(ZSTD(3)),
    `duration_ms`   UInt32                  CODEC(T64, ZSTD(3)),
    `error_kind`    LowCardinality(String)  DEFAULT '' CODEC(ZSTD(3)),
    `error_message` String                  DEFAULT '' CODEC(ZSTD(3)),
    `trace_id`      String                  DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = MergeTree
ORDER BY (site, command, event_at, run_id)
SETTINGS index_granularity = 8192;
