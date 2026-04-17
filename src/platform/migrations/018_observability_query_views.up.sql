-- Operator observability read models.
--
-- Raw OTel storage remains split by metric point type. These views are the
-- ergonomic query surface for CLI tools and dashboards.

DROP VIEW IF EXISTS default.otel_metric_latest;
DROP VIEW IF EXISTS default.otel_metric_catalog_live;
DROP VIEW IF EXISTS default.otel_signal_errors;
DROP VIEW IF EXISTS default.otel_metric_scalar;

CREATE VIEW default.otel_metric_scalar AS
SELECT
    'sum' AS MetricKind,
    ResourceAttributes,
    ResourceSchemaUrl,
    ScopeName,
    ScopeVersion,
    ScopeAttributes,
    ScopeSchemaUrl,
    ServiceName,
    MetricName,
    MetricDescription,
    MetricUnit,
    Attributes,
    StartTimeUnix,
    TimeUnix,
    Value,
    AggregationTemporality,
    IsMonotonic
FROM default.otel_metrics_sum

UNION ALL

SELECT
    'gauge' AS MetricKind,
    ResourceAttributes,
    ResourceSchemaUrl,
    ScopeName,
    ScopeVersion,
    ScopeAttributes,
    ScopeSchemaUrl,
    ServiceName,
    MetricName,
    MetricDescription,
    MetricUnit,
    Attributes,
    StartTimeUnix,
    TimeUnix,
    Value,
    toInt32(0) AS AggregationTemporality,
    false AS IsMonotonic
FROM default.otel_metrics_gauge;

CREATE VIEW default.otel_metric_latest AS
SELECT
    ServiceName,
    MetricName,
    MetricKind,
    MetricUnit,
    Attributes,
    argMax(ResourceAttributes, TimeUnix) AS ResourceAttributes,
    argMax(MetricDescription, TimeUnix) AS MetricDescription,
    argMax(Value, TimeUnix) AS CurrentValue,
    max(TimeUnix) AS SampledAt,
    argMax(StartTimeUnix, TimeUnix) AS StartTimeUnix,
    argMax(AggregationTemporality, TimeUnix) AS AggregationTemporality,
    argMax(IsMonotonic, TimeUnix) AS IsMonotonic,
    count() AS Samples
FROM default.otel_metric_scalar
GROUP BY
    ServiceName,
    MetricName,
    MetricKind,
    MetricUnit,
    Attributes;

CREATE VIEW default.otel_metric_catalog_live AS
SELECT
    ServiceName,
    MetricName,
    MetricKind,
    MetricUnit,
    anyLast(MetricDescription) AS MetricDescription,
    min(TimeUnix) AS FirstSeenAt,
    max(TimeUnix) AS LastSeenAt,
    count() AS Samples,
    uniqExact(Attributes) AS AttributeSets
FROM default.otel_metric_scalar
GROUP BY
    ServiceName,
    MetricName,
    MetricKind,
    MetricUnit;

CREATE VIEW default.otel_signal_errors AS
SELECT
    Timestamp,
    'trace' AS SignalKind,
    ServiceName,
    StatusCode AS Severity,
    SpanName AS Name,
    toUInt16OrZero(SpanAttributes['http.status_code']) AS HttpStatus,
    SpanAttributes['http.method'] AS HttpMethod,
    SpanAttributes['http.target'] AS Path,
    toFloat64(Duration) / 1000000 AS DurationMs,
    TraceId,
    SpanId,
    SpanAttributes AS Attributes,
    StatusMessage AS Message
FROM default.otel_traces
WHERE StatusCode IN ('Error', 'STATUS_CODE_ERROR')
   OR toUInt16OrZero(SpanAttributes['http.status_code']) >= 400

UNION ALL

SELECT
    Timestamp,
    'log' AS SignalKind,
    ServiceName,
    SeverityText AS Severity,
    Body AS Name,
    toUInt16(0) AS HttpStatus,
    '' AS HttpMethod,
    '' AS Path,
    toFloat64(0) AS DurationMs,
    TraceId,
    SpanId,
    LogAttributes AS Attributes,
    Body AS Message
FROM default.otel_logs
WHERE SeverityText IN ('ERROR', 'FATAL', 'WARN')

UNION ALL

SELECT
    Timestamp,
    'http_access' AS SignalKind,
    ServiceName,
    toString(Status) AS Severity,
    concat(Method, ' ', Path) AS Name,
    Status AS HttpStatus,
    Method AS HttpMethod,
    Path,
    DurationMs,
    TraceId,
    SpanId,
    map(
        'host', Host,
        'client_ip', ClientIP,
        'user_agent', UserAgent
    ) AS Attributes,
    Body AS Message
FROM default.http_access_logs
WHERE Status >= 400;
