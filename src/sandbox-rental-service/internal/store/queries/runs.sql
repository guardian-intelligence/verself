-- name: ListRuns :many
SELECT
    e.execution_id,
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
    e.state,
    e.correlation_id,
    e.idempotency_key,
    e.run_command,
    e.created_at,
    e.updated_at,
    a.attempt_id,
    a.attempt_seq,
    a.state AS attempt_state,
    COALESCE(a.lease_id, '')::text AS lease_id,
    COALESCE(a.exec_id, '')::text AS exec_id,
    COALESCE(a.billing_job_id, 0)::bigint AS billing_job_id,
    a.failure_reason,
    a.exit_code,
    a.duration_ms,
    a.zfs_written,
    a.stdout_bytes,
    a.stderr_bytes,
    a.rootfs_provisioned_bytes,
    a.boot_time_us,
    a.block_read_bytes,
    a.block_write_bytes,
    a.net_rx_bytes,
    a.net_tx_bytes,
    a.vcpu_exit_count,
    a.trace_id,
    a.started_at,
    a.completed_at,
    a.created_at AS attempt_created_at,
    a.updated_at AS attempt_updated_at,
    COALESCE(rr.provider_installation_id, 0)::bigint AS provider_installation_id,
    COALESCE(rr.provider_run_id, 0)::bigint AS provider_run_id,
    COALESCE(rr.provider_job_id, 0)::bigint AS provider_job_id,
    COALESCE(rr.repository_full_name, '')::text AS repository_full_name,
    COALESCE(rr.workflow_name, '')::text AS workflow_name,
    COALESCE(rr.job_name, '')::text AS job_name,
    COALESCE(rr.head_branch, '')::text AS head_branch,
    COALESCE(rr.head_sha, '')::text AS head_sha,
    COALESCE(sc.schedule_id, '00000000-0000-0000-0000-000000000000'::uuid) AS schedule_id,
    COALESCE(sc.display_name, '')::text AS schedule_display_name,
    COALESCE(sc.temporal_workflow_id, '')::text AS temporal_workflow_id,
    COALESCE(sc.temporal_run_id, '')::text AS temporal_run_id
FROM executions e
JOIN LATERAL (
    SELECT attempt_id, attempt_seq, state, lease_id, exec_id, billing_job_id, failure_reason, exit_code,
           duration_ms, zfs_written, stdout_bytes, stderr_bytes, rootfs_provisioned_bytes, boot_time_us,
           block_read_bytes, block_write_bytes, net_rx_bytes, net_tx_bytes, vcpu_exit_count,
           trace_id, started_at, completed_at, created_at, updated_at
    FROM execution_attempts
    WHERE execution_id = e.execution_id
    ORDER BY attempt_seq DESC
    LIMIT 1
) a ON true
LEFT JOIN LATERAL (
    SELECT
        j.provider_installation_id AS provider_installation_id,
        j.provider_run_id AS provider_run_id,
        j.provider_job_id AS provider_job_id,
        j.repository_full_name,
        j.workflow_name,
        j.job_name,
        j.head_branch,
        j.head_sha
    FROM runner_allocations ga
    LEFT JOIN runner_job_bindings gb ON gb.allocation_id = ga.allocation_id
    LEFT JOIN runner_jobs j ON j.provider = ga.provider AND j.provider_job_id = COALESCE(gb.provider_job_id, ga.requested_for_provider_job_id)
    WHERE ga.execution_id = e.execution_id
    ORDER BY j.updated_at DESC, j.provider_job_id DESC
    LIMIT 1
) rr ON true
LEFT JOIN LATERAL (
    SELECT
        d.schedule_id,
        s.display_name,
        d.temporal_workflow_id,
        d.temporal_run_id
    FROM execution_schedule_dispatches d
    JOIN execution_schedules s ON s.schedule_id = d.schedule_id
    WHERE d.source_workflow_run_id = e.execution_id
    ORDER BY d.created_at DESC, d.dispatch_id DESC
    LIMIT 1
) sc ON true
WHERE e.org_id = sqlc.arg(org_id)
  AND (sqlc.arg(source_kind)::text = '' OR e.source_kind = sqlc.arg(source_kind))
  AND (sqlc.arg(status)::text = '' OR e.state = sqlc.arg(status))
  AND (sqlc.arg(repository)::text = '' OR COALESCE(rr.repository_full_name, '') = sqlc.arg(repository))
  AND (sqlc.arg(workflow)::text = '' OR COALESCE(rr.workflow_name, '') = sqlc.arg(workflow))
  AND (sqlc.arg(branch)::text = '' OR COALESCE(rr.head_branch, '') = sqlc.arg(branch))
  AND (sqlc.arg(runner_class)::text = '' OR e.runner_class = sqlc.arg(runner_class))
  AND (sqlc.arg(cursor_enabled)::boolean = false OR (e.updated_at, e.execution_id) < (sqlc.arg(cursor_updated_at)::timestamptz, sqlc.arg(cursor_execution_id)::uuid))
ORDER BY e.updated_at DESC, e.execution_id DESC
LIMIT sqlc.arg(limit_count);

