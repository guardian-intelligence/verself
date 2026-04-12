-- vm-orchestrator run evidence:
-- Typed projection of run lifecycle + telemetry diagnostics from default.otel_logs.
-- This avoids proof/CI coupling to free-form log body text.

CREATE TABLE IF NOT EXISTS forge_metal.vm_run_evidence
(
    `evidence_time`                 DateTime64(9)            CODEC(Delta(8), ZSTD(3)),
    `evidence_date`                 Date                      DEFAULT toDate(evidence_time),
    `service_name`                  LowCardinality(String)   CODEC(ZSTD(3)),
    `run_id`                        String                    CODEC(ZSTD(3)),
    `evidence_type`                 LowCardinality(String)   CODEC(ZSTD(3)),
    `diagnostic_kind`               LowCardinality(String)   CODEC(ZSTD(3)),
    `from_state`                    LowCardinality(String)   CODEC(ZSTD(3)),
    `to_state`                      LowCardinality(String)   CODEC(ZSTD(3)),
    `reason_code`                   LowCardinality(String)   CODEC(ZSTD(3)),
    `protocol_state`                LowCardinality(String)   CODEC(ZSTD(3)),
    `reason`                        String                    CODEC(ZSTD(3)),
    `expected_seq`                  UInt32                    CODEC(T64, ZSTD(3)),
    `observed_seq`                  UInt32                    CODEC(T64, ZSTD(3)),
    `missing_samples`               UInt32                    CODEC(T64, ZSTD(3)),
    `host_received_unix_nano`       UInt64                    CODEC(T64, ZSTD(3)),
    `telemetry_received_unix_nano`  UInt64                    CODEC(T64, ZSTD(3)),
    `trace_id`                      String                    CODEC(ZSTD(3)),
    `span_id`                       String                    CODEC(ZSTD(3)),
    INDEX idx_run_id run_id TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY evidence_date
ORDER BY (evidence_type, diagnostic_kind, reason_code, to_state, run_id, evidence_time)
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

DROP VIEW IF EXISTS forge_metal.vm_run_evidence_mv;

CREATE MATERIALIZED VIEW forge_metal.vm_run_evidence_mv
TO forge_metal.vm_run_evidence
AS
SELECT
    Timestamp AS evidence_time,
    ServiceName AS service_name,
    LogAttributes['run_id'] AS run_id,
    multiIf(
        Body = 'guest telemetry hello received', 'telemetry_hello',
        Body = 'guest telemetry stream diagnostic', 'telemetry_diagnostic',
        Body = 'host run state transition', 'run_state_transition',
        'other'
    ) AS evidence_type,
    if(Body = 'guest telemetry stream diagnostic', LogAttributes['kind'], '') AS diagnostic_kind,
    if(Body = 'host run state transition', LogAttributes['from_state'], '') AS from_state,
    if(Body = 'host run state transition', LogAttributes['to_state'], '') AS to_state,
    multiIf(
        Body = 'guest telemetry stream diagnostic', 'telemetry_diagnostic',
        Body = 'guest telemetry hello received', 'telemetry_hello',
        Body = 'host run state transition' AND startsWith(LogAttributes['reason'], 'guest control protocol violation in '), 'guest_protocol_violation',
        Body = 'host run state transition' AND LogAttributes['to_state'] = 'failed', 'run_failed',
        Body = 'host run state transition' AND LogAttributes['to_state'] = 'succeeded', 'run_succeeded',
        Body = 'host run state transition' AND LogAttributes['to_state'] = 'running', 'run_running',
        Body = 'host run state transition' AND LogAttributes['to_state'] = 'pending', 'run_accepted',
        'other'
    ) AS reason_code,
    if(
        Body = 'host run state transition' AND startsWith(LogAttributes['reason'], 'guest control protocol violation in '),
        extract(LogAttributes['reason'], 'guest control protocol violation in ([^:]+):'),
        ''
    ) AS protocol_state,
    if(Body = 'host run state transition', LogAttributes['reason'], '') AS reason,
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
    'guest telemetry hello received',
    'guest telemetry stream diagnostic',
    'host run state transition'
  )
  AND LogAttributes['run_id'] != '';
