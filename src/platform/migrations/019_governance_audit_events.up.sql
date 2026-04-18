CREATE TABLE IF NOT EXISTS forge_metal.audit_events
(
    recorded_at DateTime64(6, 'UTC'),
    event_date Date DEFAULT toDate(recorded_at),

    event_id UUID,
    org_id LowCardinality(String),
    service_name LowCardinality(String),
    operation_id LowCardinality(String),
    audit_event LowCardinality(String),

    principal_type LowCardinality(String),
    principal_id String,
    principal_email String,

    permission LowCardinality(String),
    resource_kind LowCardinality(String),
    resource_id String,
    action LowCardinality(String),
    org_scope LowCardinality(String),
    rate_limit_class LowCardinality(String),

    result LowCardinality(String),
    error_code LowCardinality(String),
    error_message String,
    client_ip String,
    user_agent_hash String,
    idempotency_key_hash String,
    request_id String,
    trace_id String,

    payload_json String,
    content_sha256 String,
    sequence UInt64,
    prev_hmac String,
    row_hmac String
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_date)
ORDER BY (org_id, event_date, service_name, result, operation_id, recorded_at, event_id);
