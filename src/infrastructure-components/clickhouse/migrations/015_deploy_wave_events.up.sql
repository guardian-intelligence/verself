CREATE TABLE IF NOT EXISTS verself.deploy_wave_events
(
    event_at DateTime64(9, 'UTC') CODEC(Delta(8), ZSTD(3)),
    deploy_run_key String CODEC(ZSTD(3)),
    site LowCardinality(String),
    wave LowCardinality(String),
    event_kind LowCardinality(String),
    job_count UInt16 DEFAULT 0,
    changed_job_count UInt16 DEFAULT 0,
    artifact_count UInt16 DEFAULT 0,
    duration_ms UInt32 DEFAULT 0,
    error_message String DEFAULT '' CODEC(ZSTD(3)),
    trace_id String DEFAULT '',
    span_id String DEFAULT ''
)
ENGINE = MergeTree
ORDER BY (site, wave, event_kind, event_at, deploy_run_key)
TTL toDateTime(event_at) + INTERVAL 90 DAY;
