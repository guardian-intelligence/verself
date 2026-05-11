DROP TABLE IF EXISTS verself.analytics_access_events;
DROP TABLE IF EXISTS verself.analytics_ingest_events;
DROP TABLE IF EXISTS verself.analytics_events;

CREATE TABLE verself.analytics_events
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

CREATE TABLE verself.analytics_ingest_events
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

CREATE TABLE verself.analytics_access_events
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
