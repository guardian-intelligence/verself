-- OTel tables with optimized types and codecs.
-- The OTel ClickHouse exporter runs with create_schema: false,
-- so these tables must exist before the exporter starts.
--
-- ── Codec rationale (derived from A/B shadow-table testing) ──────────────
--
-- Metric timestamps use DateTime64(3) (millisecond), not the exporter's
-- default DateTime64(9) (nanosecond). The hostmetrics scraper runs at 1s
-- intervals; sub-millisecond digits carry no information. Millisecond
-- precision reduces the TimeUnix column from 47 MiB to 6 MiB (8x) because
-- Delta(8) output shrinks from 30-bit to 10-bit values, leaving 6 of every
-- 8 bytes as zeros that ZSTD compresses trivially.
--
-- Delta(8) is used over DoubleDelta for metric timestamps. The sort key
-- (ServiceName, MetricName, Attributes, TimeUnix) produces a bimodal delta
-- distribution: 84% ~1-second gaps (between scrapes) and 16% ~30us gaps
-- (the OTel process scraper emits one row per matched PID per scrape, all
-- sharing the same (MetricName, Attributes) but offset by microseconds).
-- DoubleDelta's second derivative spikes by +/-10^9 at every mode transition
-- (~every 6 rows), producing noise that ZSTD encodes poorly. Delta(8)'s
-- raw output — two tight value clusters — is more ZSTD-friendly.
-- Tested: Delta(8)+ZSTD(3) = 5.89 MiB vs DoubleDelta+ZSTD(3) = 7.76 MiB
-- at DateTime64(3), same data and merge state.
--
-- Gorilla is not used for Float64 Value/Sum/Min/Max columns. The sort key
-- interleaves different metrics (CPU %, memory bytes, disk ops) so adjacent
-- rows have unrelated magnitudes. Gorilla XOR produces random bits.
-- Tested: ZSTD(3) = 5.23 MiB vs Gorilla+ZSTD(3) = 7.77 MiB.
--
-- Traces keep DateTime64(9) — span timing legitimately uses nanosecond
-- granularity. Logs keep DateTime64(9) for event correlation with traces.
--
-- LowCardinality is applied to all repetitive string columns (ScopeName,
-- MetricName, etc.) — the exporter's default schema uses plain String.
-- Compression improvement: 98%+ per column (e.g. ScopeName 4.3->0.06 MiB).
--
-- ZSTD level raised from 1 to 3 everywhere. 10-20% better compression
-- with negligible write overhead at OTel batch sizes.

