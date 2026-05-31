CREATE DATABASE IF NOT EXISTS verself_deploy;

CREATE TABLE IF NOT EXISTS verself_deploy.canary_events_v1
(
    `site`              LowCardinality(String) CODEC(ZSTD(3)),
    `event_name`        LowCardinality(String) CODEC(ZSTD(3)),
    `status`            LowCardinality(String) CODEC(ZSTD(3)),
    `component`         LowCardinality(String) CODEC(ZSTD(3)),
    `job_id`            LowCardinality(String) CODEC(ZSTD(3)),
    `selection`         LowCardinality(String) CODEC(ZSTD(3)),
    `deploy_run_key`    String                 CODEC(ZSTD(3)),
    `canary_count`      UInt16                 CODEC(T64, ZSTD(3)),
    `duration_ms`       UInt32                 CODEC(T64, ZSTD(3)),
    `error_message`     String                 CODEC(ZSTD(3)),
    `trace_id`          String                 CODEC(ZSTD(3)),
    `span_id`           String                 CODEC(ZSTD(3)),
    `event_time`        DateTime64(9)          CODEC(Delta(8), ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toDate(event_time)
ORDER BY (site, event_name, status, component, toDateTime(event_time), deploy_run_key, job_id)
SETTINGS index_granularity = 8192;

CREATE MATERIALIZED VIEW IF NOT EXISTS verself_deploy.canary_events_from_otel_v1
TO verself_deploy.canary_events_v1
AS
SELECT
    event_attrs['verself.site'] AS site,
    event_name,
    multiIf(
        event_name = 'verself.canary.succeeded', 'succeeded',
        event_name = 'verself.canary.failed', 'failed',
        event_name = 'verself.canary.skipped', 'skipped',
        ''
    ) AS status,
    event_attrs['verself.component'] AS component,
    event_attrs['nomad.job_id'] AS job_id,
    event_attrs['verself.canary_selection'] AS selection,
    event_attrs['verself.deploy_run_key'] AS deploy_run_key,
    toUInt16OrZero(event_attrs['verself.canary_count']) AS canary_count,
    toUInt32OrZero(event_attrs['verself.duration_ms']) AS duration_ms,
    event_attrs['error.message'] AS error_message,
    TraceId AS trace_id,
    SpanId AS span_id,
    event_time
FROM default.otel_traces
ARRAY JOIN
    `Events.Name` AS event_name,
    `Events.Timestamp` AS event_time,
    `Events.Attributes` AS event_attrs
WHERE ServiceName = 'verself-deploy'
  AND event_name IN ('verself.canary.succeeded', 'verself.canary.failed', 'verself.canary.skipped');

CREATE TABLE IF NOT EXISTS verself_deploy.canary_executions_v1
(
    `site`              LowCardinality(String) CODEC(ZSTD(3)),
    `status`            LowCardinality(String) CODEC(ZSTD(3)),
    `kind`              LowCardinality(String) CODEC(ZSTD(3)),
    `size`              LowCardinality(String) CODEC(ZSTD(3)),
    `component`         LowCardinality(String) CODEC(ZSTD(3)),
    `job_id`            LowCardinality(String) CODEC(ZSTD(3)),
    `deploy_run_key`    String                 CODEC(ZSTD(3)),
    `label`             String                 CODEC(ZSTD(3)),
    `target`            String                 CODEC(ZSTD(3)),
    `duration_ms`       UInt32                 CODEC(T64, ZSTD(3)),
    `error_message`     String                 CODEC(ZSTD(3)),
    `trace_id`          String                 CODEC(ZSTD(3)),
    `span_id`           String                 CODEC(ZSTD(3)),
    `started_at`        DateTime64(9)          CODEC(Delta(8), ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toDate(started_at)
ORDER BY (site, status, kind, size, component, toDateTime(started_at), deploy_run_key, job_id)
SETTINGS index_granularity = 8192;

CREATE MATERIALIZED VIEW IF NOT EXISTS verself_deploy.canary_executions_from_otel_v1
TO verself_deploy.canary_executions_v1
AS
SELECT
    ResourceAttributes['verself.site'] AS site,
    if(StatusCode = 'Ok', 'succeeded', 'failed') AS status,
    SpanAttributes['verself.canary_kind'] AS kind,
    SpanAttributes['verself.canary_size'] AS size,
    SpanAttributes['verself.component'] AS component,
    SpanAttributes['nomad.job_id'] AS job_id,
    ResourceAttributes['verself.deploy_run_key'] AS deploy_run_key,
    SpanAttributes['verself.canary_label'] AS label,
    SpanAttributes['bazel.target'] AS target,
    toUInt32OrZero(SpanAttributes['verself.duration_ms']) AS duration_ms,
    StatusMessage AS error_message,
    TraceId AS trace_id,
    SpanId AS span_id,
    Timestamp AS started_at
FROM default.otel_traces
WHERE ServiceName = 'verself-deploy'
  AND SpanName = 'verself_deploy.canary.execute';
