-- Optimize existing OTel table columns.
-- On fresh installs (tables created by 007) this is a no-op.
-- On existing installs this applies the type + codec changes.
--
-- ALTER TABLE MODIFY COLUMN is metadata-only: new inserts use the new codec
-- immediately, existing parts are rewritten on the next background merge
-- (or on explicit OPTIMIZE TABLE FINAL).

-- ─── otel_metrics_sum ────────────────────────────────────────────────────────

ALTER TABLE default.otel_metrics_sum
    MODIFY COLUMN `ResourceSchemaUrl`      LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeName`              LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeVersion`           LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeSchemaUrl`         LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `MetricName`             LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `MetricDescription`      LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `MetricUnit`             LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `StartTimeUnix`          DateTime64(9)          CODEC(DoubleDelta, ZSTD(3)),
    MODIFY COLUMN `TimeUnix`               DateTime64(9)          CODEC(DoubleDelta, ZSTD(3)),
    MODIFY COLUMN `Value`                  Float64                CODEC(Gorilla, ZSTD(3)),
    MODIFY COLUMN `Flags`                  UInt32                 CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `ScopeDroppedAttrCount`  UInt32                 CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `AggregationTemporality` Int32                  CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `IsMonotonic`            Bool                   CODEC(Delta(1), ZSTD(3)),
    MODIFY COLUMN `ResourceAttributes`           Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeAttributes`              Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    MODIFY COLUMN `Attributes`                   Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    MODIFY COLUMN `Exemplars.FilteredAttributes` Array(Map(LowCardinality(String), String)) CODEC(ZSTD(3)),
    MODIFY COLUMN `Exemplars.TimeUnix`           Array(DateTime64(9))                       CODEC(ZSTD(3)),
    MODIFY COLUMN `Exemplars.Value`              Array(Float64)                             CODEC(ZSTD(3)),
    MODIFY COLUMN `Exemplars.SpanId`             Array(String)                              CODEC(ZSTD(3)),
    MODIFY COLUMN `Exemplars.TraceId`            Array(String)                              CODEC(ZSTD(3)),
    MODIFY COLUMN `ServiceName`            LowCardinality(String) CODEC(ZSTD(3));

-- ─── otel_metrics_gauge ──────────────────────────────────────────────────────

ALTER TABLE default.otel_metrics_gauge
    MODIFY COLUMN `ResourceSchemaUrl`      LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeName`              LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeVersion`           LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeSchemaUrl`         LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `MetricName`             LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `MetricDescription`      LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `MetricUnit`             LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `StartTimeUnix`          DateTime64(9)          CODEC(DoubleDelta, ZSTD(3)),
    MODIFY COLUMN `TimeUnix`               DateTime64(9)          CODEC(DoubleDelta, ZSTD(3)),
    MODIFY COLUMN `Value`                  Float64                CODEC(Gorilla, ZSTD(3)),
    MODIFY COLUMN `Flags`                  UInt32                 CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `ScopeDroppedAttrCount`  UInt32                 CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `ResourceAttributes`           Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeAttributes`              Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    MODIFY COLUMN `Attributes`                   Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    MODIFY COLUMN `Exemplars.FilteredAttributes` Array(Map(LowCardinality(String), String)) CODEC(ZSTD(3)),
    MODIFY COLUMN `Exemplars.TimeUnix`           Array(DateTime64(9))                       CODEC(ZSTD(3)),
    MODIFY COLUMN `Exemplars.Value`              Array(Float64)                             CODEC(ZSTD(3)),
    MODIFY COLUMN `Exemplars.SpanId`             Array(String)                              CODEC(ZSTD(3)),
    MODIFY COLUMN `Exemplars.TraceId`            Array(String)                              CODEC(ZSTD(3)),
    MODIFY COLUMN `ServiceName`            LowCardinality(String) CODEC(ZSTD(3));

-- ─── otel_metrics_histogram ──────────────────────────────────────────────────