-- ─── Metrics: sum ────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS default.otel_metrics_sum
(
    -- LowCardinality: exporter default is plain String; these have <100 distinct
    -- values. Measured 98%+ compression gain (e.g. ScopeName 4.3 MiB -> 57 KiB).
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
    -- DateTime64(3): ms not ns. 1s scrape interval carries no sub-ms information.
    -- Shrinks Delta(8) output from 30-bit to 10-bit values (8x column reduction).
    -- Delta(8) not DoubleDelta: bimodal delta distribution from multi-process
    -- scraping makes DoubleDelta's 2nd derivative spike every ~6 rows. See header.
    `StartTimeUnix`                DateTime64(3)                              CODEC(Delta(8), ZSTD(3)),
    `TimeUnix`                     DateTime64(3)                              CODEC(Delta(8), ZSTD(3)),
    -- No Gorilla: sort key interleaves different metrics so adjacent floats have
    -- unrelated magnitudes. ZSTD(3) alone measured 33% smaller than Gorilla+ZSTD(3).
    `Value`                        Float64                                    CODEC(ZSTD(3)),
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
ORDER BY (ServiceName, MetricName, Attributes, toUnixTimestamp64Milli(TimeUnix))
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

-- ─── Metrics: gauge ──────────────────────────────────────────────────────────
-- Same codec rationale as otel_metrics_sum above.

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
    `StartTimeUnix`                DateTime64(3)                              CODEC(Delta(8), ZSTD(3)),
    `TimeUnix`                     DateTime64(3)                              CODEC(Delta(8), ZSTD(3)),
    `Value`                        Float64                                    CODEC(ZSTD(3)),
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
ORDER BY (ServiceName, MetricName, Attributes, toUnixTimestamp64Milli(TimeUnix))
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
    `StartTimeUnix`                DateTime64(3)                              CODEC(Delta(8), ZSTD(3)),
    `TimeUnix`                     DateTime64(3)                              CODEC(Delta(8), ZSTD(3)),
    `Count`                        UInt64                                     CODEC(T64, ZSTD(3)), -- T64: counter values use few of 64 bits
    `Sum`                          Float64                                    CODEC(ZSTD(3)),     -- No Gorilla: same reasoning as Value above
    `BucketCounts`                 Array(UInt64)                              CODEC(ZSTD(3)),
    `ExplicitBounds`               Array(Float64)                             CODEC(ZSTD(3)),
    `Exemplars.FilteredAttributes` Array(Map(LowCardinality(String), String)) CODEC(ZSTD(3)),
    `Exemplars.TimeUnix`           Array(DateTime64(9))                       CODEC(ZSTD(3)),
    `Exemplars.Value`              Array(Float64)                             CODEC(ZSTD(3)),
    `Exemplars.SpanId`             Array(String)                              CODEC(ZSTD(3)),
    `Exemplars.TraceId`            Array(String)                              CODEC(ZSTD(3)),
    `Flags`                        UInt32                                     CODEC(T64, ZSTD(3)),
    `Min`                          Float64                                    CODEC(ZSTD(3)),
    `Max`                          Float64                                    CODEC(ZSTD(3)),
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
ORDER BY (ServiceName, MetricName, Attributes, toUnixTimestamp64Milli(TimeUnix))
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
    `StartTimeUnix`                DateTime64(3)                              CODEC(Delta(8), ZSTD(3)),
    `TimeUnix`                     DateTime64(3)                              CODEC(Delta(8), ZSTD(3)),
    `Count`                        UInt64                                     CODEC(T64, ZSTD(3)),
    `Sum`                          Float64                                    CODEC(ZSTD(3)),
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
    `Min`                          Float64                                    CODEC(ZSTD(3)),
    `Max`                          Float64                                    CODEC(ZSTD(3)),
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
ORDER BY (ServiceName, MetricName, Attributes, toUnixTimestamp64Milli(TimeUnix))
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
    `StartTimeUnix`                DateTime64(3)                              CODEC(Delta(8), ZSTD(3)),
    `TimeUnix`                     DateTime64(3)                              CODEC(Delta(8), ZSTD(3)),
    `Count`                        UInt64                                     CODEC(T64, ZSTD(3)),
    `Sum`                          Float64                                    CODEC(ZSTD(3)),
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
ORDER BY (ServiceName, MetricName, Attributes, toUnixTimestamp64Milli(TimeUnix))
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

-- ─── Logs ────────────────────────────────────────────────────────────────────
-- DateTime64(9): keeps ns precision for trace-log correlation via TraceId/SpanId.
-- Delta(8) not DoubleDelta: sort key (ServiceName, TimestampTime, Timestamp) has
-- multi-key boundaries where DoubleDelta's 2nd derivative produces ZSTD-hostile noise.

CREATE TABLE IF NOT EXISTS default.otel_logs
(
    `Timestamp`            DateTime64(9)                              CODEC(Delta(8), ZSTD(3)),
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
-- DateTime64(9): span timing legitimately uses nanosecond granularity.
-- Delta(8): sort key (ServiceName, SpanName, Timestamp) — same multi-key reasoning.

CREATE TABLE IF NOT EXISTS default.otel_traces
(
    `Timestamp`            DateTime64(9)                              CODEC(Delta(8), ZSTD(3)),
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
    `Duration`             UInt64                                     CODEC(T64, ZSTD(3)),  -- span duration ns; T64 crops unused high bits
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
-- Delta(4): DateTime is 4 bytes. Sorted by (TraceId, Start) — high-cardinality
-- TraceId means frequent boundary jumps; Delta handles this better than DoubleDelta.

CREATE TABLE IF NOT EXISTS default.otel_traces_trace_id_ts
(
    `TraceId`  String                                         CODEC(ZSTD(3)),
    `Start`    DateTime                                       CODEC(Delta(4), ZSTD(3)),
    `End`      DateTime                                       CODEC(Delta(4), ZSTD(3)),
    INDEX idx_trace_id TraceId TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY toDate(Start)
ORDER BY (TraceId, Start)
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;