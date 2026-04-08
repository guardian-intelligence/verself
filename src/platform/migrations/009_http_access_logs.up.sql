-- HTTP access logs: denormalized from otel_logs via Materialized View.
-- Filters on mapContains(LogAttributes, 'http_method') which is emitted by
-- Caddy (filelog), rent-a-sandbox (OTLP), and Zitadel (journald).
--
-- Attribute name normalization per service:
--   status:    caddy=http_status, rent-a-sandbox=http_status_code, zitadel=status
--   path:      caddy=http_uri,    rent-a-sandbox=http_target,      zitadel=path
--   host:      caddy=http_host,   rent-a-sandbox=(none),           zitadel=instance_host
--   client_ip: caddy=client_ip,   rent-a-sandbox=forwarded_for,    zitadel=(none)
--   duration:  caddy=duration_s,  rent-a-sandbox=duration_ms,      zitadel=duration (ns)

CREATE TABLE IF NOT EXISTS default.http_access_logs
(
    `Timestamp`       DateTime64(9)                CODEC(Delta(8), ZSTD(3)),
    `TimestampTime`   DateTime                     DEFAULT toDateTime(Timestamp),
    `ServiceName`     LowCardinality(String)       CODEC(ZSTD(3)),
    `Method`          LowCardinality(String)       CODEC(ZSTD(3)),
    `Status`          UInt16                        CODEC(T64, ZSTD(3)),
    `Path`            String                        CODEC(ZSTD(3)),
    `Host`            LowCardinality(String)       CODEC(ZSTD(3)),
    `ClientIP`        String                        CODEC(ZSTD(3)),
    `DurationMs`      Float64                       CODEC(ZSTD(3)),
    `RespSizeBytes`   UInt64                        CODEC(T64, ZSTD(3)),
    `UserAgent`       String                        CODEC(ZSTD(3)),
    `TraceId`         String                        CODEC(ZSTD(3)),
    `SpanId`          String                        CODEC(ZSTD(3)),
    `SeverityText`    LowCardinality(String)       CODEC(ZSTD(3)),
    `Body`            String                        CODEC(ZSTD(3)),
    INDEX idx_trace_id TraceId              TYPE bloom_filter(0.001) GRANULARITY 1,
    INDEX idx_path Path                    TYPE tokenbf_v1(8192, 3, 0) GRANULARITY 4,
    INDEX idx_client_ip ClientIP           TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY toDate(TimestampTime)
PRIMARY KEY (ServiceName, TimestampTime)
ORDER BY (ServiceName, TimestampTime, Method, Status)
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

-- MV runs on every insert into otel_logs; rows without http_method are skipped.
CREATE MATERIALIZED VIEW IF NOT EXISTS default.http_access_logs_mv
TO default.http_access_logs
AS SELECT
    Timestamp,
    toDateTime(Timestamp)                                              AS TimestampTime,
    ServiceName,
    LogAttributes['http_method']                                       AS Method,
    toUInt16OrZero(
        multiIf(
            LogAttributes['http_status']      != '', LogAttributes['http_status'],
            LogAttributes['http_status_code'] != '', LogAttributes['http_status_code'],
            LogAttributes['status']           != '', LogAttributes['status'],
            '0'
        )
    )                                                                  AS Status,
    multiIf(
        LogAttributes['http_uri']    != '', LogAttributes['http_uri'],
        LogAttributes['http_target'] != '', LogAttributes['http_target'],
        LogAttributes['url_path']    != '', LogAttributes['url_path'],
        LogAttributes['path']        != '', LogAttributes['path'],
        ''
    )                                                                  AS Path,
    multiIf(
        LogAttributes['http_host']      != '', LogAttributes['http_host'],
        LogAttributes['instance_host']  != '', LogAttributes['instance_host'],
        ''
    )                                                                  AS Host,
    multiIf(
        LogAttributes['client_ip']     != '', LogAttributes['client_ip'],
        LogAttributes['forwarded_for'] != '', LogAttributes['forwarded_for'],
        ''
    )                                                                  AS ClientIP,
    multiIf(
        -- caddy: seconds (float string)
        LogAttributes['duration_s']  != '', toFloat64OrZero(LogAttributes['duration_s']) * 1000,
        -- rent-a-sandbox: milliseconds
        LogAttributes['duration_ms'] != '', toFloat64OrZero(LogAttributes['duration_ms']),
        -- zitadel: nanoseconds
        LogAttributes['duration']    != '', toFloat64OrZero(LogAttributes['duration']) / 1e6,
        0
    )                                                                  AS DurationMs,
    toUInt64OrZero(LogAttributes['resp_size_bytes'])                    AS RespSizeBytes,
    LogAttributes['user_agent']                                        AS UserAgent,
    TraceId,
    SpanId,
    SeverityText,
    Body
FROM default.otel_logs
WHERE mapContains(LogAttributes, 'http_method');
