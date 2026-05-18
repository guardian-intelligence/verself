ALTER TABLE verself.vm_lease_evidence
    ADD COLUMN IF NOT EXISTS `org_id` String CODEC(ZSTD(3)) AFTER `service_name`;

ALTER TABLE verself.vm_lease_evidence
    ADD COLUMN IF NOT EXISTS `storage_key_version` String CODEC(ZSTD(3)) AFTER `org_id`;

ALTER TABLE verself.vm_lease_evidence
    ADD COLUMN IF NOT EXISTS `storage_key_ref_count` UInt32 CODEC(T64, ZSTD(3)) AFTER `storage_key_version`;

ALTER TABLE verself.vm_lease_evidence
    ADD COLUMN IF NOT EXISTS `image_ref` String CODEC(ZSTD(3)) AFTER `storage_key_ref_count`;

ALTER TABLE verself.vm_lease_evidence
    ADD COLUMN IF NOT EXISTS `zfs_dataset` String CODEC(ZSTD(3)) AFTER `image_ref`;

ALTER TABLE verself.vm_lease_evidence
    ADD COLUMN IF NOT EXISTS `source_snapshot_ref` String CODEC(ZSTD(3)) AFTER `zfs_dataset`;

ALTER TABLE verself.vm_lease_evidence
    ADD COLUMN IF NOT EXISTS `target_snapshot_ref` String CODEC(ZSTD(3)) AFTER `source_snapshot_ref`;

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
WHERE ServiceName = 'vm-orchestrator';