-- name: GetRun :one
SELECT
    e.execution_id,
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
    e.state,
    e.correlation_id,
    e.idempotency_key,
    e.run_command,
    e.created_at,
    e.updated_at,
    a.attempt_id,
    a.attempt_seq,
    a.state AS attempt_state,
    COALESCE(a.lease_id, '')::text AS lease_id,
    COALESCE(a.exec_id, '')::text AS exec_id,
    COALESCE(a.billing_job_id, 0)::bigint AS billing_job_id,
    a.failure_reason,
    a.exit_code,
    a.duration_ms,
    a.zfs_written,
    a.stdout_bytes,
    a.stderr_bytes,
    a.rootfs_provisioned_bytes,
    a.boot_time_us,
    a.block_read_bytes,
    a.block_write_bytes,
    a.net_rx_bytes,
    a.net_tx_bytes,
    a.vcpu_exit_count,
    a.trace_id,
    a.started_at,
    a.completed_at,
    a.created_at AS attempt_created_at,
    a.updated_at AS attempt_updated_at,
    COALESCE(rr.provider_installation_id, 0)::bigint AS provider_installation_id,
    COALESCE(rr.provider_run_id, 0)::bigint AS provider_run_id,
    COALESCE(rr.provider_job_id, 0)::bigint AS provider_job_id,
    COALESCE(rr.repository_full_name, '')::text AS repository_full_name,
    COALESCE(rr.workflow_name, '')::text AS workflow_name,
    COALESCE(rr.job_name, '')::text AS job_name,
    COALESCE(rr.head_branch, '')::text AS head_branch,
    COALESCE(rr.head_sha, '')::text AS head_sha,
    COALESCE(sc.schedule_id, '00000000-0000-0000-0000-000000000000'::uuid) AS schedule_id,
    COALESCE(sc.display_name, '')::text AS schedule_display_name,
    COALESCE(sc.temporal_workflow_id, '')::text AS temporal_workflow_id,
    COALESCE(sc.temporal_run_id, '')::text AS temporal_run_id
FROM executions e
JOIN LATERAL (
    SELECT attempt_id, attempt_seq, state, lease_id, exec_id, billing_job_id, failure_reason, exit_code,
           duration_ms, zfs_written, stdout_bytes, stderr_bytes, rootfs_provisioned_bytes, boot_time_us,
           block_read_bytes, block_write_bytes, net_rx_bytes, net_tx_bytes, vcpu_exit_count,
           trace_id, started_at, completed_at, created_at, updated_at
    FROM execution_attempts
    WHERE execution_id = e.execution_id
    ORDER BY attempt_seq DESC
    LIMIT 1
) a ON true
LEFT JOIN LATERAL (
    SELECT
        j.provider_installation_id AS provider_installation_id,
        j.provider_run_id AS provider_run_id,
        j.provider_job_id AS provider_job_id,
        j.repository_full_name,
        j.workflow_name,
        j.job_name,
        j.head_branch,
        j.head_sha
    FROM runner_allocations ga
    LEFT JOIN runner_job_bindings gb ON gb.allocation_id = ga.allocation_id
    LEFT JOIN runner_jobs j ON j.provider = ga.provider AND j.provider_job_id = COALESCE(gb.provider_job_id, ga.requested_for_provider_job_id)
    WHERE ga.execution_id = e.execution_id
    ORDER BY j.updated_at DESC, j.provider_job_id DESC
    LIMIT 1
) rr ON true
LEFT JOIN LATERAL (
    SELECT
        d.schedule_id,
        s.display_name,
        d.temporal_workflow_id,
        d.temporal_run_id
    FROM execution_schedule_dispatches d
    JOIN execution_schedules s ON s.schedule_id = d.schedule_id
    WHERE d.source_workflow_run_id = e.execution_id
    ORDER BY d.created_at DESC, d.dispatch_id DESC
    LIMIT 1
) sc ON true
WHERE e.org_id = sqlc.arg(org_id)
  AND e.execution_id = sqlc.arg(execution_id);

-- name: ListRunBillingSummaries :many
SELECT
    attempt_id,
    count(*)::int AS window_count,
    COALESCE(sum(reserved_charge_units), 0)::bigint AS reserved_charge_units,
    COALESCE(sum(billed_charge_units), 0)::bigint AS billed_charge_units,
    COALESCE(sum(writeoff_charge_units), 0)::bigint AS writeoff_charge_units,
    COALESCE(max(cost_per_unit), 0)::bigint AS cost_per_unit,
    COALESCE(max(pricing_phase), '')::text AS pricing_phase
FROM execution_billing_windows
WHERE attempt_id = ANY(sqlc.arg(attempt_ids)::uuid[])
GROUP BY attempt_id;

-- name: ListStickyDiskMountsForAttempts :many
SELECT
    attempt_id,
    mount_id,
    mount_name,
    key_hash,
    mount_path,
    base_generation,
    committed_generation,
    save_requested,
    save_state,
    failure_reason,
    requested_at,
    completed_at
FROM execution_sticky_disk_mounts
WHERE attempt_id = ANY(sqlc.arg(attempt_ids)::uuid[])
ORDER BY sort_order, mount_name;
