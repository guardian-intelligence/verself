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
