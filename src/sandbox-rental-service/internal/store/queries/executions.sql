-- name: InsertExecution :one
INSERT INTO executions (
    execution_id, org_id, actor_id, kind, source_kind, workload_kind, source_ref,
    runner_class, external_provider, external_task_id, provider, product_id,
    state, correlation_id, idempotency_key, run_command, max_wall_seconds,
    requested_vcpus, requested_memory_mib, requested_root_disk_gib, requested_kernel_image,
    created_at, updated_at
) VALUES (
    sqlc.arg(execution_id), sqlc.arg(org_id), sqlc.arg(actor_id), sqlc.arg(kind), sqlc.arg(source_kind), sqlc.arg(workload_kind), sqlc.arg(source_ref),
    sqlc.arg(runner_class), sqlc.arg(external_provider), sqlc.arg(external_task_id), sqlc.arg(provider), sqlc.arg(product_id),
    sqlc.arg(state), sqlc.arg(correlation_id), sqlc.arg(idempotency_key), sqlc.arg(run_command), sqlc.arg(max_wall_seconds),
    sqlc.arg(requested_vcpus), sqlc.arg(requested_memory_mib), sqlc.arg(requested_root_disk_gib), sqlc.arg(requested_kernel_image),
    sqlc.arg(created_at), sqlc.arg(created_at)
)
ON CONFLICT (org_id, idempotency_key) DO NOTHING
RETURNING execution_id;

-- name: InsertExecutionAttempt :exec
INSERT INTO execution_attempts (
    attempt_id, execution_id, attempt_seq, state, created_at, updated_at
) VALUES (sqlc.arg(attempt_id), sqlc.arg(execution_id), 1, sqlc.arg(state), sqlc.arg(created_at), sqlc.arg(created_at));

-- name: GetExistingSubmission :one
SELECT e.execution_id, a.attempt_id
FROM executions e
JOIN execution_attempts a ON a.execution_id = e.execution_id
WHERE e.org_id = sqlc.arg(org_id) AND e.idempotency_key = sqlc.arg(idempotency_key)
ORDER BY a.attempt_seq DESC
LIMIT 1;

-- name: GetRunnerClassResources :one
SELECT product_id, vcpus, memory_mib, rootfs_gib
FROM runner_classes
WHERE runner_class = sqlc.arg(runner_class) AND active;

-- name: ListRunnerClassFilesystemMounts :many
SELECT mount_name, source_ref, mount_path, fs_type, read_only
FROM runner_class_filesystem_mounts
WHERE runner_class = sqlc.arg(runner_class) AND active
ORDER BY sort_order, mount_name;

-- name: InsertExecutionFilesystemMount :exec
INSERT INTO execution_filesystem_mounts (
    execution_id, mount_name, source_ref, mount_path, fs_type, read_only, sort_order, created_at
) VALUES (
    sqlc.arg(execution_id), sqlc.arg(mount_name), sqlc.arg(source_ref), sqlc.arg(mount_path),
    sqlc.arg(fs_type), sqlc.arg(read_only), sqlc.arg(sort_order), sqlc.arg(created_at)
);

-- name: InsertExecutionStickyDiskMount :exec
INSERT INTO execution_sticky_disk_mounts (
    mount_id, execution_id, attempt_id, allocation_id, mount_name, key_hash, key, mount_path,
    base_generation, source_ref, target_source_ref, save_requested, save_state, committed_generation,
    failure_reason, sort_order, created_at, updated_at
) VALUES (
    sqlc.arg(mount_id), sqlc.arg(execution_id), sqlc.arg(attempt_id), sqlc.arg(allocation_id),
    sqlc.arg(mount_name), sqlc.arg(key_hash), sqlc.arg(key), sqlc.arg(mount_path),
    sqlc.arg(base_generation), sqlc.arg(source_ref), sqlc.arg(target_source_ref),
    false, sqlc.arg(save_state), 0, '', sqlc.arg(sort_order), sqlc.arg(created_at), sqlc.arg(created_at)
);

-- name: GetExecutionWorkItem :one
SELECT
    e.execution_id,
    a.attempt_id,
    e.org_id,
    e.actor_id,
    e.kind,
    e.source_kind,
    e.workload_kind,
    e.source_ref,
    e.runner_class,
    e.external_provider,
    e.external_task_id,
    e.provider,
    e.product_id,
    e.run_command,
    e.max_wall_seconds,
    e.correlation_id,
    e.requested_vcpus,
    e.requested_memory_mib,
    e.requested_root_disk_gib,
    e.requested_kernel_image,
    COALESCE(a.lease_id, '')::text AS lease_id,
    COALESCE(a.exec_id, '')::text AS exec_id
