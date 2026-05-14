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
