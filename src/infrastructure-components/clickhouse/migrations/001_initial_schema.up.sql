-- ClickHouse initial schema for verself.
--
-- The ansible clickhouse role applies this file with --database verself,
-- so bare table names land in verself; OTel-compatible tables use fully
-- qualified default.* since the OTel ClickHouse exporter runs with
-- create_schema: false and expects tables in `default`.
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

-- ═══════════════════════════════════════════════════════════════════════════
-- OTel raw storage (database: default)
-- ═══════════════════════════════════════════════════════════════════════════

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

-- ═══════════════════════════════════════════════════════════════════════════
-- HTTP access logs: denormalized projection of otel_logs
-- ═══════════════════════════════════════════════════════════════════════════
-- Filters on mapContains(LogAttributes, 'http_method') which is emitted by
-- HAProxy (journald), console (OTLP), and Zitadel (journald).
--
-- Attribute name normalization per service:
--   status:    haproxy=http_status, console=http_status_code, zitadel=status
--   path:      haproxy=http_uri,    console=http_target,      zitadel=path
--   host:      haproxy=http_host,   console=(none),           zitadel=instance_host
--   client_ip: haproxy=client_ip,   console=client_ip,        zitadel=(none)
--   duration:  haproxy=duration_ms, console=duration_ms,      zitadel=duration (ns)

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
DROP VIEW IF EXISTS default.http_access_logs_mv;

CREATE MATERIALIZED VIEW default.http_access_logs_mv
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
        -- Some log producers report seconds as a float string.
        LogAttributes['duration_s']  != '', toFloat64OrZero(LogAttributes['duration_s']) * 1000,
        -- console: milliseconds
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

