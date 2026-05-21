CREATE TABLE IF NOT EXISTS verself.sandbox_phase_events
(
    observed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    event_source LowCardinality(String),
    phase_group LowCardinality(String),
    phase_name LowCardinality(String),
    phase_order UInt16 CODEC(T64, ZSTD(3)),
    result LowCardinality(String),
    reason String DEFAULT '' CODEC(ZSTD(3)),
    org_id LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    execution_id UUID,
    attempt_id UUID,
    allocation_id UUID,
    provider LowCardinality(String) DEFAULT '',
    external_provider LowCardinality(String) DEFAULT '',
    provider_installation_id UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    provider_repository_id UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    provider_run_id UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    provider_run_attempt UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    provider_job_id UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    repository_full_name LowCardinality(String) DEFAULT '',
    workflow_name LowCardinality(String) DEFAULT '',
    job_name LowCardinality(String) DEFAULT '',
    head_branch LowCardinality(String) DEFAULT '',
    head_sha String DEFAULT '' CODEC(ZSTD(3)),
    runner_class LowCardinality(String) DEFAULT '',
    runner_name String DEFAULT '' CODEC(ZSTD(3)),
    lease_id String DEFAULT '' CODEC(ZSTD(3)),
    exec_id String DEFAULT '' CODEC(ZSTD(3)),
    correlation_id String DEFAULT '' CODEC(ZSTD(3)),
    started_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    completed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    duration_ms Int64 CODEC(Delta(8), ZSTD(3)),
    attributes_json String DEFAULT '' CODEC(ZSTD(3)),
    trace_id String DEFAULT '' CODEC(ZSTD(3)),
    span_id String DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (phase_group, phase_name, result, provider, org_id, provider_repository_id, provider_run_id, provider_job_id, execution_id, started_at)
TTL toDateTime(observed_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;

DROP VIEW IF EXISTS verself.sandbox_execution_phase_timeline;
DROP VIEW IF EXISTS verself.sandbox_phase_timeline;

CREATE VIEW verself.sandbox_phase_timeline AS
SELECT
    observed_at,
    event_source,
    phase_group,
    phase_name,
    phase_order,
    result,
    reason,
    org_id,
    execution_id,
    attempt_id,
    allocation_id,
    provider,
    external_provider,
    provider_installation_id,
    provider_repository_id,
    provider_run_id,
    provider_run_attempt,
    provider_job_id,
    repository_full_name,
    workflow_name,
    job_name,
    head_branch,
    head_sha,
    runner_class,
    runner_name,
    lease_id,
    exec_id,
    correlation_id,
    started_at,
    completed_at,
    duration_ms,
    attributes_json,
    trace_id,
    span_id
FROM verself.sandbox_phase_events

UNION ALL

SELECT
    toDateTime64(Timestamp, 6, 'UTC') AS observed_at,
    'vm-orchestrator-otel' AS event_source,
    multiIf(
        startsWith(SpanName, 'vmorchestrator.guest.boot.'), 'vm.guest.boot',
        startsWith(SpanName, 'vmorchestrator.guest.kernel_'), 'vm.guest.kernel',
        startsWith(SpanName, 'vmorchestrator.guest.exec_'), 'vm.guest.exec',
        startsWith(SpanName, 'vmorchestrator.guest.'), 'vm.guest',
        startsWith(SpanName, 'vmorchestrator.firecracker.'), 'vm.firecracker',
        startsWith(SpanName, 'vmorchestrator.zfs.'), 'vm.zfs',
        startsWith(SpanName, 'vmorchestrator.zvol.'), 'vm.zvol',
        startsWith(SpanName, 'vmorchestrator.network.'), 'vm.network',
        startsWith(SpanName, 'vmorchestrator.jailer.'), 'vm.jailer',
        startsWith(SpanName, 'vmorchestrator.jail.'), 'vm.jail',
        SpanName = 'vmorchestrator.lease.boot', 'vm.lease',
        startsWith(SpanName, 'rpc.'), 'vm.rpc',
        'vm.orchestrator'
    ) AS phase_group,
    multiIf(
        SpanName = 'rpc.AcquireLease', 'vm.rpc.acquire_lease',
        SpanName = 'rpc.StartExec', 'vm.rpc.start_exec',
        SpanName = 'rpc.WaitExec', 'vm.rpc.wait_exec',
        SpanName = 'rpc.ReleaseLease', 'vm.rpc.release_lease',
        concat('vm.', replaceOne(SpanName, 'vmorchestrator.', ''))
    ) AS phase_name,
    toUInt16(multiIf(
        SpanName = 'rpc.AcquireLease', 88,
        SpanName = 'vmorchestrator.lease.boot', 91,
        SpanName = 'vmorchestrator.org_runtime.require_ready_check', 92,
        SpanName = 'vmorchestrator.zfs.root_clone', 93,
        SpanName = 'vmorchestrator.zfs.root_resize_ext4', 94,
        SpanName = 'vmorchestrator.zfs.mounts_prepare', 95,
        SpanName = 'vmorchestrator.zfs.mount_prepare', 96,
        SpanName = 'vmorchestrator.zvol.wait_device', 97,
        SpanName = 'vmorchestrator.zvol.mount_wait_device', 98,
        SpanName = 'vmorchestrator.jail.setup', 99,
        SpanName = 'vmorchestrator.network.setup', 100,
        SpanName = 'vmorchestrator.jailer.start', 101,
        SpanName = 'vmorchestrator.firecracker.api_socket_wait', 102,
        SpanName = 'vmorchestrator.firecracker.golden_snapshot_lookup', 103,
        SpanName = 'vmorchestrator.firecracker.snapshot_stage', 104,
        SpanName = 'vmorchestrator.firecracker.restore_metrics', 105,
        SpanName = 'vmorchestrator.firecracker.snapshot_load', 106,
        SpanName = 'vmorchestrator.firecracker.snapshot_resume', 107,
        SpanName = 'vmorchestrator.firecracker.configure_all', 108,
        SpanName = 'vmorchestrator.firecracker.configure', 109,
        SpanName = 'vmorchestrator.firecracker.instance_start', 110,
        SpanName = 'vmorchestrator.guest.control_socket_wait', 111,
        SpanName = 'vmorchestrator.guest.control_connect', 112,
        SpanName = 'vmorchestrator.guest.after_restore', 113,
        SpanName = 'vmorchestrator.guest.hello', 114,
        SpanName = 'vmorchestrator.guest.lease_init', 115,
        startsWith(SpanName, 'vmorchestrator.guest.kernel_'), 116,
        SpanName = 'vmorchestrator.guest.boot_report', 117,
        startsWith(SpanName, 'vmorchestrator.guest.boot.'), 118,
        SpanName = 'rpc.StartExec', 121,
        SpanName = 'vmorchestrator.guest.exec_dispatch', 122,
        SpanName = 'vmorchestrator.guest.exec_workload', 170,
        SpanName = 'vmorchestrator.guest.exec_teardown', 181,
        SpanName = 'rpc.WaitExec', 191,
        SpanName = 'rpc.ReleaseLease', 241,
        1000
    )) AS phase_order,
    if(StatusCode IN ('Error', 'STATUS_CODE_ERROR'), 'failed', 'succeeded') AS result,
    StatusMessage AS reason,
    SpanAttributes['org.id'] AS org_id,
    toUUID('00000000-0000-0000-0000-000000000000') AS execution_id,
    toUUID('00000000-0000-0000-0000-000000000000') AS attempt_id,
    toUUID('00000000-0000-0000-0000-000000000000') AS allocation_id,
    '' AS provider,
    '' AS external_provider,
    toUInt64(0) AS provider_installation_id,
    toUInt64(0) AS provider_repository_id,
    toUInt64(0) AS provider_run_id,
    toUInt64(0) AS provider_run_attempt,
    toUInt64(0) AS provider_job_id,
    '' AS repository_full_name,
    '' AS workflow_name,
    '' AS job_name,
    '' AS head_branch,
    '' AS head_sha,
    '' AS runner_class,
    '' AS runner_name,
    SpanAttributes['lease.id'] AS lease_id,
    SpanAttributes['exec.id'] AS exec_id,
    '' AS correlation_id,
    toDateTime64(Timestamp, 6, 'UTC') AS started_at,
    toDateTime64(Timestamp + toIntervalNanosecond(Duration), 6, 'UTC') AS completed_at,
    toInt64(intDiv(Duration, 1000000)) AS duration_ms,
    toJSONString(SpanAttributes) AS attributes_json,
    TraceId AS trace_id,
    SpanId AS span_id
FROM default.otel_traces
WHERE ServiceName = 'vm-orchestrator'
  AND SpanAttributes['lease.id'] != ''
  AND (startsWith(SpanName, 'vmorchestrator.') OR SpanName IN ('rpc.AcquireLease', 'rpc.StartExec', 'rpc.WaitExec', 'rpc.ReleaseLease'));

CREATE VIEW verself.sandbox_execution_phase_timeline AS
WITH toUUID('00000000-0000-0000-0000-000000000000') AS zero_uuid
SELECT
    t.observed_at AS observed_at,
    t.event_source AS event_source,
    t.phase_group AS phase_group,
    t.phase_name AS phase_name,
    t.phase_order AS phase_order,
    t.result AS result,
    t.reason AS reason,
    if(t.org_id = '', l.org_id, t.org_id) AS org_id,
    if(t.execution_id = zero_uuid, l.execution_id, t.execution_id) AS execution_id,
    if(t.attempt_id = zero_uuid, l.attempt_id, t.attempt_id) AS attempt_id,
    if(t.allocation_id = zero_uuid, l.allocation_id, t.allocation_id) AS allocation_id,
    if(t.provider = '', l.provider, t.provider) AS provider,
    if(t.external_provider = '', l.external_provider, t.external_provider) AS external_provider,
    if(t.provider_installation_id = 0, l.provider_installation_id, t.provider_installation_id) AS provider_installation_id,
    if(t.provider_repository_id = 0, l.provider_repository_id, t.provider_repository_id) AS provider_repository_id,
    if(t.provider_run_id = 0, l.provider_run_id, t.provider_run_id) AS provider_run_id,
    if(t.provider_run_attempt = 0, l.provider_run_attempt, t.provider_run_attempt) AS provider_run_attempt,
    if(t.provider_job_id = 0, l.provider_job_id, t.provider_job_id) AS provider_job_id,
    if(t.repository_full_name = '', l.repository_full_name, t.repository_full_name) AS repository_full_name,
    if(t.workflow_name = '', l.workflow_name, t.workflow_name) AS workflow_name,
    if(t.job_name = '', l.job_name, t.job_name) AS job_name,
    if(t.head_branch = '', l.head_branch, t.head_branch) AS head_branch,
    if(t.head_sha = '', l.head_sha, t.head_sha) AS head_sha,
    if(t.runner_class = '', l.runner_class, t.runner_class) AS runner_class,
    if(t.runner_name = '', l.runner_name, t.runner_name) AS runner_name,
    t.lease_id AS lease_id,
    t.exec_id AS exec_id,
    if(t.correlation_id = '', l.correlation_id, t.correlation_id) AS correlation_id,
    t.started_at AS started_at,
    t.completed_at AS completed_at,
    t.duration_ms AS duration_ms,
    t.attributes_json AS attributes_json,
    t.trace_id AS trace_id,
    t.span_id AS span_id
FROM verself.sandbox_phase_timeline AS t
LEFT JOIN
(
    SELECT
        lease_id,
        anyLast(e.org_id) AS org_id,
        anyLast(e.execution_id) AS execution_id,
        anyLast(e.attempt_id) AS attempt_id,
        anyLast(e.allocation_id) AS allocation_id,
        anyLast(e.provider) AS provider,
        anyLast(e.external_provider) AS external_provider,
        anyLast(e.provider_installation_id) AS provider_installation_id,
        anyLast(e.provider_repository_id) AS provider_repository_id,
        anyLast(e.provider_run_id) AS provider_run_id,
        anyLast(e.provider_run_attempt) AS provider_run_attempt,
        anyLast(e.provider_job_id) AS provider_job_id,
        anyLast(e.repository_full_name) AS repository_full_name,
        anyLast(e.workflow_name) AS workflow_name,
        anyLast(e.job_name) AS job_name,
        anyLast(e.head_branch) AS head_branch,
        anyLast(e.head_sha) AS head_sha,
        anyLast(e.runner_class) AS runner_class,
        anyLast(e.runner_name) AS runner_name,
        anyLast(e.correlation_id) AS correlation_id
    FROM verself.sandbox_phase_events AS e
    WHERE e.lease_id != ''
      AND e.execution_id != toUUID('00000000-0000-0000-0000-000000000000')
    GROUP BY lease_id
) AS l ON t.lease_id = l.lease_id;

GRANT SELECT, INSERT ON verself.sandbox_phase_events TO sandbox_rental;
GRANT SELECT ON verself.sandbox_phase_timeline TO sandbox_rental;
GRANT SELECT ON verself.sandbox_execution_phase_timeline TO sandbox_rental;