ALTER TABLE default.otel_metrics_histogram
    MODIFY COLUMN `ResourceSchemaUrl`      LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeName`              LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeVersion`           LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeSchemaUrl`         LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `MetricName`             LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `MetricDescription`      LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `MetricUnit`             LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `StartTimeUnix`          DateTime64(9)          CODEC(DoubleDelta, ZSTD(3)),
    MODIFY COLUMN `TimeUnix`               DateTime64(9)          CODEC(DoubleDelta, ZSTD(3)),
    MODIFY COLUMN `Count`                  UInt64                 CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `Sum`                    Float64                CODEC(Gorilla, ZSTD(3)),
    MODIFY COLUMN `Min`                    Float64                CODEC(Gorilla, ZSTD(3)),
    MODIFY COLUMN `Max`                    Float64                CODEC(Gorilla, ZSTD(3)),
    MODIFY COLUMN `Flags`                  UInt32                 CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `ScopeDroppedAttrCount`  UInt32                 CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `AggregationTemporality` Int32                  CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `BucketCounts`           Array(UInt64)          CODEC(ZSTD(3)),
    MODIFY COLUMN `ExplicitBounds`         Array(Float64)         CODEC(ZSTD(3)),
    MODIFY COLUMN `ResourceAttributes`           Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeAttributes`              Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    MODIFY COLUMN `Attributes`                   Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    MODIFY COLUMN `Exemplars.FilteredAttributes` Array(Map(LowCardinality(String), String)) CODEC(ZSTD(3)),
    MODIFY COLUMN `Exemplars.TimeUnix`           Array(DateTime64(9))                       CODEC(ZSTD(3)),
    MODIFY COLUMN `Exemplars.Value`              Array(Float64)                             CODEC(ZSTD(3)),
    MODIFY COLUMN `Exemplars.SpanId`             Array(String)                              CODEC(ZSTD(3)),
    MODIFY COLUMN `Exemplars.TraceId`            Array(String)                              CODEC(ZSTD(3)),
    MODIFY COLUMN `ServiceName`            LowCardinality(String) CODEC(ZSTD(3));

-- ─── otel_metrics_exponential_histogram ──────────────────────────────────────

ALTER TABLE default.otel_metrics_exponential_histogram
    MODIFY COLUMN `ResourceSchemaUrl`      LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeName`              LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeVersion`           LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeSchemaUrl`         LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `MetricName`             LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `MetricDescription`      LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `MetricUnit`             LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `StartTimeUnix`          DateTime64(9)          CODEC(DoubleDelta, ZSTD(3)),
    MODIFY COLUMN `TimeUnix`               DateTime64(9)          CODEC(DoubleDelta, ZSTD(3)),
    MODIFY COLUMN `Count`                  UInt64                 CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `Sum`                    Float64                CODEC(Gorilla, ZSTD(3)),
    MODIFY COLUMN `Scale`                  Int32                  CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `ZeroCount`              UInt64                 CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `PositiveOffset`         Int32                  CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `NegativeOffset`         Int32                  CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `Min`                    Float64                CODEC(Gorilla, ZSTD(3)),
    MODIFY COLUMN `Max`                    Float64                CODEC(Gorilla, ZSTD(3)),
    MODIFY COLUMN `Flags`                  UInt32                 CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `ScopeDroppedAttrCount`  UInt32                 CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `AggregationTemporality` Int32                  CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `PositiveBucketCounts`   Array(UInt64)          CODEC(ZSTD(3)),
    MODIFY COLUMN `NegativeBucketCounts`   Array(UInt64)          CODEC(ZSTD(3)),
    MODIFY COLUMN `ResourceAttributes`           Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeAttributes`              Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    MODIFY COLUMN `Attributes`                   Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    MODIFY COLUMN `Exemplars.FilteredAttributes` Array(Map(LowCardinality(String), String)) CODEC(ZSTD(3)),
    MODIFY COLUMN `Exemplars.TimeUnix`           Array(DateTime64(9))                       CODEC(ZSTD(3)),
    MODIFY COLUMN `Exemplars.Value`              Array(Float64)                             CODEC(ZSTD(3)),
    MODIFY COLUMN `Exemplars.SpanId`             Array(String)                              CODEC(ZSTD(3)),
    MODIFY COLUMN `Exemplars.TraceId`            Array(String)                              CODEC(ZSTD(3)),
    MODIFY COLUMN `ServiceName`            LowCardinality(String) CODEC(ZSTD(3));

-- ─── otel_metrics_summary ────────────────────────────────────────────────────

