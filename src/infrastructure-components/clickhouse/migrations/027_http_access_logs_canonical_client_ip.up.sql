DROP VIEW IF EXISTS default.http_access_logs_mv;

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
    LogAttributes['client_ip']                                          AS ClientIP,
    multiIf(
        LogAttributes['duration_s']  != '', toFloat64OrZero(LogAttributes['duration_s']) * 1000,
        LogAttributes['duration_ms'] != '', toFloat64OrZero(LogAttributes['duration_ms']),
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
