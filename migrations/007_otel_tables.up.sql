-- Pre-create OTel tables with optimized types and codecs.
-- The OTel ClickHouse exporter runs with create_schema: false,
-- so these tables must exist before the exporter starts.
--
-- Optimizations applied vs. the exporter's default schema:
--   Repetitive strings:  String -> LowCardinality(String)
--   Timestamps:          Delta(8) -> DoubleDelta (monotonic, evenly spaced)
--   Float values:        ZSTD(1) -> Gorilla + ZSTD(3)
--   Integer counters:    T64 + ZSTD(3) (crops unused high bits)
--   ZSTD level:          1 -> 3 everywhere (10-20% better, negligible write cost)
--
-- On existing installs these are no-ops (IF NOT EXISTS).
-- Migration 008 applies ALTER TABLE MODIFY COLUMN for the delta.

-- ─── Metrics: sum ────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS default.otel_metrics_sum
(
    `ResourceAttributes`           Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    `ResourceSchemaUrl`            LowCardinality(String)                     CODEC(ZSTD(3)),
    `ScopeName`                    LowCardinality(String)                     CODEC(ZSTD(3)),
    `ScopeVersion`                 LowCardinality(String)                     CODEC(ZSTD(3)),
    `ScopeAttributes`              Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    `ScopeDroppedAttrCount`        UInt32                                     CODEC(T64, ZSTD(3)),
    `ScopeSchemaUrl`               LowCardinality(String)                     CODEC(ZSTD(3)),
    `ServiceName`                  LowCardinality(String)                     CODEC(ZSTD(3)),
    `MetricName`                   LowCardinality(String)                     CODEC(ZSTD(3)),
    `MetricDescription`            LowCardinality(String)                     CODEC(ZSTD(3)),
    `MetricUnit`                   LowCardinality(String)                     CODEC(ZSTD(3)),
    `Attributes`                   Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    `StartTimeUnix`                DateTime64(9)                              CODEC(DoubleDelta, ZSTD(3)),
    `TimeUnix`                     DateTime64(9)                              CODEC(DoubleDelta, ZSTD(3)),
    `Value`                        Float64                                    CODEC(Gorilla, ZSTD(3)),
    `Flags`                        UInt32                                     CODEC(T64, ZSTD(3)),
    `Exemplars.FilteredAttributes` Array(Map(LowCardinality(String), String)) CODEC(ZSTD(3)),
    `Exemplars.TimeUnix`           Array(DateTime64(9))                       CODEC(ZSTD(3)),
    `Exemplars.Value`              Array(Float64)                             CODEC(ZSTD(3)),
    `Exemplars.SpanId`             Array(String)                              CODEC(ZSTD(3)),
    `Exemplars.TraceId`            Array(String)                              CODEC(ZSTD(3)),
    `AggregationTemporality`       Int32                                      CODEC(T64, ZSTD(3)),
    `IsMonotonic`                  Bool                                       CODEC(Delta(1), ZSTD(3)),
    INDEX idx_res_attr_key mapKeys(ResourceAttributes)     TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_res_attr_value mapValues(ResourceAttributes) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_scope_attr_key mapKeys(ScopeAttributes)      TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_scope_attr_value mapValues(ScopeAttributes)  TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_attr_key mapKeys(Attributes)                 TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_attr_value mapValues(Attributes)             TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY toDate(TimeUnix)
ORDER BY (ServiceName, MetricName, Attributes, toUnixTimestamp64Nano(TimeUnix))
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

-- ─── Metrics: gauge ──────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS default.otel_metrics_gauge
(
    `ResourceAttributes`           Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    `ResourceSchemaUrl`            LowCardinality(String)                     CODEC(ZSTD(3)),
    `ScopeName`                    LowCardinality(String)                     CODEC(ZSTD(3)),
    `ScopeVersion`                 LowCardinality(String)                     CODEC(ZSTD(3)),
    `ScopeAttributes`              Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    `ScopeDroppedAttrCount`        UInt32                                     CODEC(T64, ZSTD(3)),
    `ScopeSchemaUrl`               LowCardinality(String)                     CODEC(ZSTD(3)),
    `ServiceName`                  LowCardinality(String)                     CODEC(ZSTD(3)),
    `MetricName`                   LowCardinality(String)                     CODEC(ZSTD(3)),
    `MetricDescription`            LowCardinality(String)                     CODEC(ZSTD(3)),
    `MetricUnit`                   LowCardinality(String)                     CODEC(ZSTD(3)),
    `Attributes`                   Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    `StartTimeUnix`                DateTime64(9)                              CODEC(DoubleDelta, ZSTD(3)),
    `TimeUnix`                     DateTime64(9)                              CODEC(DoubleDelta, ZSTD(3)),
    `Value`                        Float64                                    CODEC(Gorilla, ZSTD(3)),
    `Flags`                        UInt32                                     CODEC(T64, ZSTD(3)),
    `Exemplars.FilteredAttributes` Array(Map(LowCardinality(String), String)) CODEC(ZSTD(3)),
    `Exemplars.TimeUnix`           Array(DateTime64(9))                       CODEC(ZSTD(3)),
    `Exemplars.Value`              Array(Float64)                             CODEC(ZSTD(3)),
    `Exemplars.SpanId`             Array(String)                              CODEC(ZSTD(3)),
    `Exemplars.TraceId`            Array(String)                              CODEC(ZSTD(3)),
    INDEX idx_res_attr_key mapKeys(ResourceAttributes)     TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_res_attr_value mapValues(ResourceAttributes) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_scope_attr_key mapKeys(ScopeAttributes)      TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_scope_attr_value mapValues(ScopeAttributes)  TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_attr_key mapKeys(Attributes)                 TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_attr_value mapValues(Attributes)             TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY toDate(TimeUnix)
ORDER BY (ServiceName, MetricName, Attributes, toUnixTimestamp64Nano(TimeUnix))
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

-- ─── Metrics: histogram ──────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS default.otel_metrics_histogram
(
    `ResourceAttributes`           Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    `ResourceSchemaUrl`            LowCardinality(String)                     CODEC(ZSTD(3)),
    `ScopeName`                    LowCardinality(String)                     CODEC(ZSTD(3)),
    `ScopeVersion`                 LowCardinality(String)                     CODEC(ZSTD(3)),
    `ScopeAttributes`              Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    `ScopeDroppedAttrCount`        UInt32                                     CODEC(T64, ZSTD(3)),
    `ScopeSchemaUrl`               LowCardinality(String)                     CODEC(ZSTD(3)),
    `ServiceName`                  LowCardinality(String)                     CODEC(ZSTD(3)),
    `MetricName`                   LowCardinality(String)                     CODEC(ZSTD(3)),
    `MetricDescription`            LowCardinality(String)                     CODEC(ZSTD(3)),
    `MetricUnit`                   LowCardinality(String)                     CODEC(ZSTD(3)),
    `Attributes`                   Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    `StartTimeUnix`                DateTime64(9)                              CODEC(DoubleDelta, ZSTD(3)),
    `TimeUnix`                     DateTime64(9)                              CODEC(DoubleDelta, ZSTD(3)),
    `Count`                        UInt64                                     CODEC(T64, ZSTD(3)),
    `Sum`                          Float64                                    CODEC(Gorilla, ZSTD(3)),
    `BucketCounts`                 Array(UInt64)                              CODEC(ZSTD(3)),
    `ExplicitBounds`               Array(Float64)                             CODEC(ZSTD(3)),
    `Exemplars.FilteredAttributes` Array(Map(LowCardinality(String), String)) CODEC(ZSTD(3)),
    `Exemplars.TimeUnix`           Array(DateTime64(9))                       CODEC(ZSTD(3)),
    `Exemplars.Value`              Array(Float64)                             CODEC(ZSTD(3)),
    `Exemplars.SpanId`             Array(String)                              CODEC(ZSTD(3)),
    `Exemplars.TraceId`            Array(String)                              CODEC(ZSTD(3)),
    `Flags`                        UInt32                                     CODEC(T64, ZSTD(3)),
    `Min`                          Float64                                    CODEC(Gorilla, ZSTD(3)),
    `Max`                          Float64                                    CODEC(Gorilla, ZSTD(3)),
    `AggregationTemporality`       Int32                                      CODEC(T64, ZSTD(3)),
    INDEX idx_res_attr_key mapKeys(ResourceAttributes)     TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_res_attr_value mapValues(ResourceAttributes) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_scope_attr_key mapKeys(ScopeAttributes)      TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_scope_attr_value mapValues(ScopeAttributes)  TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_attr_key mapKeys(Attributes)                 TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_attr_value mapValues(Attributes)             TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY toDate(TimeUnix)
ORDER BY (ServiceName, MetricName, Attributes, toUnixTimestamp64Nano(TimeUnix))
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

-- ─── Metrics: exponential histogram ─────────────────────────────────────────

CREATE TABLE IF NOT EXISTS default.otel_metrics_exponential_histogram
(
    `ResourceAttributes`           Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    `ResourceSchemaUrl`            LowCardinality(String)                     CODEC(ZSTD(3)),
    `ScopeName`                    LowCardinality(String)                     CODEC(ZSTD(3)),
    `ScopeVersion`                 LowCardinality(String)                     CODEC(ZSTD(3)),
    `ScopeAttributes`              Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    `ScopeDroppedAttrCount`        UInt32                                     CODEC(T64, ZSTD(3)),
    `ScopeSchemaUrl`               LowCardinality(String)                     CODEC(ZSTD(3)),
    `ServiceName`                  LowCardinality(String)                     CODEC(ZSTD(3)),
    `MetricName`                   LowCardinality(String)                     CODEC(ZSTD(3)),
    `MetricDescription`            LowCardinality(String)                     CODEC(ZSTD(3)),
    `MetricUnit`                   LowCardinality(String)                     CODEC(ZSTD(3)),
    `Attributes`                   Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    `StartTimeUnix`                DateTime64(9)                              CODEC(DoubleDelta, ZSTD(3)),
    `TimeUnix`                     DateTime64(9)                              CODEC(DoubleDelta, ZSTD(3)),
    `Count`                        UInt64                                     CODEC(T64, ZSTD(3)),
    `Sum`                          Float64                                    CODEC(Gorilla, ZSTD(3)),
    `Scale`                        Int32                                      CODEC(T64, ZSTD(3)),
    `ZeroCount`                    UInt64                                     CODEC(T64, ZSTD(3)),
    `PositiveOffset`               Int32                                      CODEC(T64, ZSTD(3)),
    `PositiveBucketCounts`         Array(UInt64)                              CODEC(ZSTD(3)),
    `NegativeOffset`               Int32                                      CODEC(T64, ZSTD(3)),
    `NegativeBucketCounts`         Array(UInt64)                              CODEC(ZSTD(3)),
    `Exemplars.FilteredAttributes` Array(Map(LowCardinality(String), String)) CODEC(ZSTD(3)),
    `Exemplars.TimeUnix`           Array(DateTime64(9))                       CODEC(ZSTD(3)),
    `Exemplars.Value`              Array(Float64)                             CODEC(ZSTD(3)),
    `Exemplars.SpanId`             Array(String)                              CODEC(ZSTD(3)),
    `Exemplars.TraceId`            Array(String)                              CODEC(ZSTD(3)),
    `Flags`                        UInt32                                     CODEC(T64, ZSTD(3)),
    `Min`                          Float64                                    CODEC(Gorilla, ZSTD(3)),
    `Max`                          Float64                                    CODEC(Gorilla, ZSTD(3)),
    `AggregationTemporality`       Int32                                      CODEC(T64, ZSTD(3)),
    INDEX idx_res_attr_key mapKeys(ResourceAttributes)     TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_res_attr_value mapValues(ResourceAttributes) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_scope_attr_key mapKeys(ScopeAttributes)      TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_scope_attr_value mapValues(ScopeAttributes)  TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_attr_key mapKeys(Attributes)                 TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_attr_value mapValues(Attributes)             TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY toDate(TimeUnix)
ORDER BY (ServiceName, MetricName, Attributes, toUnixTimestamp64Nano(TimeUnix))
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

-- ─── Metrics: summary ────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS default.otel_metrics_summary
(
    `ResourceAttributes`           Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    `ResourceSchemaUrl`            LowCardinality(String)                     CODEC(ZSTD(3)),
    `ScopeName`                    LowCardinality(String)                     CODEC(ZSTD(3)),
    `ScopeVersion`                 LowCardinality(String)                     CODEC(ZSTD(3)),
    `ScopeAttributes`              Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    `ScopeDroppedAttrCount`        UInt32                                     CODEC(T64, ZSTD(3)),
    `ScopeSchemaUrl`               LowCardinality(String)                     CODEC(ZSTD(3)),
    `ServiceName`                  LowCardinality(String)                     CODEC(ZSTD(3)),
    `MetricName`                   LowCardinality(String)                     CODEC(ZSTD(3)),
    `MetricDescription`            LowCardinality(String)                     CODEC(ZSTD(3)),
    `MetricUnit`                   LowCardinality(String)                     CODEC(ZSTD(3)),
    `Attributes`                   Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    `StartTimeUnix`                DateTime64(9)                              CODEC(DoubleDelta, ZSTD(3)),
    `TimeUnix`                     DateTime64(9)                              CODEC(DoubleDelta, ZSTD(3)),
    `Count`                        UInt64                                     CODEC(T64, ZSTD(3)),
    `Sum`                          Float64                                    CODEC(Gorilla, ZSTD(3)),
    `ValueAtQuantiles.Quantile`    Array(Float64)                             CODEC(ZSTD(3)),
    `ValueAtQuantiles.Value`       Array(Float64)                             CODEC(ZSTD(3)),
    `Flags`                        UInt32                                     CODEC(T64, ZSTD(3)),
    INDEX idx_res_attr_key mapKeys(ResourceAttributes)     TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_res_attr_value mapValues(ResourceAttributes) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_scope_attr_key mapKeys(ScopeAttributes)      TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_scope_attr_value mapValues(ScopeAttributes)  TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_attr_key mapKeys(Attributes)                 TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_attr_value mapValues(Attributes)             TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY toDate(TimeUnix)
ORDER BY (ServiceName, MetricName, Attributes, toUnixTimestamp64Nano(TimeUnix))
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

-- ─── Logs ────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS default.otel_logs
(
    `Timestamp`            DateTime64(9)                              CODEC(DoubleDelta, ZSTD(3)),
    `TimestampTime`        DateTime                                   DEFAULT toDateTime(Timestamp),
    `TraceId`              String                                     CODEC(ZSTD(3)),
    `SpanId`               String                                     CODEC(ZSTD(3)),
    `TraceFlags`           UInt8                                      CODEC(ZSTD(3)),
    `SeverityText`         LowCardinality(String)                     CODEC(ZSTD(3)),
    `SeverityNumber`       UInt8                                      CODEC(ZSTD(3)),
    `ServiceName`          LowCardinality(String)                     CODEC(ZSTD(3)),
    `Body`                 String                                     CODEC(ZSTD(3)),
    `ResourceSchemaUrl`    LowCardinality(String)                     CODEC(ZSTD(3)),
    `ResourceAttributes`   Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    `ScopeSchemaUrl`       LowCardinality(String)                     CODEC(ZSTD(3)),
    `ScopeName`            LowCardinality(String)                     CODEC(ZSTD(3)),
    `ScopeVersion`         LowCardinality(String)                     CODEC(ZSTD(3)),
    `ScopeAttributes`      Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    `LogAttributes`        Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    INDEX idx_trace_id TraceId                                TYPE bloom_filter(0.001) GRANULARITY 1,
    INDEX idx_res_attr_key mapKeys(ResourceAttributes)        TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_res_attr_value mapValues(ResourceAttributes)    TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_scope_attr_key mapKeys(ScopeAttributes)         TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_scope_attr_value mapValues(ScopeAttributes)     TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_log_attr_key mapKeys(LogAttributes)             TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_log_attr_value mapValues(LogAttributes)         TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_body Body                                       TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 8
)
ENGINE = MergeTree
PARTITION BY toDate(TimestampTime)
PRIMARY KEY (ServiceName, TimestampTime)
ORDER BY (ServiceName, TimestampTime, Timestamp)
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

-- ─── Traces ──────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS default.otel_traces
(
    `Timestamp`            DateTime64(9)                              CODEC(DoubleDelta, ZSTD(3)),
    `TraceId`              String                                     CODEC(ZSTD(3)),
    `SpanId`               String                                     CODEC(ZSTD(3)),
    `ParentSpanId`         String                                     CODEC(ZSTD(3)),
    `TraceState`           String                                     CODEC(ZSTD(3)),
    `SpanName`             LowCardinality(String)                     CODEC(ZSTD(3)),
    `SpanKind`             LowCardinality(String)                     CODEC(ZSTD(3)),
    `ServiceName`          LowCardinality(String)                     CODEC(ZSTD(3)),
    `ResourceAttributes`   Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    `ScopeName`            LowCardinality(String)                     CODEC(ZSTD(3)),
    `ScopeVersion`         LowCardinality(String)                     CODEC(ZSTD(3)),
    `SpanAttributes`       Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    `Duration`             UInt64                                     CODEC(T64, ZSTD(3)),
    `StatusCode`           LowCardinality(String)                     CODEC(ZSTD(3)),
    `StatusMessage`        String                                     CODEC(ZSTD(3)),
    `Events.Timestamp`     Array(DateTime64(9))                       CODEC(ZSTD(3)),
    `Events.Name`          Array(LowCardinality(String))              CODEC(ZSTD(3)),
    `Events.Attributes`    Array(Map(LowCardinality(String), String)) CODEC(ZSTD(3)),
    `Links.TraceId`        Array(String)                              CODEC(ZSTD(3)),
    `Links.SpanId`         Array(String)                              CODEC(ZSTD(3)),
    `Links.TraceState`     Array(String)                              CODEC(ZSTD(3)),
    `Links.Attributes`     Array(Map(LowCardinality(String), String)) CODEC(ZSTD(3)),
    INDEX idx_trace_id TraceId                                TYPE bloom_filter(0.001) GRANULARITY 1,
    INDEX idx_res_attr_key mapKeys(ResourceAttributes)        TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_res_attr_value mapValues(ResourceAttributes)    TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_span_attr_key mapKeys(SpanAttributes)           TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_span_attr_value mapValues(SpanAttributes)       TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_duration Duration                               TYPE minmax GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY toDate(Timestamp)
ORDER BY (ServiceName, SpanName, toDateTime(Timestamp))
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

-- ─── Traces: trace_id -> timestamp lookup ────────────────────────────────────

CREATE TABLE IF NOT EXISTS default.otel_traces_trace_id_ts
(
    `TraceId`  String                                         CODEC(ZSTD(3)),
    `Start`    DateTime                                       CODEC(DoubleDelta, ZSTD(3)),
    `End`      DateTime                                       CODEC(DoubleDelta, ZSTD(3)),
    INDEX idx_trace_id TraceId TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY toDate(Start)
ORDER BY (TraceId, Start)
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;