FROM executions e
JOIN execution_attempts a ON a.execution_id = e.execution_id
WHERE e.execution_id = sqlc.arg(execution_id) AND a.attempt_id = sqlc.arg(attempt_id);

-- name: ListExecutionFilesystemMounts :many
SELECT mount_name, source_ref, mount_path, fs_type, read_only
FROM execution_filesystem_mounts
WHERE execution_id = sqlc.arg(execution_id)
ORDER BY sort_order, mount_name;

-- name: InsertExecutionBillingWindow :exec
INSERT INTO execution_billing_windows (
    attempt_id, window_seq, billing_window_id, reservation_shape, reserved_quantity, actual_quantity,
    reserved_charge_units, billed_charge_units, writeoff_charge_units, cost_per_unit,
    pricing_phase, state, window_start, created_at, reservation_jsonb
) VALUES (
    sqlc.arg(attempt_id), sqlc.arg(window_seq), sqlc.arg(billing_window_id), sqlc.arg(reservation_shape),
    sqlc.arg(reserved_quantity), 0, sqlc.arg(reserved_charge_units), 0, 0, sqlc.arg(cost_per_unit),
    sqlc.arg(pricing_phase), 'reserved', sqlc.arg(window_start), sqlc.arg(created_at), sqlc.arg(reservation_jsonb)::jsonb
);

-- name: MarkExecutionBillingWindow :exec
UPDATE execution_billing_windows
SET state = sqlc.arg(state),
    actual_quantity = sqlc.arg(actual_quantity),
    billed_charge_units = sqlc.arg(billed_charge_units),
    writeoff_charge_units = sqlc.arg(writeoff_charge_units),
    settled_at = sqlc.arg(settled_at)
WHERE attempt_id = sqlc.arg(attempt_id)
  AND billing_window_id = sqlc.arg(billing_window_id);

-- name: SetExecutionState :exec
UPDATE executions
SET state = sqlc.arg(state), updated_at = sqlc.arg(updated_at)
WHERE execution_id = sqlc.arg(execution_id);

-- name: CASAttemptState :execrows
UPDATE execution_attempts
SET state = sqlc.arg(to_state),
    billing_job_id = COALESCE(NULLIF(sqlc.arg(billing_job_id)::bigint, 0), billing_job_id),
    updated_at = sqlc.arg(updated_at)
WHERE attempt_id = sqlc.arg(attempt_id)
  AND state = sqlc.arg(from_state);

-- name: MarkAttemptRunningCAS :execrows
UPDATE execution_attempts
SET state = sqlc.arg(to_state),
    started_at = sqlc.arg(started_at),
    updated_at = sqlc.arg(updated_at)
WHERE attempt_id = sqlc.arg(attempt_id)
  AND state = sqlc.arg(from_state);

-- name: SetAttemptLeaseExec :exec
UPDATE execution_attempts
SET lease_id = NULLIF(sqlc.arg(lease_id)::text, ''),
    exec_id = NULLIF(sqlc.arg(exec_id)::text, ''),
    updated_at = sqlc.arg(updated_at)
WHERE attempt_id = sqlc.arg(attempt_id);

-- name: CompleteAttemptCAS :execrows
UPDATE execution_attempts
SET state = sqlc.arg(state),
    failure_reason = sqlc.arg(failure_reason),
    exit_code = sqlc.arg(exit_code),
    duration_ms = sqlc.arg(duration_ms),
    zfs_written = sqlc.arg(zfs_written),
    stdout_bytes = sqlc.arg(stdout_bytes),
    stderr_bytes = sqlc.arg(stderr_bytes),
    rootfs_provisioned_bytes = sqlc.arg(rootfs_provisioned_bytes),
    boot_time_us = sqlc.arg(boot_time_us),
    block_read_bytes = sqlc.arg(block_read_bytes),
    block_write_bytes = sqlc.arg(block_write_bytes),
    net_rx_bytes = sqlc.arg(net_rx_bytes),
    net_tx_bytes = sqlc.arg(net_tx_bytes),
    vcpu_exit_count = sqlc.arg(vcpu_exit_count),
    trace_id = sqlc.arg(trace_id),
    completed_at = sqlc.arg(completed_at),
    updated_at = sqlc.arg(updated_at)
