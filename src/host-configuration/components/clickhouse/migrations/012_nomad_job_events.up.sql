CREATE TABLE IF NOT EXISTS verself.nomad_job_events
(
    `event_at`               DateTime64(9)                CODEC(Delta(8), ZSTD(3)),
    `deploy_run_key`         String                       CODEC(ZSTD(3)),
    `site`                   LowCardinality(String)       CODEC(ZSTD(3)),
    `job_id`                 LowCardinality(String)       CODEC(ZSTD(3)),
    `event_kind`             LowCardinality(String)       CODEC(ZSTD(3)),
    `spec_sha256`            String         DEFAULT ''    CODEC(ZSTD(3)),
    `artifact_sha256`        String         DEFAULT ''    CODEC(ZSTD(3)),
    `prior_job_modify_index` UInt64         DEFAULT 0     CODEC(T64, ZSTD(3)),
    `prior_version`          UInt64         DEFAULT 0     CODEC(T64, ZSTD(3)),
    `prior_stopped`          UInt8          DEFAULT 0     CODEC(T64, ZSTD(3)),
    `no_op`                  UInt8          DEFAULT 0     CODEC(T64, ZSTD(3)),
    `eval_id`                String         DEFAULT ''    CODEC(ZSTD(3)),
    `deployment_id`          String         DEFAULT ''    CODEC(ZSTD(3)),
    `job_modify_index`       UInt64         DEFAULT 0     CODEC(T64, ZSTD(3)),
    `desired_total`          UInt16         DEFAULT 0     CODEC(T64, ZSTD(3)),
    `healthy_total`          UInt16         DEFAULT 0     CODEC(T64, ZSTD(3)),
    `unhealthy_total`        UInt16         DEFAULT 0     CODEC(T64, ZSTD(3)),
    `placed_total`           UInt16         DEFAULT 0     CODEC(T64, ZSTD(3)),
    `terminal_status`        LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    `duration_ms`            UInt32         DEFAULT 0     CODEC(T64, ZSTD(3)),
    `error_message`          String         DEFAULT ''    CODEC(ZSTD(3)),
    `trace_id`               String         DEFAULT ''    CODEC(ZSTD(3)),
    `span_id`                String         DEFAULT ''    CODEC(ZSTD(3))
)
ENGINE = MergeTree
ORDER BY (site, job_id, event_kind, event_at, deploy_run_key)
SETTINGS index_granularity = 8192;