-- ═══════════════════════════════════════════════════════════════════════════
-- verself: host SSH authentication events
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS verself.host_auth_events
(
    recorded_at         DateTime64(9)               CODEC(Delta(8), ZSTD(3)),
    event_date          Date                        DEFAULT toDate(recorded_at),

    outcome             LowCardinality(String)      CODEC(ZSTD(3)),
    auth_method         LowCardinality(String)      CODEC(ZSTD(3)),
    user                LowCardinality(String)      CODEC(ZSTD(3)),

    source_ip           String                      CODEC(ZSTD(3)),
    source_port         UInt16                      CODEC(T64, ZSTD(3)),

    key_type            LowCardinality(String)      CODEC(ZSTD(3)),
    key_fingerprint     String                      CODEC(ZSTD(3)),

    cert_serial         String                      CODEC(ZSTD(3)),
    cert_id             String                      CODEC(ZSTD(3)),
    ca_fingerprint      String                      CODEC(ZSTD(3)),

    body                String                      CODEC(ZSTD(3)),

    INDEX idx_source_ip       source_ip       TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_cert_serial     cert_serial     TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_user            user            TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY toDate(event_date)
PRIMARY KEY (event_date, outcome, auth_method)
ORDER BY (event_date, outcome, auth_method, recorded_at)
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

DROP VIEW IF EXISTS verself.host_auth_events_mv;

CREATE MATERIALIZED VIEW verself.host_auth_events_mv
TO verself.host_auth_events
AS SELECT
    Timestamp                                                              AS recorded_at,
    toDate(Timestamp)                                                      AS event_date,
    multiIf(
        match(Body, '^Accepted '),                                          'accepted',
        match(Body, 'invalid user '),                                       'invalid_user',
        match(Body, '^Invalid user '),                                      'invalid_user',
        match(Body, '^Failed password '),                                   'failed_password',
        match(Body, '^Connection closed by authenticating user '),          'preauth_close',
        match(Body, '^Disconnected from authenticating user '),             'preauth_close',
        match(Body, '^Connection closed by '),                              'preauth_close',
        match(Body, '^Authentication refused'),                             'auth_refused',
        'other'
    )                                                                      AS outcome,
    multiIf(
        match(Body, ' CA \\S+ SHA256:'),                                    'publickey-cert',
        match(Body, '^Accepted publickey '),                                'publickey',
        match(Body, '^Accepted password '),                                 'password',
        match(Body, '^Failed password '),                                   'password',
        match(Body, '\\[preauth\\]'),                                       'preauth',
        'other'
    )                                                                      AS auth_method,
    if(
        length(extract(
            Body,
            '(?:Accepted (?:publickey|password) for|Invalid user|Failed password for invalid user|Failed password for|Connection closed by invalid user|Disconnected from invalid user|Connection closed by authenticating user|Disconnected from authenticating user) (\\S+)'
        )) > 32,
        '__truncated__',
        extract(
            Body,
            '(?:Accepted (?:publickey|password) for|Invalid user|Failed password for invalid user|Failed password for|Connection closed by invalid user|Disconnected from invalid user|Connection closed by authenticating user|Disconnected from authenticating user) (\\S+)'
        )
    )                                                                      AS user,
    multiIf(
        match(Body, 'from \\S+ port \\d+'), extract(Body, 'from (\\S+) port \\d+'),
        match(Body, 'user \\S+ \\S+ port \\d+'), extract(Body, 'user \\S+ (\\S+) port \\d+'),
        ''
    )                                                                      AS source_ip,
    toUInt16OrZero(extract(Body, ' port (\\d+)'))                           AS source_port,
    extract(Body, 'ssh2: (\\w+) SHA256:')                                   AS key_type,
    extract(Body, 'ssh2: \\w+ SHA256:(\\S+)')                               AS key_fingerprint,
    extract(Body, '\\(serial (\\d+)\\)')                                    AS cert_serial,
    multiIf(
        match(Body, ' ID "[^"]+"'), extract(Body, ' ID "([^"]+)"'),
        match(Body, ' ID [^ ]+'),   extract(Body, ' ID ([^ ]+)'),
        ''
    )                                                                      AS cert_id,
    extract(Body, ' CA \\w+ SHA256:(\\S+)')                                 AS ca_fingerprint,
    Body                                                                    AS body
FROM default.otel_logs
WHERE ServiceName = 'sshd';

-- ═══════════════════════════════════════════════════════════════════════════
-- verself: sandbox execution logs and wide events
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS verself.job_logs
(
    execution_id        UUID,
    attempt_id          UUID,
    org_id              LowCardinality(String),
    source_kind         LowCardinality(String) DEFAULT '',
    workload_kind       LowCardinality(String) DEFAULT '',
    runner_class        LowCardinality(String) DEFAULT '',
    external_provider   LowCardinality(String) DEFAULT '',
    product_id          LowCardinality(String) DEFAULT '',
    correlation_id      String DEFAULT ''       CODEC(ZSTD(3)),
    repository_full_name LowCardinality(String) DEFAULT '',
    workflow_name       LowCardinality(String) DEFAULT '',
    job_name            LowCardinality(String) DEFAULT '',
    head_branch         LowCardinality(String) DEFAULT '',
    schedule_id         String DEFAULT ''       CODEC(ZSTD(3)),
    seq                 UInt32,
    stream              LowCardinality(String),
    chunk               String                  CODEC(ZSTD(3)),
    created_at          DateTime64(6, 'UTC')   CODEC(DoubleDelta, ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (org_id, source_kind, runner_class, created_at, execution_id, attempt_id, seq)
TTL toDateTime(created_at) + INTERVAL 90 DAY
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS verself.job_events
(
    execution_id            UUID,
    attempt_id              UUID,
    org_id                  LowCardinality(String),
    actor_id                LowCardinality(String),
    kind                    LowCardinality(String),
    source_kind             LowCardinality(String) DEFAULT '',
    workload_kind           LowCardinality(String) DEFAULT '',
    source_ref              String DEFAULT ''       CODEC(ZSTD(3)),
    runner_class            LowCardinality(String) DEFAULT '',
    external_provider       LowCardinality(String) DEFAULT '',
    external_task_id        String DEFAULT ''       CODEC(ZSTD(3)),
    provider                LowCardinality(String),
    product_id              LowCardinality(String),
    lease_id                String DEFAULT ''       CODEC(ZSTD(3)),
    exec_id                 String DEFAULT ''       CODEC(ZSTD(3)),
    repository_full_name    LowCardinality(String) DEFAULT '',
    workflow_name           LowCardinality(String) DEFAULT '',
    job_name                LowCardinality(String) DEFAULT '',
    head_branch             LowCardinality(String) DEFAULT '',
    head_sha                String DEFAULT ''       CODEC(ZSTD(3)),
    provider_installation_id UInt64 DEFAULT 0       CODEC(T64, ZSTD(3)),
    provider_run_id         UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    provider_job_id         UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    schedule_id             String DEFAULT ''       CODEC(ZSTD(3)),
    schedule_display_name   LowCardinality(String) DEFAULT '',
    temporal_workflow_id    String DEFAULT ''       CODEC(ZSTD(3)),
    temporal_run_id         String DEFAULT ''       CODEC(ZSTD(3)),
    run_command             String                  CODEC(ZSTD(3)),
    status                  LowCardinality(String),
    exit_code               Int32                   CODEC(ZSTD(3)),
    duration_ms             Int64                   CODEC(Delta(8), ZSTD(3)),
    zfs_written             UInt64                  CODEC(T64, ZSTD(3)),
    stdout_bytes            UInt64                  CODEC(T64, ZSTD(3)),
    stderr_bytes            UInt64                  CODEC(T64, ZSTD(3)),
    billing_job_id          Int64 DEFAULT 0         CODEC(ZSTD(3)),
    reserved_charge_units   UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    billed_charge_units     UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    writeoff_charge_units   UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    cost_per_unit           UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    pricing_phase           LowCardinality(String) DEFAULT '',
    rootfs_provisioned_bytes UInt64 DEFAULT 0       CODEC(T64, ZSTD(3)),
    boot_time_us            UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    block_read_bytes        UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    block_write_bytes       UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    net_rx_bytes            UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    net_tx_bytes            UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    vcpu_exit_count         UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    correlation_id          String DEFAULT ''       CODEC(ZSTD(3)),
    started_at              DateTime64(6, 'UTC')    CODEC(DoubleDelta, ZSTD(3)),
    completed_at            DateTime64(6, 'UTC')    CODEC(DoubleDelta, ZSTD(3)),
    created_at              DateTime64(6, 'UTC')    CODEC(DoubleDelta, ZSTD(3)),
    trace_id                String DEFAULT ''       CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (org_id, source_kind, runner_class, repository_full_name, created_at, execution_id)
TTL toDateTime(created_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;

-- ═══════════════════════════════════════════════════════════════════════════
-- verself: billing ledger and windowed metering
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS verself.metering (
    window_id                   String                               CODEC(ZSTD(3)),
    org_id                      LowCardinality(String)               CODEC(ZSTD(3)),
    actor_id                    String DEFAULT ''                    CODEC(ZSTD(3)),
    product_id                  LowCardinality(String)               CODEC(ZSTD(3)),
    source_type                 LowCardinality(String)               CODEC(ZSTD(3)),
    source_ref                  String                               CODEC(ZSTD(3)),
    window_seq                  UInt32                               CODEC(Delta(4), ZSTD(3)),
    reservation_shape           LowCardinality(String)               CODEC(ZSTD(3)),
    started_at                  DateTime64(6)                        CODEC(DoubleDelta, ZSTD(3)),
    ended_at                    DateTime64(6)                        CODEC(DoubleDelta, ZSTD(3)),
    reserved_quantity           UInt64                               CODEC(T64, ZSTD(3)),
    actual_quantity             UInt64                               CODEC(T64, ZSTD(3)),
    billable_quantity           UInt64                               CODEC(T64, ZSTD(3)),
    writeoff_quantity           UInt64                               CODEC(T64, ZSTD(3)),
    cycle_id                    LowCardinality(String) DEFAULT ''    CODEC(ZSTD(3)),
    pricing_contract_id         LowCardinality(String) DEFAULT ''    CODEC(ZSTD(3)),
    pricing_phase_id            LowCardinality(String) DEFAULT ''    CODEC(ZSTD(3)),
    pricing_plan_id             LowCardinality(String) DEFAULT ''    CODEC(ZSTD(3)),
    pricing_phase               LowCardinality(String) DEFAULT ''    CODEC(ZSTD(3)),
    dimensions                  Map(LowCardinality(String), Float64) CODEC(ZSTD(3)),
    component_quantities        Map(LowCardinality(String), Float64) CODEC(ZSTD(3)),
    component_charge_units      Map(LowCardinality(String), UInt64)  CODEC(ZSTD(3)),
    bucket_charge_units         Map(LowCardinality(String), UInt64)  CODEC(ZSTD(3)),
    charge_units                UInt64                               CODEC(T64, ZSTD(3)),
    writeoff_charge_units       UInt64                               CODEC(T64, ZSTD(3)),
    free_tier_units             UInt64                               CODEC(T64, ZSTD(3)),
    contract_units              UInt64                               CODEC(T64, ZSTD(3)),
    purchase_units              UInt64                               CODEC(T64, ZSTD(3)),
    promo_units                 UInt64                               CODEC(T64, ZSTD(3)),
    refund_units                UInt64                               CODEC(T64, ZSTD(3)),
    receivable_units            UInt64                               CODEC(T64, ZSTD(3)),
    adjustment_units            UInt64                               CODEC(T64, ZSTD(3)),
    adjustment_reason           LowCardinality(String) DEFAULT ''    CODEC(ZSTD(3)),
    component_free_tier_units   Map(LowCardinality(String), UInt64)  CODEC(ZSTD(3)),
    component_contract_units    Map(LowCardinality(String), UInt64)  CODEC(ZSTD(3)),
    component_purchase_units    Map(LowCardinality(String), UInt64)  CODEC(ZSTD(3)),
    component_promo_units       Map(LowCardinality(String), UInt64)  CODEC(ZSTD(3)),
    component_refund_units      Map(LowCardinality(String), UInt64)  CODEC(ZSTD(3)),
    component_receivable_units  Map(LowCardinality(String), UInt64)  CODEC(ZSTD(3)),
    component_adjustment_units  Map(LowCardinality(String), UInt64)  CODEC(ZSTD(3)),
    usage_evidence              Map(LowCardinality(String), UInt64)  CODEC(ZSTD(3)),
    cost_per_unit               UInt64 DEFAULT 0                     CODEC(T64, ZSTD(3)),
    recorded_at                 DateTime64(6) DEFAULT now64(6)       CODEC(DoubleDelta, ZSTD(3)),
    trace_id                    String DEFAULT ''                    CODEC(ZSTD(3))
)
ENGINE = MergeTree()
ORDER BY (org_id, product_id, started_at, source_ref, window_seq, window_id);

CREATE TABLE IF NOT EXISTS verself.billing_events (
    event_id           String                 CODEC(ZSTD(3)),
    event_type         LowCardinality(String) CODEC(ZSTD(3)),
    event_version      UInt16                 DEFAULT 1 CODEC(T64, ZSTD(3)),
    aggregate_type     LowCardinality(String) CODEC(ZSTD(3)),
    aggregate_id       String                 CODEC(ZSTD(3)),
    contract_id        LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    cycle_id           LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    pricing_contract_id LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    pricing_phase_id   LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    pricing_plan_id    LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    finalization_id    LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    document_id        LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    document_kind      LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    provider_event_id  LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    org_id             LowCardinality(String) CODEC(ZSTD(3)),
    product_id         LowCardinality(String) CODEC(ZSTD(3)),
    occurred_at        DateTime64(6, 'UTC')  CODEC(DoubleDelta, ZSTD(3)),
    payload            String                 CODEC(ZSTD(3)),
    payload_hash       String                 DEFAULT '' CODEC(ZSTD(3)),
    correlation_id     String                 DEFAULT '' CODEC(ZSTD(3)),
    causation_event_id String                 DEFAULT '' CODEC(ZSTD(3)),
    recorded_at        DateTime64(6, 'UTC')  CODEC(DoubleDelta, ZSTD(3))
)
ENGINE = ReplacingMergeTree(recorded_at)
ORDER BY (event_id, occurred_at, aggregate_type, aggregate_id);

-- ═══════════════════════════════════════════════════════════════════════════
-- verself: IAM lifecycle events
-- ═══════════════════════════════════════════════════════════════════════════

CREATE USER IF NOT EXISTS iam_service IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/iam-service' HOST LOCAL;
ALTER USER iam_service IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/iam-service' HOST LOCAL;

CREATE TABLE IF NOT EXISTS verself.iam_events
(
    event_id                 String                 CODEC(ZSTD(3)),
    event_type               LowCardinality(String) CODEC(ZSTD(3)),
    event_version            UInt16 DEFAULT 1       CODEC(T64, ZSTD(3)),
    aggregate_type           LowCardinality(String) CODEC(ZSTD(3)),
    aggregate_id             String                 CODEC(ZSTD(3)),
    signup_intent_id         String DEFAULT ''      CODEC(ZSTD(3)),
    org_id                   String DEFAULT ''      CODEC(ZSTD(3)),
    identity_provider_org_id String DEFAULT ''      CODEC(ZSTD(3)),
    identity_provider_user_id String DEFAULT ''     CODEC(ZSTD(3)),
    step                     LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    state                    LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    outcome                  LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    retryable                UInt8 DEFAULT 0        CODEC(T64, ZSTD(3)),
    attempt                  UInt32 DEFAULT 0       CODEC(T64, ZSTD(3)),
    error_kind               LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    error_message            String DEFAULT ''      CODEC(ZSTD(3)),
    occurred_at              DateTime64(6, 'UTC')   CODEC(DoubleDelta, ZSTD(3)),
    payload                  String DEFAULT '{}'    CODEC(ZSTD(3)),
    idempotency_key_hash     String DEFAULT ''      CODEC(ZSTD(3)),
    correlation_id           String DEFAULT ''      CODEC(ZSTD(3)),
    recorded_at              DateTime64(6, 'UTC') DEFAULT now64(6) CODEC(DoubleDelta, ZSTD(3)),
    trace_id                 String DEFAULT ''      CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(toDate(recorded_at))
ORDER BY (event_type, org_id, signup_intent_id, occurred_at, event_id)
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

GRANT INSERT ON verself.iam_events TO iam_service;

-- ═══════════════════════════════════════════════════════════════════════════
-- verself: notification delivery ledger
-- ═══════════════════════════════════════════════════════════════════════════

CREATE USER IF NOT EXISTS notifications_service IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/notifications-service' HOST LOCAL;
ALTER USER notifications_service IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/notifications-service' HOST LOCAL;
GRANT SELECT ON default.otel_traces TO notifications_service;

CREATE TABLE IF NOT EXISTS verself.notification_events
(
    recorded_at DateTime64(9, 'UTC') CODEC(Delta(8), ZSTD(3)),
    occurred_at DateTime64(9, 'UTC') CODEC(Delta(8), ZSTD(3)),
    schema_version LowCardinality(String) CODEC(ZSTD(3)),
    ledger_event_id UUID,
    event_type LowCardinality(String) CODEC(ZSTD(3)),
    org_id LowCardinality(String) CODEC(ZSTD(3)),
    recipient_subject_id String CODEC(ZSTD(3)),
    notification_id UUID,
    recipient_sequence UInt64 CODEC(T64, ZSTD(3)),
    event_source LowCardinality(String) CODEC(ZSTD(3)),
    source_subject LowCardinality(String) CODEC(ZSTD(3)),
    source_event_id UUID,
    kind LowCardinality(String) CODEC(ZSTD(3)),
    priority LowCardinality(String) CODEC(ZSTD(3)),
    status LowCardinality(String) CODEC(ZSTD(3)),
    reason LowCardinality(String) CODEC(ZSTD(3)),
    content_sha256 FixedString(64) CODEC(ZSTD(3)),
    trace_id String CODEC(ZSTD(3)),
    span_id String CODEC(ZSTD(3)),
    traceparent String CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(toDate(recorded_at))
ORDER BY (event_type, org_id, kind, status, recipient_subject_id, occurred_at, ledger_event_id)
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

GRANT SELECT, INSERT ON verself.notification_events TO notifications_service;

-- ═══════════════════════════════════════════════════════════════════════════
-- verself: vm-orchestrator lease evidence projection
-- ═══════════════════════════════════════════════════════════════════════════
-- Typed projection of lease lifecycle, exec starts, and telemetry diagnostics.

CREATE TABLE IF NOT EXISTS verself.vm_lease_evidence
(
    `evidence_time`                 DateTime64(9)            CODEC(Delta(8), ZSTD(3)),
    `evidence_date`                 Date                      DEFAULT toDate(evidence_time),
    `service_name`                  LowCardinality(String)   CODEC(ZSTD(3)),
    `org_id`                        String                    CODEC(ZSTD(3)),
    `storage_key_version`           String                    CODEC(ZSTD(3)),
    `storage_key_ref_count`         UInt32                    CODEC(T64, ZSTD(3)),
    `image_ref`                     String                    CODEC(ZSTD(3)),
    `zfs_dataset`                   String                    CODEC(ZSTD(3)),
    `source_snapshot_ref`           String                    CODEC(ZSTD(3)),
    `target_snapshot_ref`           String                    CODEC(ZSTD(3)),
    `mount_name`                    String                    CODEC(ZSTD(3)),
    `lease_id`                      String                    CODEC(ZSTD(3)),
    `exec_id`                       String                    CODEC(ZSTD(3)),
    `evidence_type`                 LowCardinality(String)   CODEC(ZSTD(3)),
    `diagnostic_kind`               LowCardinality(String)   CODEC(ZSTD(3)),
    `reason_code`                   LowCardinality(String)   CODEC(ZSTD(3)),
    `reason`                        String                    CODEC(ZSTD(3)),
    `expected_seq`                  UInt32                    CODEC(T64, ZSTD(3)),
    `observed_seq`                  UInt32                    CODEC(T64, ZSTD(3)),
    `missing_samples`               UInt32                    CODEC(T64, ZSTD(3)),
    `host_received_unix_nano`       UInt64                    CODEC(T64, ZSTD(3)),
    `telemetry_received_unix_nano`  UInt64                    CODEC(T64, ZSTD(3)),
    `trace_id`                      String                    CODEC(ZSTD(3)),
    `span_id`                       String                    CODEC(ZSTD(3)),
    INDEX idx_lease_id lease_id TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_exec_id exec_id TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY evidence_date
ORDER BY (evidence_type, diagnostic_kind, reason_code, lease_id, exec_id, evidence_time)
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

DROP VIEW IF EXISTS verself.vm_lease_evidence_mv;

CREATE MATERIALIZED VIEW verself.vm_lease_evidence_mv
TO verself.vm_lease_evidence
AS
SELECT
    Timestamp AS evidence_time,
    ServiceName AS service_name,
    LogAttributes['org_id'] AS org_id,
    LogAttributes['key_version'] AS storage_key_version,
    toUInt32OrZero(LogAttributes['ref_count']) AS storage_key_ref_count,
    LogAttributes['image_ref'] AS image_ref,
    LogAttributes['dataset'] AS zfs_dataset,
    LogAttributes['source_snapshot'] AS source_snapshot_ref,
    LogAttributes['target_snapshot'] AS target_snapshot_ref,
    LogAttributes['mount_name'] AS mount_name,
    LogAttributes['lease_id'] AS lease_id,
    LogAttributes['exec_id'] AS exec_id,
    multiIf(
        Body = 'lease ready', 'lease_ready',
        Body = 'guest exec started', 'exec_started',
        Body = 'guest telemetry hello received', 'telemetry_hello',
        Body = 'guest telemetry stream diagnostic', 'telemetry_diagnostic',
        Body = 'lease runtime cleaned up', 'lease_cleanup',
        Body = 'storage key acquired', 'storage_key_acquired',
        Body = 'storage key released', 'storage_key_released',
        Body = 'storage namespace key unloaded', 'storage_key_unloaded',
        Body = 'idle storage namespace key unloaded', 'storage_key_idle_unloaded',
        Body = 'org image materialized', 'org_image_materialized',
        Body = 'filesystem mount source snapshot missing', 'filesystem_mount_cache_miss',
        'other'
    ) AS evidence_type,
    if(Body = 'guest telemetry stream diagnostic', LogAttributes['kind'], '') AS diagnostic_kind,
    multiIf(
        Body = 'guest telemetry stream diagnostic', 'telemetry_diagnostic',
        Body = 'guest telemetry hello received', 'telemetry_hello',
        Body = 'lease ready', 'lease_ready',
        Body = 'guest exec started', 'exec_started',
        Body = 'lease runtime cleaned up', 'lease_cleanup',
        Body = 'storage key acquired', 'storage_key_acquired',
        Body = 'storage key released', 'storage_key_released',
        Body = 'storage namespace key unloaded', 'storage_key_unloaded',
        Body = 'idle storage namespace key unloaded', 'storage_key_idle_unloaded',
        Body = 'org image materialized', 'org_image_materialized',
        Body = 'filesystem mount source snapshot missing', 'filesystem_mount_cache_miss',
        'other'
    ) AS reason_code,
    LogAttributes['reason'] AS reason,
    toUInt32OrZero(if(Body = 'guest telemetry stream diagnostic', LogAttributes['expected_seq'], '0')) AS expected_seq,
    toUInt32OrZero(if(Body = 'guest telemetry stream diagnostic', LogAttributes['observed_seq'], '0')) AS observed_seq,
    toUInt32OrZero(if(Body = 'guest telemetry stream diagnostic', LogAttributes['missing_samples'], '0')) AS missing_samples,
    toUInt64OrZero(LogAttributes['host_received_unix_nano']) AS host_received_unix_nano,
    toUInt64OrZero(LogAttributes['telemetry_received_unix_nano']) AS telemetry_received_unix_nano,
    TraceId AS trace_id,
    SpanId AS span_id
FROM default.otel_logs
WHERE ServiceName = 'vm-orchestrator'
  AND Body IN (
    'lease ready',
    'guest exec started',
    'guest telemetry hello received',
    'guest telemetry stream diagnostic',
    'lease runtime cleaned up',
    'storage key acquired',
    'storage key released',
    'storage namespace key unloaded',
    'idle storage namespace key unloaded',
    'org image materialized',
    'filesystem mount source snapshot missing'
  );

-- ═══════════════════════════════════════════════════════════════════════════
-- verself: governance OCSF API Activity ledger
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS verself.api_activity_events
(
    time DateTime64(9, 'UTC') CODEC(Delta(8), ZSTD(3)),
    event_date Date DEFAULT toDate(time),
    metadata_uid UUID,
    org_id LowCardinality(String),
    sequence UInt64,

    ocsf_version LowCardinality(String),
    category_uid UInt8,
    category_name LowCardinality(String),
    class_uid UInt16,
    class_name LowCardinality(String),
    type_uid UInt32,
    activity_id UInt8,
    activity_name LowCardinality(String),
    action_id UInt8,
    action LowCardinality(String),
    status_id UInt8,
    status LowCardinality(String),
    status_code LowCardinality(String),
    severity_id UInt8,
    severity LowCardinality(String),

    api_service LowCardinality(String),
    api_operation LowCardinality(String),
    api_version LowCardinality(String),
    actor_type LowCardinality(String),
    actor_uid String,
    actor_name String,
    credential_uid String,
    primary_resource_type LowCardinality(String),
    primary_resource_uid String,
    primary_resource_name String,
    primary_resource_full_name String,
    permission LowCardinality(String),

    http_request_uid String,
    http_method LowCardinality(String),
    http_route String,
    http_args String,
    http_user_agent String,
    src_endpoint_ip String,
    src_endpoint_name String,
    http_response_code UInt16,
    trace_uid String,
    span_uid String,
    ocsf_sha256 FixedString(64),

    prev_hmac String,
    row_hmac String,
    hmac_key_id LowCardinality(String)
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_date)
ORDER BY (org_id, event_date, api_service, status_id, time, sequence, metadata_uid)
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS verself.api_activity_payloads
(
    metadata_uid UUID,
    org_id LowCardinality(String),
    event_date Date,
    ocsf_json String CODEC(ZSTD(6)),
    ocsf_sha256 FixedString(64)
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_date)
ORDER BY (org_id, event_date, metadata_uid)
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS verself.api_activity_resources
(
    metadata_uid UUID,
    org_id LowCardinality(String),
    event_date Date,
    resource_ordinal UInt8,
    resource_role_id UInt8,
    resource_role LowCardinality(String),
    resource_type LowCardinality(String),
    resource_uid String,
    resource_name String,
    resource_full_name String
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_date)
ORDER BY (org_id, resource_type, resource_uid, event_date, metadata_uid)
SETTINGS index_granularity = 8192;

-- ═══════════════════════════════════════════════════════════════════════════
-- verself: object-storage access events
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS verself.object_access_events
(
    recorded_at DateTime64(6, 'UTC'),
    event_date Date DEFAULT toDate(recorded_at),

    environment LowCardinality(String),
    service_version LowCardinality(String),
    writer_instance_id String,

    org_id LowCardinality(String),
    bucket_id String,
    bucket_name LowCardinality(String),
    requested_bucket LowCardinality(String),
    resolved_alias LowCardinality(String),
    resolved_prefix String,

    operation LowCardinality(String),
    auth_mode LowCardinality(String),
    access_key_id String,
    spiffe_peer_id String,
    trace_id String,
    span_id String,

    status UInt16,
    bytes_in UInt64 CODEC(T64, ZSTD(3)),
    bytes_out UInt64 CODEC(T64, ZSTD(3)),
    latency_ms UInt32 CODEC(T64, ZSTD(3)),
    client_ip_hash String,
    user_agent_hash String,
    error_class LowCardinality(String),
    error_message String,
    upstream_status UInt16,
    upstream_request_uri String
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_date)
ORDER BY (org_id, operation, auth_mode, status, event_date, recorded_at, bucket_id)
TTL toDateTime(recorded_at) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192;

-- ═══════════════════════════════════════════════════════════════════════════
-- verself: operator command evidence
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS verself.operator_command_runs
(
    `event_at`        DateTime64(3)             CODEC(Delta(8), ZSTD(3)),
    `run_id`          String                    CODEC(ZSTD(3)),
    `site`            LowCardinality(String)    CODEC(ZSTD(3)),
    `command`         LowCardinality(String)    CODEC(ZSTD(3)),
    `ssh_auth_method` LowCardinality(String)    CODEC(ZSTD(3)),
    `target_host`     String                    CODEC(ZSTD(3)),
    `target_user`     LowCardinality(String)    CODEC(ZSTD(3)),
    `status`          LowCardinality(String)    CODEC(ZSTD(3)),
    `duration_ms`     UInt32                    CODEC(T64, ZSTD(3)),
    `error_kind`      LowCardinality(String)    DEFAULT '' CODEC(ZSTD(3)),
    `error_message`   String                    DEFAULT '' CODEC(ZSTD(3)),
    `trace_id`        String                    DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = MergeTree
ORDER BY (site, command, event_at, run_id)
SETTINGS index_granularity = 8192;

-- ═══════════════════════════════════════════════════════════════════════════
-- verself: sandbox-rental append-only observability
-- ═══════════════════════════════════════════════════════════════════════════

CREATE USER IF NOT EXISTS sandbox_rental IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/sandbox-rental-service' HOST LOCAL;
ALTER USER sandbox_rental IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/sandbox-rental-service' HOST LOCAL;

CREATE TABLE IF NOT EXISTS verself.durable_events
(
    observed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    org_id LowCardinality(String) CODEC(ZSTD(3)),
    repository_id UInt64 CODEC(T64, ZSTD(3)),
    provider LowCardinality(String),
    provider_repository_id UInt64 CODEC(T64, ZSTD(3)),
    provider_run_id UInt64 CODEC(T64, ZSTD(3)),
    provider_run_attempt UInt64 CODEC(T64, ZSTD(3)),
    provider_job_id UInt64 CODEC(T64, ZSTD(3)),
    execution_id UUID,
    attempt_id UUID,
    operation_id UUID,
    durable_scope_id UUID,
    durable_generation_id UUID,
    cache_name LowCardinality(String),
    event_name LowCardinality(String),
    result LowCardinality(String),
    reason String DEFAULT '' CODEC(ZSTD(3)),
    mount_name LowCardinality(String),
    source_generation_id UUID,
    candidate_generation_id UUID,
    current_generation_id UUID,
    zfs_snapshot_ref String DEFAULT '' CODEC(ZSTD(3)),
    used_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    written_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    pool_size_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    pool_allocated_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    pool_free_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    org_quota_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    org_used_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    org_available_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    trace_id String DEFAULT '' CODEC(ZSTD(3)),
    span_id String DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (event_name, provider, org_id, repository_id, cache_name, observed_at, operation_id);

GRANT SELECT, INSERT ON verself.durable_events TO sandbox_rental;
GRANT SELECT ON verself.durable_events TO notifications_service;

CREATE TABLE IF NOT EXISTS verself.bazel_invocations
(
    observed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    org_id LowCardinality(String) CODEC(ZSTD(3)),
    provider LowCardinality(String),
    provider_repository_id UInt64 CODEC(T64, ZSTD(3)),
    provider_run_id UInt64 CODEC(T64, ZSTD(3)),
    provider_job_id UInt64 CODEC(T64, ZSTD(3)),
    execution_id UUID,
    attempt_id UUID,
    invocation_id String CODEC(ZSTD(3)),
    command LowCardinality(String),
    args Array(String),
    target_patterns Array(String),
    working_directory String DEFAULT '' CODEC(ZSTD(3)),
    github_workflow LowCardinality(String) DEFAULT '',
    github_job LowCardinality(String) DEFAULT '',
    github_ref String DEFAULT '' CODEC(ZSTD(3)),
    github_sha String DEFAULT '' CODEC(ZSTD(3)),
    exit_code Int32 CODEC(ZSTD(3)),
    duration_ms Int64 CODEC(Delta(8), ZSTD(3)),
    profile_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    bep_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    execution_log_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    profile_span_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    package_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    spawn_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    target_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    failed_target_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    started_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    completed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    trace_id String DEFAULT '' CODEC(ZSTD(3)),
    span_id String DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (command, provider, org_id, provider_repository_id, provider_run_id, provider_job_id, observed_at, invocation_id)
TTL toDateTime(observed_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS verself.bazel_events
(
    observed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    org_id LowCardinality(String) CODEC(ZSTD(3)),
    provider LowCardinality(String),
    provider_repository_id UInt64 CODEC(T64, ZSTD(3)),
    provider_run_id UInt64 CODEC(T64, ZSTD(3)),
    provider_job_id UInt64 CODEC(T64, ZSTD(3)),
    execution_id UUID,
    attempt_id UUID,
    invocation_id String CODEC(ZSTD(3)),
    command LowCardinality(String),
    event_name LowCardinality(String),
    result LowCardinality(String),
    reason String DEFAULT '' CODEC(ZSTD(3)),
    exit_code Int32 CODEC(ZSTD(3)),
    duration_ms Int64 CODEC(Delta(8), ZSTD(3)),
    profile_span_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    package_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    spawn_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    target_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    trace_id String DEFAULT '' CODEC(ZSTD(3)),
    span_id String DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (event_name, result, command, provider, org_id, provider_repository_id, provider_run_id, observed_at, invocation_id)
TTL toDateTime(observed_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS verself.bazel_profile_spans
(
    observed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    org_id LowCardinality(String) CODEC(ZSTD(3)),
    provider LowCardinality(String),
    provider_repository_id UInt64 CODEC(T64, ZSTD(3)),
    provider_run_id UInt64 CODEC(T64, ZSTD(3)),
    provider_job_id UInt64 CODEC(T64, ZSTD(3)),
    execution_id UUID,
    attempt_id UUID,
    invocation_id String CODEC(ZSTD(3)),
    command LowCardinality(String),
    span_kind LowCardinality(String),
    category LowCardinality(String),
    event_name String CODEC(ZSTD(3)),
    package_name String DEFAULT '' CODEC(ZSTD(3)),
    build_file String DEFAULT '' CODEC(ZSTD(3)),
    external_repo LowCardinality(String) DEFAULT '',
    started_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    duration_ms Int64 CODEC(Delta(8), ZSTD(3)),
    trace_id String DEFAULT '' CODEC(ZSTD(3)),
    span_id String DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (span_kind, command, provider, org_id, provider_repository_id, provider_run_id, build_file, started_at, invocation_id)
TTL toDateTime(observed_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS verself.bazel_spawns
(
    observed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    org_id LowCardinality(String) CODEC(ZSTD(3)),
    provider LowCardinality(String),
    provider_repository_id UInt64 CODEC(T64, ZSTD(3)),
    provider_run_id UInt64 CODEC(T64, ZSTD(3)),
    provider_job_id UInt64 CODEC(T64, ZSTD(3)),
    execution_id UUID,
    attempt_id UUID,
    invocation_id String CODEC(ZSTD(3)),
    command LowCardinality(String),
    target_label String CODEC(ZSTD(3)),
    package_name String DEFAULT '' CODEC(ZSTD(3)),
    build_file String DEFAULT '' CODEC(ZSTD(3)),
    rule_name LowCardinality(String) DEFAULT '',
    mnemonic LowCardinality(String) DEFAULT '',
    runner LowCardinality(String) DEFAULT '',
    cache_hit UInt8 DEFAULT 0 CODEC(T64, ZSTD(3)),
    exit_code Int32 CODEC(ZSTD(3)),
    status String DEFAULT '' CODEC(ZSTD(3)),
    output_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    output_first String DEFAULT '' CODEC(ZSTD(3)),
    started_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    duration_ms Int64 CODEC(Delta(8), ZSTD(3)),
    trace_id String DEFAULT '' CODEC(ZSTD(3)),
    span_id String DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (command, mnemonic, runner, cache_hit, provider, org_id, provider_repository_id, provider_run_id, build_file, started_at, invocation_id)
TTL toDateTime(observed_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS verself.bazel_targets
(
    observed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    org_id LowCardinality(String) CODEC(ZSTD(3)),
    provider LowCardinality(String),
    provider_repository_id UInt64 CODEC(T64, ZSTD(3)),
    provider_run_id UInt64 CODEC(T64, ZSTD(3)),
    provider_job_id UInt64 CODEC(T64, ZSTD(3)),
    execution_id UUID,
    attempt_id UUID,
    invocation_id String CODEC(ZSTD(3)),
    command LowCardinality(String),
    label String CODEC(ZSTD(3)),
    package_name String DEFAULT '' CODEC(ZSTD(3)),
    build_file String DEFAULT '' CODEC(ZSTD(3)),
    rule_name LowCardinality(String) DEFAULT '',
    success UInt8 DEFAULT 0 CODEC(T64, ZSTD(3)),
    output_group_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    output_file_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    trace_id String DEFAULT '' CODEC(ZSTD(3)),
    span_id String DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (command, success, provider, org_id, provider_repository_id, provider_run_id, build_file, observed_at, invocation_id)
TTL toDateTime(observed_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;

GRANT SELECT, INSERT ON verself.bazel_invocations TO sandbox_rental;
GRANT SELECT, INSERT ON verself.bazel_events TO sandbox_rental;
GRANT SELECT, INSERT ON verself.bazel_profile_spans TO sandbox_rental;
GRANT SELECT, INSERT ON verself.bazel_spawns TO sandbox_rental;
GRANT SELECT, INSERT ON verself.bazel_targets TO sandbox_rental;

CREATE TABLE IF NOT EXISTS verself.golden_vm_events
(
    observed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    org_id LowCardinality(String) CODEC(ZSTD(3)),
    repository_id UInt64 CODEC(T64, ZSTD(3)),
    provider LowCardinality(String),
    provider_repository_id UInt64 CODEC(T64, ZSTD(3)),
    provider_run_id UInt64 CODEC(T64, ZSTD(3)),
    provider_run_attempt UInt64 CODEC(T64, ZSTD(3)),
    provider_job_id UInt64 CODEC(T64, ZSTD(3)),
    execution_id UUID,
    attempt_id UUID,
    operation_id UUID,
    golden_vm_snapshot_id UUID,
    job_shape_id UUID,
    event_name LowCardinality(String),
    result LowCardinality(String),
    reason String DEFAULT '' CODEC(ZSTD(3)),
    from_state LowCardinality(String) DEFAULT '',
    to_state LowCardinality(String) DEFAULT '',
    lease_id String DEFAULT '' CODEC(ZSTD(3)),
    exec_id String DEFAULT '' CODEC(ZSTD(3)),
    river_job_id Int64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    generation_set_hash String DEFAULT '' CODEC(ZSTD(3)),
    source_generation_set_hash String DEFAULT '' CODEC(ZSTD(3)),
    snapshot_key String DEFAULT '' CODEC(ZSTD(3)),
    activation_mode LowCardinality(String),
    vmstate_artifact_ref String DEFAULT '' CODEC(ZSTD(3)),
    memory_artifact_ref String DEFAULT '' CODEC(ZSTD(3)),
    root_snapshot_ref String DEFAULT '' CODEC(ZSTD(3)),
    root_snapshot_guid String DEFAULT '' CODEC(ZSTD(3)),
    state_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    memory_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    trace_id String DEFAULT '' CODEC(ZSTD(3)),
    span_id String DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (event_name, provider, org_id, repository_id, observed_at, operation_id, golden_vm_snapshot_id);

GRANT SELECT, INSERT ON verself.golden_vm_events TO sandbox_rental;
GRANT SELECT ON verself.golden_vm_events TO notifications_service;

CREATE TABLE IF NOT EXISTS verself.sandbox_phase_events
(
    observed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    event_source LowCardinality(String),
    phase_group LowCardinality(String),
    phase_name LowCardinality(String),
    phase_order UInt16 CODEC(T64, ZSTD(3)),
    result LowCardinality(String),
    reason String DEFAULT '' CODEC(ZSTD(3)),
    org_id LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    execution_id UUID,
    attempt_id UUID,
    allocation_id UUID,
    provider LowCardinality(String) DEFAULT '',
    external_provider LowCardinality(String) DEFAULT '',
    provider_installation_id UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    provider_repository_id UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    provider_run_id UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    provider_run_attempt UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    provider_job_id UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    repository_full_name LowCardinality(String) DEFAULT '',
    workflow_name LowCardinality(String) DEFAULT '',
    job_name LowCardinality(String) DEFAULT '',
    head_branch LowCardinality(String) DEFAULT '',
    head_sha String DEFAULT '' CODEC(ZSTD(3)),
    runner_class LowCardinality(String) DEFAULT '',
    runner_name String DEFAULT '' CODEC(ZSTD(3)),
    lease_id String DEFAULT '' CODEC(ZSTD(3)),
    exec_id String DEFAULT '' CODEC(ZSTD(3)),
    correlation_id String DEFAULT '' CODEC(ZSTD(3)),
    started_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    completed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    duration_ms Int64 CODEC(Delta(8), ZSTD(3)),
    attributes_json String DEFAULT '' CODEC(ZSTD(3)),
    trace_id String DEFAULT '' CODEC(ZSTD(3)),
    span_id String DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (phase_group, phase_name, result, provider, org_id, provider_repository_id, provider_run_id, provider_job_id, execution_id, started_at)
TTL toDateTime(observed_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;

DROP VIEW IF EXISTS verself.sandbox_execution_phase_timeline;
DROP VIEW IF EXISTS verself.sandbox_phase_timeline;

CREATE VIEW verself.sandbox_phase_timeline AS
SELECT
    observed_at,
    event_source,
    phase_group,
    phase_name,
    phase_order,
    result,
    reason,
    org_id,
    execution_id,
    attempt_id,
    allocation_id,
    provider,
    external_provider,
    provider_installation_id,
    provider_repository_id,
    provider_run_id,
    provider_run_attempt,
    provider_job_id,
    repository_full_name,
    workflow_name,
    job_name,
    head_branch,
    head_sha,
    runner_class,
    runner_name,
    lease_id,
    exec_id,
    correlation_id,
    started_at,
    completed_at,
    duration_ms,
    attributes_json,
    trace_id,
    span_id
FROM verself.sandbox_phase_events

UNION ALL

SELECT
    toDateTime64(Timestamp, 6, 'UTC') AS observed_at,
    'vm-orchestrator-otel' AS event_source,
    multiIf(
        startsWith(SpanName, 'vmorchestrator.guest.boot.'), 'vm.guest.boot',
        startsWith(SpanName, 'vmorchestrator.guest.kernel_'), 'vm.guest.kernel',
        startsWith(SpanName, 'vmorchestrator.guest.exec_'), 'vm.guest.exec',
        startsWith(SpanName, 'vmorchestrator.guest.'), 'vm.guest',
        startsWith(SpanName, 'vmorchestrator.firecracker.'), 'vm.firecracker',
        startsWith(SpanName, 'vmorchestrator.zfs.'), 'vm.zfs',
        startsWith(SpanName, 'vmorchestrator.zvol.'), 'vm.zvol',
        startsWith(SpanName, 'vmorchestrator.network.'), 'vm.network',
        startsWith(SpanName, 'vmorchestrator.jailer.'), 'vm.jailer',
        startsWith(SpanName, 'vmorchestrator.jail.'), 'vm.jail',
        SpanName = 'vmorchestrator.lease.boot', 'vm.lease',
        startsWith(SpanName, 'rpc.'), 'vm.rpc',
        'vm.orchestrator'
    ) AS phase_group,
    multiIf(
        SpanName = 'rpc.AcquireLease', 'vm.rpc.acquire_lease',
        SpanName = 'rpc.StartExec', 'vm.rpc.start_exec',
        SpanName = 'rpc.WaitExec', 'vm.rpc.wait_exec',
        SpanName = 'rpc.ReleaseLease', 'vm.rpc.release_lease',
        concat('vm.', replaceOne(SpanName, 'vmorchestrator.', ''))
    ) AS phase_name,
    toUInt16(multiIf(
        SpanName = 'rpc.AcquireLease', 88,
        SpanName = 'vmorchestrator.lease.boot', 91,
        SpanName = 'vmorchestrator.org_runtime.require_ready_check', 92,
        SpanName = 'vmorchestrator.zfs.root_clone', 93,
        SpanName = 'vmorchestrator.zfs.root_resize_ext4', 94,
        SpanName = 'vmorchestrator.zfs.mounts_prepare', 95,
        SpanName = 'vmorchestrator.zfs.mount_prepare', 96,
        SpanName = 'vmorchestrator.zvol.wait_device', 97,
        SpanName = 'vmorchestrator.zvol.mount_wait_device', 98,
        SpanName = 'vmorchestrator.jail.setup', 99,
        SpanName = 'vmorchestrator.network.setup', 100,
        SpanName = 'vmorchestrator.jailer.start', 101,
        SpanName = 'vmorchestrator.firecracker.api_socket_wait', 102,
        SpanName = 'vmorchestrator.firecracker.golden_snapshot_lookup', 103,
        SpanName = 'vmorchestrator.firecracker.snapshot_stage', 104,
        SpanName = 'vmorchestrator.firecracker.restore_metrics', 105,
        SpanName = 'vmorchestrator.firecracker.snapshot_load', 106,
        SpanName = 'vmorchestrator.firecracker.snapshot_resume', 107,
        SpanName = 'vmorchestrator.firecracker.configure_all', 108,
        SpanName = 'vmorchestrator.firecracker.configure', 109,
        SpanName = 'vmorchestrator.firecracker.instance_start', 110,
        SpanName = 'vmorchestrator.guest.control_socket_wait', 111,
        SpanName = 'vmorchestrator.guest.control_connect', 112,
        SpanName = 'vmorchestrator.guest.after_restore', 113,
        SpanName = 'vmorchestrator.guest.hello', 114,
        SpanName = 'vmorchestrator.guest.lease_init', 115,
        startsWith(SpanName, 'vmorchestrator.guest.kernel_'), 116,
        SpanName = 'vmorchestrator.guest.boot_report', 117,
        startsWith(SpanName, 'vmorchestrator.guest.boot.'), 118,
        SpanName = 'rpc.StartExec', 121,
        SpanName = 'vmorchestrator.guest.exec_dispatch', 122,
        SpanName = 'vmorchestrator.guest.exec_workload', 170,
        SpanName = 'vmorchestrator.guest.exec_teardown', 181,
        SpanName = 'rpc.WaitExec', 191,
        SpanName = 'rpc.ReleaseLease', 241,
        1000
    )) AS phase_order,
    if(StatusCode IN ('Error', 'STATUS_CODE_ERROR'), 'failed', 'succeeded') AS result,
    StatusMessage AS reason,
    SpanAttributes['org.id'] AS org_id,
    toUUID('00000000-0000-0000-0000-000000000000') AS execution_id,
    toUUID('00000000-0000-0000-0000-000000000000') AS attempt_id,
    toUUID('00000000-0000-0000-0000-000000000000') AS allocation_id,
    '' AS provider,
    '' AS external_provider,
    toUInt64(0) AS provider_installation_id,
    toUInt64(0) AS provider_repository_id,
    toUInt64(0) AS provider_run_id,
    toUInt64(0) AS provider_run_attempt,
    toUInt64(0) AS provider_job_id,
    '' AS repository_full_name,
    '' AS workflow_name,
    '' AS job_name,
    '' AS head_branch,
    '' AS head_sha,
    '' AS runner_class,
    '' AS runner_name,
    SpanAttributes['lease.id'] AS lease_id,
    SpanAttributes['exec.id'] AS exec_id,
    '' AS correlation_id,
    toDateTime64(Timestamp, 6, 'UTC') AS started_at,
    toDateTime64(Timestamp + toIntervalNanosecond(Duration), 6, 'UTC') AS completed_at,
    toInt64(intDiv(Duration, 1000000)) AS duration_ms,
    toJSONString(SpanAttributes) AS attributes_json,
    TraceId AS trace_id,
    SpanId AS span_id
FROM default.otel_traces
WHERE ServiceName = 'vm-orchestrator'
  AND SpanAttributes['lease.id'] != ''
  AND (startsWith(SpanName, 'vmorchestrator.') OR SpanName IN ('rpc.AcquireLease', 'rpc.StartExec', 'rpc.WaitExec', 'rpc.ReleaseLease'));

CREATE VIEW verself.sandbox_execution_phase_timeline AS
WITH toUUID('00000000-0000-0000-0000-000000000000') AS zero_uuid
SELECT
    t.observed_at AS observed_at,
    t.event_source AS event_source,
    t.phase_group AS phase_group,
    t.phase_name AS phase_name,
    t.phase_order AS phase_order,
    t.result AS result,
    t.reason AS reason,
    multiIf(t.org_id != '', t.org_id, l.org_id != '', l.org_id, p.org_id) AS org_id,
    if(t.execution_id != zero_uuid, t.execution_id, if(l.execution_id != zero_uuid, l.execution_id, p.execution_id)) AS execution_id,
    if(t.attempt_id != zero_uuid, t.attempt_id, if(l.attempt_id != zero_uuid, l.attempt_id, p.attempt_id)) AS attempt_id,
    if(t.allocation_id != zero_uuid, t.allocation_id, if(l.allocation_id != zero_uuid, l.allocation_id, p.allocation_id)) AS allocation_id,
    multiIf(t.provider != '', t.provider, l.provider != '', l.provider, p.provider) AS provider,
    multiIf(t.external_provider != '', t.external_provider, l.external_provider != '', l.external_provider, p.external_provider) AS external_provider,
    if(t.provider_installation_id != 0, t.provider_installation_id, if(l.provider_installation_id != 0, l.provider_installation_id, p.provider_installation_id)) AS provider_installation_id,
    if(t.provider_repository_id != 0, t.provider_repository_id, if(l.provider_repository_id != 0, l.provider_repository_id, p.provider_repository_id)) AS provider_repository_id,
    if(t.provider_run_id != 0, t.provider_run_id, if(l.provider_run_id != 0, l.provider_run_id, p.provider_run_id)) AS provider_run_id,
    if(t.provider_run_attempt != 0, t.provider_run_attempt, if(l.provider_run_attempt != 0, l.provider_run_attempt, p.provider_run_attempt)) AS provider_run_attempt,
    if(t.provider_job_id != 0, t.provider_job_id, if(l.provider_job_id != 0, l.provider_job_id, p.provider_job_id)) AS provider_job_id,
    multiIf(t.repository_full_name != '', t.repository_full_name, l.repository_full_name != '', l.repository_full_name, p.repository_full_name) AS repository_full_name,
    multiIf(t.workflow_name != '', t.workflow_name, l.workflow_name != '', l.workflow_name, p.workflow_name) AS workflow_name,
    multiIf(t.job_name != '', t.job_name, l.job_name != '', l.job_name, p.job_name) AS job_name,
    multiIf(t.head_branch != '', t.head_branch, l.head_branch != '', l.head_branch, p.head_branch) AS head_branch,
    multiIf(t.head_sha != '', t.head_sha, l.head_sha != '', l.head_sha, p.head_sha) AS head_sha,
    multiIf(t.runner_class != '', t.runner_class, l.runner_class != '', l.runner_class, p.runner_class) AS runner_class,
    multiIf(t.runner_name != '', t.runner_name, l.runner_name != '', l.runner_name, p.runner_name) AS runner_name,
    t.lease_id AS lease_id,
    t.exec_id AS exec_id,
    multiIf(t.correlation_id != '', t.correlation_id, l.correlation_id != '', l.correlation_id, p.correlation_id) AS correlation_id,
    t.started_at AS started_at,
    t.completed_at AS completed_at,
    t.duration_ms AS duration_ms,
    t.attributes_json AS attributes_json,
    t.trace_id AS trace_id,
    t.span_id AS span_id
FROM verself.sandbox_phase_timeline AS t
LEFT JOIN
(
    SELECT
        lease_id,
        anyLast(e.org_id) AS org_id,
        anyLast(e.execution_id) AS execution_id,
        anyLast(e.attempt_id) AS attempt_id,
        anyLast(e.allocation_id) AS allocation_id,
        anyLast(e.provider) AS provider,
        anyLast(e.external_provider) AS external_provider,
        max(e.provider_installation_id) AS provider_installation_id,
        max(e.provider_repository_id) AS provider_repository_id,
        max(e.provider_run_id) AS provider_run_id,
        max(e.provider_run_attempt) AS provider_run_attempt,
        max(e.provider_job_id) AS provider_job_id,
        anyLast(e.repository_full_name) AS repository_full_name,
        anyLast(e.workflow_name) AS workflow_name,
        anyLast(e.job_name) AS job_name,
        anyLast(e.head_branch) AS head_branch,
        anyLast(e.head_sha) AS head_sha,
        anyLast(e.runner_class) AS runner_class,
        anyLast(e.runner_name) AS runner_name,
        anyLast(e.correlation_id) AS correlation_id
    FROM verself.sandbox_phase_events AS e
    WHERE e.lease_id != ''
      AND e.execution_id != toUUID('00000000-0000-0000-0000-000000000000')
    GROUP BY lease_id
) AS l ON t.lease_id = l.lease_id
LEFT JOIN
(
    SELECT
        e.provider_job_id AS provider_job_id,
        anyLast(e.org_id) AS org_id,
        anyLast(e.execution_id) AS execution_id,
        anyLast(e.attempt_id) AS attempt_id,
        anyLast(e.allocation_id) AS allocation_id,
        anyLast(e.provider) AS provider,
        anyLast(e.external_provider) AS external_provider,
        max(e.provider_installation_id) AS provider_installation_id,
        max(e.provider_repository_id) AS provider_repository_id,
        max(e.provider_run_id) AS provider_run_id,
        max(e.provider_run_attempt) AS provider_run_attempt,
        anyLast(e.repository_full_name) AS repository_full_name,
        anyLast(e.workflow_name) AS workflow_name,
        anyLast(e.job_name) AS job_name,
        anyLast(e.head_branch) AS head_branch,
        anyLast(e.head_sha) AS head_sha,
        anyLast(e.runner_class) AS runner_class,
        anyLast(e.runner_name) AS runner_name,
        anyLast(e.correlation_id) AS correlation_id
    FROM verself.sandbox_phase_events AS e
    WHERE e.provider_job_id != 0
    GROUP BY e.provider_job_id
) AS p ON t.provider_job_id = p.provider_job_id;

GRANT SELECT, INSERT ON verself.sandbox_phase_events TO sandbox_rental;
GRANT SELECT ON verself.sandbox_phase_timeline TO sandbox_rental;
GRANT SELECT ON verself.sandbox_execution_phase_timeline TO sandbox_rental;

-- ═══════════════════════════════════════════════════════════════════════════
-- verself: project analytics events
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS verself.analytics_events
(
    `event_date`              Date                                      DEFAULT toDate(observed_at) CODEC(Delta(2), ZSTD(3)),
    `observed_at`             DateTime64(6, 'UTC')                      CODEC(DoubleDelta, ZSTD(3)),
    `timestamp`               DateTime64(6, 'UTC')                      CODEC(DoubleDelta, ZSTD(3)),
    `event_id`                String                                    CODEC(ZSTD(3)),
    `org_id`                  LowCardinality(String)                    CODEC(ZSTD(3)),
    `project_id`              String                                    CODEC(ZSTD(3)),
    `dataset_id`              String                                    CODEC(ZSTD(3)),
    `environment`             LowCardinality(String)                    CODEC(ZSTD(3)),
    `signal_kind`             LowCardinality(String)                    CODEC(ZSTD(3)),
    `event_name`              LowCardinality(String)                    CODEC(ZSTD(3)),
    `source_kind`             LowCardinality(String)                    CODEC(ZSTD(3)),
    `source_subject`          String                                    CODEC(ZSTD(3)),
    `service_name`            LowCardinality(String)                    CODEC(ZSTD(3)),
    `service_version`         LowCardinality(String)                    CODEC(ZSTD(3)),
    `severity_text`           LowCardinality(String)                    CODEC(ZSTD(3)),
    `status`                  LowCardinality(String)                    CODEC(ZSTD(3)),
    `trace_id`                String                                    CODEC(ZSTD(3)),
    `span_id`                 String                                    CODEC(ZSTD(3)),
    `parent_span_id`          String                                    CODEC(ZSTD(3)),
    `duration_ms`             UInt64                                    CODEC(T64, ZSTD(3)),
    `body`                    String                                    CODEC(ZSTD(3)),
    `body_redaction_status`   LowCardinality(String)                    CODEC(ZSTD(3)),
    `string_attributes`       Map(LowCardinality(String), String)       CODEC(ZSTD(3)),
    `int_attributes`          Map(LowCardinality(String), Int64)        CODEC(ZSTD(3)),
    `float_attributes`        Map(LowCardinality(String), Float64)      CODEC(ZSTD(3)),
    `bool_attributes`         Map(LowCardinality(String), UInt8)        CODEC(ZSTD(3)),
    INDEX idx_trace_id trace_id TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_string_attr_key mapKeys(string_attributes) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_string_attr_value mapValues(string_attributes) TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_date)
ORDER BY (org_id, project_id, dataset_id, environment, signal_kind, event_name, service_name, observed_at, event_id)
TTL toDateTime(observed_at) + INTERVAL 180 DAY
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

CREATE TABLE IF NOT EXISTS verself.analytics_ingest_events
(
    `event_date`       Date                         DEFAULT toDate(recorded_at) CODEC(Delta(2), ZSTD(3)),
    `recorded_at`      DateTime64(6, 'UTC')         CODEC(DoubleDelta, ZSTD(3)),
    `org_id`           LowCardinality(String)       CODEC(ZSTD(3)),
    `project_id`       String                       CODEC(ZSTD(3)),
    `dataset_id`       String                       CODEC(ZSTD(3)),
    `environment`      LowCardinality(String)       CODEC(ZSTD(3)),
    `source_kind`      LowCardinality(String)       CODEC(ZSTD(3)),
    `source_subject`   String                       CODEC(ZSTD(3)),
    `request_kind`     LowCardinality(String)       CODEC(ZSTD(3)),
    `outcome`          LowCardinality(String)       CODEC(ZSTD(3)),
    `accepted_records` UInt32                       CODEC(T64, ZSTD(3)),
    `rejected_records` UInt32                       CODEC(T64, ZSTD(3)),
    `error_code`       LowCardinality(String)       CODEC(ZSTD(3)),
    `trace_id`         String                       CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_date)
ORDER BY (org_id, project_id, dataset_id, environment, outcome, recorded_at)
TTL toDateTime(recorded_at) + INTERVAL 90 DAY
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

CREATE TABLE IF NOT EXISTS verself.analytics_access_events
(
    `event_date`            Date                   DEFAULT toDate(recorded_at) CODEC(Delta(2), ZSTD(3)),
    `recorded_at`           DateTime64(6, 'UTC')   CODEC(DoubleDelta, ZSTD(3)),
    `org_id`                LowCardinality(String) CODEC(ZSTD(3)),
    `project_id`            String                 CODEC(ZSTD(3)),
    `dataset_id`            String                 CODEC(ZSTD(3)),
    `environment`           LowCardinality(String) CODEC(ZSTD(3)),
    `subject_type`          LowCardinality(String) CODEC(ZSTD(3)),
    `subject_id`            String                 CODEC(ZSTD(3)),
    `operation_permission`  LowCardinality(String) CODEC(ZSTD(3)),
    `resource_permission`   LowCardinality(String) CODEC(ZSTD(3)),
    `outcome`               LowCardinality(String) CODEC(ZSTD(3)),
    `result_count`          UInt32                 CODEC(T64, ZSTD(3)),
    `error_code`            LowCardinality(String) CODEC(ZSTD(3)),
    `trace_id`              String                 CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_date)
ORDER BY (org_id, project_id, dataset_id, resource_permission, outcome, recorded_at)
TTL toDateTime(recorded_at) + INTERVAL 90 DAY
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

-- ═══════════════════════════════════════════════════════════════════════════
-- verself: GitHub integration events
-- ═══════════════════════════════════════════════════════════════════════════

CREATE USER IF NOT EXISTS github_integration_service IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/github-integration-service' HOST LOCAL;
ALTER USER github_integration_service IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/github-integration-service' HOST LOCAL;

CREATE TABLE IF NOT EXISTS verself.github_integration_events
(
    observed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    event_name LowCardinality(String),
    result LowCardinality(String),
    reason String DEFAULT '' CODEC(ZSTD(3)),
    delivery_id String DEFAULT '' CODEC(ZSTD(3)),
    action LowCardinality(String) DEFAULT '',
    org_id LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    installation_binding_id UUID,
    repository_binding_id UUID,
    provider_installation_id UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    provider_repository_id UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    provider_run_id UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    provider_run_attempt UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    provider_job_id UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    repository_full_name LowCardinality(String) DEFAULT '',
    runner_id UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    runner_name String DEFAULT '' CODEC(ZSTD(3)),
    runner_class LowCardinality(String) DEFAULT '',
    job_shape_id String DEFAULT '' CODEC(ZSTD(3)),
    trust_class LowCardinality(String) DEFAULT '',
    allocation_id UUID,
    execution_id UUID,
    attempt_id UUID,
    started_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    completed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    duration_ms Int64 CODEC(Delta(8), ZSTD(3)),
    attributes_json String DEFAULT '' CODEC(ZSTD(3)),
    trace_id String DEFAULT '' CODEC(ZSTD(3)),
    span_id String DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (event_name, result, org_id, provider_repository_id, provider_run_id, provider_job_id, observed_at);

ALTER TABLE verself.github_integration_events
    ADD COLUMN IF NOT EXISTS installation_binding_id UUID AFTER org_id;

ALTER TABLE verself.github_integration_events
    ADD COLUMN IF NOT EXISTS repository_binding_id UUID AFTER installation_binding_id;

GRANT SELECT, INSERT ON verself.github_integration_events TO github_integration_service;

-- ═══════════════════════════════════════════════════════════════════════════
-- Operator observability views (database: default)
-- ═══════════════════════════════════════════════════════════════════════════
-- Raw OTel storage remains split by metric point type. These views are the
-- ergonomic query surface for CLI tools and dashboards.

DROP VIEW IF EXISTS default.otel_signal_errors;
DROP VIEW IF EXISTS default.otel_metric_catalog_live;
DROP VIEW IF EXISTS default.otel_metric_latest;
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

-- ═══════════════════════════════════════════════════════════════════════════
-- Email observability views (database: default)
-- ═══════════════════════════════════════════════════════════════════════════
-- Normalize inbound delivery attempts from Stalwart traces and email-service
-- sync/outbound lifecycle logs into one ClickHouse surface that is easy to
-- query during live debugging.

DROP VIEW IF EXISTS default.mail_events;
DROP VIEW IF EXISTS default.email_events;
DROP VIEW IF EXISTS default.mail_metrics_latest;
DROP VIEW IF EXISTS default.email_metrics_latest;

CREATE VIEW default.email_events AS
SELECT
    Timestamp,
    toDateTime(Timestamp) AS TimestampTime,
    'log' AS SourceKind,
    ServiceName AS SourceService,
    multiIf(
        Body = 'email-service: outbound accepted', 'outbound_accepted',
        Body = 'email-service: provider accepted', 'provider_accepted',
        Body = 'email-service: provider send failed', 'provider_send_failed',
        Body = 'email-service: send_as denied', 'send_as_denied',
        Body = 'email-service: email changes applied', 'email_sync_email_changes',
        Body = 'email-service: sync worker bootstrap completed', 'email_sync_bootstrap_completed',
        Body = 'email-service: sync worker eventsource connected', 'email_sync_eventsource_connected',
        'email_service_log'
    ) AS EventType,
    multiIf(
        Body IN (
            'email-service: outbound accepted',
            'email-service: provider accepted',
            'email-service: provider send failed',
            'email-service: send_as denied'
        ), 'outbound',
        'inbound'
    ) AS Direction,
    LogAttributes['org_id'] AS OrgID,
    LogAttributes['mailbox_account'] AS AccountID,
    LogAttributes['message_id'] AS MessageID,
    LogAttributes['email_id'] AS EmailID,
    '' AS QueueID,
    '' AS QueueName,
    LogAttributes['provider'] AS Provider,
    LogAttributes['provider_message_id'] AS ProviderMessageID,
    LogAttributes['from_address'] AS FromAddress,
    LogAttributes['to_address'] AS RecipientSummary,
    LogAttributes['subject'] AS Subject,
    LogAttributes['state'] AS SyncState,
    toUInt32OrZero(LogAttributes['upserted_emails']) AS UpsertedEmails,
    toUInt32OrZero(LogAttributes['destroyed_emails']) AS DestroyedEmails,
    toUInt32OrZero(LogAttributes['upserted_threads']) AS UpsertedThreads,
    toUInt32OrZero(LogAttributes['emails']) AS BootstrapEmails,
    toUInt32OrZero(LogAttributes['mailboxes']) AS BootstrapMailboxes,
    toUInt32OrZero(LogAttributes['threads']) AS BootstrapThreads,
    toUInt64(0) AS MessageSizeBytes,
    toUInt16(0) AS RecipientCount,
    TraceId,
    SpanId,
    Body AS Message,
    LogAttributes AS RawAttributes
FROM default.otel_logs
WHERE ServiceName = 'email-service'
  AND Body IN (
    'email-service: outbound accepted',
    'email-service: provider accepted',
    'email-service: provider send failed',
    'email-service: send_as denied',
    'email-service: email changes applied',
    'email-service: sync worker bootstrap completed',
    'email-service: sync worker eventsource connected'
  )

UNION ALL

SELECT
    Timestamp,
    toDateTime(Timestamp) AS TimestampTime,
    'trace' AS SourceKind,
    ServiceName AS SourceService,
    'stalwart_delivery_attempt' AS EventType,
    'inbound' AS Direction,
    '' AS OrgID,
    '' AS AccountID,
    '' AS MessageID,
    '' AS EmailID,
    SpanAttributes['queueId'] AS QueueID,
    SpanAttributes['queueName'] AS QueueName,
    'stalwart' AS Provider,
    '' AS ProviderMessageID,
    SpanAttributes['from'] AS FromAddress,
    SpanAttributes['to'] AS RecipientSummary,
    '' AS Subject,
    '' AS SyncState,
    toUInt32(0) AS UpsertedEmails,
    toUInt32(0) AS DestroyedEmails,
    toUInt32(0) AS UpsertedThreads,
    toUInt32(0) AS BootstrapEmails,
    toUInt32(0) AS BootstrapMailboxes,
    toUInt32(0) AS BootstrapThreads,
    toUInt64OrZero(SpanAttributes['size']) AS MessageSizeBytes,
    toUInt16OrZero(SpanAttributes['total']) AS RecipientCount,
    TraceId,
    SpanId,
    SpanName AS Message,
    SpanAttributes AS RawAttributes
FROM default.otel_traces
WHERE ServiceName = 'stalwart'
  AND SpanName = 'delivery.attempt-start';

CREATE VIEW default.email_metrics_latest AS
SELECT
    ServiceName,
    multiIf(
        MetricName LIKE 'message-ingest.%', 'ingest',
        MetricName LIKE 'delivery.%', 'delivery',
        MetricName LIKE 'queue.%', 'queue',
        MetricName LIKE 'smtp.%', 'smtp',
        'other'
    ) AS MetricGroup,
    MetricName,
    argMax(Value, TimeUnix) AS CurrentValue,
    max(TimeUnix) AS SampledAt
FROM default.otel_metrics_sum
WHERE ServiceName = 'stalwart'
  AND (
    MetricName LIKE 'message-ingest.%'
    OR MetricName LIKE 'delivery.%'
    OR MetricName LIKE 'queue.%'
    OR MetricName LIKE 'smtp.%'
  )
GROUP BY ServiceName, MetricGroup, MetricName;