WHERE attempt_id = sqlc.arg(attempt_id)
  AND state = sqlc.arg(from_state);

-- name: MarkAttemptFailed :exec
UPDATE execution_attempts
SET state = sqlc.arg(state),
    failure_reason = sqlc.arg(failure_reason),
    trace_id = sqlc.arg(trace_id),
    completed_at = sqlc.arg(completed_at),
    updated_at = sqlc.arg(completed_at)
WHERE attempt_id = sqlc.arg(attempt_id);

-- name: InsertExecutionEvent :exec
INSERT INTO execution_events (
    execution_id, attempt_id, from_state, to_state, reason, created_at
) VALUES (
    sqlc.arg(execution_id), sqlc.arg(attempt_id), sqlc.arg(from_state), sqlc.arg(to_state), sqlc.arg(reason), sqlc.arg(created_at)
);

-- name: GetLatestAttemptForExecution :one
SELECT a.attempt_id
FROM executions e
JOIN execution_attempts a ON a.execution_id = e.execution_id
WHERE e.org_id = sqlc.arg(org_id) AND e.execution_id = sqlc.arg(execution_id)
ORDER BY a.attempt_seq DESC
LIMIT 1;

-- name: ListExecutionLogChunks :many
SELECT chunk
FROM execution_logs
WHERE attempt_id = sqlc.arg(attempt_id)
ORDER BY seq ASC;

-- name: InsertExecutionLog :exec
INSERT INTO execution_logs (
    execution_id, attempt_id, seq, stream, chunk, created_at
) VALUES (
    sqlc.arg(execution_id), sqlc.arg(attempt_id), 1, 'combined', sqlc.arg(chunk), sqlc.arg(created_at)
);

-- name: ListExecutionBillingWindows :many
SELECT
    attempt_id,
    billing_window_id,
    window_seq,
    reservation_shape,
    reserved_quantity,
    actual_quantity,
    reserved_charge_units,
    billed_charge_units,
    writeoff_charge_units,
    cost_per_unit,
    pricing_phase,
    state,
    window_start,
    created_at,
    settled_at
FROM execution_billing_windows
WHERE attempt_id = sqlc.arg(attempt_id)
ORDER BY window_seq;

-- name: ListStaleReservedAttempts :many
SELECT e.execution_id, a.attempt_id, COALESCE(w.billing_window_id, '')::text AS billing_window_id
FROM executions e
JOIN execution_attempts a ON a.execution_id = e.execution_id
LEFT JOIN execution_billing_windows w ON w.attempt_id = a.attempt_id
WHERE a.state = sqlc.arg(state)
  AND COALESCE(w.state, 'reserved') = 'reserved'
  AND COALESCE(a.lease_id, '') = ''
  AND a.updated_at < (now() - (sqlc.arg(stale_seconds) * interval '1 second'));

-- name: ListStaleLaunchingAttempts :many
SELECT e.execution_id, a.attempt_id, COALESCE(w.billing_window_id, '')::text AS billing_window_id
FROM executions e
JOIN execution_attempts a ON a.execution_id = e.execution_id
LEFT JOIN LATERAL (
    SELECT billing_window_id
    FROM execution_billing_windows
    WHERE attempt_id = a.attempt_id
    ORDER BY window_seq DESC
    LIMIT 1
) w ON true
WHERE a.state = sqlc.arg(state)
  AND COALESCE(a.exec_id, '') = ''
  AND a.updated_at < (now() - (sqlc.arg(stale_seconds) * interval '1 second'));

-- name: ListCleanedRunnerAttempts :many
SELECT e.execution_id, a.attempt_id, COALESCE(w.billing_window_id, '')::text AS billing_window_id
FROM executions e
JOIN execution_attempts a ON a.execution_id = e.execution_id
JOIN runner_allocations ra ON ra.execution_id = e.execution_id
LEFT JOIN LATERAL (
    SELECT billing_window_id
    FROM execution_billing_windows
    WHERE attempt_id = a.attempt_id
    ORDER BY window_seq DESC
    LIMIT 1
) w ON true
WHERE e.workload_kind = sqlc.arg(workload_kind)
  AND a.state = sqlc.arg(state)
  AND ra.state = 'cleaned'
  AND ra.updated_at < (now() - (sqlc.arg(stale_seconds) * interval '1 second'));
