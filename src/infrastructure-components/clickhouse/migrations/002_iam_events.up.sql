CREATE TABLE IF NOT EXISTS verself.iam_events
(
    event_id                  String                 CODEC(ZSTD(3)),
    event_type                LowCardinality(String) CODEC(ZSTD(3)),
    event_version             UInt16 DEFAULT 1       CODEC(T64, ZSTD(3)),
    aggregate_type            LowCardinality(String) CODEC(ZSTD(3)),
    aggregate_id              String                 CODEC(ZSTD(3)),
    signup_intent_id          String DEFAULT ''      CODEC(ZSTD(3)),
    org_id                    String DEFAULT ''      CODEC(ZSTD(3)),
    identity_provider_org_id  String DEFAULT ''      CODEC(ZSTD(3)),
    identity_provider_user_id String DEFAULT ''      CODEC(ZSTD(3)),
    step                      LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    state                     LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    outcome                   LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    retryable                 UInt8 DEFAULT 0        CODEC(T64, ZSTD(3)),
    attempt                   UInt32 DEFAULT 0       CODEC(T64, ZSTD(3)),
    error_kind                LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    error_message             String DEFAULT ''      CODEC(ZSTD(3)),
    occurred_at               DateTime64(6, 'UTC')   CODEC(DoubleDelta, ZSTD(3)),
    payload                   String DEFAULT '{}'    CODEC(ZSTD(3)),
    idempotency_key_hash      String DEFAULT ''      CODEC(ZSTD(3)),
    correlation_id            String DEFAULT ''      CODEC(ZSTD(3)),
    recorded_at               DateTime64(6, 'UTC') DEFAULT now64(6) CODEC(DoubleDelta, ZSTD(3)),
    trace_id                  String DEFAULT ''      CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(toDate(recorded_at))
ORDER BY (event_type, org_id, signup_intent_id, occurred_at, event_id)
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

CREATE USER IF NOT EXISTS iam_service IDENTIFIED WITH ssl_certificate SAN 'URI:__CLICKHOUSE_SPIFFE_SERVICE_PREFIX__/iam-service' HOST LOCAL;
ALTER USER iam_service IDENTIFIED WITH ssl_certificate SAN 'URI:__CLICKHOUSE_SPIFFE_SERVICE_PREFIX__/iam-service' HOST LOCAL;
GRANT INSERT ON verself.iam_events TO iam_service;