ALTER TABLE default.otel_metrics_summary
    MODIFY COLUMN `ResourceSchemaUrl`      LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeName`              LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeVersion`           LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeSchemaUrl`         LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `MetricName`             LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `MetricDescription`      LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `MetricUnit`             LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `StartTimeUnix`          DateTime64(9)          CODEC(DoubleDelta, ZSTD(3)),
    MODIFY COLUMN `TimeUnix`               DateTime64(9)          CODEC(DoubleDelta, ZSTD(3)),
    MODIFY COLUMN `Count`                  UInt64                 CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `Sum`                    Float64                CODEC(Gorilla, ZSTD(3)),
    MODIFY COLUMN `Flags`                  UInt32                 CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `ScopeDroppedAttrCount`  UInt32                 CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `ValueAtQuantiles.Quantile` Array(Float64)      CODEC(ZSTD(3)),
    MODIFY COLUMN `ValueAtQuantiles.Value`    Array(Float64)      CODEC(ZSTD(3)),
    MODIFY COLUMN `ResourceAttributes`           Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeAttributes`              Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    MODIFY COLUMN `Attributes`                   Map(LowCardinality(String), String)        CODEC(ZSTD(3)),
    MODIFY COLUMN `ServiceName`            LowCardinality(String) CODEC(ZSTD(3));

-- ─── otel_logs ───────────────────────────────────────────────────────────────

ALTER TABLE default.otel_logs
    MODIFY COLUMN `Timestamp`              DateTime64(9)          CODEC(DoubleDelta, ZSTD(3)),
    MODIFY COLUMN `TraceId`                String                 CODEC(ZSTD(3)),
    MODIFY COLUMN `SpanId`                 String                 CODEC(ZSTD(3)),
    MODIFY COLUMN `TraceFlags`             UInt8                  CODEC(ZSTD(3)),
    MODIFY COLUMN `SeverityText`           LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `SeverityNumber`         UInt8                  CODEC(ZSTD(3)),
    MODIFY COLUMN `ServiceName`            LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `Body`                   String                 CODEC(ZSTD(3)),
    MODIFY COLUMN `ResourceSchemaUrl`      LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ResourceAttributes`     Map(LowCardinality(String), String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeSchemaUrl`         LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeName`              LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeVersion`           LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeAttributes`        Map(LowCardinality(String), String) CODEC(ZSTD(3)),
    MODIFY COLUMN `LogAttributes`          Map(LowCardinality(String), String) CODEC(ZSTD(3));

-- ─── otel_traces ─────────────────────────────────────────────────────────────

ALTER TABLE default.otel_traces
    MODIFY COLUMN `Timestamp`              DateTime64(9)          CODEC(DoubleDelta, ZSTD(3)),
    MODIFY COLUMN `TraceId`                String                 CODEC(ZSTD(3)),
    MODIFY COLUMN `SpanId`                 String                 CODEC(ZSTD(3)),
    MODIFY COLUMN `ParentSpanId`           String                 CODEC(ZSTD(3)),
    MODIFY COLUMN `TraceState`             String                 CODEC(ZSTD(3)),
    MODIFY COLUMN `SpanName`               LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `SpanKind`               LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ServiceName`            LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ResourceAttributes`     Map(LowCardinality(String), String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeName`              LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `ScopeVersion`           LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `SpanAttributes`         Map(LowCardinality(String), String) CODEC(ZSTD(3)),
    MODIFY COLUMN `Duration`               UInt64                 CODEC(T64, ZSTD(3)),
    MODIFY COLUMN `StatusCode`             LowCardinality(String) CODEC(ZSTD(3)),
    MODIFY COLUMN `StatusMessage`          String                 CODEC(ZSTD(3)),
    MODIFY COLUMN `Events.Timestamp`       Array(DateTime64(9))                       CODEC(ZSTD(3)),
    MODIFY COLUMN `Events.Name`            Array(LowCardinality(String))              CODEC(ZSTD(3)),
    MODIFY COLUMN `Events.Attributes`      Array(Map(LowCardinality(String), String)) CODEC(ZSTD(3)),
    MODIFY COLUMN `Links.TraceId`          Array(String)                              CODEC(ZSTD(3)),
    MODIFY COLUMN `Links.SpanId`           Array(String)                              CODEC(ZSTD(3)),
    MODIFY COLUMN `Links.TraceState`       Array(String)                              CODEC(ZSTD(3)),
    MODIFY COLUMN `Links.Attributes`       Array(Map(LowCardinality(String), String)) CODEC(ZSTD(3));

-- ─── otel_traces_trace_id_ts ─────────────────────────────────────────────────

ALTER TABLE default.otel_traces_trace_id_ts
    MODIFY COLUMN `TraceId` String   CODEC(ZSTD(3)),
    MODIFY COLUMN `Start`   DateTime CODEC(DoubleDelta, ZSTD(3)),
    MODIFY COLUMN `End`     DateTime CODEC(DoubleDelta, ZSTD(3));
