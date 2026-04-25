-- ClickHouse initial schema for forge-metal.
--
-- The ansible clickhouse role applies this file with --database forge_metal,
-- so bare table names land in forge_metal; OTel-compatible tables use fully
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
-- Caddy (filelog), console (OTLP), and Zitadel (journald).
--
-- Attribute name normalization per service:
--   status:    caddy=http_status, console=http_status_code, zitadel=status
--   path:      caddy=http_uri,    console=http_target,      zitadel=path
--   host:      caddy=http_host,   console=(none),           zitadel=instance_host
--   client_ip: caddy=client_ip,   console=forwarded_for,    zitadel=(none)
--   duration:  caddy=duration_s,  console=duration_ms,      zitadel=duration (ns)

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
-- forge_metal: sandbox execution logs and wide events
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS forge_metal.job_logs
(
    execution_id        UUID,
    attempt_id          UUID,
    org_id              UInt64,
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

CREATE TABLE IF NOT EXISTS forge_metal.job_events
(
    execution_id            UUID,
    attempt_id              UUID,
    org_id                  UInt64,
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
    github_installation_id  UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    github_run_id           UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    github_job_id           UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
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

CREATE TABLE IF NOT EXISTS forge_metal.job_cache_events
(
    event_time                 DateTime64(9, 'UTC')    CODEC(Delta(8), ZSTD(3)),
    org_id                     UInt64,
    event_name                 LowCardinality(String),
    repository_full_name       LowCardinality(String) DEFAULT '',
    runner_class               LowCardinality(String) DEFAULT '',
    checkout_cache_hit         UInt8 DEFAULT 0         CODEC(T64, ZSTD(3)),
    sticky_restore_hit_count   UInt32 DEFAULT 0        CODEC(T64, ZSTD(3)),
    sticky_restore_miss_count  UInt32 DEFAULT 0        CODEC(T64, ZSTD(3)),
    sticky_state               LowCardinality(String) DEFAULT '',
    trace_id                   String DEFAULT ''       CODEC(ZSTD(3)),
    span_id                    String DEFAULT ''       CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_time)
ORDER BY (org_id, event_name, repository_full_name, runner_class, event_time, trace_id)
TTL toDateTime(event_time) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;

DROP VIEW IF EXISTS forge_metal.job_cache_events_mv;

CREATE MATERIALIZED VIEW forge_metal.job_cache_events_mv
TO forge_metal.job_cache_events
AS
SELECT
    Timestamp AS event_time,
    toUInt64OrZero(SpanAttributes['forge_metal.org_id']) AS org_id,
    SpanName AS event_name,
    SpanAttributes['github.repository'] AS repository_full_name,
    SpanAttributes['github.runner_class'] AS runner_class,
    toUInt8(SpanAttributes['github.checkout.cache_hit'] = 'true') AS checkout_cache_hit,
    toUInt32OrZero(SpanAttributes['github.stickydisk.restore_hit_count']) AS sticky_restore_hit_count,
    toUInt32OrZero(SpanAttributes['github.stickydisk.restore_miss_count']) AS sticky_restore_miss_count,
    SpanAttributes['github.stickydisk.state'] AS sticky_state,
    TraceId AS trace_id,
    SpanId AS span_id
FROM default.otel_traces
WHERE ServiceName = 'sandbox-rental-service'
  AND SpanName IN (
    'github.checkout.bundle',
    'github.stickydisk.compile',
    'github.stickydisk.save_request',
    'github.stickydisk.commit_zfs'
  )
  AND SpanAttributes['forge_metal.org_id'] != '';

-- ═══════════════════════════════════════════════════════════════════════════
-- forge_metal: billing ledger and windowed metering
-- ═══════════════════════════════════════════════════════════════════════════

DROP TABLE IF EXISTS forge_metal.metering;

CREATE TABLE forge_metal.metering (
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

DROP TABLE IF EXISTS forge_metal.billing_events;

CREATE TABLE forge_metal.billing_events (
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
-- forge_metal: notification delivery ledger
-- ═══════════════════════════════════════════════════════════════════════════

DROP TABLE IF EXISTS forge_metal.notification_events;

CREATE TABLE forge_metal.notification_events
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

-- ═══════════════════════════════════════════════════════════════════════════
-- forge_metal: domain update ledger
-- ═══════════════════════════════════════════════════════════════════════════

DROP TABLE IF EXISTS forge_metal.domain_update_ledger;

CREATE TABLE forge_metal.domain_update_ledger
(
    recorded_at DateTime64(9, 'UTC') CODEC(Delta(8), ZSTD(3)),
    occurred_at DateTime64(9, 'UTC') CODEC(Delta(8), ZSTD(3)),
    schema_version LowCardinality(String) CODEC(ZSTD(3)),
    event_id UUID,
    event_type LowCardinality(String) CODEC(ZSTD(3)),
    service_name LowCardinality(String) CODEC(ZSTD(3)),
    org_id LowCardinality(String) CODEC(ZSTD(3)),
    actor_id String CODEC(ZSTD(3)),
    operation_id LowCardinality(String) CODEC(ZSTD(3)),
    command_id UUID,
    idempotency_key_hash FixedString(64) CODEC(ZSTD(3)),
    aggregate_kind LowCardinality(String) CODEC(ZSTD(3)),
    aggregate_id String CODEC(ZSTD(3)),
    aggregate_version UInt32 CODEC(T64, ZSTD(3)),
    target_kind LowCardinality(String) CODEC(ZSTD(3)),
    target_id String CODEC(ZSTD(3)),
    result LowCardinality(String) CODEC(ZSTD(3)),
    reason LowCardinality(String) CODEC(ZSTD(3)),
    conflict_policy LowCardinality(String) CODEC(ZSTD(3)),
    expected_version UInt32 CODEC(T64, ZSTD(3)),
    actual_version UInt32 CODEC(T64, ZSTD(3)),
    expected_hash FixedString(64) CODEC(ZSTD(3)),
    actual_hash FixedString(64) CODEC(ZSTD(3)),
    requested_hash FixedString(64) CODEC(ZSTD(3)),
    changed_fields Array(LowCardinality(String)) CODEC(ZSTD(3)),
    payload_json String CODEC(ZSTD(3)),
    trace_id String CODEC(ZSTD(3)),
    span_id String CODEC(ZSTD(3)),
    traceparent String CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(toDate(recorded_at))
ORDER BY (service_name, event_type, org_id, aggregate_kind, result, occurred_at, event_id)
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

-- ═══════════════════════════════════════════════════════════════════════════
-- forge_metal: vm-orchestrator lease evidence projection
-- ═══════════════════════════════════════════════════════════════════════════
-- Typed projection of lease lifecycle, exec starts, and telemetry diagnostics.

CREATE TABLE IF NOT EXISTS forge_metal.vm_lease_evidence
(
    `evidence_time`                 DateTime64(9)            CODEC(Delta(8), ZSTD(3)),
    `evidence_date`                 Date                      DEFAULT toDate(evidence_time),
    `service_name`                  LowCardinality(String)   CODEC(ZSTD(3)),
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

DROP VIEW IF EXISTS forge_metal.vm_lease_evidence_mv;

CREATE MATERIALIZED VIEW forge_metal.vm_lease_evidence_mv
TO forge_metal.vm_lease_evidence
AS
SELECT
    Timestamp AS evidence_time,
    ServiceName AS service_name,
    LogAttributes['lease_id'] AS lease_id,
    LogAttributes['exec_id'] AS exec_id,
    multiIf(
        Body = 'lease ready', 'lease_ready',
        Body = 'guest exec started', 'exec_started',
        Body = 'guest telemetry hello received', 'telemetry_hello',
        Body = 'guest telemetry stream diagnostic', 'telemetry_diagnostic',
        Body = 'lease runtime cleaned up', 'lease_cleanup',
        Body = 'checkpoint snapshot saved', 'checkpoint_saved',
        'other'
    ) AS evidence_type,
    if(Body = 'guest telemetry stream diagnostic', LogAttributes['kind'], '') AS diagnostic_kind,
    multiIf(
        Body = 'guest telemetry stream diagnostic', 'telemetry_diagnostic',
        Body = 'guest telemetry hello received', 'telemetry_hello',
        Body = 'lease ready', 'lease_ready',
        Body = 'guest exec started', 'exec_started',
        Body = 'lease runtime cleaned up', 'lease_cleanup',
        Body = 'checkpoint snapshot saved', 'checkpoint_saved',
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
    'checkpoint snapshot saved'
  )
  AND LogAttributes['lease_id'] != '';

-- ═══════════════════════════════════════════════════════════════════════════
-- forge_metal: governance audit events (OCSF-aligned append-only ledger)
-- ═══════════════════════════════════════════════════════════════════════════

DROP TABLE IF EXISTS forge_metal.audit_events;

CREATE TABLE forge_metal.audit_events
(
    recorded_at DateTime64(6, 'UTC'),
    event_date Date DEFAULT toDate(recorded_at),
    ingested_at DateTime64(6, 'UTC') DEFAULT now64(6, 'UTC'),

    schema_version LowCardinality(String),
    event_id UUID,
    org_id LowCardinality(String),
    environment LowCardinality(String),
    source_product_area LowCardinality(String),
    service_name LowCardinality(String),
    service_version LowCardinality(String),
    writer_instance_id String,

    request_id String,
    trace_id String,
    span_id String,
    parent_span_id String,
    route_template String,
    http_method LowCardinality(String),
    http_status UInt16,
    duration_ms Float64,
    idempotency_key_hash String,

    actor_type LowCardinality(String),
    actor_id String,
    actor_display String,
    actor_org_id String,
    actor_owner_id String,
    actor_owner_display String,
    credential_id String,
    credential_name String,
    credential_fingerprint String,
    auth_method LowCardinality(String),
    auth_assurance_level LowCardinality(String),
    mfa_present UInt8,
    session_id_hash String,
    delegation_chain String,
    actor_spiffe_id String,

    operation_id LowCardinality(String),
    audit_event LowCardinality(String),
    operation_display String,
    operation_type LowCardinality(String),
    event_category LowCardinality(String),
    risk_level LowCardinality(String),
    data_classification LowCardinality(String),
    rate_limit_class LowCardinality(String),

    target_kind LowCardinality(String),
    target_id String,
    target_display String,
    target_scope LowCardinality(String),
    target_path_hash String,
    resource_owner_org_id String,
    resource_region LowCardinality(String),

    permission LowCardinality(String),
    action LowCardinality(String),
    org_scope LowCardinality(String),
    policy_id String,
    policy_version String,
    policy_hash String,
    matched_rule String,
    decision LowCardinality(String),
    result LowCardinality(String),
    denial_reason LowCardinality(String),
    trust_class LowCardinality(String),

    client_ip String,
    client_ip_version LowCardinality(String),
    client_ip_hash String,
    ip_chain String,
    ip_chain_trusted_hops UInt8,
    user_agent_raw String,
    user_agent_hash String,
    referer_origin String,
    origin String,
    host String,
    tls_subject_hash String,
    mtls_subject_hash String,

    geo_country LowCardinality(String),
    geo_region String,
    geo_city String,
    asn UInt32,
    asn_org String,
    network_type LowCardinality(String),
    geo_source String,
    geo_source_version String,

    changed_fields String,
    before_hash String,
    after_hash String,
    content_sha256 String,
    artifact_sha256 String,
    artifact_bytes UInt64,
    error_code LowCardinality(String),
    error_class LowCardinality(String),
    error_message String,

    secret_mount String,
    secret_path_hash String,
    secret_version UInt64,
    secret_operation LowCardinality(String),
    lease_id_hash String,
    lease_ttl_seconds UInt64,
    key_id String,
    openbao_request_id String,
    openbao_accessor_hash String,

    payload_json String,
    sequence UInt64,
    prev_hmac String,
    row_hmac String,
    hmac_key_id String,
    retention_class LowCardinality(String),
    legal_hold UInt8
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_date)
ORDER BY (org_id, event_date, risk_level, operation_type, source_product_area, result, recorded_at, sequence, event_id)
SETTINGS index_granularity = 8192;

-- ═══════════════════════════════════════════════════════════════════════════
-- forge_metal: object-storage access events
-- ═══════════════════════════════════════════════════════════════════════════

DROP TABLE IF EXISTS forge_metal.object_access_events;

CREATE TABLE forge_metal.object_access_events
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
-- Mail observability views (database: default)
-- ═══════════════════════════════════════════════════════════════════════════
-- Normalize inbound delivery attempts from Stalwart traces and mailbox-service
-- sync/forwarder lifecycle logs into one ClickHouse surface that is easy to
-- query during live debugging.

DROP VIEW IF EXISTS default.mail_events;
DROP VIEW IF EXISTS default.mail_metrics_latest;

CREATE VIEW default.mail_events AS
SELECT
    Timestamp,
    toDateTime(Timestamp) AS TimestampTime,
    'log' AS SourceKind,
    ServiceName AS SourceService,
    multiIf(
        Body = 'mailbox-service: email changes applied', 'mailbox_sync_email_changes',
        Body = 'mailbox-service: forwarded email', 'operator_forwarded_email',
        Body = 'mailbox-service: operator forwarder skipped self-generated message', 'operator_forwarder_skip',
        Body = 'mailbox-service: sync worker bootstrap completed', 'mailbox_sync_bootstrap_completed',
        Body = 'mailbox-service: sync worker eventsource connected', 'mailbox_sync_eventsource_connected',
        'mailbox_service_log'
    ) AS EventType,
    multiIf(
        Body = 'mailbox-service: forwarded email', 'outbound',
        Body = 'mailbox-service: operator forwarder skipped self-generated message', 'outbound',
        'inbound'
    ) AS Direction,
    LogAttributes['mailbox_account'] AS MailboxAccount,
    LogAttributes['email_id'] AS EmailID,
    '' AS QueueID,
    '' AS QueueName,
    LogAttributes['resend_id'] AS ExternalID,
    '' AS Sender,
    LogAttributes['subject'] AS Subject,
    '' AS RecipientSummary,
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
WHERE ServiceName = 'mailbox-service'
  AND Body IN (
    'mailbox-service: email changes applied',
    'mailbox-service: forwarded email',
    'mailbox-service: operator forwarder skipped self-generated message',
    'mailbox-service: sync worker bootstrap completed',
    'mailbox-service: sync worker eventsource connected'
  )

UNION ALL

SELECT
    Timestamp,
    toDateTime(Timestamp) AS TimestampTime,
    'trace' AS SourceKind,
    ServiceName AS SourceService,
    'stalwart_delivery_attempt' AS EventType,
    'inbound' AS Direction,
    '' AS MailboxAccount,
    '' AS EmailID,
    SpanAttributes['queueId'] AS QueueID,
    SpanAttributes['queueName'] AS QueueName,
    '' AS ExternalID,
    SpanAttributes['from'] AS Sender,
    '' AS Subject,
    SpanAttributes['to'] AS RecipientSummary,
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

CREATE VIEW default.mail_metrics_latest AS
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
