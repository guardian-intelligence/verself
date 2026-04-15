-- vm-orchestrator lease evidence:
-- Typed projection of lease lifecycle, exec starts, and telemetry diagnostics.

DROP VIEW IF EXISTS forge_metal.vm_run_evidence_mv;
DROP TABLE IF EXISTS forge_metal.vm_run_evidence;
DROP VIEW IF EXISTS forge_metal.vm_lease_evidence_mv;

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
