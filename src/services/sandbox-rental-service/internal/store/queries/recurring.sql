-- name: InsertExecutionSchedule :exec
INSERT INTO execution_schedules (
    schedule_id, org_id, actor_id, display_name, idempotency_key,
    temporal_schedule_id, temporal_namespace, task_queue, state,
    interval_seconds, project_id, source_repository_id, workflow_path, ref, inputs_json, created_at, updated_at
) VALUES (
    sqlc.arg(schedule_id), sqlc.arg(org_id), sqlc.arg(actor_id), sqlc.arg(display_name), sqlc.arg(idempotency_key),
    sqlc.arg(temporal_schedule_id), sqlc.arg(temporal_namespace), sqlc.arg(task_queue), sqlc.arg(state),
    sqlc.arg(interval_seconds), sqlc.arg(project_id), sqlc.arg(source_repository_id), sqlc.arg(workflow_path), sqlc.arg(ref),
    sqlc.arg(inputs_json)::jsonb, sqlc.arg(created_at), sqlc.arg(updated_at)
);

-- name: ListExecutionSchedules :many
SELECT
    schedule_id,
    org_id,
    actor_id,
    display_name,
    idempotency_key,
    temporal_schedule_id,
    temporal_namespace,
    task_queue,
    state,
    interval_seconds,
    project_id,
    source_repository_id,
    workflow_path,
    ref,
    inputs_json,
    created_at,
    updated_at
FROM execution_schedules
WHERE org_id = sqlc.arg(org_id)
ORDER BY created_at DESC, schedule_id DESC;

-- name: GetExecutionSchedule :one
SELECT
    schedule_id,
    org_id,
    actor_id,
    display_name,
    idempotency_key,
    temporal_schedule_id,
    temporal_namespace,
    task_queue,
    state,
    interval_seconds,
    project_id,
    source_repository_id,
    workflow_path,
    ref,
    inputs_json,
    created_at,
    updated_at
FROM execution_schedules
WHERE org_id = sqlc.arg(org_id)
  AND schedule_id = sqlc.arg(schedule_id);

-- name: GetExecutionScheduleByIdempotencyKey :one
SELECT
    schedule_id,
    org_id,
    actor_id,
    display_name,
    idempotency_key,
    temporal_schedule_id,
    temporal_namespace,
    task_queue,
    state,
    interval_seconds,
    project_id,
    source_repository_id,
    workflow_path,
    ref,
    inputs_json,
    created_at,
    updated_at
FROM execution_schedules
WHERE org_id = sqlc.arg(org_id)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: UpdateExecutionScheduleState :execrows
UPDATE execution_schedules
SET state = sqlc.arg(state),
    updated_at = sqlc.arg(updated_at)
WHERE schedule_id = sqlc.arg(schedule_id);

-- name: ListExecutionScheduleDispatches :many
SELECT
    dispatch_id,
    schedule_id,
    temporal_workflow_id,
    temporal_run_id,
    source_workflow_run_id,
    project_id,
    workflow_state,
    state,
    failure_reason,
    scheduled_at,
    submitted_at,
    created_at,
    updated_at
FROM execution_schedule_dispatches
WHERE schedule_id = sqlc.arg(schedule_id)
ORDER BY created_at DESC, dispatch_id DESC
LIMIT sqlc.arg(limit_count);

-- name: UpsertExecutionScheduleDispatchStart :one
INSERT INTO execution_schedule_dispatches (
    dispatch_id, schedule_id, temporal_workflow_id, temporal_run_id,
    project_id, state, failure_reason, scheduled_at, submitted_at, created_at, updated_at
) VALUES (
    sqlc.arg(dispatch_id), sqlc.arg(schedule_id), sqlc.arg(temporal_workflow_id), sqlc.arg(temporal_run_id),
    sqlc.arg(project_id), sqlc.arg(state), '', sqlc.arg(scheduled_at), NULL, sqlc.arg(created_at), sqlc.arg(created_at)
)
ON CONFLICT (schedule_id, temporal_workflow_id, temporal_run_id)
DO UPDATE SET updated_at = EXCLUDED.updated_at
RETURNING
    dispatch_id,
    schedule_id,
    temporal_workflow_id,
    temporal_run_id,
    source_workflow_run_id,
    project_id,
    workflow_state,
    state,
    failure_reason,
    scheduled_at,
    submitted_at,
    created_at,
    updated_at;

-- name: MarkExecutionScheduleDispatchSubmitted :execrows
UPDATE execution_schedule_dispatches
SET state = sqlc.arg(state),
    failure_reason = '',
    source_workflow_run_id = sqlc.arg(source_workflow_run_id),
    workflow_state = sqlc.arg(workflow_state),
    submitted_at = sqlc.arg(submitted_at),
    updated_at = sqlc.arg(submitted_at)
WHERE dispatch_id = sqlc.arg(dispatch_id);

-- name: MarkExecutionScheduleDispatchFailed :exec
UPDATE execution_schedule_dispatches
SET state = sqlc.arg(state),
    failure_reason = sqlc.arg(failure_reason),
    updated_at = sqlc.arg(updated_at)
WHERE dispatch_id = sqlc.arg(dispatch_id);
